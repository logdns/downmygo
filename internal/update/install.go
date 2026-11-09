package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Download fetches an asset into dir, reporting how far it is, and checks
// its SHA-256 when the release gives one.
func Download(ctx context.Context, client *http.Client, asset Asset, dir string, onProgress func(done, total int64)) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
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
		return "", fmt.Errorf("the download answered %s", resp.Status)
	}
	path := filepath.Join(dir, filepath.Base(asset.Name))
	out, err := os.OpenFile(path+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(path + ".part")
				return "", werr
			}
			_, _ = hash.Write(buf[:n])
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(path + ".part")
			return "", rerr
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if asset.Digest != "" {
		want := strings.TrimPrefix(asset.Digest, "sha256:")
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(want, got) {
			os.Remove(path + ".part")
			return "", fmt.Errorf("the download is not the file the release signed: %s, not %s", got, want)
		}
	}
	if err := os.Rename(path+".part", path); err != nil {
		return "", err
	}
	return path, nil
}

// Installer replaces the running app with a downloaded release, after the
// app has quit, and starts it again.
type Installer struct {
	// AssetPath is the file Download wrote.
	AssetPath string
	// Asset is the release file it came from.
	Asset Asset
	// Bundle is the .app on macOS, or the executable elsewhere.
	Bundle string
	// PID is the process the helper waits for.
	PID int
	// TempDir is where the helper unpacks; empty uses the system's.
	TempDir string
}

// Run starts the helper and returns; the caller quits, and the helper
// finishes the swap and reopens the app.
func (i Installer) Run() error {
	switch runtime.GOOS {
	case "darwin":
		return i.runMac()
	case "windows":
		return i.runWindows()
	default:
		return i.runLinux()
	}
}

func (i Installer) temp() (string, error) {
	dir := i.TempDir
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "downmygo-update")
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// waitFor is the shell loop that waits for the app to quit.
func waitFor(pid int) string {
	if pid <= 0 {
		return "sleep 2\n"
	}
	return fmt.Sprintf("while kill -0 %d 2>/dev/null; do sleep 1; done\n", pid)
}

func (i Installer) runMac() error {
	if !strings.HasSuffix(strings.ToLower(i.AssetPath), ".dmg") {
		return fmt.Errorf("the macOS update is not a disk image: %s", filepath.Base(i.AssetPath))
	}
	if !strings.HasSuffix(i.Bundle, ".app") {
		return errors.New("the update needs the app bundle's path")
	}
	dir, err := i.temp()
	if err != nil {
		return err
	}
	mount := filepath.Join(dir, "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return err
	}
	if out, err := exec.Command("hdiutil", "attach", i.AssetPath, "-nobrowse", "-readonly", "-mountpoint", mount).CombinedOutput(); err != nil {
		return fmt.Errorf("could not open the disk image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", mount, "-quiet").Run()

	src := filepath.Join(mount, filepath.Base(i.Bundle))
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("the disk image holds no %s", filepath.Base(i.Bundle))
	}
	newApp := filepath.Join(dir, filepath.Base(i.Bundle))
	if out, err := exec.Command("cp", "-R", src, newApp).CombinedOutput(); err != nil {
		return fmt.Errorf("could not copy the new app: %v: %s", err, strings.TrimSpace(string(out)))
	}

	script := filepath.Join(dir, "install.sh")
	body := "#!/bin/sh\nset -e\n" + waitFor(i.PID) +
		"rm -rf " + sh(i.Bundle) + "\n" +
		"cp -R " + sh(newApp) + " " + sh(i.Bundle) + "\n" +
		"open " + sh(i.Bundle) + "\n" +
		"rm -rf " + sh(dir) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	return startDetached(script)
}

func (i Installer) runLinux() error {
	dir, err := i.temp()
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "install.sh")
	// The Linux release is a tar.gz with the app and an install.sh; take
	// the app out of it and put it where the old one was.
	body := "#!/bin/sh\nset -e\n" + waitFor(i.PID) +
		"mkdir -p " + sh(dir) + "/unpack\n" +
		"tar -xzf " + sh(i.AssetPath) + " -C " + sh(dir) + "/unpack\n" +
		"new=$(find " + sh(dir) + "/unpack -type f -name downmygo | head -n 1)\n" +
		"cp \"$new\" " + sh(i.Bundle) + "\n" +
		"chmod +x " + sh(i.Bundle) + "\n" +
		"nohup " + sh(i.Bundle) + " >/dev/null 2>&1 &\n" +
		"rm -rf " + sh(dir) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	return startDetached(script)
}

func (i Installer) runWindows() error {
	dir, err := i.temp()
	if err != nil {
		return err
	}
	script := filepath.Join(dir, "install.cmd")
	body := "@echo off\r\n" +
		":wait\r\n" +
		fmt.Sprintf("tasklist /FI \"PID eq %d\" 2>nul | find \"%d\" >nul\r\n", i.PID, i.PID) +
		"if not errorlevel 1 (\r\n" +
		"  timeout /t 1 /nobreak >nul\r\n" +
		"  goto wait\r\n" +
		")\r\n" +
		"start \"\" \"" + i.AssetPath + "\" /S\r\n" +
		"del \"%~f0\"\r\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "start", "", script)
	cmd.SysProcAttr = detachedAttr()
	return cmd.Start()
}

// startDetached runs a script so that it outlives the app.
func startDetached(script string) error {
	cmd := exec.Command("/bin/sh", script)
	cmd.SysProcAttr = detachedAttr()
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func sh(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
