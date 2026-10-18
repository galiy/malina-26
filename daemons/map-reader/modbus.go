package main

import (
	"errors"
	"time"
)

// crc16 — CRC Modbus (полином 0xA001).
func crc16(d []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range d {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

var errModbusExc = errors.New("modbus exception")

// interFrameGap — межкадровый интервал t3.5 Modbus-RTU: 3.5 символа тишины
// (символ = 11 бит). Для скоростей >19200 — фиксированные 1.75 мс.
func interFrameGap(baud int) time.Duration {
	if baud <= 0 {
		return 2 * time.Millisecond
	}
	if baud > 19200 {
		return 1750 * time.Microsecond
	}
	ns := 3.5 * 11.0 / float64(baud) * 1e9
	return time.Duration(ns)
}

// modbusReadRegisters читает count регистров (ровно 2*count байт) у unit.
// Возвращает ошибку при кадре исключения, неверном CRC или если устройство
// вернуло не то число байт, что запрошено.
func modbusReadRegisters(p *Port, unit int, addr, count uint16, respTO time.Duration) ([]byte, error) {
	p.Flush()                           // сбросить возможный мусор после сбоя
	time.Sleep(interFrameGap(p.Baud())) // межкадровый интервал t3.5 (Modbus-RTU)
	req := []byte{byte(unit), 0x03, byte(addr >> 8), byte(addr), byte(count >> 8), byte(count)}
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))
	if err := p.WriteAll(req); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(respTO)
	resp := make([]byte, 0, 4+int(count)*2+2)
	var tmp [260]byte
	need := 0 // полная длина кадра, как только известен заголовок
	for len(resp) < 260 && time.Now().Before(deadline) {
		readN := len(tmp)
		if need > 0 {
			if r := need - len(resp); r < readN {
				readN = r
			}
			if readN <= 0 {
				break
			}
		}
		n, err := p.Read(tmp[:readN], deadline)
		if n > 0 {
			resp = append(resp, tmp[:n]...)
			if need == 0 && len(resp) >= 3 {
				if resp[1]&0x80 != 0 {
					need = 5 // кадр исключения
				} else {
					need = 3 + int(resp[2]) + 2
				}
			}
			if need > 0 && len(resp) >= need {
				resp = resp[:need] // отсечь возможный хвост за кадром
				break
			}
		}
		if err != nil {
			break
		}
	}
	if need > 0 && len(resp) > need {
		resp = resp[:need]
	}
	if len(resp) < 5 || resp[0] != byte(unit) {
		return nil, errNoMap
	}
	if got := uint16(resp[len(resp)-2]) | uint16(resp[len(resp)-1])<<8; crc16(resp[:len(resp)-2]) != got {
		return nil, errNoMap
	}
	if resp[1]&0x80 != 0 {
		return nil, errModbusExc
	}
	if resp[1] != 0x03 {
		return nil, errNoMap
	}
	if int(resp[2]) != int(count)*2 {
		return nil, errNoMap
	}
	return resp[3 : 3+int(resp[2])], nil
}

// probeModbus — сигнатура МАП по блоку 0x00..0x2F для заданного unit.
func probeModbus(p *Port, unit int, respTO time.Duration) (*mapInfo, error) {
	data, err := modbusReadRegisters(p, unit, 0x0000, 0x18, respTO)
	if err != nil {
		return nil, err
	}
	return parseCells(data, "modbus", p.Baud())
}

// modbusWriteExchange отправляет кадр записи и проверяет эхо-ответ.
// Успешный ответ: тот же fc/адрес; исключение (старший бит fc) — modbusError.
func modbusWriteExchange(p *Port, req []byte, want int, respTO time.Duration) error {
	p.Flush()
	time.Sleep(interFrameGap(p.Baud()))
	if err := p.WriteAll(req); err != nil {
		return err
	}
	unit := req[0]
	deadline := time.Now().Add(respTO)
	buf := make([]byte, 0, 16)
	var tmp [260]byte
	for len(buf) < want && time.Now().Before(deadline) {
		readN := len(tmp)
		if r := want - len(buf); r < readN {
			readN = r
		}
		n, err := p.Read(tmp[:readN], deadline)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) >= 2 && buf[1]&0x80 != 0 && len(buf) >= 5 {
				break // кадр исключения — 5 байт
			}
			if len(buf) >= want {
				break
			}
		}
		if err != nil {
			break
		}
	}
	// Кадр исключения — ровно 5 байт; хвост за ним не должен попадать в CRC.
	if len(buf) >= 5 && buf[1]&0x80 != 0 {
		buf = buf[:5]
	}
	if len(buf) < 5 || buf[0] != unit {
		return errNoMap
	}
	if got := uint16(buf[len(buf)-2]) | uint16(buf[len(buf)-1])<<8; crc16(buf[:len(buf)-2]) != got {
		return errNoMap
	}
	if buf[1]&0x80 != 0 {
		return &modbusError{code: int(buf[2])}
	}
	if len(buf) != want {
		return errNoMap
	}
	return nil
}

// modbusWriteSingle — запись одного слова (fc 0x06). Для байта-команды значение
// кладётся в младший байт (старший = 0).
func modbusWriteSingle(p *Port, unit int, addr, val uint16, respTO time.Duration) error {
	req := []byte{byte(unit), 0x06, byte(addr >> 8), byte(addr), byte(val >> 8), byte(val)}
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))
	return modbusWriteExchange(p, req, 8, respTO)
}

// modbusWriteBlock — запись блока (fc 0x10). Только чётный адрес и чётная длина
// (ограничение до выяснения байтового порядка — docs/map-write.md §6).
// Байты пакуются в слова: чётный адрес — старший байт слова.
func modbusWriteBlock(p *Port, unit int, addr uint16, data []byte, respTO time.Duration) error {
	if addr%2 != 0 || len(data)%2 != 0 {
		return errUnsupported
	}
	n := len(data)
	words := n / 2
	req := []byte{byte(unit), 0x10, byte(addr >> 8), byte(addr), byte(words >> 8), byte(words), byte(n)}
	req = append(req, data...) // data[0] — старший байт первого слова
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))
	return modbusWriteExchange(p, req, 8, respTO)
}
