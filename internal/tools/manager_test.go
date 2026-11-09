package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// wheel makes a zip that looks like a wheel.
func wheel(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"gallery_dl/__init__.py":            "def main():\n    return 0\n",
		"gallery_dl-1.0.dist-info/METADATA": "Name: gallery-dl\n",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestManagerDownloadsYtDlp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test checks the executable bit")
	}
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

func TestManagerUnpacksGalleryDL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gallery-dl is a wheel with a launcher elsewhere")
	}
	if _, err := pythonPath(); err != nil {
		t.Skip("no python3 here")
	}
	body := wheel(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := &Manager{Dir: dir, Client: srv.Client()}
	oldPyPI, oldGH := pypiURL, galleryExeURL
	pypiURL = srv.URL + "/json"
	galleryExeURL = srv.URL + "/gallery-dl.exe"
	defer func() { pypiURL, galleryExeURL = oldPyPI, oldGH }()

	// The wheel URL comes from the JSON of PyPI; serve one that points at
	// the test server.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/json") {
			_, _ = w.Write([]byte(`{"urls":[{"packagetype":"bdist_wheel","url":"` + srv.URL + `/gallery.whl"}]}`))
			return
		}
		_, _ = w.Write(body)
	})

	path, err := m.Ensure(context.Background(), GalleryDL)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("path = %s", path)
	}
	launcher, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(launcher), "#!/bin/sh") || !strings.Contains(string(launcher), "run.py") {
		t.Fatalf("launcher = %q", launcher)
	}
	if _, err := os.Stat(filepath.Join(dir, "gallery-dl-lib", "gallery_dl", "__init__.py")); err != nil {
		t.Fatal(err)
	}
}

func TestUnzipRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.whl")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil.py")
	_, _ = w.Write([]byte("no"))
	_ = zw.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unzipInto(zipPath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("a wheel reached outside its folder")
	}
}

func TestPythonAndQuoting(t *testing.T) {
	if got := shQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shQuote = %q", got)
	}
	if got := pyQuote(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("pyQuote = %q", got)
	}
}
