// Package store keeps downmygo's state on disk. Every write is atomic: a
// crash leaves either the old file or the new one, never half of each.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/downmygo/downmygo/internal/model"
)

// State is the whole of what the app remembers between runs.
type State struct {
	Version    int               `json:"version"`
	Settings   model.Settings    `json:"settings"`
	Categories []model.Category  `json:"categories"`
	Queues     []model.Queue     `json:"queues"`
	Downloads  []*model.Download `json:"downloads"`

	// Download limit bookkeeping: how much was downloaded in the current
	// window, and when the window started.
	LimitUsed  int64     `json:"limitUsed,omitempty"`
	LimitSince time.Time `json:"limitSince,omitempty"`
}

// Store reads and writes the state file.
type Store struct {
	mu   sync.Mutex
	path string
	dir  string
}

// DataDir returns the folder downmygo keeps its files in. A portable copy
// (a `portable` file beside the program) keeps them in `data` beside it,
// and DOWNMYGO_DATA_DIR overrides both.
func DataDir() (string, error) {
	if v := os.Getenv("DOWNMYGO_DATA_DIR"); v != "" {
		return v, os.MkdirAll(v, 0o755)
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		base := filepath.Dir(exe)
		for _, name := range []string{"portable", "downmygo.portable"} {
			if _, err := os.Stat(filepath.Join(base, name)); err == nil {
				dir := filepath.Join(base, "data")
				return dir, os.MkdirAll(dir, 0o755)
			}
		}
	}
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = filepath.Join(os.Getenv("APPDATA"), "downmygo")
	case "darwin":
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Library", "Application Support", "downmygo")
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			dir = filepath.Join(x, "downmygo")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".local", "share", "downmygo")
		}
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// DefaultDownloadDir is where downloads go before the user chooses.
func DefaultDownloadDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Downloads", "downmygo")
	}
	return "downloads"
}

// Open prepares the store in the given folder.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, path: filepath.Join(dir, "state.json")}, nil
}

// Dir is the folder the store writes in.
func (s *Store) Dir() string { return s.dir }

// Path is the state file.
func (s *Store) Path() string { return s.path }

// Load reads the state, or returns defaults the first time.
func (s *Store) Load() (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		st := s.fresh()
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		// Keep the broken file for the user, and start clean rather than
		// refusing to open the app.
		_ = os.WriteFile(s.path+".broken", data, 0o600)
		fresh := s.fresh()
		return fresh, fmt.Errorf("state.json was unreadable (%v); a copy is at state.json.broken", err)
	}
	s.repair(&st)
	return &st, nil
}

func (s *Store) fresh() *State {
	dir := model.Defaults(DefaultDownloadDir())
	return &State{
		Version:    1,
		Settings:   dir,
		Categories: model.DefaultCategories(dir.DownloadDir),
		Queues:     model.DefaultQueues(),
		Downloads:  []*model.Download{},
	}
}

// repair fills what an older or hand-edited file leaves out.
func (s *Store) repair(st *State) {
	def := s.fresh()
	if st.Version == 0 {
		st.Version = def.Version
	}
	if st.Settings.DownloadDir == "" {
		// No settings at all: start from the defaults rather than from a
		// struct of zero values, which would silently turn every option off.
		st.Settings = def.Settings
	}
	if st.Settings.UserAgent == "" {
		st.Settings.UserAgent = def.Settings.UserAgent
	}
	if st.Settings.Accent == "" {
		st.Settings.Accent = def.Settings.Accent
	}
	if st.Settings.Theme == "" {
		st.Settings.Theme = def.Settings.Theme
	}
	if st.Settings.Connections < 1 {
		st.Settings.Connections = def.Settings.Connections
	}
	if st.Settings.MaxConnections < st.Settings.Connections {
		st.Settings.MaxConnections = def.Settings.MaxConnections
	}
	if st.Settings.MaxRetries == 0 {
		st.Settings.MaxRetries = def.Settings.MaxRetries
	}
	if st.Settings.DuplicateAction == "" {
		st.Settings.DuplicateAction = def.Settings.DuplicateAction
	}
	if st.Settings.SidebarWidth < 160 {
		st.Settings.SidebarWidth = def.Settings.SidebarWidth
	}
	if st.Settings.DetailHeight < 120 {
		st.Settings.DetailHeight = def.Settings.DetailHeight
	}
	if len(st.Categories) == 0 {
		st.Categories = model.DefaultCategories(st.Settings.DownloadDir)
	}
	for i := range st.Categories {
		if st.Categories[i].Folder == "" {
			if st.Categories[i].ID == "general" {
				st.Categories[i].Folder = st.Settings.DownloadDir
			} else {
				st.Categories[i].Folder = filepath.Join(st.Settings.DownloadDir, st.Categories[i].Name)
			}
		}
	}
	if len(st.Queues) == 0 {
		st.Queues = def.Queues
	}
	// A hand-edited file may hold a null entry; drop it rather than
	// crashing on the first frame.
	kept := st.Downloads[:0]
	for _, d := range st.Downloads {
		if d == nil {
			continue
		}
		kept = append(kept, d)
	}
	st.Downloads = kept
	for _, d := range st.Downloads {
		if d.ID == "" {
			d.ID = model.NewID()
		}
		if d.Added.IsZero() {
			d.Added = now()
		}
		// A run that ended while the app was closed is paused, not running.
		if d.Status.Active() || d.Status == model.StatusQueued || d.Status == model.StatusWaiting {
			d.Status = model.StatusPaused
		}
	}
}

// Save writes the state atomically.
func (s *Store) Save(st *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// The state holds the API token: only the user reads it.
	return WriteFileAtomic(s.path, data, 0o600)
}

// WriteFileAtomic writes data to a temporary file beside path and renames
// it into place, syncing both the file and its folder.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() {
		f.Close()
		os.Remove(tmp)
	}
	if _, err := f.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func now() time.Time { return time.Now() }
