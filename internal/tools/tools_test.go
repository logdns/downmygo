package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseProgress(t *testing.T) {
	p, ok := parseProgress("downmygo-progress 5242880 10485760 0 1048576.5 5")
	if !ok {
		t.Fatal("not a progress line")
	}
	if p.Done != 5242880 || p.Size != 10485760 || p.Speed != 1048576.5 {
		t.Fatalf("p = %+v", p)
	}
	if p.Percent < 0.49 || p.Percent > 0.51 {
		t.Fatalf("percent = %v", p.Percent)
	}
	if p.ETA != 5*time.Second {
		t.Fatalf("eta = %v", p.ETA)
	}

	// The estimate stands in when the total is unknown.
	p, ok = parseProgress("downmygo-progress 100 0 400 0 NA")
	if !ok || p.Size != 400 || p.Percent != 0.25 {
		t.Fatalf("p = %+v", p)
	}

	p, ok = parseProgress("[download]  42.3% of 12.4MiB at 1.2MiB/s")
	if !ok || p.Percent < 0.42 || p.Percent > 0.43 {
		t.Fatalf("p = %+v", p)
	}

	if _, ok := parseProgress("nothing to see"); ok {
		t.Fatal("a plain line was progress")
	}
}

func TestRunWithAFakeProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake program is a shell script")
	}
	dir := t.TempDir()
	bin := t.TempDir()
	script := filepath.Join(bin, "yt-dlp")
	body := `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "downmygo-progress 10 100 0 1000 1"
echo "downmygo-progress 50 100 0 1000 1"
echo "downmygo-file:$out"
: > "$out"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var seen []float64
	files, err := Run(context.Background(), YtDlp, "https://example.com/v", Options{
		Dir: dir, FileName: "v.mp4",
	}, func(p Progress) { seen = append(seen, p.Percent) })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != 0.1 || seen[1] != 0.5 {
		t.Fatalf("progress = %v", seen)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "v.mp4") {
		t.Fatalf("files = %v", files)
	}
}

func TestFindSaysWhenItIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(GalleryDL); err == nil {
		t.Fatal("gallery-dl was found on an empty PATH")
	}
	if Available(GalleryDL) {
		t.Fatal("Available said yes")
	}
}

func TestBuildArgs(t *testing.T) {
	args := buildArgs(YtDlp, "https://x/v", Options{Dir: "/tmp/d", Proxy: "socks5://p:1", Cookies: "/c.txt"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--newline", "--proxy socks5://p:1", "--cookies /c.txt", "-- https://x/v", "-o /tmp/d/%(title)s.%(ext)s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args miss %q: %s", want, joined)
		}
	}
	args = buildArgs(GalleryDL, "https://x/g", Options{Dir: "/tmp/d"})
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--dest /tmp/d") || !strings.Contains(joined, "after:downmygo-file:") {
		t.Errorf("gallery args = %s", joined)
	}
}
