// Package engine downloads one file over HTTP(S): it probes the server,
// splits the file into ranges, fetches them on several connections,
// resumes after a crash and reports progress. It knows nothing about the
// window; the app sees it only through events.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/downmygo/downmygo/internal/limiter"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/store"
)

// Event is a snapshot of a download, sent as it changes.
type Event struct {
	ID           string
	Status       model.Status
	Done         int64
	Size         int64
	Speed        float64
	Error        string
	Segments     []model.Segment
	FinalURL     string
	FileName     string
	ETag         string
	LastModified string
	Resumable    bool
	Connections  int
	Completed    bool
}

// Config is how a download runs. The app passes the user's settings.
type Config struct {
	Connections      int
	PerDownloadLimit int64
	Global           *limiter.Limiter
	UserAgent        string
	Proxy            string
	BindIP           string
	MaxRetries       int
	FileDate         bool
}

// Manager owns the running downloads.
type Manager struct {
	mu      sync.Mutex
	jobs    map[string]*job
	OnEvent func(Event)
}

// NewManager makes a manager with no downloads running.
func NewManager() *Manager {
	return &Manager{jobs: make(map[string]*job)}
}

// Start begins a download. It returns at once; progress comes as events.
func (m *Manager) Start(d model.Download, cfg Config) error {
	m.mu.Lock()
	if old, ok := m.jobs[d.ID]; ok {
		select {
		case <-old.doneCh:
			delete(m.jobs, d.ID)
		default:
			m.mu.Unlock()
			return errors.New("already downloading")
		}
	}
	if cfg.Connections < 1 {
		cfg.Connections = 8
	}
	if cfg.Connections > 32 {
		cfg.Connections = 32
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{
		m:      m,
		d:      d,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		client: NewClient(RequestOptions{
			UserAgent: cfg.UserAgent,
			Referer:   d.Referer,
			Cookies:   d.Cookies,
			Proxy:     cfg.Proxy,
			BindIP:    cfg.BindIP,
			MaxConns:  cfg.Connections,
		}),
		maxConn: cfg.Connections,
		doneCh:  make(chan struct{}),
	}
	if d.Connections > 0 {
		j.maxConn = min(max(d.Connections, 1), 32)
	}
	j.limiter = limiter.New(cfg.PerDownloadLimit)
	m.jobs[d.ID] = j
	m.mu.Unlock()

	j.emit(model.StatusConnecting)
	go j.run()
	return nil
}

// Pause stops a download and keeps what it has for a resume.
func (m *Manager) Pause(id string) {
	if j := m.get(id); j != nil {
		j.pause()
	}
}

// Cancel stops a download, keeping its partial file.
func (m *Manager) Cancel(id string) {
	if j := m.get(id); j != nil {
		j.pause()
	}
}

// Remove stops a download and deletes its partial file and resume state.
func (m *Manager) Remove(id string) {
	j := m.get(id)
	if j == nil {
		return
	}
	j.pause()
	go func() {
		select {
		case <-j.doneCh:
		case <-time.After(5 * time.Second):
		}
		_ = os.Remove(j.d.PartPath())
		_ = os.Remove(j.d.StatePath())
	}()
}

// Running reports whether a download is being worked on.
func (m *Manager) Running(id string) bool {
	return m.get(id) != nil
}

// StopAll pauses every running download, as quitting does.
func (m *Manager) StopAll() {
	m.mu.Lock()
	jobs := make([]*job, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		j.pause()
	}
}

// SetLimits updates the speed ceilings of running downloads.
func (m *Manager) SetLimits(perDownload int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		j.limiter.SetRate(perDownload)
	}
}

func (m *Manager) get(id string) *job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}

func (m *Manager) finish(id string) {
	m.mu.Lock()
	delete(m.jobs, id)
	m.mu.Unlock()
}

type job struct {
	m       *Manager
	d       model.Download
	cfg     Config
	ctx     context.Context
	cancel  context.CancelFunc
	client  *http.Client
	limiter *limiter.Limiter
	doneCh  chan struct{}

	mu       sync.Mutex
	segs     []model.Segment
	active   []bool
	attempts []int
	done     int64
	size     int64
	speed    float64
	status   model.Status
	fatal    error
	paused   bool
	maxConn  int
	file     *os.File
	lastSave time.Time
}

func (j *job) pause() {
	j.mu.Lock()
	j.paused = true
	j.mu.Unlock()
	j.cancel()
}

func (j *job) emit(s model.Status) {
	j.mu.Lock()
	j.status = s
	ev := Event{
		ID:           j.d.ID,
		Status:       s,
		Done:         j.done,
		Size:         j.size,
		Speed:        j.speed,
		Segments:     append([]model.Segment(nil), j.segs...),
		FinalURL:     j.d.FinalURL,
		FileName:     j.d.FileName,
		ETag:         j.d.ETag,
		LastModified: j.d.LastModified,
		Resumable:    j.d.Resumable,
		Connections:  j.maxConn,
	}
	if j.fatal != nil {
		ev.Error = j.fatal.Error()
	}
	j.mu.Unlock()
	if j.m != nil && j.m.OnEvent != nil {
		j.m.OnEvent(ev)
	}
}

func (j *job) fail(err error) {
	j.mu.Lock()
	if j.fatal == nil {
		j.fatal = err
	}
	j.mu.Unlock()
	j.cancel()
}

func (j *job) run() {
	defer close(j.doneCh)
	defer j.m.finish(j.d.ID)
	defer j.cancel()

	if err := os.MkdirAll(j.d.Dir, 0o755); err != nil {
		j.fail(fmt.Errorf("cannot create %s: %w", j.d.Dir, err))
		j.emit(model.StatusError)
		return
	}

	if j.d.Size <= 0 || j.d.FileName == "" || j.d.FinalURL == "" {
		info, err := Probe(j.ctx, j.client, j.d.URL, RequestOptions{
			UserAgent: j.cfg.UserAgent, Referer: j.d.Referer,
			Cookies: j.d.Cookies, Proxy: j.cfg.Proxy, BindIP: j.cfg.BindIP,
			MaxConns: j.maxConn,
		})
		if err != nil {
			if j.ctx.Err() != nil {
				j.finishPaused()
				return
			}
			j.fail(err)
			j.emit(model.StatusError)
			return
		}
		j.mu.Lock()
		j.d.FinalURL = info.FinalURL
		j.d.ETag = info.ETag
		j.d.LastModified = info.LastModified
		j.d.Resumable = info.Resumable
		j.size = info.Size
		if j.d.FileName == "" || j.d.FileName == "download" {
			j.d.FileName = info.FileName
		}
		j.mu.Unlock()
		j.emit(model.StatusConnecting)
	}

	j.mu.Lock()
	size := j.size
	if size == 0 {
		size = j.d.Size
		j.size = size
	}
	resumable := j.d.Resumable
	j.mu.Unlock()

	if size > 0 && resumable {
		j.runRanges()
		return
	}
	j.runSingle()
}

func (j *job) finishPaused() {
	j.mu.Lock()
	j.speed = 0
	j.mu.Unlock()
	j.saveState()
	j.emit(model.StatusPaused)
}

func (j *job) runRanges() {
	if err := j.openPart(); err != nil {
		j.fail(err)
		j.emit(model.StatusError)
		return
	}
	defer func() {
		if j.file != nil {
			_ = j.file.Close()
			j.file = nil
		}
	}()

	j.loadOrPlan()

	j.mu.Lock()
	workers := min(j.maxConn, len(j.segs))
	if workers < 1 {
		workers = 1
	}
	j.mu.Unlock()

	stop := make(chan struct{})
	var ticker sync.WaitGroup
	ticker.Add(1)
	go func() {
		defer ticker.Done()
		j.progressLoop(stop)
	}()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.worker()
		}()
	}
	wg.Wait()
	close(stop)
	ticker.Wait()

	j.mu.Lock()
	fatal := j.fatal
	paused := j.paused
	done := j.done
	size := j.size
	j.mu.Unlock()

	if fatal != nil {
		j.saveState()
		j.emit(model.StatusError)
		return
	}
	if paused || j.ctx.Err() != nil {
		j.finishPaused()
		return
	}
	if done < size {
		j.saveState()
		j.fail(fmt.Errorf("the download stopped at %d of %d bytes", done, size))
		j.emit(model.StatusError)
		return
	}

	if err := j.finishFile(); err != nil {
		j.fail(err)
		j.emit(model.StatusError)
		return
	}
	j.mu.Lock()
	j.done = size
	j.speed = 0
	j.segs = nil
	j.mu.Unlock()
	j.emit(model.StatusComplete)
}

func (j *job) openPart() error {
	f, err := os.OpenFile(j.d.PartPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", j.d.PartPath(), err)
	}
	j.file = f
	if j.size > 0 {
		if err := f.Truncate(j.size); err != nil {
			// A sparse file is an optimisation; a full disk is not.
			return fmt.Errorf("cannot make room for the file: %w", err)
		}
	}
	return nil
}

// loadOrPlan resumes a saved segment table when it still matches the
// server's file, and starts a fresh plan otherwise.
func (j *job) loadOrPlan() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if st, err := readState(j.d.StatePath()); err == nil && st.matches(j.d, j.size) {
		j.segs = st.Segments
		j.active = make([]bool, len(j.segs))
		j.attempts = make([]int, len(j.segs))
		j.done = TotalDone(j.segs)
		return
	}
	j.segs = InitialPlan(j.size, j.maxConn)
	j.active = make([]bool, len(j.segs))
	j.attempts = make([]int, len(j.segs))
	j.done = 0
}

func (j *job) worker() {
	for {
		j.mu.Lock()
		if j.ctx.Err() != nil || j.fatal != nil {
			j.mu.Unlock()
			return
		}
		maxConn := j.maxConn
		idx := ClaimInto(&j.segs, &j.active, maxConn, MinSplit)
		j.mu.Unlock()

		switch idx {
		case -1:
			return
		case -2:
			select {
			case <-j.ctx.Done():
				return
			case <-time.After(25 * time.Millisecond):
			}
			continue
		}

		err := j.fetch(idx)

		j.mu.Lock()
		if idx < len(j.active) {
			j.active[idx] = false
		}
		attempt := 0
		if idx < len(j.attempts) {
			attempt = j.attempts[idx]
		}
		j.mu.Unlock()

		if err == nil {
			continue
		}
		if j.ctx.Err() != nil {
			return
		}
		if !retryable(err) {
			j.fail(err)
			return
		}
		if attempt >= j.cfg.MaxRetries {
			j.fail(fmt.Errorf("gave up after %d tries: %w", attempt+1, err))
			return
		}
		j.mu.Lock()
		if idx < len(j.attempts) {
			j.attempts[idx]++
		}
		j.mu.Unlock()
		wait := time.Duration(attempt+1) * 700 * time.Millisecond
		if wait > 8*time.Second {
			wait = 8 * time.Second
		}
		select {
		case <-j.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// stallTimeout is how long a connection may send nothing before it is
// dropped, so a server that goes quiet retries instead of hanging.
const stallTimeout = 90 * time.Second

type retryError struct{ err error }

func (r retryError) Error() string { return r.err.Error() }
func (r retryError) Unwrap() error { return r.err }

func retryable(err error) bool {
	var r retryError
	return errors.As(err, &r) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func (j *job) fetch(idx int) error {
	j.mu.Lock()
	if idx >= len(j.segs) {
		j.mu.Unlock()
		return nil
	}
	seg := j.segs[idx]
	start := seg.Start + seg.Done
	end := seg.End
	size := j.size
	j.mu.Unlock()
	if start > end {
		return nil
	}

	req, err := http.NewRequestWithContext(j.ctx, http.MethodGet, j.d.URL, nil)
	if err != nil {
		return err
	}
	setHeaders(req, RequestOptions{
		UserAgent: j.cfg.UserAgent, Referer: j.d.Referer, Cookies: j.d.Cookies,
	})
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := j.client.Do(req)
	if err != nil {
		if j.ctx.Err() != nil {
			return j.ctx.Err()
		}
		return retryError{friendlyNetError(err)}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		// The server ignored the range. If this is the whole file, one
		// stream is still correct; otherwise the plan cannot be trusted.
		if !(start == 0 && end == size-1) {
			return retryError{errors.New("the server stopped accepting range requests")}
		}
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		j.mu.Lock()
		if j.maxConn > 1 {
			j.maxConn /= 2
		}
		j.mu.Unlock()
		return retryError{fmt.Errorf("the server asked to slow down (%s)", resp.Status)}
	case http.StatusRequestedRangeNotSatisfiable:
		// Already complete, or the file changed size.
		j.mu.Lock()
		if idx < len(j.segs) {
			j.segs[idx].Done = j.segs[idx].Len()
		}
		j.mu.Unlock()
		return nil
	default:
		if resp.StatusCode >= 500 {
			return retryError{fmt.Errorf("the server answered %s", resp.Status)}
		}
		return fmt.Errorf("the server answered %s", resp.Status)
	}

	// A server that goes quiet would hold this connection for ever: close
	// the body when nothing arrives, and the read fails into a retry.
	stall := time.AfterFunc(stallTimeout, func() { _ = resp.Body.Close() })
	defer stall.Stop()

	buf := make([]byte, 64<<10)
	written := int64(0)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			if err := j.limiter.WaitN(j.ctx, n); err != nil {
				return err
			}
			if _, werr := j.file.WriteAt(buf[:n], start+written); werr != nil {
				return fmt.Errorf("cannot write to disk: %w", werr)
			}
			written += int64(n)
			j.mu.Lock()
			if idx < len(j.segs) {
				j.segs[idx].Done += int64(n)
			}
			j.done += int64(n)
			j.mu.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if j.ctx.Err() != nil {
				return j.ctx.Err()
			}
			return retryError{rerr}
		}
	}
	if written < end-start+1 {
		return retryError{io.ErrUnexpectedEOF}
	}
	return nil
}

func (j *job) runSingle() {
	_ = os.Remove(j.d.PartPath())
	f, err := os.OpenFile(j.d.PartPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		j.fail(fmt.Errorf("cannot write %s: %w", j.d.PartPath(), err))
		j.emit(model.StatusError)
		return
	}
	j.file = f
	defer func() {
		if j.file != nil {
			_ = j.file.Close()
			j.file = nil
		}
	}()

	req, err := http.NewRequestWithContext(j.ctx, http.MethodGet, j.d.URL, nil)
	if err != nil {
		j.fail(err)
		j.emit(model.StatusError)
		return
	}
	setHeaders(req, RequestOptions{
		UserAgent: j.cfg.UserAgent, Referer: j.d.Referer, Cookies: j.d.Cookies,
	})
	resp, err := j.client.Do(req)
	if err != nil {
		if j.ctx.Err() != nil {
			j.finishPaused()
			return
		}
		j.fail(friendlyNetError(err))
		j.emit(model.StatusError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		j.fail(fmt.Errorf("the server answered %s", resp.Status))
		j.emit(model.StatusError)
		return
	}

	j.mu.Lock()
	if j.size <= 0 {
		j.size = resp.ContentLength
	}
	j.mu.Unlock()

	stop := make(chan struct{})
	var ticker sync.WaitGroup
	ticker.Add(1)
	go func() {
		defer ticker.Done()
		j.progressLoop(stop)
	}()

	stall := time.AfterFunc(stallTimeout, func() { _ = resp.Body.Close() })
	defer stall.Stop()

	buf := make([]byte, 128<<10)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			if err := j.limiter.WaitN(j.ctx, n); err != nil {
				break
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				j.fail(fmt.Errorf("cannot write to disk: %w", werr))
				break
			}
			written += int64(n)
			j.mu.Lock()
			j.done = written
			j.mu.Unlock()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if j.ctx.Err() == nil {
				j.fail(friendlyNetError(rerr))
			}
			break
		}
	}
	close(stop)
	ticker.Wait()

	j.mu.Lock()
	fatal := j.fatal
	paused := j.paused
	size := j.size
	j.mu.Unlock()

	if fatal != nil {
		j.emit(model.StatusError)
		return
	}
	if paused || j.ctx.Err() != nil {
		j.finishPaused()
		return
	}
	if size > 0 && written != size {
		j.fail(fmt.Errorf("the download stopped at %d of %d bytes", written, size))
		j.emit(model.StatusError)
		return
	}
	if size <= 0 {
		j.mu.Lock()
		j.size = written
		j.mu.Unlock()
	}
	if err := j.finishFile(); err != nil {
		j.fail(err)
		j.emit(model.StatusError)
		return
	}
	j.mu.Lock()
	j.speed = 0
	j.mu.Unlock()
	j.emit(model.StatusComplete)
}

func (j *job) progressLoop(stop <-chan struct{}) {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	var last int64
	lastAt := time.Now()
	for {
		select {
		case <-stop:
			return
		case <-j.ctx.Done():
			return
		case now := <-t.C:
			j.mu.Lock()
			done := j.done
			elapsed := now.Sub(lastAt).Seconds()
			if elapsed > 0 {
				inst := float64(done-last) / elapsed
				// Smooth the reading so the number does not flicker.
				if j.speed == 0 {
					j.speed = inst
				} else {
					j.speed = j.speed*0.65 + inst*0.35
				}
			}
			last = done
			lastAt = now
			saveDue := time.Since(j.lastSave) > 2*time.Second
			j.mu.Unlock()
			j.emit(model.StatusDownloading)
			if saveDue {
				j.saveState()
			}
		}
	}
}

// finishFile verifies the file and moves it into place.
func (j *job) finishFile() error {
	if j.file != nil {
		if err := j.file.Sync(); err != nil {
			return fmt.Errorf("cannot flush the file: %w", err)
		}
		if err := j.file.Close(); err != nil {
			return err
		}
		j.file = nil
	}
	part := j.d.PartPath()
	if j.d.Checksum != "" {
		if err := verifyChecksum(part, j.d.Checksum); err != nil {
			return err
		}
	}
	final := j.d.Path()
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return fmt.Errorf("cannot move the file into place: %w", err)
	}
	_ = os.Remove(j.d.StatePath())
	if j.cfg.FileDate && j.d.LastModified != "" {
		if t, err := http.ParseTime(j.d.LastModified); err == nil {
			_ = os.Chtimes(final, t, t)
		}
	}
	return nil
}

func verifyChecksum(path, want string) error {
	want = strings.TrimSpace(want)
	algo := "sha256"
	if i := strings.IndexByte(want, ':'); i >= 0 {
		algo = strings.ToLower(want[:i])
		want = want[i+1:]
	}
	want = strings.ToLower(strings.TrimSpace(want))
	if algo != "sha256" {
		return fmt.Errorf("checksum %q is not supported; use sha256:<hex>", algo)
	}
	if _, err := hex.DecodeString(want); err != nil || len(want) != 64 {
		return errors.New("the checksum is not a sha256 hex digest")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("the file's checksum is %s, not %s", got, want)
	}
	return nil
}

type resumeState struct {
	URL          string          `json:"url"`
	Size         int64           `json:"size"`
	ETag         string          `json:"etag,omitempty"`
	LastModified string          `json:"lastModified,omitempty"`
	Segments     []model.Segment `json:"segments"`
}

func (s resumeState) matches(d model.Download, size int64) bool {
	if s.URL != d.URL || s.Size != size || len(s.Segments) == 0 {
		return false
	}
	if d.ETag != "" && s.ETag != "" && d.ETag != s.ETag {
		return false
	}
	if d.LastModified != "" && s.LastModified != "" && d.LastModified != s.LastModified {
		return false
	}
	for _, seg := range s.Segments {
		if seg.Start < 0 || seg.End < seg.Start || seg.End >= size || seg.Done < 0 || seg.Done > seg.Len() {
			return false
		}
	}
	return true
}

func readState(path string) (resumeState, error) {
	var st resumeState
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(data, &st)
	return st, err
}

func (j *job) saveState() {
	j.mu.Lock()
	if len(j.segs) == 0 || j.size <= 0 {
		j.mu.Unlock()
		return
	}
	st := resumeState{
		URL:          j.d.URL,
		Size:         j.size,
		ETag:         j.d.ETag,
		LastModified: j.d.LastModified,
		Segments:     append([]model.Segment(nil), j.segs...),
	}
	j.lastSave = time.Now()
	j.mu.Unlock()

	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = store.WriteFileAtomic(j.d.StatePath(), data, 0o644)
}

// FormatBytes writes a size the way the list does.
func FormatBytes(n int64) string {
	if n < 0 {
		return "—"
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	decimals := 1
	if value >= 100 {
		decimals = 0
	}
	return strconv.FormatFloat(value, 'f', decimals, 64) + " " + string("KMGTPE"[exp]) + "B"
}

// FormatSpeed writes bytes per second.
func FormatSpeed(n float64) string {
	if n <= 0 {
		return "—"
	}
	return FormatBytes(int64(n)) + "/s"
}

// FormatETA writes a duration as the list does.
func FormatETA(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
