// stats.go — UI статистики: список устройств из обеих tsdb (list из памяти) и
// постраничная выборка таблицы (docs/ui-stat.md).
package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
	"github.com/galiy/malina-26/daemons/lib/tsdbclient"
	"github.com/galiy/malina-26/web/internal/accumulator"
)

// followWindowMs — ширина окна режима «Следовать за временем» ([now−W, now]).
const followWindowMs int64 = int64(5 * time.Minute / time.Millisecond)

// statsDev — устройство для закладок.
type statsDev struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Kind uint8  `json:"kind,omitempty"` // grid: 0 Deye, 1 Sofar
	// Last — время последнего обновления телеметрии (мс), max(raw,avg) из памяти
	// tsdb. 0 — данных нет (busy/ненайден). Клиент фильтрует неактуальные по нему.
	Last int64 `json:"last,omitempty"`
}

// lastSeen — максимальное max-время устройства из обеих tsdb; false — данных нет.
func lastSeen(rawCli, avgCli *tsdbclient.Client, typ, id string) (int64, bool) {
	last := int64(0)
	for _, cli := range []*tsdbclient.Client{rawCli, avgCli} {
		if _, mx, ok, err := cli.Extremes(typ, id); err == nil && ok && mx > last {
			last = mx
		}
	}
	return last, last > 0
}

// statsCol — колонка таблицы (поле контракта + человеческая единица для UI).
// ID — строковый ключ: «<field_id>» для обычных полей и «<field_id>.<bit>» для
// виртуальных колонок битфилда реле.
type statsCol struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type uint8  `json:"type"`
	Unit string `json:"unit"` // единица измерения (пусто — коды/битфилды)
	Raw  bool   `json:"raw"`  // значение показывается без пересчёта (код/битфилд)
	Desc string `json:"desc"` // i18n-ключ описания (всплывающий хинт)
}

// descKeyFor — i18n-ключ описания поля («stats.f.<тип>.<имя>»); тип в ключе, чтобы
// одноимённые поля разных устройств (напр. `PLoad`/`RSErrSis`) не смешивались.
// У BMS имя содержит номер ячейки — берём базовое. Неизвестное поле — пустой ключ.
func descKeyFor(typ string, id uint16, name string) string {
	switch typ {
	case "map":
		return "stats.f.map." + name
	case "kes":
		return "stats.f.kes." + name
	case "grid":
		return "stats.d.grid." + name
	case "mbms":
		switch id % 3 {
		case 1:
			return "stats.f.mbms.UakbCell"
		case 2:
			return "stats.f.mbms.TakbCell"
		default:
			return "stats.f.mbms.Q_Cell"
		}
	}
	return ""
}

// statsRow — строка таблицы для клиента (значения: число или символ).
type statsRow struct {
	Time    int64          `json:"t"`
	Version uint16         `json:"v"`
	Vals    map[string]any `json:"f"` // ключ колонки → число/строка
}

// fieldPlan — предвычисленная раскладка поля для рендера (кэш на версию).
type fieldPlan struct {
	id    uint16
	bit   int // >0 — виртуальная колонка битфилда реле
	key   string
	fc    fieldConv
	name  string
	ftype uint8
	desc  string
}

// verPlan — контракт версии + предвычисленные поля (кэш, R4).
type verPlan struct {
	c     tscontract.Contract
	convs map[uint16]fieldConv
	cols  []fieldPlan
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

	var maps, kess, grids, bmss []string
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
			case "grid":
				if !containsStr(grids, d.ID) {
					grids = append(grids, d.ID)
				}
			case "mbms":
				mbms[d.ID] = true
			case "bms":
				if !containsStr(bmss, d.ID) {
					bmss = append(bmss, d.ID)
				}
			}
		}
	}
	sort.Strings(maps)
	sort.Strings(kess)
	sort.Strings(grids)
	sort.Strings(bmss)
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
	for _, g := range grids {
		add("grid", g)
	}
	for _, b := range bmss { // BMS-Receiver (device_type=bms), id — наш серийник/ID
		add("bms", b)
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
	// Время последнего обновления телеметрии — для фильтра «неактуальных»
	// на клиенте (показ только устройств с обновлением в последний час).
	rawCli := tsdbclient.New(s.opts.TsdbRawSock)
	avgCli := tsdbclient.New(s.opts.TsdbSock)
	for i := range order {
		if last, ok := lastSeen(rawCli, avgCli, order[i].Type, order[i].ID); ok {
			order[i].Last = last
		}
	}
	if len(order) > 0 {
		kinds := map[string]uint8{}
		for _, it := range readGridSHM(s.opts.GridSHMPath) {
			kinds[it.Serial] = it.Kind
		}
		for i := range order {
			if order[i].Type == "grid" {
				order[i].Kind = kinds[order[i].ID]
			}
		}
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

	// Наборы для обхода: базовый; при отсутствии «детальных» и пустом avg —
	// фолбэк на raw (avg наполняется по окнам, сразу после рестарта/очистки пуст).
	orders := [][](*tsdbclient.Client){order}
	if !detail {
		orders = append(orders, [](*tsdbclient.Client){rawCli})
	}
	byTime := map[int64]tsdbclient.Row{}
	var times []int64
	for _, ord := range orders {
		byTime = map[int64]tsdbclient.Row{}
		times = nil
		for _, c := range ord {
			isRaw := c == rawCli
			st, rows, err := c.Query(tsdbclient.ModeWholeOne, typ, []string{id}, nil, from, to, dir, uint32(limit), 0)
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
		if len(times) > 0 {
			break
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
	planCache := map[uint16]*verPlan{}
	getPlan := func(ver uint16) *verPlan {
		if p, seen := planCache[ver]; seen {
			return p
		}
		c, ok := reg.Get(typ, ver)
		if !ok {
			planCache[ver] = nil
			return nil
		}
		p := &verPlan{c: c, convs: map[uint16]fieldConv{}}
		for _, f := range c.Fields() {
			fc, _ := fieldConvFor(typ, ver, f.ID)
			p.convs[f.ID] = fc
			if fc.Kind == kindRelay3 {
				for i := 1; i <= 3; i++ {
					p.cols = append(p.cols, fieldPlan{id: f.ID, bit: i, key: relayKey(f.ID, i),
						fc: fieldConv{Kind: kindRelay3}, name: fmt.Sprintf("Relay%d", i), ftype: uint8(f.Type),
						desc: descKeyFor(typ, f.ID, f.Name)})
				}
				continue
			}
			p.cols = append(p.cols, fieldPlan{id: f.ID, key: strconv.Itoa(int(f.ID)), fc: fc,
				name: f.Name, ftype: uint8(f.Type), desc: descKeyFor(typ, f.ID, f.Name)})
		}
		planCache[ver] = p
		return p
	}

	cols := map[string]statsCol{}
	out := make([]statsRow, 0, len(times))
	for _, t := range times {
		r := byTime[t]
		p := getPlan(r.Version)
		if p == nil {
			continue
		}
		for _, pl := range p.cols {
			if _, seen := cols[pl.key]; !seen {
				cols[pl.key] = statsCol{ID: pl.key, Name: pl.name, Type: pl.ftype, Unit: pl.fc.Unit, Raw: pl.fc.Raw, Desc: pl.desc}
			}
		}
		vals, err := p.c.Decode(r.Payload)
		if err != nil {
			continue
		}
		rawRow := make(map[uint16]float64, len(vals))
		for _, v := range vals {
			rawRow[v.ID] = rawNum(v)
		}
		m := make(map[string]any, len(vals))
		for _, v := range vals {
			fc, ok := p.convs[v.ID]
			if !ok {
				continue
			}
			raw := rawRow[v.ID]
			switch fc.Kind {
			case kindRelay3: // битфилд реле → 3 колонки 0/1
				for i := 0; i < 3; i++ {
					m[relayKey(v.ID, i+1)] = float64((int64(raw) >> uint(i)) & 1)
				}
			case kindSymbol: // одиночный печатаемый ASCII-символ (Mode); иначе «—» (R3)
				b := byte(int64(raw))
				if b < 0x20 || b > 0x7E {
					continue
				}
				m[strconv.Itoa(int(v.ID))] = string(rune(b))
			default:
				if hv, ok := fc.Num(raw, rawRow); ok { // human; иначе «—» (поле пропускаем)
					m[strconv.Itoa(int(v.ID))] = hv
				}
			}
		}
		out = append(out, statsRow{Time: t, Version: r.Version, Vals: m})
	}
	colList := make([]statsCol, 0, len(cols))
	for _, c := range cols {
		colList = append(colList, c)
	}
	sort.Slice(colList, func(i, j int) bool { return colLess(colList[i].ID, colList[j].ID) })
	return out, colList, 0
}

func rawNum(v tscontract.Value) float64 {
	if v.Type == tscontract.FTFloat {
		return v.Float
	}
	return float64(v.Int)
}

// relayKey — ключ виртуальной колонки реле: «<field_id>.<bit 1..3>».
func relayKey(id uint16, bit int) string { return fmt.Sprintf("%d.%d", id, bit) }

// colLess — порядок колонок: по field_id, затем по номеру бита (численно).
func colLess(a, b string) bool {
	ai, as := splitColID(a)
	bi, bs := splitColID(b)
	if ai != bi {
		return ai < bi
	}
	return as < bs
}

func splitColID(s string) (int, int) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		a, _ := strconv.Atoi(s[:i])
		b, _ := strconv.Atoi(s[i+1:])
		return a, b
	}
	a, _ := strconv.Atoi(s)
	return a, 0
}

// seriesPoint — точка ряда (время, значение). Типизированная (без interface{}),
// чтобы не боксить значения при построении рядов (§производительность графиков).
type seriesPoint struct {
	T int64
	V float64
}

// statsSeries — ряды запрошенных полей (ключи колонок) за период. Без лимита
// точек и времени: avg читаем всегда, raw — за всю доступную глубину (порога нет,
// глубина raw определяется объёмом, §14.1 telemetry-store.md). На сшиве приоритет
// у raw: avg берём лишь левее первой raw-точки, правее — raw. Сервер отдаёт только
// значения полей контракта (в человеческих единицах); вычисляемые величины
// (мощности, ток MPPT-сумма и т.п.) — на клиенте (§3.2).
func (s *Server) statsSeries(typ, id string, from, to int64, fields []string, source string) map[string][]seriesPoint {
	all := len(fields) == 0 // без fields — вернуть все поля ряда
	want := map[string]bool{}
	out := map[string][]seriesPoint{}
	for _, k := range fields {
		want[k] = true
		out[k] = []seriesPoint{}
	}
	addPt := func(key string, t int64, v float64) {
		if all || want[key] {
			out[key] = append(out[key], seriesPoint{t, v})
		}
	}

	// Прореживание (§9 §3.3): bucket = окно/1000; если меньше порога — 0 (без
	// прореживания). Порог: raw 1.75 с; avg PeriodMin × 1.5 (период агрегации).
	window := to - from
	bucketAvg := bucketFor(window, int64(s.accPeriodMin())*60_000*3/2)
	rawBucketMin := int64(1750)
	if typ == "grid" {
		gp := s.loadGridConfig().PollPeriod
		if gp <= 0 {
			gp = 10
		}
		rawBucketMin = int64(gp) * 1000 * 7 / 2 // 3,5 периода
	}
	bucketRaw := bucketFor(window, rawBucketMin)

	// source: "" — сшив avg+raw; "raw" — только raw; "avg" — только avg.
	var rowsAvg, rowsRaw []tsdbclient.Row
	if source != "raw" { // avg — всегда, кроме режима «только raw» (follow)
		rowsAvg = s.querySeries(s.opts.TsdbSock, typ, id, from, to, bucketAvg)
	}
	if source != "avg" { // raw — за всю доступную глубину (порога нет, §14.1)
		rowsRaw = s.querySeries(s.opts.TsdbRawSock, typ, id, from, to, bucketRaw)
	}

	// Граница сшива — время первой raw-точки. Левее берём avg, от неё и правее — raw.
	rawMin := int64(-1)
	for _, r := range rowsRaw {
		if rawMin < 0 || r.Time < rawMin {
			rawMin = r.Time
		}
	}
	if rawMin >= 0 {
		trimmed := rowsAvg[:0]
		for _, r := range rowsAvg {
			if r.Time < rawMin {
				trimmed = append(trimmed, r)
			}
		}
		rowsAvg = trimmed
	}
	s.decodeRowsInto(typ, rowsAvg, addPt)
	s.decodeRowsInto(typ, rowsRaw, addPt)
	return out
}

// accPeriodMin — период агрегации avg (мин) с кешем по mtime/size (горячий путь
// графиков; без кеша каждый запрос читал бы accumulator.json, §2 ревью 2026-10-15).
func (s *Server) accPeriodMin() int {
	path := s.opts.AccConfigPath
	fi, err := os.Stat(path)
	s.accMu.Lock()
	defer s.accMu.Unlock()
	if err == nil && s.accOK && fi.ModTime().Equal(s.accMod) && fi.Size() == s.accSize {
		return s.accPeriod
	}
	p := 5
	if acc := accumulator.Load(path); acc.PeriodMin > 0 {
		p = acc.PeriodMin
	}
	s.accPeriod, s.accOK = p, true
	if err == nil {
		s.accMod, s.accSize = fi.ModTime(), fi.Size()
	}
	return p
}

// bucketFor — размер мини-окна деcimation: окно/1000 (мс на ~1000 точек).
// Если меньше порога minMS — 0 (прореживание не запрашивается; §9).
func bucketFor(windowMS, minMS int64) uint32 {
	w := windowMS / 1000
	if w < minMS {
		return 0
	}
	return uint32(w)
}

// querySeries — чтение строк ряда за период; bucket — мини-окно decimation (мс, 0 = выкл).
func (s *Server) querySeries(sock, typ, id string, from, to int64, bucket uint32) []tsdbclient.Row {
	st, rows, err := tsdbclient.New(sock).Query(tsdbclient.ModeWholeOne, typ, []string{id}, nil, from, to, tsdbclient.DirectionFrom, 0, bucket)
	if err != nil || st != tsdbclient.StatusOK {
		return nil
	}
	return rows
}

// decodeRowsInto декодирует строки контракта в точки рядов через addPt.
func (s *Server) decodeRowsInto(typ string, rows []tsdbclient.Row, addPt func(string, int64, float64)) {
	reg := tscontract.NewDefaultRegistry()
	for _, r := range rows {
		c, ok := reg.Get(typ, r.Version)
		if !ok {
			continue
		}
		vals, err := c.Decode(r.Payload)
		if err != nil {
			continue
		}
		rawRow := make(map[uint16]float64, len(vals))
		for _, v := range vals {
			rawRow[v.ID] = rawNum(v)
		}
		for _, v := range vals {
			fc, ok := fieldConvFor(typ, r.Version, v.ID)
			if !ok {
				continue
			}
			raw := rawRow[v.ID]
			switch fc.Kind {
			case kindRelay3:
				for i := 0; i < 3; i++ {
					addPt(relayKey(v.ID, i+1), r.Time, float64((int64(raw)>>uint(i))&1))
				}
			case kindSymbol:
				// символы в графики не выводим
			default:
				if hv, ok := fc.Num(raw, rawRow); ok {
					addPt(strconv.Itoa(int(v.ID)), r.Time, hv)
				}
			}
		}
	}
}

// handleAPIStatsSeries — ряды для графиков: ?type=&id=&from=&to=&fields=a,b,c
func (s *Server) handleAPIStatsSeries(w http.ResponseWriter, r *http.Request) {
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
	var fields []string
	if f := q.Get("fields"); f != "" {
		fields = strings.Split(f, ",")
	}
	var from, to int64
	source := q.Get("source")
	if q.Get("follow") == "1" {
		// Режим «Следовать за временем». Время считает backend (совпадает с tsdb).
		// Глубину окна задаёт клиент (depth, мин), либо клиент просит только новые
		// точки после after (догрузка). Источник — только raw.
		now := time.Now().UnixMilli()
		to, source = now, "raw"
		if a := q.Get("after"); a != "" {
			if af, err := strconv.ParseInt(a, 10, 64); err == nil && af > 0 {
				from = af + 1 // только точки после последней известной клиенту
			} else {
				from = now - followWindowMs
			}
		} else {
			depth := 5 // минуты по умолчанию
			if d := q.Get("depth"); d != "" {
				if dv, err := strconv.Atoi(d); err == nil && dv >= 1 && dv <= 10 {
					depth = dv
				}
			}
			from = now - int64(depth)*60000
		}
	} else {
		var errF, errT error
		from, errF = strconv.ParseInt(q.Get("from"), 10, 64)
		to, errT = strconv.ParseInt(q.Get("to"), 10, 64)
		if errF != nil || errT != nil || from > to {
			http.Error(w, "bad from/to", http.StatusBadRequest)
			return
		}
	}
	series := s.statsSeries(typ, id, from, to, fields, source)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeSeriesJSON(w, series, from, to)
}

// writeSeriesJSON стримит `{"from":F,"to":T,"series":{"<key>":[[t,v],…],…}}` прямо в ответ, без
// промежуточного map[string]any и без полного буфера ответа (§производительность:
// типизированные числа пишутся strconv, буферизация лишь chunk-ами).
func writeSeriesJSON(w io.Writer, series map[string][]seriesPoint, from, to int64) {
	bw := bufio.NewWriterSize(w, 64*1024)
	_, _ = bw.WriteString(`{"from":`)
	var num []byte
	num = strconv.AppendInt(num[:0], from, 10)
	_, _ = bw.Write(num)
	_, _ = bw.WriteString(`,"to":`)
	num = strconv.AppendInt(num[:0], to, 10)
	_, _ = bw.Write(num)
	_, _ = bw.WriteString(`,"series":{`)
	first := true
	for k, pts := range series {
		if first {
			first = false
		} else {
			_ = bw.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		_, _ = bw.Write(kb)
		_, _ = bw.WriteString(`:[`)
		firstPt := true
		for i := range pts {
			p := pts[i]
			if !isFinite(p.V) {
				continue // несериализуемые значения (NaN/Inf) пропускаем
			}
			if firstPt {
				firstPt = false
			} else {
				_ = bw.WriteByte(',')
			}
			_ = bw.WriteByte('[')
			num = strconv.AppendInt(num[:0], p.T, 10)
			_, _ = bw.Write(num)
			_ = bw.WriteByte(',')
			num = strconv.AppendFloat(num[:0], p.V, 'f', -1, 64)
			_, _ = bw.Write(num)
			_ = bw.WriteByte(']')
		}
		_ = bw.WriteByte(']')
	}
	_, _ = bw.WriteString("}}")
	_ = bw.Flush()
}

// isFinite — значение конечно (не NaN/±Inf).
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

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
