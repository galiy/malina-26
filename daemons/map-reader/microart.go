package main

import (
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/device"
	"github.com/galiy/malina-26/daemons/lib/microart"
)

// Минимальный размер прочитанной области, покрывающий сигнатуру и серийник:
// 0x00 (_VerPow), 0x02 (_VerPO), 0x07 (_DevOpt), 0x18/0x19 (серийник),
// 0x22/0x23 (резерв/буква) — читаем страницу 0x00..0x2F (48 байт).
const sigPage byte = 0x2F // P = len-1 (48 байт)

var (
	errNoMap    = errors.New("не МАП")
	errMapReply = errors.New("ошибочный ответ МАП")

	// errForeignMicroArt — устройство ответило валидным кадром MicroArt, но это не МАП
	// (например, КЭС/MPPT: _Device == 5). Значит порт занят чужим MicroArt-устройством:
	// Modbus туда слать нельзя, порт надо быстро отпустить (и не долбить его повторно).
	errForeignMicroArt = errors.New("MicroArt-устройство, но не МАП")
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

// translateMicroartErr переводит ошибки общего кадрового слоя (lib/microart) в
// локальные сентинелы/ошибки МАП.
func translateMicroartErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, microart.ErrNoDevice) {
		return errNoMap
	}
	var de *microart.DeviceError
	if errors.As(err, &de) {
		return &mapError{code: de.Code}
	}
	if errors.Is(err, microart.ErrBadReply) {
		return errMapReply
	}
	return err
}

// parseCells проверяет сигнатуру МАП по байтам памяти и заполняет mapInfo.
// Адресация нативного протокола и Modbus совпадает (байтовые ячейки EEPROM).
//
// Базовый критерий — _Device (0x00) == 3 (у КЭС/MPPT здесь 0x05). Прочие признаки
// (_VerPO/_DevOpt) устройство больше НЕ отбраковывают — они лишь уточняют модель:
// иначе валидный МАП мог попасть под отказ и уйти в медленный перебор Modbus.
func parseCells(data []byte, proto string, baud int) (*mapInfo, error) {
	if len(data) == 0 {
		return nil, errNoMap
	}
	// F1: код изделия — 3 у МАП; иной код (напр. 5 у КЭС/MPPT) — чужое MicroArt-устройство.
	if data[0x00] != device.TypeMap {
		return nil, errForeignMicroArt
	}
	if len(data) <= 0x24 {
		return nil, errNoMap
	}
	verpo := data[0x02]
	devopt := data[0x07]
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

// microartRead — чтение страницы кадровым слоем MicroArt (см. lib/microart).
func microartRead(p *Port, addr uint16, page byte, echoTO, respTO time.Duration) ([]byte, error) {
	data, err := microart.Read(p, addr, page, echoTO, respTO)
	if err != nil {
		return nil, translateMicroartErr(err)
	}
	return data, nil
}

// microartWrite — запись страницы кадровым слоем MicroArt.
func microartWrite(p *Port, addr uint16, data []byte, echoTO, respTO time.Duration) error {
	return translateMicroartErr(microart.Write(p, addr, data, echoTO, respTO))
}

// parseWriteReply разбирает ответ на запись (локальная обёртка над lib/microart).
func parseWriteReply(raw []byte) error {
	return translateMicroartErr(microart.ParseWriteReply(raw))
}

// probeMicroart — проба нативного протокола: читаем сигнатурную страницу.
func probeMicroart(p *Port, baud int, echoTO, respTO time.Duration) (*mapInfo, error) {
	data, err := microartRead(p, 0x0000, sigPage, echoTO, respTO)
	if err != nil {
		return nil, err
	}
	return parseCells(data, "microart", baud)
}
