package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/downmygo/downmygo/internal/model"
	"github.com/downmygo/downmygo/internal/torrent"
	"github.com/downmygo/downmygo/internal/version"
	"github.com/downmygo/downmygo/locales"
)

// Version is the app's version, shown in About and the API.
const Version = version.Version

// apiServer is the local automation API and the page a phone on the same
// network opens. It never listens on a public address: loopback only,
// unless the user turns remote control on.
type apiServer struct {
	srv   *http.Server
	app   *App
	token string
	port  int
	lan   bool
	// locale and pageHTML are the app's language and the remote page in
	// it, read once when the server starts, on the main thread.
	locale   string
	pageHTML string
}

type apiDownload struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	URL      string  `json:"url"`
	Size     int64   `json:"size"`
	Done     int64   `json:"done"`
	Percent  float64 `json:"percent"`
	Speed    float64 `json:"speed"`
	Status   string  `json:"status"`
	State    string  `json:"statusText"`
	Error    string  `json:"error,omitempty"`
	Category string  `json:"category"`
	Queue    string  `json:"queue,omitempty"`
	Dir      string  `json:"dir"`
	Added    string  `json:"added"`
}

// StartAPI starts the local API. lan makes it listen on every private
// address for the remote page; otherwise it stays on 127.0.0.1.
func (a *App) StartAPI(lan bool) error {
	if a.api != nil {
		a.api.Close()
	}
	s := &a.st.Settings
	if s.APIToken == "" {
		s.APIToken = model.NewID() + model.NewID()
		a.markDirty()
	}
	port := s.RemotePort
	if port == 0 {
		port = 8463
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if lan {
		addr = "0.0.0.0:" + strconv.Itoa(port)
	}
	locale := mygo.App.Locale()
	api := &apiServer{app: a, token: s.APIToken, port: port, lan: lan,
		locale: locale, pageHTML: remotePageHTML(locale)}
	mux := http.NewServeMux()
	mux.HandleFunc("/", api.page)
	mux.HandleFunc("/api/health", api.auth(api.health))
	mux.HandleFunc("/api/downloads", api.auth(api.downloads))
	mux.HandleFunc("/api/add", api.auth(api.add))
	mux.HandleFunc("/api/pause", api.auth(api.pause))
	mux.HandleFunc("/api/resume", api.auth(api.resume))
	mux.HandleFunc("/api/remove", api.auth(api.remove))
	mux.HandleFunc("/api/queues", api.auth(api.queues))
	mux.HandleFunc("/api/queue", api.auth(api.queue))
	mux.HandleFunc("/api/options", api.auth(api.options))
	api.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	go func() { _ = api.srv.Serve(ln) }()
	a.api = api
	return nil
}

// Close stops the API.
func (s *apiServer) Close() {
	if s != nil && s.srv != nil {
		_ = s.srv.Close()
	}
}

// onMain runs fn on the main thread and waits for it.
func (a *App) onMain(fn func()) {
	win := a.win
	if win == nil {
		return
	}
	done := make(chan struct{})
	win.Update(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (s *apiServer) auth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *apiServer) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "app": "downmygo", "version": Version, "locale": s.locale})
}

func (s *apiServer) downloadList() []apiDownload {
	out := make([]apiDownload, 0, len(s.app.st.Downloads))
	for _, d := range s.app.st.Downloads {
		out = append(out, apiDownload{
			ID: d.ID, Name: d.FileName, URL: d.URL, Size: d.Size, Done: d.Done,
			Percent: d.Percent(), Speed: d.Speed, Status: d.Status.String(),
			State: statusText(d), Error: s.app.errorText(d), Category: d.Category,
			Queue: d.Queue, Dir: d.Dir,
			Added: d.Added.Format(time.RFC3339),
		})
	}
	return out
}

func (s *apiServer) downloads(w http.ResponseWriter, r *http.Request) {
	var list []apiDownload
	var speed float64
	s.app.onMain(func() {
		list = s.downloadList()
		speed = s.app.TotalSpeed()
	})
	writeJSON(w, map[string]any{"downloads": list, "speed": speed})
}

func (s *apiServer) add(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		URL       string `json:"url"`
		Queue     string `json:"queue"`
		Start     *bool  `json:"start"`
		Dir       string `json:"dir"`
		Name      string `json:"name"`
		Referer   string `json:"referer"`
		Cookies   string `json:"cookies"`
		Duplicate bool   `json:"duplicate"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return
	}
	if !looksLikeURL(strings.TrimSpace(body.URL)) && !torrent.IsTorrent(body.URL) {
		http.Error(w, "not an http(s) link", http.StatusBadRequest)
		return
	}
	var d *model.Download
	var reason string
	if torrent.IsTorrent(body.URL) {
		s.app.onMain(func() { d = s.app.addLink(body.URL) })
		if d == nil {
			http.Error(w, "could not add the torrent", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"id": d.ID, "name": d.FileName})
		return
	}
	s.app.onMain(func() {
		s.app.add = AddForm{
			URL: body.URL, Dir: body.Dir, FileName: body.Name,
			Category: "general", Connections: s.app.st.Settings.Connections,
			UserAgent: s.app.st.Settings.UserAgent, Referer: body.Referer,
			Cookies: body.Cookies, Queue: body.Queue, StartNow: true,
			Duplicate: body.Duplicate,
		}
		if body.Start != nil {
			s.app.add.StartNow = *body.Start
		}
		d = s.app.commitAdd()
		if d == nil {
			reason = s.app.add.Err
		}
	})
	if d == nil {
		if reason == "" {
			reason = "could not add the link"
		}
		http.Error(w, reason, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"id": d.ID, "name": d.FileName})
}

func (s *apiServer) pause(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		var body struct{ ID string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		id = body.ID
	}
	s.app.onMain(func() { s.app.Pause(id) })
	writeJSON(w, map[string]any{"ok": true})
}

func (s *apiServer) resume(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		var body struct{ ID string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		id = body.ID
	}
	s.app.onMain(func() { s.app.Resume(id) })
	writeJSON(w, map[string]any{"ok": true})
}

func (s *apiServer) remove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	del := r.URL.Query().Get("delete") == "1"
	s.app.onMain(func() { s.app.Remove(id, del) })
	writeJSON(w, map[string]any{"ok": true})
}

func (s *apiServer) queues(w http.ResponseWriter, r *http.Request) {
	type q struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Running bool   `json:"running"`
		Count   int    `json:"count"`
	}
	var out []q
	s.app.onMain(func() {
		for _, queue := range s.app.st.Queues {
			n := 0
			for _, d := range s.app.st.Downloads {
				if d.Queue == queue.ID {
					n++
				}
			}
			out = append(out, q{queue.ID, queue.Name, s.app.queueRunning[queue.ID], n})
		}
	})
	writeJSON(w, out)
}

func (s *apiServer) queue(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	action := r.URL.Query().Get("action")
	s.app.onMain(func() {
		if action == "stop" {
			s.app.StopQueue(id)
		} else {
			s.app.StartQueue(id)
		}
	})
	writeJSON(w, map[string]any{"ok": true})
}

func (s *apiServer) options(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	s.app.onMain(func() {
		body = map[string]any{
			"downloadDir": s.app.st.Settings.DownloadDir,
			"connections": s.app.st.Settings.Connections,
			"globalLimit": s.app.st.Settings.GlobalLimit,
		}
	})
	writeJSON(w, body)
}

// page is the remote control page: a small, self-contained page a phone
// on the same network opens. The token is injected, so the page can call
// the API without asking the user to copy it.
func (s *apiServer) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	// The page carries the token only when it was opened with it, so a
	// device on the network cannot read it from the page alone.
	token := ""
	if r.URL.Query().Get("token") == s.token {
		token = s.token
	}
	page := s.pageHTML
	if page == "" {
		page = remotePageHTML("en")
	}
	// The token goes into a JavaScript string: a token the user made up
	// must not be able to leave it, nor close the script element.
	_, _ = w.Write([]byte(strings.Replace(page, "__TOKEN__", jsString(token), 1)))
}

// jsString writes s as the inside of a JavaScript string literal, safe
// inside a script element.
func jsString(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`'`, `\'`,
		"\n", `\n`,
		"\r", `\r`,
		"<", `\u003c`,
		">", `\u003e`,
		"&", `\u0026`,
	).Replace(s)
}

// remotePageHTML says the remote page in the language of msgs.
func remotePageHTML(tag string) string {
	msgs := locales.In(tag)
	return strings.NewReplacer(
		"__LANG__", tag,
		"__TITLE__", msgs.RemoteTitle(),
		"__PLACEHOLDER__", msgs.RemotePlaceholder(),
		"__ADD__", msgs.RemoteAdd(),
		"__LOADING__", msgs.RemoteLoading(),
		"__EMPTY__", msgs.RemoteEmpty(),
		"__PAUSE__", msgs.Pause(),
		"__RESUME__", msgs.Resume(),
		"__REMOVE__", msgs.Remove(),
		"__OF__", msgs.RemoteOf("__DONE__", "__SIZE__"),
	).Replace(remotePage)
}

const remotePage = `<!doctype html>
<html lang="__LANG__"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="theme-color" content="#0e1116">
<title>__TITLE__</title>
<style>
:root{color-scheme:light dark;--bg:#f6f7fb;--card:#fff;--ink:#10131a;--muted:#667085;--line:#e3e7ef;--accent:#3b6ef6;--ok:#12b76a;--bad:#f04438}
@media(prefers-color-scheme:dark){:root{--bg:#0e1116;--card:#171b22;--ink:#edf1f7;--muted:#98a2b3;--line:#262c36;--accent:#6e93ff}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.45 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;padding:16px;padding-bottom:40px}
header{display:flex;align-items:center;gap:10px;margin:4px 0 18px}
.logo{width:34px;height:34px;border-radius:10px;background:linear-gradient(135deg,#3b6ef6,#12b8a6);display:grid;place-items:center;color:#fff;font-weight:800;font-size:18px}
h1{font-size:20px;margin:0;letter-spacing:-.02em}
.sub{color:var(--muted);font-size:13px;margin-left:auto}
form{display:flex;gap:8px;background:var(--card);border:1px solid var(--line);border-radius:14px;padding:8px;margin-bottom:16px}
input{flex:1;min-width:0;border:0;background:transparent;color:inherit;font:inherit;padding:10px;outline:none}
button{border:0;border-radius:10px;padding:10px 16px;font:inherit;font-weight:600;background:var(--accent);color:#fff}
button.ghost{background:transparent;color:var(--muted);padding:6px 10px;font-size:13px}
.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:12px 14px;margin-bottom:10px}
.row{display:flex;align-items:center;gap:10px}
.name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.meta{color:var(--muted);font-size:13px;margin-top:3px}
.bar{height:6px;border-radius:99px;background:var(--line);overflow:hidden;margin-top:10px}
.bar>i{display:block;height:100%;background:var(--accent);border-radius:99px;transition:width .4s}
.done .bar>i{background:var(--ok)}.err .bar>i{background:var(--bad)}
.pct{margin-left:auto;font-variant-numeric:tabular-nums;color:var(--muted);font-size:13px}
.empty{color:var(--muted);text-align:center;padding:40px 0}
</style></head><body>
<header><div class="logo">d</div><h1>downmygo</h1><span class="sub" id="speed"></span></header>
<form id="add"><input id="url" placeholder="__PLACEHOLDER__" autocomplete="off" autocapitalize="off"><button>__ADD__</button></form>
<div id="list"><div class="empty">__LOADING__</div></div>
<script>
let TOKEN = "__TOKEN__";
if (!TOKEN) TOKEN = localStorage.getItem("downmygo-token") || "";
if (!TOKEN) {
  TOKEN = prompt("downmygo token (Options → Advanced)") || "";
  if (TOKEN) localStorage.setItem("downmygo-token", TOKEN);
}
const api = (p, o={}) => fetch(p, {...o, headers:{...o.headers, "Authorization":"Bearer "+TOKEN, "Content-Type":"application/json"}}).then(r => r.json());
const fmt = n => n>=1073741824?(n/1073741824).toFixed(1)+" GB":n>=1048576?(n/1048576).toFixed(1)+" MB":n>=1024?(n/1024).toFixed(0)+" KB":n+" B";
const spd = n => n>0?fmt(n)+"/s":"";
let busy = false;
async function load(){
  try{
    const d = await api("/api/downloads");
    document.getElementById("speed").textContent = d.speed>0 ? spd(d.speed) : "";
    const list = document.getElementById("list");
    if(!d.downloads.length){ list.innerHTML = '<div class="empty">__EMPTY__</div>'; return; }
    list.innerHTML = d.downloads.map(x => {
      const pct = x.percent < 0 ? 0 : Math.round(x.percent*100);
      const cls = x.status === "Complete" ? "card done" : x.status === "Error" ? "card err" : "card";
      const state = x.status === "Downloading" ? spd(x.speed) : x.statusText;
      return '<div class="'+cls+'"><div class="row"><div class="name">'+esc(x.name)+'</div><div class="pct">'+(x.status==="Complete"?"100%":pct+"%")+'</div></div>'+
        '<div class="meta">'+of(x)+' · '+esc(state)+(x.error?' · '+esc(x.error):'')+'</div>'+
        '<div class="bar"><i style="width:'+(x.status==="Complete"?100:pct)+'%"></i></div>'+
        '<div class="row" style="margin-top:8px">'+
        (x.status==="Downloading"||x.status==="Connecting"?'<button class="ghost" onclick="act(\'pause\',\''+x.id+'\')">__PAUSE__</button>':'')+
        (x.status==="Paused"||x.status==="Error"?'<button class="ghost" onclick="act(\'resume\',\''+x.id+'\')">__RESUME__</button>':'')+
        '<button class="ghost" style="margin-left:auto" onclick="act(\'remove\',\''+x.id+'\')">__REMOVE__</button></div></div>';
    }).join("");
  }catch(e){}
}
const of = x => "__OF__".replace("__DONE__", fmt(x.done)).replace("__SIZE__", x.size>0?fmt(x.size):"?");
function esc(s){return String(s).replace(/[&<>"]/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]))}
async function act(what,id){ await api("/api/"+what+"?id="+encodeURIComponent(id),{method:"POST"}); load(); }
document.getElementById("add").onsubmit = async e => {
  e.preventDefault(); const u = document.getElementById("url");
  if(!u.value.trim()) return;
  await api("/api/add",{method:"POST",body:JSON.stringify({url:u.value.trim()})});
  u.value=""; load();
};
load(); setInterval(load, 2000);
</script></body></html>`
