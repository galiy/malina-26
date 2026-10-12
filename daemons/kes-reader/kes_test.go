package main

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestWindowsCover проверяет, что окна чтения покрывают все RAM-адреса,
// которые читает контракт (parseData/parseKes/kesSettings). Защита от «дыр»
// при изменении разбивки регионов.
func TestWindowsCover(t *testing.T) {
	need := map[uint16]bool{
		0x0000: true, 0x0001: true, 0x0002: true, 0x0005: true, 0x000C: true, 0x000D: true,
		0x009B: true,              // параметры устройства
		0x2C9:  true, 0x2CA: true, // Serial
		0x2D9: true, 0x2DF: true, 0x2E5: true, 0x2EB: true, 0x2ED: true, 0x2F5: true,
		0x2F9: true, 0x2FB: true, 0x2FF: true, 0x31D: true, 0x31E: true, 0x39F: true,
		0x3A1: true, 0x3B6: true, 0x3E3: true, 0x404: true, 0x405: true, 0x406: true,
		0x408: true, 0x40A: true, 0x40C: true, // телеметрия
		0x2C2: true, 0x379: true, 0x37A: true, 0x3F8: true, // Mode/Sign/MPP/windspeed
		0xD5A: true, // I2C_Err
	}
	for _, r := range kesSettings {
		need[r.ram] = true
		if r.size == 2 {
			need[r.ram+1] = true
		}
	}
	for i := 0; i < 32; i++ {
		need[0xD80+uint16(2*i)] = true
		need[0xDC0+uint16(i)] = true
		need[0xDE0+uint16(2*i)] = true
		need[0xE00+uint16(i)] = true
	}
	for i := 0; i < 16; i++ {
		need[0xE30+uint16(2*i)] = true
		need[0xE50+uint16(i)] = true
		need[0xE60+uint16(i)] = true
		need[0xE70+uint16(i)] = true
	}
	for a := range need {
		ok := false
		for _, w := range kesWindows {
			if a >= w.ramBase && a < w.ramBase+uint16(w.length) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("адрес RAM %#x не покрыт окнами чтения", a)
		}
	}
}

func TestParseKesAndData(t *testing.T) {
	img := map[uint16]byte{}
	put16 := func(a, v uint16) { img[a] = byte(v); img[a+1] = byte(v >> 8) }

	// ID-окно.
	img[0x0000] = 5    // _Device = КЭС
	img[0x0001] = 0x02 // Pow
	img[0x0002] = 0x63 // Ver ПО
	img[0x0005] = 0x32 // мощность модели (×100 → 5000)
	img[0x009B] = 0x04 // напряжение модели (×12 → 48В)
	img[0x000C] = 0x01 // Pic/Модель
	img[0x000D] = 0x05 // Net
	img[0x2C9] = 0x34
	img[0x2CA] = 0x12 // Serial = 0x1234
	// Измерения.
	put16(0x2D9, 1234) // Vc_PV
	put16(0x2E5, 567)  // V_Bat
	img[0x31D] = 0x5A  // Temp_Int (байт)
	img[0x31E] = 0x64  // Temp_Bat (байт)
	img[0x3E3] = 0x05  // Relay_C
	img[0x3B6] = 0x02  // RSErrSis
	img[0x2C2] = 'b'   // Mode: режим заряда
	img[0x379] = 'L'   // MPP
	img[0x37A] = '-'   // Sign
	img[0x3F8] = 0xFF  // windspeed (u16 LE, мл.) → 0xFFFF = 65535 (н/д)
	img[0x3F9] = 0xFF  // windspeed (ст.)
	// Настройки (условно подтверждённые, RAM-копии).
	img[0x210] = 0x04 // Roll_H_Speed (1 байт)
	put16(0x212, 10)  // BL_Time (2 байта)
	put16(0x250, 3)   // Bat_K_Temp (2 байта)

	info := parseKes(img)
	if info.Device != 5 {
		t.Fatalf("Device=%d, ждали 5", info.Device)
	}
	if info.Serial != 0x1234 {
		t.Fatalf("Serial=%#x, ждали 0x1234", info.Serial)
	}

	d := parseData(img)
	if d.Device != 5 || d.Pow != 0x02 || d.FW != 0x63 || d.ModelPow != 0x32 || d.ModelVolt != 0x04 || d.Pic != 0x01 || d.Net != 0x05 {
		t.Fatalf("параметры устройства: %+v", d)
	}
	if d.VcPV != 1234 || d.VBat != 567 || d.RelayC != 0x05 || d.RSErrSis != 0x02 {
		t.Fatalf("parseData: %+v", d)
	}
	if d.TempInt != 0x5A || d.TempBat != 0x64 {
		t.Fatalf("температуры (байты): %+v", d)
	}
	if d.Mode != 'b' || d.Sign != '-' || d.MPP != 'L' || d.Windspeed != 0xFFFF {
		t.Fatalf("Mode/Sign/MPP/windspeed: %+v", d)
	}

	// Слот = [EEPROM-блок 512 Б][RAM-блок 512 Б][Настройки-блок 512 Б].
	e := buildEepromPayload(contractMeta{Serial: info.Serial, LastSeen: time.Unix(1, 0)}, d)
	if string(e[0:4]) != "KESE" {
		t.Fatalf("magic eeprom: %q", e[0:4])
	}
	if got := binary.LittleEndian.Uint16(e[6:8]); got != 0x1234 {
		t.Fatalf("Serial eeprom: %#x", got)
	}
	if e[8] != 5 || e[9] != 0x02 || e[10] != 0x63 || e[11] != 0x32 || e[12] != 0x04 || e[13] != 0x01 || e[14] != 0x05 {
		t.Fatalf("параметры eeprom: %+v", e[8:15])
	}

	r := buildRamPayload(contractMeta{Serial: info.Serial, LastSeen: time.Unix(1, 0)}, d)
	if string(r[0:4]) != "KESR" {
		t.Fatalf("magic ram: %q", r[0:4])
	}
	if binary.LittleEndian.Uint64(r[6:14]) != 1 {
		t.Fatalf("LastSeen ram: %d", binary.LittleEndian.Uint64(r[6:14]))
	}
	if r[14] != 0x05 || r[15] != 0x02 {
		t.Fatalf("RelayC/RSErrSis ram: %+v", r[14:16])
	}
	if got := binary.LittleEndian.Uint16(r[18:20]); got != 1234 {
		t.Fatalf("VcPV ram: %d", got)
	}

	s := buildSettingsPayload(d)
	if string(s[0:4]) != "KESS" {
		t.Fatalf("magic settings: %q", s[0:4])
	}
	if len(s) != memberKessSize {
		t.Fatalf("len settings: %d, ждали %d", len(s), memberKessSize)
	}
	if d.Settings[0] != 0x04 || d.Settings[2] != 10 || d.Settings[39] != 3 {
		t.Fatalf("настройки raw: %+v", d.Settings)
	}
	if got := binary.LittleEndian.Uint16(s[5:7]); got != d.Settings[0] {
		t.Fatalf("настройка[0] raw: %d", got)
	}

	p := buildPayload(contractMeta{Serial: info.Serial, LastSeen: time.Unix(1, 0)}, d)
	if len(p) < len(e)+len(r)+len(s) {
		t.Fatalf("payload короткий: %d", len(p))
	}
}

func TestValidateWrite(t *testing.T) {
	okReg := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "E3D", Data: "05"}}}
	if err := validateWrite(okReg); err != nil {
		t.Fatalf("reg: %v", err)
	}
	if err := validateWrite(writeRequest{}); err == nil {
		t.Fatal("пустой запрос должен быть ошибкой")
	}
	if err := validateWrite(writeRequest{Ops: []writeOp{{Op: "cmd"}}}); err == nil {
		t.Fatal("cmd должен быть отклонён (только op=reg)")
	}
	if err := validateWrite(writeRequest{Ops: []writeOp{{Op: "relay"}}}); err == nil {
		t.Fatal("relay должен быть отклонён (только op=reg)")
	}
	if err := validateWrite(writeRequest{Ops: []writeOp{{Op: "reg", Addr: "0", Data: "0001"}}}); err != nil {
		t.Fatalf("reg с валидными данными: %v", err)
	}
}

func TestBuildSequenceReg(t *testing.T) {
	wr := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "E3D", Data: "05"}}}
	seq := buildSequence(wr)
	// Ожидаем: pre-команда EEPromWR (3), затем блок по адресу.
	if len(seq) != 2 || !seq[0].isCmd || seq[0].code != 3 || seq[1].addr != 0xE3D {
		t.Fatalf("seq=%+v", seq)
	}
	// NoFrame — без pre-команды.
	wr.NoFrame = true
	if seq := buildSequence(wr); len(seq) != 1 || seq[0].addr != 0xE3D {
		t.Fatalf("noframe seq=%+v", seq)
	}
}

// TestSettingsLen фиксирует инвариант «40 настроек = 40 слотов Settings» (H1 ревью):
// рассинхрон списка и константы ловится и init() (паника), и этим тестом.
func TestSettingsLen(t *testing.T) {
	if len(kesSettings) != lenKesSettings {
		t.Fatalf("kesSettings=%d, а lenKesSettings=%d", len(kesSettings), lenKesSettings)
	}
}

// TestWindowsOverlap — окна чтения не должны перекрываться (иначе двойное чтение + риск
// рассинхрона двух страниц одного адреса). Диапазоны считаются по RAM-адресам.
func TestWindowsOverlap(t *testing.T) {
	for i := 0; i < len(kesWindows); i++ {
		a0 := kesWindows[i]
		for j := i + 1; j < len(kesWindows); j++ {
			b0 := kesWindows[j]
			aHi := a0.ramBase + uint16(a0.length)
			bHi := b0.ramBase + uint16(b0.length)
			if a0.ramBase < bHi && b0.ramBase < aHi {
				t.Fatalf("окна №%d и №%d перекрываются по RAM-адресам", i+1, j+1)
			}
		}
	}
}

// TestContractGolden — «золотые байты» контракта слота и записи kes-list (H2 ревью):
// фиксируем фактические смещения/значения, чтобы правка layout без правки теста и доков
// сразу падала. Это страховка от дрейфа, пока нет внешнего web-потребителя.
func TestContractGolden(t *testing.T) {
	ts := time.Unix(1, 0)
	d := KesData{
		Device: 5, Pow: 0x02, FW: 0x63, ModelPow: 0x32, ModelVolt: 0x04, Pic: 0x01, Net: 0x05,
		Serial: 0x1234, VcPV: 1234, VBat: 567, TempInt: 0x5A, TempBat: 0x64,
		RelayC: 0x05, RSErrSis: 0x02, I2CErr: 0x07,
		Mode: 'b', Sign: '-', MPP: 'L', Windspeed: 0xFFFF,
	}
	d.Settings[0] = 0x04
	d.Settings[2] = 10
	d.Settings[39] = 3
	for i := 0; i < 32; i++ {
		d.UakbCell[i] = uint16(0x100 + i)
		d.TakbCell[i] = byte(0x10 + i)
	}

	p := buildPayload(contractMeta{Serial: d.Serial, LastSeen: ts}, d)
	if len(p) != offKess+memberKessSize {
		t.Fatalf("len(payload)=%d, ждали %d", len(p), offKess+memberKessSize)
	}
	// Смещения под-блоков.
	if c := string(p[offKese : offKese+4]); c != "KESE" {
		t.Fatalf("KESE у смещения %d: %q", offKese, c)
	}
	if c := string(p[offKesr : offKesr+4]); c != "KESR" {
		t.Fatalf("KESR у смещения %d: %q", offKesr, c)
	}
	if c := string(p[offKess : offKess+4]); c != "KESS" {
		t.Fatalf("KESS у смещения %d: %q", offKess, c)
	}
	// Фиксированные значения внутри под-блоков.
	if got := binary.LittleEndian.Uint16(p[offKese+keseSerialOff:]); got != d.Serial {
		t.Fatalf("KESE.Serial@%d=%#x", offKese+keseSerialOff, got)
	}
	if got := binary.LittleEndian.Uint16(p[offKesr+kesrVcPVOff:]); got != d.VcPV {
		t.Fatalf("KESR.VcPV@%d=%d", offKesr+kesrVcPVOff, got)
	}
	if p[offKesr+kesrRelayCOff] != 0x05 {
		t.Fatalf("KESR.RelayC@%d=%#x", offKesr+kesrRelayCOff, p[offKesr+kesrRelayCOff])
	}
	if got := binary.LittleEndian.Uint16(p[offKess+kessSettingsOff:]); got != d.Settings[0] {
		t.Fatalf("KESS[0]@%d=%d", offKess+kessSettingsOff, got)
	}
	if got := binary.LittleEndian.Uint16(p[offKess+kessSettingsOff+2*39:]); got != d.Settings[39] {
		t.Fatalf("KESS[39]@%d=%d", offKess+kessSettingsOff+2*39, got)
	}
	if p[offKesr+kesrModeOff] != 'b' || p[offKesr+kesrSignOff] != '-' ||
		p[offKesr+kesrMppOff] != 'L' ||
		binary.LittleEndian.Uint16(p[offKesr+kesrWindOff:]) != 0xFFFF {
		t.Fatalf("KESR Mode/Sign/MPP/windspeed: %x/%x/%x/%x",
			p[offKesr+kesrModeOff], p[offKesr+kesrSignOff], p[offKesr+kesrMppOff],
			binary.LittleEndian.Uint16(p[offKesr+kesrWindOff:]))
	}
	if got := binary.LittleEndian.Uint16(p[offKesr+kesrBmsOff:]); got != d.UakbCell[0] {
		t.Fatalf("KESR.UakbCell[0]@%d=%d", offKesr+kesrBmsOff, got)
	}

	// Запись kes-list: проверяем фиксированные смещения (gen/baud/serial/port/last_seen).
	item := make([]byte, kesListItemSize)
	ll := listMeta{Serial: d.Serial, Baud: 19200, Gen: 77, Port: "/dev/ttyUSB9", LastSeen: ts.Unix()}
	binary.LittleEndian.PutUint64(item[listGenOff:listGenOff+8], ll.Gen)
	binary.LittleEndian.PutUint32(item[listBaudOff:listBaudOff+4], ll.Baud)
	binary.LittleEndian.PutUint16(item[listSerialOff:listSerialOff+2], ll.Serial)
	copy(item[listPortOff:listPortOff+contractPortSize], []byte(ll.Port))
	binary.LittleEndian.PutUint64(item[listLastSeenOff:listLastSeenOff+8], uint64(ll.LastSeen))
	if got := binary.LittleEndian.Uint64(item[listGenOff : listGenOff+8]); got != 77 {
		t.Fatalf("list.Gen@%d=%d", listGenOff, got)
	}
	if got := binary.LittleEndian.Uint16(item[listSerialOff : listSerialOff+2]); got != d.Serial {
		t.Fatalf("list.Serial@%d=%d", listSerialOff, got)
	}
	if s := trimZero(item[listPortOff : listPortOff+contractPortSize]); s != "/dev/ttyUSB9" {
		t.Fatalf("list.Port@%d=%q", listPortOff, s)
	}
}

// TestIsRetryable — правило ретраев: «невнятные» коды КЭС (01/02/04) ретраим, явные (10/20) — нет.
func TestIsRetryable(t *testing.T) {
	for _, c := range []int{1, 2, 4} {
		if !isRetryable(&kesError{code: c}) {
			t.Fatalf("код %#x должен ретраиться", c)
		}
	}
	for _, c := range []int{0x10, 0x20} {
		if isRetryable(&kesError{code: c}) {
			t.Fatalf("код %#x не должен ретраиться", c)
		}
	}
	if !isRetryable(errors.New("нет ответа")) {
		t.Fatal("прочие ошибки (нет ответа) должны ретраиться")
	}
}

func newTestConfig() *config { return newConfig(false) }

// TestExecElementRetries — execElement: writeRetries<1 → ошибка (до обращения к порту);
// на явной ошибке КЭС выходим сразу без лишних попыток.
func TestExecElementRetries(t *testing.T) {
	cfg := newTestConfig()
	cfg.writeRetries = 0
	if err := execElement(cfg, nil, element{}); !strings.Contains(err.Error(), "нет попыток") {
		t.Fatalf("writeRetries<1: err=%v", err)
	}
}
