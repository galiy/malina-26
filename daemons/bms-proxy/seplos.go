package main

import (
	"encoding/binary"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// Сборка SEPLOS INFO (75 Б) по образцу seplos_bms_server.py (docs/bms-proxy.md
// §10). Поля берём из бинарного контракта bmscontract (docs/bms-proxy.md §5).
//
// Формат INFO (67 байт ядра + 8 хвоста = 75), карта 16S_V20
// (energybms/protocol-485.md §5):
//
//	0     dataflag = 0x00
//	1     командная группа/адрес пака = 0x01 (как в эталоне)
//	2     число ячеек (u8)
//	3..   напряжения ячеек (u16 мВ)
//	35    число температур (u8) — 6 по карте
//	36..  температуры (u16 0.1 K)
//	ток (s16 0.01 А; SEPLOS: разряд «−»), общее/клеммное напряжение (u16 0.01 В),
//	остаточная/полная ёмкость (u16 0.01 А·ч), SOC/SOH (u16 0.1 %), циклы (u16),
//	хвост 8 Б.
const (
	seplosTail  = 8
	seplosTemps = 6 // число температур в карте 16S_V20 → ядро ровно 67 Б
)

// buildSeplosInfo собирает SEPLOS INFO из распакованного контракта телеметрии.
// Длина зависит от числа ячеек/температур: 31 + 2*ncells + 2*ntemps (для
// контракта bmscontract ≤ 16 ячеек, ≤ 8 темп.). Точная длина уходит в поле LEN
// кадра (docs/bms-proxy.md §10).
func buildSeplosInfo(t *bmscontract.Telemetry) []byte {
	b := make([]byte, 0, 80)

	// заголовок: dataflag=0x00, командная группа/адрес пака = 0x01 (как в эталоне)
	b = append(b, 0x00, 0x01)

	// число ячеек (1..16). Если контракт не заполнил CellCount (0) — считаем по
	// ненулевым CellsMV (эталон при n<=0 берёт len(cells)).
	n := int(t.CellCount)
	if n > bmscontract.MaxCells {
		n = bmscontract.MaxCells
	}
	if n <= 0 {
		for i := 0; i < bmscontract.MaxCells; i++ {
			if t.CellsMV[i] != 0 {
				n = i + 1
			}
		}
	}
	b = append(b, byte(n&0xFF))

	// напряжения ячеек (мВ, u16 big-endian)
	for i := 0; i < n; i++ {
		b = binary.BigEndian.AppendUint16(b, t.CellsMV[i])
	}

	// число температур и температуры (0.1 K). Карта 16S_V20 — 6 датчиков;
	// ограничиваем, чтобы ядро INFO осталось 67 Б (а весь блок — 75 Б).
	tc := int(t.TempCount)
	if tc > seplosTemps {
		tc = seplosTemps
	}
	b = append(b, byte(tc&0xFF))
	for i := 0; i < tc; i++ {
		// контракт: 0.1 °C; SEPLOS: 0.1 K = T*10 + 2731 (273.1 → 2731 дес. К/10)
		b = binary.BigEndian.AppendUint16(b, uint16(int32(t.TempsDC[i])+2731))
	}

	// ток: контракт 0.01 А, «разряд +»; SEPLOS: разряд «−» → знак инвертируем
	b = binary.BigEndian.AppendUint16(b, uint16(-t.CurrentDA)&0xFFFF)

	// общее напряжение (0.01 В). Если TotalCV не заполнен (0) — считаем суммой
	// ячеек (эталон: total_v = tv or sum(cells)); сумма мВ → 0.01 В = /10.
	totalCV := t.TotalCV
	if totalCV == 0 {
		var sum int
		for i := 0; i < n; i++ {
			sum += int(t.CellsMV[i])
		}
		totalCV = uint16(sum / 10)
	}
	b = binary.BigEndian.AppendUint16(b, totalCV)

	// остаточная ёмкость (0.001 А·ч → 0.01 А·ч: /10)
	b = binary.BigEndian.AppendUint16(b, uint16(t.RemainingMAh/10))

	// field count / customerp — 0x00 (как в эталоне seplos_bms_server.py)
	b = append(b, 0x00)

	// Полная ёмкость (0.01 А·ч). Берём Rated, при отсутствии — Cap (у AntBMS
	// rated=0, а cap заполнен), как делает эталон (capacity_ah).
	capMAh := t.RatedMAh
	if capMAh == 0 {
		capMAh = t.CapMAh
	}
	capC := uint16(capMAh / 10)
	b = binary.BigEndian.AppendUint16(b, capC)

	// SOC (0.1 %)
	b = binary.BigEndian.AppendUint16(b, t.SocDP)

	// номинал ≈ полная ёмкость
	b = binary.BigEndian.AppendUint16(b, capC)

	// циклы
	b = binary.BigEndian.AppendUint16(b, t.Cycles)

	// SOH (0.1 %). У AntBMS soh=0 (нет данных) — эталон подставляет 100 %;
	// иначе инвертор может счесть пакет невалидным.
	soh := t.SohDP
	if soh == 0 {
		soh = 1000
	}
	b = binary.BigEndian.AppendUint16(b, soh)

	// напряжение клемм (0.01 В). У AntBMS port=0 — берём общее напряжение
	// (эталон: port_v = port_voltage_v or total_v).
	portCV := t.PortCV
	if portCV == 0 {
		portCV = totalCV
	}
	b = binary.BigEndian.AppendUint16(b, portCV)

	// хвост
	b = append(b, make([]byte, seplosTail)...)

	return b
}
