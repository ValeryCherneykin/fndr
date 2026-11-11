package preview

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"

	// Registered with image.Decode: the formats the standard library reads.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/ValeryCherneykin/fndr/internal/opener"
)

// decodable are the fileTypes names LoadImage can decode.
var decodable = map[string]bool{"PNG image": true, "JPEG image": true, "GIF image": true}

// maxPixels is the largest picture LoadImage decodes: 64 megapixels, above
// any phone camera (48). Decoding holds every pixel in memory, and the size
// comes from the file's header, which costs nothing to inflate: a 428 KB
// PNG can say 20000×20000 and take 1.9 GB and 3 s to decode, and a little
// more would end fndr for lack of memory. Larger pictures get a card.
const maxPixels = 64 << 20

// ErrTooLarge is returned for pictures over maxPixels.
var ErrTooLarge = errors.New("picture too large to preview")

// LoadImage decodes the picture at path and shrinks it so neither side is
// longer than maxSide pixels. A phone photo is 4000 pixels wide and a
// preview pane a few hundred: sending the terminal all of it would cost
// megabytes per row the cursor rests on, for pixels nobody can see.
func LoadImage(path string, maxSide int) (image.Image, error) {
	img, err := decode(path)
	if err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	return shrink(img, maxSide), nil
}

// decode reads the picture's size from its header first, a few bytes, and
// decodes the pixels only if they fit in maxPixels.
func decode(path string) (image.Image, error) {
	f, err := opener.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: %d×%d", ErrTooLarge, cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	return img, err
}

// shrink scales img down by a whole factor so it fits in maxSide, averaging
// each factor×factor block of pixels into one (a box filter). Averaging, not
// picking one pixel per block, is what keeps fine detail from turning into
// noise. A whole factor keeps the code to one loop; the result may be up to
// half as small again as it could be, which a preview pane does not show.
func shrink(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	k := max((max(b.Dx(), b.Dy())+maxSide-1)/maxSide, 1)
	if k == 1 {
		return img
	}

	// Converted to RGBA k rows at a time, into one reused strip: draw.Draw
	// has fast paths for the decoders' own types (YCbCr for JPEG, paletted
	// for GIF), and the loop below then reads bytes, not one interface call
	// per pixel. A strip, not the whole picture, because a whole RGBA copy
	// is 4 bytes a pixel: 192 MB more for a 48-megapixel photo.
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), k))

	w, h := b.Dx()/k, b.Dy()/k
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(k * k) //nolint:gosec // k is at most the image side over maxSide: tiny
	for y := range h {
		draw.Draw(src, src.Bounds(), img, image.Pt(b.Min.X, b.Min.Y+y*k), draw.Src)
		for x := range w {
			var sum [4]uint32
			for dy := range k {
				row := src.Pix[dy*src.Stride+x*k*4:]
				for dx := range k {
					for c := range 4 {
						sum[c] += uint32(row[dx*4+c])
					}
				}
			}
			o := dst.PixOffset(x, y)
			for c := range 4 {
				dst.Pix[o+c] = uint8(sum[c] / n) //nolint:gosec // an average of bytes is a byte
			}
		}
	}
	return dst
}
