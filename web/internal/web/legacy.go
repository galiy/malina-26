package web

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/galiy/malina-26/web/internal/mapmode"
)

// legacy-api: совместимость со старым read_json.php?device=map (mapd).
// Формат/формулы сверены с живым mapd на боевом узле (см. docs/legacy_read_json_map.md).

// legacyFieldIdx — имя поля -> id в контракте MapData (порядок mapFieldNames).
var legacyFieldIdx = func() map[string]int {
	m := make(map[string]int, len(mapFieldNames))
	for i, n := range mapFieldNames {
		if _, dup := m[n]; !dup {
			m[n] = i
		}
	}
	return m
}()

type legacyMeta struct {
	Gen      uint64
	LastSeen int64
}

type legacyFields struct {
	val  []uint32
	pres []bool
	meta legacyMeta
}

// legacyContractVersion — версия бинарного контракта map-reader, которую понимает
// legacy-api. Обязана совпадать с contractVersion в daemons/map-reader/contract.go:
// при несовпадении decodeLegacy отвергает payload, снимок read_json не обновляется,
// а check_daemons отдаёт mapd=1.
//
// Потребитель этой версии — внешний легаси-агрегатор «Мастер-Узел» (другие
// узлы): страница master_node.php опрашивает узел через request_node.php
// ручками /check_daemons.php и /read_json.php?device=map.
// Если check_daemons отвечает mapd=1, блок МАП не рисуется и read_json вообще
// не запрашивается — телеметрия «не идёт» (см. docs/legacy/master-node.md).
// При подъёме contractVersion в map-reader обязательно править и эту константу.
const legacyContractVersion = 5

// decodeLegacy разбирает бинарный payload слота (как daemon/contract.go).
func decodeLegacy(payload []byte) (*legacyFields, bool) {
	n := len(mapFieldNames)
	bitmapLen := (n + 7) / 8
	if len(payload) < 64+bitmapLen+n*4 {
		return nil, false
	}
	if binary.LittleEndian.Uint32(payload[0:4]) != 0x4441504D {
		return nil, false
	}
	if binary.LittleEndian.Uint16(payload[4:6]) != legacyContractVersion {
		return nil, false
	}
	if int(binary.LittleEndian.Uint16(payload[6:8])) != n {
		return nil, false
	}
	f := &legacyFields{
		val:  make([]uint32, n),
		pres: make([]bool, n),
		meta: legacyMeta{
			Gen:      binary.LittleEndian.Uint64(payload[48:56]),
			LastSeen: int64(binary.LittleEndian.Uint64(payload[56:64])),
		},
	}
	for i := 0; i < n; i++ {
		f.pres[i] = payload[64+i/8]&(1<<(uint(i)%8)) != 0
		off := 64 + bitmapLen + i*4
		f.val[i] = binary.LittleEndian.Uint32(payload[off : off+4])
	}
	return f, true
}

func (f *legacyFields) u(name string) uint32 {
	if i, ok := legacyFieldIdx[name]; ok {
		return f.val[i]
	}
	return 0
}

func (f *legacyFields) s16(name string) int32 { return int32(int16(uint16(f.u(name)))) }

// u24 — 24-битное значение по трём однобайтовым ячейкам (_L/_H/_HH).
func (f *legacyFields) u24(l, h, hh string) uint64 {
	return uint64(f.u(l)) | uint64(f.u(h))<<8 | uint64(f.u(hh))<<16
}

// legacyMemSize — размер адресуемого образа памяти МАП (0x000..0x7FF), как
// читает legacy read_memory.php.
const legacyMemSize = 2048

// buildMem собирает байтовый образ памяти из полей контракта (обратное
// преобразование): значение каждой присутствующей ячейки раскладывается LE по
// её адресу и размеру; незаданные регионы остаются нулями.
func buildMem(f *legacyFields) []byte {
	mem := make([]byte, legacyMemSize)
	for i := range mapCellAddr {
		if !f.pres[i] {
			continue
		}
		addr := int(mapCellAddr[i])
		size := int(mapCellSize[i])
		v := f.val[i]
		for b := 0; b < size && addr+b < legacyMemSize; b++ {
			mem[addr+b] = byte(v >> (8 * uint(b)))
		}
	}
	return mem
}

// legacyAcc — накопители _E_*_B (в памяти, при рестарте теряются).
type legacyAcc struct {
	started                   bool
	baseNet, baseAcc, baseChg int64
	lastNet, lastAcc, lastChg uint64
}

type legacyEnergy struct {
	net, acc, chg             uint64
	signNet                   int64
	baseNet, baseAcc, baseChg int64
}

// updateE — daily = счётчики МАП; на «перевороте» (уменьшение) фиксируем в base.
// Для знакового _E_NET_SIGN накопитель не ведём: он законно меняет знак, поэтому
// «уменьшение» не означает переполнение (иначе base копил бы мусор).
func (a *legacyAcc) updateE(net, acc, chg uint64, sign int64) legacyEnergy {
	if a.started {
		if net < a.lastNet {
			a.baseNet += int64(a.lastNet)
		}
		if acc < a.lastAcc {
			a.baseAcc += int64(a.lastAcc)
		}
		if chg < a.lastChg {
			a.baseChg += int64(a.lastChg)
		}
	}
	a.started = true
	a.lastNet, a.lastAcc, a.lastChg = net, acc, chg
	return legacyEnergy{
		net: net, acc: acc, chg: chg, signNet: sign,
		baseNet: a.baseNet, baseAcc: a.baseAcc, baseChg: a.baseChg,
	}
}

// _I_phN: ((H&0x7F)<<8 | L)/10, знак — бит 0x80 в H.
func phCurrent(l, h uint32) float64 {
	v := float64((l & 0xff) | ((h & 0x7f) << 8))
	if h&0x80 != 0 {
		v = -v
	}
	return v / 10
}

// kesRec — один КЭС (из ячеек МАП).
type kesRec struct {
	I float64
	T int
}

// mpptList возвращает список КЭС из ячеек МАП (I_Akb_MPPT_*/Takb_MPPT*).
// Количество — LCD_MPPTNum (0x157), при наличии MPPT (LCD_BMSFunk ∈ {2,3}).
func mpptList(f *legacyFields) []kesRec {
	bmsf := f.u("LCD_BMSFunk")
	n := int(f.u("LCD_MPPTNum"))
	if (bmsf != 2 && bmsf != 3) || n < 1 || n > 16 {
		return nil
	}
	out := make([]kesRec, 0, n)
	for i := 0; i < n; i++ {
		l := f.u(fmt.Sprintf("I_Akb_MPPT_L%d", i))
		h := f.u(fmt.Sprintf("I_Akb_MPPT_H%d", i))
		cur := float64((l&0xff)|((h&0x7f)<<8)) / 16 // А*16, маска старшего бита
		t := int(f.u(fmt.Sprintf("Takb_MPPT%d", i))) - 50
		out = append(out, kesRec{I: cur, T: t})
	}
	return out
}

// bmsRec — одна банка BMS.
type bmsRec struct {
	V float64
	T int
	I float64
}

// bmsList возвращает список банок BMS из ячеек МАП (UakbCell_*/TakbCell*/Q_Cell*).
// Количество — BMS_Num (0x100), при наличии BMS (LCD_BMSFunk ∈ {1,3}).
func bmsList(f *legacyFields) []bmsRec {
	bmsf := f.u("LCD_BMSFunk")
	n := int(f.u("BMS_Num"))
	if (bmsf != 1 && bmsf != 3) || n < 1 || n > 32 {
		return nil
	}
	out := make([]bmsRec, 0, n)
	for i := 0; i < n; i++ {
		l := f.u(fmt.Sprintf("UakbCell_L%d", i))
		h := f.u(fmt.Sprintf("UakbCell_H%d", i))
		v := float64((l&0xff)|((h&0x7f)<<8)) / 100 // В*100, маска старшего бита
		raw := int(f.u(fmt.Sprintf("TakbCell%d", i)))
		t := raw - 50
		if raw == 255 {
			t = 127 // нет датчика (как ждёт панель)
		}
		q := float64(f.u(fmt.Sprintf("Q_Cell%d", i)))
		out = append(out, bmsRec{V: v, T: t, I: q / 100 * v})
	}
	return out
}

// renderLegacy формирует готовую строку JSON (формат mapd).
func renderLegacy(f *legacyFields, a *legacyAcc) []byte {
	unetRaw := f.u("UNET")
	inet := f.u("INET")
	uacc := float64(f.u("UAcc_med_VH")*256+f.u("UAcc_med_VL")) / 10
	sh := f.u("UACC") & 0x1f
	uch := float64(f.u("E_LCD_UAccChMax")<<sh) / 10
	ubuf := float64(f.u("E_LCD_UAccChBUF")<<sh) / 10

	var unet uint32
	if unetRaw != 0 {
		unet = unetRaw + 100
	}
	var uout uint32
	if u := f.u("UOUTmed"); u != 0 {
		uout = u + 100
	}
	unetLimit := f.u("UNET_Limit") + 100

	// Разрешение _INET_16_4 зависит от диапазона тока сети.
	inet16_4 := float64(f.u("INET_16_4"))
	if inet >= 16 {
		inet16_4 /= 4
	} else {
		inet16_4 /= 16
	}
	pnetCalc := float64(unet) * inet16_4
	if f.u("PNET_Sign_P") == 0 {
		pnetCalc = -pnetCalc
	}
	pnet := int32(int16(uint16(f.u("PNET_8")))) * 10

	freq := func(name string) float64 {
		if v := f.u(name); v != 0 {
			return 6250 / float64(v)
		}
		return 0
	}
	tfLimit := 0.0
	if v := f.u("TFNET_Limit"); v != 0 {
		tfLimit = 2500 / float64(v)
	}

	verpo := f.u("VerPO")
	fwHi := verpo & 0x1f
	fwLo := verpo >> 5

	serial := f.u("SerialNum0") | f.u("SerialNum1")<<8

	ph1 := phCurrent(f.u("I_Akb_MAP_faz1L"), f.u("I_Akb_MAP_faz1H"))
	ph2 := phCurrent(f.u("I_Akb_MAP_faz2L"), f.u("I_Akb_MAP_faz2H"))
	ph3 := phCurrent(f.u("I_Akb_MAP_faz3L"), f.u("I_Akb_MAP_faz3H"))
	i3ph := ph1 + ph2 + ph3

	mode := mapmode.Mode(func(n string) uint32 {
		if n == "UNET" {
			if r := f.u("UNET"); r != 0 {
				return r + 100 // как в mapd: признак сети = UNET+100
			}
			return 0
		}
		return f.u(n)
	})

	// Ток АКБ и мощность нагрузки — строго как в mapd (all.asm, парсер и блок
	// знаков 0x14060..0x140b0):
	//   _Iacc           = byte[0x408] (IAcc_med_A_2) * 2          — беззнаковый
	//   _IAcc_med_A_u16 = (byte[0x433]*256 + byte[0x432]) / 16
	//   _PLoad          = byte[0x409] (PLoad_L) * 100
	//   _PLoad_calc     = Uacc * _IAcc_med_A_u16
	// Знак: вне режимов заряда (MODE не 4..9) _IAcc_med_A_u16 и _PLoad инвертируются.
	iaccU16 := (float64(f.u("IAcc_med_A_u16_L")) + float64(f.u("IAcc_med_A_u16_H"))*256) / 16
	pload := int32(f.u("PLoad_L")) * 100
	if !(mode >= 4 && mode <= 9) { // заряд — режимы 4..9
		iaccU16 = -iaccU16
	}
	iaccInt := int32(f.u("IAcc_med_A_2")) * 2

	// _I2C_Err — код I2C-ошибок из битов 2..4 регистра RSErrDop (0x447):
	// бит2 — ошибка I2C для BMS, бит3 — для MPPT, бит4 — устаревшая версия ПО MPPT.
	i2c := (f.u("RSErrDop") >> 2) & 0x7

	relay := f.u("Status_RELEdop")
	relay1 := relay & 1
	relay2 := (relay >> 1) & 1

	e := a.updateE(
		f.u24("M_POWhourNET_L", "M_POWhourNET_H", "M_POWhourNET_HH"),
		f.u24("M_POWhourMAP_L", "M_POWhourMAP_H", "M_POWhourMAP_HH"),
		f.u24("M_POWhourMAPCharge_L", "M_POWhourMAPCharge_H", "M_POWhourMAPCharge_HH"),
		int64(int32(f.u("M_POWhourNET_sign"))),
	)

	t := time.Unix(f.meta.LastSeen, 0)

	// КЭС (маленькая коллекция) из ячеек МАП; средний ток для «солнышка».
	kes := mpptList(f)
	bms := bmsList(f)
	impptAvg := 0.0
	if len(kes) > 0 {
		var sum float64
		for _, k := range kes {
			sum += k.I
		}
		impptAvg = sum / float64(len(kes))
	}

	s := fmt.Sprintf(
		`{"timestamp":"%d","time":"%02d:%02d:%02d","_UID":"%d","_MODE":"%d","_Status_Char":"%d","_Uacc":"%.1f","_Uch_T":"%.1f","_Ubuf_T":"%.1f","_Iacc":"%d","_PLoad":"%d","_PLoad_calc":"%.1f","_F_Acc_Over":"%d","_F_Net_Over":"%d","_UNET":"%d","_INET":"%d","_PNET":"%d","_PNET_calc":"%.1f","_TFNET":"%.1f","_ThFMAP":"%.1f","_UOUTmed":"%d","_TFNET_Limit":"%.1f","_UNET_Limit":"%d","_RSErrSis":"%d","_RSErrJobM":"%d","_RSErrJob":"%d","_RSWarning":"%d","_Temp_Grad0":"%d","_Temp_Grad2":"%d","_INET_16_4":"%.1f","_IAcc_med_A_u16":"%.1f","Temp_off":"%d","_E_NET":"%d","_E_ACC":"%d","_E_ACC_CHARGE":"%d","_E_NET_SIGN":"%d","_Inet_flag":"%d","_I_acc_avg":"%.1f","_I_mppt_avg":"%.1f","_I2C_Err":"%d","_Temp_Grad1":"%d","_Relay1":"%d","_Relay2":"%d","_Flag_ECO":"%d","_RSErrDop":"%d","_flagUnet2":"%d","_CoolerSpeed":"%d","_MPPTs_mode":"%d","_I_acc_3ph":"%.1f","_I_ph1":"%.1f","_I_ph2":"%.1f","_I_ph3":"%.1f","fw":"%d.%d","_P_mppt_avg":"%d","_P_acc_3ph":"%d","_P_ph1":"%d","_P_ph2":"%d","_P_ph3":"%d","_E_NET_B":"%d","_E_ACC_B":"%d","_E_ACC_CHARGE_B":"%d","_E_NET_SIGN_B":"%d","_I_MPPT_WIND":"%.1f","_P_MPPT_WIND":"%.0f","_fl_UAccChBUF_24h":"%d"}`,
		f.meta.LastSeen, t.Hour(), t.Minute(), t.Second(),
		serial, mode, f.u("StatusCh"),
		uacc, uch, ubuf,
		iaccInt, pload, uacc*iaccU16,
		f.u("F_AccOver"), f.u("F_NETOver"),
		unet, inet, pnet, pnetCalc,
		freq("TFNET"), freq("ThFMAP"), uout, tfLimit, unetLimit,
		f.u("RSErrSis"), f.u("RSErrJobM"), f.u("RSErrJob"), f.u("RSWarning"),
		int32(f.u("Temp_Grad0"))-50, int32(f.u("Temp_Grad2"))-50,
		inet16_4, iaccU16, f.u("Temp_off"),
		e.net, e.acc, e.chg, e.signNet, 0,
		0.0, impptAvg, i2c,
		int32(f.u("Temp_Grad1"))-50, relay1, relay2,
		f.u("flag_NETON_ECO"), f.u("RSErrDop"), f.u("flagUnet2"), f.u("CulerSpead"), 0,
		i3ph, ph1, ph2, ph3, fwHi, fwLo,
		int32(math.Round(uacc*impptAvg)), int32(uacc*i3ph),
		int32(uacc*ph1), int32(uacc*ph2), int32(uacc*ph3),
		uint64(e.baseNet)+e.net, uint64(e.baseAcc)+e.acc, uint64(e.baseChg)+e.chg,
		// _E_NET_SIGN_B: без накопителя (знаковый счётчик меняет знак, см. updateE).
		e.signNet,
		0.0, 0.0,
		f.u("fl_UAccChBUF_24h_45A"),
	)

	// После основного объекта — доп. коллекции ровно как ждёт оригинал (home.js):
	// BMSFunk==1 → inter[1]=BMS; ==2 → inter[1]=КЭС; ==3 → inter[1]=BMS, inter[2]=КЭС.
	out := s
	bmsf := f.u("LCD_BMSFunk")
	if bmsf == 1 || bmsf == 3 {
		if len(bms) > 0 {
			var b strings.Builder
			b.WriteByte('[')
			for i, k := range bms {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, `{"V":"%.2f","t":"%d","I":"%.2f"}`, k.V, k.T, k.I)
			}
			b.WriteByte(']')
			out += "\n" + b.String()
		} else {
			out += "\n[]"
		}
	}
	if (bmsf == 2 || bmsf == 3) && len(kes) > 0 {
		var b strings.Builder
		b.WriteByte('[')
		for i, k := range kes {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"MID":"%d","I":"%.2f","T":"%d"}`, i+1, k.I, k.T)
		}
		b.WriteByte(']')
		out += "\n" + b.String()
	}
	return []byte(out + "\n")
}
