package main

import (
	"strings"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// telemetry — значения в единицах контракта grid v1 (см. docs/grid-reader.md).
type telemetry struct {
	vals [24]int64
	// shm-only (в tsdb не пишутся)
	energyToday uint16
	energyTotal uint32
	energySat   bool
}

func clampU16(v uint64) (uint16, bool) {
	if v > 0xFFFF {
		return 0xFFFF, true
	}
	return uint16(v), false
}

func clampU24(v uint64) (uint32, bool) {
	if v > 0xFFFFFF {
		return 0xFFFFFF, true
	}
	return uint32(v), false
}

func (t *telemetry) value(id uint16, v int64) { t.vals[id-1] = v }

func (t *telemetry) contractValues() []tscontract.Value {
	out := make([]tscontract.Value, 0, 24)
	for i := 0; i < 24; i++ {
		out = append(out, tscontract.Value{ID: uint16(i + 1), Type: gridFieldType(uint16(i + 1)), Int: t.vals[i]})
	}
	return out
}

func gridFieldType(id uint16) tscontract.FieldType {
	switch id {
	case 8, 16, 17:
		return tscontract.FTI16
	case 18:
		return tscontract.FTU8
	default:
		return tscontract.FTU16
	}
}

func u32(lo, hi uint16) uint32 { return uint32(hi)<<16 | uint32(lo) }
func roundDiv(v int64, d int64) int64 {
	if v >= 0 {
		return (v + d/2) / d
	}
	return -((-v + d/2) / d)
}

// mapDeye строит телеметрию по регистрам диапазона 0x3B..0x74.
func mapDeye(regs map[uint16]uint16) telemetry {
	var t telemetry
	get := func(a uint16) int64 { return int64(regs[a]) }
	// PV: V ×0,1 В, I ×0,1 А; мощность = V×I → 10 Вт: rawV*rawI/1000.
	t.value(1, get(0x6D))
	t.value(2, get(0x6E)*10)
	t.value(3, roundDiv(get(0x6D)*get(0x6E), 1000))
	t.value(4, get(0x6F))
	t.value(5, get(0x70)*10)
	t.value(6, roundDiv(get(0x6F)*get(0x70), 1000))
	// AC: 32-бит low word first, ×0,1 Вт/вар → 10 Вт/вар.
	t.value(7, roundDiv(int64(u32(regs[0x56], regs[0x57])), 100))
	t.value(8, roundDiv(int64(int16(regs[0x58])), 100))
	t.value(9, get(0x4F))
	t.value(10, get(0x49))
	t.value(11, get(0x4C)*10)
	t.value(12, get(0x4A))
	t.value(13, get(0x4D)*10)
	t.value(14, get(0x4B))
	t.value(15, get(0x4E)*10)
	// Температуры: ×0,1 °C −100 → (raw−1000).
	t.value(16, get(0x5B)-1000) // IGBT
	t.value(17, get(0x5A)-1000) // радиатор
	t.value(18, get(0x3B))
	// Аварии: W1..W32 (0x65/0x66), F1..F64 (0x67..0x6A).
	t.value(19, get(0x65))
	t.value(20, get(0x66))
	t.value(21, get(0x67))
	t.value(22, get(0x68))
	t.value(23, get(0x69))
	t.value(24, get(0x6A))
	// Энергия (shm): 0,1 кВт·ч.
	t.energyToday = regs[0x3C]
	var sat bool
	t.energyTotal, sat = clampU24(uint64(u32(regs[0x3F], regs[0x40])))
	t.energySat = sat
	return t
}

// mapSofar строит телеметрию по регистрам диапазона 0x0000..0x0027.
func mapSofar(regs map[uint16]uint16) telemetry {
	var t telemetry
	get := func(a uint16) int64 { return int64(regs[a]) }
	t.value(1, get(0x0006))
	t.value(2, get(0x0007))
	t.value(3, get(0x000A))
	t.value(4, get(0x0008))
	t.value(5, get(0x0009))
	t.value(6, get(0x000B))
	t.value(7, get(0x000C))
	t.value(8, int64(int16(regs[0x000D])))
	t.value(9, get(0x000E))
	t.value(10, get(0x000F))
	t.value(11, get(0x0010))
	t.value(12, get(0x0011))
	t.value(13, get(0x0012))
	t.value(14, get(0x0013))
	t.value(15, get(0x0014))
	// Температуры Sofar — целые °C → 0,1 °C.
	t.value(16, int64(int16(regs[0x001B]))*10) // module
	t.value(17, int64(int16(regs[0x001C]))*10) // inner
	t.value(18, get(0x0000))
	t.value(19, get(0x0001))
	t.value(20, get(0x0002))
	t.value(21, get(0x0003))
	t.value(22, get(0x0004))
	t.value(23, get(0x0005))
	t.value(24, get(0x0021))
	// Энергия (shm): today ×10 Вт·ч → 0,1 кВт·ч = raw/10; total кВт·ч → ×10.
	t.energyToday, _ = clampU16(uint64(max64(0, roundDiv(get(0x0019), 10))))
	t.energyTotal, t.energySat = clampU24(uint64(u32(regs[0x0016], regs[0x0015])) * 10)
	return t
}

// asciiFromRegs собирает строку из регистров (старший байт — первый).
func asciiFromRegs(regs []uint16, skipFirst bool) string {
	var b []byte
	for i, v := range regs {
		if skipFirst && i == 0 {
			continue
		}
		for _, c := range []byte{byte(v >> 8), byte(v)} {
			if c == 0 {
				return strings.TrimRight(string(b), " ")
			}
			if c >= 0x20 && c <= 0x7E {
				b = append(b, c)
			}
		}
	}
	return strings.TrimRight(string(b), " ")
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// normalizeSofarSN срезает хвостовые версии V<digits>, как в sunReceiver.
func normalizeSofarSN(sn string) string {
	for {
		i := strings.LastIndexByte(sn, 'V')
		if i <= 0 {
			break
		}
		tail := sn[i+1:]
		if tail == "" || strings.Trim(tail, "0123456789") != "" {
			break
		}
		sn = sn[:i]
	}
	return sn
}
