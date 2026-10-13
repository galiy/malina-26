package main

import (
	"fmt"

	"github.com/galiy/malina-26/daemons/lib/stat"
	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// statsink — модуль сохранения сырья (raw-tsdb); nil, если не настроен.
var statsink *stat.Sink

// tsreg — реестр контрактов, создаётся один раз (не в горячем пути опроса).
var tsreg = tscontract.NewDefaultRegistry()

func initStatsink(sock string) {
	if sock == "" {
		return
	}
	statsink = stat.New(sock)
}

// publishRaw — сырой снапшот КЭС (контракт `kes` v1, 15 телеметрических полей;
// docs/stat.md) в raw-хранилище. Неблокирующий Publish, в пулере расчётов нет.
func publishRaw(serial uint16, t int64, d KesData) {
	if statsink == nil {
		return
	}
	c, ok := tsreg.Get("kes", 1)
	if !ok {
		return
	}
	vals := []tscontract.Value{
		{ID: 1, Type: tscontract.FTU16, Int: int64(d.VcPV)},
		{ID: 2, Type: tscontract.FTU16, Int: int64(d.IcPV)},
		{ID: 3, Type: tscontract.FTU16, Int: int64(d.PPV)},
		{ID: 4, Type: tscontract.FTU16, Int: int64(d.Windspeed)},
		{ID: 5, Type: tscontract.FTU16, Int: int64(d.VBat)},
		{ID: 6, Type: tscontract.FTU16, Int: int64(d.ICh)},
		{ID: 7, Type: tscontract.FTU16, Int: int64(d.PCurr)},
		{ID: 8, Type: tscontract.FTU8, Int: int64(d.TempBat)},
		{ID: 9, Type: tscontract.FTU16, Int: int64(d.POut)},
		{ID: 10, Type: tscontract.FTU16, Int: int64(d.PLoad)},
		{ID: 11, Type: tscontract.FTU16, Int: int64(d.IOut)},
		{ID: 12, Type: tscontract.FTU8, Int: int64(d.TempInt)},
		{ID: 13, Type: tscontract.FTU8, Int: int64(d.Mode)},
		{ID: 14, Type: tscontract.FTU8, Int: int64(d.RelayC)},
		{ID: 15, Type: tscontract.FTU8, Int: int64(d.RSErrSis)},
	}
	p, err := c.Encode(vals)
	if err != nil {
		return
	}
	statsink.Publish("kes", fmt.Sprintf("%d", serial), t, 1, p)
}
