package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// antBMS — тип AntBMS-DSP (дисплейный порт, пассивный). docs/bms/antbms-dsp.md.
type antBMS struct{}

func (antBMS) TypeID() uint8  { return typeAntBMS }
func (antBMS) Name() string   { return "AntBMS-DSP" }
func (antBMS) Framing() uint8 { return framingNative }
func (antBMS) Speeds() []int  { return []int{19200} }

// MissThreshold: пассивный поток — потеря после двух подряд пустых окон.
// Запас нужен, т.к. Poll делает Flush и может отбросить кадр на границе.
func (antBMS) MissThreshold() int { return 2 }

// MaxAddr: пассивный тип без адресации — единственное устройство на порту.
func (antBMS) MaxAddr() int { return 0 }

// antMarker — заголовок live-кадра.
var antMarker = []byte{0xAA, 0x55, 0xAA, 0xFF}

const antFrameLen = 140

// takeAntFrame извлекает первый валидный кадр и возвращает остаток буфера.
// Если полного валидного кадра нет, remainder начинается с первого возможного
// маркера (включая неполный) — так частичный кадр не теряется между чтениями.
func takeAntFrame(b []byte) (frame, rest []byte) {
	for i := 0; i+antFrameLen <= len(b); i++ {
		if b[i] != antMarker[0] {
			continue
		}
		if !bytesEq(b[i:i+4], antMarker) {
			continue
		}
		var sum int
		for _, x := range b[i+4 : i+138] {
			sum += int(x)
		}
		if uint16(sum) != binary.BigEndian.Uint16(b[i+138:i+140]) {
			continue
		}
		return b[i : i+antFrameLen], b[i+antFrameLen:]
	}
	// Полного кадра нет: оставляем всё, начиная с первого префикса маркера.
	for i := 0; i < len(b); i++ {
		l := len(antMarker)
		if left := len(b) - i; left < l {
			l = left
		}
		if bytesEq(b[i:i+l], antMarker[:l]) {
			return nil, b[i:]
		}
	}
	return nil, nil
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readAntFrame читает доступный пассивный поток и возвращает ПОСЛЕДНИЙ
// валидный кадр. Чтение идёт до первого валидного кадра (общий дедлайн 500 мс);
// после него — короткий дренаж 150 мс, чтобы забрать уже доступные кадры.
// Это устраняет публикацию «головы» FIFO: при потоке быстрее опроса старые
// кадры не накапливаются.
func readAntFrame(t Transport) ([]byte, error) {
	const (
		readWindow  = 500 * time.Millisecond
		drainWindow = 150 * time.Millisecond
		drainWait   = 10 * time.Millisecond
	)
	t.SetReadTimeout(readWindow)
	defer t.SetReadTimeout(readWindow)
	deadline := time.Now().Add(readWindow)
	var (
		acc       []byte
		last      []byte
		frames    int
		total     int
		drainTill time.Time
	)
	var tmp [256]byte // переиспользуемый буфер (без аллокации на итерацию)
	for {
		rem := time.Until(deadline)
		if rem <= 0 {
			break
		}
		to := rem
		if last != nil {
			// После первого валидного кадра не ждём полное окно: добираем
			// только уже доступные байты.
			if d := time.Until(drainTill); d < to {
				to = d
			}
			if to > drainWait {
				to = drainWait
			}
			if to <= 0 {
				break
			}
		}
		t.SetReadTimeout(to)
		n, err := t.Read(tmp[:])
		if n > 0 {
			total += n
			acc = append(acc, tmp[:n]...)
			for {
				f, rest := takeAntFrame(acc)
				if f == nil {
					acc = rest // сохранить частичный кадр до следующего чтения
					break
				}
				last = f
				frames++
				acc = rest
				if drainTill.IsZero() {
					drainTill = time.Now().Add(drainWindow)
				}
			}
			if len(acc) > 4096 {
				acc = acc[len(acc)-antFrameLen:]
			}
		}
		if err != nil && err != errTimeout {
			if last != nil {
				break // уже есть валидный кадр — ошибку не теряем
			}
			return nil, err
		}
		if last != nil && !drainTill.IsZero() && !time.Now().Before(drainTill) {
			break
		}
	}
	if last != nil {
		lx.Debugf("ant: прочитано %d Б, кадров %d, выбран последний (uptime=%d)",
			total, frames, antUptime(last))
		return last, nil
	}
	return nil, errTimeout
}

// validateAntCells проверяет правдоподобность напряжений (только для Probe:
// в Poll аварийный кадр с 0 мВ должен публиковаться, а не считаться потерей).
// Достаточно хотя бы одной правдоподобной ячейки: кадр с частично мёртвыми
// ячейками принимается (авария видна), полностью недостоверный — отклоняется.
func validateAntCells(f []byte, n int) error {
	ok := 0
	for i := 0; i < n; i++ {
		mv := binary.BigEndian.Uint16(f[6+2*i : 8+2*i])
		if mv >= 1000 && mv <= 5000 {
			ok++
		}
	}
	if ok == 0 {
		return fmt.Errorf("antbms: нет правдоподобных напряжений ячеек (n=%d)", n)
	}
	return nil
}

// antUptime — uptime BMS из кадра (offset 87, u32 BE), для диагностики свежести.
func antUptime(f []byte) uint32 {
	if len(f) < 91 {
		return 0
	}
	return binary.BigEndian.Uint32(f[87:91])
}

// parseAntFrame разбирает 140 Б в контракт (§6 antbms-dsp).
func parseAntFrame(f []byte) (bmscontract.Telemetry, error) {
	var r bmscontract.Telemetry
	if len(f) < antFrameLen {
		return r, fmt.Errorf("antbms: кадр %d Б", len(f))
	}
	n := int(f[123])
	if n < 1 || n > bmscontract.MaxCells {
		return r, fmt.Errorf("antbms: ячеек %d", n)
	}
	if f[74] > 100 {
		return r, fmt.Errorf("antbms: soc %d", f[74])
	}
	r.CellCount = uint8(n)
	for i := 0; i < n; i++ {
		r.CellsMV[i] = binary.BigEndian.Uint16(f[6+2*i : 8+2*i])
	}
	r.CurrentDA = int32(binary.BigEndian.Uint32(f[70:74])) * 10 // 0.1А→0.01А
	r.SocDP = uint16(f[74]) * 10
	r.CapMAh = binary.BigEndian.Uint32(f[75:79]) / 1000 // µА·ч→мА·ч
	r.RemainingMAh = binary.BigEndian.Uint32(f[79:83]) / 1000
	r.TempCount = 6
	for i := 0; i < 6; i++ {
		r.TempsDC[i] = int16(binary.BigEndian.Uint16(f[91+2*i:93+2*i])) * 10 // °C→0.1°C
	}
	if f[103] != 0 {
		r.Flags |= bmscontract.FlagChargeMOS
	}
	if f[104] != 0 {
		r.Flags |= bmscontract.FlagDischargeMOS
	}
	if f[105] != 0 {
		r.Flags |= bmscontract.FlagBalancer
	}
	r.PowerW = int32(binary.BigEndian.Uint32(f[111:115]))
	var totalMV uint32
	for i := 0; i < n; i++ {
		totalMV += uint32(r.CellsMV[i])
	}
	r.TotalCV = uint16(totalMV / 10) // мВ→0.01В
	r.BalanceMask = binary.BigEndian.Uint32(f[132:136])

	// alarm (docs/bms/antbms-dsp.md «Раскладка alarm», docs/bms-reader.md §1):
	// коды MOS по 4 бита + system log (16 бит), LSB-first (setAlarmBit).
	setAlarmCode(&r.Alarm, 0, f[103]) // charge_mos    0..3
	setAlarmCode(&r.Alarm, 4, f[104]) // discharge_mos 4..7
	setAlarmCode(&r.Alarm, 8, f[105]) // balancer      8..11
	syslog := binary.BigEndian.Uint16(f[136:138])
	for k := 0; k < 16; k++ { // system log 12..27
		if syslog&(1<<uint(k)) != 0 {
			setAlarmBit(&r.Alarm, 12+k)
		}
	}

	r.Mode = modeFromCurrent(r.CurrentDA)
	return r, nil
}

// setAlarmCode упаковывает 4-битный код (0..15) в alarm, начиная с бита base
// (LSB-first), не меняя остальные биты.
func setAlarmCode(a *[bmscontract.AlarmLen]byte, base int, code byte) {
	for b := 0; b < 4; b++ {
		if code&(1<<uint(b)) != 0 {
			setAlarmBit(a, base+b)
		}
	}
}

// modeFromCurrent — канонический режим по знаку тока (docs/bms-reader.md §1).
func modeFromCurrent(currentDA int32) uint16 {
	switch {
	case currentDA > 0:
		return bmscontract.ModeDischarge
	case currentDA < 0:
		return bmscontract.ModeCharge
	default:
		return bmscontract.ModeIdle
	}
}

// antBase формирует BASE от ёмкости (серийника в кадре нет).
func antBase(f []byte) string {
	capAh := binary.BigEndian.Uint32(f[75:79]) / 1_000_000
	return fmt.Sprintf("AntBMS-DSP-%d", capAh)
}

// antProbeWindows — сколько окон по 500 мс слушаем пассивный поток при первичном
// обнаружении. Один промах не должен отправлять порт в отстойник: кадры AntBMS
// идут ~раз в секунду, одно 500-мс окно ловит их примерно через раз.
const antProbeWindows = 3

func (antBMS) Probe(t Transport, _ []int) (probeResult, error) {
	for i := 0; i < antProbeWindows; i++ {
		f, err := readAntFrame(t)
		if err != nil {
			continue
		}
		tel, err := parseAntFrame(f)
		if err != nil {
			continue
		}
		if err := validateAntCells(f, int(tel.CellCount)); err != nil {
			continue
		}
		return probeResult{Kind: probeFound, Slots: []probeSlot{{Addr: 0, Base: antBase(f)}}}, nil
	}
	return probeResult{Kind: probeNoAnswer}, nil
}

func (antBMS) Poll(t Transport, _ int) (bmscontract.Telemetry, error) {
	// Пассивный поток: сбрасываем накопленный FIFO перед чтением, иначе
	// публикуется самый старый кадр (см. docs/bms/antbms-dsp.md §5).
	if f, ok := t.(interface{ Flush() }); ok {
		f.Flush()
	}
	f, err := readAntFrame(t)
	if err != nil {
		return bmscontract.Telemetry{}, err
	}
	return parseAntFrame(f)
}
