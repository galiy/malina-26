// stats.go — UI статистики: список устройств из обеих tsdb (list из памяти) и
// постраничная выборка таблицы (docs/ui-stat.md).
package web

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
	"github.com/galiy/malina-26/daemons/lib/tsdbclient"
)

// statsDev — устройство для закладок.
type statsDev struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// statsCol — колонка таблицы (поле контракта).
type statsCol struct {
	ID   uint16 `json:"id"`
	Name string `json:"name"`
	Type uint8  `json:"type"`
}

// statsRow — строка таблицы для клиента (поля декодированы).
type statsRow struct {
	Time    int64              `json:"t"`
	Version uint16             `json:"v"`
	Vals    map[string]float64 `json:"f"` // field_id → значение
}

// statsDevices — объединение устройств обеих tsdb (list из памяти, без чтения
// сегментов). Порядок: МАП(+его BMS) → КЭС → BMS-сироты; внутри — по серийнику (§4).
func (s *Server) statsDevices() []statsDev {
	uniq := map[string]bool{}
	var order []statsDev
	mbms := map[string]bool{}
	add := func(typ, id string) {
		k := typ + "\x00" + id
		if uniq[k] {
			return
		}
		uniq[k] = true
		order = append(order, statsDev{Type: typ, ID: id})
	}

	var maps, kess []string
	for _, cli := range []*tsdbclient.Client{tsdbclient.New(s.opts.TsdbRawSock), tsdbclient.New(s.opts.TsdbSock)} {
		devs, err := cli.List()
		if err != nil {
			continue
		}
		for _, d := range devs {
			switch d.Type {
			case "map":
				if !containsStr(maps, d.ID) {
					maps = append(maps, d.ID)
				}
			case "kes":
				if !containsStr(kess, d.ID) {
					kess = append(kess, d.ID)
				}
			case "mbms":
				mbms[d.ID] = true
			}
		}
	}
	sort.Strings(maps)
	sort.Strings(kess)
	usedMBMS := map[string]bool{}
	for _, m := range maps {
		add("map", m)
		if mbms[m] {
			add("mbms", m)
			usedMBMS[m] = true
		}
	}
	for _, k := range kess {
		add("kes", k)
	}
	var orph []string
	for id := range mbms {
		if !usedMBMS[id] {
			orph = append(orph, id)
		}
	}
	sort.Strings(orph)
	for _, id := range orph {
		add("mbms", id)
	}
	return order
}

func containsStr(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

// statsTable — выборка по устройству из обеих tsdb; приоритет raw в его зоне,
// вне зоны — avg (§4.2). detail=false — только avg. Направление/лимит — §9.
func (s *Server) statsTable(typ, id string, from, to int64, dir uint8, limit int, rawMinHint int64, detail bool) ([]statsRow, []statsCol, int) {
	rawCli := tsdbclient.New(s.opts.TsdbRawSock)
	avgCli := tsdbclient.New(s.opts.TsdbSock)

	var order [](*tsdbclient.Client)
	rawMin := int64(-1)
	if detail {
		rawMin = rawMinHint
		if rawMin <= 0 { // fallback — крайняя точка raw из памяти
			if mn, _, ok, err := rawCli.Extremes(typ, id); err == nil && ok {
				rawMin = mn
			}
		}
		order = [](*tsdbclient.Client){avgCli, rawCli}
		if dir == tsdbclient.DirectionTo {
			order = [](*tsdbclient.Client){rawCli, avgCli}
		}
	} else {
		order = [](*tsdbclient.Client){avgCli}
	}
	// Н3 (краевой, пренебрежимо для MVP): avg-строка окна, пересекающего rawMin,
	// имеет time = последний сэмпл окна; при time >= rawMin она отбрасывается.

	byTime := map[int64]tsdbclient.Row{}
	var times []int64
	for _, c := range order {
		isRaw := c == rawCli
		st, rows, err := c.Query(tsdbclient.ModeWholeOne, typ, []string{id}, nil, from, to, dir, uint32(limit))
		if err != nil || st != tsdbclient.StatusOK {
			continue
		}
		for _, r := range rows {
			if !isRaw && rawMin >= 0 && r.Time >= rawMin {
				continue // зона приоритета raw
			}
			if _, ok := byTime[r.Time]; !ok {
				times = append(times, r.Time)
			}
			byTime[r.Time] = r
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	if limit > 0 && len(times) > limit {
		if dir == tsdbclient.DirectionTo {
			times = times[len(times)-limit:]
		} else {
			times = times[:limit]
		}
	}
	reg := tscontract.NewDefaultRegistry()
	cols := map[uint16]statsCol{}
	out := make([]statsRow, 0, len(times))
	for _, t := range times {
		r := byTime[t]
		c, ok := reg.Get(typ, r.Version)
		if !ok {
			continue
		}
		for _, f := range c.Fields() {
			if _, seen := cols[f.ID]; !seen {
				cols[f.ID] = statsCol{ID: f.ID, Name: f.Name, Type: uint8(f.Type)}
			}
		}
		vals, err := c.Decode(r.Payload)
		if err != nil {
			continue
		}
		m := make(map[string]float64, len(vals))
		for _, v := range vals {
			if v.Type == tscontract.FTFloat {
				m[strconv.Itoa(int(v.ID))] = v.Float
			} else {
				m[strconv.Itoa(int(v.ID))] = float64(v.Int)
			}
		}
		out = append(out, statsRow{Time: t, Version: r.Version, Vals: m})
	}
	colList := make([]statsCol, 0, len(cols))
	for _, c := range cols {
		colList = append(colList, c)
	}
	sort.Slice(colList, func(i, j int) bool { return colList[i].ID < colList[j].ID })
	return out, colList, 0
}

// --- handlers ---

func (s *Server) handleStatsCharts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.render(w, http.StatusOK, "stats_charts", s.statsBaseData(r))
}

func (s *Server) handleStatsTables(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	s.render(w, http.StatusOK, "stats_tables", s.statsBaseData(r))
}

func (s *Server) statsBaseData(r *http.Request) map[string]any {
	d := s.baseData(r)
	d["Active"] = "stats"
	return d
}

// handleAPIStatsDevices — список устройств (закладки), из памяти обеих tsdb.
func (s *Server) handleAPIStatsDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	devs := s.statsDevices()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"devices": devs})
}

// handleAPIStatsExtremes — крайние точки устройства: ?type=&id= → {raw,avg} min/max.
func (s *Server) handleAPIStatsExtremes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	typ, id := r.URL.Query().Get("type"), r.URL.Query().Get("id")
	if typ == "" || id == "" {
		http.Error(w, "type/id required", http.StatusBadRequest)
		return
	}
	ex := func(sock string) map[string]any {
		mn, mx, ok, err := tsdbclient.New(sock).Extremes(typ, id)
		if err != nil {
			return map[string]any{"found": false}
		}
		return map[string]any{"found": ok, "min": mn, "max": mx}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"raw": ex(s.opts.TsdbRawSock),
		"avg": ex(s.opts.TsdbSock),
	})
}

// handleAPIStatsTable — данные таблицы: ?type=&id=&from=&to=&dir=&limit=&detail=
func (s *Server) handleAPIStatsTable(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	q := r.URL.Query()
	typ, id := q.Get("type"), q.Get("id")
	if typ == "" || id == "" {
		http.Error(w, "type/id required", http.StatusBadRequest)
		return
	}
	from, errF := strconv.ParseInt(q.Get("from"), 10, 64)
	to, errT := strconv.ParseInt(q.Get("to"), 10, 64)
	if errF != nil || errT != nil {
		http.Error(w, "bad from/to", http.StatusBadRequest)
		return
	}
	if from > to {
		http.Error(w, "from > to", http.StatusBadRequest)
		return
	}
	dir := tsdbclient.DirectionFrom
	if q.Get("dir") == "to" {
		dir = tsdbclient.DirectionTo
	}
	limit := 200
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 2000 {
		limit = v
	}
	rawMin, _ := strconv.ParseInt(q.Get("rawmin"), 10, 64)
	detail := q.Get("detail") == "1" // по умолчанию — только avg
	rows, cols, status := s.statsTable(typ, id, from, to, dir, limit, rawMin, detail)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "columns": cols, "rows": rows})
}
