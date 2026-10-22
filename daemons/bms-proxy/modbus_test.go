package main

import (
	"testing"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

func withCRC(data []byte) []byte {
	crc := modbusCRC(data)
	return append(append([]byte{}, data...), byte(crc), byte(crc>>8))
}

func TestValidModbusRequest(t *testing.T) {
	// Стандартный запрос 01 03 00 00 00 01 + CRC 84 0A.
	valid := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	if a, l, ok := validModbusRequest(valid); !ok || a != 1 || l != 8 {
		t.Fatalf("valid: a=%d l=%d ok=%v", a, l, ok)
	}
	// Битый CRC.
	if _, _, ok := validModbusRequest([]byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0B}); ok {
		t.Fatal("битый CRC принят")
	}
	// Адрес вне 1..16 (0x11 = 17).
	if _, _, ok := validModbusRequest(withCRC([]byte{0x11, 0x03, 0x00, 0x00, 0x00, 0x01})); ok {
		t.Fatal("адрес 17 принят")
	}
	// 0x10 write multiple: addr 2, 1 регистр.
	frame := withCRC([]byte{0x02, 0x10, 0x00, 0x10, 0x00, 0x01, 0x02, 0x00, 0x64})
	if a, l, ok := validModbusRequest(frame); !ok || a != 2 || l != len(frame) {
		t.Fatalf("0x10: a=%d l=%d ok=%v len=%d", a, l, ok, len(frame))
	}
	// Мусор перед кадром не мешает найти запрос.
	buf := append([]byte{0xAA, 0xBB}, valid...)
	if a, _, ok := validModbusRequest(buf); !ok || a != 1 {
		t.Fatal("кадр после мусора не найден")
	}
}

// TestIsVektorCellRequest — отвечаем только на 0x03 0x0071 qty 16.
func TestIsVektorCellRequest(t *testing.T) {
	// Реальный запрос инвертора: 01 03 00 71 00 10 14 1D.
	req := []byte{0x01, 0x03, 0x00, 0x71, 0x00, 0x10, 0x14, 0x1D}
	start, _, n, ok := findModbusRequest(req)
	if !ok {
		t.Fatal("запрос ячеек не распознан")
	}
	if !isVektorCellRequest(req, start, n) {
		t.Fatal("запрос ячеек не принят")
	}
	// Второй пакет 0x0081.. не поддерживаем (режим «1 устройство»).
	req2 := []byte{0x01, 0x03, 0x00, 0x81, 0x00, 0x10, 0x14, 0x2E}
	start, _, n, ok = findModbusRequest(req2)
	if !ok {
		t.Fatal("запрос 0x0081 не распознан")
	}
	if isVektorCellRequest(req2, start, n) {
		t.Fatal("запрос 0x0081 принят, должен игнорироваться")
	}
	// Другая функция (0x04) — не отвечаем.
	req4 := withCRC([]byte{0x01, 0x04, 0x00, 0x71, 0x00, 0x10})
	start, _, n, ok = findModbusRequest(req4)
	if !ok {
		t.Fatal("0x04 не распознан")
	}
	if isVektorCellRequest(req4, start, n) {
		t.Fatal("0x04 принят, должен игнорироваться")
	}
}

// TestBuildVektorCellResponse — формат ответа и CRC.
func TestBuildVektorCellResponse(t *testing.T) {
	var cells [vektorCellCount]uint16
	for i := range cells {
		cells[i] = uint16(3200 + i)
	}
	b := buildVektorCellResponse(1, cells)
	if len(b) != 3+2*vektorCellCount+2 {
		t.Fatalf("длина ответа = %d", len(b))
	}
	if b[0] != 1 || b[1] != modbusFnReadHolding || b[2] != 2*vektorCellCount {
		t.Fatalf("заголовок: % X", b[:3])
	}
	if modbusCRC(b) != 0 {
		t.Fatalf("CRC ответа неверен: % X", b)
	}
	for i := range cells {
		got := uint16(b[3+2*i])<<8 | uint16(b[4+2*i])
		if got != cells[i] {
			t.Fatalf("ячейка %d = %d, ожидалось %d", i+1, got, cells[i])
		}
	}
}

// TestSyntheticCells — min/max в первых двух, сумма = среднее пакетов.
func TestSyntheticCells(t *testing.T) {
	mk := func(vals ...uint16) bmscontract.Telemetry {
		var t bmscontract.Telemetry
		t.CellCount = uint8(len(vals))
		copy(t.CellsMV[:], vals)
		return t
	}
	tels := []bmscontract.Telemetry{
		mk(3200, 3300, 3400, 3500), // P=13400
		mk(3100, 3200, 3300, 3400), // P=13000
	}
	cells, ok := syntheticCells(tels)
	if !ok {
		t.Fatal("синтез не выполнен")
	}
	if cells[0] != 3100 || cells[1] != 3500 {
		t.Fatalf("min/max: %d %d", cells[0], cells[1])
	}
	sum := 0
	for _, v := range cells {
		sum += int(v)
	}
	if sum != 13200 { // PAvg = (13400+13000)/2
		t.Fatalf("сумма = %d, ожидалось 13200", sum)
	}
	// Остаток делится ровно: 6600/14 = 471 + 6.
	if cells[2] != 472 || cells[7] != 472 || cells[8] != 471 || cells[15] != 471 {
		t.Fatalf("распределение остатка: %v", cells)
	}
}

// TestSyntheticCellsEmpty — нет активных BMS.
func TestSyntheticCellsEmpty(t *testing.T) {
	if _, ok := syntheticCells(nil); ok {
		t.Fatal("без BMS синтез должен быть невозможен")
	}
	var empty bmscontract.Telemetry
	empty.CellCount = 4 // ячейки нулевые
	if _, ok := syntheticCells([]bmscontract.Telemetry{empty}); ok {
		t.Fatal("BMS с нулевыми ячейками не должен учитываться")
	}
}

// TestSyntheticCellsRounding — PAvg округляется half-up; одна BMS работает.
func TestSyntheticCellsRounding(t *testing.T) {
	mk := func(vals ...uint16) bmscontract.Telemetry {
		var t bmscontract.Telemetry
		t.CellCount = uint8(len(vals))
		copy(t.CellsMV[:], vals)
		return t
	}
	// (6400+6401)/2 = 6400.5 → 6401.
	cells, ok := syntheticCells([]bmscontract.Telemetry{
		mk(3200, 3200),
		mk(3200, 3201),
	})
	if !ok || cells[0] != 3200 || cells[1] != 3201 {
		t.Fatalf("округление/min-max: ok=%v cells=%v", ok, cells)
	}
	sum := 0
	for _, v := range cells {
		sum += int(v)
	}
	if sum != 6401 {
		t.Fatalf("сумма = %d, ожидалось 6401", sum)
	}
	// Одна BMS: PAvg = P, все ячейки заполняются.
	one, ok := syntheticCells([]bmscontract.Telemetry{mk(3300, 3300, 3300)})
	if !ok {
		t.Fatal("одна BMS не синтезирована")
	}
	sum = 0
	for _, v := range one {
		sum += int(v)
	}
	if sum != 9900 || one[0] != 3300 || one[1] != 3300 {
		t.Fatalf("одна BMS: sum=%d cells=%v", sum, one)
	}
	// CellCount > 16 — запись отбрасывается (без паники).
	var bad bmscontract.Telemetry
	bad.CellCount = 17
	if _, ok := syntheticCells([]bmscontract.Telemetry{bad}); ok {
		t.Fatal("CellCount>16 не должен учитываться")
	}
}
