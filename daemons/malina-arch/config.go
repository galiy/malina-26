package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/galiy/malina-26/daemons/lib/logx"
)

const defConfig = "/malina/etc/malina/malina-arch.json"
const defKey = "/malina/etc/malina/arch.key"

type networkConfig struct {
	Protocol  string `json:"protocol"` // sftp|smb2
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Path      string `json:"path"`
	User      string `json:"user"`
	Password  string `json:"password_enc"` // base64(nonce||ciphertext)
	Domain    string `json:"domain"`
	HostKey   string `json:"host_key"`
	AccessSec int    `json:"access_timeout_sec"`
}

type flashConfig struct {
	UUID string `json:"uuid"`
}

type config struct {
	Enabled    bool          `json:"enabled"`
	Target     string        `json:"target"` // network|flash
	AvgScanMin int           `json:"avg_scan_min"`
	LogLevel   string        `json:"log_level"` // debug|info|warn|error
	Network    networkConfig `json:"network"`
	Flash      flashConfig   `json:"flash"`
}

func loadConfig(path string) (config, error) {
	var c config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.AvgScanMin <= 0 {
		c.AvgScanMin = 5
	}
	if c.Target == "" {
		c.Target = "network"
	}
	if _, ok := logx.ParseLevel(c.LogLevel); !ok {
		c.LogLevel = "info"
	}
	lx.Debugf("конфиг загружен: enabled=%v target=%s level=%s", c.Enabled, c.Target, c.LogLevel)
	return c, nil
}

func saveConfig(path string, c config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != 32 {
		return nil, errors.New("arch.key: ожидается 32 байта")
	}
	return b, nil
}

// archAAD — детерминированный AAD: malina-arch/<protocol>/<field>/v1.
// Значение из конфига не используется.
func archAAD(protocol, field string) string {
	return "malina-arch/" + protocol + "/" + field + "/v1"
}

func encryptSecret(key []byte, field, plain string) (string, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := g.Seal(nil, nonce, []byte(plain), []byte(field))
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func decryptSecret(key []byte, field, enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", errors.New("короткий шифртекст")
	}
	nonce, ct := raw[:g.NonceSize()], raw[g.NonceSize():]
	p, err := g.Open(nil, nonce, ct, []byte(field))
	if err == nil {
		return string(p), nil
	}
	// Совместимость с прежним AAD без версии: malina-arch/<protocol>/password.
	if legacy := strings.TrimSuffix(field, "/v1"); legacy != field {
		if p, lerr := g.Open(nil, nonce, ct, []byte(legacy)); lerr == nil {
			return string(p), nil
		}
	}
	return "", errors.New("не удалось расшифровать секрет (ключ или AAD не совпадают)")
}

func deviceID() string {
	for _, p := range []string{"/proc/device-tree/serial-number"} {
		if b, err := os.ReadFile(p); err == nil {
			s := strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
			s = strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
					return r
				}
				return -1
			}, s)
			if s != "" {
				return s
			}
		}
	}
	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return "unknown"
}

func keygen(path string) error {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	lx.Infof("arch.key создан: %s", path)
	return nil
}
