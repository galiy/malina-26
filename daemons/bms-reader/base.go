package main

import (
	"strings"
	"unicode"
)

// sanitizeBase приводит BASE к виду, пригодному для ID, shm и пути tsdb.
//
// ID = BASE (без суффикса и без реестра). BASE должен быть непустым, без `/`,
// NUL, управляющих символов (unicode.IsControl) и ведущих/хвостовых пробелов,
// не `.`/`..`. Ограничение — idMax (50) БАЙТ; обрезка не должна разрывать UTF-8.
func sanitizeBase(s string) string {
	out, _ := sanitizeBaseN(s)
	return out
}

// sanitizeBaseN — sanitizeBase + признак того, что строка была обрезана по
// лимиту idMax (для предупреждения в лог).
func sanitizeBaseN(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "BMS", false
	}
	if len(out) <= idMax {
		return out, false
	}
	cut := idMax
	for cut > 0 && out[cut]&0xC0 == 0x80 { // continuation byte
		cut--
	}
	out = strings.TrimSpace(out[:cut])
	// Повторная проверка: после обрезки строка снова может стать `.`/`..`.
	if out == "" || out == "." || out == ".." {
		out = "BMS"
	}
	return out, true
}
