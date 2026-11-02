package server

import (
	"crypto/subtle"
	_ "embed"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const cookieName = "acc_token"

// Access says who may use the daemon besides holders of the local token.
type Access struct {
	// RemoteKey is a pre-shared key other devices sign in with, accepted
	// wherever the local token is. "" accepts only the token.
	RemoteKey string
	// NoAuth lets anyone who can reach the daemon use it, with no key.
	NoAuth bool
	// Hosts are host names, besides IP addresses, localhost and this
	// machine's own name, the daemon may be reached by.
	Hosts []string
}

// SetAccess configures access beyond the local token; call before serving.
func (s *Server) SetAccess(a Access) { s.acc = a }

//go:embed login.html
var loginPage []byte

// guard rejects what a hostile web page could send while you browse:
//   - requests by a host name ACC doesn't know, so DNS rebinding can't turn
//     a page's own origin into a path to the daemon;
//   - cross-site writes (form posts, fetches from other origins).
//
// It runs before auth because with NoAuth it is the only protection.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.knownHost(r.Host) {
			http.Error(w, "unknown host name: start acc serve with --allow-host "+hostOnly(r.Host), http.StatusMisdirectedRequest)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && crossSite(r) {
			writeErr(w, http.StatusForbidden, errors.New("cross-site request refused"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) knownHost(hostport string) bool {
	h := strings.ToLower(hostOnly(hostport))
	if h == "" || h == "localhost" || net.ParseIP(h) != nil {
		return true
	}
	if name, err := os.Hostname(); err == nil {
		name = strings.ToLower(name)
		short, _, _ := strings.Cut(name, ".")
		if h == name || h == short || h == short+".local" {
			return true
		}
	}
	for _, a := range s.acc.Hosts {
		if strings.EqualFold(h, a) {
			return true
		}
	}
	return false
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func crossSite(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return false // not from a browser page
	}
	u, err := url.Parse(o)
	return err != nil || !strings.EqualFold(u.Host, r.Host)
}

// auth accepts a bearer token (CLI, hooks, TUI) or a cookie (browser), set
// by visiting /?token=… once (acc open builds that URL) or by signing in on
// the login page with the remote key. With NoAuth everything passes.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || s.acc.NoAuth {
			next.ServeHTTP(w, r)
			return
		}
		if t := r.URL.Query().Get("token"); t != "" && r.URL.Path == "/" {
			if !s.validToken(t) {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			s.setCookie(w, r, t)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if c, err := r.Cookie(cookieName); tok == "" && err == nil {
			tok = c.Value
		}
		if !s.validToken(tok) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write(loginPage)
				return
			}
			writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// login signs a browser in with the remote key (or the local token).
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := hostOnly(r.RemoteAddr)
	if !s.fails.allow(ip) {
		http.Error(w, "too many failed sign-ins: wait a minute", http.StatusTooManyRequests)
		return
	}
	key := strings.TrimSpace(r.PostFormValue("key"))
	if !s.validToken(key) {
		s.fails.add(ip)
		s.log.Warn("failed sign-in", "remote", ip)
		http.Redirect(w, r, "/?failed=1", http.StatusSeeOther)
		return
	}
	s.log.Info("signed in", "audit", true, "remote", ip)
	s.setCookie(w, r, key)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, v string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: v, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
	})
}

func (s *Server) validToken(t string) bool {
	if t == "" {
		return false
	}
	ok := subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
	if k := s.acc.RemoteKey; k != "" {
		ok = subtle.ConstantTimeCompare([]byte(t), []byte(k)) == 1 || ok
	}
	return ok
}

// failures limits sign-in attempts per address: a guessable key set with
// ACC_REMOTE_KEY shouldn't be cheap to brute-force.
type failures struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

const (
	maxFailures   = 10
	failureWindow = time.Minute
)

func newFailures() *failures { return &failures{m: map[string][]time.Time{}} }

func (f *failures) recent(ip string) []time.Time {
	cut := time.Now().Add(-failureWindow)
	ts := f.m[ip]
	for len(ts) > 0 && ts[0].Before(cut) {
		ts = ts[1:]
	}
	f.m[ip] = ts
	return ts
}

func (f *failures) allow(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recent(ip)) < maxFailures
}

func (f *failures) add(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[ip] = append(f.recent(ip), time.Now())
}
