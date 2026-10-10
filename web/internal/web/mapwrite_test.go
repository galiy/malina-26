package web

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/galiy/malina-26/web/internal/authstore"
)

// TestMapWriteStatuses — маппинг кодов map-reader в HTTP-статусы + 401/400.
func TestMapWriteStatuses(t *testing.T) {
	cases := []struct {
		resp string
		want int
	}{
		{`{"ok":true}`, http.StatusOK},
		{`{"ok":false,"code":"bad_request","error":"x"}`, http.StatusBadRequest},
		{`{"ok":false,"code":"no_device","error":"x"}`, http.StatusNotFound},
		{`{"ok":false,"code":"map_error","error":"x","op":0,"ecode":"10"}`, http.StatusUnprocessableEntity},
		{`{"ok":false,"code":"retry_exhausted","error":"x"}`, http.StatusBadGateway},
		{`{"ok":false,"code":"internal","error":"x"}`, http.StatusInternalServerError},
		{`{"ok":false,"error":"bad request"}`, http.StatusBadRequest}, // пустой code → 400
	}
	for _, c := range cases {
		sock := filepath.Join(t.TempDir(), "s.sock")
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		go func(resp string) {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				_, _ = conn.Write([]byte(resp + "\n"))
				conn.Close()
			}
		}(c.resp)

		st, _ := authstore.Open(filepath.Join(t.TempDir(), "as"), time.Hour)
		s := &Server{opts: Options{User: "malina", SessionIdle: time.Hour, ControlSock: sock}, store: st}
		tok, _ := authstore.NewToken()
		st.Issue(tok)
		h := s.authMiddleware(http.HandlerFunc(s.handleAPIMapWrite))

		req := httptest.NewRequest(http.MethodPost, "/api/map/write",
			strings.NewReader(`{"id":0,"gen":1,"ops":[{"op":"cmd","code":"07"}]}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("resp %s: got %d, want %d", c.resp, rec.Code, c.want)
		}
		ln.Close()
		st.Close()
	}
}

func TestMapWriteNoAuthAndBadJSON(t *testing.T) {
	st, _ := authstore.Open(filepath.Join(t.TempDir(), "as"), time.Hour)
	defer st.Close()
	s := &Server{opts: Options{User: "malina", SessionIdle: time.Hour, ControlSock: "/nonexistent"}, store: st}
	h := s.authMiddleware(http.HandlerFunc(s.handleAPIMapWrite))

	// Без авторизации — 401, сокет не трогаем.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/map/write", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("без auth: got %d, want 401", rec.Code)
	}

	// С авторизацией, но битый JSON — 400.
	tok, _ := authstore.NewToken()
	st.Issue(tok)
	req := httptest.NewRequest(http.MethodPost, "/api/map/write", strings.NewReader(`{bad`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("битый JSON: got %d, want 400", rec.Code)
	}
}
