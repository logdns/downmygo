package torrent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// makeTorrent writes a torrent file for a file, and its magnet.
func makeTorrent(t *testing.T, dir, name string, body []byte) (torrentPath, magnet string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(path); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: infoBytes}
	torrentPath = filepath.Join(dir, "test.torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mi.Write(f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	m, err := MagnetOf(torrentPath)
	if err != nil {
		t.Fatal(err)
	}
	return torrentPath, m
}

func TestIsTorrent(t *testing.T) {
	yes := []string{"magnet:?xt=urn:btih:abc", "MAGNET:?xt=urn:btih:abc", "https://x/a.torrent", "/tmp/a.TORRENT"}
	no := []string{"https://x/a.zip", "magnet", "http://x/a.zip?f=a.torrent"}
	for _, s := range yes {
		if !IsTorrent(s) {
			t.Errorf("%q is not a torrent", s)
		}
	}
	for _, s := range no {
		if IsTorrent(s) {
			t.Errorf("%q is a torrent", s)
		}
	}
}

func TestLoopbackDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("a real torrent exchange")
	}
	body := []byte("hello from a torrent, over loopback")
	src := t.TempDir()
	torrentPath, magnet := makeTorrent(t, src, "hello.txt", body)

	// The seeder has the file and serves it.
	seeder, err := New(Config{
		DownloadDir: src, DataDir: t.TempDir(), Seed: true,
		DisableDHT: true, DisableTrackers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer seeder.Close()
	var seeded atomic.Int64
	if err := seeder.Add(context.Background(), "seed", torrentPath, func(p Progress) {
		if p.Uploaded > seeded.Load() {
			seeded.Store(p.Uploaded)
		}
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for seeder.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	// The leecher gets the magnet and the seeder's address.
	dst := t.TempDir()
	leecher, err := New(Config{
		DownloadDir: dst, DataDir: t.TempDir(), Seed: true,
		SeedTime: time.Minute, DisableDHT: true, DisableTrackers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer leecher.Close()

	var last Progress
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := leecher.Add(ctx, "get", magnet, func(p Progress) {
		last = p
		if p.Complete {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	leecher.AddPeers("get", seeder.Addr())

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("the download did not finish; last progress %+v", last)
	}
	if last.Name != "hello.txt" || last.Size != int64(len(body)) {
		t.Fatalf("progress = %+v", last)
	}
	if !last.Seeding {
		t.Fatalf("a finished torrent with Seed on is not seeding: %+v", last)
	}
	// The seeder shared the file, which is what seeding means.
	deadline = time.Now().Add(10 * time.Second)
	for seeded.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if seeded.Load() == 0 {
		t.Fatal("the seeder uploaded nothing")
	}
	got, err := os.ReadFile(filepath.Join(dst, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("got %q", got)
	}

	// Pause and resume do not lose what is on disk.
	leecher.Pause("get")
	leecher.Resume("get")
	if !leecher.Done("get") {
		t.Fatal("a finished torrent is not done")
	}
	leecher.Remove("get")
	if leecher.Name("get") != "" {
		t.Fatal("a removed torrent is still there")
	}
}

func TestAddRefusesWhatIsNotATorrent(t *testing.T) {
	e, err := New(Config{DownloadDir: t.TempDir(), DataDir: t.TempDir(), DisableDHT: true, DisableTrackers: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	path := filepath.Join(t.TempDir(), "not.torrent")
	if err := os.WriteFile(path, []byte("not a torrent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Add(context.Background(), "x", path, nil); err == nil {
		t.Fatal("a text file was taken as a torrent")
	}
	if err := e.Add(context.Background(), "y", "magnet:?xt=urn:btih:zzz", nil); err == nil {
		t.Fatal("a broken magnet was accepted")
	}
}

func TestZeroInfoHashIsRefused(t *testing.T) {
	e, err := New(Config{DownloadDir: t.TempDir(), DataDir: t.TempDir(), DisableDHT: true, DisableTrackers: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, uri := range []string{
		"magnet:?xt=urn:btih:0000000000000000000000000000000000000000&dn=x",
		"magnet:?dn=x",
		"magnet:?xt=urn:btih:zzz",
	} {
		if err := e.Add(context.Background(), "x", uri, nil); err == nil {
			t.Errorf("%q was accepted", uri)
		}
	}
}
