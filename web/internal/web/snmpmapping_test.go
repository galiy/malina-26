package web

import (
	"testing"

	"github.com/galiy/malina-26/web/internal/snmp"
)

func TestBuildMapSnmp(t *testing.T) {
	f := map[string]uint32{
		"UNET":        220,
		"INET_16_4":   160, // 10 А
		"TFNET":       160, // 6250/160 = 39.06 → 390
		"ThFMAP":      160,
		"UOUTmed":     220,
		"UAcc_med_VH": 1, "UAcc_med_VL": 200, // 456/10 = 45.6 → ×0.1 → 456
		"IAcc_med_A_u16_L": 0, "IAcc_med_A_u16_H": 0,
		"Status_RELEdop": 1, "CulerSpead": 2,
		"Temp_Grad0": 80, "Temp_Grad2": 90,
		"VerPO": 26, "POW": 1, "UACC": 1,
		"SoC":            5000,
		"M_POWhourNET_L": 100, "M_POWhourNET_H": 0, "M_POWhourNET_HH": 0,
		"StatusCh": 0, "MODE_RAM": 0,
	}
	m := buildMapSnmp(f)

	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.1.1"); v != 320 { // inputVoltage 220+100
		t.Fatalf("inputVoltage=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.1.2"); v != 100 { // inputCurrent 160*10/16
		t.Fatalf("inputCurrent=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.1.4"); v != 390 { // inputFrequency
		t.Fatalf("inputFrequency=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.2.1"); v != 390 { // outputFrequency
		t.Fatalf("outputFrequency=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.2.2"); v != 320 { // outputVoltage
		t.Fatalf("outputVoltage=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.3.1"); v != 45 { // batteryVoltage (456/10)
		t.Fatalf("batteryVoltage=%d", v)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.1.2"); s != "" {
		_ = s
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.4.1"); v != 1 { // countersENET 100/100
		t.Fatalf("countersENET=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.5.1"); v != 0 { // statusMODE (MODE_RAM=0)
		t.Fatalf("statusMODE=%d", v)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.5.3"); s != "on" { // relay1
		t.Fatalf("relay1=%q", s)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.5.4"); s != "off" { // relay2
		t.Fatalf("relay2=%q", s)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.5.5"); s != "hi" { // cooler
		t.Fatalf("cooler=%q", s)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.5.6"); v != 30 { // statusTACC 80-50
		t.Fatalf("statusTACC=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.5.7"); v != 40 { // statusTMAC 90-50
		t.Fatalf("statusTMAC=%d", v)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.6.1"); s != "26.0" { // fwRev
		t.Fatalf("fwRev=%q", s)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.1.6.3"); s != "3" { // macPower POW=1
		t.Fatalf("macPower=%q", s)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.6.4"); v != 24 { // dcVoltage UACC=1
		t.Fatalf("dcVoltage=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.1.7.1"); v != 50 { // SoC 5000/100
		t.Fatalf("batteryMonitorSoC=%d", v)
	}
}

func TestBuildKesSnmp(t *testing.T) {
	f := &kesFields{
		VcPV: 1234, IcPV: 567, VBat: 1250, PPV: 700,
		IExts0: 100, IExts1: 200, TempInt: 80, TempBat: 70,
		PwrKW: 3, PwrW: 500, Mode: 'i', Sign: '+', MPP: 'R',
		FW: 42, RelayC: 0x05, Windspeed: 65535,
	}
	m := buildKesSnmp(f, 1)
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.1"); v != 12 { // PVVoltage 1234/100
		t.Fatalf("mpptPVVoltage=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.2"); v != 700 { // PVPower
		t.Fatalf("mpptPVPower=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.4"); v != 12 { // BatteryVoltage 1250/100 → 12.5→12
		t.Fatalf("mpptBatteryVoltage=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.7"); v != 350 { // Energy (3*1000+500)/10
		t.Fatalf("mpptEnergy=%d", v)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.8"); v != 30 { // InternalTemperature 80-50
		t.Fatalf("mpptInternalTemperature=%d", v)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.2.1.10"); s != "i" { // Mode
		t.Fatalf("mpptMode=%q", s)
	}
	if v := intVal(t, m, "1.3.6.1.4.1.50330.2.1.17"); v != 65535 { // Windspeed н/д
		t.Fatalf("mpptWindspeed=%d", v)
	}
	if s := strVal(t, m, "1.3.6.1.4.1.50330.2.1.13"); s != "10.1" { // Fw: 42&0xF=10, 42>>5=1
		t.Fatalf("mpptFw=%q", s)
	}
}

func intVal(t *testing.T, m map[string]snmp.Value, oid string) int64 {
	t.Helper()
	v, ok := m[oid]
	if !ok {
		t.Fatalf("нет OID %s", oid)
	}
	if v.Kind != snmp.VInt {
		t.Fatalf("%s: ожидали int, получили %+v", oid, v)
	}
	return v.Int
}

func strVal(t *testing.T, m map[string]snmp.Value, oid string) string {
	t.Helper()
	v, ok := m[oid]
	if !ok {
		t.Fatalf("нет OID %s", oid)
	}
	return string(v.Str)
}
func TestOIDOrdering(t *testing.T) {
	m := map[string]snmp.Value{
		oidKey(macChild(1, 2)): snmp.IntValue(2),
		oidKey(macChild(1, 1)): snmp.IntValue(1),
		oidKey(macChild(2, 1)): snmp.IntValue(3),
		oidKey(macChild(7, 1)): snmp.IntValue(4),
	}
	keys := sortedOIDs(m)
	want := []string{oidKey(macChild(1, 1)), oidKey(macChild(1, 2)), oidKey(macChild(2, 1)), oidKey(macChild(7, 1))}
	for i, k := range keys {
		if k != want[i] {
			t.Fatalf("порядок %d: %s, ожидали %s", i, k, want[i])
		}
	}
	if !oidLess(parseOID(want[0]), parseOID(want[1])) {
		t.Fatal("oidLess между соседними")
	}
}
