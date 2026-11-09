// Package tools runs the downloaders that know a site better than an HTTP
// client does: yt-dlp for videos and gallery-dl for galleries. They are
// external programs, found on PATH, and their progress is read from their
// own output.
package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind is which program runs.
type Kind string

const (
	YtDlp     Kind = "yt-dlp"
	GalleryDL Kind = "gallery-dl"
)

// Options say where the files go and how the program reaches them.
type Options struct {
	Dir       string
	FileName  string
	Proxy     string
	Cookies   string // a cookies.txt file
	UserAgent string
	Extra     []string
	// Program is the program to run; empty finds one.
	Program string
}

// Progress is what a program says while it works.
type Progress struct {
	Percent float64 // -1 when unknown
	Done    int64
	Size    int64
	Speed   float64
	ETA     time.Duration
	Line    string
}

// Find looks for a program on PATH and beside the app, which is where a
// packaged build may ship it.
func Find(kind Kind) (string, error) {
	name := string(kind)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		for _, dir := range []string{base, filepath.Join(base, "bin"), filepath.Join(base, "..", "Resources", "bin")} {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed: put it on PATH, or in the app's folder", kind)
}

// Available says whether a program can run.
func Available(kind Kind) bool {
	_, err := Find(kind)
	return err == nil
}

// Run downloads url with kind, calling onProgress as it goes, and returns
// the files it wrote.
func Run(ctx context.Context, kind Kind, url string, opts Options, onProgress func(Progress)) ([]string, error) {
	path := opts.Program
	if path == "" {
		found, err := Find(kind)
		if err != nil {
			return nil, err
		}
		path = found
	}
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, err
	}
	before := snapshot(opts.Dir)

	args := buildArgs(kind, url, opts)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = opts.Dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var files []string
	var lastErr string
	read := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if p, ok := parseProgress(line); ok {
				if onProgress != nil {
					onProgress(p)
				}
				continue
			}
			if f, ok := strings.CutPrefix(line, "downmygo-file:"); ok {
				mu.Lock()
				files = append(files, strings.TrimSpace(f))
				mu.Unlock()
				continue
			}
			if strings.Contains(line, "ERROR") || strings.Contains(line, "error:") {
				mu.Lock()
				lastErr = strings.TrimSpace(line)
				mu.Unlock()
			}
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); read(stdout) }()
	go func() { defer wg.Done(); read(stderr) }()
	wg.Wait()
	err = cmd.Wait()

	mu.Lock()
	reported := append([]string(nil), files...)
	msg := lastErr
	mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			return reported, ctx.Err()
		}
		if msg != "" {
			return reported, errors.New(msg)
		}
		return reported, fmt.Errorf("%s stopped: %w", kind, err)
	}
	if len(reported) > 0 {
		return reported, nil
	}
	// A program that does not say what it wrote: diff the folder.
	return diff(before, snapshot(opts.Dir)), nil
}

// buildArgs is the command line, which asks the program to print the
// files it wrote on lines of its own.
func buildArgs(kind Kind, url string, opts Options) []string {
	var args []string
	switch kind {
	case GalleryDL:
		args = append(args, "--dest", opts.Dir, "--print", "after:downmygo-file:{filename}")
	default:
		args = append(args,
			"--newline", "--no-colors", "--no-playlist",
			"--progress", "--progress-template",
			"download:downmygo-progress %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s",
			"--print", "after_move:downmygo-file:%(filepath)s",
		)
		if opts.FileName != "" {
			args = append(args, "-o", filepath.Join(opts.Dir, opts.FileName))
		} else {
			args = append(args, "-o", filepath.Join(opts.Dir, "%(title)s.%(ext)s"))
		}
	}
	if opts.Proxy != "" && opts.Proxy != "system" {
		args = append(args, "--proxy", opts.Proxy)
	}
	if opts.Cookies != "" {
		args = append(args, "--cookies", opts.Cookies)
	}
	if opts.UserAgent != "" {
		args = append(args, "--user-agent", opts.UserAgent)
	}
	args = append(args, opts.Extra...)
	args = append(args, "--", url)
	return args
}

// parseProgress reads a line of either program's output.
func parseProgress(line string) (Progress, bool) {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "downmygo-progress "); ok {
		fields := strings.Fields(rest)
		p := Progress{Percent: -1, Line: line}
		if len(fields) > 0 {
			p.Done = atoi(fields[0])
		}
		if len(fields) > 1 {
			p.Size = atoi(fields[1])
		}
		if p.Size == 0 && len(fields) > 2 {
			p.Size = atoi(fields[2])
		}
		if len(fields) > 3 {
			p.Speed = atof(fields[3])
		}
		if len(fields) > 4 {
			if s := atoi(fields[4]); s > 0 {
				p.ETA = time.Duration(s) * time.Second
			}
		}
		if p.Size > 0 {
			p.Percent = float64(p.Done) / float64(p.Size)
			if p.Percent > 1 {
				p.Percent = 1
			}
		}
		return p, true
	}
	// gallery-dl and yt-dlp's plain lines: "[download]  42.3% of 12.4MiB at 1.2MiB/s"
	if strings.Contains(line, "[download]") && strings.Contains(line, "%") {
		p := Progress{Percent: -1, Line: line}
		if i := strings.Index(line, "%"); i > 0 {
			start := strings.LastIndexAny(line[:i], " ") + 1
			if v, err := strconv.ParseFloat(strings.TrimSpace(line[start:i]), 64); err == nil {
				p.Percent = v / 100
			}
		}
		return p, true
	}
	return Progress{}, false
}

func atoi(s string) int64 {
	if s == "" || s == "NA" || s == "None" {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func atof(s string) float64 {
	if s == "" || s == "NA" || s == "None" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// snapshot lists the files in dir with their sizes.
func snapshot(dir string) map[string]int64 {
	out := map[string]int64{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out[path] = info.Size()
		}
		return nil
	})
	return out
}

// diff lists the files that appeared or grew.
func diff(before, after map[string]int64) []string {
	var out []string
	for path, size := range after {
		if old, ok := before[path]; !ok || size > old {
			out = append(out, path)
		}
	}
	return out
}
