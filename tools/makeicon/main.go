// Command makeicon writes the app's icon from the drawing the window and
// the tray use, so the picture never drifts from the code.
//
// It writes the Icon Composer document at resources/downmygo.icon: the
// document's fill is the brand's gradient, its layer is the mark drawn
// here as glyph.png. On macOS it then asks Icon Composer's own renderer,
// ictool, for resources/icon.png, the PNG mygo build turns into the .icns,
// the .ico and the desktop icons. Elsewhere it draws that PNG itself.
//
//	go run ./tools/makeicon
package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/downmygo/downmygo/internal/app"
)

const (
	docPath   = "resources/downmygo.icon"
	pngPath   = "resources/icon.png"
	ictoolApp = "/Applications/Icon Composer.app/Contents/Executables/ictool"
)

// document is the Icon Composer document: a fill and one layer.
type document struct {
	Fill   fill    `json:"fill"`
	Groups []group `json:"groups"`
	// The macOS icon grid: the artwork is a square with rounded corners.
	Platforms platforms `json:"supported-platforms"`
}

type fill struct {
	LinearGradient []string `json:"linear-gradient"`
}

type group struct {
	Layers []layerJSON `json:"layers"`
}

type layerJSON struct {
	Name      string   `json:"name"`
	ImageName string   `json:"image-name"`
	Fill      fillJSON `json:"fill"`
}

type fillJSON struct {
	Solid string `json:"solid"`
}

type platforms struct {
	Squares []string `json:"squares"`
}

func main() {
	// The glyph is the layer; the tile is the fill, drawn by Icon Composer
	// so the glass and the lighting are the system's.
	if err := os.MkdirAll(filepath.Join(docPath, "Assets"), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docPath, "Assets", "glyph.png"), app.GlyphPNG(1024), 0o644); err != nil {
		log.Fatal(err)
	}
	doc := document{
		Fill: fill{LinearGradient: []string{
			"extended-srgb:0.29020,0.47059,1.00000,1.00000",
			"extended-srgb:0.05882,0.65490,0.60392,1.00000",
		}},
		Groups: []group{{Layers: []layerJSON{{
			Name:      "Mark",
			ImageName: "glyph.png",
			Fill:      fillJSON{Solid: "extended-srgb:1.00000,1.00000,1.00000,1.00000"},
		}}}},
		Platforms: platforms{Squares: []string{"macOS"}},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docPath, "icon.json"), append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", docPath)

	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(ictoolApp); err == nil {
			cmd := exec.Command(ictoolApp, docPath,
				"--export-image", "--output-file", pngPath,
				"--platform", "macOS", "--rendition", "Default",
				"--width", "1024", "--height", "1024", "--scale", "1")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("ictool: %v", err)
			}
			// Icon Composer draws the artwork edge to edge; macOS icons keep
			// a margin around theirs, which the system's grid asks for.
			raw, err := os.ReadFile(pngPath)
			if err != nil {
				log.Fatal(err)
			}
			if err := os.WriteFile(pngPath, composeIcon(raw, 1024, 824), 0o644); err != nil {
				log.Fatal(err)
			}
			log.Printf("wrote %s from Icon Composer", pngPath)
			return
		}
		log.Printf("Icon Composer is not installed; drawing the PNG here")
	}
	if err := os.WriteFile(pngPath, app.IconPNG(1024), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", pngPath)
}

// composeIcon puts the artwork of src, scaled to artwork pixels, in the
// middle of a transparent canvas pixels square, as macOS's icon grid
// places it.
func composeIcon(src []byte, canvas, artwork int) []byte {
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		log.Fatal(err)
	}
	out := image.NewRGBA(image.Rect(0, 0, canvas, canvas))
	off := (canvas - artwork) / 2
	draw.Draw(out, image.Rect(off, off, off+artwork, off+artwork), scaleTo(img, artwork), image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// tap is one of the four pixels a bilinear sample weighs.
type tap struct {
	dx, dy int
	w      float64
}

// scaleTo resizes an image to a square of size, in premultiplied space so
// the edges stay clean.
func scaleTo(img image.Image, size int) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(size) - 0.5
		y0 := int(fy)
		ty := fy - float64(y0)
		if y0 < 0 {
			y0, ty = 0, 0
		}
		if y0 >= sh-1 {
			y0, ty = sh-1, 0
		}
		for x := 0; x < size; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(size) - 0.5
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
