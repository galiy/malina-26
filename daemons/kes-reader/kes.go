package main

import (
	"encoding/binary"
	"errors"
	"fmt"
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

// Окна чтения КЭС — оптимальная разбивка (министрация числа страниц MicroArt, каждая
// ≤240 Б, без слияния удалённых кластеров; см. docs/kes-reader.md §7.7 — как у МАП).
// Покрывают все адреса, нужные контракту, без «дыр» (старая разбивка теряла 0xDE0..0xDFF и
// тянула лишние байты). devAddr для RAM = RAM + 0x1F0; ID-окно читается «как есть».
//
// Особый случай — серийник: он хранится в EEPROM по адресу 0x2C9/0x2CA
// (docs/kes-memory.md §2), а не в RAM. Но RAM-адрес 0x2C9 совпадает с началом окна
// измерений, поэтому серийник НЕЛЬЗЯ класть в img по ключу 0x2C9 (там будет измерение,
// а не серийник). Читаем его отдельным окном по devAddr 0x2C9 (Адрес «как есть», EEPROM)
// и раскладываем по резервному ключу вне RAM-диапазона — kesSerialImgKey.
var kesWindows = []kesWindow{
	{devAddr: 0x0000, length: 156, ramBase: 0x0000},       // ID/паспорт (EEPROM 0x000..0x09B, read as-is)
	{devAddr: 0x0400, length: 185, ramBase: 0x0210},       // настройки + Mode(0x2C2) (RAM 0x210..0x2C8)
	{devAddr: 0x04B9, length: 240, ramBase: 0x02C9},       // измерения (RAM 0x2C9..0x3B8)
	{devAddr: 0x05A9, length: 85, ramBase: 0x03B9},        // реле/знаки/измерения (RAM 0x3B9..0x40D)
	{devAddr: 0x0F4A, length: 240, ramBase: 0x0D5A},       // I2C_Err(0xD5A), Uakb/Takb/QCell/ErrCells (RAM 0xD5A..0xE49)
	{devAddr: 0x103A, length: 54, ramBase: 0x0E4A},        // BMS/параллельные MPPT хвост (RAM 0xE4A..0xE7F)
	{devAddr: 0x2C9, length: 2, ramBase: kesSerialImgKey}, // серийник (EEPROM 0x2C9, read as-is)
}

// kesInfo — идентификация КЭС (из ID-окна).
type kesInfo struct {
	Device byte // _Device (0x00) — должно быть 5
	Serial uint16
	FW     byte // версия ПО
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

// le16 читает 16-битное LE из образа по RAM-адресу.
func le16(img map[uint16]byte, addr uint16) uint16 {
	return uint16(img[addr]) | uint16(img[addr+1])<<8
}

// probeKes — проба: читаем 1 байт _Device (0x0000). 5 → КЭС; 3 → МАП (чужое).
// Ошибка чтения оборачивается в errNoKes с сохранением причины (для -debug).
func probeKes(p *transport.Port, echoTO, respTO time.Duration) (*kesInfo, error) {
	data, err := microart.Read(p, 0x0000, 0, echoTO, respTO) // страница 1 байт
	if err != nil {
		return nil, fmt.Errorf("%w (чтение _Device: %v)", errNoKes, err)
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
		return nil, fmt.Errorf("%w (_Device=%d)", errNoKes, data[0])
	}
}

// readAllWindows читает все окна КЭС в плоский образ (ключ — RAM-адрес).
func readAllWindows(p *transport.Port, echoTO, respTO time.Duration) (map[uint16]byte, error) {
	img := make(map[uint16]byte, kesRAMHigh)
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

// parseKes разбирает образ в идентификацию (Serial/FW).
func parseKes(img map[uint16]byte) *kesInfo {
	return &kesInfo{
		Device: img[0x0000],
		FW:     img[0x0002],
		Serial: le16(img, kesSerialImgKey),
	}
}

// kesSetting — одна настройка КЭС (условно подтверждённая, RAM-копия).
// Адреса/размеры — docs/kes-memory.md §3 (Карта Памяти_WG.csv, требуется проверка).
type kesSetting struct {
	ram  uint16 // адрес RAM-копии
	size int    // 1 или 2 байта
}

// kesSettings — список настроек в порядке KESS (docs/kes-memory.md §3).
var kesSettings = []kesSetting{
	{0x210, 1}, {0x211, 1}, {0x212, 2}, {0x214, 2}, {0x216, 1}, {0x217, 1},
	{0x218, 1}, {0x219, 1}, {0x21A, 1}, {0x21C, 1}, {0x21D, 1}, {0x21E, 2},
	{0x220, 2}, {0x222, 2}, {0x224, 2}, {0x226, 2}, {0x228, 2}, {0x22A, 2},
	{0x22C, 2}, {0x22E, 2}, {0x230, 2}, {0x232, 2}, {0x234, 2}, {0x236, 2},
	{0x238, 2}, {0x23A, 2}, {0x23C, 1}, {0x23D, 1}, {0x23E, 1}, {0x23F, 1},
	{0x240, 1}, {0x241, 2}, {0x243, 1}, {0x244, 2}, {0x246, 2}, {0x248, 2},
	{0x24A, 2}, {0x24C, 2}, {0x24E, 2}, {0x250, 2},
}

// KesData — параметры устройства + измерения КЭС (docs/kes-memory.md §2/§4).
type KesData struct {
	// Параметры устройства (паспорт/номинал, источник истины 2).
	Device    byte // _Device (0x00) — должно быть 5
	Pow       byte // Pow (0x01), значение как есть
	FW        byte // версия ПО (0x02): major=v&0x0F, minor=v>>5
	ModelPow  byte // мощность модели (0x05), raw: Вт = значение×100
	ModelVolt byte // напряжение модели (0x9B), raw: В = значение×12
	Pic       byte // Pic/Модель (0x0C) — фиксировано =1
	Net       byte // Net (0x0D), значение как есть

	Serial   uint16
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
	I2CErr   byte

	// Режим/знак/MPP/ветер (публикуются в read_json?device=mppt; см. legacy §1b).
	Mode      byte   // 0x2C2: 'i'/'v'/'B'/'b'/'S' (режим заряда)
	Sign      byte   // 0x37A: '+'/'-'
	MPP       byte   // 0x379: 'R'/'L'/'0'
	Windspeed uint16 // 0x3F8 (LE, u16): 0xFFFF → 65535 (н/д)

	// BMS и параллельные MPPT (Мастер) — docs/kes-memory.md §1.
	UakbCell [32]uint16 // UakbCell (0xD80), напряжение банок
	TakbCell [32]byte   // TakbCell (0xDC0), температура банок (x−50)
	QCell    [32]uint16 // Q_Cell (0xDE0), ток разряда банок
	ErrCells [32]byte   // ErrCells (0xE00), ошибки BMS
	IAkbMPPT [16]uint16 // I_Akb_MPPT (0xE30), ток заряда параллельных MPPT
	ModeMPPT [16]byte   // Mode_MPPT (0xE50): 0 цикл, 1 дозаряд, 2 буфер1, 3 буфер2
	TakbMPPT [16]byte   // Takb_MPPT (0xE60), температура параллельных MPPT (x−50)
	ErrMPPT  [16]byte   // ErrMPPT (0xE70), ошибки параллельных MPPT

	// Настройки (условно подтверждённые — RFC, docs/kes-memory.md §3): сырые значения
	// RAM-копий (KESS). Порядок = kesSettings.
	Settings [lenKesSettings]uint16
}

// lenKesSettings — число настроек КЭС.
const lenKesSettings = 40

// init — защита от рассинхрона kesSettings/lenKesSettings: если список разросся без
// правки константы, падаем при старте с внятной ошибкой, а не в горутине опроса.
func init() {
	if len(kesSettings) != lenKesSettings {
		panic(fmt.Sprintf("kesSettings: %d настроек, а lenKesSettings=%d — поправь константу/список",
			len(kesSettings), lenKesSettings))
	}
}

// parseData заполняет измерения из образа (ключ — RAM-адрес).
func parseData(img map[uint16]byte) KesData {
	d := KesData{Device: img[0x0000],
		Pow:       img[0x0001],
		FW:        img[0x0002],
		ModelPow:  img[0x0005],
		ModelVolt: img[0x009B],
		Pic:       img[0x000C],
		Net:       img[0x000D],
		// Серийник — из EEPROM (окно devAddr 0x2C9 кладётся в kesSerialImgKey);
		// RAM 0x2C9 перекрыта окном измерений, читать оттуда серийник нельзя.
		Serial:    le16(img, kesSerialImgKey),
		VcPV:      le16(img, 0x2D9),
		IcPV:      le16(img, 0x2DF),
		VBat:      le16(img, 0x2E5),
		PPV:       le16(img, 0x2EB),
		POut:      le16(img, 0x2ED),
		PLoad:     le16(img, 0x2F5),
		PCurr:     le16(img, 0x2F9),
		ICh:       le16(img, 0x2FB),
		IOut:      le16(img, 0x2FF),
		TempInt:   img[0x31D],
		TempBat:   img[0x31E],
		PwrW:      le16(img, 0x39F),
		PwrKW:     le16(img, 0x3A1),
		SignC0:    img[0x404],
		SignC1:    img[0x405],
		IExts0:    le16(img, 0x406),
		IExts1:    le16(img, 0x408),
		PExts0:    le16(img, 0x40A),
		PExts1:    le16(img, 0x40C),
		RelayC:    img[0x3E3],
		RSErrSis:  img[0x3B6],
		I2CErr:    img[0xD5A],
		Mode:      img[0x2C2],
		Sign:      img[0x37A],
		MPP:       img[0x379],
		Windspeed: le16(img, 0x3F8),
	}
	for i := 0; i < 32; i++ {
		d.UakbCell[i] = le16(img, 0xD80+uint16(2*i))
		d.TakbCell[i] = img[0xDC0+uint16(i)]
		d.QCell[i] = le16(img, 0xDE0+uint16(2*i))
		d.ErrCells[i] = img[0xE00+uint16(i)]
	}
	for i := 0; i < 16; i++ {
		d.IAkbMPPT[i] = le16(img, 0xE30+uint16(2*i))
		d.ModeMPPT[i] = img[0xE50+uint16(i)]
		d.TakbMPPT[i] = img[0xE60+uint16(i)]
		d.ErrMPPT[i] = img[0xE70+uint16(i)]
	}
	for i, s := range kesSettings {
		if s.size == 2 {
			d.Settings[i] = le16(img, s.ram)
		} else {
			d.Settings[i] = uint16(img[s.ram])
		}
	}
	return d
}

// Фиксированные размеры под-блоков слота (по 512 Б, запас на будущие поля). Каждый блок
// выделяется по своей константе, неиспользуемая часть — нули (резерв). Слот =
// [EEPROM-блок][RAM-блок][Настройки-блок], суммарный payload = 3×512 = 1536 Б.
const (
	memberKeseSize = 512 // EEPROM-контракт (параметры устройства): занято 15 Б + резерв
	memberKesrSize = 512 // RAM-контракт (телеметрия + BMS/MPPT): занято ~324 Б + резерв
	memberKessSize = 512 // настройки (KESS): занято 85 Б + резерв
)

// Константы смещений контракта слота (см. docs/kes-reader.md §4/§7.7) и образа чтения.
// Введены, чтобы код и тесты не расходились с документом (защита от дрейфа, «H2» ревью).
const (
	// kesRAMHigh — верхняя граница RAM-адресов контракта (+1); используется как hint карты.
	kesRAMHigh = 0xE80

	// kesSerialImgKey — резервный ключ образа для серийника из EEPROM (0x2C9).
	// Вне RAM-диапазона (kesRAMHigh=0xE80), чтобы не пересекаться с окнами измерений
	// и не перетирать RAM-значение 0x2C9. Читается отдельным окном devAddr 0x2C9.
	kesSerialImgKey = 0xE80

	// Смещения под-блоков EEPROM ("KESE").
	keseVerOff       = 4
	keseSerialOff    = 6 // uint16 LE
	keseDeviceOff    = 8
	kesePowOff       = 9
	keseFWOff        = 10
	keseModelPowOff  = 11
	keseModelVoltOff = 12
	kesePicOff       = 13
	keseNetOff       = 14

	// Смещения под-блока RAM ("KESR").
	kesrVerOff      = 4
	kesrLastSeenOff = 6 // uint64 LE (Unix-ts)
	kesrRelayCOff   = 14
	kesrRSErrSisOff = 15
	kesrI2CErrOff   = 16
	kesrVcPVOff     = 18
	kesrIcPVOff     = 20 // u16 LE
	kesrVBatOff     = 22
	kesrPPVOff      = 24
	kesrPOutOff     = 26
	kesrPLoadOff    = 28
	kesrPCurrOff    = 30
	kesrIChOff      = 32
	kesrIOutOff     = 34
	kesrTempIntOff  = 36 // byte (x-50)
	kesrTempBatOff  = 37 // byte (x-50)
	kesrPwrWOff     = 38 // u16 LE (младшая часть Pwr_kW)
	kesrPwrKWOff    = 40 // u16 LE (целая часть Pwr_kW)
	kesrIExts0Off   = 42
	kesrIExts1Off   = 44
	kesrPExts0Off   = 46
	kesrPExts1Off   = 48
	kesrModeOff     = 50 // byte: режим заряда (0x2C2)
	kesrSignOff     = 51 // byte: знак (0x37A)
	kesrMppOff      = 52 // byte: MPP (0x379)
	kesrWindOff     = 53 // u16 LE: windspeed (0x3F8), 0xFFFF → 65535 (н/д)
	kesrBmsOff      = 55 // UakbCell[32]×u16 — начало BMS/MPPT массивов

	// Смещения под-блока настроек ("KESS").
	kessVerOff      = 4
	kessSettingsOff = 5 // [40]×uint16 LE raw значений
)

// blockOffsets — смещения под-блоков в собранном payload слота.
const (
	offKese = 0                               // блок "KESE"
	offKesr = memberKeseSize                  // блок "KESR" (после EEPROM)
	offKess = memberKeseSize + memberKesrSize // блок "KESS" (после RAM)
)

// Контракт слота КЭС разделён на три независимых под-блока, чтобы не было перекрытия
// адресов/отображений; каждый блок — фиксированного размера с резервом:
//   - EEPROM-контракт (magic "KESE"): параметры устройства (паспорт/номинал), адрес «как есть»;
//   - RAM-контракт (magic "KESR"): телеметрия + BMS/параллельные MPPT, адрес = RAM + 0x1F0;
//   - Настройки (magic "KESS"): 40 значений RAM-копий (условно подтверждённые).
// Режим/знак/MPP/ветер (Mode 0x2C2, Sign 0x37A, MPP 0x379, windspeed 0x3F8) публикуются
// в RAM-контракт и отдаются в read_json?device=mppt (legacy §1b).
// Смещения контракта зафиксированы тестом TestContractGolden (защита от дрейфа).

// buildEepromPayload — EEPROM-контракт (параметры устройства).
func buildEepromPayload(meta contractMeta, d KesData) []byte {
	b := make([]byte, memberKeseSize)
	copy(b[0:4], "KESE")
	b[keseVerOff] = 1
	binary.LittleEndian.PutUint16(b[keseSerialOff:keseSerialOff+2], meta.Serial)
	b[keseDeviceOff] = d.Device
	b[kesePowOff] = d.Pow
	b[keseFWOff] = d.FW
	b[keseModelPowOff] = d.ModelPow
	b[keseModelVoltOff] = d.ModelVolt
	b[kesePicOff] = d.Pic
	b[keseNetOff] = d.Net
	return b
}

// buildRamPayload — RAM-контракт (телеметрия + BMS/MPPT).
func buildRamPayload(meta contractMeta, d KesData) []byte {
	b := make([]byte, memberKesrSize)
	copy(b[0:4], "KESR")
	b[kesrVerOff] = 1
	binary.LittleEndian.PutUint64(b[kesrLastSeenOff:kesrLastSeenOff+8], uint64(meta.LastSeen.Unix()))
	b[kesrRelayCOff] = d.RelayC
	b[kesrRSErrSisOff] = d.RSErrSis
	b[kesrI2CErrOff] = d.I2CErr
	binary.LittleEndian.PutUint16(b[kesrVcPVOff:kesrVcPVOff+2], d.VcPV)
	binary.LittleEndian.PutUint16(b[kesrIcPVOff:kesrIcPVOff+2], d.IcPV)
	binary.LittleEndian.PutUint16(b[kesrVBatOff:kesrVBatOff+2], d.VBat)
	binary.LittleEndian.PutUint16(b[kesrPPVOff:kesrPPVOff+2], d.PPV)
	binary.LittleEndian.PutUint16(b[kesrPOutOff:kesrPOutOff+2], d.POut)
	binary.LittleEndian.PutUint16(b[kesrPLoadOff:kesrPLoadOff+2], d.PLoad)
	binary.LittleEndian.PutUint16(b[kesrPCurrOff:kesrPCurrOff+2], d.PCurr)
	binary.LittleEndian.PutUint16(b[kesrIChOff:kesrIChOff+2], d.ICh)
	binary.LittleEndian.PutUint16(b[kesrIOutOff:kesrIOutOff+2], d.IOut)
	b[kesrTempIntOff] = d.TempInt // однобайтовые ячейки (x-50)
	b[kesrTempBatOff] = d.TempBat
	binary.LittleEndian.PutUint16(b[kesrPwrWOff:kesrPwrWOff+2], d.PwrW)
	binary.LittleEndian.PutUint16(b[kesrPwrKWOff:kesrPwrKWOff+2], d.PwrKW)
	binary.LittleEndian.PutUint16(b[kesrIExts0Off:kesrIExts0Off+2], d.IExts0)
	binary.LittleEndian.PutUint16(b[kesrIExts1Off:kesrIExts1Off+2], d.IExts1)
	binary.LittleEndian.PutUint16(b[kesrPExts0Off:kesrPExts0Off+2], d.PExts0)
	binary.LittleEndian.PutUint16(b[kesrPExts1Off:kesrPExts1Off+2], d.PExts1)
	b[kesrModeOff] = d.Mode
	b[kesrSignOff] = d.Sign
	b[kesrMppOff] = d.MPP
	b[kesrWindOff] = byte(d.Windspeed)
	b[kesrWindOff+1] = byte(d.Windspeed >> 8)
	// BMS и параллельные MPPT (Мастер).
	off := kesrBmsOff
	for i := 0; i < 32; i++ {
		binary.LittleEndian.PutUint16(b[off+2*i:off+2*i+2], d.UakbCell[i])
	}
	off += 64
	for i := 0; i < 32; i++ {
		b[off+i] = d.TakbCell[i]
	}
	off += 32
	for i := 0; i < 32; i++ {
		binary.LittleEndian.PutUint16(b[off+2*i:off+2*i+2], d.QCell[i])
	}
	off += 64
	for i := 0; i < 32; i++ {
		b[off+i] = d.ErrCells[i]
	}
	off += 32
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint16(b[off+2*i:off+2*i+2], d.IAkbMPPT[i])
	}
	off += 32
	for i := 0; i < 16; i++ {
		b[off+i] = d.ModeMPPT[i]
	}
	off += 16
	for i := 0; i < 16; i++ {
		b[off+i] = d.TakbMPPT[i]
	}
	off += 16
	for i := 0; i < 16; i++ {
		b[off+i] = d.ErrMPPT[i]
	}
	return b
}

// buildSettingsPayload — контракт настроек (KESS, условно подтверждённый):
// [magic "KESS"][version][len×uint16 raw значений настроек].
func buildSettingsPayload(d KesData) []byte {
	b := make([]byte, memberKessSize)
	copy(b[0:4], "KESS")
	b[kessVerOff] = 1
	off := kessSettingsOff
	for _, v := range d.Settings {
		binary.LittleEndian.PutUint16(b[off:off+2], v)
		off += 2
	}
	return b
}

// buildPayload — слот: объединение EEPROM-, RAM- и настроечного блоков.
func buildPayload(meta contractMeta, d KesData) []byte {
	p := append(buildEepromPayload(meta, d), buildRamPayload(meta, d)...)
	return append(p, buildSettingsPayload(d)...)
}
