// Package themes reads theme files: JSON that names a few colors over one
// of the built-in themes, so a theme is a short file and not a fork of
// the whole palette.
package themes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Theme is a theme file.
type Theme struct {
	Name string `json:"name"`
	// Base is "light" or "dark"; empty keeps the app's own.
	Base string `json:"base,omitempty"`
	// Colors are the roles a theme may set, as #rrggbb or #rrggbbaa.
	Colors map[string]string `json:"colors"`
	// Radius and Spacing override the metrics, in points.
	Radius  float32 `json:"radius,omitempty"`
	Spacing float32 `json:"spacing,omitempty"`
	// File is where the theme was read from.
	File string `json:"-"`
}

// Roles are the colors a theme may set.
var Roles = []string{
	"background", "surface", "surfaceHover", "surfacePressed", "border",
	"text", "textMuted", "accent", "accentHover", "accentPressed",
	"accentText", "selection", "focus", "scrollbar", "danger", "warning",
	"success", "inverse", "inverseText",
}

// Load reads every *.json in dir, sorted by name. A broken file is
// returned as an error but does not stop the others.
func Load(dir string) ([]Theme, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var out []Theme
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t, err := Read(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errs
}

// Read reads one theme file.
func Read(path string) (Theme, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	if err := json.Unmarshal(body, &t); err != nil {
		return Theme{}, err
	}
	if strings.TrimSpace(t.Name) == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if t.Base != "" && t.Base != "light" && t.Base != "dark" {
		return Theme{}, fmt.Errorf("base is %q, not light or dark", t.Base)
	}
	for role, value := range t.Colors {
		if !knownRole(role) {
			return Theme{}, fmt.Errorf("unknown color %q", role)
		}
		if !isHex(value) {
			return Theme{}, fmt.Errorf("%s is %q, not #rrggbb or #rrggbbaa", role, value)
		}
	}
	return t, nil
}

// Write saves a theme, for the "save the current look as a theme" action.
func Write(dir string, t Theme) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := slug(t.Name)
	if name == "" {
		name = "theme"
	}
	path := filepath.Join(dir, name+".json")
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func knownRole(role string) bool {
	for _, r := range Roles {
		if r == role {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	if len(s) != 7 && len(s) != 9 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
