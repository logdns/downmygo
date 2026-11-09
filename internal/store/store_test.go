package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/downmygo/downmygo/internal/model"
)

func TestLoadDefaultsAndSave(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Categories) == 0 || len(st.Queues) == 0 {
		t.Fatalf("defaults missing: %+v", st)
	}
	st.Downloads = append(st.Downloads, &model.Download{
		ID: "x", URL: "https://example.com/x", FileName: "x", Dir: dir,
		Status: model.StatusComplete,
	})
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	again, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Downloads) != 1 || again.Downloads[0].ID != "x" {
		t.Fatalf("round trip lost the download: %+v", again.Downloads)
	}
	// No temporary files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

func TestBrokenFileIsKept(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err == nil {
		t.Fatal("a broken file loaded without a word")
	}
	if st == nil || len(st.Categories) == 0 {
		t.Fatal("no fresh state after a broken file")
	}
	if _, err := os.Stat(s.Path() + ".broken"); err != nil {
		t.Fatal("the broken file was not kept")
	}
}

func TestRepairParksRunningDownloads(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	st, _ := s.Load()
	st.Downloads = []*model.Download{{ID: "r", URL: "u", FileName: "f", Status: model.StatusDownloading}}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Load()
	if again.Downloads[0].Status != model.StatusPaused {
		t.Fatalf("status = %v", again.Downloads[0].Status)
	}
}

func TestSaveKeepsTheTokenPrivate(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := s.fresh()
	st.Settings.APIToken = "secret-token"
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("state.json is %o, want 600: it holds the API token", perm)
	}
}

func TestNullDownloadIsDropped(t *testing.T) {
	dir := t.TempDir()
	body := `{"version":1,"settings":{},"categories":[],"queues":[],"downloads":[null,{"id":"a","fileName":"a.zip"}]}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Downloads) != 1 || st.Downloads[0].ID != "a" {
		t.Fatalf("downloads = %v", st.Downloads)
	}
}
