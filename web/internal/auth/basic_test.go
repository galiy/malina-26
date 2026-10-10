package auth

import (
	"errors"
	"testing"
	"time"
)

type fakeAuth struct {
	calls      int
	user, pass string
}

func (f *fakeAuth) Authenticate(u, p string) error {
	f.calls++
	if u == f.user && p == f.pass {
		return nil
	}
	return errors.New("invalid")
}

func TestBasicCacheHitsAndMisses(t *testing.T) {
	fa := &fakeAuth{user: "malina", pass: "pw"}
	c := NewBasicCache(fa, "malina", time.Minute, 2)

	if !c.Check("malina", "pw") {
		t.Fatal("верный пароль должен проходить")
	}
	if fa.calls != 1 {
		t.Fatalf("PAM должен быть вызван 1 раз, got %d", fa.calls)
	}
	if !c.Check("malina", "pw") {
		t.Fatal("повторный верный должен проходить")
	}
	if fa.calls != 1 {
		t.Fatalf("кэш должен избегать PAM, got calls=%d", fa.calls)
	}
	if c.Check("malina", "bad") {
		t.Fatal("неверный пароль не должен проходить")
	}
	if fa.calls != 2 {
		t.Fatalf("неверный всегда в PAM, got calls=%d", fa.calls)
	}
	if c.Check("other", "pw") {
		t.Fatal("чужой пользователь не должен проходить")
	}
	if fa.calls != 2 {
		t.Fatalf("чужой пользователь без PAM, got calls=%d", fa.calls)
	}
}

func TestBasicCacheEviction(t *testing.T) {
	fa := &fakeAuth{user: "malina", pass: "pw"}
	// max=1: после второго успеха первый вытесняется.
	c := NewBasicCache(fa, "malina", time.Minute, 1)
	if !c.Check("malina", "pw") {
		t.Fatal("первый")
	}
	// Тот же пароль — снова из кэша.
	if !c.Check("malina", "pw") || fa.calls != 1 {
		t.Fatalf("до вытеснения: calls=%d", fa.calls)
	}
}
