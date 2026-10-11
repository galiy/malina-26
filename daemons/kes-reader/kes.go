package main

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/galiy/malina-26/daemons/lib/device"
	"github.com/galiy/malina-26/daemons/lib/microart"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Ошибки идентификации.
var (
	errNoKes      = errors.New("не КЭС")
	errForeignMap = errors.New("MicroArt-устройство, но не КЭС")
)

// kesWindow — окно чтения устройства. Адрес — «адрес устройства» из карты
// (RAM-адрес + 0x1F0, см. docs/kes-memory.md §3); в образ кладём по RAM-адресу.
type kesWindow struct {
	devAddr uint16
	length  int    // байт данных (страница = length-1)
	ramBase uint16 // RAM-адрес, по которому раскладываем данные
}

// Окна чтения КЭС (подтверждены дизасмом mpptd, см. docs/legacy/kes-legacy-malina2.md §1b).
var kesWindows = []kesWindow{
	{devAddr: 0x0000, length: 190, ramBase: 0x0000}, // ID/версия/UID-область
	{devAddr: 0x02C2, length: 240, ramBase: 0x00D2}, // настройки/служебное
	{devAddr: 0x04B2, length: 240, ramBase: 0x02C2}, // измерения (Vc_PV, UID, Mode…)
	{devAddr: 0x05A3, length: 240, ramBase: 0x03B3}, // реле/ошибки/датчики
}

// kesInfo — идентификация КЭС (из ID-окна).
type kesInfo struct {
	Device byte // _Device (0x00) — должно быть 5
	UID    uint16
	FW     byte // версия ПО
	Mode   byte
	Model  string // определяется позже (200/100, 250/60, 200/60)
}

// trMicroart переводит ошибку кадрового слоя (lib/microart) в kesError.
func trMicroart(err error) error {
	if err == nil {
		return nil
	}
	var de *microart.DeviceError
	if errors.As(err, &de) {
		return &kesError{code: de.Code}
	}
	return err
}

// mapChar приводит «символьную» ячейку КЭС (Mode/Sign/MPP) к символу:
// 0x2A < c <= 0x7C → c, иначе 'U' (mapper из mpptd, docs/legacy/kes-legacy-malina2.md §1b).
func mapChar(b byte) byte {
	if b > 0x2A && b <= 0x7C {
		return b
	}
	return 'U'
}

// le16 читает 16-битное LE из образа по RAM-адресу.
func le16(img map[uint16]byte, addr uint16) uint16 {
	return uint16(img[addr]) | uint16(img[addr+1])<<8
}

// probeKes — проба: читаем 1 байт _Device (0x0000). 5 → КЭС; 3 → МАП (чужое).
func probeKes(p *transport.Port, echoTO, respTO time.Duration) (*kesInfo, error) {
	data, err := microart.Read(p, 0x0000, 0, echoTO, respTO) // страница 1 байт
	if err != nil {
		return nil, errNoKes
	}
	if len(data) == 0 {
		return nil, errNoKes
	}
	switch data[0] {
	case device.TypeKes:
		return &kesInfo{Device: data[0]}, nil
	case device.TypeMap:
		return nil, errForeignMap
	default:
		return nil, errNoKes
	}
}

// readAllWindows читает все окна КЭС в плоский образ (ключ — RAM-адрес).
func readAllWindows(p *transport.Port, echoTO, respTO time.Duration) (map[uint16]byte, error) {
	img := make(map[uint16]byte, 1024)
	for _, w := range kesWindows {
		data, err := microart.Read(p, w.devAddr, byte(w.length-1), echoTO, respTO)
		if err != nil {
			return nil, err
		}
		if len(data) < w.length {
			return nil, errNoKes
		}
		for i := 0; i < w.length; i++ {
			img[w.ramBase+uint16(i)] = data[i]
		}
	}
	return img, nil
}

// parseKes разбирает образ в идентификацию (UID/FW/Mode).
func parseKes(img map[uint16]byte) *kesInfo {
	return &kesInfo{
		Device: img[0x0000],
		FW:     img[0x0002],
		UID:    le16(img, 0x2C9),
		Mode:   mapChar(img[0x2C2]),
	}
}

// KesData — измерения КЭС (docs/kes-memory.md §4).
type KesData struct {
	UID      uint16
	Mode     byte
	Sign     byte
	MPP      byte
	VcPV     uint16
	IcPV     uint16
	VBat     uint16
	PPV      uint16
	POut     uint16
	PLoad    uint16
	PCurr    uint16
	ICh      uint16
	IOut     uint16
	TempInt  byte
	TempBat  byte
	PwrW     uint16
	PwrKW    uint16
	SignC0   byte
	SignC1   byte
	IExts0   uint16
	IExts1   uint16
	PExts0   uint16
	PExts1   uint16
	RelayC   byte
	RSErrSis byte
	Wind     uint16
}

// parseData заполняет измерения из образа (ключ — RAM-адрес).
func parseData(img map[uint16]byte) KesData {
	return KesData{
		UID:      le16(img, 0x2C9),
		Mode:     mapChar(img[0x2C2]),
		Sign:     mapChar(img[0x37A]),
		MPP:      mapChar(img[0x379]),
		VcPV:     le16(img, 0x2D9),
		IcPV:     le16(img, 0x2DF),
		VBat:     le16(img, 0x2E5),
		PPV:      le16(img, 0x2EB),
		POut:     le16(img, 0x2ED),
		PLoad:    le16(img, 0x2F5),
		PCurr:    le16(img, 0x2F9),
		ICh:      le16(img, 0x2FB),
		IOut:     le16(img, 0x2FF),
		TempInt:  img[0x31D],
		TempBat:  img[0x31E],
		PwrW:     le16(img, 0x39F),
		PwrKW:    le16(img, 0x3A1),
		SignC0:   img[0x404],
		SignC1:   img[0x405],
		IExts0:   le16(img, 0x406),
		IExts1:   le16(img, 0x408),
		PExts0:   le16(img, 0x40A),
		PExts1:   le16(img, 0x40C),
		RelayC:   img[0x3E3],
		RSErrSis: img[0x3B6],
		Wind:     le16(img, 0x3F8),
	}
}

// buildPayload — бинарный контракт слота КЭС (v1, LE). Формат — docs/kes-contract.md (план).
func buildPayload(meta contractMeta, d KesData) []byte {
	b := make([]byte, 80)
	copy(b[0:4], "KES1")
	b[4] = 1
	binary.LittleEndian.PutUint16(b[6:8], meta.UID)
	b[8] = d.Mode
	b[9] = d.Sign
	b[10] = d.MPP
	b[11] = d.RelayC
	b[12] = d.RSErrSis
	binary.LittleEndian.PutUint16(b[14:16], d.VcPV)
	binary.LittleEndian.PutUint16(b[16:18], d.IcPV)
	binary.LittleEndian.PutUint16(b[18:20], d.VBat)
	binary.LittleEndian.PutUint16(b[20:22], d.PPV)
	binary.LittleEndian.PutUint16(b[22:24], d.POut)
	binary.LittleEndian.PutUint16(b[24:26], d.PLoad)
	binary.LittleEndian.PutUint16(b[26:28], d.PCurr)
	binary.LittleEndian.PutUint16(b[28:30], d.ICh)
	binary.LittleEndian.PutUint16(b[30:32], d.IOut)
	b[32] = d.TempInt // однобайтовые ячейки (x-50)
	b[33] = d.TempBat
	binary.LittleEndian.PutUint16(b[34:36], d.PwrW)
	binary.LittleEndian.PutUint16(b[36:38], d.PwrKW)
	binary.LittleEndian.PutUint16(b[38:40], d.IExts0)
	binary.LittleEndian.PutUint16(b[40:42], d.IExts1)
	binary.LittleEndian.PutUint16(b[42:44], d.PExts0)
	binary.LittleEndian.PutUint16(b[44:46], d.PExts1)
	binary.LittleEndian.PutUint16(b[46:48], d.Wind)
	binary.LittleEndian.PutUint64(b[56:64], uint64(meta.LastSeen.Unix()))
	return b
}
