package web

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	dir := t.TempDir()
	s.opts.ArchConfigPath = filepath.Join(dir, "malina-arch.json")
	s.opts.ArchKeyPath = filepath.Join(dir, "arch.key")
	s.opts.ArchControlSock = filepath.Join(dir, "control.sock")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := os.WriteFile(s.opts.ArchKeyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

func authRequest(t *testing.T, s *Server, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/stats/settings", strings.NewReader(form.Encode()))
	r.Host = "malina.local"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://malina.local")
	return r.WithContext(context.WithValue(r.Context(), authKey, true))
}

func TestArchSaveValidation(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"save"}, "target": {"network"}, "protocol": {"sftp"},
		"port": {"22"}, "access_timeout_sec": {"10"}, "avg_scan_min": {"5"}}
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, form))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=validation") {
		t.Fatalf("ожидался redirect err=validation, получено %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := os.Stat(s.opts.ArchConfigPath); !os.IsNotExist(err) {
		t.Fatalf("конфиг не должен сохраняться при ошибке: %v", err)
	}
}

func TestArchSaveSameOriginRejected(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"disable"}}
	r := authRequest(t, s, form)
	r.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403, получено %d", rec.Code)
	}
}

func TestArchAADEncrypt(t *testing.T) {
	s := archTestServer(t)
	enc, err := archEncrypt(s.opts.ArchKeyPath, archAAD("smb2", "password"), "p@ss")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(s.opts.ArchKeyPath)
	block, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(block)
	nonce, ct := raw[:g.NonceSize()], raw[g.NonceSize():]
	plain, err := g.Open(nil, nonce, ct, []byte("malina-arch/smb2/password/v1"))
	if err != nil || string(plain) != "p@ss" {
		t.Fatalf("AAD/шифртекст: %q %v", plain, err)
	}
	if _, err := g.Open(nil, nonce, ct, []byte("malina-arch/sftp/password/v1")); err == nil {
		t.Fatal("AAD другого протокола не должен подходить")
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://malina.local/stats/settings", nil)
	r.Host = "malina.local"
	r.Header.Set("Origin", "http://malina.local")
	if !sameOrigin(r) {
		t.Fatal("same origin отклонён")
	}
	r.Header.Set("Origin", "http://evil.example")
	if sameOrigin(r) {
		t.Fatal("чужой origin принят")
	}
	r.Header.Del("Origin")
	r.Header.Set("Referer", "http://malina.local/stats/settings")
	if !sameOrigin(r) {
		t.Fatal("same referer отклонён")
	}
	r.Header.Del("Referer")
	if sameOrigin(r) {
		t.Fatal("запрос без Origin/Referer принят")
	}
}

func TestArchFlashValidation(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"save"}, "target": {"flash"}, "flash_uuid": {"no-such-uuid"}, "avg_scan_min": {"5"}}
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, form))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "err=validation") {
		t.Fatalf("ожидался err=validation, получено %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestArchSaveLogLevel(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"save"}, "target": {"network"}, "protocol": {"sftp"},
		"host": {"nas.local"}, "port": {"22"}, "path": {"/backup"}, "user": {"u"},
		"access_timeout_sec": {"10"}, "avg_scan_min": {"5"}, "log_level": {"debug"}}
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, form))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ожидался redirect, получено %d", rec.Code)
	}
	cfg := loadArchConfig(s.opts.ArchConfigPath)
	if cfg.LogLevel != "debug" {
		t.Fatalf("log_level=%q", cfg.LogLevel)
	}
}

func TestArchSaveLogLevelInvalid(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"save"}, "target": {"network"}, "protocol": {"sftp"},
		"host": {"nas.local"}, "port": {"22"}, "path": {"/backup"}, "user": {"u"},
		"access_timeout_sec": {"10"}, "avg_scan_min": {"5"}, "log_level": {"bogus"}}
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, form))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ожидался redirect, получено %d", rec.Code)
	}
	if cfg := loadArchConfig(s.opts.ArchConfigPath); cfg.LogLevel != "info" {
		t.Fatalf("log_level=%q, ожидался info", cfg.LogLevel)
	}
}

func TestArchTestActionReportsError(t *testing.T) {
	s := archTestServer(t)
	form := url.Values{"action": {"test"}}
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, form))
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "err=test") || !strings.Contains(loc, "why=") {
		t.Fatalf("ожидался err=test с текстом, получено %d %q", rec.Code, loc)
	}
}

func archStub(t *testing.T, response string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "control.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = bufio.NewReader(c).ReadString('\n')
		_, _ = c.Write([]byte(response + "\n"))
	}()
	return sock
}

func TestArchEnableContinuedMessage(t *testing.T) {
	s := archTestServer(t)
	s.opts.ArchControlSock = archStub(t, `{"ok":true,"state":"continued"}`)
	rec := httptest.NewRecorder()
	s.handleStatsSettings(rec, authRequest(t, s, url.Values{"action": {"enable_auto"}}))
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "ok=continued") {
		t.Fatalf("ожидался ok=continued, получено %q", loc)
	}
}
