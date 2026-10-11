package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

// Бинарный контракт в слоте. В полезной нагрузке нет имён — только значения по
// индексу поля в documentedCells (порядок таблицы = id). Читатель сопоставляет
// id→имя по той же таблице (версия в заголовке).
//
// Полезная нагрузка (payload):
//
//	0   magic     uint32  ('M','A','P','D')
//	4   version   uint16
//	6   nfields   uint16
//	8   port      [32]byte (zero-padded)
//	40  model     uint8   ('P'/'H'/'D'/'T')
//	41  protocol  uint8   (0=microart, 1=modbus)
//	42  unit      uint8
//	43  reserved  uint8
//	44  reserved  uint32
//	48  gen       uint64
//	56  lastSeen  uint64  (unix)
//	64  presence  [ceil(nfields/8)]byte   (бит i = поле i присутствует)
//	64+presenceLen  values  nfields × uint32 LE (сырое значение ячейки)
const (
	contractMagic    = 0x4441504D // "MAPD"
	contractVersion  = 5
	contractMetaSize = 64
	contractPortSize = 32
)

type contractMeta struct {
	Port     string
	Model    string
	Protocol string // "microart"/"modbus"
	Unit     int
	Gen      uint64
	LastSeen time.Time
}

type memMap map[int]byte

func (m memMap) valueLE(addr, size int) uint64 {
	var v uint64
	for i := 0; i < size; i++ {
		b, ok := m[addr+i]
		if !ok {
			return v
		}
		v |= uint64(b) << (8 * i)
	}
	return v
}

func (m memMap) covered(addr, size int) bool {
	for i := 0; i < size; i++ {
		if _, ok := m[addr+i]; !ok {
			return false
		}
	}
	return true
}

func protoCode(p string) uint8 {
	if p == "modbus" {
		return 1
	}
	return 0
}

// buildContract формирует бинарную полезную нагрузку слота.
func buildContract(mem memMap, meta contractMeta) []byte {
	n := len(documentedCells)
	bitmapLen := (n + 7) / 8
	buf := make([]byte, contractMetaSize+bitmapLen+n*4)

	binary.LittleEndian.PutUint32(buf[0:4], contractMagic)
	binary.LittleEndian.PutUint16(buf[4:6], contractVersion)
	binary.LittleEndian.PutUint16(buf[6:8], uint16(n))
	copy(buf[8:8+contractPortSize], []byte(meta.Port))
	if len(meta.Model) > 0 {
		buf[40] = meta.Model[0]
	}
	buf[41] = protoCode(meta.Protocol)
	buf[42] = uint8(meta.Unit)
	binary.LittleEndian.PutUint64(buf[48:56], meta.Gen)
	binary.LittleEndian.PutUint64(buf[56:64], uint64(meta.LastSeen.Unix()))

	pres := buf[contractMetaSize : contractMetaSize+bitmapLen]
	vals := buf[contractMetaSize+bitmapLen:]
	for i, c := range documentedCells {
		if !mem.covered(c.Addr, c.Size) {
			continue
		}
		pres[i/8] |= 1 << (uint(i) % 8)
		binary.LittleEndian.PutUint32(vals[i*4:i*4+4], uint32(mem.valueLE(c.Addr, c.Size)))
	}
	return buf
}

// readRegionsNative читает области нативным протоколом в карту памяти.
func readRegionsNative(p *Port, regions [][2]int, echoTO, respTO time.Duration) (memMap, error) {
	mem := make(memMap, 512)
	for _, r := range regions {
		addr, last := r[0], r[1]
		length := last - addr + 1
		if length > 256 { // лимит страницы нативного протокола
			return nil, errNoMap
		}
		data, err := microartRead(p, uint16(addr), byte(length-1), echoTO, respTO)
		if err != nil {
			return nil, err
		}
		if len(data) < length {
			return nil, errNoMap
		}
		for i := 0; i < length; i++ {
			mem[addr+i] = data[i]
		}
	}
	return mem, nil
}

// readRegionsModbus читает области через Modbus (unit), байтовая адресация.
// Копирует ровно запрошенный диапазон addr..last (без лишних байт регистра).
func readRegionsModbus(p *Port, unit int, regions [][2]int, respTO time.Duration) (memMap, error) {
	mem := make(memMap, 512)
	for _, r := range regions {
		addr, last := r[0], r[1]
		length := last - addr + 1
		regStart := addr / 2
		count := last/2 - regStart + 1
		data, err := modbusReadRegisters(p, unit, uint16(regStart), uint16(count), respTO)
		if err != nil {
			return nil, err
		}
		off := addr - regStart*2
		if off+length > len(data) {
			return nil, errNoMap
		}
		for i := 0; i < length; i++ {
			mem[addr+i] = data[off+i]
		}
	}
	return mem, nil
}

// ContractMeta — разобранные мета-поля payload.
type ContractMeta struct {
	Port     string
	Model    string
	Protocol string
	Unit     int
	Gen      uint64
	LastSeen int64
}

func trimZero(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// DecodePayload разбирает бинарный payload слота: мета + MapData + присутствие.
// presence[i] == true — поле с id=i присутствует (заполнено) для этой модели.
func DecodePayload(payload []byte) (ContractMeta, MapData, []bool, error) {
	var md MapData
	n := len(documentedCells)
	bitmapLen := (n + 7) / 8
	if len(payload) < contractMetaSize+bitmapLen+n*4 {
		return ContractMeta{}, md, nil, errors.New("payload too short")
	}
	if binary.LittleEndian.Uint32(payload[0:4]) != contractMagic {
		return ContractMeta{}, md, nil, errors.New("bad magic")
	}
	if binary.LittleEndian.Uint16(payload[4:6]) != contractVersion {
		return ContractMeta{}, md, nil, errors.New("bad version")
	}
	if int(binary.LittleEndian.Uint16(payload[6:8])) != n {
		return ContractMeta{}, md, nil, errors.New("bad nfields")
	}
	var pm uint8
	if payload[40] != 0 {
		pm = payload[40]
	}
	var proto string
	if payload[41] == 1 {
		proto = "modbus"
	} else {
		proto = "microart"
	}
	meta := ContractMeta{
		Port:     trimZero(payload[8 : 8+contractPortSize]),
		Model:    string([]byte{pm}),
		Protocol: proto,
		Unit:     int(payload[42]),
		Gen:      binary.LittleEndian.Uint64(payload[48:56]),
		LastSeen: int64(binary.LittleEndian.Uint64(payload[56:64])),
	}
	pres := make([]bool, n)
	base := contractMetaSize
	for i := 0; i < n; i++ {
		pres[i] = payload[base+i/8]&(1<<(uint(i)%8)) != 0
	}
	if err := binary.Read(bytes.NewReader(payload[base+bitmapLen:]), binary.LittleEndian, &md); err != nil {
		return meta, md, pres, err
	}
	return meta, md, pres, nil
}
