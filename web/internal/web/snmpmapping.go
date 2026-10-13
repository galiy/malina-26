package web

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/web/internal/mapmode"
	"github.com/galiy/malina-26/web/internal/snmp"
)

// OID-базис MICROART-MIB: enterprises 50330 (docs/snmp.md §0).
var microartOID = []int{1, 3, 6, 1, 4, 1, 50330}

// Ветки MIB.
var (
	notificationsOID = append(append([]int{}, microartOID...), 0) // { microart 0 }
	macOID           = append(append([]int{}, microartOID...), 1) // { microart 1 }
	mpptOID          = append(append([]int{}, microartOID...), 2) // { microart 2 }
)

// macChild — OID ветки mac с суффиксом.
func macChild(parts ...int) []int { return append(append([]int{}, macOID...), parts...) }

// mpptChild — OID ветки mppt с суффиксом (mpptN + поле).
func mpptChild(n int, parts ...int) []int {
	o := append(append([]int{}, mpptOID...), n)
	return append(o, parts...)
}

// notifChild — полный OID нотификации notifications N.
func notifChild(n int) []int { return append(append([]int{}, notificationsOID...), n) }

// oidKey — строковое представление OID (для карты/сортировки).
func oidKey(o []int) string {
	var b strings.Builder
	for i, x := range o {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.Itoa(x))
	}
	return b.String()
}

// parseOID — разбор строкового OID в числовой путь.
func parseOID(s string) []int {
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, _ := strconv.Atoi(p)
		out = append(out, v)
	}
	return out
}

// oidLess — лексикографически корректное сравнение OID (по элементам).
func oidLess(a, b []int) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// POWER_KW — мощность МАП в кВт по коду POW (таблица legacy).
var powerKW = map[uint32]string{
	0: "2", 1: "3", 2: "4", 3: "5", 4: "6", 5: "8", 6: "10", 7: "12",
	8: "16", 9: "20", 10: "24", 11: "30", 12: "40", 13: "50", 14: "60", 15: "80",
}

// ratedDC — номинальное напряжение АКБ по коду UACC.
var ratedDC = map[uint32]int{0: 12, 1: 24, 2: 48, 3: 96}

// buildMapSnmp — снимок МАП → карта «OID → значение» (§2.6). Источник — поля
// бинарного снимка map-reader (docs/map-contract.md).
func buildMapSnmp(f map[string]uint32) map[string]snmp.Value {
	out := map[string]snmp.Value{}
	put := func(oid []int, v snmp.Value) { out[oidKey(oid)] = v }

	unet := f["UNET"]
	// inputVoltage (.1.1.1): +100 (0 → 0)
	if unet == 0 {
		put(macChild(1, 1), snmp.IntValue(0))
	} else {
		put(macChild(1, 1), snmp.IntValue(int64(unet)+100))
	}
	// inputCurrent (.1.1.2): INET_16_4/16 → ×0.1 (десятые А)
	inet := f["INET_16_4"]
	put(macChild(1, 2), snmp.IntValue(int64(inet)*10/16))
	// inputPower (.1.1.3): |U·I| по модулю (ВА) — знак теряется (см. §2.8)
	put(macChild(1, 3), snmp.IntValue(int64(unet)*int64(inet)/16))
	// inputFrequency (.1.1.4): 6250/TFNET → ×0.1 Гц
	if tf := f["TFNET"]; tf > 0 {
		put(macChild(1, 4), snmp.IntValue(62500/int64(tf)))
	}
	// outputFrequency (.1.2.1): 6250/ThFMAP → ×0.1 Гц
	if tf := f["ThFMAP"]; tf > 0 {
		put(macChild(2, 1), snmp.IntValue(62500/int64(tf)))
	}
	// outputVoltage (.1.2.2): UOUTmed +100 (0 → 0)
	if u := f["UOUTmed"]; u != 0 {
		put(macChild(2, 2), snmp.IntValue(int64(u)+100))
	} else {
		put(macChild(2, 2), snmp.IntValue(0))
	}
	// batteryVoltage (.1.3.1): (VH·256+VL)/10 → как есть (целое ×0.1)
	ub := int64(f["UAcc_med_VH"]*256 + f["UAcc_med_VL"])
	put(macChild(3, 1), snmp.IntValue(ub/10))
	// batteryCurrent (.1.3.2): (L+H·256)/16 → ×10 (десятые А)
	ib := int64(f["IAcc_med_A_u16_L"] + f["IAcc_med_A_u16_H"]*256)
	put(macChild(3, 2), snmp.IntValue(ib*10/16))
	// batteryPower (.1.3.3): U(В)·I(А) = (batteryVoltage/10)·(batteryCurrent/10)
	put(macChild(3, 3), snmp.IntValue((ub/10)*(ib/16)))
	// batteryMpptCurrent (.1.3.4): сумма I_Akb_MPPT → ×0.1 А
	var imppt int64
	for i := 0; i < 16; i++ {
		l, ok1 := f["I_Akb_MPPT_L"+strconv.Itoa(i)]
		h, ok2 := f["I_Akb_MPPT_H"+strconv.Itoa(i)]
		if ok1 && ok2 {
			imppt += int64(l + (h&0x7F)*256) // сырое 1/16 А
		}
	}
	put(macChild(3, 4), snmp.IntValue(imppt*10/16))
	// batteryMpptPower (.1.3.5): Uacc·Imppt (Вт)
	put(macChild(3, 5), snmp.IntValue((ub/10)*(imppt/16)))
	// countersENET/EACC/ECHG (.1.4.1..3): M_POWhour* /100 (×0.01 кВт·ч)
	put(macChild(4, 1), snmp.IntValue(powerHour(f, "M_POWhourNET")))
	put(macChild(4, 2), snmp.IntValue(powerHour(f, "M_POWhourMAP")))
	put(macChild(4, 3), snmp.IntValue(powerHour(f, "M_POWhourMAPCharge")))
	// statusMODE (.1.5.1) + statusModeText (.1.5.2)
	mode := mapmode.Mode(func(name string) uint32 { return f[name] })
	put(macChild(5, 1), snmp.IntValue(int64(mode)))
	put(macChild(5, 2), snmp.StringValue([]byte(mapModeLabel(mode))))
	// statusRelay1/2 (.1.5.3/.1.5.4): биты Status_RELEdop
	rele := f["Status_RELEdop"]
	put(macChild(5, 3), snmp.StringValue([]byte(relayStr(rele&1))))
	put(macChild(5, 4), snmp.StringValue([]byte(relayStr((rele>>1)&1))))
	// statusCooler (.1.5.5): CulerSpead 0→off 1→med 2→hi
	put(macChild(5, 5), snmp.StringValue([]byte(coolerStr(f["CulerSpead"]))))
	// statusTACC (.1.5.6): Temp_Grad0 −50
	put(macChild(5, 6), snmp.IntValue(int64(f["Temp_Grad0"])-50))
	// statusTMAC (.1.5.7): Temp_Grad2 −50
	put(macChild(5, 7), snmp.IntValue(int64(f["Temp_Grad2"])-50))
	// fwRev (.1.6.1): VerPO → v&0x1f + "." + (v>>5)
	if v := f["VerPO"]; v > 0 {
		put(macChild(6, 1), snmp.StringValue([]byte(fmt.Sprintf("%d.%d", v&0x1F, v>>5))))
	}
	// macPower (.1.6.3): POWER_KW[POW]
	if s, ok := powerKW[f["POW"]]; ok {
		put(macChild(6, 3), snmp.StringValue([]byte(s)))
	}
	// dcVoltage (.1.6.4): {12,24,48,96}[UACC]
	if v, ok := ratedDC[f["UACC"]]; ok {
		put(macChild(6, 4), snmp.IntValue(int64(v)))
	}
	// batteryMonitorSoC (.1.7.1): SoC/100 (0..100)
	if soc := f["SoC"]; soc > 0 {
		put(macChild(7, 1), snmp.IntValue(int64(soc/100)))
	}
	return out
}

// mapModeLabel — текст режима МАП для statusModeText (DISPLAY-строка MIB).
// Языконезависимая краткая подпись (как в legacy-MIB).
func mapModeLabel(code uint32) string {
	if s, ok := modeLabels[code]; ok {
		return s
	}
	return "Режим-" + strconv.FormatUint(uint64(code), 10)
}

var modeLabels = map[uint32]string{
	0: "Выключен, без сети", 1: "Выключен, сеть входит",
	2: "Генерация от АКБ", 3: "Трансляция сети",
	4: "Трансляция + заряд. Трансляция", 5: "Трансляция + заряд. Буфер",
	6: "Трансляция + заряд. 1 ступень", 7: "Трансляция + заряд. 2 ступень",
	8: "Трансляция + заряд. Дозаряд", 9: "Трансляция + заряд. Прочие",
	10: "Принудительная генерация", 11: "Тарифная сеть. Принудительная генерация",
	12: "Тарифная сеть. Мин. тариф", 13: "Трансляция + эко-подкачка",
	14: "Трансляция + продажа в сеть", 15: "Ожидание внешнего заряда",
	16: "Тарифная сеть. Трансляция + эко-подкачка", 17: "Тарифная сеть. Трансляция + продажа",
	18: "Подкачка Pmax",
}

// powerHour — счётчик кВт·ч (×0.01) из трёх байт *_L/_H/_HH.
func powerHour(f map[string]uint32, prefix string) int64 {
	l := f[prefix+"_L"]
	h := f[prefix+"_H"]
	hh := f[prefix+"_HH"]
	return int64(l+h*256+hh*65536) / 100
}

// relayStr — "on"/"off" по биту.
func relayStr(bit uint32) string {
	if bit != 0 {
		return "on"
	}
	return "off"
}

// coolerStr — скорость кулера по коду.
func coolerStr(v uint32) string {
	switch v {
	case 1:
		return "med"
	case 2:
		return "hi"
	default:
		return "off"
	}
}

// buildKesSnmp — снимок КЭС → карта «OID → значение» (§2.7) для экземпляра n (1..4).
func buildKesSnmp(f *kesFields, n int) map[string]snmp.Value {
	out := map[string]snmp.Value{}
	put := func(oid []int, v snmp.Value) { out[oidKey(oid)] = v }
	if n < 1 || n > 4 {
		return out
	}
	puts := func(field int, v snmp.Value) { put(mpptChild(n, field), v) }

	puts(1, snmp.IntValue(int64(f.VcPV)/100))                                    // PVVoltage
	puts(2, snmp.IntValue(int64(f.PPV)))                                         // PVPower
	puts(3, snmp.IntValue(int64(f.IcPV)/100))                                    // PVCurrent
	puts(4, snmp.IntValue(int64(f.VBat)/100))                                    // BatteryVoltage (×0.1)
	puts(5, snmp.IntValue(int64(f.IExts0)/100))                                  // CurrentSensor1
	puts(6, snmp.IntValue(int64(f.IExts1)/100))                                  // CurrentSensor2
	puts(7, snmp.IntValue((int64(f.PwrKW)*1000+int64(f.PwrW))/10))               // Energy (×0.01)
	puts(8, snmp.IntValue(int64(f.TempInt)-50))                                  // InternalTemperature
	puts(9, snmp.IntValue(int64(f.TempBat)-50))                                  // BatteryTemperature
	puts(10, snmp.StringValue([]byte(byteField(f.Mode))))                        // Mode
	puts(11, snmp.StringValue([]byte(byteField(f.Sign))))                        // Sign
	puts(12, snmp.StringValue([]byte(byteField(f.MPP))))                         // Mpp
	puts(13, snmp.StringValue([]byte(fmt.Sprintf("%d.%d", f.FW&0x0F, f.FW>>5)))) // Fw
	puts(14, snmp.StringValue([]byte(relayStr(uint32(f.RelayC)&1))))             // Relay1
	puts(15, snmp.StringValue([]byte(relayStr((uint32(f.RelayC)>>1)&1))))        // Relay2
	puts(16, snmp.StringValue([]byte(relayStr((uint32(f.RelayC)>>2)&1))))        // Relay3
	puts(17, snmp.IntValue(int64(windVal(f.Windspeed))))                         // Windspeed
	return out
}

// byteField — один символ (Mode/Sign/MPP) или пусто.
func byteField(v byte) string {
	if v == 0 {
		return ""
	}
	if v > 0x2A && v <= 0x7C {
		return string(rune(v))
	}
	return "U"
}

// snmpView — кэш объединённой карты «OID → значение» (TTL 1 c). SNMP-обход
// (GetNext по каждому OID) не должен пересобирать карту на каждый запрос.
var (
	snmpCacheMu  sync.Mutex
	snmpCacheMap map[string]snmp.Value
	snmpCacheAt  time.Time
)

func snmpView() map[string]snmp.Value {
	snmpCacheMu.Lock()
	defer snmpCacheMu.Unlock()
	if snmpCacheMap != nil && time.Since(snmpCacheAt) < time.Second {
		return snmpCacheMap
	}
	snmpCacheMap = buildSnmpView()
	snmpCacheAt = time.Now()
	return snmpCacheMap
}

// buildSnmpView собирает карту «OID → значение» (МАП «первый» + КЭС 1..4) из
// актуального снимка shm.
func buildSnmpView() map[string]snmp.Value {
	out := map[string]snmp.Value{}
	// «первый» МАП в списке (порядок слота map-list) — ограничение «1 МАП на хост».
	if m, ok := firstMap(); ok {
		f := decodeMapFields(mapRawPayload(m.ID))
		if f != nil {
			for k, v := range buildMapSnmp(f) {
				out[k] = v
			}
		}
	}
	// КЭС 1..4 по порядку kes-list.
	for i, k := range firstKes(4) {
		if p := kesRawPayload(k.ID); p != nil {
			if f, ok := parseKesPayload(p); ok {
				for kk, vv := range buildKesSnmp(f, i+1) {
					out[kk] = vv
				}
			}
		}
	}
	return out
}

// firstMap — первый валидный МАП в списке.
func firstMap() (MapInfo, bool) {
	for _, m := range loadMapList().Maps {
		if validSerial(m.Serial) {
			return m, true
		}
	}
	return MapInfo{}, false
}

// firstKes — первые n валидных КЭС в списке.
func firstKes(n int) []KesInfo {
	list := loadKesList().Kes
	out := make([]KesInfo, 0, n)
	for _, k := range list {
		if !validSerial(k.Serial) {
			continue
		}
		out = append(out, k)
		if len(out) == n {
			break
		}
	}
	return out
}

// sortedOIDs — отсортированные по возрастанию ключи карты OID.
func sortedOIDs(m map[string]snmp.Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return oidLess(parseOID(keys[i]), parseOID(keys[j]))
	})
	return keys
}

// webSnmpHandler — реализация snmp.Handler на общей карте snmpView.
type webSnmpHandler struct{}

// Get — точечное значение OID (GET).
func (webSnmpHandler) Get(oid []int) (snmp.Value, bool) {
	v, ok := snmpView()[oidKey(oid)]
	return v, ok
}

// Next — первый OID строго больше запрошенного (GETNEXT/WALK).
func (webSnmpHandler) Next(oid []int) ([]int, snmp.Value, bool) {
	m := snmpView()
	keys := sortedOIDs(m)
	next := parseOID(oidKey(oid))
	for _, k := range keys {
		if oidLess(parseOID(k), next) {
			continue
		}
		// строго больше: если равен запрошенному — пропускаем (существует только
		// при запросе GETNEXT от этого OID), берём следующий.
		if oidKey(parseOID(k)) == oidKey(next) {
			continue
		}
		return parseOID(k), m[k], true
	}
	return nil, snmp.Value{}, false
}
