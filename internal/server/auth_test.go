package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emdzej/acc/internal/store"
)

func testServer(t *testing.T, a Access) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "acc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, "local-token", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.SetAccess(a)
	return s.Handler()
}

func do(h http.Handler, method, target string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRemoteKey(t *testing.T) {
	h := testServer(t, Access{RemoteKey: "shared-key"})
	for _, c := range []struct {
		auth string
		want int
	}{{"local-token", 200}, {"shared-key", 200}, {"nope", 401}, {"", 401}} {
		if got := do(h, "GET", "http://192.168.1.5:7317/api/v1/sessions", map[string]string{"Authorization": "Bearer " + c.auth}, "").Code; got != c.want {
			t.Errorf("token %q: %d, want %d", c.auth, got, c.want)
		}
	}
	if w := do(h, "GET", "http://192.168.1.5:7317/", nil, ""); w.Code != 401 || !strings.Contains(w.Body.String(), `action="/login"`) {
		t.Errorf("signed-out dashboard should show the sign-in page: %d", w.Code)
	}
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": "http://192.168.1.5:7317"}
	w := do(h, "POST", "http://192.168.1.5:7317/login", form, url.Values{"key": {"shared-key"}}.Encode())
	if c := w.Result().Cookies(); w.Code != http.StatusSeeOther || len(c) != 1 || c[0].Value != "shared-key" {
		t.Fatalf("sign-in: %d %v", w.Code, c)
	}
	for range maxFailures {
		do(h, "POST", "http://192.168.1.5:7317/login", form, "key=wrong")
	}
	if w := do(h, "POST", "http://192.168.1.5:7317/login", form, "key=shared-key"); w.Code != http.StatusTooManyRequests {
		t.Errorf("after %d failures: %d", maxFailures, w.Code)
	}
}

func TestNoAuthGuards(t *testing.T) {
	h := testServer(t, Access{NoAuth: true, Hosts: []string{"acc.lan"}})
	if got := do(h, "GET", "http://192.168.1.5:7317/api/v1/sessions", nil, "").Code; got != 200 {
		t.Errorf("no auth: %d", got)
	}
	if got := do(h, "GET", "http://acc.lan:7317/api/v1/sessions", nil, "").Code; got != 200 {
		t.Errorf("allowed host: %d", got)
	}
	// DNS rebinding: a page on evil.example resolving to this machine.
	if got := do(h, "GET", "http://evil.example:7317/api/v1/sessions", nil, "").Code; got != http.StatusMisdirectedRequest {
		t.Errorf("unknown host: %d", got)
	}
	// A page elsewhere posting to the daemon.
	body := `{"cwd":"/tmp","prompt":"rm -rf ~"}`
	for _, hdr := range []map[string]string{{"Origin": "https://evil.example"}, {"Sec-Fetch-Site": "cross-site"}} {
		if got := do(h, "POST", "http://192.168.1.5:7317/api/v1/sessions", hdr, body).Code; got != http.StatusForbidden {
			t.Errorf("cross-site %v: %d", hdr, got)
		}
	}
}
