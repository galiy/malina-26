// Package events — «Менеджер событий»: правила «если параметр устройства по условию →
// действие», оценка раз в 2 c, фронт условия + «Ok», кулдаун на правило.
// Требования — docs/events.md. Legacy-API не используется.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultPath — файл правил.
const DefaultPath = "/malina/etc/malina/events.json"

// DefaultCooldown — кулдаун по умолчанию, c.
const DefaultCooldown = 30

// Device — ссылка на устройство (источник/получатель).
type Device struct {
	Kind   string `json:"kind,omitempty"`   // map | kes (пусто = текущее/источник)
	Serial string `json:"serial,omitempty"` // числовой серийник без буквы
}

// Event — правило.
type Event struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"` // param|time|mode
	Source    Device `json:"source"`
	MPPTNo    int    `json:"mppt_no,omitempty"`
	Field     string `json:"field,omitempty"`
	Op        string `json:"op"`    // le|ge|eq|ne|time|mode
	Value     string `json:"value"` // порог / HH:MM / код режима
	Cooldown  int    `json:"cooldown"`
	Action    string `json:"action"` // email|relayN_on|relayN_off|mac_stop_gen|mac_stop|mac_start|snmp
	Target    Device `json:"target"`
	Recipient string `json:"recipient,omitempty"` // для email; пусто — из настроек
	TrapHost  string `json:"trap_host,omitempty"`
}

// Config — версионированный список правил.
type Config struct {
	Version int     `json:"version"`
	Events  []Event `json:"events"`
}

var allowedTypes = map[string]bool{"param": true, "time": true, "mode": true}
var allowedOps = map[string]bool{"le": true, "ge": true, "eq": true, "ne": true}
var allowedActions = map[string]bool{
	"email":     true,
	"relay1_on": true, "relay1_off": true,
	"relay2_on": true, "relay2_off": true,
	"relay3_on": true, "relay3_off": true,
	"mac_stop_gen": true, "mac_stop": true, "mac_start": true,
	"snmp": true,
}

// RelayAction — если действие — управление реле, вернуть (номер реле, вкл?).
func RelayAction(action string) (int, bool, bool) {
	switch action {
	case "relay1_on":
		return 1, true, true
	case "relay1_off":
		return 1, false, true
	case "relay2_on":
		return 2, true, true
	case "relay2_off":
		return 2, false, true
	case "relay3_on":
		return 3, true, true
	case "relay3_off":
		return 3, false, true
	}
	return 0, false, false
}

var idRe = regexp.MustCompile(`^[0-9a-f]{8}$`)
var serialRe = regexp.MustCompile(`^[0-9]+$`)

// ValidID — id строго 8 hex (защита путей).
func ValidID(s string) bool { return idRe.MatchString(s) }

func newID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Load читает конфиг (отсутствие файла — пустой).
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

// Save валидирует и атомарно пишет (0600).
func Save(path string, c Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Events == nil {
		c.Events = []Event{}
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

// Normalize проставляет дефолты (id, cooldown).
func (e *Event) Normalize() error {
	if e.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		e.ID = id
	}
	if e.Cooldown == 0 {
		e.Cooldown = DefaultCooldown
	}
	if e.Source.Kind == "" {
		e.Source.Kind = "map"
	}
	if e.Type == "" { // миграция: тип из старого поля op
		switch e.Op {
		case "time":
			e.Type = "time"
		case "mode":
			e.Type = "mode"
		default:
			e.Type = "param"
		}
	}
	return nil
}

// Validate проверяет и нормализует конфиг.
func Validate(c Config) error {
	ids := map[string]bool{}
	for i := range c.Events {
		e := &c.Events[i]
		if err := e.Normalize(); err != nil {
			return err
		}
		if !ValidID(e.ID) {
			return fmt.Errorf("правило %d: некорректный id", i+1)
		}
		if ids[e.ID] {
			return fmt.Errorf("дубль id %s", e.ID)
		}
		ids[e.ID] = true
		if !serialRe.MatchString(e.Source.Serial) {
			return fmt.Errorf("правило %d: обязателен числовой серийник источника", i+1)
		}
		if e.Source.Kind != "map" && e.Source.Kind != "kes" {
			return fmt.Errorf("правило %d: тип источника map|kes", i+1)
		}
		if !allowedTypes[e.Type] {
			return fmt.Errorf("правило %d: недопустимый тип события %q", i+1, e.Type)
		}
		if !allowedActions[e.Action] {
			return fmt.Errorf("правило %d: недопустимое действие %q", i+1, e.Action)
		}
		switch e.Type {
		case "param":
			if !allowedOps[e.Op] {
				return fmt.Errorf("правило %d: недопустимое условие %q", i+1, e.Op)
			}
			if !IsParam(e.Field) {
				return fmt.Errorf("правило %d: недопустимый параметр %q", i+1, e.Field)
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(e.Value), 64); err != nil {
				return fmt.Errorf("правило %d: значение — число", i+1)
			}
		case "time":
			if _, err := parseHHMM(e.Value); err != nil {
				return fmt.Errorf("правило %d: время в формате HH:MM", i+1)
			}
		case "mode":
			if _, err := strconv.Atoi(strings.TrimSpace(e.Value)); err != nil {
				return fmt.Errorf("правило %d: код режима — число", i+1)
			}
		}
		if e.Target.Serial != "" && !serialRe.MatchString(e.Target.Serial) {
			return fmt.Errorf("правило %d: серийник получателя — числовой", i+1)
		}
	}
	return nil
}

func parseHHMM(s string) (int, error) {
	p := strings.Split(strings.TrimSpace(s), ":")
	if len(p) != 2 {
		return 0, fmt.Errorf("bad hh:mm")
	}
	h, err1 := strconv.Atoi(p[0])
	m, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("bad hh:mm")
	}
	return h*60 + m, nil
}
