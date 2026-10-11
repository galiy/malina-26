// Package masternode — «Мастер-Узел»: список узлов и ленивый кэширующий прокси
// к их legacy-ручкам (см. docs/master-node.md).
package masternode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultPath — путь к конфигу узлов мастер-узла.
const DefaultPath = "/malina/etc/malina/master/nodes.json"

// DefaultCacheDir — каталог кэша в tmpfs.
const DefaultCacheDir = "/dev/shm/malina-master"

// MaxNodes — лимит числа узлов.
const MaxNodes = 30

// Node — один узел «Малины», опрашиваемый по legacy-ручкам.
type Node struct {
	ID     string `json:"id"`     // стабильный id (8 hex), генерируется при добавлении
	Name   string `json:"name"`   // подпись, ≤ 32
	Scheme string `json:"scheme"` // http | https
	Host   string `json:"host"`   // IPv4 или hostname
	Port   int    `json:"port"`   // TCP-порт, по умолчанию 80
}

// Config — версионированный список узлов.
type Config struct {
	Version int    `json:"version"`
	Nodes   []Node `json:"nodes"`
}

// Load читает конфиг; отсутствие файла — пустой список (не ошибка).
func Load(path string) (Config, error) {
	c := Config{Version: 1}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version == 0 {
		c.Version = 1
	}
	return c, nil
}

// Save валидирует и атомарно пишет конфиг (0600).
func Save(path string, c Config) error {
	if err := Validate(&c); err != nil {
		return err
	}
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Nodes == nil {
		c.Nodes = []Node{}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

// NewID — новый id узла (8 hex).
func NewID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var idRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidID — id узла строго 8 hex-символов (защита путей кэша от traversal).
func ValidID(s string) bool { return idRe.MatchString(s) }

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// validHost — IPv4 или корректное имя хоста.
func validHost(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	return hostRe.MatchString(s)
}

// Normalize приводит узел к канону: дефолты scheme/port, обрезка пробелов/id.
func (n *Node) Normalize() error {
	n.Name = strings.TrimSpace(n.Name)
	n.Scheme = strings.ToLower(strings.TrimSpace(n.Scheme))
	n.Host = strings.TrimSpace(n.Host)
	if n.Scheme == "" {
		n.Scheme = "http"
	}
	if n.Port == 0 {
		n.Port = 80
	}
	if n.ID == "" {
		id, err := NewID()
		if err != nil {
			return err
		}
		n.ID = id
	}
	return nil
}

// Validate проверяет и нормализует список (лимит, поля, дубли host:port).
func Validate(c *Config) error {
	if len(c.Nodes) > MaxNodes {
		return fmt.Errorf("слишком много узлов: %d (макс %d)", len(c.Nodes), MaxNodes)
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for i := range c.Nodes {
		n := &c.Nodes[i]
		if err := n.Normalize(); err != nil {
			return err
		}
		if n.Scheme != "http" && n.Scheme != "https" {
			return fmt.Errorf("узел %q: scheme должен быть http или https", n.Name)
		}
		if n.Name == "" || len([]rune(n.Name)) > 32 {
			return fmt.Errorf("узел %d: имя 1..32 символов", i+1)
		}
		if !ValidID(n.ID) {
			return fmt.Errorf("узел %q: некорректный id %q", n.Name, n.ID)
		}
		if !validHost(n.Host) {
			return fmt.Errorf("узел %q: некорректный host", n.Name)
		}
		if n.Port < 1 || n.Port > 65535 {
			return fmt.Errorf("узел %q: порт 1..65535", n.Name)
		}
		key := fmt.Sprintf("%s:%d", strings.ToLower(n.Host), n.Port)
		if seen[key] {
			return fmt.Errorf("дубль узла %s", key)
		}
		seen[key] = true
		if ids[n.ID] {
			return fmt.Errorf("дубль id %s", n.ID)
		}
		ids[n.ID] = true
	}
	return nil
}

// BaseURL — базовый адрес узла, напр. http://host:80.
func (n Node) BaseURL() string {
	return fmt.Sprintf("%s://%s:%d", n.Scheme, n.Host, n.Port)
}

// Find — узел по id.
func (c Config) Find(id string) (Node, bool) {
	for _, n := range c.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}
