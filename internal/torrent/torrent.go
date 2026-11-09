// Package torrent downloads torrents and magnets: the app's own client,
// with DHT, trackers and peers, so a magnet needs no other program.
package torrent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

// Config is how the client behaves.
type Config struct {
	// DownloadDir is where the files land.
	DownloadDir string
	// DataDir keeps the torrent metadata and the resume data.
	DataDir string
	// ListenPort is the port peers reach; 0 picks one.
	ListenPort int
	// Seed keeps sharing after the download is done.
	Seed bool
	// SeedRatio stops seeding at this share ratio (uploaded / size); 0
	// keeps seeding until SeedTime.
	SeedRatio float64
	// SeedTime stops seeding after this long; 0 keeps seeding.
	SeedTime time.Duration
	// UploadLimit is the most this client uploads, in bytes per second; 0
	// is no limit.
	UploadLimit int64
	// DisableDHT turns the distributed hash table off, for a private
	// tracker or a test.
	DisableDHT bool
	// DisableTrackers asks only the peers it is given.
	DisableTrackers bool
}

// Progress is what a torrent is doing.
type Progress struct {
	Name    string
	Done    int64
	Size    int64
	Speed   float64
	Peers   int
	Percent float64
	Files   int
	// Uploaded is how much this torrent has shared, and Ratio is that
	// over its size.
	Uploaded    int64
	UploadSpeed float64
	Ratio       float64
	// Complete says every byte is on disk.
	Complete bool
	// Seeding says the download is done and the torrent is still sharing.
	Seeding bool
}

// Engine runs the torrents of one app.
type Engine struct {
	client *torrent.Client
	cfg    Config

	mu   sync.Mutex
	jobs map[string]*job
}

type job struct {
	torrent  *torrent.Torrent
	name     string
	lastDone int64
	lastAt   time.Time
	speed    float64
	paused   bool
	done     bool
	doneAt   time.Time
}

// New starts a client.
func New(cfg Config) (*Engine, error) {
	if cfg.DownloadDir == "" {
		return nil, errors.New("a torrent needs a folder to download into")
	}
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return nil, err
	}
	if cfg.DataDir != "" {
		if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
			return nil, err
		}
	}
	cc := torrent.NewDefaultClientConfig()
	cc.DataDir = cfg.DataDir
	cc.DefaultStorage = storage.NewFile(cfg.DownloadDir)
	cc.ListenPort = cfg.ListenPort
	cc.NoUpload = !cfg.Seed
	cc.Seed = cfg.Seed
	cc.NoDHT = cfg.DisableDHT
	cc.DisableTrackers = cfg.DisableTrackers
	if cfg.UploadLimit > 0 {
		// A burst of a quarter second keeps small pieces moving.
		burst := int(cfg.UploadLimit / 4)
		if burst < 16<<10 {
			burst = 16 << 10
		}
		cc.UploadRateLimiter = rate.NewLimiter(rate.Limit(cfg.UploadLimit), burst)
	}
	cc.DisableIPv6 = true
	cc.DisableTCP = false
	cc.DisableUTP = false
	client, err := torrent.NewClient(cc)
	if err != nil {
		return nil, err
	}
	return &Engine{client: client, cfg: cfg, jobs: map[string]*job{}}, nil
}

// Addr is the address peers reach this client on, for the tests and for
// the "peers" line of a torrent.
func (e *Engine) Addr() string {
	if e == nil || e.client == nil {
		return ""
	}
	for _, a := range e.client.ListenAddrs() {
		if strings.HasPrefix(a.Network(), "tcp") {
			return a.String()
		}
	}
	for _, a := range e.client.ListenAddrs() {
		return a.String()
	}
	return ""
}

// Close stops every torrent and the client.
func (e *Engine) Close() {
	if e == nil || e.client == nil {
		return
	}
	e.client.Close()
}

// Add starts a magnet, a .torrent file or a link to one, and reports its
// progress to onProgress until it is complete or removed.
func (e *Engine) Add(ctx context.Context, id, uri string, onProgress func(Progress)) error {
	t, err := e.open(ctx, uri)
	if err != nil {
		return err
	}
	j := &job{torrent: t, lastAt: time.Now()}
	e.mu.Lock()
	e.jobs[id] = j
	e.mu.Unlock()

	go e.watch(ctx, id, j, onProgress)
	return nil
}

// open turns the three ways of naming a torrent into a torrent.
func (e *Engine) open(ctx context.Context, uri string) (*torrent.Torrent, error) {
	uri = strings.TrimSpace(uri)
	switch {
	case strings.HasPrefix(strings.ToLower(uri), "magnet:"):
		// A magnet without an info hash, or with a zero one, is not a
		// torrent; the client panics on it, so it is refused here.
		m, err := metainfo.ParseMagnetUri(uri)
		if err != nil {
			return nil, fmt.Errorf("this is not a magnet link: %w", err)
		}
		if m.InfoHash.IsZero() {
			return nil, errors.New("the magnet has no info hash")
		}
		return e.client.AddMagnet(uri)
	case strings.HasPrefix(uri, "http://"), strings.HasPrefix(uri, "https://"):
		body, err := fetch(ctx, uri)
		if err != nil {
			return nil, err
		}
		mi, err := metainfo.Load(body)
		if err != nil {
			return nil, fmt.Errorf("this is not a torrent file: %w", err)
		}
		return e.client.AddTorrent(mi)
	default:
		mi, err := metainfo.LoadFromFile(uri)
		if err != nil {
			return nil, fmt.Errorf("this is not a torrent file: %w", err)
		}
		return e.client.AddTorrent(mi)
	}
}

func fetch(ctx context.Context, rawURL string) (io.Reader, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "downmygo")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the torrent file answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(body)), nil
}

// watch waits for the metadata, then reports the progress every second.
func (e *Engine) watch(ctx context.Context, id string, j *job, onProgress func(Progress)) {
	// A client that panics on a strange torrent must not take the app
	// with it.
	defer func() {
		if r := recover(); r != nil {
			if onProgress != nil {
				onProgress(Progress{Name: j.name, Complete: false})
			}
		}
	}()
	// A magnet has no name or size until its metadata arrives.
	select {
	case <-j.torrent.GotInfo():
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
		// No metadata came: say so, and forget the job.
		if onProgress != nil {
			onProgress(Progress{Name: j.torrent.InfoHash().String()})
		}
		e.mu.Lock()
		if e.jobs[id] == j {
			delete(e.jobs, id)
		}
		e.mu.Unlock()
		j.torrent.Drop()
		return
	}

	e.mu.Lock()
	j.name = j.torrent.Name()
	j.lastDone = j.torrent.BytesCompleted()
	j.lastAt = time.Now()
	e.mu.Unlock()
	j.torrent.DownloadAll()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		p := e.progress(j)
		if p.Complete && !j.done {
			e.mu.Lock()
			j.done = true
			j.doneAt = time.Now()
			e.mu.Unlock()
		}
		if p.Complete && e.cfg.Seed && !e.seedOver(j) {
			p.Seeding = true
		}
		if onProgress != nil {
			onProgress(p)
		}
		if p.Complete && (!e.cfg.Seed || e.seedOver(j)) {
			// Sharing stops here; the files stay.
			j.torrent.DisallowDataUpload()
			return
		}
	}
}

// seedOver says whether a finished torrent has shared enough.
func (e *Engine) seedOver(j *job) bool {
	if e.cfg.SeedTime > 0 && !j.doneAt.IsZero() && time.Since(j.doneAt) >= e.cfg.SeedTime {
		return true
	}
	if e.cfg.SeedRatio > 0 {
		size := j.torrent.Length()
		if size > 0 {
			st := j.torrent.Stats()
			uploaded := st.BytesWrittenData.Int64()
			if float64(uploaded)/float64(size) >= e.cfg.SeedRatio {
				return true
			}
		}
	}
	return false
}

func (e *Engine) progress(j *job) Progress {
	done := j.torrent.BytesCompleted()
	size := j.torrent.Length()
	stats := j.torrent.Stats()
	peers := stats.ActivePeers
	written := stats.BytesWrittenData
	uploaded := written.Int64()

	e.mu.Lock()
	now := time.Now()
	if elapsed := now.Sub(j.lastAt).Seconds(); elapsed > 0 {
		delta := float64(done - j.lastDone)
		// A little smoothing, so the number does not jump.
		j.speed = j.speed*0.6 + (delta/elapsed)*0.4
	}
	j.lastDone = done
	j.lastAt = now
	speed := j.speed
	e.mu.Unlock()

	p := Progress{
		Name: j.name, Done: done, Size: size, Speed: speed, Peers: peers,
		Files: len(j.torrent.Files()), Uploaded: uploaded,
	}
	if size > 0 {
		p.Percent = float64(done) / float64(size)
		if p.Percent > 1 {
			p.Percent = 1
		}
	}
	if size > 0 {
		p.Ratio = float64(uploaded) / float64(size)
	}
	p.Complete = size > 0 && done >= size
	return p
}

// Pause stops asking for pieces, keeping what is on disk.
func (e *Engine) Pause(id string) {
	if j := e.get(id); j != nil {
		j.torrent.DisallowDataDownload()
		j.torrent.DisallowDataUpload()
		e.mu.Lock()
		j.paused = true
		j.speed = 0
		e.mu.Unlock()
	}
}

// Resume asks for pieces again.
func (e *Engine) Resume(id string) {
	if j := e.get(id); j != nil {
		j.torrent.AllowDataDownload()
		if e.cfg.Seed {
			j.torrent.AllowDataUpload()
		}
		e.mu.Lock()
		j.paused = false
		j.lastAt = time.Now()
		j.lastDone = j.torrent.BytesCompleted()
		e.mu.Unlock()
	}
}

// Remove drops a torrent, leaving its files on disk.
func (e *Engine) Remove(id string) {
	e.mu.Lock()
	j := e.jobs[id]
	delete(e.jobs, id)
	e.mu.Unlock()
	if j != nil {
		j.torrent.Drop()
	}
}

// Name is the name a torrent writes, once its metadata is here.
func (e *Engine) Name(id string) string {
	if j := e.get(id); j != nil {
		return j.name
	}
	return ""
}

// Done says whether a torrent is complete.
func (e *Engine) Done(id string) bool {
	if j := e.get(id); j != nil {
		return j.done
	}
	return false
}

// Peers is how many peers a torrent has, for the status bar.
func (e *Engine) Peers(id string) int {
	if j := e.get(id); j != nil {
		return j.torrent.Stats().ActivePeers
	}
	return 0
}

func (e *Engine) get(id string) *job {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.jobs[id]
}

// IsTorrent says whether a link is one this engine takes.
func IsTorrent(uri string) bool {
	lower := strings.ToLower(strings.TrimSpace(uri))
	if strings.HasPrefix(lower, "magnet:") {
		return true
	}
	if i := strings.IndexAny(lower, "?#"); i >= 0 {
		lower = lower[:i]
	}
	return strings.HasSuffix(lower, ".torrent")
}

// MagnetOf makes a magnet for a torrent file, for the tests and for
// "copy magnet link".
func MagnetOf(path string) (string, error) {
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return "", err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", err
	}
	hash := mi.HashInfoBytes()
	return mi.Magnet(&hash, &info).String(), nil
}

// AddPeers is for tests: it gives a torrent the address of a peer.
func (e *Engine) AddPeers(id string, addresses ...string) {
	j := e.get(id)
	if j == nil {
		return
	}
	var infos []torrent.PeerInfo
	for _, a := range addresses {
		infos = append(infos, torrent.PeerInfo{Addr: peerAddr(a)})
	}
	j.torrent.AddPeers(infos)
}

// peerAddr is a peer's address, as the torrent package wants one.
type peerAddr string

func (p peerAddr) String() string { return string(p) }

// Files lists what a torrent writes.
func (e *Engine) Files(id string) []string {
	j := e.get(id)
	if j == nil {
		return nil
	}
	var out []string
	for _, f := range j.torrent.Files() {
		out = append(out, filepath.Join(e.cfg.DownloadDir, filepath.FromSlash(f.Path())))
	}
	return out
}
