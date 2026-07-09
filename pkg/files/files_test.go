package files

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	got := SanitizeFilename("My Report (final)!.PDF")
	if !strings.HasPrefix(got, "my_report_final") || !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("sanitized = %s", got)
	}
	// traversal attempts neutralized
	got = SanitizeFilename("../../etc/passwd")
	if strings.Contains(got, "/") || strings.Contains(got, "..") {
		t.Fatalf("traversal survived: %s", got)
	}
}

func TestLocalStorage(t *testing.T) {
	ctx := context.Background()
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	key := Key("posts", "rec1", "hello.txt")
	if err := st.Put(ctx, key, strings.NewReader("hello world"), 11, "text/plain"); err != nil {
		t.Fatal(err)
	}
	rc, info, err := st.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "hello world" || info.Size != 11 {
		t.Fatalf("roundtrip: %q %+v", data, info)
	}
	if ok, _ := st.Exists(ctx, key); !ok {
		t.Fatal("exists = false")
	}

	// key traversal refused
	if _, _, err := st.Get(ctx, "../secrets"); err == nil {
		t.Fatal("traversal allowed")
	}

	// prefix delete
	st.Put(ctx, Key("posts", "rec1", "b.txt"), strings.NewReader("x"), 1, "")
	if err := st.DeletePrefix(ctx, "posts/rec1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.Exists(ctx, key); ok {
		t.Fatal("prefix delete missed file")
	}
}

func TestThumbs(t *testing.T) {
	ctx := context.Background()
	st, _ := NewLocal(t.TempDir())

	// generate a 100x50 png
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	key := Key("posts", "r1", "pic.png")
	st.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), "image/png")

	rc, info, err := Thumb(ctx, st, "posts", "r1", "pic.png", "40x40")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	thumb, _, err := image.Decode(rc)
	if err != nil {
		t.Fatal(err)
	}
	if w := thumb.Bounds().Dx(); w != 40 {
		t.Fatalf("thumb width = %d, want 40", w)
	}
	if h := thumb.Bounds().Dy(); h != 20 {
		t.Fatalf("thumb height = %d, want 20 (aspect kept)", h)
	}
	if info.ContentType != "image/png" {
		t.Fatalf("content type = %s", info.ContentType)
	}

	// cached variant reused
	if ok, _ := st.Exists(ctx, ThumbKey("posts", "r1", "pic.png", "40x40")); !ok {
		t.Fatal("thumb not cached")
	}
	if !ValidThumbSize("100x100") || ValidThumbSize("evil") {
		t.Fatal("thumb size validation")
	}
}
