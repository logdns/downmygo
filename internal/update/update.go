// Package update asks GitHub for the latest release and says whether it
// is newer than the one running, and which of its files fits this
// platform.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Asset is a file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is a GitHub release.
type Release struct {
	Tag     string  `json:"tag_name"`
	Name    string  `json:"name"`
	Notes   string  `json:"body"`
	URL     string  `json:"html_url"`
	Draft   bool    `json:"draft"`
	Pre     bool    `json:"prerelease"`
	Assets  []Asset `json:"assets"`
	Version string  `json:"-"`
}

// APIBase is where releases are asked for; a test changes it.
var APIBase = "https://api.github.com"

// Check asks repo (as "owner/name") for its latest release.
func Check(ctx context.Context, client *http.Client, repo, current string) (*Release, bool, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		APIBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "downmygo")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, false, err
	}
	rel.Version = strings.TrimPrefix(rel.Tag, "v")
	return &rel, Compare(rel.Version, current) > 0, nil
}

// Compare orders two versions as -1, 0 or 1. A version is numbers
// separated by dots, with an optional -prerelease that sorts before the
// release it belongs to.
func Compare(a, b string) int {
	ac, apre := split(a)
	bc, bpre := split(b)
	for i := 0; i < len(ac) || i < len(bc); i++ {
		var x, y int
		if i < len(ac) {
			x = ac[i]
		}
		if i < len(bc) {
			y = bc[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	case apre < bpre:
		return -1
	}
	return 1
}

func split(v string) ([]int, string) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	var nums []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	return nums, pre
}

// AssetFor picks the file of a release that fits a platform, or "" when
// the release has none.
func AssetFor(rel *Release, goos, goarch string) Asset {
	for _, a := range rel.Assets {
		if !strings.HasSuffix(strings.ToLower(a.Name), ".dmg") {
			continue
		}
		if goos == "darwin" {
			return a
		}
	}
	lower := strings.ToLower(goos + "-" + goarch)
	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		switch {
		case goos == "windows" && strings.HasSuffix(name, ".exe") && strings.Contains(name, goarch):
			return a
		case goos == "linux" && strings.Contains(name, lower) && strings.HasSuffix(name, ".tar.gz"):
			return a
		case goos == "linux" && strings.Contains(name, goarch) && strings.HasSuffix(name, ".deb"):
			return a
		}
	}
	return Asset{}
}

// ThisPlatform is the platform the running app is.
func ThisPlatform() (string, string) { return runtime.GOOS, runtime.GOARCH }
