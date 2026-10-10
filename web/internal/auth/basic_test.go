package auth

import (
	"errors"
	"testing"
	"time"
)

type fakeAuth struct {
	calls      int
	user, pass string
	acceptAll  bool
}

func (f *fakeAuth) Authenticate(u, p string) error {
	f.calls++
	if f.acceptAll || (u == f.user && p == f.pass) {
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
	fa := &fakeAuth{acceptAll: true}
	c := NewBasicCache(fa, "malina", time.Minute, 1)
	if !c.Check("malina", "a") || fa.calls != 1 {
		t.Fatalf("a: calls=%d", fa.calls)
	}
	if !c.Check("malina", "b") || fa.calls != 2 {
		t.Fatalf("b: calls=%d", fa.calls)
	}
	// max=1: ключ "a" вытеснен → снова PAM.
	if !c.Check("malina", "a") || fa.calls != 3 {
		t.Fatalf("a после вытеснения должен снова идти в PAM: calls=%d", fa.calls)
	}
}

func TestBasicCacheTTL(t *testing.T) {
	fa := &fakeAuth{user: "malina", pass: "pw"}
	c := NewBasicCache(fa, "malina", 20*time.Millisecond, 8)
	if !c.Check("malina", "pw") || fa.calls != 1 {
		t.Fatalf("первый: calls=%d", fa.calls)
	}
	time.Sleep(40 * time.Millisecond)
	if !c.Check("malina", "pw") || fa.calls != 2 {
		t.Fatalf("после TTL снова PAM: calls=%d", fa.calls)
	}
}

func TestBasicCacheInvalidate(t *testing.T) {
	fa := &fakeAuth{user: "malina", pass: "pw"}
	c := NewBasicCache(fa, "malina", time.Minute, 8)
	if !c.Check("malina", "pw") || fa.calls != 1 {
		t.Fatalf("первый: calls=%d", fa.calls)
	}
	c.Invalidate()
	if !c.Check("malina", "pw") || fa.calls != 2 {
		t.Fatalf("после Invalidate снова PAM: calls=%d", fa.calls)
	}
}
