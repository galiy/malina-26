package main

// Modbus RTU (Vektor/Growatt): валидация запросов и ответ с напряжениями ячеек.
// docs/bms-proxy.md §5.1.

import (
	"encoding/binary"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

const (
	modbusFnReadHolding = 0x03

	// Vektor/Growatt: 0x0071..0x0080 — напряжения ячеек 1..16, по 1 мВ на
	// регистр. 0x0015..0x0029 — блок состояния (SOC, U, I, T, лимиты, ёмкости,
	// SOH, CV, warning, cell max/min). Отвечаем только на эти два блока.
	vektorCellReg      = 0x0071
	vektorCellQty      = 16
	vektorCellCount    = 16
	vektorStatusReg    = 0x0015
	vektorStatusQty    = 21 // новая прошивка MAP: 0x0015..0x0029
	vektorStatusQtyAlt = 16 // старая прошивка MAP: 0x0015..0x0024
	vektorStatusCount  = 21
)

// modbusCRC возвращает CRC16 Modbus (poly 0xA001, init 0xFFFF). Для полного
// корректного кадра результат равен 0.
func modbusCRC(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, c := range b {
		crc ^= uint16(c)
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

// validModbusRequest — обёртка findModbusRequest (адрес и длина).
func validModbusRequest(buf []byte) (addr int, frameLen int, ok bool) {
	_, addr, frameLen, ok = findModbusRequest(buf)
	return addr, frameLen, ok
}

// findModbusRequest ищет в buf первый валидный Modbus RTU-запрос:
// slave address 1..16, любая функция, корректный CRC16 и согласованная длина.
// Возвращает смещение начала, адрес и длину найденного кадра.
func findModbusRequest(buf []byte) (start int, addr int, frameLen int, ok bool) {
	for i := 0; i+4 <= len(buf); i++ {
		a := int(buf[i])
		if a < 1 || a > 16 {
			continue
		}
		var lengths []int
		switch fn := buf[i+1]; {
		case fn >= 0x01 && fn <= 0x06:
			lengths = []int{8}
		case fn == 0x0F || fn == 0x10:
			if i+7 > len(buf) {
				continue
			}
			bc := int(buf[i+6])
			l := 9 + bc
			if l >= 9 && l <= 256 {
				lengths = []int{l}
			}
		default:
			// Неизвестная функция: пробуем правдоподобные длины; CRC отсекает мусор.
			max := len(buf) - i
			if max > 256 {
				max = 256
			}
			for l := 8; l <= max; l++ {
				lengths = append(lengths, l)
			}
		}
		for _, l := range lengths {
			if i+l > len(buf) {
				continue
			}
			if modbusCRC(buf[i:i+l]) == 0 {
				return i, a, l, true
			}
		}
	}
	return 0, 0, 0, false
}

// isVektorCellRequest — это запрос `0x03` (read holding) блока 0x0071..0x0080,
// то есть напряжений 16 ячеек. start/frameLen получены из findModbusRequest.
func isVektorCellRequest(buf []byte, start, frameLen int) bool {
	if frameLen != 8 || start < 0 || start+frameLen > len(buf) {
		return false
	}
	f := buf[start : start+frameLen]
	return f[1] == modbusFnReadHolding &&
		binary.BigEndian.Uint16(f[2:4]) == vektorCellReg &&
		binary.BigEndian.Uint16(f[4:6]) == vektorCellQty
}

// isVektorStatusRequest — это запрос `0x03` блока 0x0015..0x0029 (21 регистр).
func isVektorStatusRequest(buf []byte, start, frameLen int) bool {
	if frameLen != 8 || start < 0 || start+frameLen > len(buf) {
		return false
	}
	f := buf[start : start+frameLen]
	if f[1] != modbusFnReadHolding || binary.BigEndian.Uint16(f[2:4]) != vektorStatusReg {
		return false
	}
	qty := binary.BigEndian.Uint16(f[4:6])
	// Старая прошивка MAP читает 16 регистров (0x0015..0x0024), новая — 21
	// (0x0015..0x0029). Отвечаем ровно запрошенным количеством.
	return qty == vektorStatusQtyAlt || qty == vektorStatusQty
}

// vektorRequestQty — количество регистров из запроса (bytes 4..5).
func vektorRequestQty(buf []byte, start int) int {
	if start < 0 || start+6 > len(buf) {
		return 0
	}
	return int(binary.BigEndian.Uint16(buf[start+4 : start+6]))
}

// isVektorKnownRequest — запрос одного из блоков, на которые отвечает прокси.
func isVektorKnownRequest(buf []byte, start, frameLen int) bool {
	return isVektorCellRequest(buf, start, frameLen) ||
		isVektorStatusRequest(buf, start, frameLen)
}

// buildVektorRegisterResponse собирает ответ Modbus RTU:
// [slave][0x03][2n][n×u16 BE][CRC lo][CRC hi].
func buildVektorRegisterResponse(slave byte, regs []uint16) []byte {
	n := 3 + 2*len(regs)
	b := make([]byte, n+2)
	b[0] = slave
	b[1] = modbusFnReadHolding
	b[2] = byte(2 * len(regs))
	for i, v := range regs {
		binary.BigEndian.PutUint16(b[3+2*i:], v)
	}
	crc := modbusCRC(b[:n])
	b[n] = byte(crc)
	b[n+1] = byte(crc >> 8)
	return b
}

// buildVektorCellResponse — ответ на блок напряжений 16 ячеек.
func buildVektorCellResponse(slave byte, cells [vektorCellCount]uint16) []byte {
	return buildVektorRegisterResponse(slave, cells[:])
}

// vektorCellReply читает активные BMS из shm и собирает ответ на запрос
// напряжений ячеек. nil — активных BMS нет (молчим).
func vektorCellReply(shmPath string, slave byte) []byte {
	ats := readActiveTelemetries(readShm(shmPath), time.Now())
	snap, ok := buildVektorSnapshot(ats)
	if !ok {
		return nil
	}
	return buildVektorCellResponse(slave, snap.Cells)
}

// validCells проверяет, что BMS пригодна для синтеза: cell_count 1..16 и все
// заявленные ячейки непустые (> 0 мВ).
func validCells(t *bmscontract.Telemetry) (count int, ok bool) {
	count = int(t.CellCount)
	if count <= 0 || count > bmscontract.MaxCells {
		return 0, false
	}
	for c := 0; c < count; c++ {
		if t.CellsMV[c] == 0 {
			return 0, false // неинициализированная/битая ячейка
		}
	}
	return count, true
}

// packStats — общие данные для синтеза 16 ячеек.
type packStats struct {
	cells       [vektorCellCount]uint16
	sumPacks    int // сумма P по BMS
	n           int // число учтённых BMS
	minMV       int
	maxMV       int
	totalCellMV int // сумма всех фактических ячеек (для среднего на ячейку)
	totalCells  int
}

// computePackStats считает синтетические ячейки:
//
//  1. P — сумма всех ячеек каждой BMS (готовое поле TotalCV не используется);
//  2. PAvg — среднее P по всем активным BMS;
//  3. min/max — глобальные минимум и максимум напряжения ячейки;
//  4. ячейка 1 = min, ячейка 2 = max, остальные 14 делят остаток так, чтобы
//     сумма всех 16 равнялась PAvg (округлённому до мВ).
func computePackStats(tels []bmscontract.Telemetry) (packStats, bool) {
	var st packStats
	st.minMV = 0xFFFF
	for i := range tels {
		t := &tels[i]
		count, ok := validCells(t)
		if !ok {
			continue
		}
		pack := 0
		for c := 0; c < count; c++ {
			mv := int(t.CellsMV[c])
			pack += mv
			st.totalCellMV += mv
			if mv < st.minMV {
				st.minMV = mv
			}
			if mv > st.maxMV {
				st.maxMV = mv
			}
		}
		st.totalCells += count
		st.sumPacks += pack
		st.n++
	}
	if st.n == 0 {
		return st, false
	}
	target := (st.sumPacks + st.n/2) / st.n // PAvg с округлением до целого мВ
	rest := target - st.minMV - st.maxMV
	if rest < 0 {
		// min+max уже больше среднего пакета — синтезировать нельзя.
		return st, false
	}
	base := rest / (vektorCellCount - 2)
	extra := rest % (vektorCellCount - 2)
	st.cells[0] = uint16(st.minMV)
	st.cells[1] = uint16(st.maxMV)
	for i := 2; i < vektorCellCount; i++ {
		v := base
		if i-2 < extra {
			v++
		}
		st.cells[i] = uint16(v)
	}
	return st, true
}

// syntheticCells — обёртка для тестов/простого вызова.
func syntheticCells(tels []bmscontract.Telemetry) ([vektorCellCount]uint16, bool) {
	st, ok := computePackStats(tels)
	return st.cells, ok
}
