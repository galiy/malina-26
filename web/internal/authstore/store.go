package authstore

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync"
	"syscall"
	"time"
)

// Хранилище сессий/токенов в POSIX shared memory (tmpfs).
//
//	[header 64B][slots × slotSize]
//
// Живёт перезапуск malina-web, умирает при ребуте. Пишет/читает только процесс
// web, поэтому синхронизация — мьютекс в процессе (seqlock не нужен).
// Хранится ХЕШ токена (sha256), не сам токен.
const (
	magic     = 0x5455414D // "MAUT"
	version   = 1
	headerLen = 64
	slots     = 4096
	slotSize  = 64

	offState    = 0  // uint32: 0 свободен / 1 занят
	offHash     = 8  // [32]byte sha256(token)
	offCreated  = 40 // uint64 unix
	offLastUsed = 48 // uint64 unix
)

// Store — хранилище.
type Store struct {
	mu        sync.Mutex
	data      []byte
	f         *os.File
	idle      time.Duration
	done      chan struct{}
	closeOnce sync.Once
	// onExpire вызывается при истечении простаивающей записи (для аудита);
	// задаётся через SetOnExpire до запуска reaper'а.
	onExpire func(hashPrefix string, lastUsed time.Time)
}

// SetOnExpire задаёт колбэк истечения записи (hashPrefix — 8 hex-символов
// sha256 токена, lastUsed — время последнего использования).
func (s *Store) SetOnExpire(fn func(hashPrefix string, lastUsed time.Time)) {
	s.mu.Lock()
	s.onExpire = fn
	s.mu.Unlock()
}

func hashPrefix(h [hashLen]byte) string { return hex.EncodeToString(h[:4]) }

// NewToken — случайный токен (64 hex-символа).
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Open открывает/создаёт сегмент. idle — срок простоя (скользящий TTL).
func Open(path string, idle time.Duration) (*Store, error) {
	size := headerLen + slots*slotSize
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, err
	}
	_ = f.Chmod(0o600) // на случай уже существующего файла
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	s := &Store{data: data, f: f, idle: idle, done: make(chan struct{})}
	// Валидный заголовок — родовая раскладка; иначе переинициализация.
	if binary.LittleEndian.Uint32(data[0:4]) != magic ||
		binary.LittleEndian.Uint32(data[4:8]) != version ||
		binary.LittleEndian.Uint32(data[8:12]) != slotSize ||
		binary.LittleEndian.Uint32(data[12:16]) != slots {
		for i := range data {
			data[i] = 0
		}
		binary.LittleEndian.PutUint32(data[0:4], magic)
		binary.LittleEndian.PutUint32(data[4:8], version)
		binary.LittleEndian.PutUint32(data[8:12], slotSize)
		binary.LittleEndian.PutUint32(data[12:16], slots)
	}
	go s.reapLoop()
	return s, nil
}

func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		_ = syscall.Munmap(s.data)
		err = s.f.Close()
	})
	return err
}

func (s *Store) slot(i int) []byte {
	off := headerLen + i*slotSize
	return s.data[off : off+slotSize]
}

func hashOf(token string) [hashLen]byte { return sha256.Sum256([]byte(token)) }

const hashLen = 32

func (s *Store) find(h [hashLen]byte) int {
	for i := 0; i < slots; i++ {
		sl := s.slot(i)
		if binary.LittleEndian.Uint32(sl[offState:offState+4]) != 1 {
			continue
		}
		if subtle.ConstantTimeCompare(sl[offHash:offHash+hashLen], h[:]) == 1 {
			return i
		}
	}
	return -1
}

// Issue сохраняет новый токен (вытесняет самый простаивающий при переполнении).
func (s *Store) Issue(token string) {
	h := hashOf(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	free := -1
	oldest, oldestIdx := uint64(1<<63-1), -1
	for i := 0; i < slots; i++ {
		sl := s.slot(i)
		if binary.LittleEndian.Uint32(sl[offState:offState+4]) != 1 {
			if free < 0 {
				free = i
			}
			continue
		}
		if lu := binary.LittleEndian.Uint64(sl[offLastUsed : offLastUsed+8]); lu < oldest {
			oldest, oldestIdx = lu, i
		}
	}
	idx := free
	if idx < 0 {
		idx = oldestIdx
	}
	sl := s.slot(idx)
	for i := range sl {
		sl[i] = 0
	}
	copy(sl[offHash:offHash+hashLen], h[:])
	binary.LittleEndian.PutUint64(sl[offCreated:offCreated+8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(sl[offLastUsed:offLastUsed+8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint32(sl[offState:offState+4], 1)
}

// Touch проверяет токен и продлевает срок (скользящий TTL). Протухший — удаляет.
func (s *Store) Touch(token string) bool {
	h := hashOf(token)
	now := time.Now()
	s.mu.Lock()
	for i := 0; i < slots; i++ {
		sl := s.slot(i)
		if binary.LittleEndian.Uint32(sl[offState:offState+4]) != 1 {
			continue
		}
		if subtle.ConstantTimeCompare(sl[offHash:offHash+hashLen], h[:]) != 1 {
			continue
		}
		last := int64(binary.LittleEndian.Uint64(sl[offLastUsed : offLastUsed+8]))
		if now.Sub(time.Unix(0, last)) >= s.idle {
			binary.LittleEndian.PutUint32(sl[offState:offState+4], 0)
			fn := s.onExpire
			s.mu.Unlock()
			if fn != nil {
				fn(hashPrefix(h), time.Unix(0, last))
			}
			return false
		}
		binary.LittleEndian.PutUint64(sl[offLastUsed:offLastUsed+8], uint64(now.UnixNano()))
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	return false
}

// Revoke удаляет токен.
func (s *Store) Revoke(token string) {
	h := hashOf(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.find(h); i >= 0 {
		binary.LittleEndian.PutUint32(s.slot(i)[offState:offState+4], 0)
	}
}

// PurgeAll удаляет все сессии/токены (смена пароля).
func (s *Store) PurgeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < slots; i++ {
		binary.LittleEndian.PutUint32(s.slot(i)[offState:offState+4], 0)
	}
}

func (s *Store) reapLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.purgeExpired()
		}
	}
}

func (s *Store) purgeExpired() {
	now := time.Now()
	type expired struct {
		prefix   string
		lastUsed time.Time
	}
	var list []expired
	s.mu.Lock()
	for i := 0; i < slots; i++ {
		sl := s.slot(i)
		if binary.LittleEndian.Uint32(sl[offState:offState+4]) != 1 {
			continue
		}
		last := int64(binary.LittleEndian.Uint64(sl[offLastUsed : offLastUsed+8]))
		if now.Sub(time.Unix(0, last)) >= s.idle {
			binary.LittleEndian.PutUint32(sl[offState:offState+4], 0)
			var h [hashLen]byte
			copy(h[:], sl[offHash:offHash+hashLen])
			list = append(list, expired{hashPrefix(h), time.Unix(0, last)})
		}
	}
	fn := s.onExpire
	s.mu.Unlock()
	if fn != nil {
		for _, e := range list {
			fn(e.prefix, e.lastUsed)
		}
	}
}
