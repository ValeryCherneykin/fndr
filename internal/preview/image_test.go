package preview

import (
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
