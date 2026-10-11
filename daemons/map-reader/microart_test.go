package main

import (
	"errors"
	"testing"
)

func page48() []byte {
	return make([]byte, 48)
}

func TestParseCellsForeign(t *testing.T) {
	// КЭС/MPPT: _Device == 5 (даже при коротком ответе) → чужое MicroArt-устройство.
	data := page48()
	data[0x00] = 0x05
	if _, err := parseCells(data, "microart", 19200); !errors.Is(err, errForeignMicroArt) {
		t.Fatalf("_Device=5: получено %v, ждали errForeignMicroArt", err)
	}
	// Минимальный ответ (1 байт) тоже должен классифицироваться как чужой.
	if _, err := parseCells([]byte{0x05}, "microart", 19200); !errors.Is(err, errForeignMicroArt) {
		t.Fatalf("короткий _Device=5: получено %v, ждали errForeignMicroArt", err)
	}
}

func TestParseCellsEmpty(t *testing.T) {
	if _, err := parseCells(nil, "microart", 19200); !errors.Is(err, errNoMap) {
		t.Fatalf("пустой ответ: получено %v, ждали errNoMap", err)
	}
}

func TestParseCellsMapAccepted(t *testing.T) {
	// Базовый критерий — только _Device == 3; _VerPO/_DevOpt не отбраковывают.
	cases := []struct {
		name   string
		verpo  byte
		devopt byte
	}{
		{"нулевая версия", 0x00, 0x01},
		{"снятый процессор (2)", 0x30, 0x02},
		{"необычный devopt", 0x30, 0x07},
	}
	for _, c := range cases {
		data := page48()
		data[0x00] = 0x03
		data[0x02] = c.verpo
		data[0x07] = c.devopt
		info, err := parseCells(data, "microart", 19200)
		if err != nil {
			t.Fatalf("%s (_VerPO=%#x _DevOpt=%#x): ошибка %v, ждали приём", c.name, c.verpo, c.devopt, err)
		}
		if info == nil {
			t.Fatalf("%s: nil mapInfo", c.name)
		}
	}
}

func TestParseCellsShortMap(t *testing.T) {
	// _Device==3, но страница короче серийника — некорректный ответ МАП.
	data := make([]byte, 10)
	data[0x00] = 0x03
	if _, err := parseCells(data, "microart", 19200); !errors.Is(err, errNoMap) {
		t.Fatalf("короткий МАП: получено %v, ждали errNoMap", err)
	}
}

func TestParseCellsModelSerial(t *testing.T) {
	data := page48()
	data[0x00] = 0x03
	data[0x07] = 0x03 // Титанатор
	data[0x18] = 0x34
	data[0x19] = 0x12
	data[0x23] = 'A'
	info, err := parseCells(data, "microart", 115200)
	if err != nil {
		t.Fatalf("ошибка: %v", err)
	}
	if info.Model != "T" {
		t.Errorf("модель: %q, ждали T", info.Model)
	}
	if info.Serial != 0x1234 {
		t.Errorf("серийник: %#x, ждали 0x1234", info.Serial)
	}
	if info.SerialLetter != 'A' {
		t.Errorf("буква: %q, ждали A", info.SerialLetter)
	}
	if info.Baud != 115200 {
		t.Errorf("baud: %d, ждали 115200", info.Baud)
	}

	// Гибрид: _DevOpt!=3, 0x01 бит7 установлен.
	data = page48()
	data[0x00] = 0x03
	data[0x01] = 0x80
	if info, err := parseCells(data, "microart", 19200); err != nil || info.Model != "H" {
		t.Fatalf("гибрид: info=%+v err=%v, ждали модель H", info, err)
	}

	// Доминатор: 0x24 бит0.
	data = page48()
	data[0x00] = 0x03
	data[0x24] = 0x01
	if info, err := parseCells(data, "microart", 19200); err != nil || info.Model != "D" {
		t.Fatalf("доминатор: info=%+v err=%v, ждали модель D", info, err)
	}
}
