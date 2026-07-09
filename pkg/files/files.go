// Package files provides pluggable blob storage for record file fields:
// local filesystem (default) and S3-compatible object storage, plus
// on-demand image thumbnails.
package files

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path"
	"regexp"
	"strings"

	"github.com/myfoxit/goforge/pkg/security"
)

// FileInfo describes a stored blob.
type FileInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

// Storage abstracts a blob store. Keys use forward slashes:
// "<collection>/<recordId>/<filename>".
type Storage interface {
	Name() string
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, *FileInfo, error)
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes all blobs under a prefix (record/collection cleanup).
	DeletePrefix(ctx context.Context, prefix string) error
	Exists(ctx context.Context, key string) (bool, error)
}

var filenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_\-.]+`)

// SanitizeFilename makes a client filename safe and appends a random suffix
// before the extension: "My Report.pdf" → "my_report_a1b2c3d4.pdf".
func SanitizeFilename(original string) string {
	base := path.Base(original)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = filenameSanitizer.ReplaceAllString(strings.ToLower(name), "_")
	name = strings.Trim(name, "._-")
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "file"
	}
	ext = filenameSanitizer.ReplaceAllString(strings.ToLower(ext), "")
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("%s_%s%s", name, security.RandomID(10), ext)
}

// ContentTypeByName guesses a content type from the file extension.
func ContentTypeByName(name string) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// Key builds a storage key.
func Key(collection, recordID, filename string) string {
	return collection + "/" + recordID + "/" + filename
}

// ThumbKey builds the cache key for a thumbnail variant.
func ThumbKey(collection, recordID, filename, size string) string {
	return ".thumbs/" + collection + "/" + recordID + "/" + size + "_" + filename
}
