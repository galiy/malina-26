package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeAlarmsDeye(t *testing.T) {
	if got := decodeAlarms(0, 0, 1); len(got) != 1 || got[0].NameKey != "grid.alm.deye.w1" || got[0].Code != "W1" {
		t.Fatalf("W1: %+v", got)
	}
	if got := decodeAlarms(0, 2, 1); len(got) != 1 || got[0].NameKey != "grid.alm.deye.f1" || got[0].Code != "F1" {
		t.Fatalf("F1: %+v", got)
	}
	if got := decodeAlarms(0, 5, 1); len(got) != 1 || got[0].Code != "F49" {
		t.Fatalf("F49: %+v", got)
	}
}

func TestDecodeAlarmsSofar(t *testing.T) {
	if got := decodeAlarms(1, 0, 2); len(got) != 1 || got[0].NameKey != "grid.alm.sofar.grid_under_voltage" {
		t.Fatalf("sofar fault: %+v", got)
	}
	if got := decodeAlarms(1, 5, 0x21); len(got) != 1 || got[0].NameKey != "grid.alm.sofar.alert" {
		t.Fatalf("sofar alert: %+v", got)
	}
}

func TestDecodeAlarmsID11Unknown(t *testing.T) {
	if got := decodeAlarms(1, 0, 1024); len(got) != 1 || got[0].NameKey != "grid.alm.unknown" {
		t.Fatalf("ID11: %+v", got)
	}
}

func TestGridAlarmI18nParity(t *testing.T) {
	keys := []string{"grid.alm.deye.f1", "grid.alm.deye.w1", "grid.alm.sofar.grid_over_voltage", "grid.alm.sofar.alert", "grid.alm.unknown"}
	for _, lang := range []string{"ru", "en", "be", "zh", "ka", "kk", "uz", "tg"} {
		b, err := os.ReadFile(filepath.Join("../../assets/i18n", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			if m[k] == "" {
				t.Fatalf("%s: нет %s", lang, k)
			}
		}
	}
}
