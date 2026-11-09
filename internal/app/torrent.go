package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/downmygo/downmygo/internal/engine"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/torrent"
	"github.com/downmygo/downmygo/locales"
)

// torrentEngine starts the app's torrent client the first time a magnet
// or a torrent file arrives.
func (a *App) torrentEngine() (*torrent.Engine, error) {
	if a.torrents != nil {
		return a.torrents, nil
	}
	s := &a.st.Settings
	eng, err := torrent.New(torrent.Config{
		DownloadDir: a.st.Settings.DownloadDir,
		DataDir:     filepath.Join(a.dataDir(), "torrents"),
		Seed:        s.SeedAfterDone,
		SeedRatio:   s.SeedRatio,
		SeedTime:    time.Duration(s.SeedMinutes) * time.Minute,
		UploadLimit: s.UploadLimit,
	})
	if err != nil {
		return nil, err
	}
	a.torrents = eng
	return eng, nil
}

// addTorrent adds a magnet or a torrent file as a download of its own.
func (a *App) addTorrent(raw string) *model.Download {
	eng, err := a.torrentEngine()
	if err != nil {
		a.toast = locales.ToastTorrentFailed(err.Error())
		return nil
	}
	// A torrent's name comes from the link, and a magnet can say
	// anything: it names a file, so it is cleaned like one.
	name := engine.SanitizeName(torrentName(raw))
	// The client writes into one folder, so a torrent stays in the
	// download folder even though its category is Torrents.
	d := &model.Download{
		ID: model.NewID(), URL: raw, FileName: name,
		Dir:    a.st.Settings.DownloadDir,
		Status: model.StatusConnecting, Kind: model.KindTorrent,
		Category: "torrents", Added: time.Now(), Connections: 1,
	}
	a.st.Downloads = append(a.st.Downloads, d)
	a.markDirty()

	ctx, cancel := context.WithCancel(context.Background())
	if a.torrentCancels == nil {
		a.torrentCancels = map[string]context.CancelFunc{}
	}
	a.torrentCancels[d.ID] = cancel

	go func() {
		err := eng.Add(ctx, d.ID, raw, func(p torrent.Progress) {
			win := a.win
			if win == nil {
				return
			}
			win.Update(func() {
				if d.Status == model.StatusPaused {
					return
				}
				if p.Name != "" && d.FileName != p.Name {
					d.FileName = p.Name
				}
				if p.Size > 0 {
					d.Size = p.Size
				}
				d.Done = p.Done
				d.Speed = p.Speed
				d.Peers = p.Peers
				d.Uploaded = p.Uploaded
				d.Ratio = p.Ratio
				if p.Seeding {
					if d.Status != model.StatusSeeding {
						d.Status = model.StatusSeeding
						a.notifyComplete(d)
						a.markDirty()
						a.Save()
					}
					return
				}
				if p.Complete {
					d.Status = model.StatusComplete
					d.Completed = time.Now()
					d.Speed = 0
					a.notifyComplete(d)
					a.markDirty()
					a.Save()
					a.pump()
					return
				}
				d.Status = model.StatusDownloading
				a.markDirty()
			})
		})
		if err != nil {
			win := a.win
			if win == nil {
				return
			}
			win.Update(func() {
				d.Status = model.StatusError
				d.Error = err.Error()
				d.Speed = 0
				a.notifyError(d)
				a.markDirty()
				a.Save()
			})
		}
	}()
	return d
}

// torrentName is what a magnet or a torrent file says the download is
// called, before its metadata arrives.
func torrentName(raw string) string {
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "magnet:") {
		for _, part := range strings.Split(raw, "&") {
			if strings.HasPrefix(part, "dn=") {
				if name := strings.TrimPrefix(part, "dn="); name != "" {
					return strings.ReplaceAll(name, "+", " ")
				}
			}
		}
		return "magnet"
	}
	base := filepath.Base(strings.SplitN(raw, "?", 2)[0])
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// pauseTorrent stops asking for pieces.
func (a *App) pauseTorrent(d *model.Download) {
	if a.torrents != nil {
		a.torrents.Pause(d.ID)
	}
	d.Status = model.StatusPaused
	d.Speed = 0
	a.markDirty()
}

// resumeTorrent asks for pieces again.
func (a *App) resumeTorrent(d *model.Download) {
	if a.torrents != nil {
		a.torrents.Resume(d.ID)
	}
	if d.Size > 0 && d.Done >= d.Size {
		d.Status = model.StatusSeeding
	} else {
		d.Status = model.StatusDownloading
	}
	a.markDirty()
}

// removeTorrent drops a torrent and, when asked, its files.
func (a *App) removeTorrent(d *model.Download, deleteFile bool) {
	if a.torrents != nil {
		a.torrents.Remove(d.ID)
	}
	if cancel, ok := a.torrentCancels[d.ID]; ok {
		cancel()
		delete(a.torrentCancels, d.ID)
	}
	if deleteFile {
		// A torrent writes a file or a folder named after it; it must be
		// inside the download folder, whatever the name says.
		path := filepath.Join(d.Dir, d.FileName)
		if rel, err := filepath.Rel(d.Dir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			_ = os.RemoveAll(path)
		}
	}
}

// closeTorrents stops the client when the app quits.
func (a *App) closeTorrents() {
	if a.torrents != nil {
		a.torrents.Close()
		a.torrents = nil
	}
	for id, cancel := range a.torrentCancels {
		cancel()
		delete(a.torrentCancels, id)
	}
}
