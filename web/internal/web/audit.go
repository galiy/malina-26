package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

// clientIP — адрес клиента. За TLS-прокси реальный адрес приходит в
// X-Forwarded-For (Apache его проставляет); иначе берём RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// tokenPrefix — префикс sha256 токена (8 hex-символов) для корреляции событий
// одного клиента без раскрытия самого токена.
func tokenPrefix(tok string) string {
	if tok == "" {
		return ""
	}
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:4])
}
