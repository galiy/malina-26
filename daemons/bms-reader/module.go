package main

import (
	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// Внутренние коды типов BMS.
const (
	typeAntBMS uint8 = 1
	typeEnBMS  uint8 = 2
)

// probeKind — результат пробы одного типа (arch §19).
type probeKind uint8

const (
	probeFound    probeKind = iota // устройство наше
	probeNotMine                   // семейство ответило, но не наше
	probeNoAnswer                  // ответа нет
)

// probeSlot — найденный слот (для шинных типов — адрес; для «нулевых» — 0).
type probeSlot struct {
	Addr int
	Base string // BASE для ID (arch §9)
}

// probeResult — итог probe типа.
type probeResult struct {
	Kind  probeKind
	Slots []probeSlot
}

// bmsType — тип BMS: проба и опрос одного слота.
type bmsType interface {
	TypeID() uint8
	Name() string
	Framing() uint8 // framingNative / framingMRTU / framingMTCP
	// Speeds — список скоростей COM для этого типа (оркестратор перебирает).
	Speeds() []int
	// Probe распознаёт свои устройства на открытом источнике (порт/скорость —
	// задаёт оркестратор). Для шинных — перебирает адреса.
	Probe(t Transport) (probeResult, error)
	// Poll опрашивает один слот (addr) и возвращает контракт телеметрии.
	Poll(t Transport, addr int) (bmscontract.Telemetry, error)
}

// bmsTypesOrder — порядок вызова probe-функций (arch §19).
var bmsTypesOrder = []bmsType{
	antBMS{},
	enBMS{},
}
