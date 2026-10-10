package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/web/internal/authstore"
	"github.com/galiy/malina-26/web/internal/session"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := authstore.Open(filepath.Join(t.TempDir(), "auth-shm"), time.Hour)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return &Server{
		opts:  Options{User: "malina", SessionIdle: time.Hour},
		store: st,
	}
}

func (s *Server) probeCurrentUser(r *http.Request) bool {
	_, ok := s.currentUser(r)
	return ok
}

func TestAuthMiddlewareCookieAndBearer(t *testing.T) {
	s := newTestServer(t)
	tok, _ := authstore.NewToken()
	s.store.Issue(tok)
	stale, _ := authstore.NewToken()
	s.store.Issue(stale)
	s.store.Revoke(stale) // протухшая cookie

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.probeCurrentUser(r) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	h := s.authMiddleware(next)

	cases := []struct {
		name   string
		setup  func(*http.Request)
		status int
	}{
		{"нет авторизации", func(*http.Request) {}, http.StatusUnauthorized},
		{"cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: tok})
		}, http.StatusOK},
		{"bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+tok)
		}, http.StatusOK},
		{"битый bearer", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer deadbeef")
		}, http.StatusUnauthorized},
		{"протухшая cookie + валидный bearer", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: stale})
			r.Header.Set("Authorization", "Bearer "+tok)
		}, http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/map/devices", nil)
		c.setup(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.status {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.status)
		}
	}
}
