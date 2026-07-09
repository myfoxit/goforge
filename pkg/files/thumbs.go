package files

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // decoder registration
	"image/jpeg"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decode-only webp support
)

var thumbSizeRe = regexp.MustCompile(`^(\d{1,4})x(\d{1,4})$`)

// ValidThumbSize reports whether size is an accepted "WxH" spec.
func ValidThumbSize(size string) bool { return thumbSizeRe.MatchString(size) }

// IsImage reports whether the filename looks like a resizable image.
func IsImage(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(pathExt(name), ".")) {
	case "jpg", "jpeg", "png", "gif", "webp":
		return true
	}
	return false
}

func pathExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// Thumb returns a reader for the resized variant of an image, generating and
// caching it in storage on first use. size is "WxH" (fit inside, keep ratio;
// a 0 dimension means "auto").
func Thumb(ctx context.Context, storage Storage, collection, recordID, filename, size string) (io.ReadCloser, *FileInfo, error) {
	if !ValidThumbSize(size) {
		return nil, nil, fmt.Errorf("files: invalid thumb size %q", size)
	}
	key := ThumbKey(collection, recordID, filename, size)
	if rc, info, err := storage.Get(ctx, key); err == nil {
		return rc, info, nil
	}

	src, _, err := storage.Get(ctx, Key(collection, recordID, filename))
	if err != nil {
		return nil, nil, err
	}
	defer src.Close()

	img, format, err := image.Decode(io.LimitReader(src, 64<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("files: decode image: %w", err)
	}

	m := thumbSizeRe.FindStringSubmatch(size)
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	resized := resizeFit(img, w, h)

	var buf bytes.Buffer
	contentType := "image/jpeg"
	switch format {
	case "png", "gif", "webp":
		contentType = "image/png"
		err = png.Encode(&buf, resized)
	default:
		err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		return nil, nil, err
	}

	if err := storage.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), contentType); err != nil {
		return nil, nil, err
	}
	rc, info, err := storage.Get(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	info.ContentType = contentType
	return rc, info, nil
}

// resizeFit scales img to fit within w×h preserving aspect ratio.
func resizeFit(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return img
	}
	if w == 0 && h == 0 {
		return img
	}
	if w == 0 {
		w = srcW * h / srcH
	}
	if h == 0 {
		h = srcH * w / srcW
	}
	scale := min(float64(w)/float64(srcW), float64(h)/float64(srcH))
	if scale >= 1 { // never upscale
		return img
	}
	dstW := max(1, int(float64(srcW)*scale))
	dstH := max(1, int(float64(srcH)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}
