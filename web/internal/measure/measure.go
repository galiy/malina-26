// Package measure — единый слой физического пересчёта сырых значений МАП/КЭС
// в человеческие единицы. Общий для менеджера событий (пороги из настроек) и UI
// статистики, чтобы формулы не расходились. Эталон — панель 1 `map.js` (`compute`).
//
// Слой оперирует уже слитыми значениями (u8/u16 → uint32); выбор конкретного поля
// из снимка/строки контракта — на стороне вызывающего.
package measure

// Volt220 — напряжение сети/выхода МАП: 0 — нет сети, иначе значение+100 (В).
func Volt220(raw uint32) float64 {
	if raw == 0 {
		return 0
	}
	return float64(raw) + 100
}

// Inet — ток сети (МАП): INET_16_4 делится на 4 при INET≥16, иначе на 16 (А).
func Inet(inet, inet164 uint32) float64 {
	d := 16.0
	if inet >= 16 {
		d = 4
	}
	return float64(inet164) / d
}

// IsCharge — режим заряда: MODE_RAM>3 (эквивалентно modeNum ∈ 4..9 в панели 1).
func IsCharge(modeRAM uint32) bool { return modeRAM > 3 }

// Iacc — ток АКБ (МАП): u16/16 (А), знак по режиму (заряд → «+», иначе «−»).
func Iacc(u16, modeRAM uint32) float64 {
	a := float64(u16) / 16
	if !IsCharge(modeRAM) {
		a = -a
	}
	return a
}

// Power100 — мощность МАП по паре байт (L, H): (L+H·256)·100 (Вт).
func Power100(l, h uint32) float64 { return float64(l+h*256) * 100 }

// BattVolt — напряжение АКБ (МАП): (VH·256+VL)/10 (В).
func BattVolt(vh, vl uint32) float64 { return float64(vh*256+vl) / 10 }

// Temp — температура МАП/КЭС: значение−50 (°C).
func Temp(raw uint32) float64 { return float64(raw) - 50 }

// Centi — величина в «сотых» (КЭС/BMS: VcPV, IcPV, VBat, ICh, IOut, ячейки BMS).
func Centi(raw uint32) float64 { return float64(raw) / 100 }

// Freq6250 — частота сети/выхода МАП: 6250/значение (Гц); 0 — нет данных.
func Freq6250(raw uint32) (float64, bool) {
	if raw == 0 {
		return 0, false
	}
	return 6250 / float64(raw), true
}

// MpptCurrent — ток параллельного MPPT: (L+(H&0x7F)·256)/16 (А).
func MpptCurrent(l, h uint32) float64 { return float64(l+(h&0x7F)*256) / 16 }
