// Package audit — аудит-журнал действий web-сервиса.
//
// События пишутся одним JSON-объектом в syslog (через /dev/log) с отдельным
// тегом (по умолчанию "malina-web-audit"). На внешнем log-хосте
// штатная универсальная раскладка rsyslog раскладывает сообщения по
// %PROGRAMNAME%, поэтому наши события попадают в ОТДЕЛЬНЫЙ файл:
//
//	/var/log/remote/<HOSTNAME>-<fromhost-ip>/<category>/malina-web-audit.log
//
// Никаких доп. настроек на log-хосте и в системном логировании устройства не
// требуется: используется уже существующий форвард «всего» syslog на удалённый
// сервер и её generic-шаблон. Секреты (пароли/токены/куки) не логируются:
// см. MaskFields; токен — только префикс sha256.
package audit

import (
	"encoding/json"
	"log"
	"log/syslog"
	"strings"
	"sync"
	"time"
)

// DefaultTag — syslog-тег аудита (→ имя файла на log-хосте).
const DefaultTag = "malina-web-audit"

// Logger пишет аудит-события. Нулевое значение безопасно (выключено).
type Logger struct {
	enabled bool
	mu      sync.Mutex
	w       *syslog.Writer // nil, если /dev/log недоступен
	sink    func(string)   // задан только в тестах (перехват строки)
}

// New создаёт логгер. enabled=false — все вызовы Event ничего не делают.
// При недоступности /dev/log пишет в stderr (сервис не падает).
func New(enabled bool, tag string) *Logger {
	if !enabled {
		return &Logger{}
	}
	if tag == "" {
		tag = DefaultTag
	}
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, tag)
	if err != nil {
		log.Printf("audit: syslog (%s) недоступен: %v; пишу в stderr", tag, err)
	}
	return &Logger{enabled: true, w: w}
}

// NewWithSink — логгер с перехватом строки (для тестов).
func NewWithSink(enabled bool, sink func(string)) *Logger {
	return &Logger{enabled: enabled, sink: sink}
}

// Event пишет одно событие. Поля fields добавляются к базовым (ts, event).
func (l *Logger) Event(event string, fields map[string]any) {
	if l == nil || !l.enabled {
		return
	}
	m := make(map[string]any, len(fields)+2)
	m["ts"] = time.Now().UTC().Format(time.RFC3339)
	m["event"] = event
	for k, v := range MaskFields(fields) {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	l.emit(string(b))
}

func (l *Logger) emit(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sink != nil {
		l.sink(line)
		return
	}
	if l.w != nil {
		_ = l.w.Info(line)
		return
	}
	log.Print("AUDIT " + line)
}

// secretSubstr — подстроки ключей, значения которых маскируются.
//
// Поле "token" намеренно НЕ маскируется: во все события оно кладётся только как
// префикс sha256 (tokenPrefix), а не сам токен. Всё, что реально секрет —
// пароли/секреты/куки/заголовок авторизации — маскируется.
var secretSubstr = []string{"pass", "secret", "cookie", "authorization"}

// MaskFields возвращает копию карты, где значения «секретных» ключей заменены
// на "***". Ключ считается секретным, если содержит одну из secretSubstr
// (без учёта регистра).
func MaskFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if isSecretKey(k) {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

func isSecretKey(k string) bool {
	lk := strings.ToLower(k)
	for _, s := range secretSubstr {
		if strings.Contains(lk, s) {
			return true
		}
	}
	return false
}
