package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManagerDownloadsYtDlp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test checks the executable bit")
	}
	t.Setenv("PATH", t.TempDir())
	body := []byte("#!/bin/sh\necho 2026.01.01\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := &Manager{Dir: dir, Client: srv.Client()}
	// Point the installer at the test server.
	old := ytDlpBase
	ytDlpBase = srv.URL + "/"
	defer func() { ytDlpBase = old }()

	var progress int
	m.Progress = func(kind Kind, done, total int64) { progress++ }
	path, err := m.Ensure(context.Background(), YtDlp)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "yt-dlp") {
		t.Fatalf("path = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the program is not executable: %v", info.Mode())
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(body) {
		t.Fatalf("body = %q", got)
	}
	if progress == 0 {
		t.Fatal("no progress")
	}
	// A second Ensure finds it and downloads nothing.
	if again, err := m.Ensure(context.Background(), YtDlp); err != nil || again != path {
		t.Fatalf("%s %v", again, err)
	}
}

func TestManagerInstallsGallery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix launcher")
	}
	dir := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	python := filepath.Join(bin, "python3")
	err := os.WriteFile(python, []byte("#!/bin/sh\ncase \"$*\" in *'pip --isolated install'*'--target'*gallery-dl) exit 0;; '-m gallery_dl --version') echo 1.32.21;; *) exit 1;; esac\n"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	m := Manager{Dir: dir}
	p, err := m.Ensure(t.Context(), GalleryDL)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(p, "--version").Output(); err != nil || strings.TrimSpace(string(out)) != "1.32.21" {
		t.Fatalf("%q %v", out, err)
	}
	// An unsuccessful refresh leaves the installed launcher and its libraries alone.
	before, _ := os.ReadFile(p)
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(t.Context(), GalleryDL); err == nil {
		t.Fatal("expected install failure")
	}
	after, _ := os.ReadFile(p)
	libs, _ := filepath.Glob(filepath.Join(dir, "gallery-dl-lib-*"))
	if string(before) != string(after) || len(libs) != 1 {
		t.Fatal("old installation changed")
	}
}

func TestWindowsAssets(t *testing.T) {
	if ytDlpAsset("windows", "arm64") != "yt-dlp_arm64.exe" {
		t.Fatal("wrong architecture")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"info":{"version":"1.32.21"}}`) }))
	defer srv.Close()
	old := pypiURL
	pypiURL = srv.URL
	defer func() { pypiURL = old }()
	u, err := galleryWindowsURL(t.Context(), srv.Client())
	if err != nil || u != "https://codeberg.org/mikf/gallery-dl/releases/download/v1.32.21/gallery-dl.exe" {
		t.Fatalf("%s %v", u, err)
	}
}
