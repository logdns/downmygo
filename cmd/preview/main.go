// Command preview renders the downmygo window without opening it, for
// screenshots in the docs and for checking a change to the layout.
//
//	go run ./cmd/preview
package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/app"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
)

func main() {
	out := "docs/screens"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		panic(err)
	}
	var renderAt func(name string, a *app.App, w, h int)
	renderAt = func(name string, a *app.App, w, h int) {
		a.SetPreviewSize(w, h)
		img := ui.Render(a.View, w, h, 2)
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, img); err != nil {
			panic(err)
		}
		f.Close()
		fmt.Println(filepath.Join(out, name))
	}
	render := func(name string, a *app.App) { renderAt(name, a, 1120, 720) }
	// The same window in each language the app ships, and narrow enough
	// for the compact layout.
	for _, lang := range []struct{ tag, suffix string }{
		{"en", ""},
		{"zh-Hans", "-zh-Hans"},
		{"zh-Hant", "-zh-Hant"},
	} {
		i18n.SetLocaleFunc(func() string { return lang.tag })
		render("main-light"+lang.suffix+".png", demo(false))
		if lang.tag == "en" {
			render("main-dark.png", demo(true))
			renderAt("main-narrow.png", demo(false), 760, 620)

			opts := demo(false)
			opts.ShowOptions()
			render("options-light.png", opts)

			conn := demo(false)
			conn.ShowOptions()
			conn.ShowOptionsTab(1)
			render("options-connection.png", conn)

			adv := demo(false)
			adv.ShowOptions()
			adv.ShowOptionsTab(4)
			render("options-advanced.png", adv)

			add := demo(false)
			add.OpenAdd("")
			render("add-light.png", add)
		}
	}
	i18n.SetLocaleFunc(func() string { return "en" })
}

func demo(dark bool) *app.App {
	dir := "."
	st := &store.State{
		Version:    1,
		Settings:   model.Defaults(dir),
		Categories: model.DefaultCategories(dir),
		Queues:     model.DefaultQueues(),
	}
	st.Settings.Theme = "light"
	if dark {
		st.Settings.Theme = "dark"
	}
	st.Settings.RemoteEnabled = true
	now := time.Now()
	mk := func(name string, size, done int64, status model.Status, speed float64, ago time.Duration) *model.Download {
		d := &model.Download{
			ID: model.NewID(), URL: "https://example.com/files/" + name,
			FileName: name, Dir: filepath.Join(dir, "Downloads"), Size: size, Done: done,
			Status: status, Speed: speed, Category: "video", Added: now.Add(-ago),
			Connections: 8, Resumable: true,
		}
		if status == model.StatusComplete {
			d.Completed = now.Add(-ago + time.Minute)
			d.Done = size
		}
		if status == model.StatusDownloading {
			n := 8
			part := size / int64(n)
			for i := 0; i < n; i++ {
				seg := model.Segment{Start: int64(i) * part, End: int64(i+1)*part - 1}
				if i == n-1 {
					seg.End = size - 1
				}
				seg.Done = seg.Len() * done / size
				if i%3 == 0 {
					seg.Done = seg.Len()
				}
				d.Segments = append(d.Segments, seg)
			}
		}
		return d
	}
	st.Downloads = []*model.Download{
		mk("Interstellar.2014.2160p.HDR.mkv", 18<<30, 11<<30, model.StatusDownloading, 42<<20, 12*time.Minute),
		mk("Ubuntu-24.04.1-desktop-amd64.iso", 5<<30, 5<<30, model.StatusComplete, 0, 2*time.Hour),
		mk("design-system.fig", 240<<20, 96<<20, model.StatusPaused, 0, 26*time.Hour),
		mk("annual-report-2026.pdf", 12<<20, 12<<20, model.StatusComplete, 0, 50*time.Hour),
		mk("voice-memo.m4a", 48<<20, 3<<20, model.StatusError, 0, 72*time.Hour),
	}
	st.Downloads[4].Error = "the server closed the connection"
	a := app.New(st, nil)
	// New() parks anything that was running; the demo wants it running.
	for _, d := range a.State().Downloads {
		if d.FileName == "Interstellar.2014.2160p.HDR.mkv" {
			d.Status = model.StatusDownloading
			d.Speed = 42 << 20
		}
	}
	for i, d := range a.Visible() {
		if d.FileName == "Interstellar.2014.2160p.HDR.mkv" {
			a.SetSelection(i)
		}
	}
	return a
}
