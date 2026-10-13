package events

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/web/internal/mapmode"
)

// Snapshot — текущие данные устройства-источника.
type Snapshot struct {
	Serial string
	Kind   string
	Model  string // T/D/H/P (МАП) или kes
	Fields map[string]uint32
	OK     bool
}

// Provider отдаёт снимки устройств (МАП сейчас; КЭС — заглушки).
// Available — существует ли устройство (для target; учитывает локальный блэклист).
type Provider interface {
	Snapshot(serial, kind string) (Snapshot, bool)
	Available(serial, kind string) bool
}

// Actions — исполнители действий (E-mail, реле, команды МАП, SNMP-trap).
type Actions interface {
	Email(subject, body, recipient string) error
	Relay(serial, kind string, relayNo int, on bool) error
	MacCmd(serial, kind, cmd string) error // mac_stop_gen|mac_stop|mac_start
	// SNMPTrap отправляет v2c-trap. ok=false — фронт условия; ok=true — «Ok»
	// (выход условия в норму; если у поля нет Ok-нотификации — действие разумно
	// не отправлять, реализация сама отбивает).
	SNMPTrap(host, field string, value float64, mpptNo int, ok bool) error
}

// LogFn — запись в лог (отдельный лог менеджера событий).
type LogFn func(result string, fields map[string]any)

// DefaultStatePath — состояние правил в tmpfs (переживает рестарт сервиса).
const DefaultStatePath = "/dev/shm/malina-events/state.json"

// DefaultInterval — период оценки правил.
const DefaultInterval = 30 * time.Second

// persistedState — сериализуемое состояние правила.
type persistedState struct {
	Trigger   bool   `json:"trigger"`
	LastFire  int64  `json:"last_fire"` // UnixNano
	FiredText string `json:"fired_text"`
	Sig       string `json:"sig"`
}

// Engine — движок правил.
type Engine struct {
	path      string
	statePath string
	prov      Provider
	act       Actions
	log       LogFn
	interval  time.Duration

	// Тексты режима (задаются из web, зависят от языка):
	//   ModeLabel(code)            — статический текст по номеру (для настройки/списка; 9 → «Прочие режимы»)
	//   ModeText(code, statusCh)   — текущий текст (для 9 — с числом/двоичным StatusCh)
	ModeLabel func(code uint32) string
	ModeText  func(code, statusCh uint32) string

	mu    sync.Mutex
	state map[string]*ruleState
}

type ruleState struct {
	trigger   bool
	lastFire  time.Time
	sig       string // «подпись» содержания правила; при изменении — сброс
	firedText string // текст режима на момент срабатывания (для обратного письма)
}

// signature — подпись правила (без id/enabled/cooldown): при её изменении
// состояние (trigger/кулдаун) сбрасываем, начинаем с чистого листа.
func signature(r *Event) string {
	return fmt.Sprintf("%s|%s|%s|%d|%s|%s|%s|%s|%s|%s|%s|%s",
		r.Source.Kind, r.Source.Serial, r.Type, r.MPPTNo, r.Field, r.Op, r.Value,
		r.Action, r.Target.Kind, r.Target.Serial, r.Recipient, r.TrapHost)
}

// New создаёт движок (interval<=0 → DefaultInterval).
func New(path string, prov Provider, act Actions, log LogFn) *Engine {
	if path == "" {
		path = DefaultPath
	}
	e := &Engine{path: path, statePath: DefaultStatePath, prov: prov, act: act, log: log,
		interval: DefaultInterval, state: map[string]*ruleState{}}
	e.loadState()
	return e
}

// SetStatePath — путь файла состояния (пусто → DefaultStatePath). Для тестов/конфига.
func (e *Engine) SetStatePath(p string) {
	e.mu.Lock()
	e.statePath = p
	e.mu.Unlock()
	e.loadState()
}

// loadState читает состояние из tmpfs в память.
func (e *Engine) loadState() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.statePath == "" {
		return
	}
	b, err := os.ReadFile(e.statePath)
	if err != nil {
		return
	}
	var m map[string]persistedState
	if json.Unmarshal(b, &m) != nil {
		return
	}
	for id, ps := range m {
		st := &ruleState{trigger: ps.Trigger, firedText: ps.FiredText, sig: ps.Sig}
		if ps.LastFire != 0 {
			st.lastFire = time.Unix(0, ps.LastFire)
		}
		e.state[id] = st
	}
}

// saveState пишет состояние в tmpfs (атомарно). Вызывается при изменениях.
func (e *Engine) saveState() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.statePath == "" {
		return
	}
	m := make(map[string]persistedState, len(e.state))
	for id, st := range e.state {
		var lf int64
		if !st.lastFire.IsZero() {
			lf = st.lastFire.UnixNano()
		}
		m[id] = persistedState{Trigger: st.trigger, LastFire: lf, FiredText: st.firedText, Sig: st.sig}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(e.statePath), 0o755); err != nil {
		return
	}
	tmp := e.statePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, e.statePath)
}

// Run — цикл оценки (останавливается по ctx).
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			e.tick(now)
		}
	}
}

func (e *Engine) tick(now time.Time) {
	cfg, err := Load(e.path)
	if err != nil {
		return
	}
	// Чистим состояния правил, которых больше нет в конфиге.
	alive := make(map[string]bool, len(cfg.Events))
	for i := range cfg.Events {
		alive[cfg.Events[i].ID] = true
	}
	e.mu.Lock()
	pruned := false
	for id := range e.state {
		if !alive[id] {
			delete(e.state, id)
			pruned = true
		}
	}
	e.mu.Unlock()
	if pruned {
		e.saveState()
	}

	for i := range cfg.Events {
		e.evalOne(&cfg.Events[i], now)
	}
}

func (e *Engine) evalOne(r *Event, now time.Time) {
	sig := signature(r)
	e.mu.Lock()
	st := e.state[r.ID]
	if st == nil || st.sig != sig {
		// Новое правило или содержимое изменилось → начинаем с чистого листа.
		// Как в legacy: «стоп генерации» стартует с trigger=1 (не сработать сразу).
		st = &ruleState{trigger: r.Type == "time" || r.Action == "mac_stop_gen", sig: sig}
		e.state[r.ID] = st
		e.mu.Unlock()
		e.saveState()
	} else {
		e.mu.Unlock()
	}

	if !r.Enabled {
		return
	}
	cd := time.Duration(r.Cooldown) * time.Second
	if r.Type == "time" {
		cd = 23*time.Hour + 59*time.Minute // «свой» кулдаун времени: фактически раз в сутки
	}
	if cd > 0 && !st.lastFire.IsZero() && now.Sub(st.lastFire) < cd {
		return // в кулдауне правило не проверяем
	}

	cond, val, sc, ok := e.condition(r, now)
	if !ok {
		return
	}
	if cond {
		if !st.trigger {
			// Устройство-получатель должно существовать в списке; иначе событие
			// игнорируем (не срабатываем и не ставим trigger), повторим позже.
			if deviceAction(r.Action) {
				serial, kind := e.targetDev(r)
				if !e.prov.Available(serial, kind) {
					return
				}
			}
			st.trigger = true
			st.lastFire = now
			st.firedText = e.curText(r, val, sc)
			e.fire(r, val, st.firedText)
			e.saveState()
		}
	} else if st.trigger {
		e.ok(r, val, sc, st.firedText)
		st.trigger = false
		st.firedText = ""
		e.saveState()
	}
}

// curText — текущий текстовый режим (для mode — по номеру+StatusCh, иначе — поле/значение).
func (e *Engine) curText(r *Event, val float64, sc uint32) string {
	if r.Type == "mode" {
		if e.ModeText != nil {
			return e.ModeText(uint32(val), sc)
		}
		return strconv.Itoa(int(val))
	}
	return fmtVal(val, r)
}

// labelText — статический текст по номеру режима (для настроенного значения).
func (e *Engine) labelText(code uint32) string {
	if e.ModeLabel != nil {
		return e.ModeLabel(code)
	}
	return strconv.Itoa(int(code))
}

// deviceAction — действия, требующие устройства-получателя (реле и команды МАП).
func deviceAction(action string) bool {
	if _, _, isRelay := RelayAction(action); isRelay {
		return true
	}
	switch action {
	case "mac_stop_gen", "mac_stop", "mac_start":
		return true
	}
	return false
}

// condition → (условие, значение, statusCh, ok). statusCh используется для текста режима (9).
func (e *Engine) condition(r *Event, now time.Time) (bool, float64, uint32, bool) {
	if r.Type == "time" {
		mm := now.Hour()*60 + now.Minute()
		target, err := parseHHMM(r.Value)
		if err != nil {
			return false, 0, 0, false
		}
		return mm == target, 0, 0, true
	}
	snap, ok := e.prov.Snapshot(r.Source.Serial, r.Source.Kind)
	if !ok || !snap.OK {
		return false, 0, 0, false
	}
	if r.Type == "mode" {
		mode := mapmode.Mode(func(name string) uint32 {
			if name == "UNET" {
				if v := snap.Fields["UNET"]; v != 0 {
					return v + 100 // как в mapd
				}
				return 0
			}
			return snap.Fields[name]
		})
		target, _ := strconv.Atoi(strings.TrimSpace(r.Value))
		return float64(mode) == float64(target), float64(mode), snap.Fields["StatusCh"], true
	}
	// param
	v, ok := ResolveParam(r.Field, snap.Fields)
	if !ok {
		return false, 0, 0, false
	}
	tv, err := strconv.ParseFloat(strings.TrimSpace(r.Value), 64)
	if err != nil {
		return false, v, 0, true
	}
	switch r.Op {
	case "le":
		return v <= tv, v, 0, true
	case "ge":
		return v >= tv, v, 0, true
	case "eq":
		return v == tv, v, 0, true
	case "ne":
		return v != tv, v, 0, true
	}
	return false, v, 0, true
}

func (e *Engine) targetDev(r *Event) (serial, kind string) {
	if r.Target.Serial != "" {
		k := r.Target.Kind
		if k == "" {
			k = r.Source.Kind
		}
		return r.Target.Serial, k
	}
	return r.Source.Serial, r.Source.Kind
}

func (e *Engine) fire(r *Event, val float64, modeText string) {
	subject := fmt.Sprintf("Событие: %s = %s", evField(r), evValue(r))
	body := fmt.Sprintf("Правило %s сработало.\nИсточник: %s #%s\n%s = %s", r.ID, r.Source.Kind, r.Source.Serial, evField(r), fmtVal(val, r))
	if r.Type == "mode" {
		subject = "Событие: Режим " + modeText
		body = fmt.Sprintf("Правило %s сработало.\nИсточник: %s #%s\nРежим: %s", r.ID, r.Source.Kind, r.Source.Serial, modeText)
	}
	var err error
	if relayNo, on, isRelay := RelayAction(r.Action); isRelay {
		// Реле — на устройстве-получателе (target; пусто → источник).
		s, k := e.targetDev(r)
		err = e.act.Relay(s, k, relayNo, on)
	} else {
		switch r.Action {
		case "email":
			err = e.act.Email(subject, body, r.Recipient)
		case "mac_stop_gen":
			s, k := e.targetDev(r)
			err = e.act.MacCmd(s, k, "mac_stop_gen")
		case "mac_stop":
			s, k := e.targetDev(r)
			err = e.act.MacCmd(s, k, "mac_stop")
		case "mac_start":
			s, k := e.targetDev(r)
			err = e.act.MacCmd(s, k, "mac_start")
		case "snmp":
			err = e.act.SNMPTrap(r.TrapHost, r.Field, val, r.MPPTNo, false)
		}
	}
	e.logResult("fire", r, val, err)
}

func (e *Engine) ok(r *Event, val float64, sc uint32, _ string) {
	if r.Type == "mode" {
		// Обратное событие: режим перестал быть равным настроенному.
		if r.Action != "email" {
			return
		}
		code, _ := strconv.Atoi(strings.TrimSpace(r.Value))
		cur := e.curText(r, val, sc)
		err := e.act.Email(
			fmt.Sprintf("Режим вышел из «%s» и стал «%s»", e.labelText(uint32(code)), cur),
			fmt.Sprintf("Правило %s: режим вышел из «%s» и стал «%s».", r.ID, e.labelText(uint32(code)), cur),
			r.Recipient)
		e.logResult("ok", r, val, err)
		return
	}
	// «Ok» — только для E-mail/SNMP и только для числовых условий (param).
	if r.Action != "email" && r.Action != "snmp" {
		return
	}
	if r.Type != "param" {
		return
	}
	var err error
	if r.Action == "email" {
		err = e.act.Email("Событие OK: "+evField(r),
			fmt.Sprintf("Правило %s вернулось в норму.\n%s = %s", r.ID, evField(r), fmtVal(val, r)), r.Recipient)
	} else if r.Action == "snmp" {
		// «Ok»-trap при выходе условия в норму (если у поля есть Ok-нотификация).
		err = e.act.SNMPTrap(r.TrapHost, r.Field, val, r.MPPTNo, true)
	}
	e.logResult("ok", r, val, err)
}

func (e *Engine) logResult(kind string, r *Event, val float64, err error) {
	if e.log == nil {
		return
	}
	res := "ok"
	if err != nil {
		res = "err"
	}
	fields := map[string]any{
		"kind": kind, "result": res, "rule": r.ID, "action": r.Action,
		"source": r.Source.Kind + "#" + r.Source.Serial,
		"field":  evField(r), "value": fmtVal(val, r),
	}
	if err != nil {
		fields["error"] = err.Error()
	}
	e.log("events", fields)
}

func evField(r *Event) string {
	switch r.Type {
	case "time":
		return "TIME"
	case "mode":
		return "MODE"
	}
	return r.Field
}

func evValue(r *Event) string { return r.Value }

func fmtVal(val float64, r *Event) string {
	switch r.Type {
	case "time":
		return r.Value
	case "mode":
		return strconv.Itoa(int(val))
	}
	return strconv.FormatFloat(val, 'f', -1, 64)
}
