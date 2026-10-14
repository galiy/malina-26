package events

import (
	"fmt"
	"strings"

	"github.com/galiy/malina-26/web/internal/measure"
)

// ParamSpec — курированный параметр события (МАП) с локализованной подписью.
type ParamSpec struct {
	Key   string // ключ в конфиге
	Label string // i18n-ключ подписи
}

// ParamsMap — курированный список параметров МАП (см. docs/events.md §4).
var ParamsMap = []ParamSpec{
	{"unet", "events.f_unet"},
	{"inet", "events.f_inet"},
	{"pnet", "events.f_pnet"},
	{"uacc", "events.f_uacc"},
	{"iacc", "events.f_iacc"},
	{"pacc", "events.f_pacc"},
	{"imppt", "events.f_imppt"},
	{"pmppt", "events.f_pmppt"},
	{"tmap", "events.f_tmap"},
	{"tbat", "events.f_tbat"},
	{"t1", "events.f_t1"},
}

// ParamsKes — курированный список параметров КЭС (ключи с префиксом «k_»).
var ParamsKes = []ParamSpec{
	{"k_vcpv", "events.k_vcpv"},
	{"k_icpv", "events.k_icpv"},
	{"k_vbat", "events.k_vbat"},
	{"k_ich", "events.k_ich"},
	{"k_iout", "events.k_iout"},
	{"k_ppv", "events.k_ppv"},
	{"k_pout", "events.k_pout"},
	{"k_pload", "events.k_pload"},
	{"k_pcurr", "events.k_pcurr"},
	{"k_tint", "events.k_tint"},
	{"k_tbat", "events.k_tbat"},
	{"k_windspeed", "events.k_windspeed"},
	{"k_mode", "events.k_mode"},
}

// ParamsFor — курированный список параметров по типу источника.
func ParamsFor(kind string) []ParamSpec {
	if kind == "kes" {
		return ParamsKes
	}
	return ParamsMap
}

// IsParam — ключ входит в курированный список своего типа источника.
func IsParam(kind, key string) bool {
	for _, p := range ParamsFor(kind) {
		if p.Key == key {
			return true
		}
	}
	return false
}

// ResolveParam — значение параметра из сырых полей снимка (МАП или КЭС).
// Ключи КЭС имеют префикс «k_».
func ResolveParam(key string, f map[string]uint32) (float64, bool) {
	if strings.HasPrefix(key, "k_") {
		return resolveKes(key, f)
	}
	u := func(n string) uint32 { return f[n] }
	switch key {
	case "unet":
		return measure.Volt220(u("UNET")), true
	case "inet":
		return measure.Inet(u("INET"), u("INET_16_4")), true
	case "pnet":
		return measure.Power100(u("PNET_L"), u("PNET_H")), true
	case "uacc":
		return measure.BattVolt(u("UAcc_med_VH"), u("UAcc_med_VL")), true
	case "iacc":
		return measure.Iacc(u("IAcc_med_A_u16_L")+u("IAcc_med_A_u16_H")*256, u("MODE_RAM")), true
	case "pacc":
		return measure.Power100(u("PLoad_L"), u("PLoad_H")), true
	case "imppt":
		// Суммарный ток MPPT — сумма по каналам (не среднее). Нет ни одного
		// канала → нет данных (ok=false), чтобы отличать «нет MPPT» от «0 А».
		var sum float64
		cnt := 0
		for i := 0; i < 16; i++ {
			l, ok1 := f[fmt.Sprintf("I_Akb_MPPT_L%d", i)]
			h, ok2 := f[fmt.Sprintf("I_Akb_MPPT_H%d", i)]
			if !ok1 || !ok2 {
				continue
			}
			sum += measure.MpptCurrent(l, h)
			cnt++
		}
		if cnt == 0 {
			return 0, false
		}
		return sum, true
	case "pmppt":
		ia, _ := ResolveParam("imppt", f)
		ua, _ := ResolveParam("uacc", f)
		return ua * ia, true
	case "tmap":
		return measure.Temp(u("Temp_Grad2")), true
	case "tbat":
		return measure.Temp(u("Temp_Grad0")), true
	case "t1":
		return measure.Temp(u("Temp_Grad1")), true
	}
	return 0, false
}

// resolveKes — параметры КЭС из раcпакованных полей снимка (docs/events.md §4).
func resolveKes(key string, f map[string]uint32) (float64, bool) {
	u := func(n string) uint32 { return f[n] }
	switch key {
	case "k_vcpv":
		return measure.Centi(u("VcPV")), true
	case "k_icpv":
		return measure.Centi(u("IcPV")), true
	case "k_vbat":
		return measure.Centi(u("VBat")), true
	case "k_ich":
		return measure.Centi(u("ICh")), true
	case "k_iout":
		return measure.Centi(u("IOut")), true
	case "k_ppv":
		return float64(u("PPV")), true
	case "k_pout":
		return float64(u("POut")), true
	case "k_pload":
		return float64(u("PLoad")), true
	case "k_pcurr":
		return float64(u("PCurr")), true
	case "k_tint":
		return measure.Temp(u("TempInt")), true
	case "k_tbat":
		return measure.Temp(u("TempBat")), true
	case "k_windspeed":
		return float64(u("Windspeed")), true
	case "k_mode":
		return float64(u("Mode")), true
	}
	return 0, false
}
