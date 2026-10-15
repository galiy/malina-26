package main

import (
	"fmt"
	"sync"
)

// idRegistry — реестр идентификаторов (arch §9): BASE↔ID, привязка, сквозной счётчик.
type idRegistry struct {
	mu  sync.Mutex
	f   identFile
	dir string
}

func newIDRegistry(f identFile) *idRegistry { return &idRegistry{f: f} }

// assignID возвращает ID для (base, binding), создавая новый при необходимости.
func (r *idRegistry) assignID(base, binding string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 1) точное совпадение base+binding
	for _, e := range r.f.Entries {
		if e.Base == base && e.Binding == binding && binding != "" {
			return e.ID, false
		}
	}
	// 2) запись с base и без привязки — переиспользуем, фиксируем привязку
	for i := range r.f.Entries {
		if r.f.Entries[i].Base == base && r.f.Entries[i].Binding == "" {
			r.f.Entries[i].Binding = binding
			_ = saveIdent(r.f)
			return r.f.Entries[i].ID, false
		}
	}
	// 3) новая запись
	r.f.Counter++
	id := fmt.Sprintf("%s-%05d", base, r.f.Counter)
	r.f.Entries = append(r.f.Entries, identEntry{Base: base, ID: id, Binding: binding})
	_ = saveIdent(r.f)
	return id, true
}
