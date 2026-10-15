package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/galiy/malina-26/daemons/lib/bmscontract"
)

// antBMS — тип AntBMS-DSP (дисплейный порт, пассивный). docs/bms/antbms-dsp.md.
type antBMS struct{}

func (antBMS) TypeID() uint8  { return typeAntBMS }
func (antBMS) Name() string   { return "AntBMS-DSP" }
func (antBMS) Framing() uint8 { return framingNative }

// antMarker — заголовок live-кадра.
var antMarker = []byte{0xAA, 0x55, 0xAA, 0xFF}

const antFrameLen = 140

// extractAntFrame ищет в буфере валидный 140-байтный кадр (маркер + CRC).
func extractAntFrame(b []byte) []byte {
	for i := 0; i+antFrameLen <= len(b); i++ {
		if b[i] != antMarker[0] {
			continue
		}
		if !bytesEq(b[i:i+4], antMarker) {
			continue
		}
		var sum int
		for _, x := range b[i+4 : i+138] {
			sum += int(x)
		}
		if uint16(sum) != binary.BigEndian.Uint16(b[i+138:i+140]) {
			continue
		}
		return b[i : i+antFrameLen]
	}
	return nil
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readAntFrame читает пассивный поток до валидного кадра (окно 500 мс).
func readAntFrame(t Transport) ([]byte, error) {
	t.SetReadTimeout(500 * time.Millisecond)
	deadline := time.Now().Add(500 * time.Millisecond)
	var acc []byte
	for time.Now().Before(deadline) {
		tmp := make([]byte, 256)
		n, err := t.Read(tmp)
		if n > 0 {
			acc = append(acc, tmp[:n]...)
			if f := extractAntFrame(acc); f != nil {
				return f, nil
			}
			if len(acc) > 4096 {
				acc = acc[len(acc)-antFrameLen:]
			}
		}
		if err != nil && err != errTimeout {
			return nil, err
		}
	}
	if f := extractAntFrame(acc); f != nil {
		return f, nil
	}
	return nil, errTimeout
}

// parseAntFrame разбирает 140 Б в контракт (§6 antbms-dsp).
func parseAntFrame(f []byte) (bmscontract.Telemetry, error) {
	var r bmscontract.Telemetry
	if len(f) < antFrameLen {
		return r, fmt.Errorf("antbms: кадр %d Б", len(f))
	}
	n := int(f[123])
	if n < 1 || n > bmscontract.MaxCells {
		return r, fmt.Errorf("antbms: ячеек %d", n)
	}
	if f[74] > 100 {
		return r, fmt.Errorf("antbms: soc %d", f[74])
	}
	r.CellCount = uint8(n)
	for i := 0; i < n; i++ {
		r.CellsMV[i] = binary.BigEndian.Uint16(f[6+2*i : 8+2*i])
	}
	r.CurrentDA = int32(binary.BigEndian.Uint32(f[70:74])) * 10 // 0.1А→0.01А
	r.SocDP = uint16(f[74]) * 10
	r.CapMAh = binary.BigEndian.Uint32(f[75:79]) / 1000 // µА·ч→мА·ч
	r.RemainingMAh = binary.BigEndian.Uint32(f[79:83]) / 1000
	r.TempCount = 6
	for i := 0; i < 6; i++ {
		r.TempsDC[i] = int16(binary.BigEndian.Uint16(f[91+2*i:93+2*i])) * 10 // °C→0.1°C
	}
	if f[103] != 0 {
		r.Flags |= bmscontract.FlagChargeMOS
	}
	if f[104] != 0 {
		r.Flags |= bmscontract.FlagDischargeMOS
	}
	if f[105] != 0 {
		r.Flags |= bmscontract.FlagBalancer
	}
	r.PowerW = int32(binary.BigEndian.Uint32(f[111:115]))
	var totalMV uint32
	for i := 0; i < n; i++ {
		totalMV += uint32(r.CellsMV[i])
	}
	r.TotalCV = uint16(totalMV / 10) // мВ→0.01В
	r.BalanceMask = binary.BigEndian.Uint32(f[132:136])
	r.Mode = antMode(r.CurrentDA)
	return r, nil
}

// antMode — канонический режим по знаку тока (docs/bms-reader.md §1).
func antMode(currentDA int32) uint16 {
	switch {
	case currentDA > 0:
		return bmscontract.ModeDischarge
	case currentDA < 0:
		return bmscontract.ModeCharge
	default:
		return bmscontract.ModeIdle
	}
}

// antBase формирует BASE от ёмкости (серийника в кадре нет).
func antBase(f []byte) string {
	capAh := binary.BigEndian.Uint32(f[75:79]) / 1_000_000
	return fmt.Sprintf("AntBMS-DSP-%d", capAh)
}

func (antBMS) Probe(t Transport) (probeResult, error) {
	f, err := readAntFrame(t)
	if err != nil {
		return probeResult{Kind: probeNoAnswer}, nil
	}
	if _, err := parseAntFrame(f); err != nil {
		return probeResult{Kind: probeNoAnswer}, nil
	}
	return probeResult{Kind: probeFound, Slots: []probeSlot{{Addr: 0, Base: antBase(f)}}}, nil
}

func (antBMS) Poll(t Transport, _ int) (bmscontract.Telemetry, error) {
	f, err := readAntFrame(t)
	if err != nil {
		return bmscontract.Telemetry{}, err
	}
	return parseAntFrame(f)
}
