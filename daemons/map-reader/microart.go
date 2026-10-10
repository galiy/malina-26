package main

import (
	"errors"
	"fmt"
	"time"
)

// Минимальный размер прочитанной области, покрывающий сигнатуру и серийник:
// 0x00 (_VerPow), 0x02 (_VerPO), 0x07 (_DevOpt), 0x18/0x19 (серийник),
// 0x22/0x23 (резерв/буква) — читаем страницу 0x00..0x2F (48 байт).
const sigPage byte = 0x2F // P = len-1 (48 байт)

var (
	errNoMap    = errors.New("не МАП")
	errMapReply = errors.New("ошибочный ответ МАП")
)

// mapInfo — распознанные признаки МАП.
type mapInfo struct {
	Protocol     string
	Baud         int
	Model        string // буква модели: P, H, D, T
	Serial       uint16
	SerialLetter byte
	DevOpt       byte
	VerPO        byte
}

// parseCells проверяет сигнатуру МАП по байтам памяти и заполняет mapInfo.
// Адресация нативного протокола и Modbus совпадает (байтовые ячейки EEPROM).
func parseCells(data []byte, proto string, baud int) (*mapInfo, error) {
	if len(data) <= 0x24 {
		return nil, errNoMap
	}
	// F1: код изделия — 3 у МАП (у КЭС/MPPT здесь 0x05).
	if data[0x00] != 0x03 {
		return nil, errNoMap
	}
	verpo := data[0x02]
	devopt := data[0x07]
	// F2/F3: сигнатура семейства МАП. _DevOpt==2 (снят) не берём.
	if verpo == 0 {
		return nil, errNoMap
	}
	if devopt != 1 && devopt != 3 {
		return nil, errNoMap
	}
	// Модель: T — Титанатор; иначе D (есть 3ф-синхр.) / H (гибрид) / P.
	model := "P"
	switch {
	case devopt == 3:
		model = "T"
	case data[0x24]&0x01 != 0:
		model = "D"
	case data[0x01]&0x80 != 0:
		model = "H"
	}
	return &mapInfo{
		Protocol:     proto,
		Baud:         baud,
		Model:        model,
		Serial:       uint16(data[0x18]) | uint16(data[0x19])<<8,
		SerialLetter: data[0x23],
		DevOpt:       devopt,
		VerPO:        verpo,
	}, nil
}

// escByte добавляет байт в посылку с экранированием и учётом контрольной суммы.
func escByte(v byte, sum *byte) []byte {
	switch v {
	case 0x0A:
		*sum += 0xDB
		*sum += 0xDC
		return []byte{0xDB, 0xDC}
	case 0xDB:
		*sum += 0xDB
		*sum += 0xDD
		return []byte{0xDB, 0xDD}
	default:
		*sum += v
		return []byte{v}
	}
}

// buildReadRequest собирает посылку чтения страницы: 'r' P A1 A0 S [\n].
func buildReadRequest(addr uint16, page byte) []byte {
	var sum byte
	seq := make([]byte, 0, 8)
	for _, b := range []byte{'r', page, byte(addr >> 8), byte(addr)} {
		seq = append(seq, escByte(b, &sum)...)
	}
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}

// parseReply разбирает ответ Микроарт (o…/e…) и возвращает данные.
func parseReply(raw []byte, page byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errNoMap
	}
	if raw[0] == 'e' {
		// кадр ошибки: 'e' код S [\n]; проверим сумму (S-байт на позиции 2).
		if len(raw) >= 3 {
			var sum byte
			for k := 0; k <= 2; k++ {
				sum += raw[k]
			}
			if sum == 0 {
				return nil, fmt.Errorf("%w: код %d", errMapReply, raw[1])
			}
		}
		return nil, errMapReply
	}
	if raw[0] != 'o' {
		return nil, errNoMap
	}
	j := 1
	data := make([]byte, 0, int(page)+1)
	for i := 0; i <= int(page); i++ {
		if j >= len(raw) {
			return nil, errNoMap
		}
		if raw[j] == 0xDB {
			if j+1 >= len(raw) {
				return nil, errNoMap
			}
			switch raw[j+1] {
			case 0xDC:
				data = append(data, 0x0A)
			case 0xDD:
				data = append(data, 0xDB)
			default:
				return nil, errNoMap
			}
			j += 2
		} else {
			data = append(data, raw[j])
			j++
		}
	}
	if j >= len(raw) {
		return nil, errNoMap
	}
	var sum byte
	for k := 0; k <= j; k++ {
		sum += raw[k]
	}
	if sum != 0 {
		return nil, errNoMap
	}
	return data, nil
}

// microartRead выполняет чтение страницы по нативному протоколу с побайтовым эхо.
func microartRead(p *Port, addr uint16, page byte, echoTO, respTO time.Duration) ([]byte, error) {
	p.flush() // сбросить возможный мусор после сбоя — иначе линия «залипает»
	req := buildReadRequest(addr, page)
	for _, b := range req {
		if err := p.writeAll([]byte{b}); err != nil {
			return nil, err
		}
		e, ok := p.readByte(time.Now().Add(echoTO))
		if !ok || e != b {
			return nil, errNoMap
		}
	}
	raw, ok := p.readUntilNL(time.Now().Add(respTO), 600, true)
	if !ok || len(raw) == 0 {
		return nil, errNoMap
	}
	return parseReply(raw, page)
}

// probeMicroart — проба нативного протокола: читаем сигнатурную страницу.
func probeMicroart(p *Port, baud int, echoTO, respTO time.Duration) (*mapInfo, error) {
	data, err := microartRead(p, 0x0000, sigPage, echoTO, respTO)
	if err != nil {
		return nil, err
	}
	return parseCells(data, "microart", baud)
}

// buildWriteRequest собирает посылку записи страницы: 'w' P A1 A0 D0..Dp S [\n].
func buildWriteRequest(addr uint16, data []byte) []byte {
	var sum byte
	page := byte(len(data) - 1)
	seq := make([]byte, 0, len(data)+8)
	for _, b := range []byte{'w', page, byte(addr >> 8), byte(addr)} {
		seq = append(seq, escByte(b, &sum)...)
	}
	for _, d := range data {
		seq = append(seq, escByte(d, &sum)...)
	}
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}

// parseWriteReply разбирает ответ на запись: 'o' S [\n] (успех) или 'e' D S [\n].
// Сумма байт до S включительно должна быть нулевой (как в parseReply).
func parseWriteReply(raw []byte) error {
	if len(raw) == 0 {
		return errNoMap
	}
	switch raw[0] {
	case 'o':
		if len(raw) < 2 {
			return errNoMap
		}
		var sum byte
		for k := 0; k <= 1; k++ {
			sum += raw[k]
		}
		if sum != 0 {
			return errNoMap
		}
		return nil
	case 'e':
		if len(raw) < 3 {
			return errNoMap
		}
		var sum byte
		for k := 0; k <= 2; k++ {
			sum += raw[k]
		}
		if sum != 0 {
			return errNoMap
		}
		return &mapError{code: int(raw[1])}
	}
	return errNoMap
}

// microartWrite выполняет запись страницы по нативному протоколу с побайтовым эхо.
func microartWrite(p *Port, addr uint16, data []byte, echoTO, respTO time.Duration) error {
	p.flush()
	req := buildWriteRequest(addr, data)
	for _, b := range req {
		if err := p.writeAll([]byte{b}); err != nil {
			return err
		}
		e, ok := p.readByte(time.Now().Add(echoTO))
		if !ok || e != b {
			return errNoMap
		}
	}
	raw, ok := p.readUntilNL(time.Now().Add(respTO), 16, true)
	if !ok || len(raw) == 0 {
		return errNoMap
	}
	return parseWriteReply(raw)
}
