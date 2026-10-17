package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/galiy/malina-26/web/internal/audit"
)

func TestMapTimeSyncFlag(t *testing.T) {
	off := filepath.Join(t.TempDir(), "map-time-sync.off")
	if !timeSyncEnabled(off) {
		t.Fatal("без файла синхронизация должна быть включена")
	}
	if err := setTimeSyncEnabled(off, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if timeSyncEnabled(off) {
		t.Fatal("при наличии флаг-файла синхронизация должна быть выключена")
	}
	if err := setTimeSyncEnabled(off, true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !timeSyncEnabled(off) {
		t.Fatal("после включения флага нет — синхронизация включена")
	}
}

func TestHandleSystemShutdownRequiresAuth(t *testing.T) {
	s := &Server{audit: audit.NewWithSink(false, nil)}
	r := httptest.NewRequest(http.MethodPost, "/system/shutdown", nil)
	w := httptest.NewRecorder()
	s.handleSystemShutdown(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("без авторизации ожидали редирект 303, получили %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("ожидали редирект на /login, получили %q", loc)
	}
}

func TestHandleSystemShutdownMethod(t *testing.T) {
	s := &Server{audit: audit.NewWithSink(false, nil)}
	r := httptest.NewRequest(http.MethodGet, "/system/shutdown", nil)
	r = newAuthedContext(r)
	w := httptest.NewRecorder()
	s.handleSystemShutdown(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET должен отклоняться 405, получили %d", w.Code)
	}
}
