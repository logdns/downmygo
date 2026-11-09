package engine

import (
	"mime"
	"net/url"
	"path"
	"strings"
	"unicode"
)

// FileNameFromHeaders works out the file's name from Content-Disposition,
// falling back to the last part of the URL's path.
func FileNameFromHeaders(disposition, rawURL string) string {
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if v := params["filename"]; v != "" {
				return SanitizeName(v)
			}
		}
		// Some servers send an unquoted name; take what follows the token.
		if i := strings.Index(strings.ToLower(disposition), "filename="); i >= 0 {
			v := strings.TrimSpace(disposition[i+len("filename="):])
			v = strings.Trim(v, `"'`)
			if j := strings.IndexByte(v, ';'); j >= 0 {
				v = v[:j]
			}
			if v != "" {
				return SanitizeName(v)
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
			if decoded, err := url.PathUnescape(base); err == nil {
				base = decoded
			}
			return SanitizeName(base)
		}
	}
	return "download"
}

// SanitizeName makes a name safe on Windows, macOS and Linux at once:
// no separators or control characters, no reserved device names, no
// trailing dot or space, and a length the filesystems accept.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' ||
			r == '"' || r == '<' || r == '>' || r == '|':
			return '_'
		case r < 0x20 || r == 0x7f:
			return -1
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	// Windows device names, with or without an extension.
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if len(name) > 180 {
		ext := path.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		keep := 180 - len(ext)
		if keep < 1 {
			keep = 1
		}
		name = truncateUTF8(name, keep) + ext
	}
	return name
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// UniqueName returns name, or "name (2).ext" and so on, so that it does
// not exist in dir. exists reports whether a name is taken.
func UniqueName(name string, exists func(string) bool) string {
	if !exists(name) {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; i < 10000; i++ {
		candidate := base + " (" + itoa(i) + ")" + ext
		if !exists(candidate) {
			return candidate
		}
	}
	return base + " (" + itoa(len(name)) + ")" + ext
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
