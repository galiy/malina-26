// Package mailer — отправка почты из сервиса: настройки/секрет, аутбокс, SMTP.
// Требования — docs/email.md.
package mailer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// DefaultConfigPath — настройки (без секрета).
	DefaultConfigPath = "/malina/etc/malina/email.json"
	// DefaultKeyPath — ключ шифрования секрета (генерируется при отсутствии).
	DefaultKeyPath = "/malina/var/spool/sys/debug/ticker"
	// DefaultOutboxDir — аутбокс в tmpfs (рестарт сервиса переживает, ребут хоста — нет).
	DefaultOutboxDir = "/dev/shm/malina-outbox"
	// MaxOutbox — лимит писем в аутбоксе.
	MaxOutbox = 200
)

// Security — тип защиты соединения.
type Security string

const (
	SecSSL      Security = "ssl"      // implicit TLS (обычно 465)
	SecSTARTTLS Security = "starttls" // STARTTLS (обычно 587)
	SecNone     Security = "none"
)

// Provider — пресет почтового сервиса.
type Provider struct {
	Host     string
	Port     int
	Security Security
}

// Providers — пресеты (ключ → настройки). custom — ручной ввод.
var Providers = map[string]Provider{
	"mailru":   {"smtp.mail.ru", 465, SecSSL},
	"yandex":   {"smtp.yandex.ru", 465, SecSSL},
	"gmail":    {"smtp.gmail.com", 465, SecSSL},
	"hosterby": {"smtp.hoster.by", 465, SecSSL},
	"qqmail":   {"smtp.qq.com", 465, SecSSL},
	"custom":   {"", 0, SecSSL},
}

// ProviderKeys — порядок для UI.
var ProviderKeys = []string{"mailru", "yandex", "gmail", "hosterby", "qqmail", "custom"}

// Config — настройки (секрет — только шифротекстом).
type Config struct {
	Provider  string `json:"provider"`
	From      string `json:"from"`
	Username  string `json:"username"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Security  string `json:"security,omitempty"`
	Recipient string `json:"recipient"`
	SecretEnc string `json:"secret_enc,omitempty"`
}

// Effective — host/port/security с учётом пресета.
func (c Config) Effective() (string, int, Security) {
	if p, ok := Providers[c.Provider]; ok && c.Provider != "custom" {
		return p.Host, p.Port, p.Security
	}
	sec := SecSSL
	switch c.Security {
	case string(SecSTARTTLS):
		sec = SecSTARTTLS
	case string(SecNone):
		sec = SecNone
	}
	return c.Host, c.Port, sec
}

// IsCustom — выбран ручной SMTP.
func (c Config) IsCustom() bool { return c.Provider == "custom" }

// Load читает настройки; отсутствие файла — пустой конфиг.
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Provider: "mailru", Security: string(SecSSL)}, nil
		}
		return c, err
	}
	if len(b) == 0 {
		return Config{Provider: "mailru", Security: string(SecSSL)}, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

// Save валидирует и атомарно пишет настройки (0600).
func Save(path string, c Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && strings.Contains(a.Address, "@")
}

// Validate — проверки настроек.
func Validate(c Config) error {
	if _, ok := Providers[c.Provider]; !ok {
		return fmt.Errorf("неизвестный сервис %q", c.Provider)
	}
	if !validEmail(c.From) {
		return fmt.Errorf("некорректный адрес отправителя")
	}
	if !validEmail(c.Recipient) {
		return fmt.Errorf("некорректный адрес получателя")
	}
	if strings.TrimSpace(c.Username) == "" {
		return fmt.Errorf("не задан логин")
	}
	host, port, _ := c.Effective()
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("не задан SMTP-сервер")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("некорректный порт")
	}
	return nil
}

// normalizeSecret убирает все пробельные символы из секрета: пароли приложений
// (Gmail/Яндекс/Mail.ru) показывают группами с пробелами — их вставляют как есть.
func normalizeSecret(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// --- секрет (AES-256-GCM, ключ из файла) ---

// keyMu сериализует доступ к файлу ключа (в т.ч. генерацию при отсутствии).
var keyMu sync.Mutex

func loadOrCreateKey(path string) ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	b, err := os.ReadFile(path)
	if err == nil {
		if len(b) == 32 {
			return b, nil
		}
		return nil, fmt.Errorf("ключ %s повреждён", path)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// encryptWithKey шифрует секрет ключом (base64: nonce|ct).
func encryptWithKey(key []byte, plain string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := g.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ct...)), nil
}

// decryptWithKey расшифровывает секрет ключом.
func decryptWithKey(key []byte, enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", fmt.Errorf("секрет повреждён")
	}
	nonce, ct := raw[:g.NonceSize()], raw[g.NonceSize():]
	pt, err := g.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// EncryptSecret шифрует секрет ключом устройства (base64).
func EncryptSecret(keyPath, plain string) (string, error) {
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return "", err
	}
	return encryptWithKey(key, plain)
}

// DecryptSecret расшифровывает секрет ключом устройства.
func DecryptSecret(keyPath, enc string) (string, error) {
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return "", err
	}
	return decryptWithKey(key, enc)
}
