package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMaskFields(t *testing.T) {
	in := map[string]any{
		"m1_ap_pass": "secret", "m2_sta_pass": "x", "cookie": "c=1",
		"token": "abcd1234", "host": "1.2.3.4", "mode": "dhcp",
	}
	out := MaskFields(in)
	for _, k := range []string{"m1_ap_pass", "m2_sta_pass", "cookie"} {
		if out[k] != "***" {
			t.Fatalf("%s: ожидали ***, получили %v", k, out[k])
		}
	}
	// token — это префикс sha256 (не секрет), поэтому не маскируется.
	if out["token"] != "abcd1234" {
		t.Fatalf("token не должен маскироваться: %v", out["token"])
	}
	if out["host"] != "1.2.3.4" || out["mode"] != "dhcp" {
		t.Fatal("несекретные поля не должны меняться")
	}
}

func TestEventJSONAndMasking(t *testing.T) {
	var got string
	l := NewWithSink(true, func(s string) { got = s })
	l.Event("network_settings", map[string]any{
		"m1_ap_pass": "topsecret", "mode": "dhcp", "ip": "1.2.3.4",
	})
	if got == "" {
		t.Fatal("нет вывода")
	}
	if strings.Contains(got, "topsecret") {
		t.Fatal("секрет утёк в лог")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("не JSON: %v (%s)", err, got)
	}
	if m["event"] != "network_settings" {
		t.Fatalf("event=%v", m["event"])
	}
	if m["ts"] == nil || m["ts"] == "" {
		t.Fatal("нет поля ts")
	}
	if m["m1_ap_pass"] != "***" {
		t.Fatalf("пароль не замаскирован: %v", m["m1_ap_pass"])
	}
	if m["mode"] != "dhcp" {
		t.Fatalf("mode=%v", m["mode"])
	}
}

func TestDisabledNoEmit(t *testing.T) {
	called := false
	l := NewWithSink(false, func(string) { called = true })
	l.Event("login", map[string]any{"result": "ok"})
	if called {
		t.Fatal("выключенный логгер не должен писать")
	}
}
