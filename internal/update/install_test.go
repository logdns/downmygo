package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDownloadChecksTheDigest(t *testing.T) {
	body := []byte("the new version, in a few bytes")
	sum := sha256.Sum256(body)
	good := "sha256:" + hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	var seen int64
	path, err := Download(context.Background(), srv.Client(), Asset{Name: "a.dmg", URL: srv.URL, Digest: good}, dir, func(done, total int64) {
		seen = done
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != int64(len(body)) {
		t.Fatalf("progress = %d", seen)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("%q %v", got, err)
	}

	// A digest that does not match leaves nothing behind.
	if _, err := Download(context.Background(), srv.Client(), Asset{Name: "b.dmg", URL: srv.URL, Digest: "sha256:00"}, dir, nil); err == nil {
		t.Fatal("a wrong digest was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.dmg")); err == nil {
		t.Fatal("the file was kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.dmg.part")); err == nil {
		t.Fatal("the part file was kept")
	}
}

func TestWaitForAndQuote(t *testing.T) {
	if !strings.Contains(waitFor(42), "kill -0 42") {
		t.Errorf("waitFor = %q", waitFor(42))
	}
	if waitFor(0) == "" {
		t.Error("waitFor(0) is empty")
	}
	if got := sh("a'b"); got != `'a'\''b'` {
		t.Errorf("sh = %q", got)
	}
}

func TestInstallMacReplacesTheBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the disk image and the swap are macOS's")
	}
	if _, err := exec.LookPath("hdiutil"); err != nil {
		t.Skip("no hdiutil")
	}
	work := t.TempDir()

	// The app that is running: a bundle with an old marker.
	bundle := filepath.Join(work, "downmygo.app")
	mustMkdir(t, filepath.Join(bundle, "Contents", "MacOS"))
	mustWrite(t, filepath.Join(bundle, "Contents", "MacOS", "downmygo"), "old")
	mustWrite(t, filepath.Join(bundle, "version"), "old")

	// The app in the disk image: the same bundle with a new marker.
	src := filepath.Join(work, "src", "downmygo.app")
	mustMkdir(t, filepath.Join(src, "Contents", "MacOS"))
	mustWrite(t, filepath.Join(src, "Contents", "MacOS", "downmygo"), "new")
	mustWrite(t, filepath.Join(src, "version"), "new")

	dmg := filepath.Join(work, "update.dmg")
	if out, err := exec.Command("hdiutil", "create", "-quiet", "-volname", "downmygo", "-srcfolder", src, "-ov", "-format", "UDZO", dmg).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}

	inst := Installer{AssetPath: dmg, Asset: Asset{Name: "update.dmg"}, Bundle: bundle, PID: 0, TempDir: filepath.Join(work, "helper")}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(bundle, "version")); err == nil && string(b) == "new" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	b, _ := os.ReadFile(filepath.Join(bundle, "version"))
	t.Fatalf("the bundle was not replaced: %q", b)
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
