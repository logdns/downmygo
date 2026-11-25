package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
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

	// mu keeps two downloads from installing the same program at once.
	mu sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	// Another download may have installed it while this one waited.
	if p, ok := m.Path(kind); ok {
		return p, nil
	}
	return m.install(ctx, kind)
}

func (m *Manager) install(ctx context.Context, kind Kind) (string, error) {
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.managedPath(kind) == "" {
		return "", fmt.Errorf("unknown tool %q", kind)
	}
	if p, err := Find(kind); err == nil {
		return p, nil
	} // externally managed
	return m.install(ctx, kind)
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

func ytDlpAsset(goos, arch string) string {
	switch goos {
	case "darwin":
		return "yt-dlp_macos"
	case "windows":
		if arch == "arm64" {
			return "yt-dlp_arm64.exe"
		}
		return "yt-dlp.exe"
	default:
		if arch == "arm64" {
			return "yt-dlp_linux_aarch64"
		}
		return "yt-dlp_linux"
	}
}

// ytDlpBase, pypiURL and galleryBase are where the programs come from;
// a test changes them.
var (
	ytDlpBase   = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	pypiURL     = "https://pypi.org/pypi/gallery-dl/json"
	galleryBase = "https://codeberg.org/mikf/gallery-dl/releases/download/"
)

func (m *Manager) installYtDlp(ctx context.Context) error {
	url := ytDlpBase + ytDlpAsset(runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(m.dir(), "yt-dlp"+exeSuffix())
	return m.download(ctx, YtDlp, url, path, 0o755)
}

// --- gallery-dl ---

func (m *Manager) installGalleryDL(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		url, err := galleryWindowsURL(ctx, m.client())
		if err != nil {
			return err
		}
		return m.download(ctx, GalleryDL, url, m.managedPath(GalleryDL), 0755)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return errors.New("gallery-dl requires python3 with pip")
	}
	// Install into a new directory: a failed upgrade must leave the working launcher and its dependencies intact.
	lib, err := os.MkdirTemp(m.dir(), "gallery-dl-lib-")
	if err != nil {
		return err
	}
	lib, err = filepath.Abs(lib)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(lib)
		}
	}()
	cmd := exec.CommandContext(ctx, python, "-m", "pip", "--isolated", "install", "--disable-pip-version-check", "--no-input", "--only-binary=:all:", "--index-url", "https://pypi.org/simple", "--target", lib, "gallery-dl")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install gallery-dl with pip: %w: %s", err, strings.TrimSpace(string(out)))
	}
	launcher := m.managedPath(GalleryDL)
	tmp := launcher + ".part"
	defer os.Remove(tmp)
	script := "#!/bin/sh\nexport PYTHONPATH=" + shQuote(lib) + "\nexec " + shQuote(python) + " -m gallery_dl \"$@\"\n"
	if err := os.WriteFile(tmp, []byte(script), 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp, launcher); err != nil {
		return err
	}
	ok = true
	return nil
}

var stableVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){2,3}$`)

func galleryWindowsURL(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pypiURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("PyPI answered %s", resp.Status)
	}
	var meta struct{ Info struct{ Version string } }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&meta); err != nil {
		return "", err
	}
	if !stableVersion.MatchString(meta.Info.Version) {
		return "", errors.New("invalid gallery-dl release version")
	}
	return galleryBase + "v" + meta.Info.Version + "/gallery-dl.exe", nil
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

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
