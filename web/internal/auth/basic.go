package auth

import (
	"container/list"
	"crypto/sha256"
	"sync"
	"time"
)

// BasicCache — кэш успешных Basic-проверок (пароль → PAM). Кэшируются ТОЛЬКО
// успешные проверки; неверные всегда идут в PAM. LRU, TTL по умолчанию 5 мин.
type BasicCache struct {
	mu    sync.Mutex
	auth  Authenticator
	user  string
	ttl   time.Duration
	max   int
	ll    *list.List // элементы — *basicEntry (front = недавние)
	items map[[32]byte]*list.Element
}

type basicEntry struct {
	key    [32]byte
	expiry time.Time
}

func NewBasicCache(a Authenticator, user string, ttl time.Duration, max int) *BasicCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if max <= 0 {
		max = 32
	}
	return &BasicCache{auth: a, user: user, ttl: ttl, max: max, ll: list.New(), items: make(map[[32]byte]*list.Element)}
}

// Invalidate очищает кэш (напр. при смене пароля).
func (c *BasicCache) Invalidate() {
	c.mu.Lock()
	c.ll.Init()
	c.items = make(map[[32]byte]*list.Element)
	c.mu.Unlock()
}

// Check возвращает true, если пара валидна. Успех кэшируется на ttl.
func (c *BasicCache) Check(user, pass string) bool {
	if user != c.user {
		return false
	}
	k := sha256.Sum256([]byte(user + ":" + pass))
	now := time.Now()

	c.mu.Lock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*basicEntry)
		if now.Before(e.expiry) {
			c.ll.MoveToFront(el)
			c.mu.Unlock()
			return true
		}
		c.ll.Remove(el)
		delete(c.items, k)
	}
	c.mu.Unlock()

	if err := c.auth.Authenticate(user, pass); err != nil {
		return false
	}

	c.mu.Lock()
	if el, ok := c.items[k]; ok {
		c.ll.Remove(el)
	}
	c.items[k] = c.ll.PushFront(&basicEntry{key: k, expiry: now.Add(c.ttl)})
	for c.ll.Len() > c.max {
		back := c.ll.Back()
		if back == nil {
			break
		}
		delete(c.items, back.Value.(*basicEntry).key)
		c.ll.Remove(back)
	}
	c.mu.Unlock()
	return true
}
