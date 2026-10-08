package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// CookieName — имя cookie сессии.
const CookieName = "malina_session"

type entry struct {
	user    string
	expires time.Time
}

// Manager хранит сессии в памяти (волатильно: корень ro).
type Manager struct {
	mu    sync.Mutex
	ttl   time.Duration
	store map[string]entry
}

func NewManager(ttl time.Duration) *Manager {
	m := &Manager{ttl: ttl, store: make(map[string]entry)}
	go m.reapLoop()
	return m
}

// Create создаёт сессию и возвращает токен и срок жизни.
func (m *Manager) Create(user string) (string, time.Time) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	tok := hex.EncodeToString(b)
	exp := time.Now().Add(m.ttl)
	m.mu.Lock()
	m.store[tok] = entry{user: user, expires: exp}
	m.mu.Unlock()
	return tok, exp
}

// User возвращает пользователя по токену, если сессия валидна.
func (m *Manager) User(tok string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.store[tok]
	if !ok {
		return "", false
	}
	if time.Now().After(e.expires) {
		delete(m.store, tok)
		return "", false
	}
	return e.user, true
}

// Delete удаляет сессию.
func (m *Manager) Delete(tok string) {
	m.mu.Lock()
	delete(m.store, tok)
	m.mu.Unlock()
}

func (m *Manager) reapLoop() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		now := time.Now()
		m.mu.Lock()
		for k, e := range m.store {
			if now.After(e.expires) {
				delete(m.store, k)
			}
		}
		m.mu.Unlock()
	}
}
