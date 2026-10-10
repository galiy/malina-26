package authstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIssueTouchRevoke(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth-shm")
	s, err := Open(p, time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	tok, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	if s.Touch(tok) {
		t.Fatal("неизвестный токен не должен проходить")
	}
	s.Issue(tok)
	if !s.Touch(tok) {
		t.Fatal("выданный токен должен проходить")
	}
	s.Revoke(tok)
	if s.Touch(tok) {
		t.Fatal("отозванный токен не должен проходить")
	}
}

func TestExpiry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth-shm")
	s, err := Open(p, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	tok, _ := NewToken()
	s.Issue(tok)
	if !s.Touch(tok) {
		t.Fatal("свежий токен должен проходить")
	}
	time.Sleep(40 * time.Millisecond)
	if s.Touch(tok) {
		t.Fatal("протухший (idle) токен не должен проходить")
	}
}

func TestPurgeAll(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth-shm")
	s, _ := Open(p, time.Hour)
	defer s.Close()

	t1, _ := NewToken()
	t2, _ := NewToken()
	s.Issue(t1)
	s.Issue(t2)
	s.PurgeAll()
	if s.Touch(t1) || s.Touch(t2) {
		t.Fatal("после PurgeAll ни один токен не должен проходить")
	}
}

// TestPersistAcrossReopen — сессии живут перезапуск процесса (файл в tmpfs).
func TestPersistAcrossReopen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth-shm")
	s, _ := Open(p, time.Hour)
	tok, _ := NewToken()
	s.Issue(tok)
	s.Close()

	s2, err := Open(p, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.Touch(tok) {
		t.Fatal("токен должен переживать переоткрытие сегмента")
	}
}
