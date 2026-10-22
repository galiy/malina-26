package main

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// TestFrameUpperHex — INFO в кадре должен быть в ВЕРХНЕМ регистре hex
// (инвертор принимает только верхний; на нижнем показывал 9.99).
func TestFrameUpperHex(t *testing.T) {
	var tel bmscontract.Telemetry
	tel.CellCount = 16
	tel.TempCount = 6
	for i := 0; i < 16; i++ {
		tel.CellsMV[i] = 3200 + uint16(i)
	}
	fr := buildTelemetryFrame(2, buildSeplosInfo(&tel))
	body := string(fr[1 : len(fr)-1])
	if strings.ToUpper(body) != body {
		t.Fatalf("кадр не в верхнем регистре: %s", body)
	}
	if !checkPaceChk(body) {
		t.Fatal("CHK невалиден")
	}
}

// TestUDPSyncRetry — неудачный старт UDP-слушателя (порт занят) не кэшируется:
// повторный sync с тем же адресом после освобождения порта обязан поднять его.
func TestUDPSyncRetry(t *testing.T) {
	occ, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	addr := occ.LocalAddr().String()
	d := &daemon{cfg: newConfig(), udp: &udpManager{}, sc: newPortScanner(make(chan struct{}))}

	d.udp.sync(addr, d)
	if d.udp.cur != nil {
		d.udp.stop()
		t.Fatal("слушатель поднялся на занятом порту")
	}
	if d.udp.addr != "" {
		t.Fatalf("неуспех закэширован: addr=%q", d.udp.addr)
	}
	_ = occ.Close()

	d.udp.sync(addr, d)
	if d.udp.cur == nil {
		t.Fatal("повторный sync не поднял слушатель после освобождения порта")
	}
	d.udp.stop()
	if d.udp.cur != nil {
		t.Fatal("stop не остановил слушатель")
	}
}

func TestParseRequest(t *testing.T) {
	// Собираем корректный запрос `~ VER ADR 46 42 E002 NN CHK CR`.
	tok := "20014642E00201" // VER ADR CID1 CID2 E002 NN
	req := "~" + tok + hex4(paceChk(tok)) + "\r"
	p := parseRequest([]byte(req))
	if p == nil {
		t.Fatal("не разобран корректный запрос")
	}
	if p.Adr != 1 || p.CID2 != 0x42 || !p.OK {
		t.Fatalf("поля: adr=%d cid2=%#x ok=%v", p.Adr, p.CID2, p.OK)
	}

	// Некорректный кадр.
	if parseRequest([]byte("\x00\x01\x02")) != nil {
		t.Fatal("мусор принят как PACE")
	}
	if parseRequest([]byte("~20014642E002\r")) != nil {
		t.Fatal("слишком короткий кадр принят")
	}
}

func TestBuildFrameRoundtrip(t *testing.T) {
	info := []byte{0x00, 0x01, 0x02, 0x03}
	fr := buildTelemetryFrame(2, info)
	if fr[0] != '~' || fr[len(fr)-1] != '\r' {
		t.Fatalf("кадр не в границах ~..\\r: %q", fr)
	}
	// проверка собранного кадра: CHK и LENID == длина INFO в hex-символах
	s := string(fr[1 : len(fr)-1])
	lenid := hexInt(s[8:12]) & 0x0FFF
	if lenid != 2*len(info) {
		t.Fatalf("LENID=%d, ожидалось %d", lenid, 2*len(info))
	}
	if checkPaceChk(s) != true {
		t.Fatalf("CHK невалиден: %s", s)
	}
}

func TestBuildSeplosInfo(t *testing.T) {
	var tel bmscontract.Telemetry
	tel.CellCount = 3
	tel.TempCount = 2
	tel.CellsMV[0], tel.CellsMV[1], tel.CellsMV[2] = 3200, 3210, 3190
	tel.TempsDC[0], tel.TempsDC[1] = 250, 251 // 25.0, 25.1 °C
	tel.CurrentDA = 1234                      // 12.34 А (разряд +)
	tel.TotalCV = 9600                        // 96.00 В
	tel.PortCV = 9590
	tel.RemainingMAh = 200000 // 200.0 А·ч
	tel.RatedMAh = 300000     // 300.0 А·ч
	tel.SocDP = 654           // 65.4 %
	tel.SohDP = 998           // 99.8 %
	tel.Cycles = 42

	info := buildSeplosInfo(&tel)
	// 31 + 2*ncells + 2*ntemps; здесь n=3, tc=2 → 41
	if len(info) != 31+2*3+2*2 {
		t.Fatalf("длина=%d", len(info))
	}
	if info[0] != 0x00 || info[1] != 0x00 {
		t.Fatalf("заголовок: %#x %#x", info[0], info[1])
	}
	if info[2] != 3 {
		t.Fatalf("число ячеек=%d", info[2])
	}
	// ячейки мВ
	if got := binary.BigEndian.Uint16(info[3:5]); got != 3200 {
		t.Fatalf("ячейка0=%d", got)
	}
	// температура 25.0 °C → 0.1 K = 250+2731 = 2981
	offTemp := 3 + 3*2 + 1 // header(2)+cell_count(1)+cells(3*2)+temp_count(1)
	if got := binary.BigEndian.Uint16(info[offTemp : offTemp+2]); got != uint16(250+2731) {
		t.Fatalf("temp0=%d, ожидалось %d", got, uint16(250+2731))
	}
	// ток: разряд + → SEPLOS минус → -1234
	offI := offTemp + 2*2 // сразу после temperatures
	if got := int16(binary.BigEndian.Uint16(info[offI : offI+2])); got != -int16(1234) {
		t.Fatalf("ток=%d, ожидалось -1234", got)
	}
	// field count после остаточной ёмкости = 0x00
	offRem := offI + 2         // ток
	offField := offRem + 2 + 2 // total voltage + remaining
	if info[offField] != 0x00 {
		t.Fatalf("field count=%#x, ожидалось 0x00", info[offField])
	}
}

// TestBuildSeplosInfo75 — карта 16S_V20: 16 ячеек и 6 температур дают ровно
// 75-байтное INFO и LEN=0x1096, как в эталонном ответе protocol-485.md §5.
func TestBuildSeplosInfo75(t *testing.T) {
	var tel bmscontract.Telemetry
	tel.CellCount = 16
	tel.TempCount = 8 // контракт допускает 8, карта 16S_V20 — 6
	for i := 0; i < 16; i++ {
		tel.CellsMV[i] = 3200 + uint16(i)
	}
	info := buildSeplosInfo(&tel)
	if len(info) != 75 {
		t.Fatalf("INFO=%d байт, ожидалось 75", len(info))
	}
	if len(info) != 67+seplosTail {
		t.Fatalf("ядро+хвост=%d, ожидалось %d", len(info), 67+seplosTail)
	}
	if info[0] != 0x00 || info[1] != 0x00 {
		t.Fatalf("dataflag/группа=%#x %#x, ожидалось 00 00", info[0], info[1])
	}
	if info[35] != seplosTemps {
		t.Fatalf("temp count=%d, ожидалось %d", info[35], seplosTemps)
	}
	if info[54] != 0x00 {
		t.Fatalf("field count=%#x, ожидалось 0x00", info[54])
	}
	fr := buildTelemetryFrame(0, info)
	s := string(fr[1 : len(fr)-1])
	if s[8:12] != "1096" {
		t.Fatalf("LEN=%s, ожидалось 1096", s[8:12])
	}
	if !checkPaceChk(s) {
		t.Fatal("CHK невалиден")
	}
}

// TestBuildSeplosInfoDefaults — при отсутствующих полях (AntBMS: rated=0,
// soh=0, port=0) подставляются: ёмкость из CapMAh, SOH=100 %, порт=общее.
func TestBuildSeplosInfoDefaults(t *testing.T) {
	var tel bmscontract.Telemetry
	tel.CellCount = 16
	tel.TempCount = 6
	tel.TotalCV = 5120 // 51.20 В
	tel.CapMAh = 320000
	tel.RatedMAh = 0
	tel.SohDP = 0
	tel.PortCV = 0
	info := buildSeplosInfo(&tel)
	if info[54] != 0x00 {
		t.Fatalf("field count=%#x, ожидалось 0x00", info[54])
	}
	// полная ёмкость (offset 55) = 320000/10 = 32000 = 0x7D00
	if got := binary.BigEndian.Uint16(info[55:57]); got != 32000 {
		t.Fatalf("полная ёмкость=%d, ожидалось 32000", got)
	}
	// SOH (offset 63) = 1000
	if got := binary.BigEndian.Uint16(info[63:65]); got != 1000 {
		t.Fatalf("SOH=%d, ожидалось 1000", got)
	}
	// напряжение клемм (offset 65) = TotalCV = 5120
	if got := binary.BigEndian.Uint16(info[65:67]); got != 5120 {
		t.Fatalf("порт=%d, ожидалось 5120", got)
	}
}

// TestUDPSettingsAddr — гейт UDP: адрес отдаётся только при enabled && непустом.
func TestUDPSettingsAddr(t *testing.T) {
	cases := []struct {
		en   bool
		addr string
		want string
	}{
		{false, "0.0.0.0:502", ""},
		{true, "", ""},
		{true, "  ", ""},
		{true, "0.0.0.0:502", "0.0.0.0:502"},
		{true, "  0.0.0.0:502 ", "0.0.0.0:502"},
		{false, "", ""},
	}
	for _, c := range cases {
		got := settings{UDPEnabled: c.en, UDPListen: c.addr}.udpListenAddr()
		if got != c.want {
			t.Errorf("enabled=%v addr=%q → %q, ожидалось %q", c.en, c.addr, got, c.want)
		}
	}
}

// TestParseRequestChk — невалидный CHK помечается OK=false, а не отбрасывается.
func TestParseRequestChk(t *testing.T) {
	tok := "20014642E00201" // верный префикс
	req := "~" + tok + "0000" + "\r"
	p := parseRequest([]byte(req))
	if p == nil {
		t.Fatal("кадр не разобран")
	}
	if p.OK {
		t.Fatal("битый CHK принят как валидный")
	}
}

// TestMapAddr — ADR инвертора → стабильный слот shm.
func TestMapAddr(t *testing.T) {
	if got := mapAddr(1, addrMode1); got != 0 {
		t.Fatalf("1-16: ADR=1 → %d, ожидалось 0", got)
	}
	if got := mapAddr(16, addrMode1); got != 15 {
		t.Fatalf("1-16: ADR=16 → %d, ожидалось 15", got)
	}
	if got := mapAddr(0, addrMode0); got != 0 {
		t.Fatalf("0-15: ADR=0 → %d, ожидалось 0", got)
	}
	if got := mapAddr(15, addrMode0); got != 15 {
		t.Fatalf("0-15: ADR=15 → %d, ожидалось 15", got)
	}
	// Вне диапазона — заведомо невалидный слот, данные не отдаются.
	data := mkSeg(t)
	for _, adr := range []int{0, 16} {
		if slot := mapAddr(adr, addrMode1); readSlotTelemetry(data, slot, time.Now()) != nil {
			t.Fatalf("ADR=%d вне диапазона 1-16 отдал данные", adr)
		}
	}
	if slot := mapAddr(16, addrMode0); slot != 16 || readSlotTelemetry(data, slot, time.Now()) != nil {
		t.Fatalf("ADR=16 вне диапазона 0-15: slot=%d", slot)
	}
}

// TestNormalizeAddrMode — канонизация вида адресации инвертора.
func TestNormalizeAddrMode(t *testing.T) {
	ok := map[string]string{
		"1-16": addrMode1, "1..16": addrMode1, "1_16": addrMode1, " 1-16 ": addrMode1,
		"0-15": addrMode0, "0..15": addrMode0,
	}
	for in, want := range ok {
		if got, ok := normalizeAddrMode(in); !ok || got != want {
			t.Errorf("normalizeAddrMode(%q)=%q,%v; ожидалось %q,true", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "1-15", "2-16", "x"} {
		if got, ok := normalizeAddrMode(in); ok {
			t.Errorf("normalizeAddrMode(%q)=%q,%v; ожидалось невалидно", in, got, ok)
		}
	}
}

// TestLoadSettingsAddrModeDefault — отсутствие addr_mode → 1..16.
func TestLoadSettingsAddrModeDefault(t *testing.T) {
	s := defaultSettings()
	if s.AddrMode != addrMode1 {
		t.Fatalf("умолчание addr=%q, ожидалось %q", s.AddrMode, addrMode1)
	}
}

func TestReadSlotTelemetry(t *testing.T) {
	data := mkSeg(t)
	now := time.Now()

	// слот 0: живой (state=1, last_seen свежий)
	var it [bmsListLen]byte
	binary.LittleEndian.PutUint32(it[0:4], 1)
	binary.LittleEndian.PutUint64(it[8:16], 0) // seqlock чётный
	it[itOffState] = 1
	binary.LittleEndian.PutUint64(it[itOffGen:itOffGen+8], 100)
	binary.LittleEndian.PutUint64(it[itOffLastSeen:itOffLastSeen+8], uint64(now.Unix()))
	writeItem(data, 0, it[:])

	var tel bmscontract.Telemetry
	tel.CellCount = 2
	putSlot(data, 0, 100, bmscontract.Encode(&tel))

	got := readSlotTelemetry(data, 0, now)
	if got == nil {
		t.Fatal("живой слот не прочитан")
	}
	// слот 1 свободен
	if readSlotTelemetry(data, 1, now) != nil {
		t.Fatal("свободный слот отдан")
	}
}

func TestReadSlotTelemetryStale(t *testing.T) {
	data := mkSeg(t)
	now := time.Now()
	// last_seen старше liveWindow
	var it [bmsListLen]byte
	binary.LittleEndian.PutUint32(it[0:4], 1)
	it[itOffState] = 1
	binary.LittleEndian.PutUint64(it[itOffGen:itOffGen+8], 1)
	binary.LittleEndian.PutUint64(it[itOffLastSeen:itOffLastSeen+8], uint64(now.Add(-liveWindow-time.Second).Unix()))
	writeItem(data, 0, it[:])
	var tel bmscontract.Telemetry
	tel.CellCount = 1
	putSlot(data, 0, 1, bmscontract.Encode(&tel))

	if got := readSlotTelemetry(data, 0, now); got != nil {
		t.Fatalf("протухшая BMS отдана: %v", got)
	}
}

func TestReadTelemetry(t *testing.T) {
	data := mkSeg(t)
	now := time.Now()
	// item
	var it [bmsListLen]byte
	binary.LittleEndian.PutUint32(it[0:4], 1)
	it[itOffState] = 1
	binary.LittleEndian.PutUint64(it[itOffGen:itOffGen+8], 777)
	binary.LittleEndian.PutUint64(it[itOffLastSeen:itOffLastSeen+8], uint64(now.Unix()))
	writeItem(data, 3, it[:])

	// payload слота 3
	var tel bmscontract.Telemetry
	tel.CellCount = 2
	payload := bmscontract.Encode(&tel)
	putSlot(data, 3, 777, payload)

	got := readTelemetry(data, 3, 777)
	if got == nil || len(got) != len(payload) {
		t.Fatalf("telemetry не прочитана: %v", got)
	}
	if binary.BigEndian.Uint16(got[2:4]) != 0 {
		t.Fatalf("flags=%d", binary.BigEndian.Uint16(got[2:4]))
	}
	// другая генерация → nil
	if readTelemetry(data, 3, 999) != nil {
		t.Fatal("ген не совпал, но данные выданы")
	}
}

// --- helpers ---

func hex4(v int) string { return fourHex(v) }

func fourHex(v int) string {
	const hexdig = "0123456789ABCDEF"
	return string([]byte{hexdig[(v>>12)&0xF], hexdig[(v>>8)&0xF], hexdig[(v>>4)&0xF], hexdig[v&0xF]})
}

func hexInt(s string) int {
	var v int
	for _, c := range s {
		v = v<<4 | hexVal(byte(c))
	}
	return v
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return 0
}

// mkSeg — сегмент bms-reader: header + list + slots, с магией/версией.
func mkSeg(t *testing.T) []byte {
	t.Helper()
	size := shmHeader + bmsSlots*bmsListLen + bmsSlots*bmsSlotSize
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data[0:4], shmMagic)
	binary.LittleEndian.PutUint32(data[4:8], shmVersion)
	return data
}

func writeItem(data []byte, slot int, it []byte) {
	off := shmHeader + slot*bmsListLen
	copy(data[off:off+bmsListLen], it)
}

func putSlot(data []byte, slot int, gen uint64, payload []byte) {
	off := shmHeader + bmsSlots*bmsListLen + slot*bmsSlotSize
	s := data[off : off+bmsSlotSize]
	binary.LittleEndian.PutUint32(s[0:4], 1)
	binary.LittleEndian.PutUint64(s[8:16], gen)
	binary.LittleEndian.PutUint64(s[16:24], 0)
	binary.LittleEndian.PutUint32(s[24:28], uint32(len(payload)))
	binary.LittleEndian.PutUint32(s[28:32], crc32.ChecksumIEEE(payload))
	copy(s[bmsSlotHdr:bmsSlotHdr+len(payload)], payload)
}

// TestReadSlotStableAddressing — адресация по слоту стабильна: потеря соседнего
// слота не сдвигает и не подменяет данные запрошенного ADR.
func TestReadSlotStableAddressing(t *testing.T) {
	data := mkSeg(t)
	now := time.Now()
	mk := func(slot int, cells int, gen uint64) {
		var it [bmsListLen]byte
		binary.LittleEndian.PutUint32(it[0:4], 1)
		it[itOffState] = 1
		copy(it[itOffID:], "AntBMS-DSP-160")
		binary.LittleEndian.PutUint64(it[itOffGen:], gen)
		binary.LittleEndian.PutUint64(it[itOffLastSeen:], uint64(now.Unix()))
		writeItem(data, slot, it[:])
		var tel bmscontract.Telemetry
		tel.CellCount = uint8(cells)
		putSlot(data, slot, gen, bmscontract.Encode(&tel))
	}
	mk(0, 1, 100)
	mk(2, 2, 102)

	// «1-16»: ADR 1 → слот 0, ADR 3 → слот 2; ADR 2 (слот 1) молчит.
	if slot := mapAddr(1, addrMode1); slot != 0 || readSlotTelemetry(data, slot, now) == nil {
		t.Fatalf("ADR 1: slot=%d", slot)
	}
	if slot := mapAddr(2, addrMode1); slot != 1 || readSlotTelemetry(data, slot, now) != nil {
		t.Fatalf("ADR 2 не должен отдавать данные: slot=%d", slot)
	}
	if slot := mapAddr(3, addrMode1); slot != 2 || readSlotTelemetry(data, slot, now) == nil {
		t.Fatalf("ADR 3: slot=%d", slot)
	}
	// Потеря слота 0 не сдвигает ADR 3 на чужой слот.
	writeItem(data, 0, make([]byte, bmsListLen))
	if readSlotTelemetry(data, 0, now) != nil {
		t.Fatal("слот 0 не очищен")
	}
	if slot := mapAddr(3, addrMode1); slot != 2 {
		t.Fatalf("после потери ADR 3 съехал на слот %d", slot)
	}
	if readSlotTelemetry(data, 2, now) == nil {
		t.Fatal("слот 2 перестал читаться после потери соседнего")
	}
}

// TestReadTelemetryBadCRC — испорченный payload при старом CRC → nil.
func TestReadTelemetryBadCRC(t *testing.T) {
	data := mkSeg(t)
	var tel bmscontract.Telemetry
	tel.CellCount = 1
	payload := bmscontract.Encode(&tel)
	putSlot(data, 0, 1, payload)
	off := shmHeader + bmsSlots*bmsListLen + 0*bmsSlotSize + bmsSlotHdr
	data[off] ^= 0xff
	if got := readTelemetry(data, 0, 1); got != nil {
		t.Fatalf("битый CRC принят: %v", got)
	}
}
