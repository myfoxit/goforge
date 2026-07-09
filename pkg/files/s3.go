package files

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/myfoxit/goforge/pkg/awsig"
)

// S3Storage talks to S3 or any S3-compatible service (MinIO, R2, ...)
// using path-style requests and SigV4 — no SDK required.
type S3Storage struct {
	Endpoint string // e.g. https://s3.eu-central-1.amazonaws.com or http://localhost:9000
	Region   string
	Bucket   string
	Creds    awsig.Credentials

	client *http.Client
}

// NewS3 builds an S3 storage. endpoint may be empty for AWS
// (derived from region).
func NewS3(endpoint, region, bucket, accessKey, secretKey string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("files: s3 bucket required")
	}
	if endpoint == "" {
		if region == "" {
			return nil, fmt.Errorf("files: s3 region or endpoint required")
		}
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", region)
	}
	if region == "" {
		region = "us-east-1"
	}
	return &S3Storage{
		Endpoint: strings.TrimSuffix(endpoint, "/"),
		Region:   region,
		Bucket:   bucket,
		Creds:    awsig.Credentials{AccessKey: accessKey, SecretKey: secretKey},
		client:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (s *S3Storage) Name() string { return "s3" }

func (s *S3Storage) objectURL(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return fmt.Sprintf("%s/%s/%s", s.Endpoint, url.PathEscape(s.Bucket), strings.Join(segments, "/"))
}

func (s *S3Storage) do(ctx context.Context, method, rawURL string, body []byte, contentType string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if err := awsig.Sign(req, body, s.Creds, s.Region, "s3", time.Now().UTC()); err != nil {
		return nil, err
	}
	return s.client.Do(req)
}

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	// SigV4 needs the payload hash, so buffer the object.
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, "PUT", s.objectURL(key), body, contentType)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		return s3Err("put", key, resp)
	}
	return nil
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, *FileInfo, error) {
	resp, err := s.do(ctx, "GET", s.objectURL(key), nil, "")
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 300 {
		defer drain(resp)
		return nil, nil, s3Err("get", key, resp)
	}
	info := &FileInfo{
		Size:        resp.ContentLength,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
	}
	if info.ContentType == "" {
		info.ContentType = ContentTypeByName(key)
	}
	return resp.Body, info, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	resp, err := s.do(ctx, "DELETE", s.objectURL(key), nil, "")
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		return s3Err("delete", key, resp)
	}
	return nil
}

// listResult is the ListObjectsV2 XML envelope.
type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

func (s *S3Storage) DeletePrefix(ctx context.Context, prefix string) error {
	continuation := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if continuation != "" {
			q.Set("continuation-token", continuation)
		}
		listURL := fmt.Sprintf("%s/%s?%s", s.Endpoint, url.PathEscape(s.Bucket), q.Encode())
		resp, err := s.do(ctx, "GET", listURL, nil, "")
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 {
			defer drain(resp)
			return s3Err("list", prefix, resp)
		}
		var result listResult
		err = xml.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return err
		}
		for _, obj := range result.Contents {
			if err := s.Delete(ctx, obj.Key); err != nil {
				return err
			}
		}
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return nil
		}
		continuation = result.NextContinuationToken
	}
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	resp, err := s.do(ctx, "HEAD", s.objectURL(key), nil, "")
	if err != nil {
		return false, err
	}
	defer drain(resp)
	switch {
	case resp.StatusCode == 200:
		return true, nil
	case resp.StatusCode == 404:
		return false, nil
	default:
		return false, s3Err("head", key, resp)
	}
}

func drain(resp *http.Response) {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
}

func s3Err(op, key string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("files: s3 %s %q: status %d: %s", op, key, resp.StatusCode, strings.TrimSpace(string(body)))
}
