package bmscontract

import (
	"bytes"
	"testing"
)

// TestSize — фиксированный размер записи.
func TestSize(t *testing.T) {
	if Size != 107 {
		t.Fatalf("Size = %d, ожидалось 107", Size)
	}
	if got := len(Encode(&Telemetry{})); got != Size {
		t.Fatalf("Encode = %d байт, ожидалось %d", got, Size)
	}
}

// TestRoundTrip — encode→decode сохраняет значения.
func TestRoundTrip(t *testing.T) {
	in := Telemetry{
		CellCount:    16,
		TempCount:    6,
		Flags:        FlagChargeMOS | FlagBalancer,
		CurrentDA:    -12345,
		PowerW:       4200,
		SocDP:        875,
		SohDP:        990,
		CapMAh:       320000,
		RemainingMAh: 155000,
		RatedMAh:     320000,
		Cycles:       42,
		TotalCV:      5320,
		PortCV:       5310,
		BalanceMask:  0x0003,
		Mode:         ModeCharge,
	}
	for i := 0; i < MaxCells; i++ {
		in.CellsMV[i] = uint16(3200 + i)
	}
	for i := 0; i < MaxTemps; i++ {
		in.TempsDC[i] = int16(250 - i)
	}
	copy(in.Alarm[:], []byte("alarm-bytes-0123456")) // 19 байт

	b := Encode(&in)
	out, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round-trip не совпал:\n in=%+v\nout=%+v", in, out)
	}
}

// TestDecodeShort — короткий буфер отклоняется.
func TestDecodeShort(t *testing.T) {
	if _, err := Decode(make([]byte, Size-1)); err == nil {
		t.Fatal("ожидалась ошибка на коротком буфере")
	}
}

// TestEndianness — многобайтовые поля пишутся big-endian.
func TestEndianness(t *testing.T) {
	b := Encode(&Telemetry{SocDP: 0x0102, Mode: 0x0304})
	if !bytes.Equal(b[60:62], []byte{0x01, 0x02}) {
		t.Fatalf("soc_dp не BE: % x", b[60:62])
	}
	if !bytes.Equal(b[86:88], []byte{0x03, 0x04}) {
		t.Fatalf("mode не BE: % x", b[86:88])
	}
}
