package preview

import (
	"fmt"
	"image"
	"image/draw"
	"os"

	// Registered with image.Decode: the formats the standard library reads.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// decodable are the fileTypes names LoadImage can decode.
var decodable = map[string]bool{"PNG image": true, "JPEG image": true, "GIF image": true}

// LoadImage decodes the picture at path and shrinks it so neither side is
// longer than maxSide pixels. A phone photo is 4000 pixels wide and a
// preview pane a few hundred: sending the terminal all of it would cost
// megabytes per row the cursor rests on, for pixels nobody can see.
func LoadImage(path string, maxSide int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only: Close has nothing to report

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("preview: %w", err)
	}
	return shrink(img, maxSide), nil
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

	// Into RGBA first: draw.Draw has fast paths for the decoders' own
	// types (YCbCr for JPEG, paletted for GIF), and the loop below then
	// reads bytes, not one interface call per pixel.
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)

	w, h := b.Dx()/k, b.Dy()/k
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(k * k) //nolint:gosec // k is at most the image side over maxSide: tiny
	for y := range h {
		for x := range w {
			var sum [4]uint32
			for dy := range k {
				row := src.Pix[(y*k+dy)*src.Stride+x*k*4:]
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
