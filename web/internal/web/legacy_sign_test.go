package web

import (
	"encoding/json"
	"testing"
)

// TestRenderLegacyGridCurrentSigned — _INET_16_4 отдаётся со знаком по
// PNET_Sign_P, _PNET_calc при этом не получает двойного знака; знак АКБ
// (IAcc_med) остаётся legacy-конвенцией «+ заряд / − разряд».
func TestRenderLegacyGridCurrentSigned(t *testing.T) {
	cases := []struct {
		name  string
		sign  uint32
		wantI string
		wantP string
	}{
		{"потребление", 1, "20.0", "4600.0"},
		{"отдача", 0, "-20.0", "-4600.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := testFields(t, map[string]uint32{
				"UNET":        130, // 230 В
				"INET":        20,
				"INET_16_4":   80, // 20 А
				"PNET_Sign_P": c.sign,
				"MODE_RAM":    4, // заряд: IAcc_med не инвертируется
			})
			out := renderLegacy(f, &legacyAcc{})
			var m map[string]string
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("json: %v; %s", err, out)
			}
			if m["_INET_16_4"] != c.wantI {
				t.Fatalf("_INET_16_4=%q, ожидалось %q", m["_INET_16_4"], c.wantI)
			}
			if m["_PNET_calc"] != c.wantP {
				t.Fatalf("_PNET_calc=%q, ожидалось %q", m["_PNET_calc"], c.wantP)
			}
			if m["_IAcc_med_A_u16"] != "0.0" {
				t.Fatalf("_IAcc_med_A_u16=%q, ожидалось 0.0", m["_IAcc_med_A_u16"])
			}
		})
	}
}

// TestRenderLegacyBatterySignUnchanged — _IAcc_med_A_u16 остаётся в legacy-
// конвенции «+ заряд / − разряд»: правка _INET_16_4 её не затрагивает.
func TestRenderLegacyBatterySignUnchanged(t *testing.T) {
	cases := []struct {
		name string
		mode uint32
		want string
	}{
		{"заряд", 4, "10.0"},
		{"разряд", 0, "-10.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := testFields(t, map[string]uint32{
				"IAcc_med_A_u16_L": 160,
				"MODE_RAM":         c.mode,
			})
			out := renderLegacy(f, &legacyAcc{})
			var m map[string]string
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("json: %v", err)
			}
			if m["_IAcc_med_A_u16"] != c.want {
				t.Fatalf("_IAcc_med_A_u16=%q, ожидалось %q", m["_IAcc_med_A_u16"], c.want)
			}
		})
	}
}

// TestRenderLegacyMissingSignAndPreserved — без PNET_Sign_P _INET_16_4
// остаётся магнитудой (направление неизвестно); _PNET_calc сохраняет старое
// поведение, а _INET/_IAcc_med/_PLoad_calc остаются прежними.
func TestRenderLegacyMissingSignAndPreserved(t *testing.T) {
	f := testFields(t, map[string]uint32{
		"UNET":             130,
		"INET":             20,
		"INET_16_4":        80,
		"UAcc_med_VH":      2,
		"UAcc_med_VL":      3, // 51.5 В
		"IAcc_med_A_u16_L": 160,
		"MODE_RAM":         4, // заряд
	})
	out := renderLegacy(f, &legacyAcc{})
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("json: %v", err)
	}
	checks := map[string]string{
		"_INET":           "20",
		"_INET_16_4":      "20.0",
		"_PNET_calc":      "-4600.0",
		"_IAcc_med_A_u16": "10.0",
		"_PLoad_calc":     "515.0",
	}
	for k, want := range checks {
		if m[k] != want {
			t.Fatalf("%s=%q, ожидалось %q", k, m[k], want)
		}
	}
}
