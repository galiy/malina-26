package main

import (
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
	"github.com/galiy/malina-26/daemons/lib/stat"
	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// bmsReg — реестр контрактов tsdb (bms v1 зарегистрирован в lib/tscontract).
var bmsReg = tscontract.NewDefaultRegistry()

// telemetryValues раскладывает контракт в значения полей (bms v1, field_id 1..58).
func telemetryValues(t bmscontract.Telemetry) []tscontract.Value {
	v := make([]tscontract.Value, 0, 58)
	add := func(id uint16, ft tscontract.FieldType, n int64) {
		v = append(v, tscontract.Value{ID: id, Type: ft, Int: n})
	}
	add(1, tscontract.FTU8, int64(t.CellCount))
	add(2, tscontract.FTU8, int64(t.TempCount))
	add(3, tscontract.FTU16, int64(t.Flags))
	for i := 0; i < bmscontract.MaxCells; i++ { // 4..19
		add(uint16(4+i), tscontract.FTU16, int64(t.CellsMV[i]))
	}
	for i := 0; i < bmscontract.MaxTemps; i++ { // 20..27
		add(uint16(20+i), tscontract.FTInt, int64(t.TempsDC[i]))
	}
	add(28, tscontract.FTInt, int64(t.CurrentDA))
	add(29, tscontract.FTInt, int64(t.PowerW))
	add(30, tscontract.FTU16, int64(t.SocDP))
	add(31, tscontract.FTU16, int64(t.SohDP))
	add(32, tscontract.FTInt, int64(t.CapMAh))
	add(33, tscontract.FTInt, int64(t.RemainingMAh))
	add(34, tscontract.FTInt, int64(t.RatedMAh))
	add(35, tscontract.FTU16, int64(t.Cycles))
	add(36, tscontract.FTU16, int64(t.TotalCV))
	add(37, tscontract.FTU16, int64(t.PortCV))
	add(38, tscontract.FTInt, int64(t.BalanceMask))
	add(39, tscontract.FTU16, int64(t.Mode))
	for i := 0; i < bmscontract.AlarmLen; i++ { // 40..58
		add(uint16(40+i), tscontract.FTU8, int64(t.Alarm[i]))
	}
	return v
}

// publishRaw пишет снапшот телеметрии в raw-tsdb через stat.Sink.
func publishRaw(sink *stat.Sink, id string, tel bmscontract.Telemetry) {
	if sink == nil {
		return
	}
	c, ok := bmsReg.Get("bms", 1)
	if !ok {
		return
	}
	payload, err := c.Encode(telemetryValues(tel))
	if err != nil {
		return
	}
	sink.Publish("bms", id, time.Now().Unix(), 1, payload)
}
