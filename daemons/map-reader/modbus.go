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
	p.flush()                         // сбросить возможный мусор после сбоя
	time.Sleep(interFrameGap(p.baud)) // межкадровый интервал t3.5 (Modbus-RTU)
	req := []byte{byte(unit), 0x03, byte(addr >> 8), byte(addr), byte(count >> 8), byte(count)}
	c := crc16(req)
	req = append(req, byte(c), byte(c>>8))
	if err := p.writeAll(req); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(respTO)
	resp := make([]byte, 0, 4+int(count)*2+2)
	for len(resp) < 260 && time.Now().Before(deadline) {
		b, ok := p.readByte(deadline)
		if !ok {
			break
		}
		resp = append(resp, b)
		if len(resp) >= 3 {
			if resp[1]&0x80 != 0 {
				if len(resp) >= 5 {
					break
				}
			} else if len(resp) >= 3+int(resp[2])+2 {
				break
			}
		}
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
	return parseCells(data, "modbus", p.baud)
}
