package main

import (
	"fmt"
	"strings"
	"sync"
)

// sanitizeBase — инвариант arch §9: исключить `/`, NUL, управляющие символы,
// ведущие/хвостовые пробелы, `.`/`..`; ограничить длину (ID = BASE-NNNNN ≤ 50).
func sanitizeBase(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "BMS"
	}
	if rs := []rune(out); len(rs) > 44 { // по рунам, не по байтам (Р2-4)
		out = string(rs[:44])
	}
	return out
}

// idRegistry — реестр идентификаторов (arch §9): BASE↔ID, привязка, сквозной счётчик.
//
// Упрощение v1 (относительно flowchart arch §9): нет проверки «запись активна/не
// активна» при матчинге (нет глобального набора активных BASE); семантика «привязка
// by-path живёт вечно / перезаписывается при смене BASE» не реализована. Для текущих
// типов (BASE = SN/ёмкость) достаточно; расширять при появлении конфликтующих типов.
type idRegistry struct {
	mu  sync.Mutex
	f   identFile
	dir string
}

func newIDRegistry(f identFile) *idRegistry { return &idRegistry{f: f} }

// assignID возвращает ID для (base, binding), создавая новый при необходимости.
func (r *idRegistry) assignID(base0, binding string) (string, bool) {
	base := sanitizeBase(base0)
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
