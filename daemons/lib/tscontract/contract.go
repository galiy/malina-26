// Package tscontract — общие контракты телеметрии для хранилища рядов (tsdb).
//
// Контракты — отдельные для каждого типа устройства, независимая нумерация
// версий; реестр ключуется парой (device_type, version) (§1.2, §8,
// docs/telemetry-store.md). Состав схем — docs/stat.md.
//
// Пакет общий для сервера (daemons/tsdb) и продюсера (malina-web): обе стороны
// обязаны кодировать/декодировать payload одинаково.
//
// payload := presence[ceil(nfields/8)] | значения присутствующих полей в порядке
// схемы:
//
//	FTInt(1):   zigzag-varint
//	FTFloat(2): 8 байт IEEE-754 LE
//	FTBytes(3): varint len | байты
//	FTU8(4):    1 байт
//	FTU16(5):   2 байта LE
//	FTI16(6):   2 байта LE, signed
//
// Зарегистрированные схемы (docs/stat.md):
//   - map  v1      — 26 колонок (регистры МАП, u8/u16);
//   - map  v2      — v1 + реле (id 27 `Relay`, биты 0/1/2 = реле 1/2/3);
//   - kes  v1      — 15 колонок (ячейки КЭС, u8/u16);
//   - mbms v1..v32 — BMS отдельным рядом: Vn = n ячеек × (u16+u8+u8),
//     device_id — тот же, что у МАП;
//   - bms  v1      — 58 колонок контракта BMS-Receiver (описание — docs/bms-reader.md §1);
//   - grid v1      — 24 поля (50 Б payload) сетевых инверторов (docs/grid-reader.md).
package tscontract

import (
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strconv"
)

var errShort = errors.New("короткий буфер")

// FieldType — тип значения поля контракта.
type FieldType uint8

const (
	FTInt   FieldType = 1
	FTFloat FieldType = 2
	FTBytes FieldType = 3
	FTU8    FieldType = 4
	FTU16   FieldType = 5
	FTI16   FieldType = 6
)

// Field — поле схемы контракта.
type Field struct {
	ID   uint16
	Type FieldType
	Name string // имя поля (для UI); семантику значений хранилище не знает
}

// Value — разобранное значение поля.
//
// Для FTInt/FTU8/FTU16 значение лежит в Int (для u8/u16 — беззнаковое),
// для FTFloat — в Float, для FTBytes — в Bytes.
type Value struct {
	ID    uint16
	Type  FieldType
	Int   int64
	Float float64
	Bytes []byte
}

// Contract — кодек одной версии контракта (для одного типа устройства).
type Contract interface {
	Version() uint16
	Fields() []Field
	Decode(payload []byte) ([]Value, error)
	Encode(vals []Value) ([]byte, error)
}

// Registry — реестр контрактов: тип устройства → версия → кодек.
type Registry struct {
	byType map[string]map[uint16]Contract
}

func NewRegistry() *Registry { return &Registry{byType: make(map[string]map[uint16]Contract)} }

func (r *Registry) Register(deviceType string, c Contract) {
	m := r.byType[deviceType]
	if m == nil {
		m = make(map[uint16]Contract)
		r.byType[deviceType] = m
	}
	m[c.Version()] = c
}

// Get возвращает кодек по (тип устройства, версия).
func (r *Registry) Get(deviceType string, version uint16) (Contract, bool) {
	m, ok := r.byType[deviceType]
	if !ok {
		return nil, false
	}
	c, ok := m[version]
	return c, ok
}

// Project — payload только с выбранными метриками; ok=false при неизвестной
// версии/типе или ошибке декодирования.
func (r *Registry) Project(deviceType string, version uint16, payload []byte, ids []uint16) ([]byte, bool) {
	c, ok := r.Get(deviceType, version)
	if !ok {
		return nil, false
	}
	vals, err := c.Decode(payload)
	if err != nil {
		return nil, false
	}
	want := make(map[uint16]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	sel := make([]Value, 0, len(ids))
	for _, v := range vals {
		if want[v.ID] {
			sel = append(sel, v)
		}
	}
	out, err := c.Encode(sel)
	if err != nil {
		return nil, false
	}
	return out, true
}

// --- кодирование значений (общее для payload и колонок) ---

func zigzag(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// AppendValue дописывает значение поля типа t к b.
func AppendValue(b []byte, t FieldType, v Value) []byte {
	var tmp [binary.MaxVarintLen64]byte
	switch t {
	case FTInt:
		n := binary.PutUvarint(tmp[:], zigzag(v.Int))
		return append(b, tmp[:n]...)
	case FTU8:
		return append(b, byte(v.Int))
	case FTU16:
		return binary.LittleEndian.AppendUint16(b, uint16(v.Int))
	case FTI16:
		return binary.LittleEndian.AppendUint16(b, uint16(int16(v.Int)))
	case FTFloat:
		var u [8]byte
		binary.LittleEndian.PutUint64(u[:], math.Float64bits(v.Float))
		return append(b, u[:]...)
	case FTBytes:
		n := binary.PutUvarint(tmp[:], uint64(len(v.Bytes)))
		b = append(b, tmp[:n]...)
		return append(b, v.Bytes...)
	}
	return b
}

// DecodeValue разбирает значение поля типа t из начала b; возвращает значение и
// остаток буфера.
func DecodeValue(b []byte, t FieldType) (Value, []byte, error) {
	v := Value{Type: t}
	switch t {
	case FTInt:
		u, n := binary.Uvarint(b)
		if n <= 0 {
			return v, nil, errShort
		}
		v.Int = unzigzag(u)
		return v, b[n:], nil
	case FTU8:
		if len(b) < 1 {
			return v, nil, errShort
		}
		v.Int = int64(b[0])
		return v, b[1:], nil
	case FTU16:
		if len(b) < 2 {
			return v, nil, errShort
		}
		v.Int = int64(binary.LittleEndian.Uint16(b[:2]))
		return v, b[2:], nil
	case FTI16:
		if len(b) < 2 {
			return v, nil, errShort
		}
		v.Int = int64(int16(binary.LittleEndian.Uint16(b[:2])))
		return v, b[2:], nil
	case FTFloat:
		if len(b) < 8 {
			return v, nil, errShort
		}
		v.Float = math.Float64frombits(binary.LittleEndian.Uint64(b[:8]))
		return v, b[8:], nil
	case FTBytes:
		n, m := binary.Uvarint(b)
		if m <= 0 || len(b) < m+int(n) {
			return v, nil, errShort
		}
		v.Bytes = append([]byte(nil), b[m:m+int(n)]...)
		return v, b[m+int(n):], nil
	}
	return v, nil, errors.New("неизвестный тип поля")
}

// --- обобщённый контракт по схеме ---

type schemaContract struct {
	ver    uint16
	fields []Field
}

func newSchemaContract(ver uint16, fields []Field) schemaContract {
	return schemaContract{ver: ver, fields: fields}
}

func (c schemaContract) Version() uint16 { return c.ver }
func (c schemaContract) Fields() []Field { return c.fields }

func (c schemaContract) Decode(payload []byte) ([]Value, error) {
	n := len(c.fields)
	bl := (n + 7) / 8
	if len(payload) < bl {
		return nil, errShort
	}
	pres := payload[:bl]
	rest := payload[bl:]
	out := make([]Value, 0, n)
	for i, f := range c.fields {
		if pres[i/8]&(1<<(uint(i)%8)) == 0 {
			continue
		}
		v, r, err := DecodeValue(rest, f.Type)
		if err != nil {
			return nil, err
		}
		v.ID = f.ID
		rest = r
		out = append(out, v)
	}
	return out, nil
}

func (c schemaContract) Encode(vals []Value) ([]byte, error) {
	n := len(c.fields)
	idx := make(map[uint16]int, n)
	for i, f := range c.fields {
		idx[f.ID] = i
	}
	pres := make([]byte, (n+7)/8)
	type enc struct {
		i int
		v Value
	}
	list := make([]enc, 0, len(vals))
	for _, v := range vals {
		i, ok := idx[v.ID]
		if !ok {
			continue
		}
		pres[i/8] |= 1 << (uint(i) % 8)
		list = append(list, enc{i, v})
	}
	sort.Slice(list, func(a, b int) bool { return list[a].i < list[b].i })
	b := append([]byte(nil), pres...)
	for _, e := range list {
		b = AppendValue(b, c.fields[e.i].Type, e.v)
	}
	return b, nil
}

// --- зарегистрированные схемы (docs/stat.md) ---

// mapV1 — 26 колонок регистров МАП (u8/u16).
var mapV1Fields = []Field{
	{1, FTU8, "UNET"},             // 0x422
	{2, FTU8, "INET"},             // 0x423
	{3, FTU8, "INET_16_4"},        // 0x431
	{4, FTU16, "PNET"},            // 0x424L+0x457H
	{5, FTU16, "PLoad"},           // 0x409L+0x456H
	{6, FTU8, "UOUTmed"},          // 0x427
	{7, FTU8, "TFNET"},            // 0x425
	{8, FTU16, "UAcc_med"},        // 0x405VH+0x406VL
	{9, FTU8, "IAcc_med_A_2"},     // 0x408
	{10, FTU16, "IAcc_med_A_u16"}, // 0x432L+0x433H
	{11, FTU8, "StateUAcc"},       // 0x404
	{12, FTU8, "F_AccOver"},       // 0x41C
	{13, FTU8, "MPPT_toCh"},       // 0x524
	{14, FTU8, "Temp_Grad0"},      // 0x42E
	{15, FTU8, "Temp_Grad1"},      // 0x42F
	{16, FTU8, "Temp_Grad2"},      // 0x430
	{17, FTU8, "Temp_off"},        // 0x43C
	{18, FTU8, "CulerSpead"},      // 0x458
	{19, FTU8, "MODE_RAM"},        // 0x400
	{20, FTU8, "StatusCh"},        // 0x402
	{21, FTU8, "RSErrSis"},        // 0x42A
	{22, FTU8, "RSErrJobM"},       // 0x42B
	{23, FTU8, "RSErrJob"},        // 0x42C
	{24, FTU8, "RSErrDop"},        // 0x447
	{25, FTU8, "RSWarning"},       // 0x42D
	{26, FTU8, "flagUnet2"},       // 0x527
}

// mapV2Fields — map v1 + реле (id 27: `Relay`, биты 0/1/2 = реле 1/2/3,
// источник `Status_RELEdop` 0x586; docs/stat.md).
//
// ВНИМАНИЕ: v2 — это копия v1 + одно поле. Правка v1 (добавление/перестановка
// полей) неявно меняет и v2 — держать обе схемы согласованными (проверяется
// TestSchemasRegistered).
var mapV2Fields = append(append([]Field(nil), mapV1Fields...), Field{27, FTU8, "Relay"})

// mapV3Fields — map v2 + частота выхода (`ThFMAP`) и первые 4 канала MPPT
// (`I_Akb_MPPT0..3`) для графиков (docs/stat.md §map v3, docs/ui-stat.md §3.2).
var mapV3Fields = append(append([]Field(nil), mapV2Fields...),
	Field{28, FTU8, "ThFMAP"},       // 0x426
	Field{29, FTU16, "I_Akb_MPPT0"}, // 0x530L+0x531H
	Field{30, FTU16, "I_Akb_MPPT1"}, // 0x532L+0x533H
	Field{31, FTU16, "I_Akb_MPPT2"}, // 0x534L+0x535H
	Field{32, FTU16, "I_Akb_MPPT3"}, // 0x536L+0x537H
	Field{33, FTU8, "PNET_Sign_P"},  // 0x587: 0 — знак мощности сети отрицательный
)

// kesV1 — 15 колонок ячеек КЭС (u8/u16).
var kesV1Fields = []Field{
	{1, FTU16, "VcPV"},
	{2, FTU16, "IcPV"},
	{3, FTU16, "PPV"},
	{4, FTU16, "Windspeed"},
	{5, FTU16, "VBat"},
	{6, FTU16, "ICh"},
	{7, FTU16, "PCurr"},
	{8, FTU8, "TempBat"},
	{9, FTU16, "POut"},
	{10, FTU16, "PLoad"},
	{11, FTU16, "IOut"},
	{12, FTU8, "TempInt"},
	{13, FTU8, "Mode"},
	{14, FTU8, "RelayC"},
	{15, FTU8, "RSErrSis"},
}

// bmsV1 — 58 колонок контракта телеметрии BMS (docs/bms-reader.md §1):
// счётчики/флаги, 16 ячеек (мВ), 8 температур (0.1 °C), ток/мощность/ёмкости,
// напряжения сборки/клемм, маска балансировки, режим и 19 байт `alarm`.
var bmsV1Fields = func() []Field {
	f := make([]Field, 0, 58)
	f = append(f,
		Field{1, FTU8, "CellCount"},
		Field{2, FTU8, "TempCount"},
		Field{3, FTU16, "Flags"})
	for i := 1; i <= 16; i++ { // id 4..19
		f = append(f, Field{uint16(3 + i), FTU16, "Cell" + strconv.Itoa(i)})
	}
	for i := 1; i <= 8; i++ { // id 20..27
		f = append(f, Field{uint16(19 + i), FTInt, "Temp" + strconv.Itoa(i)})
	}
	f = append(f,
		Field{28, FTInt, "Current"},
		Field{29, FTInt, "Power"},
		Field{30, FTU16, "Soc"},
		Field{31, FTU16, "Soh"},
		Field{32, FTInt, "Cap"},
		Field{33, FTInt, "Remaining"},
		Field{34, FTInt, "Rated"},
		Field{35, FTU16, "Cycles"},
		Field{36, FTU16, "TotalV"},
		Field{37, FTU16, "PortV"},
		Field{38, FTInt, "BalanceMask"},
		Field{39, FTU16, "Mode"})
	for i := 1; i <= 19; i++ { // id 40..58
		f = append(f, Field{uint16(39 + i), FTU8, "Alarm" + strconv.Itoa(i)})
	}
	return f
}()

// mbmsFields — схема BMS на n ячеек: ячейка i (1..n) → UakbCell u16,
// TakbCell u8, Q_Cell u8; field_id = 3·(i−1)+1..+3 (docs/stat.md).
func mbmsFields(n int) []Field {
	f := make([]Field, 0, 3*n)
	for i := 1; i <= n; i++ {
		base := uint16(3 * (i - 1))
		suf := strconv.Itoa(i)
		f = append(f,
			Field{base + 1, FTU16, "UakbCell" + suf},
			Field{base + 2, FTU8, "TakbCell" + suf},
			Field{base + 3, FTU8, "Q_Cell" + suf})
	}
	return f
}

// gridV1Fields — контракт сетевых инверторов (docs/grid-reader.md):
// 15 телеметрических полей, 2 температуры, состояние и 6 слов аварий.
var gridV1Fields = []Field{
	{1, FTU16, "pv1_voltage"},
	{2, FTU16, "pv1_current"},
	{3, FTU16, "pv1_power"},
	{4, FTU16, "pv2_voltage"},
	{5, FTU16, "pv2_current"},
	{6, FTU16, "pv2_power"},
	{7, FTU16, "ac_active_power"},
	{8, FTI16, "ac_reactive_power"},
	{9, FTU16, "grid_frequency"},
	{10, FTU16, "l1_voltage"},
	{11, FTU16, "l1_current"},
	{12, FTU16, "l2_voltage"},
	{13, FTU16, "l2_current"},
	{14, FTU16, "l3_voltage"},
	{15, FTU16, "l3_current"},
	{16, FTI16, "temperature_1"},
	{17, FTI16, "temperature_2"},
	{18, FTU8, "inverter_status"},
	{19, FTU16, "alarm_w0"},
	{20, FTU16, "alarm_w1"},
	{21, FTU16, "alarm_w2"},
	{22, FTU16, "alarm_w3"},
	{23, FTU16, "alarm_w4"},
	{24, FTU16, "alarm_w5"},
}

const mbmsMaxCells = 32

// NewDefaultRegistry — реестр со всеми зарегистрированными схемами (docs/stat.md).
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register("map", newSchemaContract(1, mapV1Fields))
	r.Register("map", newSchemaContract(2, mapV2Fields))
	r.Register("map", newSchemaContract(3, mapV3Fields))
	r.Register("kes", newSchemaContract(1, kesV1Fields))
	r.Register("bms", newSchemaContract(1, bmsV1Fields))
	r.Register("grid", newSchemaContract(1, gridV1Fields))
	for n := 1; n <= mbmsMaxCells; n++ {
		r.Register("mbms", newSchemaContract(uint16(n), mbmsFields(n)))
	}
	return r
}
