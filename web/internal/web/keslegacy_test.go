package web

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// buildKesPayloadTest собирает payload слота КЭС (KESE+KESR+KESS) вручную —
// валидный ввод для parseKesPayload, чтобы покрыть распаковку/VA/JSON без shm.
func buildKesPayloadTest(f *kesFields) []byte {
	kese := make([]byte, kesMemberKeseSize)
	copy(kese[0:4], "KESE")
	kese[4] = 1
	binary.LittleEndian.PutUint16(kese[kesKeseSerialOff:kesKeseSerialOff+2], f.Serial)
	kese[kesKeseDeviceOff] = f.Device
	kese[kesKesePowOff] = f.Pow
	kese[kesKeseFWOff] = f.FW
	kese[kesKeseModelPowOff] = f.ModelPow
	kese[kesKeseModelVoltOff] = f.ModelVolt
	kese[kesKesePicOff] = f.Pic
	kese[kesKeseNetOff] = f.Net

	kesr := make([]byte, kesMemberKesrSize)
	copy(kesr[0:4], "KESR")
	kesr[4] = 1
	binary.LittleEndian.PutUint64(kesr[kesKesrLastSeenOff:kesKesrLastSeenOff+8], uint64(f.LastSeen))
	kesr[kesKesrRelayCOff] = f.RelayC
	kesr[kesKesrRSErrSisOff] = f.RSErrSis
	kesr[kesKesrI2CErrOff] = f.I2CErr
	binary.LittleEndian.PutUint16(kesr[kesKesrVcPVOff:kesKesrVcPVOff+2], f.VcPV)
	binary.LittleEndian.PutUint16(kesr[kesKesrIcPVOff:kesKesrIcPVOff+2], f.IcPV)
	binary.LittleEndian.PutUint16(kesr[kesKesrVBatOff:kesKesrVBatOff+2], f.VBat)
	binary.LittleEndian.PutUint16(kesr[kesKesrPPVOff:kesKesrPPVOff+2], f.PPV)
	binary.LittleEndian.PutUint16(kesr[kesKesrPOutOff:kesKesrPOutOff+2], f.POut)
	binary.LittleEndian.PutUint16(kesr[kesKesrPLoadOff:kesKesrPLoadOff+2], f.PLoad)
	binary.LittleEndian.PutUint16(kesr[kesKesrPCurrOff:kesKesrPCurrOff+2], f.PCurr)
	binary.LittleEndian.PutUint16(kesr[kesKesrIChOff:kesKesrIChOff+2], f.ICh)
	binary.LittleEndian.PutUint16(kesr[kesKesrIOutOff:kesKesrIOutOff+2], f.IOut)
	kesr[kesKesrTempIntOff] = f.TempInt
	kesr[kesKesrTempBatOff] = f.TempBat
	binary.LittleEndian.PutUint16(kesr[kesKesrPwrWOff:kesKesrPwrWOff+2], f.PwrW)
	binary.LittleEndian.PutUint16(kesr[kesKesrPwrKWOff:kesKesrPwrKWOff+2], f.PwrKW)
	binary.LittleEndian.PutUint16(kesr[kesKesrIExts0Off:kesKesrIExts0Off+2], f.IExts0)
	binary.LittleEndian.PutUint16(kesr[kesKesrIExts1Off:kesKesrIExts1Off+2], f.IExts1)
	binary.LittleEndian.PutUint16(kesr[kesKesrPExts0Off:kesKesrPExts0Off+2], f.PExts0)
	binary.LittleEndian.PutUint16(kesr[kesKesrPExts1Off:kesKesrPExts1Off+2], f.PExts1)
	kesr[kesKesrModeOff] = f.Mode
	kesr[kesKesrSignOff] = f.Sign
	kesr[kesKesrMppOff] = f.MPP
	binary.LittleEndian.PutUint16(kesr[kesKesrWindOff:kesKesrWindOff+2], f.Windspeed)
	off := kesKesrBmsOff
	for i := 0; i < 32; i++ {
		binary.LittleEndian.PutUint16(kesr[off+2*i:off+2*i+2], f.UakbCell[i])
	}
	off += 64
	for i := 0; i < 32; i++ {
		kesr[off+i] = f.TakbCell[i]
	}
	off += 32
	for i := 0; i < 32; i++ {
		binary.LittleEndian.PutUint16(kesr[off+2*i:off+2*i+2], f.QCell[i])
	}

	kess := make([]byte, kesMemberKessSize)
	copy(kess[0:4], "KESS")
	kess[4] = 1
	settings := make([]uint16, 40)
	for i := 0; i < 40; i++ {
		binary.LittleEndian.PutUint16(kess[kesKessSettingsOff+2*i:kesKessSettingsOff+2*i+2], settings[i])
	}

	out := append(kese, kesr...)
	return append(out, kess...)
}

func testKesFields() *kesFields {
	f := &kesFields{
		Serial:    2670,
		Device:    5,
		FW:        0xA8, // 8.5
		RelayC:    0x05, // R1 и R3
		VcPV:      12580,
		IcPV:      1370,
		VBat:      5000,
		PPV:       1726,
		POut:      1692,
		PLoad:     0,
		PCurr:     1726,
		ICh:       3450,
		IOut:      3450,
		TempInt:   78,
		TempBat:   65,
		PwrW:      395,
		PwrKW:     26,
		Mode:      'b',
		Sign:      '-',
		MPP:       'L',
		Windspeed: 0xFFFF,
		I2CErr:    0,
		RSErrSis:  0,
		LastSeen:  1791801441,
	}
	return f
}

func TestParseKesPayload(t *testing.T) {
	f := testKesFields()
	p := buildKesPayloadTest(f)
	got, ok := parseKesPayload(p)
	if !ok {
		t.Fatal("parseKesPayload: отказ на валидном payload")
	}
	if got.Serial != 2670 || got.Device != 5 || got.FW != 0xA8 {
		t.Fatalf("KESE: serial=%d device=%d fw=%d", got.Serial, got.Device, got.FW)
	}
	if got.RelayC != 0x05 || got.VcPV != 12580 || got.VBat != 5000 {
		t.Fatalf("KESR: relay=%d vcpv=%d vbat=%d", got.RelayC, got.VcPV, got.VBat)
	}
	if got.TempInt != 78 || got.TempBat != 65 {
		t.Fatalf("temp: int=%d bat=%d", got.TempInt, got.TempBat)
	}
	if got.Mode != 'b' || got.Sign != '-' || got.MPP != 'L' || got.Windspeed != 0xFFFF {
		t.Fatalf("kesr Mode/Sign/MPP/windspeed: %+v", got)
	}
	if got.LastSeen != 1791801441 {
		t.Fatalf("lastseen=%d", got.LastSeen)
	}
	if len(got.Settings) != 40 {
		t.Fatalf("settings len=%d", len(got.Settings))
	}
}

func TestParseKesPayloadBadMagic(t *testing.T) {
	p := buildKesPayloadTest(testKesFields())
	p[0] = 'X' // сломать магию KESE
	if _, ok := parseKesPayload(p); ok {
		t.Fatal("ожидался отказ при битой магии KESE")
	}
}

func TestBuildKesVA(t *testing.T) {
	f := testKesFields()
	va := buildKesVA(f)
	if len(va) != kesVirtualSize {
		t.Fatalf("VA size=%d, ожидалось %d", len(va), kesVirtualSize)
	}
	// параметры устройства — адрес как есть
	if va[0x0000] != 5 {
		t.Fatalf("VA[0]=%d, ожидалось 5", va[0x0000])
	}
	if va[0x0001] != 0 { // Pow не задан в тесте
		t.Fatalf("VA[1]=%d", va[0x0001])
	}
	// серийник 0x2C9 LE
	if binary.LittleEndian.Uint16(va[0x2C9:0x2CB]) != 2670 {
		t.Fatalf("VA serial=%d", binary.LittleEndian.Uint16(va[0x2C9:0x2CB]))
	}
	// телеметрия Vc_PV 0x2D9
	if binary.LittleEndian.Uint16(va[0x2D9:0x2DB]) != 12580 {
		t.Fatalf("VA VcPV=%d", binary.LittleEndian.Uint16(va[0x2D9:0x2DB]))
	}
	// Relay_C 0x3E3
	if va[0x3E3] != 0x05 {
		t.Fatalf("VA relay=%d", va[0x3E3])
	}
	// Mode 0x2C2 / MPP 0x379 / Sign 0x37A / windspeed 0x3F8
	if va[0x2C2] != 'b' || va[0x379] != 'L' || va[0x37A] != '-' ||
		binary.LittleEndian.Uint16(va[0x3F8:0x3FA]) != 0xFFFF {
		t.Fatalf("VA Mode/MPP/Sign/windspeed: %x/%x/%x/%x",
			va[0x2C2], va[0x379], va[0x37A], binary.LittleEndian.Uint16(va[0x3F8:0x3FA]))
	}
	// непокрытый адрес — 0
	if va[0x600] != 0 {
		t.Fatalf("VA[0x600]=%d, ожидалось 0 (нулевая заливка)", va[0x600])
	}
}

func TestRenderKesJSON(t *testing.T) {
	f := testKesFields()
	var m map[string]interface{}
	if err := json.Unmarshal(renderKesJSON(f), &m); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if m["UID"] != "2670" {
		t.Fatalf("UID=%q", m["UID"])
	}
	if m["Vc_PV"] != "125.8" {
		t.Fatalf("Vc_PV=%q", m["Vc_PV"])
	}
	if m["V_Bat"] != "50.0" {
		t.Fatalf("V_Bat=%q", m["V_Bat"])
	}
	if m["FW"] != "8.5" {
		t.Fatalf("FW=%q", m["FW"])
	}
	if m["Temp_Int"] != "28" {
		t.Fatalf("Temp_Int=%q", m["Temp_Int"])
	}
	if m["Temp_Bat"] != "15" {
		t.Fatalf("Temp_Bat=%q", m["Temp_Bat"])
	}
	if m["Pwr_kW"] != "26.395" {
		t.Fatalf("Pwr_kW=%v (ожидалась строка 26.395)", m["Pwr_kW"])
	}
	if m["P_Out"] != "1692" {
		t.Fatalf("P_Out=%v (ожидалась строка 1692)", m["P_Out"])
	}
	if m["I_Ch"] != "34.5" {
		t.Fatalf("I_Ch=%v (ожидалась строка 34.5)", m["I_Ch"])
	}
	if m["R1"] != "1" || m["R2"] != "0" || m["R3"] != "1" {
		t.Fatalf("rele R1=%v R2=%v R3=%v", m["R1"], m["R2"], m["R3"])
	}
	if m["Mode"] != "b" || m["Sign"] != "-" || m["MPP"] != "L" || m["windspeed"] != "65535" {
		t.Fatalf("Mode/Sign/MPP/windspeed: %v", m)
	}
}

func TestBuildKesCellOps(t *testing.T) {
	ops, ok := buildKesCellOps(`[[438,77],[3645,1]]`)
	if !ok || len(ops) != 2 {
		t.Fatalf("ops=%v ok=%v", ops, ok)
	}
	if ops[0]["addr"] != "1B6" || ops[0]["data"] != "4D" {
		t.Fatalf("op0=%v", ops[0])
	}
	if ops[1]["addr"] != "E3D" || ops[1]["data"] != "01" {
		t.Fatalf("op1=%v", ops[1])
	}
	if _, ok := buildKesCellOps(`[[-1,5]]`); ok {
		t.Fatal("ожидался отказ при отрицательном адресе")
	}
	if _, ok := buildKesCellOps(`[[300,999]]`); ok {
		t.Fatal("ожидался отказ при значении вне 0..255")
	}
}
