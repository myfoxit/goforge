package files

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage stores blobs under a root directory.
type LocalStorage struct {
	Root string
}

// NewLocal creates a filesystem storage rooted at dir.
func NewLocal(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &LocalStorage{Root: dir}, nil
}

func (l *LocalStorage) Name() string { return "local" }

// resolve maps a key to a filesystem path, refusing traversal.
func (l *LocalStorage) resolve(key string) (string, error) {
	clean := filepath.Clean(strings.ReplaceAll(key, "\\", "/"))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("files: invalid key %q", key)
	}
	return filepath.Join(l.Root, filepath.FromSlash(clean)), nil
}

func (l *LocalStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (l *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, *FileInfo, error) {
	p, err := l.resolve(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	etag := sha1.Sum([]byte(fmt.Sprintf("%s-%d-%d", key, st.Size(), st.ModTime().UnixNano())))
	return f, &FileInfo{
		Size:        st.Size(),
		ContentType: ContentTypeByName(key),
		ETag:        hex.EncodeToString(etag[:8]),
	}, nil
}

func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *LocalStorage) DeletePrefix(ctx context.Context, prefix string) error {
	p, err := l.resolve(prefix)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}

func (l *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	p, err := l.resolve(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
