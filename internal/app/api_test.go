package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// apiFor builds the API server the handlers need, without a window: the
// parts under test here never touch the UI thread.
func apiFor(t *testing.T) *apiServer {
	t.Helper()
	a := demoApp(t)
	a.st.Settings.APIToken = "secret"
	return &apiServer{app: a, token: "secret", port: 8463, pageHTML: remotePageHTML("en")}
}

func TestAPIAuth(t *testing.T) {
	s := apiFor(t)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	h := s.auth(ok)

	cases := []struct {
		name   string
		url    string
		header string
		want   int
	}{
		{"no token", "/api/downloads", "", http.StatusUnauthorized},
		{"wrong token", "/api/downloads", "Bearer nope", http.StatusUnauthorized},
		{"header token", "/api/downloads", "Bearer secret", http.StatusOK},
		{"query token", "/api/downloads?token=secret", "", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, c.url, nil)
			if c.header != "" {
				r.Header.Set("Authorization", c.header)
			}
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}

func TestAPIHealth(t *testing.T) {
	s := apiFor(t)
	w := httptest.NewRecorder()
	s.health(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body struct {
		OK      bool   `json:"ok"`
		App     string `json:"app"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.App != "downmygo" || body.Version != Version {
		t.Fatalf("health = %+v", body)
	}
}

func TestAPIRemotePageCarriesTheToken(t *testing.T) {
	s := apiFor(t)

	// A device that only knows the address must not read the token from
	// the page; the page asks for it instead.
	w := httptest.NewRecorder()
	s.page(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), `"secret"`) {
		t.Fatal("the page leaked the token without being asked")
	}
	if strings.Contains(w.Body.String(), "__TOKEN__") {
		t.Fatal("the token placeholder was left in the page")
	}

	// Opened with the token, the page carries it, so the phone does not
	// have to type it again.
	w = httptest.NewRecorder()
	s.page(w, httptest.NewRequest(http.MethodGet, "/?token=secret", nil))
	if !strings.Contains(w.Body.String(), `let TOKEN = "secret"`) {
		t.Fatal("the page does not carry the token it was opened with")
	}

	w = httptest.NewRecorder()
	s.page(w, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAPIAddRejectsBadRequests(t *testing.T) {
	s := apiFor(t)
	cases := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{"GET", http.MethodGet, "", http.StatusMethodNotAllowed},
		{"bad JSON", http.MethodPost, "{", http.StatusBadRequest},
		{"not a link", http.MethodPost, `{"url":"file:///etc/passwd"}`, http.StatusBadRequest},
		{"empty", http.MethodPost, `{"url":""}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/api/add", strings.NewReader(c.body))
			w := httptest.NewRecorder()
			s.add(w, r)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", w.Code, c.want, w.Body.String())
			}
		})
	}
}

func TestAPIPauseAndResumeNeedAnID(t *testing.T) {
	s := apiFor(t)
	// Without a window onMain does nothing, so these only check that a
	// missing id is not a crash and that the handler answers.
	for _, h := range []func(http.ResponseWriter, *http.Request){s.pause, s.resume, s.remove} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, "/api/x", strings.NewReader("{}")))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	}
}

func TestAPITokenCannotLeaveTheScript(t *testing.T) {
	a := demoApp(t)
	a.st.Settings.APIToken = `x"</script><script>alert(1)</script>`
	s := &apiServer{app: a, token: a.st.Settings.APIToken, pageHTML: remotePageHTML("en")}
	r := httptest.NewRequest(http.MethodGet, "/?token="+url.QueryEscape(a.st.Settings.APIToken), nil)
	w := httptest.NewRecorder()
	s.page(w, r)
	body := w.Body.String()
	if strings.Contains(body, "</script><script>alert(1)") {
		t.Fatal("the token closed the script element")
	}
	want := `let TOKEN = "x\"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e";`
	if !strings.Contains(body, want) {
		i := strings.Index(body, "let TOKEN")
		if i < 0 {
			t.Fatalf("the page has no token line")
		}
		t.Fatalf("the token is not escaped:\n%s", body[i:i+120])
	}
}
