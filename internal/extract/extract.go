// Package extract unpacks an archive a download finished with: zip, 7z,
// rar, tar, tar.gz, tar.bz2 and tar.xz, into a folder beside it. It
// refuses entries that would leave that folder, so an archive cannot
// write where it likes.
package extract

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Supported reports whether the file's name is an archive this package
// can open.
func Supported(name string) bool {
	_, ok := kindOf(name)
	return ok
}

// Options say where to unpack and what to leave out.
type Options struct {
	// Dir is the folder to write into; empty means a folder beside the
	// archive, named after it without the extension.
	Dir string
	// Overwrite replaces files that are already there.
	Overwrite bool
}

// Result is what an extraction wrote.
type Result struct {
	Dir   string
	Files int
	Bytes int64
}

// safePath joins dir and name, refusing anything that would leave dir.
func safePath(dir, name string) (string, error) {
	name = filepath.FromSlash(name)
	if name == "" {
		return "", errors.New("an entry has no name")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("the archive holds an absolute path: %s", name)
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive reaches outside its folder: %s", name)
	}
	full := filepath.Join(dir, clean)
	// Belt and braces: the joined path must still be under dir.
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive reaches outside its folder: %s", name)
	}
	return full, nil
}

func extractZip(archive, dir string, opts Options, res *Result) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target, err := safePath(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if !opts.Overwrite {
			if _, err := os.Stat(target); err == nil {
				continue
			}
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		n, err := writeFile(target, rc)
		rc.Close()
		if err != nil {
			return err
		}
		res.Files++
		res.Bytes += n
	}
	return nil
}

func writeFile(target string, r io.Reader) (int64, error) {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}
