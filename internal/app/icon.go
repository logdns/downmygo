package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The mark is a squircle in the brand's gradient with a download arrow
// that lands on a progress bar: the arrow says what the app does, the bar
// says how far it is. The same geometry is drawn here for the tray and
// the window, written as the glyph of the Icon Composer document by
// tools/makeicon, and drawn as SVG by internal/ui for the window's mark.
//
// Every shape is a signed distance field, so the edges are smooth at any
// size and the corners are round by construction.

// brandFrom and brandTo are the gradient's ends, top left to bottom right.
var (
	brandFrom = color.RGBA{0x4A, 0x78, 0xFF, 0xFF}
	brandTo   = color.RGBA{0x0F, 0xA7, 0x9A, 0xFF}
)

// IconPNG draws the app's icon: the tile and the glyph, with the padding
// macOS icons keep around their artwork.
func IconPNG(size int) []byte { return render(size, tileLayer, glyphLayer) }

// GlyphPNG draws the white mark alone, on nothing: the layer an Icon
// Composer document stacks on its fill.
func GlyphPNG(size int) []byte { return render(size, glyphLayer) }

// TrayPNG draws the mark as a template image: black where the menu bar
// tints it, nothing elsewhere.
func TrayPNG(size int) []byte { return render(size, templateLayer) }

// --- geometry ---

// glyphShapes are the arrow and the bar, in a unit square. The arrow's
// shaft and head are one polygon, so its corners round together.
var (
	shaftRect = rect{x0: 0.4375, y0: 0.205, x1: 0.5625, y1: 0.545, r: 0.0625}
	headTri   = poly{pts: [][2]float64{{0.5, 0.700}, {0.313, 0.512}, {0.687, 0.512}}, r: 0.036}
	barRect   = rect{x0: 0.293, y0: 0.762, x1: 0.707, y1: 0.838, r: 0.038}
	barFillX  = 0.293 + (0.707-0.293)*0.62
)

type rect struct{ x0, y0, x1, y1, r float64 }
type poly struct {
	pts [][2]float64
	r   float64
}

// sd returns the signed distance from a point to the shape, negative
// inside it.
func (s rect) sd(x, y float64) float64 {
	cx, cy := (s.x0+s.x1)/2, (s.y0+s.y1)/2
	hx, hy := (s.x1-s.x0)/2-s.r, (s.y1-s.y0)/2-s.r
	dx := math.Abs(x-cx) - hx
	dy := math.Abs(y-cy) - hy
	out := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
	in := math.Min(math.Max(dx, dy), 0)
	return out + in - s.r
}

// sd of a convex polygon, rounded by r: the farthest edge's plane, moved
// in by the radius.
func (p poly) sd(x, y float64) float64 {
	d := math.Inf(-1)
	for i := range p.pts {
		a := p.pts[i]
		b := p.pts[(i+1)%len(p.pts)]
		ex, ey := b[0]-a[0], b[1]-a[1]
		nx, ny := ey, -ex // outward normal, for points in clockwise order
		l := math.Hypot(nx, ny)
		if l == 0 {
			continue
		}
		nx, ny = nx/l, ny/l
		if v := nx*(x-a[0]) + ny*(y-a[1]); v > d {
			d = v
		}
	}
	return d - p.r
}

// squircle is the tile's outline: a superellipse, as macOS draws its
// icons, inset from the canvas as Apple's icon grid asks.
func squircle(x, y float64) float64 {
	const inset, n = 0.0977, 4.6
	cx, cy := 0.5, 0.5
	a := 0.5 - inset
	dx, dy := math.Abs(x-cx)/a, math.Abs(y-cy)/a
	// The implicit curve |x|^n + |y|^n = 1, as a distance good enough for
	// antialiasing.
	v := math.Pow(dx, n) + math.Pow(dy, n)
	return (math.Pow(v, 1/n) - 1) * a
}

// glyphCoverage is how much of a point the mark covers: the arrow, the
// bar's track and its filled part.
func glyphCoverage(x, y float64) float64 {
	arrow := math.Min(shaftRect.sd(x, y), headTri.sd(x, y))
	bar := barRect.sd(x, y)
	return math.Min(arrow, bar)
}

// --- rendering ---

type layer func(x, y float64) (color.RGBA, float64)

func render(size int, layers ...layer) []byte {
	if size < 8 {
		size = 8
	}
	const ss = 4
	w := size * ss
	img := image.NewRGBA(image.Rect(0, 0, w, w))
	px := 1 / float64(w)

	for y := 0; y < w; y++ {
		for x := 0; x < w; x++ {
			fx := (float64(x) + 0.5) * px
			fy := (float64(y) + 0.5) * px
			var r, g, b, a float64
			for _, l := range layers {
				c, cov := l(fx, fy)
				if cov <= 0 {
					continue
				}
				ca := float64(c.A) / 255 * cov
				// Straight-alpha "over".
				na := ca + a*(1-ca)
				if na == 0 {
					continue
				}
				r = (float64(c.R)*ca + r*a*(1-ca)) / na
				g = (float64(c.G)*ca + g*a*(1-ca)) / na
				b = (float64(c.B)*ca + b*a*(1-ca)) / na
				a = na
			}
			if a > 0 {
				img.SetRGBA(x, y, color.RGBA{
					R: uint8(clamp01(r/255) * 255),
					G: uint8(clamp01(g/255) * 255),
					B: uint8(clamp01(b/255) * 255),
					A: uint8(clamp01(a) * 255),
				})
			}
		}
	}
	return downsample(img, ss)
}

func tileLayer(x, y float64) (color.RGBA, float64) {
	cov := coverage(squircle(x, y), 1/1024.0)
	if cov <= 0 {
		return color.RGBA{}, 0
	}
	// The gradient runs corner to corner, then a light from the top left
	// and a shade at the bottom right give the tile its depth.
	t := clamp01((x + y) / 2)
	c := mix(brandFrom, brandTo, t)
	light := clamp01(1 - (x*0.75+y*0.55)/0.95)
	c = mix(c, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, 0.18*light*light)
	shade := clamp01((x*0.6 + y*0.8 - 0.55) / 0.6)
	c = mix(c, color.RGBA{0x06, 0x1B, 0x33, 0xFF}, 0.16*shade*shade)
	return c, cov
}

func glyphLayer(x, y float64) (color.RGBA, float64) {
	// A soft shadow under the mark lifts it off the tile.
	shadow := coverage(glyphCoverage(x-0.006, y-0.012), 1/1024.0)
	sd := glyphCoverage(x, y)
	mark := coverage(sd, 1/1024.0)
	if mark <= 0 {
		if shadow <= 0 {
			return color.RGBA{}, 0
		}
		return color.RGBA{0x05, 0x1A, 0x2E, 0xFF}, shadow * 0.22
	}
	// The bar's track is the same white, lighter; its filled part is
	// solid, so progress reads at a glance.
	if x > barFillX && y > barRect.y0 {
		return color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, mark * 0.42
	}
	return color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, mark
}

func templateLayer(x, y float64) (color.RGBA, float64) {
	return color.RGBA{0, 0, 0, 0xFF}, coverage(glyphCoverage(x, y), 1/1024.0)
}

// coverage turns a distance into the share of a pixel the shape covers.
func coverage(sd, px float64) float64 {
	return clamp01(0.5 - sd/px)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	t = clamp01(t)
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

// downsample averages the supersampled pixels into the wanted size, in
// premultiplied space so the edges stay clean.
func downsample(img *image.RGBA, ss int) []byte {
	b := img.Bounds()
	size := b.Dx() / ss
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, bl, a uint32
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					c := img.RGBAAt(x*ss+dx, y*ss+dy)
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					bl += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
				}
			}
			if a == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r / a), G: uint8(g / a), B: uint8(bl / a),
				A: uint8(a / uint32(ss*ss)),
			})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, out)
	return buf.Bytes()
}
