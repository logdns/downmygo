// Package model holds the values downmygo saves and shows: downloads,
// categories, queues and settings. It has no dependency on the UI or the
// network, so every rule here can be tested on its own.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status is where a download stands. The values are saved, so do not
// renumber them.
type Status int

const (
	StatusQueued Status = iota
	StatusConnecting
	StatusDownloading
	StatusPaused
	StatusComplete
	StatusError
	StatusWaiting // waiting for a queue slot, the schedule or a limit
	StatusSeeding // the download is done and the torrent is still sharing
)

func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "Queued"
	case StatusConnecting:
		return "Connecting"
	case StatusDownloading:
		return "Downloading"
	case StatusPaused:
		return "Paused"
	case StatusComplete:
		return "Complete"
	case StatusError:
		return "Error"
	case StatusWaiting:
		return "Waiting"
	case StatusSeeding:
		return "Seeding"
	}
	return "Unknown"
}

// Active reports whether the status means work is happening or about to.
func (s Status) Active() bool {
	return s == StatusConnecting || s == StatusDownloading
}

// Finished reports whether no more work will happen without the user.
func (s Status) Finished() bool {
	return s == StatusComplete || s == StatusError
}

// Kind is what the download fetches.
type Kind int

const (
	KindHTTP Kind = iota
	KindTorrent
	KindVideo
	KindGallery
)

func (k Kind) String() string {
	switch k {
	case KindTorrent:
		return "Torrent"
	case KindVideo:
		return "Video"
	case KindGallery:
		return "Gallery"
	}
	return "HTTP"
}

// Segment is one byte range of a file and how much of it is on disk.
type Segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"` // inclusive
	Done  int64 `json:"done"`
}

// Len is the segment's size in bytes.
func (s Segment) Len() int64 { return s.End - s.Start + 1 }

// Remaining is how many bytes are still missing.
func (s Segment) Remaining() int64 {
	n := s.Len() - s.Done
	if n < 0 {
		return 0
	}
	return n
}

// Download is one item of the list. Its fields are saved as JSON; the
// runtime fields (Speed, ETA, Segments) are refreshed while it runs.
type Download struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	FinalURL    string    `json:"finalUrl,omitempty"`
	FileName    string    `json:"fileName"`
	Dir         string    `json:"dir"`
	Size        int64     `json:"size"`
	Done        int64     `json:"done"`
	Status      Status    `json:"status"`
	Error       string    `json:"error,omitempty"`
	Category    string    `json:"category"`
	Queue       string    `json:"queue,omitempty"`
	Description string    `json:"description,omitempty"`
	Kind        Kind      `json:"kind"`
	Added       time.Time `json:"added"`
	Completed   time.Time `json:"completed,omitempty"`

	// What the server said, kept for a safe resume.
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Resumable    bool   `json:"resumable"`

	// Request options.
	Referer     string `json:"referer,omitempty"`
	Cookies     string `json:"cookies,omitempty"`
	UserAgent   string `json:"userAgent,omitempty"`
	Checksum    string `json:"checksum,omitempty"` // "sha256:<hex>"
	Connections int    `json:"connections,omitempty"`

	// Peers is how many peers a torrent has, and Uploaded how much it has
	// shared.
	Peers    int     `json:"peers,omitempty"`
	Uploaded int64   `json:"uploaded,omitempty"`
	Ratio    float64 `json:"ratio,omitempty"`

	// Runtime, not saved as truth.
	Speed    float64   `json:"-"`
	Segments []Segment `json:"-"`
	Attempts int       `json:"-"`
}

// Path is the final file's path.
func (d *Download) Path() string { return filepath.Join(d.Dir, d.FileName) }

// PartPath is the file being written, next to the final one.
func (d *Download) PartPath() string { return d.Path() + ".dmpart" }

// StatePath is where the segment table is kept for a crash-safe resume.
func (d *Download) StatePath() string { return d.Path() + ".dmpart.json" }

// Percent is 0 to 1, or -1 when the size is unknown.
func (d *Download) Percent() float64 {
	if d.Size <= 0 {
		if d.Status == StatusComplete {
			return 1
		}
		return -1
	}
	p := float64(d.Done) / float64(d.Size)
	if p > 1 {
		return 1
	}
	return p
}

// ETA is the time left at the current speed, zero when unknown.
func (d *Download) ETA() time.Duration {
	if d.Speed <= 0 || d.Size <= 0 || d.Done >= d.Size {
		return 0
	}
	secs := float64(d.Size-d.Done) / d.Speed
	if secs > 86400*30 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// Finished reports whether the download is complete.
func (d *Download) Finished() bool { return d.Status == StatusComplete }

// Category describes where files of some types go.
type Category struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Folder    string   `json:"folder"`
	Types     []string `json:"types"`
	Builtin   bool     `json:"builtin,omitempty"`
	Protected bool     `json:"protected,omitempty"` // General and Torrents
}

// Match reports whether a file name belongs to the category. Types are
// extensions without the dot, matched case-insensitively; "*" matches all.
func (c Category) Match(name string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	for _, t := range c.Types {
		t = strings.ToLower(strings.TrimPrefix(t, "."))
		if t == "*" || t == ext {
			return true
		}
	}
	return false
}

// Queue is a list of downloads that run together, a few at a time.
type Queue struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Parallel int    `json:"parallel"`
	Builtin  bool   `json:"builtin,omitempty"`
	KeepDone bool   `json:"keepDone,omitempty"`

	// Schedule. A zero time means no schedule.
	StartAt   string `json:"startAt,omitempty"` // "HH:MM"
	StopAt    string `json:"stopAt,omitempty"`  // "HH:MM"
	Days      []int  `json:"days,omitempty"`    // 0 Sunday .. 6 Saturday; empty is every day
	Once      string `json:"once,omitempty"`    // "2006-01-02"; used with StartAt
	OnStartup bool   `json:"onStartup,omitempty"`
	Retries   int    `json:"retries,omitempty"` // 0 keeps trying
	Wake      bool   `json:"wake,omitempty"`

	// When the queue is done.
	OpenWhenDone  string `json:"openWhenDone,omitempty"`
	ExitWhenDone  bool   `json:"exitWhenDone,omitempty"`
	PowerWhenDone string `json:"powerWhenDone,omitempty"` // "", "shutdown", "sleep", "hibernate"
}

// Settings is everything the Options window changes.
type Settings struct {
	Language         string `json:"language"`
	Theme            string `json:"theme"` // "system", "light", "dark"
	Accent           string `json:"accent"`
	DownloadDir      string `json:"downloadDir"`
	TempDir          string `json:"tempDir,omitempty"`
	Connections      int    `json:"connections"`
	MaxConnections   int    `json:"maxConnections"`
	GlobalLimit      int64  `json:"globalLimit"` // bytes/sec, 0 is none
	PerDownloadLimit int64  `json:"perDownloadLimit"`
	UserAgent        string `json:"userAgent"`
	Proxy            string `json:"proxy,omitempty"` // "", "system", or a URL
	BindAdapter      string `json:"bindAdapter,omitempty"`
	DNS              string `json:"dns,omitempty"` // "", "system", "doh", "dot", or an endpoint

	StartWithSystem    bool   `json:"startWithSystem"`
	MinimizeToTray     bool   `json:"minimizeToTray"`
	CloseToTray        bool   `json:"closeToTray"`
	ClipboardWatch     bool   `json:"clipboardWatch"`
	ConfirmDelete      bool   `json:"confirmDelete"`
	NotifyOnComplete   bool   `json:"notifyOnComplete"`
	NotifyOnError      bool   `json:"notifyOnError"`
	StartDownloads     bool   `json:"startDownloads"` // start at once when added
	ShowAddDialog      bool   `json:"showAddDialog"`
	AutoExtract        bool   `json:"autoExtract"`
	DeleteArchive      bool   `json:"deleteArchive"`
	FileDateFromServer bool   `json:"fileDateFromServer"`
	DuplicateAction    string `json:"duplicateAction"` // "ask", "rename", "overwrite", "reuse"
	MaxRetries         int    `json:"maxRetries"`

	// Download limits (Scheduler).
	LimitEnabled bool  `json:"limitEnabled"`
	LimitBytes   int64 `json:"limitBytes"`
	LimitHours   int   `json:"limitHours"`

	// Remote control and automation.
	RemoteEnabled bool   `json:"remoteEnabled"`
	RemotePort    int    `json:"remotePort"`
	APIToken      string `json:"apiToken"`

	// Debrid: a premium service that turns a hosted link into a direct one.
	DebridProvider string `json:"debridProvider,omitempty"` // "", "real-debrid", "alldebrid"
	DebridToken    string `json:"debridToken,omitempty"`

	// External downloaders: yt-dlp for videos, gallery-dl for galleries.
	UseYtDlp     bool `json:"useYtDlp"`
	UseGalleryDL bool `json:"useGalleryDL"`

	// Updates.
	CheckUpdates bool `json:"checkUpdates"`
	// SilentUpdates installs a new version when it is there.
	SilentUpdates bool `json:"silentUpdates"`

	// Seeding: share a torrent after it is done, up to a ratio or a time,
	// and cap the upload.
	SeedAfterDone bool    `json:"seedAfterDone"`
	SeedRatio     float64 `json:"seedRatio,omitempty"`
	SeedMinutes   int     `json:"seedMinutes,omitempty"`
	UploadLimit   int64   `json:"uploadLimit,omitempty"` // bytes/sec, 0 is none

	// ThemeFile is the theme file to use, or "" for the built-in themes.
	ThemeFile string `json:"themeFile,omitempty"`

	// GrabExtensions are the file types the link grabber keeps; empty
	// keeps every link.
	GrabExtensions []string `json:"grabExtensions,omitempty"`

	// Window layout.
	SidebarWidth float32 `json:"sidebarWidth"`
	DetailHeight float32 `json:"detailHeight"`
	SortColumn   int     `json:"sortColumn"`
	SortDesc     bool    `json:"sortDesc"`
}

// Defaults returns the settings a new install starts with.
func Defaults(downloadDir string) Settings {
	return Settings{
		Language:         "", // "" follows the system's languages
		Theme:            "system",
		Accent:           "#3B6EF6",
		DownloadDir:      downloadDir,
		Connections:      8,
		MaxConnections:   32,
		UserAgent:        "downmygo/1.5 (+https://logdns.github.io/downmygo/)",
		MinimizeToTray:   true,
		CloseToTray:      true,
		ClipboardWatch:   true,
		ConfirmDelete:    true,
		NotifyOnComplete: true,
		NotifyOnError:    true,
		StartDownloads:   true,
		ShowAddDialog:    true,
		DuplicateAction:  "ask",
		MaxRetries:       5,
		LimitHours:       5,
		LimitBytes:       200 << 20,
		RemotePort:       8463,
		CheckUpdates:     true,
		SeedAfterDone:    true,
		SeedRatio:        2,
		SeedMinutes:      0,
		UseYtDlp:         true,
		UseGalleryDL:     true,
		SidebarWidth:     236,
		DetailHeight:     208,
	}
}

// DefaultCategories returns the built-in categories, with folders under
// downloadDir.
func DefaultCategories(downloadDir string) []Category {
	join := func(name string) string { return filepath.Join(downloadDir, name) }
	return []Category{
		{ID: "general", Name: "General", Folder: downloadDir, Types: []string{"*"}, Builtin: true, Protected: true},
		{ID: "compressed", Name: "Compressed", Folder: join("Compressed"), Types: []string{"zip", "rar", "7z", "tar", "gz", "xz", "bz2", "zst", "iso"}, Builtin: true},
		{ID: "documents", Name: "Documents", Folder: join("Documents"), Types: []string{"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "epub", "mobi", "txt", "md", "csv"}, Builtin: true},
		{ID: "music", Name: "Music", Folder: join("Music"), Types: []string{"mp3", "flac", "wav", "m4a", "aac", "ogg", "opus"}, Builtin: true},
		{ID: "video", Name: "Video", Folder: join("Video"), Types: []string{"mp4", "mkv", "webm", "avi", "mov", "m4v", "ts"}, Builtin: true},
		{ID: "programs", Name: "Programs", Folder: join("Programs"), Types: []string{"exe", "msi", "msix", "dmg", "pkg", "deb", "rpm", "appimage", "apk"}, Builtin: true},
		{ID: "torrents", Name: "Torrents", Folder: join("Torrents"), Types: []string{"torrent"}, Builtin: true, Protected: true},
	}
}

// DefaultQueues returns the two queues every install has.
func DefaultQueues() []Queue {
	return []Queue{
		{ID: "main", Name: "Main download queue", Parallel: 4, Builtin: true},
		{ID: "sync", Name: "Synchronization queue", Parallel: 2, Builtin: true, KeepDone: true},
	}
}

// CategoryFor finds the category a file belongs to, General when none does.
func CategoryFor(cats []Category, name string) string {
	for _, c := range cats {
		if c.ID == "general" {
			continue
		}
		if c.Match(name) {
			return c.ID
		}
	}
	return "general"
}

// NewID makes an identifier that is unique enough for a list and a file.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// SortDownloads orders a copy of the list by a column and direction.
func SortDownloads(list []*Download, column int, desc bool) []*Download {
	out := append([]*Download(nil), list...)
	less := func(a, b *Download) bool {
		switch column {
		case 0:
			return strings.ToLower(a.FileName) < strings.ToLower(b.FileName)
		case 1:
			return a.Size < b.Size
		case 2:
			return a.Percent() < b.Percent()
		case 3:
			return a.Speed < b.Speed
		case 4:
			return a.Status < b.Status
		case 5:
			return a.Added.Before(b.Added)
		case 6:
			return a.Completed.Before(b.Completed)
		}
		return a.Added.Before(b.Added)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if desc {
			return less(out[j], out[i])
		}
		return less(out[i], out[j])
	})
	return out
}
