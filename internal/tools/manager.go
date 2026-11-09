package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Manager keeps the external downloaders the app uses: it finds the ones
// on PATH, and downloads the ones that are not there into the app's own
// folder, so a video or a gallery needs nothing installed by hand.
type Manager struct {
	// Dir is where the app keeps the programs it downloaded.
	Dir string
	// Client fetches them; nil is a client of its own.
	Client *http.Client
	// Progress hears how a download goes, for the window.
	Progress func(kind Kind, done, total int64)
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (m *Manager) dir() string {
	if m.Dir != "" {
		return m.Dir
	}
	return "."
}

// Path is the program to run: the one on PATH, else the one the app
// downloaded, else "".
func (m *Manager) Path(kind Kind) (string, bool) {
	if p, err := Find(kind); err == nil {
		return p, true
	}
	p := m.managedPath(kind)
	if p == "" {
		return "", false
	}
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, true
	}
	return "", false
}

func (m *Manager) managedPath(kind Kind) string {
	switch kind {
	case YtDlp:
		return filepath.Join(m.dir(), "yt-dlp"+exeSuffix())
	case GalleryDL:
		if runtime.GOOS == "windows" {
			return filepath.Join(m.dir(), "gallery-dl.exe")
		}
		return filepath.Join(m.dir(), "gallery-dl")
	}
	return ""
}

// Ensure makes a program runnable, downloading it when it is neither on
// PATH nor in the app's folder.
func (m *Manager) Ensure(ctx context.Context, kind Kind) (string, error) {
	if p, ok := m.Path(kind); ok {
		return p, nil
	}
	if err := os.MkdirAll(m.dir(), 0o755); err != nil {
		return "", err
	}
	var err error
	switch kind {
	case YtDlp:
		err = m.installYtDlp(ctx)
	case GalleryDL:
		err = m.installGalleryDL(ctx)
	default:
		return "", fmt.Errorf("no program named %q", kind)
	}
	if err != nil {
		return "", err
	}
	if p, ok := m.Path(kind); ok {
		return p, nil
	}
	return "", fmt.Errorf("%s did not install", kind)
}

// Update downloads the newest again, replacing what is there.
func (m *Manager) Update(ctx context.Context, kind Kind) (string, error) {
	for _, p := range []string{m.managedPath(kind), filepath.Join(m.dir(), "gallery-dl-lib")} {
		if p != "" {
			_ = os.RemoveAll(p)
		}
	}
	return m.Ensure(ctx, kind)
}

// Version asks a program for its version.
func Version(ctx context.Context, program string) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, program, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// --- yt-dlp ---

func ytDlpAsset() string {
	switch runtime.GOOS {
	case "darwin":
		return "yt-dlp_macos"
	case "windows":
		return "yt-dlp.exe"
	default:
		if runtime.GOARCH == "arm64" {
			return "yt-dlp_linux_aarch64"
		}
		return "yt-dlp_linux"
	}
}

// ytDlpBase, pypiURL and galleryExeURL are where the programs come from;
// a test changes them.
var (
	ytDlpBase     = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	pypiURL       = "https://pypi.org/pypi/gallery-dl/json"
	galleryExeURL = "https://github.com/mikf/gallery-dl/releases/latest/download/gallery-dl.exe"
)

func (m *Manager) installYtDlp(ctx context.Context) error {
	url := ytDlpBase + ytDlpAsset()
	path := filepath.Join(m.dir(), "yt-dlp"+exeSuffix())
	return m.download(ctx, YtDlp, url, path, 0o755)
}

// --- gallery-dl ---

func (m *Manager) installGalleryDL(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		url := galleryExeURL
		return m.download(ctx, GalleryDL, url, filepath.Join(m.dir(), "gallery-dl.exe"), 0o755)
	}
	// Elsewhere gallery-dl is a Python program: the wheel from PyPI,
	// unpacked into the app's folder, runs with the system's python3.
	python, err := pythonPath()
	if err != nil {
		return err
	}
	wheel, err := galleryWheelURL(ctx, m.client())
	if err != nil {
		return err
	}
	// The unpacked wheel goes beside the launcher, not where it is: the
	// launcher is the program, and the folder holds its modules.
	dir := filepath.Join(m.dir(), "gallery-dl-lib")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(m.dir(), "gallery-dl.whl")
	if err := m.download(ctx, GalleryDL, wheel, zipPath, 0o644); err != nil {
		return err
	}
	if err := unzipInto(zipPath, dir); err != nil {
		return err
	}
	_ = os.Remove(zipPath)
	// The launcher runs the module with the unpacked wheel on the path.
	runner := filepath.Join(dir, "run.py")
	body := "import sys\nsys.path.insert(0, " + pyQuote(dir) + ")\nfrom gallery_dl import main\nsys.exit(main())\n"
	if err := os.WriteFile(runner, []byte(body), 0o644); err != nil {
		return err
	}
	launcher := filepath.Join(m.dir(), "gallery-dl")
	script := "#!/bin/sh\nexec " + python + " " + shQuote(runner) + " \"$@\"\n"
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		return err
	}
	return nil
}

// galleryWheelURL asks PyPI for the newest wheel of gallery-dl.
func galleryWheelURL(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pypiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "downmygo")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PyPI answered %s", resp.Status)
	}
	var body struct {
		URLs []struct {
			PackageType string `json:"packagetype"`
			URL         string `json:"url"`
		} `json:"urls"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return "", err
	}
	for _, u := range body.URLs {
		if u.PackageType == "bdist_wheel" && strings.HasSuffix(u.URL, ".whl") {
			return u.URL, nil
		}
	}
	return "", errors.New("PyPI has no wheel of gallery-dl")
}

func pythonPath() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("gallery-dl needs python3, which is not installed")
}

// --- fetching ---

func (m *Manager) download(ctx context.Context, kind Kind, url, path string, perm os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "downmygo")
	resp, err := m.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	tmp := path + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(tmp)
				return werr
			}
			done += int64(n)
			if m.Progress != nil {
				m.Progress(kind, done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// unzipInto unpacks a wheel, which is a zip, into dir.
func unzipInto(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("the wheel holds a path outside its folder: %s", f.Name)
		}
		target := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func pyQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
