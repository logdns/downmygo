// Command makestore draws what the extension stores want: the icons each
// extension ships, and the tiles and screenshots a listing shows.
//
//	go run ./tools/makestore
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/downmygo/downmygo/internal/app"
)

// The brand's gradient, as the icon has it.
var (
	from = color.RGBA{0x4A, 0x78, 0xFF, 0xFF}
	to   = color.RGBA{0x0F, 0xA7, 0x9A, 0xFF}
)

func main() {
	// The icons each extension ships.
	for _, dir := range []string{"extensions/chrome/icons", "extensions/firefox/icons", "extensions/userscript/icons"} {
		for _, size := range []int{16, 32, 48, 128} {
			write(filepath.Join(dir, itoa(size)+".png"), app.IconPNG(size))
		}
	}

	// The listings.
	icon128 := app.IconPNG(128)
	write("extensions/chrome/store/icon-128.png", icon128)
	write("extensions/firefox/store/icon-128.png", icon128)
	write("extensions/userscript/icon-128.png", icon128)

	write("extensions/chrome/store/small-tile-440x280.png", tile(440, 280, 120))
	write("extensions/chrome/store/marquee-1400x560.png", tile(1400, 560, 260))
	write("extensions/firefox/store/tile-440x280.png", tile(440, 280, 120))

	if shot, err := os.ReadFile("docs/screens/main-light.png"); err == nil {
		write("extensions/chrome/store/screenshot-1280x800.png", screenshot(shot, 1280, 800))
		write("extensions/firefox/store/screenshot-1280x800.png", screenshot(shot, 1280, 800))
	}
	if shot, err := os.ReadFile("docs/screens/main-light-zh-Hans.png"); err == nil {
		write("extensions/chrome/store/screenshot-zh-1280x800.png", screenshot(shot, 1280, 800))
		write("extensions/firefox/store/screenshot-zh-1280x800.png", screenshot(shot, 1280, 800))
	}
	log.Println("wrote the store assets")
}

func write(path string, body []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Fatal(err)
	}
}

// tile is a promotional image: the brand's gradient with the mark in the
// middle, and a soft light behind it.
func tile(w, h, mark int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)/float64(w) + float64(y)/float64(h)) / 2
			img.SetRGBA(x, y, mix(from, to, t))
		}
	}
	// A glow behind the mark.
	cx, cy := float64(w)/2, float64(h)/2
	radius := float64(mark) * 0.95
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := (float64(x)-cx)*(float64(x)-cx) + (float64(y)-cy)*(float64(y)-cy)
			if d > radius*radius {
				continue
			}
			f := 1 - d/(radius*radius)
			img.SetRGBA(x, y, mix(img.RGBAAt(x, y), color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}, 0.16*f))
		}
	}
	icon := app.IconPNG(mark)
	mark1 := decode(icon)
	off := image.Pt((w-mark)/2, (h-mark)/2)
	draw.Draw(img, image.Rect(off.X, off.Y, off.X+mark, off.Y+mark), mark1, image.Point{}, draw.Over)
	return encode(img)
}

// screenshot fits a window shot into the size a store asks for, on the
// app's own background.
func screenshot(src []byte, w, h int) []byte {
	img := decode(src)
	b := img.Bounds()
	scale := float64(w) / float64(b.Dx())
	if s := float64(h) / float64(b.Dy()); s < scale {
		scale = s
	}
	nw := int(float64(b.Dx()) * scale)
	nh := int(float64(b.Dy()) * scale)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{0xF6, 0xF7, 0xFB, 0xFF}
	draw.Draw(out, out.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	scaled := scaleTo(img, nw, nh)
	off := image.Pt((w-nw)/2, (h-nh)/2)
	draw.Draw(out, image.Rect(off.X, off.Y, off.X+nw, off.Y+nh), scaled, image.Point{}, draw.Over)
	return encode(out)
}

func decode(body []byte) image.Image {
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	return img
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

// scaleTo resizes an image with bilinear sampling, in premultiplied space.
func scaleTo(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(h) - 0.5
		y0 := int(fy)
		ty := fy - float64(y0)
		if y0 < 0 {
			y0, ty = 0, 0
		}
		if y0 >= sh-1 {
			y0, ty = sh-1, 0
		}
		for x := 0; x < w; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(w) - 0.5
			x0 := int(fx)
			tx := fx - float64(x0)
			if x0 < 0 {
				x0, tx = 0, 0
			}
			if x0 >= sw-1 {
				x0, tx = sw-1, 0
			}
			var r, g, bl, a float64
			for _, s := range [4]tap{
				{0, 0, (1 - tx) * (1 - ty)},
				{1, 0, tx * (1 - ty)},
				{0, 1, (1 - tx) * ty},
				{1, 1, tx * ty},
			} {
				cr, cg, cb, ca := img.At(b.Min.X+x0+s.dx, b.Min.Y+y0+s.dy).RGBA()
				r += float64(cr>>8) * s.w * float64(ca>>8)
				g += float64(cg>>8) * s.w * float64(ca>>8)
				bl += float64(cb>>8) * s.w * float64(ca>>8)
				a += float64(ca>>8) * s.w
			}
			if a == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(bl / a), A: uint8(a)})
		}
	}
	return out
}

type tap struct {
	dx, dy int
	w      float64
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
