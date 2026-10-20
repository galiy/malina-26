// Package accumulator — аккумулятор телеметрии в составе malina-web
// (docs/accumulator.md). Читает сырые снапшоты из raw-tsdb за прошедшее окно,
// усредняет по правилам (§5) и пишет усреднённый снапшот в avg-tsdb.
package accumulator

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
	"github.com/galiy/malina-26/daemons/lib/tsdbclient"
)

// reg — реестр контрактов, создаётся один раз (не в горячем пути усреднения).
var reg = tscontract.NewDefaultRegistry()

// Config — настройка аккумулятора (страница «Параметры системы»).
type Config struct {
	Enabled   bool `json:"enabled"`
	PeriodMin int  `json:"period_min"` // 1,2,3,4,5,6,10
}

// DefaultConfigPath — файл настроек.
const DefaultConfigPath = "/malina/etc/malina/accumulator.json"

// ValidatePeriod — допустимый период (кратен минутам; 7,8,9 запрещены).
func ValidatePeriod(p int) bool {
	switch p {
	case 1, 2, 3, 4, 5, 6, 10:
		return true
	}
	return false
}

// Load читает настройку; при отсутствии/неполном файле — дефолт (вкл., 5 мин).
func Load(path string) Config {
	cfg := Config{Enabled: true, PeriodMin: 5}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	if !ValidatePeriod(cfg.PeriodMin) {
		cfg.PeriodMin = 5
	}
	return cfg
}

// Save записывает настройку (период валидируется).
func Save(path string, cfg Config) error {
	if !ValidatePeriod(cfg.PeriodMin) {
		cfg.PeriodMin = 5
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Sample — снапшот устройства: время + значения полей.
type Sample struct {
	Time int64
	Vals map[uint16]tscontract.Value
}

// Result — усреднённый снапшот: время (последний снимок окна) + payload.
type Result struct {
	Time    int64
	Payload []byte
}

// --- правила усреднения (docs/accumulator.md §5) ---

type rule uint8

const (
	rAvg         rule = iota // среднее
	rOr                      // битовая маска → ИЛИ
	rLast                    // последнее значение
	rLastNonZero             // последнее ненулевое
	rUnet2                   // flagUnet2: если был активен 2-й вход → 2-й
	rFreqZeroMaj             // частота: 0 — нет данных, среднее только по валидным при большинстве
	rVoltZeroMaj             // напряжение: 0 — нет данных, среднее только по валидным при большинстве
)

// mapRules — правила для полей контракта map v1 (id → rule).
var mapRules = map[uint16]rule{
	1: rVoltZeroMaj, 2: rAvg, 3: rAvg, 4: rAvg, 5: rAvg, 6: rVoltZeroMaj, 7: rFreqZeroMaj, 8: rAvg,
	9: rAvg, 10: rAvg, 11: rLast, 12: rOr, 13: rAvg, 14: rAvg, 15: rAvg,
	16: rAvg, 17: rOr, 18: rAvg, 19: rLast, 20: rLast, 21: rOr, 22: rOr,
	23: rOr, 24: rOr, 25: rOr, 26: rUnet2,
	27: rOr,                                                             // Relay (map v2): битовая маска → ИЛИ
	28: rFreqZeroMaj, 29: rAvg, 30: rAvg, 31: rAvg, 32: rAvg, 33: rLast, // ThFMAP + MPPT0..3 + PNET_Sign_P (map v3)
}

// kesRules — правила для полей контракта kes v1.
var kesRules = map[uint16]rule{
	1: rAvg, 2: rAvg, 3: rAvg, 4: rAvg, 5: rAvg, 6: rAvg, 7: rAvg, 8: rAvg,
	9: rAvg, 10: rAvg, 11: rAvg, 12: rAvg, 13: rLast, 14: rLast, 15: rOr,
}

// bmsRules — правила для полей контракта bms v1 (docs/bms-reader.md §1):
// битфилды (flags/balance_mask/alarm) — ИЛИ; mode/flags-дискретные — last.
var bmsRules = func() map[uint16]rule {
	m := map[uint16]rule{
		1: rLast, 2: rLast, 3: rOr, // cell_count, temp_count, flags
		28: rAvg, 29: rAvg, 30: rAvg, 31: rAvg, // current, power, soc, soh
		32: rLast, 33: rAvg, 34: rLast, 35: rLast, // cap, remaining, rated, cycles
		36: rAvg, 37: rAvg, 38: rOr, 39: rLast, // totalV, portV, balance_mask, mode
	}
	for i := 4; i <= 19; i++ { // ячейки (мВ) — среднее
		m[uint16(i)] = rAvg
	}
	for i := 20; i <= 27; i++ { // температуры — среднее
		m[uint16(i)] = rAvg
	}
	for i := 40; i <= 58; i++ { // alarm_1..19 — ИЛИ
		m[uint16(i)] = rOr
	}
	return m
}()

// gridRules — правила контракта grid v1 (docs/grid-reader.md): состояние
// инвертора — последнее, слова аварий/предупреждений — побитовое ИЛИ,
// остальные измеряемые поля — среднее.
var gridRules = func() map[uint16]rule {
	m := map[uint16]rule{18: rLast}
	for id := uint16(19); id <= 24; id++ {
		m[id] = rOr
	}
	return m
}()

func rulesFor(typ string) map[uint16]rule {
	switch typ {
	case "map":
		return mapRules
	case "kes":
		return kesRules
	case "bms":
		return bmsRules
	case "grid":
		return gridRules
	default: // mbms и прочие — всё измеряемое
		return nil
	}
}

// Average усредняет выборку снапшотов одной (type, version). Время результата —
// время последнего снимка. ok=false при пустой выборке.
func Average(typ string, version uint16, samples []Sample) (Result, bool) {
	if len(samples) == 0 {
		return Result{}, false
	}
	samples = append([]Sample(nil), samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].Time < samples[j].Time })
	c, ok := reg.Get(typ, version)
	if !ok {
		return Result{}, false
	}
	last := samples[len(samples)-1]
	fields := c.Fields()
	rules := rulesFor(typ)

	// собрать значения по полям
	byField := map[uint16][]tscontract.Value{}
	var order []uint16
	for _, s := range samples {
		for id, v := range s.Vals {
			if _, seen := byField[id]; !seen {
				order = append(order, id)
			}
			byField[id] = append(byField[id], v)
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	out := make([]tscontract.Value, 0, len(order))
	for _, id := range order {
		vals := byField[id]
		typField := fieldTypeOf(fields, id)
		rl := rAvg
		if rules != nil {
			if r, ok := rules[id]; ok {
				rl = r
			}
		}
		v := tscontract.Value{ID: id, Type: typField}
		switch rl {
		case rOr:
			var acc int64
			for _, x := range vals {
				acc |= x.Int
			}
			v.Int = acc
		case rLast:
			v = lastValue(vals)
			v.ID = id
		case rLastNonZero:
			v = lastNonZero(vals)
			v.ID = id
		case rUnet2:
			v = unet2(vals)
			v.ID = id
		case rFreqZeroMaj:
			v.Int = avgFreqMajority(vals)
		case rVoltZeroMaj:
			v.Int = avgVoltMajority(vals)
		default: // rAvg
			if typField == tscontract.FTFloat {
				var sum float64
				for _, x := range vals {
					sum += x.Float
				}
				v.Float = sum / float64(len(vals))
			} else {
				var sum int64
				for _, x := range vals {
					sum += x.Int
				}
				v.Int = sum / int64(len(vals))
			}
		}
		out = append(out, v)
	}
	payload, err := c.Encode(out)
	if err != nil {
		return Result{}, false
	}
	return Result{Time: last.Time, Payload: payload}, true
}

// avgFreqMajority — частота (период 6250/x): 0 — нет данных. Нулевые снимки
// не участвуют в среднем; решение по большинству: нулей больше или столько же
// → 0, иначе среднее по Гц валидных снимков, обратно в период.
func avgFreqMajority(vals []tscontract.Value) int64 {
	var zero, valid int
	var sumHz float64
	for _, x := range vals {
		if x.Int == 0 {
			zero++
			continue
		}
		valid++
		sumHz += 6250 / float64(x.Int)
	}
	if valid == 0 || zero >= valid {
		return 0
	}
	hz := sumHz / float64(valid)
	period := int64(6250/hz + 0.5)
	if period < 1 {
		period = 1
	}
	return period
}

// avgVoltMajority — напряжение сети/выхода МАП: 0 — нет напряжения. Нулевые
// снимки не участвуют в среднем; решение по большинству. Среднее по сырым
// значениям эквивалентно mean(raw+100)−100, поэтому сохраняется как есть.
func avgVoltMajority(vals []tscontract.Value) int64 {
	var zero, valid int
	var sum int64
	for _, x := range vals {
		if x.Int == 0 {
			zero++
			continue
		}
		valid++
		sum += x.Int
	}
	if valid == 0 || zero >= valid {
		return 0
	}
	return (sum + int64(valid)/2) / int64(valid)
}

func fieldTypeOf(fields []tscontract.Field, id uint16) tscontract.FieldType {
	for _, f := range fields {
		if f.ID == id {
			return f.Type
		}
	}
	return tscontract.FTU8
}

func lastValue(vals []tscontract.Value) tscontract.Value { return vals[len(vals)-1] }

func lastNonZero(vals []tscontract.Value) tscontract.Value {
	for i := len(vals) - 1; i >= 0; i-- {
		if vals[i].Int != 0 {
			return vals[i]
		}
	}
	return vals[len(vals)-1]
}

// unet2: если в окне был активен 2-й вход (бит 0x04) — вернуть значение с ним;
// иначе — последнее.
func unet2(vals []tscontract.Value) tscontract.Value {
	for _, v := range vals {
		if v.Int&0x04 != 0 {
			return v
		}
	}
	return vals[len(vals)-1]
}

// --- цикл аккумулятора ---

// Dev — устройство (тип+идентификатор) в raw-хранилище.
type Dev struct {
	Type string
	ID   string
}

// enumerateRaw перечисляет устройства по дереву raw (<root>/<type>/<id>/…):
// «что насыпалось» (для кого есть данные).
func enumerateRaw(root string) []Dev {
	var out []Dev
	types, _ := os.ReadDir(root)
	for _, t := range types {
		if !t.IsDir() {
			continue
		}
		ids, _ := os.ReadDir(root + "/" + t.Name())
		for _, id := range ids {
			if id.IsDir() {
				out = append(out, Dev{Type: t.Name(), ID: id.Name()})
			}
		}
	}
	return out
}

// sleepCtx спит d; возвращает false, если контекст отменён (graceful stop).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Run — цикл аккумулятора: по календарным окнам (кратным периоду) читает raw за
// прошедшее окно, усредняет и пишет в avg. Окно обработки — строго через
// [5, 10] c после границы окна: не раньше +5 c и не позже +10 c; если проснулись
// позже (задержка/сон) — устаревшее окно НЕ догоняем. Настройка (вкл/период)
// перечитывается перед каждым тиком. Завершается по ctx.
func Run(ctx context.Context, rawRoot, rawSock, avgSock, cfgPath string, logf func(string, ...any)) {
	raw := tsdbclient.New(rawSock)
	avg := tsdbclient.New(avgSock)
	for {
		cfg := Load(cfgPath)
		period := time.Duration(cfg.PeriodMin) * time.Minute
		if period <= 0 {
			period = 5 * time.Minute
		}
		now := time.Now()
		windowEnd := now.Truncate(period) // конец прошедшего окна
		fire := windowEnd.Add(5 * time.Second)
		deadline := windowEnd.Add(10 * time.Second)
		if now.Before(fire) {
			if !sleepCtx(ctx, time.Until(fire)) {
				return
			}
		}
		now = time.Now()
		if cfg.Enabled && !now.After(deadline) { // только в окне [fire, deadline]
			// Окно [windowEnd-period, windowEnd): `to` — начало СЛЕДУЮЩЕГО окна
			// минус минимальный тик (1 мс), иначе строка ровно на границе попала бы
			// и в текущее, и в следующее окно (перекрытие).
			from := windowEnd.Add(-period).UnixMilli()
			to := windowEnd.UnixMilli() - 1
			processWindow(raw, avg, rawRoot, from, to, logf)
		}
		// ждать до границы следующего окна (+5 c) — без busy-loop
		next := windowEnd.Add(period).Add(5 * time.Second)
		if d := time.Until(next); d > 0 {
			if !sleepCtx(ctx, d) {
				return
			}
		}
	}
}

// processWindow усредняет окно [from,to] по всем устройствам raw и пишет в avg.
// Ошибки не логируем поштучно (чтобы не завалить лог) — считаем и отдаём одной
// сводкой за тик.
func processWindow(raw, avg *tsdbclient.Client, rawRoot string, from, to int64, logf func(string, ...any)) {
	devs := enumerateRaw(rawRoot)
	written, rowsOut, errs := 0, 0, 0
	for _, dev := range devs {
		status, rows, err := raw.Query(tsdbclient.ModeWholeOne, dev.Type, []string{dev.ID}, nil, from, to, tsdbclient.DirectionFrom, 0, 0)
		if err != nil || status != tsdbclient.StatusOK {
			errs++
			continue
		}
		if len(rows) == 0 {
			continue
		}
		// группировка по версии контракта
		byVer := map[uint16][]Sample{}
		for _, r := range rows {
			c, ok := reg.Get(dev.Type, r.Version)
			if !ok {
				errs++
				continue
			}
			vals, derr := c.Decode(r.Payload)
			if derr != nil {
				errs++
				continue
			}
			m := make(map[uint16]tscontract.Value, len(vals))
			for _, v := range vals {
				m[v.ID] = v
			}
			byVer[r.Version] = append(byVer[r.Version], Sample{Time: r.Time, Vals: m})
		}
		out := make([]tsdbclient.Row, 0, len(byVer))
		for ver, samples := range byVer {
			res, ok := Average(dev.Type, ver, samples)
			if !ok {
				continue
			}
			out = append(out, tsdbclient.Row{Type: dev.Type, ID: dev.ID, Time: res.Time, Version: ver, Payload: res.Payload})
		}
		if len(out) == 0 {
			continue
		}
		if _, _, werr := avg.Write(out); werr != nil {
			errs++
			continue
		}
		written++
		rowsOut += len(out)
	}
	if logf != nil {
		logf("аккумулятор: окно [%d..%d] устройств=%d записей=%d ошибок=%d", from, to, written, rowsOut, errs)
	}
}
