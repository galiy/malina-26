// Package microart — кадровый уровень протокола Микроарт (общий для МАП и КЭС/MPPT):
// сборка/разбор посылок чтения и записи, экранирование, контрольная сумма, эхо.
//
// Протокол (см. docs/microart-protocol.md — канонический, а также
// docs/kes-memory.md, docs/map.md):
//
//	чтение : 'r' P A1 A0 S [\n]
//	запись : 'w' P A1 A0 D0..Dp S [\n]
//	ответ  : 'o' D0..Dp S [\n]  (успех)  |  'e' код S [\n]  (ошибка)
//	S = сумма всех байт до S включительно по mod 256 == 0
//	экранирование: 0x0A -> DB DC, 0xDB -> DB DD
//	каждый отправленный байт подтверждается эхом.
//
// Пакет не знает про конкретное устройство: разбор «сигнатуры» (МАП/КЭС)
// остаётся в демоне.
package microart

import (
	"errors"
	"fmt"
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Ошибки уровня протокола.
var (
	// ErrNoDevice — валидного ответа устройства нет (таймаут/мусор/битый кадр).
	ErrNoDevice = errors.New("нет ответа устройства")
	// ErrBadReply — ответ получен, но некорректен (для не-«e» случаев).
	ErrBadReply = errors.New("ошибочный ответ устройства")
)

// DeviceError — устройство ответило кадром ошибки 'e' с кодом.
type DeviceError struct{ Code int }

func (e *DeviceError) Error() string {
	return fmt.Sprintf("ошибочный ответ устройства: код %d", e.Code)
}

// Escape добавляет байт в посылку с экранированием и учётом контрольной суммы.
func Escape(v byte, sum *byte) []byte {
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

// BuildReadRequest собирает посылку чтения страницы: 'r' P A1 A0 S [\n].
func BuildReadRequest(addr uint16, page byte) []byte {
	var sum byte
	seq := make([]byte, 0, 8)
	for _, b := range []byte{'r', page, byte(addr >> 8), byte(addr)} {
		seq = append(seq, Escape(b, &sum)...)
	}
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}

// BuildWriteRequest собирает посылку записи страницы: 'w' P A1 A0 D0..Dp S [\n].
func BuildWriteRequest(addr uint16, data []byte) []byte {
	var sum byte
	page := byte(len(data) - 1)
	seq := make([]byte, 0, len(data)+8)
	for _, b := range []byte{'w', page, byte(addr >> 8), byte(addr)} {
		seq = append(seq, Escape(b, &sum)...)
	}
	for _, d := range data {
		seq = append(seq, Escape(d, &sum)...)
	}
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}

// ParseReply разбирает ответ Микроарт (o…/e…) и возвращает данные (page+1 байт).
func ParseReply(raw []byte, page byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, ErrNoDevice
	}
	if raw[0] == 'e' {
		// кадр ошибки: 'e' код S [\n]; проверим сумму (S-байт на позиции 2).
		if len(raw) >= 3 {
			var sum byte
			for k := 0; k <= 2; k++ {
				sum += raw[k]
			}
			if sum == 0 {
				return nil, &DeviceError{Code: int(raw[1])}
			}
		}
		return nil, ErrBadReply
	}
	if raw[0] != 'o' {
		return nil, ErrNoDevice
	}
	j := 1
	data := make([]byte, 0, int(page)+1)
	for i := 0; i <= int(page); i++ {
		if j >= len(raw) {
			return nil, ErrNoDevice
		}
		if raw[j] == 0xDB {
			if j+1 >= len(raw) {
				return nil, ErrNoDevice
			}
			switch raw[j+1] {
			case 0xDC:
				data = append(data, 0x0A)
			case 0xDD:
				data = append(data, 0xDB)
			default:
				return nil, ErrNoDevice
			}
			j += 2
		} else {
			data = append(data, raw[j])
			j++
		}
	}
	if j >= len(raw) {
		return nil, ErrNoDevice
	}
	var sum byte
	for k := 0; k <= j; k++ {
		sum += raw[k]
	}
	if sum != 0 {
		return nil, ErrNoDevice
	}
	return data, nil
}

// ParseWriteReply разбирает ответ на запись: 'o' S [\n] (успех) или 'e' D S [\n].
func ParseWriteReply(raw []byte) error {
	if len(raw) == 0 {
		return ErrNoDevice
	}
	switch raw[0] {
	case 'o':
		if len(raw) < 2 {
			return ErrNoDevice
		}
		var sum byte
		for k := 0; k <= 1; k++ {
			sum += raw[k]
		}
		if sum != 0 {
			return ErrNoDevice
		}
		return nil
	case 'e':
		if len(raw) < 3 {
			return ErrNoDevice
		}
		var sum byte
		for k := 0; k <= 2; k++ {
			sum += raw[k]
		}
		if sum != 0 {
			return ErrNoDevice
		}
		return &DeviceError{Code: int(raw[1])}
	}
	return ErrNoDevice
}

// Read выполняет чтение страницы по протоколу Микроарт с побайтовым эхо.
func Read(p *transport.Port, addr uint16, page byte, echoTO, respTO time.Duration) ([]byte, error) {
	p.Flush() // сбросить возможный мусор после сбоя — иначе линия «залипает»
	req := BuildReadRequest(addr, page)
	for _, b := range req {
		if err := p.WriteAll([]byte{b}); err != nil {
			return nil, err
		}
		e, ok := p.ReadOne(time.Now().Add(echoTO))
		if !ok || e != b {
			return nil, ErrNoDevice
		}
	}
	raw, _ := p.ReadUntilNL(time.Now().Add(respTO), 600, true)
	if len(raw) == 0 {
		return nil, ErrNoDevice
	}
	return ParseReply(raw, page)
}

// Write выполняет запись страницы по протоколу Микроарт с побайтовым эхо.
func Write(p *transport.Port, addr uint16, data []byte, echoTO, respTO time.Duration) error {
	p.Flush()
	req := BuildWriteRequest(addr, data)
	for _, b := range req {
		if err := p.WriteAll([]byte{b}); err != nil {
			return err
		}
		e, ok := p.ReadOne(time.Now().Add(echoTO))
		if !ok || e != b {
			return ErrNoDevice
		}
	}
	raw, _ := p.ReadUntilNL(time.Now().Add(respTO), 16, true)
	if len(raw) == 0 {
		return ErrNoDevice
	}
	return ParseWriteReply(raw)
}

// --- Сторона устройства (для эмулятора) ---

// Request — разобранный кадр запроса от ведущего (клиента).
type Request struct {
	Cmd  byte   // 'r' | 'w'
	Page byte   // число байт данных минус 1
	Addr uint16 // адрес
	Data []byte // данные (для 'w')
}

// Unescape снимает экранирование Микроарт (0xDB 0xDC → 0x0A, 0xDB 0xDD → 0xDB).
func Unescape(b []byte) ([]byte, error) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == 0xDB {
			if i+1 >= len(b) {
				return nil, ErrNoDevice
			}
			switch b[i+1] {
			case 0xDC:
				out = append(out, 0x0A)
			case 0xDD:
				out = append(out, 0xDB)
			default:
				return nil, ErrNoDevice
			}
			i++
			continue
		}
		out = append(out, b[i])
	}
	return out, nil
}

// ParseRequestFrame разбирает принятый от клиента кадр. CRC (последний байт) НЕ
// экранируется; завершающий '\n' может отсутствовать (когда CRC == 0x0A).
func ParseRequestFrame(raw []byte) (Request, error) {
	if len(raw) == 0 {
		return Request{}, ErrNoDevice
	}
	// Вариант A: последний байт — терминатор '\n'.
	if raw[len(raw)-1] == 0x0A {
		if req, ok := parseReqBody(raw[:len(raw)-1]); ok {
			return req, nil
		}
	}
	// Вариант B: терминатора нет, CRC == 0x0A.
	if req, ok := parseReqBody(raw); ok {
		return req, nil
	}
	return Request{}, ErrBadReply
}

// parseReqBody: body = [экранированные данные][CRC]; сумма по wire-байтам == 0.
func parseReqBody(body []byte) (Request, bool) {
	if len(body) < 5 {
		return Request{}, false
	}
	var sum byte
	for _, b := range body {
		sum += b
	}
	if sum != 0 {
		return Request{}, false
	}
	// CRC — последний байт (не экранируется), остальное — экранированные данные.
	ck := body[len(body)-1]
	var wsum byte
	for _, b := range body[:len(body)-1] {
		wsum += b
	}
	if byte(0-wsum) != ck {
		return Request{}, false
	}
	logical, err := Unescape(body[:len(body)-1])
	if err != nil || len(logical) < 4 {
		return Request{}, false
	}
	req := Request{Cmd: logical[0], Page: logical[1], Addr: uint16(logical[2])<<8 | uint16(logical[3])}
	if req.Cmd == 'w' {
		want := int(req.Page) + 1
		if len(logical) < 4+want {
			return Request{}, false
		}
		req.Data = append([]byte(nil), logical[4:4+want]...)
	}
	return req, true
}

// BuildReply собирает ответ данных: 'o' данные S [\n].
func BuildReply(data []byte) []byte {
	var sum byte
	seq := make([]byte, 0, len(data)+4)
	seq = append(seq, Escape('o', &sum)...)
	for _, d := range data {
		seq = append(seq, Escape(d, &sum)...)
	}
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}

// BuildErrorReply собирает ответ ошибки: 'e' код S [\n].
func BuildErrorReply(code byte) []byte {
	var sum byte
	seq := append(Escape('e', &sum), Escape(code, &sum)...)
	ck := byte(0 - sum)
	seq = append(seq, ck)
	if ck != 0x0A {
		seq = append(seq, 0x0A)
	}
	return seq
}
