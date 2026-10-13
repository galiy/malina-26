package main

import (
	"fmt"

	"github.com/galiy/malina-26/daemons/lib/stat"
	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// statsink — модуль сохранения сырья (raw-tsdb); nil, если не настроен.
var statsink *stat.Sink

func initStatsink(sock string) {
	if sock == "" {
		return
	}
	statsink = stat.New(sock)
}

// publishRaw отдаёт сырой снапшот МАП в raw-хранилище: контракт `map` v1 и, при
// числе ячеек BMS 1..32, `mbms` Vn. В пулере расчётов нет — только сборка payload
// и неблокирующий Publish (docs/map-reader.md, docs/telemetry-store.md §14.2).
func publishRaw(serial uint16, t int64, mem memMap) {
	if statsink == nil {
		return
	}
	id := fmt.Sprintf("%d", serial)
	if p := buildMapV1(mem); p != nil {
		statsink.Publish("map", id, t, 1, p)
	}
	if n, p := buildMbms(mem); p != nil {
		statsink.Publish("mbms", id, t, uint16(n), p)
	}
}

func registry() *tscontract.Registry { return tscontract.NewDefaultRegistry() }

// buildMapV1 собирает payload контракта `map` v1 (26 полей, docs/stat.md) из
// карты памяти МАП. Присутствуют только прочитанные ячейки (presence — по ним).
func buildMapV1(mem memMap) []byte {
	vals := make([]tscontract.Value, 0, 26)
	add8 := func(id uint16, addr int) {
		if b, ok := mem[addr]; ok {
			vals = append(vals, tscontract.Value{ID: id, Type: tscontract.FTU8, Int: int64(b)})
		}
	}
	add16 := func(id uint16, lo, hi int) { // value = lo | hi<<8
		l, ok1 := mem[lo]
		h, ok2 := mem[hi]
		if ok1 && ok2 {
			vals = append(vals, tscontract.Value{ID: id, Type: tscontract.FTU16, Int: int64(uint16(l) | uint16(h)<<8)})
		}
	}
	add8(1, 0x422)          // UNET
	add8(2, 0x423)          // INET
	add8(3, 0x431)          // INET_16_4
	add16(4, 0x424, 0x457)  // PNET (L 0x424, H 0x457)
	add16(5, 0x409, 0x456)  // PLoad (L 0x409, H 0x456)
	add8(6, 0x427)          // UOUTmed
	add8(7, 0x425)          // TFNET
	add16(8, 0x406, 0x405)  // UAcc_med (VL 0x406, VH 0x405)
	add8(9, 0x408)          // IAcc_med_A_2
	add16(10, 0x432, 0x433) // IAcc_med_A_u16 (L 0x432, H 0x433)
	add8(11, 0x404)         // StateUAcc
	add8(12, 0x41C)         // F_AccOver
	add8(13, 0x524)         // MPPT_toCh
	add8(14, 0x42E)         // Temp_Grad0
	add8(15, 0x42F)         // Temp_Grad1
	add8(16, 0x430)         // Temp_Grad2
	add8(17, 0x43C)         // Temp_off
	add8(18, 0x458)         // CulerSpead
	add8(19, 0x400)         // MODE_RAM
	add8(20, 0x402)         // StatusCh
	add8(21, 0x42A)         // RSErrSis
	add8(22, 0x42B)         // RSErrJobM
	add8(23, 0x42C)         // RSErrJob
	add8(24, 0x447)         // RSErrDop
	add8(25, 0x42D)         // RSWarning
	add8(26, 0x527)         // flagUnet2
	if len(vals) == 0 {
		return nil
	}
	c, ok := registry().Get("map", 1)
	if !ok {
		return nil
	}
	p, err := c.Encode(vals)
	if err != nil {
		return nil
	}
	return p
}

// buildMbms собирает payload `mbms` Vn (n = число ячеек BMS, 1..32; docs/stat.md).
func buildMbms(mem memMap) (int, []byte) {
	nb, ok := mem[0x100] // BMS_Num
	if !ok {
		return 0, nil
	}
	n := int(nb)
	if n < 1 || n > 32 {
		return 0, nil
	}
	vals := make([]tscontract.Value, 0, 3*n)
	for i := 0; i < n; i++ {
		l, ok1 := mem[0x480+2*i] // UakbCell_L_i
		h, ok2 := mem[0x481+2*i] // UakbCell_H_i
		if ok1 && ok2 {
			vals = append(vals, tscontract.Value{ID: uint16(3*i + 1), Type: tscontract.FTU16, Int: int64(uint16(l) | uint16(h)<<8)})
		}
		if b, ok := mem[0x4C0+i]; ok { // TakbCell_i
			vals = append(vals, tscontract.Value{ID: uint16(3*i + 2), Type: tscontract.FTU8, Int: int64(b)})
		}
		if b, ok := mem[0x4E0+i]; ok { // Q_Cell_i
			vals = append(vals, tscontract.Value{ID: uint16(3*i + 3), Type: tscontract.FTU8, Int: int64(b)})
		}
	}
	c, ok := registry().Get("mbms", uint16(n))
	if !ok {
		return 0, nil
	}
	p, err := c.Encode(vals)
	if err != nil {
		return 0, nil
	}
	return n, p
}
