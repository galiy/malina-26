// units.go — единицы измерения и физический пересчёт полей телеметрии для UI
// статистики. Сырьё хранится как есть (docs/stat.md: «физический пересчёт —
// только на отображении, в контракт не входит»); конверсия применяется при
// рендере таблиц.
//
// Формулы — по эталону панели 1 МАП (`web/assets/static/map.js`, функция
// `compute`), а также docs/stat.md (КЭС) и docs/legacy/bms-api.md (BMS).
// Общие формулы МАП вынесены в `web/internal/measure` (единый слой с менеджером
// событий); здесь — только привязка «поле → единица/пересчёт» и КЭС/BMS.
// Где масштаб не задокументирован — поле без единицы и без пересчёта.
package web

import "github.com/galiy/malina-26/web/internal/measure"

// fieldKind — способ представления значения поля.
type fieldKind uint8

const (
	kindNum    fieldKind = iota // число (после пересчёта)
	kindSymbol                  // одиночный ASCII-символ (КЭС Mode)
	kindRelay3                  // битфилд реле: биты 0/1/2 → 3 колонки 0/1
)

// fieldConv — единица и пересчёт. Num вызывается для kindNum и получает сырое
// значение поля и сырые значения всей строки (по field_id, только присутствующие
// в payload) — часть формул зависит от других полей (INET_16_4 от INET, Q_Cell от
// UakbCell); если зависимое поле отсутствует — Num возвращает ok=false («—»).
// Raw=true — значение показывается без пересчёта (код/битфилд/недокументированное);
// UI помечает такие колонки как «сырые».
type fieldConv struct {
	Kind fieldKind
	Unit string
	Raw  bool
	Num  func(v float64, row map[uint16]float64) (float64, bool)
}

func numConv(unit string, f func(v float64, row map[uint16]float64) (float64, bool)) fieldConv {
	return fieldConv{Kind: kindNum, Unit: unit, Num: f}
}

// passConv — документированное прямое значение (без пересчёта, но не «сырое»).
func passConv(unit string) fieldConv {
	return numConv(unit, func(v float64, _ map[uint16]float64) (float64, bool) { return v, true })
}

// rawConv — недокументированное/кодовое поле: показывается сырым (Raw).
func rawConv() fieldConv {
	return fieldConv{Kind: kindNum, Raw: true, Num: func(v float64, _ map[uint16]float64) (float64, bool) { return v, true }}
}

func linConv(unit string, scale, offset float64) fieldConv {
	return numConv(unit, func(v float64, _ map[uint16]float64) (float64, bool) { return v*scale + offset, true })
}

// voltPlus100 — напряжение сети/выхода МАП (measure.Volt220).
func voltPlus100() fieldConv {
	return numConv("В", func(v float64, _ map[uint16]float64) (float64, bool) {
		return measure.Volt220(uint32(v)), true
	})
}

// freq6250 — частота МАП (measure.Freq6250); 0 — нет данных.
func freq6250() fieldConv {
	return numConv("Гц", func(v float64, _ map[uint16]float64) (float64, bool) {
		return measure.Freq6250(uint32(v))
	})
}

// inet164 — ток сети (МАП) со знаком по PNET_Sign_P. Без INET или без
// PNET_Sign_P — нет данных.
func inet164() fieldConv {
	return numConv("А", func(v float64, row map[uint16]float64) (float64, bool) {
		inet, ok := row[2] // INET
		if !ok {
			return 0, false
		}
		sign, ok := row[33] // PNET_Sign_P
		if !ok {
			return 0, false
		}
		return measure.GridCurrentSigned(measure.Inet(uint32(inet), uint32(v)), uint32(sign)), true
	})
}

// iaccU16 — ток АКБ (МАП) для отображения: заряд (MODE_RAM==4) «−»,
// разряд «+». Без MODE — нет данных.
func iaccU16() fieldConv {
	return numConv("А", func(v float64, row map[uint16]float64) (float64, bool) {
		mode, ok := row[19] // MODE_RAM
		if !ok {
			return 0, false
		}
		return measure.BatteryCurrentSigned(float64(v)/16, uint32(mode)), true
	})
}

// tempShift50 — температуры МАП/КЭС (measure.Temp).
func tempShift50() fieldConv {
	return numConv("°C", func(v float64, _ map[uint16]float64) (float64, bool) {
		return measure.Temp(uint32(v)), true
	})
}

// tempBms — температура ячейки BMS: `255` — нет датчика → нет данных («—»),
// иначе `V−50` (°C). (Панель 1 показывала 127; для таблицы это вводит в
// заблуждение, поэтому трактуем как «нет данных».)
func tempBms() fieldConv {
	return numConv("°C", func(v float64, _ map[uint16]float64) (float64, bool) {
		if v == 255 {
			return 0, false
		}
		return v - 50, true
	})
}

// uakbCell — напряжение ячейки BMS: `(V & 0x7FFF)/100` В (старший бит — маска).
func uakbCell() fieldConv {
	return numConv("В", func(v float64, _ map[uint16]float64) (float64, bool) {
		return float64(int64(v)&0x7FFF) / 100, true
	})
}

// qCell — ток ячейки BMS: `(Q/100)·UakbCell` А; без напряжения той же банки — нет данных.
func qCell(id uint16) fieldConv {
	return numConv("А", func(v float64, row map[uint16]float64) (float64, bool) {
		u, ok := row[id-2] // UakbCell
		if !ok {
			return 0, false
		}
		vol := float64(int64(u)&0x7FFF) / 100
		return v / 100 * vol, true
	})
}

// windConv — скорость ветрогенератора: `0xFFFF` — нет данных.
func windConv() fieldConv {
	return numConv("", func(v float64, _ map[uint16]float64) (float64, bool) {
		if v == 65535 {
			return 0, false
		}
		return v, true
	})
}

// fieldConvFor возвращает единицу и пересчёт для (type, version, field_id).
func fieldConvFor(typ string, _ uint16, id uint16) (fieldConv, bool) {
	switch typ {
	case "map":
		switch id {
		case 1:
			return voltPlus100(), true // UNET
		case 2:
			return passConv("А"), true // INET
		case 3:
			return inet164(), true // INET_16_4
		case 4, 5:
			return linConv("Вт", 100, 0), true // PNET, PLoad
		case 6:
			return voltPlus100(), true // UOUTmed
		case 7:
			return freq6250(), true // TFNET
		case 8:
			return linConv("В", 0.1, 0), true // UAcc_med
		case 10:
			return iaccU16(), true // IAcc_med_A_u16
		case 14, 15, 16:
			return tempShift50(), true // Temp_Grad0/1/2
		case 27: // Relay (map v2)
			return fieldConv{Kind: kindRelay3}, true
		case 28: // ThFMAP (map v3) — частота выхода, Гц
			return freq6250(), true
		case 29, 30, 31, 32: // I_Akb_MPPT0..3 (map v3) — ток канала, А ((v&0x7FFF)/16)
			return numConv("А", func(v float64, _ map[uint16]float64) (float64, bool) {
				return float64(int64(v)&0x7FFF) / 16, true
			}), true
		case 33: // PNET_Sign_P (map v3) — флаг знака мощности сети
			return rawConv(), true
		default: // коды/битфилды/недокументированное (9, 11..13, 17..26)
			return rawConv(), true // код/битфилд/недокументированное (9, 11..13, 17..26)
		}
	case "kes":
		switch id {
		case 1, 5:
			return linConv("В", 0.01, 0), true // VcPV, VBat
		case 2, 6, 11:
			return linConv("А", 0.01, 0), true // IcPV, ICh, IOut
		case 3, 7, 9, 10:
			return linConv("Вт", 1, 0), true // PPV, PCurr, POut, PLoad
		case 4:
			return windConv(), true // Windspeed
		case 8, 12:
			return tempShift50(), true // TempBat, TempInt
		case 13:
			return fieldConv{Kind: kindSymbol}, true // Mode
		case 14:
			return fieldConv{Kind: kindRelay3}, true // RelayC
		default:
			return rawConv(), true
		}
	case "mbms":
		switch id % 3 {
		case 1:
			return uakbCell(), true // UakbCell
		case 2:
			return tempBms(), true // TakbCell
		default:
			return passConv("А"), true // Q_Cell — ток ячейки, А (u8, как есть)
		}
	case "grid":
		// Контракт grid v1 (docs/grid-reader.md): значения в единицах контракта,
		// пересчёт в физические единицы — на отображении.
		switch id {
		case 1, 4, 10, 12, 14:
			return linConv("В", 0.1, 0), true
		case 2, 5, 11, 13, 15:
			return linConv("А", 0.01, 0), true
		case 3, 6, 7:
			return linConv("Вт", 10, 0), true
		case 8:
			return linConv("вар", 10, 0), true
		case 9:
			return linConv("Гц", 0.01, 0), true
		case 16, 17:
			return linConv("°C", 0.1, 0), true
		default:
			return rawConv(), true // статус и слова аварий
		}
	case "bms":
		// Контракт bms v1 (docs/bms-reader.md §1): значения в единицах контракта
		// (мВ, 0.01 А, 0.1 %, …), приведение/масштаб — на клиенте (stats_charts.js div).
		return rawConv(), true
	}
	return fieldConv{}, false
}
