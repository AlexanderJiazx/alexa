package browser

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type box struct {
	n          int
	x, y, w, h float64
}

var palette = []color.RGBA{
	{230, 57, 70, 255}, {29, 120, 220, 255}, {20, 150, 90, 255}, {220, 120, 0, 255},
	{140, 60, 200, 255}, {0, 150, 160, 255}, {200, 40, 140, 255}, {90, 90, 90, 255},
}

func annotate(jpg []byte, boxes []box) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	face := basicfont.Face7x13
	for _, bx := range boxes {
		c := palette[bx.n%len(palette)]
		x0, y0 := int(bx.x), int(bx.y)
		x1, y1 := int(bx.x+bx.w), int(bx.y+bx.h)
		rect(img, x0, y0, x1, y1, c)
		label := strconv.Itoa(bx.n)
		lw := font.MeasureString(face, label).Ceil() + 4
		lx, ly := x0, y0-14
		if ly < 0 {
			ly = y0
		}
		if lx+lw > img.Bounds().Dx() {
			lx = img.Bounds().Dx() - lw
		}
		draw.Draw(img, image.Rect(lx, ly, lx+lw, ly+14), &image.Uniform{c}, image.Point{}, draw.Src)
		d := &font.Drawer{Dst: img, Src: image.White, Face: face, Dot: fixed.P(lx+2, ly+11)}
		d.DrawString(label)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 72}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func rect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	b := img.Bounds()
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	x0, x1 = clamp(x0, 0, b.Dx()-1), clamp(x1, 0, b.Dx()-1)
	y0, y1 = clamp(y0, 0, b.Dy()-1), clamp(y1, 0, b.Dy()-1)
	for t := 0; t < 2; t++ {
		for x := x0; x <= x1; x++ {
			img.SetRGBA(x, clamp(y0+t, 0, b.Dy()-1), c)
			img.SetRGBA(x, clamp(y1-t, 0, b.Dy()-1), c)
		}
		for y := y0; y <= y1; y++ {
			img.SetRGBA(clamp(x0+t, 0, b.Dx()-1), y, c)
			img.SetRGBA(clamp(x1-t, 0, b.Dx()-1), y, c)
		}
	}
}
