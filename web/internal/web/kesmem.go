package web

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Распаковка payload слота КЭС (KESE+KESR+KESS, 1536 Б) в поля и построение
// виртуального адресного пространства КЭС (RAM-адреса) + legacy-снимка read_json.
// Контракт слота — daemons/kes-reader/kes.go; адреса/масштабы — docs/kes-memory.md §1/§2.
// Неподтверждённые поля (Mode/Sign/MPP 0x2C2/0x37A/0x379, windspeed 0x3F8) в контракт
// не публикуются — в JSON отдаются пустыми.

// Размеры/смещения под-блоков слота (синхронно с daemons/kes-reader/kes.go).
const (
	kesMemberKeseSize = 512
	kesMemberKesrSize = 512
	kesMemberKessSize = 512

	kesOffKese = 0
	kesOffKesr = kesMemberKeseSize
	kesOffKess = kesMemberKeseSize + kesMemberKesrSize
)

// Смещения под-блока "KESE".
const (
	kesKeseSerialOff    = 6 // uint16 LE
	kesKeseDeviceOff    = 8
	kesKesePowOff       = 9
	kesKeseFWOff        = 10
	kesKeseModelPowOff  = 11
	kesKeseModelVoltOff = 12
	kesKesePicOff       = 13
	kesKeseNetOff       = 14
)

// Смещения под-блока "KESR".
const (
	kesKesrLastSeenOff = 6  // uint64 LE (Unix-ts)
	kesKesrRelayCOff   = 14 // 1 байт
	kesKesrRSErrSisOff = 15
	kesKesrI2CErrOff   = 16
	kesKesrVcPVOff     = 18 // 2 байта
	kesKesrIcPVOff     = 20
	kesKesrVBatOff     = 22
	kesKesrPPVOff      = 24
	kesKesrPOutOff     = 26
	kesKesrPLoadOff    = 28
	kesKesrPCurrOff    = 30
	kesKesrIChOff      = 32
	kesKesrIOutOff     = 34
	kesKesrTempIntOff  = 36
	kesKesrTempBatOff  = 37
	kesKesrPwrWOff     = 38
	kesKesrPwrKWOff    = 40
	kesKesrIExts0Off   = 42
	kesKesrIExts1Off   = 44
	kesKesrPExts0Off   = 46
	kesKesrPExts1Off   = 48
	kesKesrBmsOff      = 52
)

// Смещения под-блока "KESS".
const (
	kesKessSettingsOff = 5 // [40]×uint16 LE
)

// kesVirtualSize — размер виртуального адресного пространства КЭС по RAM-адресам:
// максимальный используемый адрес + 1. Топ = ErrMPPT (0xE70..0xE7F) → 0xE80.
// Покрывает весь публикуемый диапазон (параметры, настройки, телеметрия, BMS/MPPT).
const kesVirtualSize = 0xE80

// kesFields — распакованные поля слота КЭС (RAM-контракт).
type kesFields struct {
	Serial    uint16
	Device    byte
	Pow       byte
	FW        byte
	ModelPow  byte
	ModelVolt byte
	Pic       byte
	Net       byte
	LastSeen  int64
	RelayC    byte
	RSErrSis  byte
	I2CErr    byte
	VcPV      uint16
	IcPV      uint16
	VBat      uint16
	PPV       uint16
	POut      uint16
	PLoad     uint16
	PCurr     uint16
	ICh       uint16
	IOut      uint16
	TempInt   byte
	TempBat   byte
	PwrW      uint16
	PwrKW     uint16
	SignC0    byte
	SignC1    byte
	IExts0    uint16
	IExts1    uint16
	PExts0    uint16
	PExts1    uint16
	UakbCell  [32]uint16
	TakbCell  [32]byte
	QCell     [32]uint16
	ErrCells  [32]byte
	IAkbMPPT  [16]uint16
	ModeMPPT  [16]byte
	TakbMPPT  [16]byte
	ErrMPPT   [16]byte
	Settings  []uint16
}

// parseKesPayload разбирает payload слота (KESE+KESR+KESS). Проверяет магию и
// версии под-блоков; при несовпадении — false.
func parseKesPayload(p []byte) (*kesFields, bool) {
	if len(p) < kesOffKess+kesMemberKessSize {
		return nil, false
	}
	kese := p[kesOffKese : kesOffKese+kesMemberKeseSize]
	kesr := p[kesOffKesr : kesOffKesr+kesMemberKesrSize]
	kess := p[kesOffKess : kesOffKess+kesMemberKessSize]
	if string(kese[0:4]) != "KESE" || string(kesr[0:4]) != "KESR" || string(kess[0:4]) != "KESS" {
		return nil, false
	}
	if kese[4] != 1 || kesr[4] != 1 || kess[4] != 1 {
		return nil, false
	}

	f := &kesFields{}
	f.Serial = binary.LittleEndian.Uint16(kese[kesKeseSerialOff : kesKeseSerialOff+2])
	f.Device = kese[kesKeseDeviceOff]
	f.Pow = kese[kesKesePowOff]
	f.FW = kese[kesKeseFWOff]
	f.ModelPow = kese[kesKeseModelPowOff]
	f.ModelVolt = kese[kesKeseModelVoltOff]
	f.Pic = kese[kesKesePicOff]
	f.Net = kese[kesKeseNetOff]

	f.LastSeen = int64(binary.LittleEndian.Uint64(kesr[kesKesrLastSeenOff : kesKesrLastSeenOff+8]))
	f.RelayC = kesr[kesKesrRelayCOff]
	f.RSErrSis = kesr[kesKesrRSErrSisOff]
	f.I2CErr = kesr[kesKesrI2CErrOff]
	f.VcPV = binary.LittleEndian.Uint16(kesr[kesKesrVcPVOff : kesKesrVcPVOff+2])
	f.IcPV = binary.LittleEndian.Uint16(kesr[kesKesrIcPVOff : kesKesrIcPVOff+2])
	f.VBat = binary.LittleEndian.Uint16(kesr[kesKesrVBatOff : kesKesrVBatOff+2])
	f.PPV = binary.LittleEndian.Uint16(kesr[kesKesrPPVOff : kesKesrPPVOff+2])
	f.POut = binary.LittleEndian.Uint16(kesr[kesKesrPOutOff : kesKesrPOutOff+2])
	f.PLoad = binary.LittleEndian.Uint16(kesr[kesKesrPLoadOff : kesKesrPLoadOff+2])
	f.PCurr = binary.LittleEndian.Uint16(kesr[kesKesrPCurrOff : kesKesrPCurrOff+2])
	f.ICh = binary.LittleEndian.Uint16(kesr[kesKesrIChOff : kesKesrIChOff+2])
	f.IOut = binary.LittleEndian.Uint16(kesr[kesKesrIOutOff : kesKesrIOutOff+2])
	f.TempInt = kesr[kesKesrTempIntOff]
	f.TempBat = kesr[kesKesrTempBatOff]
	f.PwrW = binary.LittleEndian.Uint16(kesr[kesKesrPwrWOff : kesKesrPwrWOff+2])
	f.PwrKW = binary.LittleEndian.Uint16(kesr[kesKesrPwrKWOff : kesKesrPwrKWOff+2])
	// Sign_C[0]/Sign_C[1] (0x404/0x405) в контракт KESR не публикуются — 0.
	f.SignC0 = 0
	f.SignC1 = 0
	f.IExts0 = binary.LittleEndian.Uint16(kesr[kesKesrIExts0Off : kesKesrIExts0Off+2])
	f.IExts1 = binary.LittleEndian.Uint16(kesr[kesKesrIExts1Off : kesKesrIExts1Off+2])
	f.PExts0 = binary.LittleEndian.Uint16(kesr[kesKesrPExts0Off : kesKesrPExts0Off+2])
	f.PExts1 = binary.LittleEndian.Uint16(kesr[kesKesrPExts1Off : kesKesrPExts1Off+2])

	off := kesKesrBmsOff
	for i := 0; i < 32; i++ {
		f.UakbCell[i] = binary.LittleEndian.Uint16(kesr[off+2*i : off+2*i+2])
	}
	off += 64
	for i := 0; i < 32; i++ {
		f.TakbCell[i] = kesr[off+i]
	}
	off += 32
	for i := 0; i < 32; i++ {
		f.QCell[i] = binary.LittleEndian.Uint16(kesr[off+2*i : off+2*i+2])
	}
	off += 64
	for i := 0; i < 32; i++ {
		f.ErrCells[i] = kesr[off+i]
	}
	off += 32
	for i := 0; i < 16; i++ {
		f.IAkbMPPT[i] = binary.LittleEndian.Uint16(kesr[off+2*i : off+2*i+2])
	}
	off += 32
	for i := 0; i < 16; i++ {
		f.ModeMPPT[i] = kesr[off+i]
	}
	off += 16
	for i := 0; i < 16; i++ {
		f.TakbMPPT[i] = kesr[off+i]
	}
	off += 16
	for i := 0; i < 16; i++ {
		f.ErrMPPT[i] = kesr[off+i]
	}

	f.Settings = make([]uint16, 40)
	for i := 0; i < 40; i++ {
		f.Settings[i] = binary.LittleEndian.Uint16(kess[kesKessSettingsOff+2*i : kesKessSettingsOff+2*i+2])
	}
	return f, true
}

// kesSettingsAddr — адреса (RAM) настроек КЭС в порядке KESS. Синхронно с
// daemons/kes-reader/kes.go (kesSettings).
var kesSettingsAddr = []struct {
	ram  uint16
	size int
}{
	{0x210, 1}, {0x211, 1}, {0x212, 2}, {0x214, 2}, {0x216, 1}, {0x217, 1},
	{0x218, 1}, {0x219, 1}, {0x21A, 1}, {0x21C, 1}, {0x21D, 1}, {0x21E, 2},
	{0x220, 2}, {0x222, 2}, {0x224, 2}, {0x226, 2}, {0x228, 2}, {0x22A, 2},
	{0x22C, 2}, {0x22E, 2}, {0x230, 2}, {0x232, 2}, {0x234, 2}, {0x236, 2},
	{0x238, 2}, {0x23A, 2}, {0x23C, 1}, {0x23D, 1}, {0x23E, 1}, {0x23F, 1},
	{0x240, 1}, {0x241, 2}, {0x243, 1}, {0x244, 2}, {0x246, 2}, {0x248, 2},
	{0x24A, 2}, {0x24C, 2}, {0x24E, 2}, {0x250, 2},
}

// buildKesVA собирает виртуальное адресное пространство КЭС: массив нулей
// kesVirtualSize байт, затем раскладывает значения из снимка по их исходным
// RAM-адресам (LE по размеру каждой ячейки), как и первоначальное размещение в
// памяти КЭС. Используется для legacy read_sec (чтение диапазона из образа).
func buildKesVA(f *kesFields) []byte {
	va := make([]byte, kesVirtualSize)
	put := func(addr uint16, size int, v uint32) {
		for b := 0; b < size && int(addr)+b < len(va); b++ {
			va[int(addr)+b] = byte(v >> (8 * uint(b)))
		}
	}

	// EEPROM/параметры устройства (адрес «как есть», docs/kes-memory.md §2).
	put(0x0000, 1, uint32(f.Device))
	put(0x0001, 1, uint32(f.Pow))
	put(0x0002, 1, uint32(f.FW))
	put(0x0005, 1, uint32(f.ModelPow))
	put(0x000C, 1, uint32(f.Pic))
	put(0x000D, 1, uint32(f.Net))
	put(0x009B, 1, uint32(f.ModelVolt))
	put(0x2C9, 2, uint32(f.Serial))

	// Настройки (RAM-копии, docs/kes-memory.md §3).
	for i, s := range kesSettingsAddr {
		if i < len(f.Settings) {
			put(s.ram, s.size, uint32(f.Settings[i]))
		}
	}

	// Телеметрия (RAM, docs/kes-memory.md §1).
	put(0x2D9, 2, uint32(f.VcPV))
	put(0x2DF, 2, uint32(f.IcPV))
	put(0x2E5, 2, uint32(f.VBat))
	put(0x2EB, 2, uint32(f.PPV))
	put(0x2ED, 2, uint32(f.POut))
	put(0x2F5, 2, uint32(f.PLoad))
	put(0x2F9, 2, uint32(f.PCurr))
	put(0x2FB, 2, uint32(f.ICh))
	put(0x2FF, 2, uint32(f.IOut))
	put(0x31D, 1, uint32(f.TempInt))
	put(0x31E, 1, uint32(f.TempBat))
	put(0x39F, 2, uint32(f.PwrW))
	put(0x3A1, 2, uint32(f.PwrKW))
	put(0x3B6, 1, uint32(f.RSErrSis))
	put(0x3E3, 1, uint32(f.RelayC))
	put(0x404, 1, uint32(f.SignC0))
	put(0x405, 1, uint32(f.SignC1))
	put(0x406, 2, uint32(f.IExts0))
	put(0x408, 2, uint32(f.IExts1))
	put(0x40A, 2, uint32(f.PExts0))
	put(0x40C, 2, uint32(f.PExts1))
	put(0xD5A, 1, uint32(f.I2CErr))

	// BMS и параллельные MPPT (docs/kes-memory.md §1).
	for i := 0; i < 32; i++ {
		put(0xD80+uint16(2*i), 2, uint32(f.UakbCell[i]))
		put(0xDC0+uint16(i), 1, uint32(f.TakbCell[i]))
		put(0xDE0+uint16(2*i), 2, uint32(f.QCell[i]))
		put(0xE00+uint16(i), 1, uint32(f.ErrCells[i]))
	}
	for i := 0; i < 16; i++ {
		put(0xE30+uint16(2*i), 2, uint32(f.IAkbMPPT[i]))
		put(0xE50+uint16(i), 1, uint32(f.ModeMPPT[i]))
		put(0xE60+uint16(i), 1, uint32(f.TakbMPPT[i]))
		put(0xE70+uint16(i), 1, uint32(f.ErrMPPT[i]))
	}
	return va
}

// renderKesJSON — legacy-снимок одного КЭС для read_json.php?device=mppt.
// Значения — строки (как в legacy). Неподтверждённые Mode/Sign/MPP/windspeed —
// пустые строки (в контракт не публикуются).
func renderKesJSON(f *kesFields) []byte {
	var b strings.Builder
	b.WriteByte('{')
	now := time.Unix(f.LastSeen, 0)
	write := func(name, val string) {
		b.WriteString(fmt.Sprintf(`%q:%q,`, name, val))
	}
	write("timestamp", fmt.Sprintf("%d", f.LastSeen))
	write("time", now.Format("15:04:05"))
	write("UID", fmt.Sprintf("%d", f.Serial))
	write("Vc_PV", fmt.Sprintf("%.1f", float64(f.VcPV)/100))
	write("Ic_PV", fmt.Sprintf("%.1f", float64(f.IcPV)/100))
	write("V_Bat", fmt.Sprintf("%.1f", float64(f.VBat)/100))
	write("P_PV", fmt.Sprintf("%d", f.PPV))
	write("P_Out", fmt.Sprintf("%d", f.POut))
	write("P_Load", fmt.Sprintf("%d", f.PLoad))
	write("P_curr", fmt.Sprintf("%d", f.PCurr))
	write("I_Ch", fmt.Sprintf("%.1f", float64(f.ICh)/100))
	write("I_Out", fmt.Sprintf("%.1f", float64(f.IOut)/100))
	write("Temp_Int", fmt.Sprintf("%d", int(f.TempInt)-50))
	write("Temp_Bat", fmt.Sprintf("%d", int(f.TempBat)-50))
	write("Pwr_kW", fmt.Sprintf("%d.%03d", f.PwrKW, f.PwrW))
	write("Sign_C0", fmt.Sprintf("%d", f.SignC0))
	write("Sign_C1", fmt.Sprintf("%d", f.SignC1))
	write("I_EXTS0", fmt.Sprintf("%.1f", float64(f.IExts0)/100))
	write("I_EXTS1", fmt.Sprintf("%.1f", float64(f.IExts1)/100))
	write("P_EXTS0", fmt.Sprintf("%d", f.PExts0))
	write("P_EXTS1", fmt.Sprintf("%d", f.PExts1))
	write("Relay_C", fmt.Sprintf("%d", f.RelayC))
	write("RSErrSis", fmt.Sprintf("%d", f.RSErrSis))
	write("Mode", "")
	write("Sign", "")
	write("MPP", "")
	write("windspeed", "")
	write("FW", fmt.Sprintf("%d.%d", f.FW&0x0F, f.FW>>5))
	write("R1", fmt.Sprintf("%d", (f.RelayC>>0)&1))
	write("R2", fmt.Sprintf("%d", (f.RelayC>>1)&1))
	write("R3", fmt.Sprintf("%d", (f.RelayC>>2)&1))
	s := b.String()
	// убрать висящую запятую перед закрывающей }
	if strings.HasSuffix(s, ",") {
		s = s[:len(s)-1]
	}
	return []byte(s + "}")
}
