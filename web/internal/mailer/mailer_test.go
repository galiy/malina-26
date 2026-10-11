package mailer

import (
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	good := Config{Provider: "gmail", From: "a@b.com", Username: "a@b.com", Recipient: "c@d.com"}
	if err := Validate(good); err != nil {
		t.Fatalf("good: %v", err)
	}
	bad := []Config{
		{Provider: "nope", From: "a@b.com", Username: "a", Recipient: "c@d.com"},
		{Provider: "gmail", From: "bad", Username: "a", Recipient: "c@d.com"},
		{Provider: "gmail", From: "a@b.com", Username: "", Recipient: "c@d.com"},
		{Provider: "gmail", From: "a@b.com", Username: "a", Recipient: "bad"},
		{Provider: "custom", From: "a@b.com", Username: "a", Recipient: "c@d.com"}, // нет host/port
	}
	for i := range bad {
		if err := Validate(bad[i]); err == nil {
			t.Fatalf("кейс %d: ожидали ошибку", i)
		}
	}
}

func TestCryptoRoundtrip(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	enc, err := EncryptSecret(key, "app-password-123")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == "" || enc == "app-password-123" {
		t.Fatal("секрет не зашифрован")
	}
	got, err := DecryptSecret(key, enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "app-password-123" {
		t.Fatalf("roundtrip: %q", got)
	}
	// другой ключ — не расшифруется
	if _, err := DecryptSecret(filepath.Join(t.TempDir(), "other"), enc); err == nil {
		t.Fatal("чужим ключом не должно расшифровываться")
	}
}

func TestOutboxAddListDeleteLimit(t *testing.T) {
	ob := &Outbox{Dir: t.TempDir()}
	for i := 0; i < MaxOutbox+5; i++ {
		if err := ob.Add(Message{Subject: "s"}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	list, err := ob.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != MaxOutbox {
		t.Fatalf("лимит не соблюдён: %d", len(list))
	}
	if err := ob.Delete(list[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if l, _ := ob.List(); len(l) != MaxOutbox-1 {
		t.Fatalf("после удаления: %d", len(l))
	}
	if err := ob.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if l, _ := ob.List(); len(l) != 0 {
		t.Fatal("после Clear должно быть пусто")
	}
	if err := ob.Delete("../../x"); err != nil {
		t.Fatalf("плохой id не должен падать: %v", err)
	}
}

func TestRenderTemplate(t *testing.T) {
	got := renderTemplate("A {{x}} B {{y}}", map[string]any{"x": 1, "y": "z"})
	if got != "A 1 B z" {
		t.Fatalf("got=%q", got)
	}
}

func TestConfigCacheSecretAndInvalidate(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "email.json"), filepath.Join(dir, "key"), filepath.Join(dir, "out"))
	c := Config{Provider: "custom", From: "a@b.com", Username: "a@b.com",
		Host: "h.example", Port: 465, Security: "ssl", Recipient: "c@d.com"}
	if err := m.SaveConfig(c, "s3cret"); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, err := m.LoadConfig()
	if err != nil || cfg.Provider != "custom" || cfg.SecretEnc == "" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if !m.SecretConfigured() {
		t.Fatal("секрет должен быть задан")
	}
	s, err := m.secretFor(cfg)
	if err != nil || s != "s3cret" {
		t.Fatalf("secret=%q err=%v", s, err)
	}
	// после сброса кэша — тоже расшифровывается
	m.Invalidate()
	cfg2, _ := m.LoadConfig()
	if s2, err := m.secretFor(cfg2); err != nil || s2 != "s3cret" {
		t.Fatalf("after invalidate: %q %v", s2, err)
	}
}

func TestNormalizeSecret(t *testing.T) {
	if got := normalizeSecret("abcd efgh\tijkl\nmnop "); got != "abcdefghijklmnop" {
		t.Fatalf("normalizeSecret=%q", got)
	}
}

func TestSaveStripsSecretSpaces(t *testing.T) {
	dir := t.TempDir()
	m := New(filepath.Join(dir, "email.json"), filepath.Join(dir, "key"), filepath.Join(dir, "out"))
	c := Config{Provider: "custom", From: "a@b.com", Username: "a@b.com",
		Host: "h.example", Port: 465, Security: "ssl", Recipient: "c@d.com"}
	if err := m.SaveConfig(c, "abcd efgh ijkl mnop"); err != nil {
		t.Fatalf("save: %v", err)
	}
	cfg, _ := m.LoadConfig()
	s, err := m.secretFor(cfg)
	if err != nil || s != "abcdefghijklmnop" {
		t.Fatalf("secret=%q err=%v", s, err)
	}
}

func TestEffective(t *testing.T) {
	c := Config{Provider: "yandex"}
	h, p, s := c.Effective()
	if h != "smtp.yandex.ru" || p != 465 || s != SecSSL {
		t.Fatalf("yandex preset: %s %d %s", h, p, s)
	}
	c = Config{Provider: "custom", Host: "mail.example", Port: 587, Security: "starttls"}
	h, p, s = c.Effective()
	if h != "mail.example" || p != 587 || s != SecSTARTTLS {
		t.Fatalf("custom: %s %d %s", h, p, s)
	}
}
