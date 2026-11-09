package extract

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
)

// kindOf says how a file is packed, from its name.
func kindOf(name string) (string, bool) {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".zip"):
		return "zip", true
	case strings.HasSuffix(n, ".7z"):
		return "7z", true
	case strings.HasSuffix(n, ".rar"):
		return "rar", true
	case strings.HasSuffix(n, ".tar.gz"), strings.HasSuffix(n, ".tgz"):
		return "targz", true
	case strings.HasSuffix(n, ".tar.bz2"), strings.HasSuffix(n, ".tbz2"):
		return "tarbz2", true
	case strings.HasSuffix(n, ".tar.xz"), strings.HasSuffix(n, ".txz"):
		return "tarxz", true
	case strings.HasSuffix(n, ".tar"):
		return "tar", true
	}
	return "", false
}

// Extract unpacks archive into opts.Dir.
func Extract(archive string, opts Options) (Result, error) {
	kind, ok := kindOf(archive)
	if !ok {
		return Result{}, fmt.Errorf("%s is not an archive this app opens", filepath.Base(archive))
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(archive), strings.TrimSuffix(filepath.Base(archive), filepath.Ext(archive)))
		dir = strings.TrimSuffix(dir, ".tar")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	res := Result{Dir: dir}
	var err error
	switch kind {
	case "zip":
		err = extractZip(archive, dir, opts, &res)
	case "7z":
		err = extractSevenZip(archive, dir, opts, &res)
	case "rar":
		err = extractRar(archive, dir, opts, &res)
	default:
		err = extractTar(archive, kind, dir, opts, &res)
	}
	return res, err
}

func extractSevenZip(archive, dir string, opts Options, res *Result) error {
	zr, err := sevenzip.OpenReader(archive)
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

func extractRar(archive, dir string, opts Options, res *Result) error {
	rr, err := rardecode.OpenReader(archive)
	if err != nil {
		return err
	}
	defer rr.Close()
	for {
		h, err := rr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safePath(dir, h.Name)
		if err != nil {
			return err
		}
		if h.IsDir {
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
		n, err := writeFile(target, rr)
		if err != nil {
			return err
		}
		res.Files++
		res.Bytes += n
	}
}

func extractTar(archive, kind, dir string, opts Options, res *Result) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	switch kind {
	case "targz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	case "tarbz2":
		r = bzip2.NewReader(f)
	case "tarxz":
		xr, err := xz.NewReader(f)
		if err != nil {
			return err
		}
		r = xr
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safePath(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if !opts.Overwrite {
				if _, err := os.Stat(target); err == nil {
					continue
				}
			}
			n, err := writeFile(target, tr)
			if err != nil {
				return err
			}
			res.Files++
			res.Bytes += n
		case tar.TypeSymlink, tar.TypeLink:
			// Links are how an archive escapes its folder; leave them out.
			continue
		}
	}
}
