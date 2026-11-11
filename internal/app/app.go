// Package app is downmygo itself: the list, the queues, the scheduler and
// the window. All of its state is read and changed on the main thread,
// which is what the view runs on; goroutines hand work over with
// Window.Update.
package app

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/downmygo/downmygo/internal/engine"
	"github.com/downmygo/downmygo/internal/grab"
	"github.com/downmygo/downmygo/internal/limiter"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/internal/torrent"
	"github.com/downmygo/downmygo/internal/update"
	"github.com/downmygo/downmygo/locales"
)

// errLimit marks a download the traffic limit holds. It is kept in the
// state as a marker, not as a sentence, so the window says it in the
// language the app shows now.
const errLimit = "limit"

// SiteURL is the project's home, which GitHub Pages serves: the About
// window and the Help menu open it.
const SiteURL = "https://2sb.org/downmygo/"

// App is the whole application state.
type App struct {
	st    *store.State
	store *store.Store
	eng   *engine.Manager
	win   *mygo.Window
	tray  *mygo.Tray

	global *limiter.Limiter
	events chan engine.Event

	// View state. Main thread only.
	filter         string // "all", "unfinished", "finished", "cat:<id>", "queue:<id>"
	query          string
	selected       int
	sortCol        int
	sortDesc       bool
	listState      ui.ListState
	sidebarSel     string
	addOpen        bool
	add            AddForm
	optionsOpen    bool
	optionsTab     int
	aboutOpen      bool
	confirm        *Confirm
	toast          string
	lastClip       string
	clipWatch      atomic.Bool
	toolCancels    map[string]context.CancelFunc
	torrents       *torrent.Engine
	torrentCancels map[string]context.CancelFunc

	// The link grabber, the subscriptions and the update check.
	grabOpen    bool
	grabBusy    bool
	grabPage    string
	grabErr     string
	grabLinks   []grab.Link
	grabPicked  map[string]bool
	feedsOpen   bool
	importOpen  bool
	updateOpen  bool
	updateBusy  bool
	updateRel   *update.Release
	updateDone  float64
	updateTotal int64
	updateErr   string
	installing  bool
	showDetail  bool
	searchFocus bool
	sidebarW    float32
	detailH     float32
	viewList    []*model.Download
	columns     []colKind
	compact     bool
	narrow      bool
	previewW    int
	previewH    int
	confirmOpen bool

	// Options forms.
	newCatName   string
	newCatTypes  string
	newCatFolder string
	newQueueName string
	newFeedURL   string
	newFeedTypes string
	newFeedEvery int

	// Runtime bookkeeping.
	queueRunning map[string]bool
	lastDone     map[string]int64
	started      time.Time
	lastSave     time.Time
	dirty        bool
	quitting     bool
	api          *apiServer
}

// Confirm is a question the app asks before a destructive action.
type Confirm struct {
	Title   string
	Message string
	Detail  string
	Action  func()
	Delete  bool
}

// New makes an app from saved state.
func New(st *store.State, s *store.Store) *App {
	a := &App{
		st:           st,
		store:        s,
		eng:          engine.NewManager(),
		events:       make(chan engine.Event, 256),
		global:       limiter.New(st.Settings.GlobalLimit),
		queueRunning: map[string]bool{},
		lastDone:     map[string]int64{},
		started:      time.Now(),
		sidebarW:     st.Settings.SidebarWidth,
		detailH:      st.Settings.DetailHeight,
		filter:       "all",
		showDetail:   true,
	}
	if a.sidebarW < 160 {
		a.sidebarW = 236
	}
	if a.detailH < 120 {
		a.detailH = 208
	}
	a.eng.OnEvent = func(ev engine.Event) {
		select {
		case a.events <- ev:
		default:
			// The list is already behind; dropping a frame is better than
			// blocking a download.
		}
	}
	for _, q := range a.st.Queues {
		if q.ID == "main" {
			a.queueRunning[q.ID] = true
		}
	}
	// Downloads that were running when the app closed wait for the user.
	for _, d := range a.st.Downloads {
		if d.Status.Active() || d.Status == model.StatusQueued || d.Status == model.StatusWaiting {
			d.Status = model.StatusPaused
		}
		a.lastDone[d.ID] = d.Done
	}
	return a
}

// State is the saved state, for the CLI and tests.
func (a *App) State() *store.State { return a.st }

// Attach gives the app its window and starts the goroutines that feed it.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	go a.eventLoop()
	go a.tickLoop()
	a.pump()
}

// Settings returns the settings (main thread).
func (a *App) Settings() *model.Settings { return &a.st.Settings }

// Downloads returns the list (main thread).
func (a *App) Downloads() []*model.Download { return a.st.Downloads }

func (a *App) markDirty() { a.dirty = true }

// Save writes the state now.
func (a *App) Save() {
	if a.store == nil {
		return
	}
	a.st.Settings.SidebarWidth = a.sidebarW
	a.st.Settings.DetailHeight = a.detailH
	if err := a.store.Save(a.st); err != nil {
		a.toast = locales.ToastSaveFailed(err.Error())
		return
	}
	a.dirty = false
	a.lastSave = time.Now()
}

func (a *App) saveIfDirty() {
	if a.dirty && time.Since(a.lastSave) > 2*time.Second {
		a.Save()
	}
}

// eventLoop batches engine events and applies them on the main thread at
// a rate the eye can follow, instead of rebuilding the view per packet.
func (a *App) eventLoop() {
	pending := map[string]engine.Event{}
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case ev := <-a.events:
			pending[ev.ID] = ev
		case <-t.C:
			if len(pending) == 0 {
				continue
			}
			batch := pending
			pending = map[string]engine.Event{}
			win := a.win
			if win == nil {
				continue
			}
			win.Update(func() {
				for _, ev := range batch {
					a.applyEvent(ev)
				}
			})
		}
	}
}

func (a *App) applyEvent(ev engine.Event) {
	d := a.find(ev.ID)
	if d == nil {
		return
	}
	if ev.Size > 0 {
		d.Size = ev.Size
	}
	if ev.FileName != "" {
		d.FileName = ev.FileName
	}
	if ev.FinalURL != "" {
		d.FinalURL = ev.FinalURL
	}
	if ev.ETag != "" {
		d.ETag = ev.ETag
	}
	if ev.LastModified != "" {
		d.LastModified = ev.LastModified
	}
	d.Resumable = ev.Resumable
	d.Done = ev.Done
	d.Speed = ev.Speed
	d.Segments = ev.Segments
	d.Status = ev.Status
	d.Error = ev.Error
	if ev.Connections > 0 {
		d.Connections = ev.Connections
	}

	// Count what this download added toward the data limit.
	if prev := a.lastDone[ev.ID]; ev.Done > prev {
		a.st.LimitUsed += ev.Done - prev
	}
	a.lastDone[ev.ID] = ev.Done

	switch ev.Status {
	case model.StatusComplete:
		d.Completed = time.Now()
		d.Speed = 0
		d.Segments = nil
		if d.Size > 0 {
			d.Done = d.Size
		}
		a.notifyComplete(d)
		a.extractDone(d)
		a.pump()
	case model.StatusError:
		d.Speed = 0
		a.notifyError(d)
		a.pump()
	case model.StatusPaused:
		d.Speed = 0
	}
	a.markDirty()
	a.checkLimit()
}

func (a *App) find(id string) *model.Download {
	for _, d := range a.st.Downloads {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// AddForm is the Add URL dialog's state.
type AddForm struct {
	URL         string
	FileName    string
	Dir         string
	Category    string
	Connections int
	Referer     string
	Cookies     string
	UserAgent   string
	Checksum    string
	Description string
	Queue       string
	StartNow    bool
	Advanced    bool

	Probing   bool
	Info      *engine.Info
	Err       string
	Duplicate bool
	FromClip  bool
	probeSeq  int
}

// OpenAdd opens the Add URL dialog, optionally with a URL already in it.
func (a *App) OpenAdd(rawURL string) {
	s := &a.st.Settings
	a.add = AddForm{
		URL:         rawURL,
		Category:    "general",
		Dir:         s.DownloadDir,
		Connections: s.Connections,
		UserAgent:   s.UserAgent,
		StartNow:    true,
		Queue:       "",
	}
	a.addOpen = true
	if rawURL != "" {
		a.probeAdd()
	}
}

// probeAdd asks the server about the URL in the dialog, off the main
// thread, and fills in the name, size and category.
func (a *App) probeAdd() {
	form := &a.add
	form.probeSeq++
	seq := form.probeSeq
	form.Probing = true
	form.Err = ""
	form.Info = nil
	raw := strings.TrimSpace(form.URL)
	if raw == "" {
		form.Probing = false
		return
	}
	opts := engine.RequestOptions{
		UserAgent: form.UserAgent,
		Referer:   form.Referer,
		Cookies:   form.Cookies,
		Proxy:     a.st.Settings.Proxy,
		BindIP:    a.st.Settings.BindAdapter,
		DNS:       a.st.Settings.DNS,
		MaxConns:  a.st.Settings.Connections,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := engine.Probe(ctx, engine.NewClient(opts), raw, opts)
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			if a.add.probeSeq != seq || !a.addOpen {
				return
			}
			a.add.Probing = false
			if err != nil {
				a.add.Err = err.Error()
				return
			}
			a.add.Info = info
			if a.add.FileName == "" {
				a.add.FileName = info.FileName
			}
			if a.add.Category == "general" || a.add.Category == "" {
				a.add.Category = model.CategoryFor(a.st.Categories, info.FileName)
			}
			if c := a.category(a.add.Category); c != nil && a.add.Dir == a.st.Settings.DownloadDir {
				a.add.Dir = c.Folder
			}
			a.add.Duplicate = a.duplicateOf(raw) != nil
		})
	}()
}

// AddURL adds a download from a URL with the current defaults, for the
// clipboard, the API and the command line.
func (a *App) AddURL(rawURL string) *model.Download {
	a.add = AddForm{
		URL:         rawURL,
		Category:    "general",
		Dir:         a.st.Settings.DownloadDir,
		Connections: a.st.Settings.Connections,
		UserAgent:   a.st.Settings.UserAgent,
		StartNow:    a.st.Settings.StartDownloads,
	}
	// A programmatic add (clipboard, API, CLI) does not wait for a
	// question: a duplicate becomes a numbered file.
	if a.duplicateOf(rawURL) != nil && a.st.Settings.DuplicateAction == "ask" {
		a.add.Duplicate = true
	}
	return a.commitAdd()
}

func (a *App) duplicateOf(rawURL string) *model.Download {
	for _, d := range a.st.Downloads {
		if d.URL == rawURL {
			return d
		}
	}
	return nil
}

// commitAdd turns the dialog's form into a download and starts it.
func (a *App) commitAdd() *model.Download {
	f := &a.add
	raw := strings.TrimSpace(f.URL)
	if raw == "" {
		f.Err = locales.ErrPasteLink()
		return nil
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		f.Err = locales.ErrOnlyHTTP()
		return nil
	}

	name := f.FileName
	if name == "" && f.Info != nil {
		name = f.Info.FileName
	}
	if name == "" {
		name = engine.FileNameFromHeaders("", raw)
	}
	name = engine.SanitizeName(name)

	catID := f.Category
	if catID == "" {
		catID = model.CategoryFor(a.st.Categories, name)
	}
	dir := f.Dir
	if dir == "" {
		if c := a.category(catID); c != nil {
			dir = c.Folder
		} else {
			dir = a.st.Settings.DownloadDir
		}
	}

	// Duplicates.
	if old := a.duplicateOf(raw); old != nil {
		switch a.st.Settings.DuplicateAction {
		case "reuse":
			if old.Status == model.StatusComplete {
				a.toast = locales.ToastAlreadyDownloaded()
				return old
			}
			a.Resume(old.ID)
			return old
		case "overwrite":
			a.removeFiles(old)
			a.removeAt(indexOf(a.st.Downloads, old.ID))
		case "rename":
			// A new name below.
		default: // ask
			if !f.Duplicate {
				f.Duplicate = true
				f.Err = locales.ErrDuplicate()
				return nil
			}
		}
	}

	// Never write over a file the app did not make.
	if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
		name = engine.UniqueName(name, func(n string) bool {
			_, err := os.Stat(filepath.Join(dir, n))
			return err == nil
		})
	}

	d := &model.Download{
		ID:          model.NewID(),
		URL:         raw,
		FileName:    name,
		Dir:         dir,
		Category:    catID,
		Queue:       f.Queue,
		Description: f.Description,
		Status:      model.StatusQueued,
		Added:       time.Now(),
		Referer:     f.Referer,
		Cookies:     f.Cookies,
		UserAgent:   f.UserAgent,
		Checksum:    f.Checksum,
		Connections: f.Connections,
		Kind:        model.KindHTTP,
	}
	if f.Info != nil {
		d.Size = f.Info.Size
		d.Resumable = f.Info.Resumable
		d.ETag = f.Info.ETag
		d.LastModified = f.Info.LastModified
		d.FinalURL = f.Info.FinalURL
	}
	a.st.Downloads = append(a.st.Downloads, d)
	a.lastDone[d.ID] = 0
	a.markDirty()
	a.addOpen = false
	a.filter = "all"
	a.selected = len(a.visible()) - 1

	if f.StartNow {
		if d.Queue == "" {
			a.start(d)
		} else {
			a.pump()
		}
	} else if d.Queue != "" {
		d.Status = model.StatusWaiting
	}
	a.Save()
	return d
}

func (a *App) category(id string) *model.Category {
	for i := range a.st.Categories {
		if a.st.Categories[i].ID == id {
			return &a.st.Categories[i]
		}
	}
	return nil
}

func (a *App) queue(id string) *model.Queue {
	for i := range a.st.Queues {
		if a.st.Queues[i].ID == id {
			return &a.st.Queues[i]
		}
	}
	return nil
}

func (a *App) start(d *model.Download) {
	if d == nil || d.Status == model.StatusComplete {
		return
	}
	cfg := engine.Config{
		Connections:      a.st.Settings.Connections,
		PerDownloadLimit: a.st.Settings.PerDownloadLimit,
		Global:           a.global,
		UserAgent:        a.st.Settings.UserAgent,
		Proxy:            a.st.Settings.Proxy,
		BindIP:           a.st.Settings.BindAdapter,
		DNS:              a.st.Settings.DNS,
		MaxRetries:       a.st.Settings.MaxRetries,
		FileDate:         a.st.Settings.FileDateFromServer,
	}
	if d.Connections > 0 {
		cfg.Connections = d.Connections
	}
	if d.UserAgent != "" {
		cfg.UserAgent = d.UserAgent
	}
	if err := a.eng.Start(*d, cfg); err != nil {
		d.Status = model.StatusError
		d.Error = err.Error()
		a.markDirty()
		return
	}
	d.Error = ""
	d.Status = model.StatusConnecting
	a.markDirty()
}

// Pause pauses a download.
func (a *App) Pause(id string) {
	d := a.find(id)
	if d == nil || d.Status == model.StatusComplete || d.Status == model.StatusError {
		return
	}
	if d.Kind == model.KindTorrent {
		a.pauseTorrent(d)
		return
	}
	a.eng.Pause(id)
	d.Status = model.StatusPaused
	d.Speed = 0
	a.markDirty()
}

// Resume starts or continues a download.
func (a *App) Resume(id string) {
	d := a.find(id)
	if d == nil {
		return
	}
	if d.Status == model.StatusComplete {
		a.OpenFile(d)
		return
	}
	if d.Kind == model.KindTorrent {
		a.resumeTorrent(d)
		return
	}
	if a.eng.Running(d.ID) {
		// A pause is still finishing; its Paused event will let the user
		// start it again.
		return
	}
	if d.Queue != "" {
		q := a.queue(d.Queue)
		if q != nil && !a.queueRunning[q.ID] {
			a.startQueue(q.ID)
			return
		}
	}
	if a.st.Settings.LimitEnabled && a.limitReached() {
		d.Status = model.StatusWaiting
		d.Error = errLimit
		a.markDirty()
		return
	}
	d.Status = model.StatusQueued
	a.start(d)
}

// PauseAll pauses every running download.
func (a *App) PauseAll() {
	for _, d := range a.st.Downloads {
		if d.Status.Active() || d.Status == model.StatusQueued {
			a.eng.Pause(d.ID)
			d.Status = model.StatusPaused
			d.Speed = 0
		}
	}
	a.markDirty()
}

// ResumeAll resumes every paused download that is not in a stopped queue.
func (a *App) ResumeAll() {
	for _, d := range a.st.Downloads {
		if d.Status == model.StatusPaused || d.Status == model.StatusError {
			a.Resume(d.ID)
		}
	}
}

// Remove takes a download off the list, optionally deleting its files.
func (a *App) Remove(id string, deleteFile bool) {
	d := a.find(id)
	if d == nil {
		return
	}
	if d.Kind == model.KindTorrent {
		a.removeTorrent(d, deleteFile)
	} else {
		a.eng.Remove(id)
		if deleteFile {
			a.removeFiles(d)
		}
	}
	a.removeAt(indexOf(a.st.Downloads, id))
	a.markDirty()
	a.Save()
	a.pump()
}

func (a *App) removeFiles(d *model.Download) {
	_ = os.Remove(d.PartPath())
	_ = os.Remove(d.StatePath())
	if d.Status == model.StatusComplete {
		_ = os.Remove(d.Path())
	}
}

func (a *App) removeAt(i int) {
	if i < 0 || i >= len(a.st.Downloads) {
		return
	}
	a.st.Downloads = append(a.st.Downloads[:i], a.st.Downloads[i+1:]...)
	if a.selected >= len(a.st.Downloads) {
		a.selected = len(a.st.Downloads) - 1
	}
	if a.selected < 0 {
		a.selected = 0
	}
}

func indexOf(list []*model.Download, id string) int {
	for i, d := range list {
		if d.ID == id {
			return i
		}
	}
	return -1
}

// OpenFile opens a finished file with its program.
func (a *App) OpenFile(d *model.Download) {
	if d.Status != model.StatusComplete {
		return
	}
	// A torrent of several files is a folder, and opens as one.
	if d.Kind == model.KindTorrent {
		if info, err := os.Stat(d.Path()); err == nil && info.IsDir() {
			_ = mygo.Shell.OpenPath(d.Path())
			return
		}
	}
	if err := mygo.Shell.OpenPath(d.Path()); err != nil {
		a.toast = locales.ToastOpenFailed(err.Error())
	}
}

// ShowInFolder shows a finished file in the file manager.
func (a *App) ShowInFolder(d *model.Download) {
	if _, err := os.Stat(d.Path()); err == nil {
		mygo.Shell.ShowItemInFolder(d.Path())
		return
	}
	_ = mygo.Shell.OpenPath(d.Dir)
}

// CopyURL puts a download's address on the clipboard.
func (a *App) CopyURL(d *model.Download) {
	mygo.Clipboard.WriteText(d.URL)
	a.toast = locales.ToastLinkCopied()
}

// OpenFolder opens a download's folder.
func (a *App) OpenFolder(d *model.Download) {
	_ = mygo.Shell.OpenPath(d.Dir)
}

// Visible returns the downloads the current filter and search show,
// sorted the way the list is.
func (a *App) Visible() []*model.Download { return a.visible() }

func (a *App) visible() []*model.Download {
	var out []*model.Download
	q := strings.ToLower(strings.TrimSpace(a.query))
	for _, d := range a.st.Downloads {
		if !a.matchesFilter(d) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(d.FileName + " " + d.URL + " " + d.Description + " " + d.Category)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		var less bool
		switch a.sortCol {
		case 0:
			less = strings.ToLower(out[i].FileName) < strings.ToLower(out[j].FileName)
		case 1:
			less = out[i].Size < out[j].Size
		case 2:
			less = out[i].Percent() < out[j].Percent()
		case 3:
			less = out[i].Speed < out[j].Speed
		case 4:
			less = out[i].Status < out[j].Status
		case 5:
			less = out[i].Added.Before(out[j].Added)
		default:
			less = out[i].Added.Before(out[j].Added)
		}
		if a.sortDesc {
			return !less
		}
		return less
	})
	return out
}

func (a *App) matchesFilter(d *model.Download) bool {
	switch {
	case a.filter == "all" || a.filter == "":
		return true
	case a.filter == "unfinished":
		return d.Status != model.StatusComplete
	case a.filter == "finished":
		return d.Status == model.StatusComplete
	case strings.HasPrefix(a.filter, "cat:"):
		return d.Category == strings.TrimPrefix(a.filter, "cat:")
	case strings.HasPrefix(a.filter, "queue:"):
		return d.Queue == strings.TrimPrefix(a.filter, "queue:")
	}
	return true
}

// SelectedDownload returns the row under the list's selection, or nil.
func (a *App) SelectedDownload() *model.Download {
	list := a.visible()
	if a.selected < 0 || a.selected >= len(list) {
		return nil
	}
	return list[a.selected]
}

// TotalSpeed sums the running downloads' speeds.
func (a *App) TotalSpeed() float64 {
	var n float64
	for _, d := range a.st.Downloads {
		n += d.Speed
	}
	return n
}

// ActiveCount counts downloads that are running.
func (a *App) ActiveCount() int {
	n := 0
	for _, d := range a.st.Downloads {
		if d.Status.Active() {
			n++
		}
	}
	return n
}

// --- Queues ---

// StartQueue starts a queue and its downloads.
func (a *App) StartQueue(id string) {
	a.startQueue(id)
	a.pump()
}

func (a *App) startQueue(id string) {
	a.queueRunning[id] = true
	a.markDirty()
}

// StopQueue pauses a queue's downloads.
func (a *App) StopQueue(id string) {
	a.queueRunning[id] = false
	for _, d := range a.st.Downloads {
		if d.Queue == id && (d.Status.Active() || d.Status == model.StatusQueued) {
			a.eng.Pause(d.ID)
			d.Status = model.StatusPaused
			d.Speed = 0
		}
	}
	a.markDirty()
}

// QueueRunning reports whether a queue is running.
func (a *App) QueueRunning(id string) bool { return a.queueRunning[id] }

// pump starts queued downloads in every running queue, up to the queue's
// parallelism, and leaves the rest waiting.
func (a *App) pump() {
	if a.st.Settings.LimitEnabled && a.limitReached() {
		return
	}
	for _, q := range a.st.Queues {
		if !a.queueRunning[q.ID] {
			continue
		}
		active := 0
		for _, d := range a.st.Downloads {
			if d.Queue == q.ID && d.Status.Active() {
				active++
			}
		}
		parallel := q.Parallel
		if parallel < 1 {
			parallel = 4
		}
		for _, d := range a.st.Downloads {
			if active >= parallel {
				break
			}
			if d.Queue != q.ID {
				continue
			}
			if d.Status == model.StatusQueued || d.Status == model.StatusWaiting {
				a.start(d)
				active++
			}
		}
	}
	// Downloads outside a queue start at once.
	for _, d := range a.st.Downloads {
		if d.Queue == "" && d.Status == model.StatusQueued {
			a.start(d)
		}
	}
}

// --- Scheduler ---

func (a *App) tickLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		<-t.C
		win := a.win
		if win == nil {
			continue
		}
		win.Update(func() {
			a.tick()
		})
	}
}

func (a *App) tick() {
	now := time.Now()
	a.checkFeeds()

	// The data limit's window.
	if a.st.Settings.LimitEnabled {
		hours := a.st.Settings.LimitHours
		if hours < 1 {
			hours = 1
		}
		if a.st.LimitSince.IsZero() || now.Sub(a.st.LimitSince) >= time.Duration(hours)*time.Hour {
			a.st.LimitSince = now
			a.st.LimitUsed = 0
			for _, d := range a.st.Downloads {
				if d.Status == model.StatusWaiting && d.Error == errLimit {
					d.Status = model.StatusQueued
					d.Error = ""
				}
			}
			a.pump()
		}
	}
	a.checkLimit()

	for _, q := range a.st.Queues {
		if q.StartAt == "" {
			if q.OnStartup && now.Sub(a.started) < 10*time.Second && !a.queueRunning[q.ID] {
				a.startQueue(q.ID)
			}
			continue
		}
		if !dayMatches(q, now) {
			continue
		}
		if q.Once != "" && q.Once != now.Format("2006-01-02") {
			continue
		}
		if hhmm(now) == q.StartAt && !a.queueRunning[q.ID] {
			a.startQueue(q.ID)
		}
		if q.StopAt != "" && hhmm(now) == q.StopAt && a.queueRunning[q.ID] {
			a.StopQueue(q.ID)
		}
		// When everything in the queue is done, stop it and act.
		if a.queueRunning[q.ID] && q.StopAt == "" {
			if a.queueDone(q.ID) {
				a.queueRunning[q.ID] = false
				a.afterQueue(q)
			}
		}
	}
	a.saveIfDirty()
}

func (a *App) queueDone(id string) bool {
	found := false
	for _, d := range a.st.Downloads {
		if d.Queue != id {
			continue
		}
		found = true
		if d.Status != model.StatusComplete && d.Status != model.StatusError {
			return false
		}
	}
	return found
}

func (a *App) afterQueue(q model.Queue) {
	if q.OpenWhenDone != "" {
		_ = mygo.Shell.OpenPath(q.OpenWhenDone)
	}
	if q.ExitWhenDone {
		a.Quit()
		return
	}
	switch q.PowerWhenDone {
	case "shutdown":
		systemPower("shutdown")
	case "sleep":
		systemPower("sleep")
	case "hibernate":
		systemPower("hibernate")
	}
}

func (a *App) limitReached() bool {
	if !a.st.Settings.LimitEnabled || a.st.Settings.LimitBytes <= 0 {
		return false
	}
	return a.st.LimitUsed >= a.st.Settings.LimitBytes
}

func (a *App) checkLimit() {
	if !a.limitReached() {
		return
	}
	changed := false
	for _, d := range a.st.Downloads {
		if d.Status.Active() || d.Status == model.StatusQueued {
			a.eng.Pause(d.ID)
			d.Status = model.StatusWaiting
			d.Speed = 0
			d.Error = errLimit
			changed = true
		}
	}
	if changed {
		a.markDirty()
	}
}

func dayMatches(q model.Queue, now time.Time) bool {
	if len(q.Days) == 0 {
		return true
	}
	for _, d := range q.Days {
		if d == int(now.Weekday()) {
			return true
		}
	}
	return false
}

func hhmm(t time.Time) string { return t.Format("15:04") }

// --- Notifications ---

func (a *App) notifyComplete(d *model.Download) {
	if !a.st.Settings.NotifyOnComplete {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:    "done:" + d.ID,
		Title: locales.NotifyCompleteTitle(),
		Body:  d.FileName,
	})
	_ = n.Show()
}

func (a *App) notifyError(d *model.Download) {
	if !a.st.Settings.NotifyOnError {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:    "error:" + d.ID,
		Title: locales.NotifyFailedTitle(),
		Body:  d.FileName + " — " + a.errorText(d),
	})
	_ = n.Show()
}

// --- Clipboard ---

func (a *App) watchClipboard() {
	t := time.NewTicker(900 * time.Millisecond)
	defer t.Stop()
	for {
		<-t.C
		// The settings are the main thread's; the watcher reads the
		// mirror the view keeps for it.
		if !a.clipWatch.Load() {
			continue
		}
		text := strings.TrimSpace(mygo.Clipboard.ReadText())
		if text == "" {
			continue
		}
		if !looksLikeURL(text) {
			continue
		}
		if text == a.lastClip {
			continue
		}
		a.lastClip = text
		raw := text
		win := a.win
		if win == nil {
			continue
		}
		win.Update(func() {
			if a.addOpen {
				return
			}
			if a.st.Settings.ShowAddDialog {
				a.OpenAdd(raw)
			} else {
				a.addLink(raw)
			}
		})
	}
}

func looksLikeURL(s string) bool {
	if strings.ContainsAny(s, " \n\t") {
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// --- Lifecycle ---

// Quit stops the downloads and quits the app.
func (a *App) Quit() {
	if a.quitting {
		return
	}
	a.quitting = true
	a.eng.StopAll()
	a.Save()
	if a.api != nil {
		a.api.Close()
	}
	mygo.App.Quit()
}

// Shutdown is called when the window closes.
func (a *App) Shutdown() {
	a.eng.StopAll()
	a.closeTorrents()
	if a.api != nil {
		a.api.Close()
		a.api = nil
	}
	a.Save()
}

// Tick is what the view calls once per frame to keep time-based work
// moving even when no engine event arrives.
func (a *App) Tick() {
	a.saveIfDirty()
}

// Toast shows a short message.
func (a *App) Toast(msg string) { a.toast = msg }

// Quitting reports whether the app is on its way out.
func (a *App) Quitting() bool { return a.quitting }

// ShowAbout opens the About dialog.
func (a *App) ShowAbout() { a.aboutOpen = true }

// ShowOptions opens the Options window.
// ShowFeeds opens the subscriptions dialog.
func (a *App) ShowFeeds() { a.feedsOpen = true }

func (a *App) ShowOptions() { a.optionsOpen = true }

// ShowOptionsTab opens the Options dialog on one of its tabs.
func (a *App) ShowOptionsTab(tab int) {
	a.optionsOpen = true
	a.optionsTab = tab
}

// SetTheme changes the appearance setting.
func (a *App) SetTheme(v string) {
	a.st.Settings.Theme = v
	a.markDirty()
	a.Save()
}

// WatchClipboard starts watching the clipboard for links.
func (a *App) WatchClipboard() { a.watchClipboard() }

// PasteLinks adds every link on the clipboard.
// AddLink adds a link the way the settings ask: debrid, an external
// downloader, or the engine.
func (a *App) AddLink(raw string) *model.Download { return a.addLink(raw) }

func (a *App) PasteLinks() {
	for _, line := range strings.Split(mygo.Clipboard.ReadText(), "\n") {
		line = strings.TrimSpace(line)
		if looksLikeURL(line) || strings.HasPrefix(strings.ToLower(line), "magnet:") {
			a.addLink(line)
		}
	}
}

// Filter returns the current filter, for the view.
func (a *App) Filter() string { return a.filter }

// SetFilter changes the filter.
func (a *App) SetFilter(f string) {
	a.filter = f
	a.selected = 0
}

// Query returns the search text.
func (a *App) Query() string { return a.query }

// SetQuery changes the search text.
func (a *App) SetQuery(q string) {
	a.query = q
	a.selected = 0
}

// SortBy sorts the list by a column, reversing when it is already sorted.
func (a *App) SortBy(col int) {
	if a.sortCol == col {
		a.sortDesc = !a.sortDesc
	} else {
		a.sortCol = col
		a.sortDesc = false
	}
}

// SortState returns the current sort.
func (a *App) SortState() (int, bool) { return a.sortCol, a.sortDesc }

// SetSelection sets the selected row.
func (a *App) SetSelection(i int) { a.selected = i }

// Selection returns the selected row.
func (a *App) Selection() int { return a.selected }

// Categories returns the categories.
func (a *App) Categories() []model.Category { return a.st.Categories }

// Queues returns the queues.
func (a *App) Queues() []model.Queue { return a.st.Queues }

// AddCategory adds a category and returns it.
func (a *App) AddCategory(name, folder string, types []string) *model.Category {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if folder == "" {
		folder = filepath.Join(a.st.Settings.DownloadDir, name)
	}
	c := model.Category{ID: model.NewID(), Name: name, Folder: folder, Types: types}
	a.st.Categories = append(a.st.Categories, c)
	a.markDirty()
	a.Save()
	return &a.st.Categories[len(a.st.Categories)-1]
}

// RemoveCategory removes a category unless it is protected.
func (a *App) RemoveCategory(id string) {
	for i, c := range a.st.Categories {
		if c.ID != id || c.Protected {
			continue
		}
		a.st.Categories = append(a.st.Categories[:i], a.st.Categories[i+1:]...)
		for _, d := range a.st.Downloads {
			if d.Category == id {
				d.Category = "general"
			}
		}
		a.markDirty()
		a.Save()
		return
	}
}

// AddQueue adds a queue.
func (a *App) AddQueue(name string) *model.Queue {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	q := model.Queue{ID: model.NewID(), Name: name, Parallel: 4}
	a.st.Queues = append(a.st.Queues, q)
	a.markDirty()
	a.Save()
	return &a.st.Queues[len(a.st.Queues)-1]
}

// RemoveQueue removes a queue, moving its downloads to the main one.
func (a *App) RemoveQueue(id string) {
	for i, q := range a.st.Queues {
		if q.ID != id || q.Builtin {
			continue
		}
		a.st.Queues = append(a.st.Queues[:i], a.st.Queues[i+1:]...)
		for _, d := range a.st.Downloads {
			if d.Queue == id {
				d.Queue = ""
			}
		}
		delete(a.queueRunning, id)
		a.markDirty()
		a.Save()
		return
	}
}

// SetDownloadQueue moves a download to a queue.
func (a *App) SetDownloadQueue(id, queue string) {
	d := a.find(id)
	if d == nil {
		return
	}
	d.Queue = queue
	a.markDirty()
	a.pump()
}

// RevealWindow shows and focuses the main window.
func (a *App) RevealWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	a.win.Focus()
}

// AddFromArgs handles a link passed on the command line or by a deep link.
func (a *App) AddFromArgs(args []string) {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if torr := torrent.IsTorrent(arg); torr {
			a.addLink(arg)
			return
		}
		if looksLikeURL(arg) {
			a.OpenAdd(arg)
			return
		}
	}
}
