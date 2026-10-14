// Package physim — общая физическая модель для эмуляторов (kes-emu и встроенного
// в map-reader sim). Согласованный подход — docs/emu-physics.md.
//
// Даёт плавные, связные тренды: суточная солнечная выработка (ночь — до 15 %
// номинала, день — до 100 %, облачность снижает), SOC АКБ LiFePO4 (16 ячеек,
// 3.15..3.6 В), тепловая инерция, малый медленный дрейф ячеек относительно друг
// друга. Без seed, без масштаба времени, без сбоев обмена.
package physim

import (
	"math"
	"math/rand"
	"time"
)

// Params — номиналы (типовые по умолчанию).
type Params struct {
	KESCount  int     // число КЭС/MPPT-каналов
	Cells     int     // ячеек в пакете BMS (16 → 48 В)
	PanelPmax float64 // массив 3S4P, Вт (на КЭС)
	Vmp       float64 // напряжение MPP массива, В
	Imp       float64 // ток MPP массива, А
	CellLo    float64 // нижний предел ячейки, В (ночь)
	CellHi    float64 // верхний предел ячейки, В (яркий день)
	LoadW     float64 // условная нагрузка, Вт
	CapAh     float64 // условная ёмкость АКБ, А·ч

	// OutputScale — индивидуальное отклонение выработки контроллера (1.0 — норма;
	// у параллельных КЭС разброс: напр. 0.95 / 1.0 / 0.97).
	OutputScale float64
}

// Default — номинал: 3S4P Hevel HVL-320 HJT, 16 ячеек LiFePO4 (48 В).
func Default() Params {
	return Params{
		KESCount:    3,
		Cells:       16,
		PanelPmax:   3840,
		Vmp:         104.7,
		Imp:         36.8,
		CellLo:      3.15,
		CellHi:      3.60,
		LoadW:       1200,
		CapAh:       300, // условная (влияет лишь на скорость SOC — плавность)
		OutputScale: 1.0,
	}
}

// Климат Москвы (Википедия, климатическая таблица 1991–2020): среднемесячные
// дневной максимум (день, °C) и ночной минимум (ночь, °C) — 12 значений по месяцам.
var moscowDay = [12]float64{-3.9, -3.0, 3.0, 11.7, 19.0, 22.4, 24.7, 22.7, 16.4, 8.9, 1.6, -2.3}
var moscowNight = [12]float64{-8.7, -8.8, -4.2, 2.3, 8.1, 12.2, 14.8, 13.0, 8.0, 3.0, -2.4, -6.5}

// monthLerp — линейная интерполяция годового хода таблицы (значение месяца — в его
// начале), плавно и циклично через Новый год. x — день года от 1 янв (дробное).
func monthLerp(x float64, tbl [12]float64) float64 {
	step := 365.0 / 12.0
	pos := math.Mod(math.Max(x, 0), 365.0) / step // 0..12
	i0 := int(math.Floor(pos))
	f := pos - float64(i0)
	return tbl[((i0%12)+12)%12]*(1-f) + tbl[(i0+1)%12]*f
}

// ambientTemp — температура воздуха по климату Москвы в момент now с суточным ходом:
// минимум ~04 ч ночи, максимум ~16 ч дня; плавный подъём утром, спад вечером.
func (m *Model) ambientTemp(now time.Time) float64 {
	doy := float64(now.YearDay() - 1)
	day := monthLerp(doy, moscowDay)
	night := monthLerp(doy, moscowNight)
	h := float64(now.Hour()) + float64(now.Minute())/60 + float64(now.Second())/3600
	diurn := 0.5 - 0.5*math.Cos(2*math.Pi*(h-4)/24)
	return night + (day-night)*diurn
}

// Sample — срез состояния для раскладки в регистры/ячейки.
type Sample struct {
	VcPV    float64   // напряжение панелей, В
	IcPV    float64   // ток панелей, А
	PPV     float64   // мощность панелей, Вт
	VBat    float64   // напряжение пакета, В
	TempBat float64   // температура АКБ, °C
	TempInt float64   // внутренняя температура, °C
	Mode    byte      // символ режима i/v/B/b/S
	SOC     float64   // 0..1
	Ucell   []float64 // напряжение ячеек, В
	Takb    []float64 // температура ячеек, °C
	QCell   []float64 // ток ячейки, А
	Imppt   []float64 // ток канала MPPT, А (на КЭС)
	Relay   byte      // биты реле (0/1/2)
}

// Model — состояние.
type Model struct {
	p     Params
	rnd   *rand.Rand
	soc   float64
	cloud float64 // текущий множитель облучённости (1 — ясно, 0.4 — пик облака)
	tBat  float64
	tInt  float64
	// Облачные события: налетает/уходит плавно, длится 1..7 мин, затеняет до 40 %.
	cloudStart time.Time
	cloudEnd   time.Time
	cloudDur   time.Duration
	cloudOn    bool
	nextCloud  time.Time
	off        []float64 // per-cell смещение, В (малый медленный дрейф)
	nextSrch   time.Time
	srchEnd    time.Time
	last       time.Time
	relay      byte
}

// New создаёт модель.
func New(p Params, now time.Time) *Model {
	if p.Cells <= 0 {
		p.Cells = 16
	}
	if p.KESCount <= 0 {
		p.KESCount = 3
	}
	if p.OutputScale <= 0 {
		p.OutputScale = 1.0
	}
	m := &Model{p: p, rnd: rand.New(rand.NewSource(now.UnixNano())), cloud: 1.0, last: now}
	// Без персиста: стартовое состояние — по текущему времени суток (правдоподобно),
	// а не фиксированное. Ночью разряжено/прохладно, днём заряжено/тепло.
	sec := float64(now.Hour()*3600 + now.Minute()*60 + now.Second())
	day := 0.5 - 0.5*math.Cos(2*math.Pi*(sec/86400.0))
	m.soc = 0.30 + 0.50*day // ночь ≈0.30, полдень ≈0.80
	g := 0.15 + 0.85*day
	m.tBat = m.ambientTemp(now)
	m.tInt = m.tBat + (83-m.tBat)*g
	m.nextCloud = now.Add(time.Duration(2+m.rnd.Intn(8)) * time.Minute)
	m.off = make([]float64, p.Cells)
	for i := range m.off {
		m.off[i] = (m.rnd.Float64()*2 - 1) * 0.004 // старт: ≤4 мВ
	}
	return m
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Tick пересчитывает состояние на момент now (интегрирует по dt, плавно).
func (m *Model) Tick(now time.Time) {
	dt := now.Sub(m.last).Seconds()
	if dt <= 0 {
		dt = 1
	}
	m.last = now

	// Облучённость: суточный профиль (0 ночь .. 1 полдень), ночь — ≥15 %.
	sec := float64(now.Hour()*3600 + now.Minute()*60 + now.Second())
	frac := sec / 86400.0
	day := 0.5 - 0.5*math.Cos(2*math.Pi*frac) // 0 в полночь, 1 в полдень
	g := 0.15 + 0.85*day                      // ночь ≥15 %

	// Облака: редкие события — налетает и уходит ПЛАВНО, длится 1..7 мин,
	// в пике затеняет до 40 % (множитель 0.4). Между облаками — ясно (1.0).
	if m.cloudOn && now.After(m.cloudEnd) {
		m.cloudOn = false
		m.nextCloud = now.Add(time.Duration(15+m.rnd.Intn(46)) * time.Minute) // 15..60 мин ясно
	}
	if !m.cloudOn && now.After(m.nextCloud) {
		m.cloudOn = true
		m.cloudStart = now
		m.cloudDur = time.Duration(1+m.rnd.Intn(7)) * time.Minute // 1..7 мин
		m.cloudEnd = now.Add(m.cloudDur)
	}
	if m.cloudOn {
		frac := float64(now.Sub(m.cloudStart)) / float64(m.cloudDur)
		bell := math.Sin(math.Pi * clamp(frac, 0, 1)) // 0→1→0 (плавный вход/выход)
		m.cloud = 1 - (1-0.4)*bell                    // 1.0 … 0.4 … 1.0
	} else {
		m.cloud += (1.0 - m.cloud) * clamp(dt/60, 0, 1) // плавный возврат к ясному
	}
	g = clamp(g*m.cloud, 0.05, 1.0)
	g *= 1 + (m.rnd.Float64()*2-1)*0.01 // лёгкая «живость» кривой (±1 %)

	// Мощности: P_pv = Pmax·g·OutputScale; АКБ добирает/отдаёт разницу с нагрузкой.
	pPV := m.p.PanelPmax * g * m.p.OutputScale
	pBat := pPV - m.p.LoadW
	vBat := m.vbat()
	iBat := pBat / vBat
	m.soc = clamp(m.soc+iBat*dt/(m.p.CapAh*3600), 0.05, 1.0)

	// Температура КЭС-АКБ — по климату Москвы с суточным ходом (день тепло, ночь
	// холодно); тепловая инерция — плавное следование за целью (постоянная ~1 ч).
	m.tBat += (m.ambientTemp(now) - m.tBat) * clamp(dt/3600, 0, 1)
	// Внутренняя температура КЭС зависит от выработки: малая мощность — почти как
	// АКБ, полная — до ~83 °C.
	tIntTarget := m.tBat + (83-m.tBat)*g
	m.tInt += (tIntTarget - m.tInt) * clamp(dt/300, 0, 1)

	// Малый очень медленный дрейф ячеек + слабая балансировка к среднему.
	for i := range m.off {
		m.off[i] += (m.rnd.Float64()*2 - 1) * 0.0004 * clamp(dt/60, 0, 1)
		m.off[i] *= 0.999
		m.off[i] = clamp(m.off[i], -0.008, 0.008)
	}

	// Режим: 'b'/'B' с редкой сменой, иногда поиск MPP 'S'.
	if now.After(m.nextSrch) {
		m.srchEnd = now.Add(60 * time.Second)
		m.nextSrch = now.Add(time.Duration(120+m.rnd.Intn(240)) * time.Second)
	}
}

// Snapshot — текущий срез.
func (m *Model) Snapshot() Sample {
	p := m.p
	uBase := p.CellLo + (p.CellHi-p.CellLo)*m.soc
	vBat := m.vbat()
	pPV := p.PanelPmax * m.gNow() * p.OutputScale
	pBat := pPV - p.LoadW
	iCh := 0.0
	if pBat > 0 {
		iCh = pBat / vBat
	}
	vPV := p.Vmp * (1 + (m.rnd.Float64()*2-1)*0.01)
	if vPV <= 1 {
		vPV = p.Vmp
	}
	iPV := pPV / vPV

	s := Sample{
		VcPV: vPV, IcPV: iPV, PPV: pPV, VBat: vBat,
		TempBat: m.tBat, TempInt: m.tInt, Mode: 'b', SOC: m.soc, Relay: m.relay,
	}
	s.Ucell = make([]float64, p.Cells)
	s.Takb = make([]float64, p.Cells)
	s.QCell = make([]float64, p.Cells)
	cellAmps := 0.0
	if pBat > 0 {
		cellAmps = iCh / float64(p.Cells)
	}
	for i := 0; i < p.Cells; i++ {
		s.Ucell[i] = uBase + m.off[i]
		s.Takb[i] = m.tBat + m.off[i]*20 // малый тепловой разброс
		s.QCell[i] = cellAmps
	}
	perKES := iPV / float64(p.KESCount)
	s.Imppt = make([]float64, p.KESCount)
	for i := range s.Imppt {
		s.Imppt[i] = perKES
	}
	return s
}

// gNow — текущая облучённость (без изменения состояния) для согласованности.
func (m *Model) gNow() float64 {
	sec := float64(m.last.Hour()*3600 + m.last.Minute()*60 + m.last.Second())
	day := 0.5 - 0.5*math.Cos(2*math.Pi*(sec/86400.0)) // пик в полдень
	return clamp((0.15+0.85*day)*m.cloud, 0.05, 1.0)
}

// vbat — напряжение пакета по SOC и текущему разбросу ячеек.
func (m *Model) vbat() float64 {
	u := m.p.CellLo + (m.p.CellHi-m.p.CellLo)*m.soc
	sum := 0.0
	for _, o := range m.off {
		sum += u + o
	}
	return sum
}

// SetRelay — состояние реле (биты 0/1/2), для эмуляции команд.
func (m *Model) SetRelay(v byte) { m.relay = v }
