package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "pack.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"a.txt", "sub/b.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("hello " + name))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Extract(archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Dir != filepath.Join(dir, "pack") {
		t.Fatalf("res = %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(res.Dir, "sub", "b.txt"))
	if err != nil || string(b) != "hello sub/b.txt" {
		t.Fatalf("%q, %v", b, err)
	}
}

func TestZipSlipIsRefused(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../escaped.txt")
	_, _ = w.Write([]byte("no"))
	_ = zw.Close()
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(archive, Options{}); err == nil {
		t.Fatal("an archive reached outside its folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escaped.txt")); err == nil {
		t.Fatal("the file was written outside the folder")
	}
}

func TestTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "notes.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("note")
	_ = tw.WriteHeader(&tar.Header{Name: "notes/one.md", Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Extract(archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 {
		t.Fatalf("files = %d", res.Files)
	}
	if _, err := os.Lstat(filepath.Join(res.Dir, "link")); err == nil {
		t.Fatal("a symlink was written")
	}
	if b, err := os.ReadFile(filepath.Join(res.Dir, "notes", "one.md")); err != nil || string(b) != "note" {
		t.Fatalf("%q, %v", b, err)
	}
}

func TestOverwriteAndSupported(t *testing.T) {
	if !Supported("a.zip") || !Supported("A.TAR.GZ") || Supported("a.rar") || Supported("a.txt") {
		t.Fatal("Supported is wrong")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "x.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("x.txt")
	_, _ = w.Write([]byte("new"))
	_ = zw.Close()
	_ = os.WriteFile(archive, buf.Bytes(), 0o644)
	out := filepath.Join(dir, "out")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(out, "x.txt"), []byte("old"), 0o644)

	if _, err := Extract(archive, Options{Dir: out}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "x.txt")); string(b) != "old" {
		t.Fatalf("overwrote without asking: %q", b)
	}
	if _, err := Extract(archive, Options{Dir: out, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "x.txt")); string(b) != "new" {
		t.Fatalf("did not overwrite: %q", b)
	}
}
