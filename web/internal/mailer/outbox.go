package mailer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Message — письмо в аутбоксе.
type Message struct {
	ID          string    `json:"id"`
	Created     time.Time `json:"created"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body"`
	HTML        bool      `json:"html"`
	Recipient   string    `json:"recipient"`
	Attempts    int       `json:"attempts"`
	LastError   string    `json:"last_error,omitempty"`
	NextAttempt time.Time `json:"next_attempt"`
}

// Outbox — каталог писем (tmpfs). Все операции сериализованы мьютексом.
type Outbox struct {
	Dir string
	mu  sync.Mutex
}

var idRe = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidID — id строго 8 hex (защита путей).
func ValidID(s string) bool { return idRe.MatchString(s) }

// NewID — новый id (8 hex).
func NewID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (o *Outbox) path(id string) string { return filepath.Join(o.Dir, id+".json") }

// Add кладёт письмо в аутбокс; при переполнении удаляет старейшее.
func (o *Outbox) Add(m Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return err
	}
	if m.ID == "" {
		id, err := NewID()
		if err != nil {
			return err
		}
		m.ID = id
	}
	if m.Created.IsZero() {
		m.Created = time.Now()
	}
	if m.NextAttempt.IsZero() {
		m.NextAttempt = m.Created
	}
	if list := o.listLocked(); len(list) >= MaxOutbox {
		// список по убыванию Created → последний = старейшее
		_ = o.deleteLocked(list[len(list)-1].ID)
	}
	return o.writeLocked(m)
}

func (o *Outbox) write(m Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.writeLocked(m)
}

func (o *Outbox) writeLocked(m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	fp := o.path(m.ID)
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, fp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Get — письмо по id.
func (o *Outbox) Get(id string) (Message, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.getLocked(id)
}

func (o *Outbox) getLocked(id string) (Message, bool) {
	var m Message
	if !ValidID(id) {
		return m, false
	}
	b, err := os.ReadFile(o.path(id))
	if err != nil {
		return m, false
	}
	if json.Unmarshal(b, &m) != nil {
		return m, false
	}
	return m, true
}

// List — все письма, свежие сверху.
func (o *Outbox) List() ([]Message, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.listLocked(), nil
}

func (o *Outbox) listLocked() []Message {
	matches, err := filepath.Glob(filepath.Join(o.Dir, "*.json"))
	if err != nil {
		return nil
	}
	var out []Message
	for _, fp := range matches {
		b, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		var m Message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// Delete — удалить письмо.
func (o *Outbox) Delete(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.deleteLocked(id)
}

func (o *Outbox) deleteLocked(id string) error {
	if !ValidID(id) {
		return nil
	}
	err := os.Remove(o.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Clear — очистить аутбокс.
func (o *Outbox) Clear() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	matches, _ := filepath.Glob(filepath.Join(o.Dir, "*.json"))
	for _, fp := range matches {
		_ = os.Remove(fp)
	}
	return nil
}
