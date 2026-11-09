package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Brand is the accent a fresh install uses.
const Brand = "#3B6EF6"

// Theme returns downmygo's theme for a light or dark window, with the
// user's accent. It starts from MyGo's own theme so every control keeps
// its behavior, then gives it the brand's roomier metrics and quieter
// surfaces.
func Theme(dark bool, accent string) *ui.Theme {
	var t *ui.Theme
	if dark {
		t = ui.DarkTheme()
	} else {
		t = ui.LightTheme()
	}
	a := ui.Hex(Brand)
	if strings.HasPrefix(accent, "#") && len(accent) >= 7 {
		if parsed, err := parseHex(accent); err == nil {
			a = parsed
		}
	}

	if dark {
		t.Background = ui.Hex("#0E1116")
		t.Surface = ui.Hex("#171C24")
		t.SurfaceHover = ui.Hex("#1E2530")
		t.SurfacePressed = ui.Hex("#252D3A")
		t.Border = ui.Hex("#262E3A")
		t.Text = ui.Hex("#EDF1F7")
		t.TextMuted = ui.Hex("#98A2B3")
		t.Accent = a
		t.AccentHover = a.Mix(ui.Hex("#FFFFFF"), 0.12)
		t.AccentPressed = a.Mix(ui.Hex("#000000"), 0.12)
		t.AccentText = ui.Hex("#FFFFFF")
		t.Selection = a.Alpha(0.30)
		t.Focus = a.Alpha(0.65)
		t.Scrollbar = ui.RGBA(255, 255, 255, 0.22)
		t.Danger = ui.Hex("#F97066")
		t.Warning = ui.Hex("#FDB022")
		t.Success = ui.Hex("#32D583")
		t.Inverse = ui.Hex("#EDF1F7")
		t.InverseText = ui.Hex("#0E1116")
	} else {
		t.Background = ui.Hex("#F6F7FB")
		t.Surface = ui.Hex("#FFFFFF")
		t.SurfaceHover = ui.Hex("#F0F2F7")
		t.SurfacePressed = ui.Hex("#E7EAF1")
		t.Border = ui.Hex("#E3E7EF")
		t.Text = ui.Hex("#10131A")
		t.TextMuted = ui.Hex("#667085")
		t.Accent = a
		t.AccentHover = a.Mix(ui.Hex("#000000"), 0.08)
		t.AccentPressed = a.Mix(ui.Hex("#000000"), 0.16)
		t.AccentText = ui.Hex("#FFFFFF")
		t.Selection = a.Alpha(0.18)
		t.Focus = a.Alpha(0.55)
		t.Scrollbar = ui.RGBA(15, 23, 42, 0.20)
		t.Danger = ui.Hex("#D92D20")
		t.Warning = ui.Hex("#B54708")
		t.Success = ui.Hex("#067647")
		t.Inverse = ui.Hex("#10131A")
		t.InverseText = ui.Hex("#FFFFFF")
	}
	t.Radius = 10
	t.Spacing = 4
	t.FocusRingWidth = 2
	t.FocusRingOffset = 2
	t.ScrollbarWidth = 8
	return t
}

// Apply paints a theme file over a theme: the colors it names, then its
// metrics. A color that does not parse is left as it was.
func Apply(t *ui.Theme, name string, base string, colors map[string]string, radius, spacing float32) {
	if base == "light" || base == "dark" {
		// The caller already made the base; a file that names another one
		// is honoured by the caller.
		_ = base
	}
	set := func(dst *ui.Color, key string) {
		if v, ok := colors[key]; ok {
			if c, err := parseHex(v); err == nil {
				*dst = c
			}
		}
	}
	set(&t.Background, "background")
	set(&t.Surface, "surface")
	set(&t.SurfaceHover, "surfaceHover")
	set(&t.SurfacePressed, "surfacePressed")
	set(&t.Border, "border")
	set(&t.Text, "text")
	set(&t.TextMuted, "textMuted")
	set(&t.Accent, "accent")
	set(&t.AccentHover, "accentHover")
	set(&t.AccentPressed, "accentPressed")
	set(&t.AccentText, "accentText")
	set(&t.Selection, "selection")
	set(&t.Focus, "focus")
	set(&t.Scrollbar, "scrollbar")
	set(&t.Danger, "danger")
	set(&t.Warning, "warning")
	set(&t.Success, "success")
	set(&t.Inverse, "inverse")
	set(&t.InverseText, "inverseText")
	if radius > 0 {
		t.Radius = radius
	}
	if spacing > 0 {
		t.Spacing = spacing
	}
}

// parseHex reads #rrggbb or #rrggbbaa.
func parseHex(s string) (ui.Color, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return ui.Color{}, errBadColor
	}
	var v uint32
	for _, r := range s {
		v <<= 4
		switch {
		case r >= '0' && r <= '9':
			v |= uint32(r - '0')
		case r >= 'a' && r <= 'f':
			v |= uint32(r-'a') + 10
		case r >= 'A' && r <= 'F':
			v |= uint32(r-'A') + 10
		default:
			return ui.Color{}, errBadColor
		}
	}
	if len(s) == 6 {
		return ui.RGBA(uint8(v>>16), uint8(v>>8), uint8(v), 1), nil
	}
	return ui.Color{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, nil
}

type colorError struct{}

func (colorError) Error() string { return "bad color" }

var errBadColor = colorError{}
