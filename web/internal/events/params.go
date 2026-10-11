package events

import "fmt"

// ParamSpec — курированный параметр события (МАП) с локализованной подписью.
type ParamSpec struct {
	Key   string // ключ в конфиге
	Label string // i18n-ключ подписи
}

// Params — курированный список параметров (см. docs/events.md §4).
var Params = []ParamSpec{
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

// IsParam — ключ входит в курированный список.
func IsParam(key string) bool {
	for _, p := range Params {
		if p.Key == key {
			return true
		}
	}
	return false
}

// ResolveParam — значение параметра из сырых полей снимка МАП.
func ResolveParam(key string, f map[string]uint32) (float64, bool) {
	u := func(n string) uint32 { return f[n] }
	switch key {
	case "unet":
		v := u("UNET")
		if v == 0 {
			return 0, true // нет сети
		}
		return float64(v) + 100, true
	case "inet":
		return float64(u("INET_16_4")) / 16, true
	case "pnet":
		return float64(u("PNET_8")) * 10, true
	case "uacc":
		return float64(u("UAcc_med_VH")*256+u("UAcc_med_VL")) / 10, true
	case "iacc":
		return float64(u("IAcc_med_A_u16_L")+u("IAcc_med_A_u16_H")*256) / 16, true
	case "pacc":
		return float64(u("PLoad_H")*256+u("PLoad_L")) * 100, true
	case "imppt":
		var sum float64
		cnt := 0
		for i := 0; i < 16; i++ {
			l, ok1 := f[fmt.Sprintf("I_Akb_MPPT_L%d", i)]
			h, ok2 := f[fmt.Sprintf("I_Akb_MPPT_H%d", i)]
			if !ok1 || !ok2 {
				continue
			}
			sum += float64(l+(h&0x7F)*256) / 16
			cnt++
		}
		if cnt == 0 {
			return 0, true
		}
		return sum / float64(cnt), true
	case "pmppt":
		ia, _ := ResolveParam("imppt", f)
		ua, _ := ResolveParam("uacc", f)
		return ua * ia, true
	case "tmap":
		return float64(u("Temp_Grad2")) - 50, true
	case "tbat":
		return float64(u("Temp_Grad0")) - 50, true
	case "t1":
		return float64(u("Temp_Grad1")) - 50, true
	}
	return 0, false
}
