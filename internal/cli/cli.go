// Package cli runs downmygo without a window: `downmygo get URL` downloads
// a file in a terminal, with progress, and exits with a useful code.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/downmygo/downmygo/internal/engine"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/internal/version"
)

// Main runs the command line and returns the process's exit code.
func Main(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "get", "download":
		return get(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "--version":
		fmt.Println("downmygo " + version.Version)
		return 0
	case "info":
		return runInfo()
	case "mcp":
		return runMCP()
	case "native-host", "native-messaging":
		return runNativeHost()
	case "install-host":
		return runInstallHost(args[1:])
	case "grab":
		return runGrab(args[1:])
	case "import":
		return runImport(args[1:])
	default:
		// A bare URL is the common case.
		if strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://") {
			return get(args)
		}
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `downmygo — downloads, on the go.

  downmygo get URL [flags]     download a file
  downmygo URL                 the same
  downmygo import FILE         add every link of a file
  downmygo grab PAGE           print the links of a page
  downmygo info                the version, the API port and the counts
  downmygo mcp                 serve the Model Context Protocol on stdio
  downmygo native-host         serve the browser's native messaging on stdio
  downmygo install-host [ID]   write the browser's host manifest

Flags:
  -o, --out DIR       folder to save in (default: your Downloads/downmygo)
  -n, --name NAME     file name
  -c, --connections N connections (default 8)
  -q, --quiet         no progress
      --checksum SUM  sha256:<hex> to verify
      --referer URL   Referer header
      --cookie VALUE  Cookie header
`)
}

func get(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("o", "", "folder to save in")
	name := fs.String("n", "", "file name")
	conns := fs.Int("c", 8, "connections")
	quiet := fs.Bool("q", false, "no progress")
	checksum := fs.String("checksum", "", "sha256:<hex>")
	referer := fs.String("referer", "", "Referer header")
	cookie := fs.String("cookie", "", "Cookie header")
	fs.StringVar(out, "out", "", "folder to save in")
	fs.StringVar(name, "name", "", "file name")
	// The URL may come before or after the flags; flag stops at the first
	// non-flag argument, so take it out first.
	var raw string
	var rest []string
	for _, arg := range args {
		if raw == "" && (strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")) {
			raw = arg
			continue
		}
		rest = append(rest, arg)
	}
	if err := fs.Parse(rest); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if raw == "" {
		if fs.NArg() > 0 {
			raw = fs.Arg(0)
		} else {
			fmt.Fprintln(os.Stderr, "downmygo: no URL")
			return 2
		}
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		fmt.Fprintf(os.Stderr, "downmygo: %q is not an http(s) link\n", raw)
		return 2
	}

	dir := *out
	if dir == "" {
		dir = filepath.Join(store.DefaultDownloadDir())
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()
	client := engine.NewClient(engine.RequestOptions{UserAgent: "downmygo/1.0"})
	info, err := engine.Probe(ctx, client, raw, engine.RequestOptions{UserAgent: "downmygo/1.0"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}
	fileName := *name
	if fileName == "" {
		fileName = info.FileName
	}
	fileName = engine.SanitizeName(fileName)
	if _, err := os.Stat(filepath.Join(dir, fileName)); err == nil {
		fileName = engine.UniqueName(fileName, func(n string) bool {
			_, err := os.Stat(filepath.Join(dir, n))
			return err == nil
		})
	}

	m := engine.NewManager()
	done := make(chan engine.Event, 64)
	m.OnEvent = func(ev engine.Event) {
		select {
		case done <- ev:
		default:
		}
	}
	d := model.Download{
		ID: model.NewID(), URL: raw, FileName: fileName, Dir: dir,
		Size: info.Size, Resumable: info.Resumable, ETag: info.ETag,
		LastModified: info.LastModified, FinalURL: info.FinalURL,
		Checksum: *checksum, Referer: *referer, Cookies: *cookie,
		Status: model.StatusQueued,
	}
	if err := m.Start(d, engine.Config{Connections: *conns, MaxRetries: 5}); err != nil {
		fmt.Fprintf(os.Stderr, "downmygo: %v\n", err)
		return 1
	}

	lastLine := 0
	for ev := range done {
		switch ev.Status {
		case model.StatusDownloading, model.StatusConnecting:
			if !*quiet {
				line := fmt.Sprintf("\r%s  %s  %s", engine.FormatBytes(ev.Done),
					engine.FormatSpeed(ev.Speed), percent(ev))
				pad := ""
				if n := lastLine - len(line); n > 0 {
					pad = strings.Repeat(" ", n)
				}
				fmt.Fprint(os.Stderr, line+pad)
				lastLine = len(line)
			}
		case model.StatusComplete:
			if !*quiet {
				fmt.Fprintln(os.Stderr)
			}
			fmt.Println(filepath.Join(dir, fileName))
			return 0
		case model.StatusError:
			if !*quiet {
				fmt.Fprintln(os.Stderr)
			}
			fmt.Fprintf(os.Stderr, "downmygo: %s\n", ev.Error)
			return 1
		}
	}
	return 1
}

func percent(ev engine.Event) string {
	if ev.Size <= 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", int(float64(ev.Done)/float64(ev.Size)*100))
}
