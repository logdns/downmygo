package engine

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/downmygo/downmygo/internal/model"
)

func TestInitialPlan(t *testing.T) {
	segs := InitialPlan(10<<20, 4)
	if len(segs) != 4 {
		t.Fatalf("%d segments, want 4", len(segs))
	}
	if segs[0].Start != 0 || segs[len(segs)-1].End != 10<<20-1 {
		t.Fatalf("plan does not cover the file: %+v", segs)
	}
	for i := 1; i < len(segs); i++ {
		if segs[i].Start != segs[i-1].End+1 {
			t.Fatalf("gap between %+v and %+v", segs[i-1], segs[i])
		}
	}
	if got := InitialPlan(100, 8); len(got) != 1 {
		t.Fatalf("small file got %d segments", len(got))
	}
	if got := InitialPlan(0, 8); got != nil {
		t.Fatalf("empty file got %+v", got)
	}
}

func TestClaimSplitsTheLargest(t *testing.T) {
	segs := []model.Segment{{Start: 0, End: 4<<20 - 1}}
	active := []bool{false}
	idx := ClaimInto(&segs, &active, 2, MinSplit)
	if idx != 0 || !active[0] {
		t.Fatalf("first claim = %d, active %v", idx, active)
	}
	if len(segs) != 2 {
		t.Fatalf("no split: %+v", segs)
	}
	idx = ClaimInto(&segs, &active, 2, MinSplit)
	if idx != 1 {
		t.Fatalf("second claim = %d, want 1", idx)
	}
	// The remaining piece is small enough to hand over whole.
	if got := ClaimInto(&segs, &active, 2, MinSplit); got != 2 {
		t.Fatalf("third claim = %d, want 2", got)
	}
	// Every piece is busy and nothing is left.
	if got := ClaimInto(&segs, &active, 2, MinSplit); got != -2 {
		t.Fatalf("busy claim = %d, want -2", got)
	}
	for i := range segs {
		active[i] = false
		segs[i].Done = segs[i].Len()
	}
	if got := ClaimInto(&segs, &active, 2, MinSplit); got != -1 {
		t.Fatalf("done claim = %d, want -1", got)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		`..\..\etc\passwd`: ".._.._etc_passwd",
		"a/b:c*d?e":        "a_b_c_d_e",
		"CON.txt":          "_CON.txt",
		"  spaced.  ":      "spaced",
		"":                 "download",
		"na\x00me":         "name",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileNameFromHeaders(t *testing.T) {
	if got := FileNameFromHeaders(`attachment; filename="report.pdf"`, "http://x/a"); got != "report.pdf" {
		t.Errorf("plain name = %q", got)
	}
	if got := FileNameFromHeaders("attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf", "http://x/a"); got != "报告.pdf" {
		t.Errorf("encoded name = %q", got)
	}
	if got := FileNameFromHeaders("", "http://x/files/My%20File.zip?x=1"); got != "My File.zip" {
		t.Errorf("url name = %q", got)
	}
}

// testFile is deterministic content of a size.
func testFile(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i/7)
	}
	return b
}

func rangeServer(t *testing.T, data []byte, ranges bool, delay time.Duration) *httptest.Server {
	t.Helper()
	write := func(w http.ResponseWriter, b []byte) {
		for i := 0; i < len(b); i += 64 << 10 {
			end := min(i+64<<10, len(b))
			if _, err := w.Write(b[i:end]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if delay > 0 {
				time.Sleep(delay)
			}
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
		w.Header().Set("Last-Modified", time.Unix(1700000000, 0).UTC().Format(http.TimeFormat))
		if !ranges {
			w.Header().Set("Accept-Ranges", "none")
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			write(w, data)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			write(w, data)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start < 0 || start >= len(data) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= len(data) || end < start {
			end = len(data) - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		write(w, data[start:end+1])
	}))
}

func TestProbeAndDownload(t *testing.T) {
	data := testFile(3 << 20)
	srv := rangeServer(t, data, true, 0)
	defer srv.Close()

	c := NewClient(RequestOptions{})
	info, err := Probe(context.Background(), c, srv.URL, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(data)) || !info.Resumable {
		t.Fatalf("info = %+v", info)
	}
	if info.FileName != "test.bin" {
		t.Fatalf("name = %q", info.FileName)
	}

	dir := t.TempDir()
	m := NewManager()
	var mu sync.Mutex
	done := make(chan Event, 100)
	m.OnEvent = func(ev Event) {
		mu.Lock()
		done <- ev
		mu.Unlock()
	}
	d := model.Download{
		ID: "t1", URL: srv.URL, FileName: "test.bin", Dir: dir,
		Size: info.Size, Resumable: info.Resumable, ETag: info.ETag,
		Status: model.StatusQueued,
	}
	if err := m.Start(d, Config{Connections: 4, MaxRetries: 2}); err != nil {
		t.Fatal(err)
	}
	var last Event
	timeout := time.After(20 * time.Second)
	for {
		select {
		case ev := <-done:
			last = ev
			if ev.Status == model.StatusComplete {
				goto complete
			}
			if ev.Status == model.StatusError {
				t.Fatalf("download failed: %s", ev.Error)
			}
		case <-timeout:
			t.Fatalf("timed out, last event %+v", last)
		}
	}
complete:
	got, err := os.ReadFile(filepath.Join(dir, "test.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("file differs: %d bytes, want %d", len(got), len(data))
	}
	if _, err := os.Stat(filepath.Join(dir, "test.bin.dmpart")); !os.IsNotExist(err) {
		t.Fatalf("part file left behind: %v", err)
	}
}

func TestNoRangeServer(t *testing.T) {
	data := testFile(300 << 10)
	srv := rangeServer(t, data, false, 0)
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()
	done := make(chan Event, 100)
	m.OnEvent = func(ev Event) { done <- ev }
	d := model.Download{ID: "t2", URL: srv.URL, FileName: "plain.bin", Dir: dir, Status: model.StatusQueued}
	if err := m.Start(d, Config{Connections: 4}); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev := <-done:
			if ev.Status == model.StatusComplete {
				got, _ := os.ReadFile(filepath.Join(dir, "plain.bin"))
				if !bytes.Equal(got, data) {
					t.Fatalf("file differs")
				}
				return
			}
			if ev.Status == model.StatusError {
				t.Fatalf("failed: %s", ev.Error)
			}
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func TestPauseAndResume(t *testing.T) {
	data := testFile(4 << 20)
	// Slow enough that a pause lands mid-file.
	srv := rangeServer(t, data, true, 100*time.Millisecond)
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager()
	events := make(chan Event, 500)
	m.OnEvent = func(ev Event) {
		select {
		case events <- ev:
		default:
		}
	}
	d := model.Download{
		ID: "t3", URL: srv.URL, FileName: "slow.bin", Dir: dir,
		Size: int64(len(data)), Resumable: true, ETag: `"v1"`,
		Status: model.StatusQueued,
	}
	if err := m.Start(d, Config{Connections: 4}); err != nil {
		t.Fatal(err)
	}

	// Wait for real progress, then pause.
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Status == model.StatusDownloading && ev.Done > 200<<10 {
				m.Pause("t3")
				goto paused
			}
			if ev.Status == model.StatusComplete {
				t.Skip("download finished before it could be paused")
			}
		case <-deadline:
			t.Fatal("no progress")
		}
	}
paused:
	for {
		select {
		case ev := <-events:
			if ev.Status == model.StatusPaused {
				if ev.Done == 0 {
					t.Fatal("pause lost all progress")
				}
				goto resume
			}
			if ev.Status == model.StatusError {
				t.Fatalf("pause turned into an error: %s", ev.Error)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no paused event")
		}
	}
resume:
	// The job must be gone from the manager before a resume can start.
	for i := 0; i < 100 && m.Running("t3"); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if err := m.Start(d, Config{Connections: 4}); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(40 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Status == model.StatusComplete {
				got, _ := os.ReadFile(filepath.Join(dir, "slow.bin"))
				if !bytes.Equal(got, data) {
					t.Fatalf("file differs after resume: %d bytes", len(got))
				}
				return
			}
			if ev.Status == model.StatusError {
				t.Fatalf("resume failed: %s", ev.Error)
			}
		case <-timeout:
			t.Fatal("resume timed out")
		}
	}
}

func TestChecksumMismatch(t *testing.T) {
	data := testFile(64 << 10)
	srv := rangeServer(t, data, true, 0)
	defer srv.Close()
	dir := t.TempDir()
	m := NewManager()
	events := make(chan Event, 50)
	m.OnEvent = func(ev Event) { events <- ev }
	d := model.Download{
		ID: "t4", URL: srv.URL, FileName: "sum.bin", Dir: dir,
		Size: int64(len(data)), Resumable: true,
		Checksum: "sha256:" + strings.Repeat("0", 64),
	}
	_ = m.Start(d, Config{Connections: 2})
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Status == model.StatusError {
				if !strings.Contains(ev.Error, "checksum") {
					t.Fatalf("error = %q", ev.Error)
				}
				return
			}
			if ev.Status == model.StatusComplete {
				t.Fatal("a bad checksum was accepted")
			}
		case <-timeout:
			t.Fatal("timed out")
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0: "0 B", 512: "512 B", 1024: "1.0 KB", 1536: "1.5 KB",
		10 << 20: "10.0 MB", 5 << 30: "5.0 GB",
	}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := FormatETA(90 * time.Second); got != "1m 30s" {
		t.Errorf("FormatETA = %q", got)
	}
	if got := FormatETA(3700 * time.Second); got != "1h 01m" {
		t.Errorf("FormatETA = %q", got)
	}
}

func ExampleFormatBytes() {
	fmt.Println(FormatBytes(2 << 20))
	// Output: 2.0 MB
}
