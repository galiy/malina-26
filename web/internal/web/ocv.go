package web

// Оценка SoC АКБ по OCV-кривым (open-circuit voltage) из снимка МАП.
//
// На узле физического batmond/БМС-контроллера нет, но «Мастер-Узел» (агрегатор)
// показывает % заряда батареи (C_100_remain = SoC). МАП отдаёт аппаратное поле
// SoC (id=235/0x718) ТОЛЬКО для модульных АКБ со связью с МАП; в остальных
// случаях (и при отсутствии такой связи) оно = 255 «нет данных» — проверено на
// реальном железе (.60): при активной BMS (LCD_BMSFunk=3, BMS_Num=16) SoC=255.
//
// Поэтому: приоритет — аппаратному полю SoC (если 0..100), иначе OCV-оценка по
// типу АКБ (LCD_AccType), номиналу (UACC) и текущему напряжению (UAcc_med_VH/VL)
// с поправкой на ток (IAcc_med_A_u16) и ёмкость (настройка LCD_CAcc):
//   заряд смещает измеренное напряжение вверх, разряд — вниз.
//
// Значения ячеек BMS (UakbCell/TakbCell/Q_Cell) НЕ используются — они фейковые
// (источник на текущем узле — эмулятор). Источник напряжения — общее UAcc_med,
// число ячеек/батарей выводится из номинала МАП.
//
// Кривые — типовые аппроксимации разряда (см. docs/legacy/batmon-api.md §5).
// Требуют верификации/настройки под реальные АКБ — значения приблизительные.

// ocvPoint — точка OCV-кривой: напряжение (В, per-cell или per-12В-блок) → SoC %.
type ocvPoint struct {
	v   float64
	soc float64
}

// OCV-кривые. Порядок — по возрастанию напряжения.
//
// liFePO4OCV — per-cell (LiFePO4/LYP, тип 4/5), построена по ФАКТИЧЕСКОЙ
// статистике pg-БД (sunReceiver) за неделю: пары (напряжение АКБ МАП ↔
// взвешенный средний SoC трёх BMS по весам 1/2/2) в состоянии покоя (|I|<1А).
// Низ (51,2–51,5 В → ~10,4%) и верх (54,7–57 В → ~96,6%) — измеренные точки
// покоя; средняя часть (51,5→54,7 В) в покое за неделю не встречалась (батареи
// либо разряжены, либо заряжены) — интерполяция по «полке» LFP. Монотонная.
var liFePO4OCV = []ocvPoint{
	{2.90, 0}, {3.05, 5}, {3.10, 7}, {3.15, 9}, {3.19, 10.3}, {3.22, 10.3},
	{3.25, 15}, {3.28, 25}, {3.31, 40}, {3.34, 58}, {3.37, 75}, {3.40, 90},
	{3.42, 96.2}, {3.44, 96.3}, {3.47, 96.5}, {3.50, 96.5}, {3.56, 96.6},
	{3.60, 97}, {3.65, 100},
}

// leadOCV12 — классический свинцово-кислотный, на 12-В блок (тип 0/3).
var leadOCV12 = []ocvPoint{
	{11.30, 0}, {11.50, 5}, {11.60, 10}, {11.75, 20}, {11.90, 30}, {12.05, 40},
	{12.20, 50}, {12.35, 60}, {12.50, 70}, {12.60, 80}, {12.70, 90}, {12.80, 100},
}

// gelOCV12 — гелевый/AGM, на 12-В блок (тип 1/2): чуть выше кривая разряда.
var gelOCV12 = []ocvPoint{
	{11.40, 0}, {11.60, 5}, {11.75, 10}, {11.90, 20}, {12.05, 30}, {12.20, 40},
	{12.35, 50}, {12.50, 60}, {12.65, 70}, {12.75, 80}, {12.85, 90}, {12.90, 100},
}

// uaccNominalVolts — номинал АКБ по UACC (битовое поле 0..3 → 12/24/48/96 В).
var uaccNominalVolts = []float64{12, 24, 48, 96}

// Тип АКБ (LCD_AccType), см. mi.acctype в i18n: 0..5.
const (
	accTypeLead     = 0 // Кислотный
	accTypeGelAGM   = 1 // Гелевый/AGM
	accTypeAGMShoto = 2 // AGM Shoto (гель/AGM)
	accTypeTrojan   = 3 // Кислотный Trojan
	accTypeLiyp39   = 4 // Li-ion LYP 3.9В (LiFePO4)
	accTypeLiyp37   = 5 // Li-ion LYP 3.7В (LiFePO4)
)

// Коэффициенты токовой поправки OCV, В на (ток в 1C). Направление задаётся
// режимом (заряд/разряд), а не знаком модуля тока.
const (
	ocvKLiCell = 0.05 // В/ячейку/1C (LiFePO4)
	ocvKLead12 = 0.18 // В/12В-блок/1C (свинец)
)

// socFromOCV — линейная интерполяция SoC по напряжению u вдоль кривой tbl.
// Напряжение ниже первой/выше последней точки — клампится к 0/100%.
func socFromOCV(tbl []ocvPoint, u float64) float64 {
	if len(tbl) == 0 {
		return 0
	}
	if u <= tbl[0].v {
		return tbl[0].soc
	}
	for i := 1; i < len(tbl); i++ {
		if u <= tbl[i].v {
			dv := tbl[i].v - tbl[i-1].v
			ds := tbl[i].soc - tbl[i-1].soc
			if dv <= 0 {
				return tbl[i].soc
			}
			return tbl[i-1].soc + (u-tbl[i-1].v)/dv*ds
		}
	}
	return tbl[len(tbl)-1].soc
}

// batSocPercent — оценка заряда АКБ (%) с приоритетом аппаратному полю SoC.
// Возвращает (soc, ok): ok=false, если данные недоступны (нет МАП/нет снимка) —
// тогда вызывающий оставляет прежнее значение (0).
func batSocPercent() (float64, bool) {
	ml := loadMapList()
	if len(ml.Maps) == 0 {
		return 0, false
	}
	payload := mapRawPayload(ml.Maps[0].ID)
	if payload == nil {
		return 0, false
	}
	f := decodeMapFields(payload)
	if f == nil {
		return 0, false
	}
	return batSocFromFields(f)
}

// batSocFromFields — оценка заряда по распакованным полям снимка МАП.
// Отдельная (чистая) функция — для юнит-тестов без обращений к shm.
func batSocFromFields(f map[string]uint32) (float64, bool) {
	// Приоритет: аппаратный SoC (id 235, 0x718). 255 = «нет данных».
	if soc, ok := f["SoC"]; ok && soc != 255 && soc <= 100 {
		return float64(soc), true
	}

	accType, okType := f["LCD_AccType"]
	uacc, okU := uaccFromFields(f)
	if !okU {
		return 0, false
	}
	sh := f["UACC"] & 0x1f
	nom := 12.0
	if int(sh) < len(uaccNominalVolts) {
		nom = uaccNominalVolts[sh]
	}

	// Направление заряд/разряд по режиму (как legacy.go: режимы 4..9 — заряд).
	charging := false
	if mode, ok := f["MODE"]; ok && mode >= 4 && mode <= 9 {
		charging = true
	}
	cur := (float64(f["IAcc_med_A_u16_L"]) + float64(f["IAcc_med_A_u16_H"])*256) / 16
	iBat := cur
	if !charging {
		iBat = -cur
	}

	// Ёмкость АКБ (Ач) — настройка «полной ёмкости» МАП, формула РОДНОГО UI
	// (map.js: cacc = (LCD_CAcc*25) >> UACC). Узел .60: LCD_CAcc=80, UACC=2 →
	// (80*25)>>2 = 500 Ач. 0 → поправка на ток не применяется.
	ah := float64((f["LCD_CAcc"]*25) >> sh)

	if accType == accTypeLiyp39 || accType == accTypeLiyp37 {
		// Литий: число ячеек по номиналу сборки LFP (12В=4S, 24В=8S, 48В=16S, 96В=32S);
		// банки BMS не используем (фейковые).
		ns := liCellsForNom(nom)
		if ns <= 0 {
			return 0, false
		}
		uCell := uacc / float64(ns)
		if ah > 0 {
			uCell -= ocvKLiCell * (iBat / ah)
		}
		if !okType {
			return 0, false
		}
		return clampSoc(socFromOCV(liFePO4OCV, uCell)), true
	}

	// Свинцовые/гель: число 12-В батарей из номинала МАП.
	blocks := nom / 12
	if blocks <= 0 {
		blocks = 1
	}
	u12 := uacc / blocks
	tbl := leadOCV12
	if accType == accTypeGelAGM || accType == accTypeAGMShoto {
		tbl = gelOCV12
	}
	if ah > 0 {
		u12 -= ocvKLead12 * (iBat / ah)
	}
	return clampSoc(socFromOCV(tbl, u12)), true
}

// uaccFromFields — измеренное напряжение АКБ (В) с приоритетом наиболее точного
// (OCV-подобного) источника:
//   1) UAcc_Optim_VL/VH (0x520/0x521, ×0,1) — «среднее между измеренным и не
//      учитывающим нагрузку»: ближайшее к OCV, компенсирует падение под нагрузкой;
//   2) UAcc_med_VS_100 (0x522/0x523, ×0,01) — то же напряжение, точность ×100;
//   3) UAcc_med_VH/VL (0x405/0x406, ×0,1) — измеренное среднее.
// Возвращает (В, ok). ok=false при отсутствии/нулевом значении всех источников.
func uaccFromFields(f map[string]uint32) (float64, bool) {
	if h, ok := f["UAcc_Optim_VH"]; ok {
		if l, ok2 := f["UAcc_Optim_V"]; ok2 {
			v := float64(h*256+l) / 10
			if v > 0 {
				return v, true
			}
		}
	}
	if h, ok := f["UAcc_med_VS_100H"]; ok {
		if l, ok2 := f["UAcc_med_VS_100L"]; ok2 {
			v := float64(h*256+l) / 100
			if v > 0 {
				return v, true
			}
		}
	}
	h, okH := f["UAcc_med_VH"]
	l, okL := f["UAcc_med_VL"]
	if okH && okL {
		v := float64(h*256+l) / 10
		if v > 0 {
			return v, true
		}
	}
	return 0, false
}

// liCellsForNom — число последовательных ячеек LFP по номиналу сборки:
// 12В=4S (3,2 В×4=12,8), 24В=8S, 48В=16S (51,2), 96В=32S. Номинальный вольтаж
// ячейки LFP ≈3,2 В, поэтому берём кратное номиналу, а не деление на 3,25.
func liCellsForNom(nom float64) int {
	switch int(nom) {
	case 12:
		return 4
	case 24:
		return 8
	case 48:
		return 16
	case 96:
		return 32
	default:
		c := int(nom/3.2 + 0.5)
		if c < 1 {
			c = 1
		}
		return c
	}
}

// clampSoc ограничивает SoC диапазоном 0..100.
func clampSoc(s float64) float64 {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}