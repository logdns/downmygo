package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
)

func demoApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	st := &store.State{
		Version:    1,
		Settings:   model.Defaults(dir),
		Categories: model.DefaultCategories(dir),
		Queues:     model.DefaultQueues(),
	}
	st.Settings.Theme = "light"
	now := time.Now()
	st.Downloads = []*model.Download{
		{ID: "a", URL: "https://example.com/a.mkv", FileName: "a.mkv", Dir: dir,
			Size: 100 << 20, Done: 40 << 20, Status: model.StatusDownloading,
			Speed: 4 << 20, Category: "video", Added: now.Add(-time.Minute), Connections: 8},
		{ID: "b", URL: "https://example.com/b.pdf", FileName: "b.pdf", Dir: dir,
			Size: 2 << 20, Done: 2 << 20, Status: model.StatusComplete,
			Category: "documents", Added: now.Add(-time.Hour)},
		{ID: "c", URL: "https://example.com/c.zip", FileName: "c.zip", Dir: dir,
			Size: 8 << 20, Done: 1 << 20, Status: model.StatusPaused,
			Category: "compressed", Added: now.Add(-2 * time.Hour)},
	}
	return New(st, nil)
}

func TestViewShowsTheList(t *testing.T) {
	a := demoApp(t)
	tt := ui.NewTester(a.View, 1120, 720)
	for _, want := range []string{"downmygo", "All Downloads", "a.mkv", "b.pdf", "c.zip", "Add URL"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestFiltersAndSearch(t *testing.T) {
	a := demoApp(t)
	tt := ui.NewTester(a.View, 1120, 720)
	if err := tt.Click("Finished"); err != nil {
		t.Fatal(err)
	}
	if a.Filter() != "finished" {
		t.Fatalf("filter = %q", a.Filter())
	}
	if !tt.HasText("b.pdf") || tt.HasText("a.mkv") {
		t.Fatalf("finished filter shows %q", tt.Texts())
	}

	a.SetFilter("all")
	a.SetQuery("zip")
	tt2 := ui.NewTester(a.View, 1120, 720)
	if !tt2.HasText("c.zip") || tt2.HasText("a.mkv") {
		t.Fatalf("search shows %q", tt2.Texts())
	}
}

func TestAddDialogOpens(t *testing.T) {
	a := demoApp(t)
	tt := ui.NewTester(a.View, 1120, 720)
	if err := tt.Click("Add URL"); err != nil {
		t.Fatal(err)
	}
	if !a.addOpen {
		t.Fatal("the dialog did not open")
	}
	if !tt.HasText("Add a download") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestSelectionAndDetail(t *testing.T) {
	a := demoApp(t)
	a.SetFilter("all")
	tt := ui.NewTester(a.View, 1120, 720)
	if err := tt.Click("a.mkv"); err != nil {
		t.Fatal(err)
	}
	d := a.SelectedDownload()
	if d == nil || d.FileName != "a.mkv" {
		t.Fatalf("selected %+v", d)
	}
	if !tt.HasText("Time left") {
		t.Fatalf("no detail panel: %q", tt.Texts())
	}
}

func TestCategoryForAndUniqueName(t *testing.T) {
	cats := model.DefaultCategories(t.TempDir())
	if got := model.CategoryFor(cats, "movie.mkv"); got != "video" {
		t.Errorf("movie category = %q", got)
	}
	if got := model.CategoryFor(cats, "unknown.xyz"); got != "general" {
		t.Errorf("unknown category = %q", got)
	}
	dir := t.TempDir()
	taken := map[string]bool{filepath.Join(dir, "a.zip"): true}
	name := uniqueIn(dir, "a.zip", func(p string) bool { return taken[p] })
	if name != "a (2).zip" {
		t.Errorf("unique name = %q", name)
	}
}
