package preview

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	writePNG(t, path, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	p, err := Build(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != Image || p.Type != "PNG image" || p.Size == 0 {
		t.Errorf("Build = %+v, want an image that still has its card facts", p)
	}
}

func TestLoadImageShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.png")
	writePNG(t, path, image.NewRGBA(image.Rect(0, 0, 1000, 400)))

	img, err := LoadImage(path, 300)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() > 300 || b.Dy() > 300 || b.Dx() < 150 {
		t.Errorf("shrunk to %v, want at most 300 on a side and not needlessly tiny", b)
	}
}

func TestShrinkAverages(t *testing.T) {
	// A black and white checkerboard averages to grey, not to noise.
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if (x+y)%2 == 0 {
				src.Set(x, y, color.White)
			} else {
				src.Set(x, y, color.Black)
			}
		}
	}
	got, ok := shrink(src, 2).(*image.RGBA)
	if !ok {
		t.Fatal("shrink did not return RGBA")
	}
	if r, g, b, _ := got.At(0, 0).RGBA(); r>>8 != 127 || g>>8 != 127 || b>>8 != 127 {
		t.Errorf("pixel = %d %d %d, want grey 127", r>>8, g>>8, b>>8)
	}
}

func TestShrinkLeavesSmallImages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if shrink(src, 100) != image.Image(src) {
		t.Error("a small image was copied for nothing")
	}
}

func TestLoadImageRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.png")
	if err := os.WriteFile(path, []byte("\x89PNG not really"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImage(path, 100); err == nil {
		t.Error("garbage decoded as an image")
	}
}

// TestLoadImageRefusesBombs: a PNG whose header claims 30000×30000 is
// refused from the header alone, before a byte of pixels is decoded. The
// file is a real 1×1 PNG with its size rewritten, as an attacker would.
func TestLoadImageRefusesBombs(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// After the 8-byte signature: chunk length (4), "IHDR" (4), then
	// width and height; the chunk's CRC follows its 13 bytes of data.
	binary.BigEndian.PutUint32(b[16:], 30000)
	binary.BigEndian.PutUint32(b[20:], 30000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	path := filepath.Join(t.TempDir(), "bomb.png")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadImage(path, 800); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err %v, want ErrTooLarge", err)
	}
}

// BenchmarkLoadImage previews a 12-megapixel photo, the size of an iPhone
// shot, and reports its memory: decoding dominates, the shrink should add
// little on top.
func BenchmarkLoadImage(b *testing.B) {
	path := filepath.Join(b.TempDir(), "photo.png")
	img := image.NewRGBA(image.Rect(0, 0, 4032, 3024))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := LoadImage(path, 800); err != nil {
			b.Fatal(err)
		}
	}
}
