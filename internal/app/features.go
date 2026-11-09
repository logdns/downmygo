package app

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/downmygo/downmygo/internal/debrid"
	"github.com/downmygo/downmygo/internal/extract"
	"github.com/downmygo/downmygo/internal/grab"
	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/nativehost"
	"github.com/downmygo/downmygo/internal/rss"
	"github.com/downmygo/downmygo/internal/store"
	"github.com/downmygo/downmygo/internal/themes"
	"github.com/downmygo/downmygo/internal/tools"
	"github.com/downmygo/downmygo/internal/update"
	"github.com/downmygo/downmygo/locales"
)

// premiumHosts are the sites a debrid service can unrestrict. A link from
// one of them is resolved before it is added.
var premiumHosts = []string{
	"rapidgator", "1fichier", "uptobox", "turbobit", "nitroflare", "filefactory",
	"mediafire", "mega.nz", "uploaded", "ddownload", "katfile", "fikper",
	"hitfile", "alfafile", "filefox", "rapidfiles", "keep2share", "tezfiles",
}

// addLink adds a link the way the settings ask: through a debrid service,
// through yt-dlp or gallery-dl, or through the engine. A magnet or a
// torrent file goes to the system's torrent client, which this app does
// not carry.
func (a *App) addLink(raw string) *model.Download {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "magnet:") || strings.HasSuffix(lower, ".torrent") {
		a.handOff(raw)
		return nil
	}
	if u, err := url.Parse(raw); err == nil {
		host := strings.ToLower(u.Hostname())
		if a.st.Settings.DebridProvider != "" && a.st.Settings.DebridToken != "" && premium(host) {
			a.resolveDebrid(raw)
			return nil
		}
		if a.st.Settings.UseYtDlp && tools.Available(tools.YtDlp) && videoSite(host) {
			return a.startTool(tools.YtDlp, raw)
		}
		if a.st.Settings.UseGalleryDL && tools.Available(tools.GalleryDL) && gallerySite(host) {
			return a.startTool(tools.GalleryDL, raw)
		}
	}
	return a.AddURL(raw)
}

func premium(host string) bool {
	for _, p := range premiumHosts {
		if strings.Contains(host, p) {
			return true
		}
	}
	return false
}

// videoSite lists the sites yt-dlp knows better than an HTTP client.
func videoSite(host string) bool {
	for _, s := range []string{
		"youtube.com", "youtu.be", "vimeo.com", "dailymotion.com", "bilibili.com",
		"twitch.tv", "tiktok.com", "twitter.com", "x.com", "facebook.com",
		"instagram.com", "soundcloud.com", "nicovideo.jp", "youku.com",
	} {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// gallerySite lists the sites gallery-dl knows better.
func gallerySite(host string) bool {
	for _, s := range []string{
		"pixiv.net", "deviantart.com", "artstation.com", "flickr.com",
		"imgur.com", "danbooru.donmai.us", "gelbooru.com", "reddit.com",
		"tumblr.com", "weibo.com", "twitter.com", "instagram.com",
	} {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// handOff gives a magnet or a torrent file to the system's torrent client.
func (a *App) handOff(raw string) {
	var err error
	if strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		err = mygo.Shell.OpenExternal(raw)
	} else {
		err = mygo.Shell.OpenPath(raw)
	}
	if err != nil {
		a.toast = locales.ToastNoTorrentClient()
		return
	}
	a.toast = locales.ToastHandedOff()
}

// resolveDebrid turns a hosted link into a direct one, then adds it.
func (a *App) resolveDebrid(raw string) {
	provider := debrid.Provider(a.st.Settings.DebridProvider)
	token := a.st.Settings.DebridToken
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		link, err := debrid.Resolve(ctx, provider, token, raw)
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			if err != nil {
				a.toast = locales.ToastDebridFailed(err.Error())
				a.AddURL(raw)
				return
			}
			d := a.AddURL(link.URL)
			if d != nil {
				if link.FileName != "" {
					d.FileName = link.FileName
				}
				if link.Size > 0 {
					d.Size = link.Size
				}
				d.Description = raw
				a.toast = locales.ToastDebridResolved()
				a.markDirty()
			}
		})
	}()
}

// startTool runs yt-dlp or gallery-dl for a link, as a download of its
// own so it shows in the list with its progress.
func (a *App) startTool(kind tools.Kind, raw string) *model.Download {
	dir := a.st.Settings.DownloadDir
	name := grab.NameOf(mustURL(raw))
	if name == "" || name == "/" {
		name = string(kind)
	}
	kindValue := model.KindVideo
	if kind == tools.GalleryDL {
		kindValue = model.KindGallery
	}
	d := &model.Download{
		ID: model.NewID(), URL: raw, FileName: name, Dir: dir,
		Status: model.StatusConnecting, Kind: kindValue, Added: time.Now(),
		Connections: 1, Description: string(kind),
	}
	a.st.Downloads = append(a.st.Downloads, d)
	a.markDirty()

	ctx, cancel := context.WithCancel(context.Background())
	if a.toolCancels == nil {
		a.toolCancels = map[string]context.CancelFunc{}
	}
	a.toolCancels[d.ID] = cancel
	opts := tools.Options{
		Dir: dir, Proxy: a.st.Settings.Proxy, UserAgent: a.st.Settings.UserAgent,
	}
	if site, ok := nativehost.LoginFor(a.st.Sites, mustURL(raw).Hostname()); ok {
		opts.Proxy = a.st.Settings.Proxy
		if site.UserAgent != "" {
			opts.UserAgent = site.UserAgent
		}
	}
	go func() {
		files, err := tools.Run(ctx, kind, raw, opts, func(p tools.Progress) {
			win := a.win
			if win == nil {
				return
			}
			win.Update(func() {
				if d.Status != model.StatusDownloading {
					d.Status = model.StatusDownloading
				}
				d.Speed = p.Speed
				if p.Size > 0 {
					d.Size = p.Size
				}
				if p.Done > 0 {
					d.Done = p.Done
				}
			})
		})
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			delete(a.toolCancels, d.ID)
			d.Speed = 0
			if err != nil {
				d.Status = model.StatusError
				d.Error = err.Error()
				a.notifyError(d)
			} else {
				d.Status = model.StatusComplete
				d.Completed = time.Now()
				if len(files) > 0 {
					d.FileName = filepath.Base(files[0])
					if info, err := os.Stat(files[0]); err == nil {
						d.Size = info.Size()
						d.Done = info.Size()
					}
				}
				a.notifyComplete(d)
			}
			a.markDirty()
			a.Save()
			a.pump()
		})
	}()
	return d
}

// extractDone unpacks an archive a download finished with, when the
// settings ask for it.
func (a *App) extractDone(d *model.Download) {
	if !a.st.Settings.AutoExtract || d.Status != model.StatusComplete {
		return
	}
	if !extract.Supported(d.FileName) {
		return
	}
	path := d.Path()
	deleteArchive := a.st.Settings.DeleteArchive
	go func() {
		res, err := extract.Extract(path, extract.Options{})
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			if err != nil {
				a.toast = locales.ToastExtractFailed(err.Error())
				return
			}
			if deleteArchive {
				_ = os.Remove(path)
			}
			a.toast = locales.ToastExtracted(res.Files, res.Dir)
		})
	}()
}

// checkFeeds asks the feeds that are due and adds what they hold.
func (a *App) checkFeeds() {
	now := time.Now()
	for i := range a.st.Subscriptions {
		sub := &a.st.Subscriptions[i]
		if !sub.Enabled {
			continue
		}
		every := time.Duration(sub.EveryMin) * time.Minute
		if every <= 0 {
			every = time.Hour
		}
		if !sub.LastCheck.IsZero() && now.Sub(sub.LastCheck) < every {
			continue
		}
		sub.LastCheck = now
		url := sub.URL
		ext := append([]string(nil), sub.Extensions...)
		folder := sub.Folder
		id := sub.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			client := &http.Client{Timeout: 45 * time.Second}
			feed, err := rss.Fetch(ctx, client, url, a.st.Settings.UserAgent)
			win := a.win
			if win == nil {
				return
			}
			win.Update(func() {
				s := a.subscription(id)
				if s == nil {
					return
				}
				if err != nil {
					s.Error = err.Error()
					a.markDirty()
					return
				}
				if s.Name == "" {
					s.Name = feed.Title
				}
				s.Error = ""
				seen := map[string]bool{}
				for _, v := range s.Seen {
					seen[v] = true
				}
				added := 0
				for _, item := range feed.Items {
					link, ok := rss.Want(item, ext)
					if !ok || seen[item.ID] {
						continue
					}
					seen[item.ID] = true
					s.Seen = append(s.Seen, item.ID)
					a.add = AddForm{
						URL: link, Dir: folder, Category: "general",
						Connections: a.st.Settings.Connections,
						UserAgent:   a.st.Settings.UserAgent,
						StartNow:    true, Duplicate: true,
					}
					if item.Title != "" {
						a.add.Description = item.Title
					}
					if d := a.commitAdd(); d != nil {
						added++
					}
				}
				// Keep the list of seen ids short.
				if len(s.Seen) > 500 {
					s.Seen = s.Seen[len(s.Seen)-500:]
				}
				if added > 0 {
					a.toast = locales.ToastFeedAdded(added, feed.Title)
				}
				a.markDirty()
			})
		}()
	}
}

// storeSubscription makes a subscription from what the dialog holds.
func storeSubscription(rawURL, types string, everyMin int, folder string) store.Subscription {
	return store.Subscription{
		ID: model.NewID(), URL: rawURL, Enabled: true, EveryMin: everyMin,
		Extensions: grab.ParseExtensions(types), Folder: folder,
	}
}

func (a *App) subscription(id string) *store.Subscription {
	for i := range a.st.Subscriptions {
		if a.st.Subscriptions[i].ID == id {
			return &a.st.Subscriptions[i]
		}
	}
	return nil
}

// Grab fetches a page and keeps the links a filter wants.
func (a *App) Grab(pageURL string) {
	pageURL = strings.TrimSpace(pageURL)
	if !looksLikeURL(pageURL) {
		a.toast = locales.ErrOnlyHTTP()
		return
	}
	a.grabOpen = true
	a.grabBusy = true
	a.grabPage = pageURL
	a.grabLinks = nil
	a.grabPicked = map[string]bool{}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		var links []grab.Link
		if err == nil {
			req.Header.Set("User-Agent", a.st.Settings.UserAgent)
			client := &http.Client{Timeout: 45 * time.Second}
			var resp *http.Response
			if resp, err = client.Do(req); err == nil {
				defer resp.Body.Close()
				if base, perr := url.Parse(pageURL); perr == nil {
					var raw []string
					raw, err = grab.FromHTML(base, resp.Body)
					if err == nil {
						links = grab.Keep(raw, grab.Filter{Extensions: a.st.Settings.GrabExtensions})
					}
				}
			}
		}
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			a.grabBusy = false
			if err != nil {
				a.grabErr = err.Error()
				return
			}
			a.grabLinks = links
			for _, l := range links {
				a.grabPicked[l.URL] = true
			}
		})
	}()
}

// ImportFile adds every link a file holds.
func (a *App) ImportFile(path string) {
	urls, err := grab.FromFile(path)
	if err != nil {
		a.toast = locales.ToastImportFailed(err.Error())
		return
	}
	added := 0
	for _, u := range urls {
		if a.AddURL(u) != nil {
			added++
		}
	}
	a.toast = locales.ToastImported(added)
}

// CheckForUpdates asks GitHub for the latest release.
func (a *App) CheckForUpdates() {
	if a.updateBusy {
		return
	}
	a.updateBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rel, newer, err := update.Check(ctx, nil, "logdns/downmygo", Version)
		win := a.win
		if win == nil {
			return
		}
		win.Update(func() {
			a.updateBusy = false
			switch {
			case err != nil:
				a.toast = locales.ToastUpdateFailed(err.Error())
			case !newer:
				a.toast = locales.ToastUpToDate()
			default:
				a.updateRel = rel
				a.updateOpen = true
			}
		})
	}()
}

// OpenUpdate opens the release page of a newer version.
func (a *App) OpenUpdate() {
	if a.updateRel == nil {
		return
	}
	_ = mygo.Shell.OpenExternal(a.updateRel.URL)
	a.updateOpen = false
}

// Themes reads the theme files in the data folder.
func (a *App) Themes() []themes.Theme {
	list, _ := themes.Load(filepath.Join(a.dataDir(), "themes"))
	return list
}

func (a *App) dataDir() string {
	if a.store != nil {
		return a.store.Dir()
	}
	return "."
}

// themeFile is the theme the settings chose, if any.
func (a *App) themeFile() (themes.Theme, bool) {
	name := a.st.Settings.ThemeFile
	if name == "" {
		return themes.Theme{}, false
	}
	for _, t := range a.Themes() {
		if t.Name == name || t.File == name {
			return t, true
		}
	}
	return themes.Theme{}, false
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return &url.URL{}
	}
	return u
}
