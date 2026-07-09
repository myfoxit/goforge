// Package awsig implements AWS Signature Version 4 request signing without
// pulling in the AWS SDK. It is used by the SES mail adapter and the S3
// storage backend (and works with S3-compatible services like MinIO and R2).
package awsig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Credentials are a static AWS access key pair.
type Credentials struct {
	AccessKey string
	SecretKey string
	// SessionToken for temporary credentials (optional).
	SessionToken string
}

const timeFormat = "20060102T150405Z"

// Sign adds SigV4 headers (Authorization, X-Amz-Date, X-Amz-Content-Sha256)
// to req. body must be the exact request payload (nil for empty).
func Sign(req *http.Request, body []byte, creds Credentials, region, service string, now time.Time) error {
	amzDate := now.Format(timeFormat)
	dateStamp := now.Format("20060102")

	payloadHash := hashHex(body)
	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}
	if service == "s3" {
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}

	canonicalURI := uriEncodePath(req.URL.EscapedPath())
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := canonicalQueryString(req.URL.Query())
	signedHeaders, canonicalHeaders := canonicalizeHeaders(req)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hashHex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveKey(creds.SecretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization", strings.Join([]string{
		"AWS4-HMAC-SHA256 Credential=" + creds.AccessKey + "/" + scope,
		"SignedHeaders=" + signedHeaders,
		"Signature=" + signature,
	}, ", "))
	return nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func deriveKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

// canonicalQueryString sorts and re-encodes query parameters per SigV4.
func canonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string{}, q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// canonicalizeHeaders returns (signedHeaders, canonicalHeaders) covering
// host and all x-amz-* / content-type headers present.
func canonicalizeHeaders(req *http.Request) (string, string) {
	include := map[string]string{"host": req.URL.Host}
	if h := req.Header.Get("Host"); h != "" {
		include["host"] = h
	}
	for name, vals := range req.Header {
		lower := strings.ToLower(name)
		if lower == "content-type" || strings.HasPrefix(lower, "x-amz-") {
			include[lower] = strings.Join(vals, ",")
		}
	}
	names := make([]string, 0, len(include))
	for name := range include {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.TrimSpace(collapseSpaces(include[name])))
		b.WriteByte('\n')
	}
	return strings.Join(names, ";"), b.String()
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// uriEncode percent-encodes per RFC 3986 (SigV4 strict form).
func uriEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0xf])
	}
	return b.String()
}

// uriEncodePath keeps path separators but normalizes other escaping.
func uriEncodePath(escapedPath string) string {
	// The path is already URL-escaped by net/url; SigV4 wants each segment
	// encoded with the strict set, so decode+re-encode segment-wise.
	segments := strings.Split(escapedPath, "/")
	for i, seg := range segments {
		dec, err := url.PathUnescape(seg)
		if err != nil {
			continue
		}
		segments[i] = uriEncode(dec)
	}
	return strings.Join(segments, "/")
}
