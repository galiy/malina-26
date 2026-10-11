package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/galiy/malina-26/web/internal/events"
)

// mapSerials — доступные серийники МАП (для выпадающих списков).
func mapSerials() []string {
	var out []string
	for _, m := range loadMapList().Maps {
		if m.Serial == 0 || m.Serial == 0xFFFF {
			continue // «пустой» серийник — устройство игнорируем
		}
		out = append(out, strconv.FormatUint(uint64(m.Serial), 10))
	}
	return out
}

func (s *Server) eventsData(r *http.Request) map[string]any {
	data := s.baseData(r)
	data["Active"] = "events"
	if cfg, err := events.Load(s.eventsPath); err == nil {
		data["Events"] = cfg.Events
	}
	data["Types"] = []string{"param", "time", "mode"}
	data["Ops"] = []string{"le", "ge", "eq", "ne"}
	data["Actions"] = []string{
		"email",
		"relay1_on", "relay1_off", "relay2_on", "relay2_off", "relay3_on", "relay3_off",
		"mac_stop_gen", "mac_stop", "mac_start", "snmp",
	}
	data["Kinds"] = []string{"map", "kes"}
	data["MapSerials"] = mapSerials()
	data["Params"] = events.Params
	lang := s.lang(r)
	// Все коды 0..18; для 9 — статический текст из событий (панель у 9 динамическая).
	modePairs := make([][2]string, 0, 19)
	for i := 0; i < 19; i++ {
		modePairs = append(modePairs, [2]string{strconv.Itoa(i), s.modeLabelStatic(lang, uint32(i))})
	}
	data["Modes"] = modePairs
	data["EvI18N"] = map[string]string{
		"param": s.tr(lang, "events.type_param"), "time": s.tr(lang, "events.type_time"),
		"mode":     s.tr(lang, "events.type_mode"),
		"kind_map": s.tr(lang, "events.kind_map"), "kind_kes": s.tr(lang, "events.kind_kes"),
		"target_cur":   s.tr(lang, "events.target_cur"),
		"act_email":    s.tr(lang, "events.act_email"),
		"act_relay_on": s.tr(lang, "events.act_relay_on"), "act_relay_off": s.tr(lang, "events.act_relay_off"),
		"act_stop_gen": s.tr(lang, "events.act_stop_gen"), "act_stop": s.tr(lang, "events.act_stop"),
		"act_start": s.tr(lang, "events.act_start"), "act_snmp": s.tr(lang, "events.act_snmp"),
		"yes": s.tr(lang, "events.yes"), "no": s.tr(lang, "events.no"),
		"serial_ph": s.tr(lang, "events.serial_ph"), "del": s.tr(lang, "events.del"),
		"confirm_clear": s.tr(lang, "events.confirm_clear"),
	}
	// Списки для JS-шаблона новой строки.
	params := make([][2]string, 0, len(events.Params))
	for _, p := range events.Params {
		params = append(params, [2]string{p.Key, s.tr(lang, p.Label)})
	}
	actLabel := map[string]string{
		"email": "events.act_email", "snmp": "events.act_snmp",
		"relay1_on": "events.act_relay1_on", "relay1_off": "events.act_relay1_off",
		"relay2_on": "events.act_relay2_on", "relay2_off": "events.act_relay2_off",
		"relay3_on": "events.act_relay3_on", "relay3_off": "events.act_relay3_off",
		"mac_stop_gen": "events.act_stop_gen", "mac_stop": "events.act_stop", "mac_start": "events.act_start",
	}
	acts := make([][2]string, 0, len(data["Actions"].([]string)))
	for _, a := range data["Actions"].([]string) {
		acts = append(acts, [2]string{a, s.tr(lang, actLabel[a])})
	}
	data["EvParams"] = params
	data["EvModes"] = modePairs
	data["EvActions"] = acts
	return data
}

// handleEvents — «Система → Менеджер событий».
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	noFrameHeaders(w)
	data := s.eventsData(r)
	lang := s.lang(r)
	switch r.URL.Query().Get("ok") {
	case "save":
		data["Ok"] = s.tr(lang, "events.ok_save")
	case "clear":
		data["Ok"] = s.tr(lang, "events.ok_clear")
	}
	s.render(w, http.StatusOK, "events", data)
}

// handleEventsSave — сохранить список правил (или очистить).
func (s *Server) handleEventsSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()

	cfg := events.Config{Version: 1, Events: parseEventsForm(r)}
	// Пустой список сохраняем только с явным подтверждением (кнопка «Очистить»
	// в UI выставляет allow_empty=1). Пустая форма по API/скрипту отбивается.
	if len(cfg.Events) == 0 && r.FormValue("allow_empty") != "1" {
		data := s.eventsData(r)
		data["Err"] = s.tr(s.lang(r), "events.err_empty")
		s.render(w, http.StatusOK, "events", data)
		return
	}
	if err := events.Save(s.eventsPath, cfg); err != nil {
		data := s.eventsData(r)
		data["Events"] = cfg.Events
		data["Err"] = err.Error()
		s.render(w, http.StatusOK, "events", data)
		return
	}
	http.Redirect(w, r, "/system/events?ok=save", http.StatusSeeOther)
}

func parseEventsForm(r *http.Request) []events.Event {
	ids := r.Form["id"]
	enabled := r.Form["enabled"]
	types := r.Form["type"]
	srcKind := r.Form["source_kind"]
	srcSerial := r.Form["source_serial"]
	fields := r.Form["field"]
	ops := r.Form["op"]
	values := r.Form["value"]
	times := r.Form["time"]
	modev := r.Form["mode"]
	cooldowns := r.Form["cooldown"]
	actions := r.Form["action"]
	tgtKind := r.Form["target_kind"]
	tgtSerial := r.Form["target_serial"]
	recipients := r.Form["recipient"]
	traps := r.Form["trap_host"]

	get := func(v []string, i int) string {
		if i < len(v) {
			return strings.TrimSpace(v[i])
		}
		return ""
	}
	var out []events.Event
	for i := 0; i < len(ids); i++ {
		typ := get(types, i)
		if typ == "" {
			typ = "param"
		}
		src := get(srcSerial, i)
		// значение по типу — нужно, чтобы понять, заполнена ли строка
		var tv string
		switch typ {
		case "time":
			tv = get(times, i)
		case "mode":
			tv = get(modev, i)
		default:
			tv = get(values, i)
		}
		meaningful := src != "" || get(ids, i) != "" || get(fields, i) != "" || tv != "" ||
			get(recipients, i) != "" || get(tgtSerial, i) != "" || get(traps, i) != ""
		if !meaningful {
			continue // действительно пустая новая строка
		}
		ev := events.Event{
			ID:        get(ids, i),
			Enabled:   get(enabled, i) == "1",
			Type:      typ,
			Source:    events.Device{Kind: get(srcKind, i), Serial: src},
			Action:    get(actions, i),
			Target:    events.Device{Kind: get(tgtKind, i), Serial: get(tgtSerial, i)},
			Recipient: get(recipients, i),
			TrapHost:  get(traps, i),
		}
		switch typ {
		case "param":
			ev.Field = get(fields, i)
			ev.Op = get(ops, i)
			ev.Value = get(values, i)
		case "time":
			ev.Value = get(times, i)
		case "mode":
			ev.Value = get(modev, i)
		}
		ev.Cooldown, _ = strconv.Atoi(get(cooldowns, i))
		out = append(out, ev)
	}
	return out
}
