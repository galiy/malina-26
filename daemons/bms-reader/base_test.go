package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeBase(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"AntBMS-DSP-160", "AntBMS-DSP-160"},
		{"  Ant/BMS\x00\x01  ", "AntBMS"},
		{"", "BMS"},
		{".", "BMS"},
		{"..", "BMS"},
	}
	for _, c := range cases {
		if got := sanitizeBase(c.in); got != c.want {
			t.Errorf("sanitizeBase(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}

	if got := sanitizeBase(strings.Repeat("A", idMax)); len(got) != idMax {
		t.Errorf("ровно %d байт: len=%d", idMax, len(got))
	}
	if got := sanitizeBase(strings.Repeat("A", idMax+1)); len(got) != idMax {
		t.Errorf("ASCII-обрезка: len=%d, ожидалось %d", len(got), idMax)
	}

	// 49 ASCII + 2-байтная руна = 51 байт: обрезка обязана отбросить руну
	// (ветка cut-- выполняется).
	got := sanitizeBase(strings.Repeat("A", 49) + "Ж")
	if len(got) != 49 || !utf8.ValidString(got) {
		t.Errorf("2-байтная руна: len=%d valid=%v %q", len(got), utf8.ValidString(got), got)
	}

	// 48 ASCII + 4-байтная руна = 52 байта: та же проверка для surrogate-пары.
	got = sanitizeBase(strings.Repeat("A", 48) + "😀")
	if len(got) != 48 || !utf8.ValidString(got) {
		t.Errorf("4-байтная руна: len=%d valid=%v %q", len(got), utf8.ValidString(got), got)
	}

	// После обрезки строка снова может стать `.`/`..`.
	if got := sanitizeBase(" ." + strings.Repeat(" ", 49) + "x"); got != "BMS" {
		t.Errorf("обрезка до '.': %q, ожидалось BMS", got)
	}
	if got := sanitizeBase(" .." + strings.Repeat(" ", 48) + "x"); got != "BMS" {
		t.Errorf("обрезка до '..': %q, ожидалось BMS", got)
	}
	// C1-управляющий символ (U+0085) удаляется.
	if got := sanitizeBase("A\u0085B"); got != "AB" {
		t.Errorf("C1-контрол: %q, ожидалось AB", got)
	}
	// Флаг фактической обрезки (для лога, без ложных срабатываний).
	if _, tr := sanitizeBaseN(strings.Repeat("A", idMax+1)); !tr {
		t.Error("51 байт: ожидалась обрезка")
	}
	if _, tr := sanitizeBaseN(strings.Repeat("A", idMax)); tr {
		t.Error("50 байт: обрезки быть не должно")
	}
	if _, tr := sanitizeBaseN(strings.Repeat("/", 60) + "A"); tr {
		t.Error("сырой BASE > 50, но после очистки короткий: обрезки быть не должно")
	}

	// Идемпотентность.
	x := sanitizeBase(" A/B\u0085 " + strings.Repeat("Ж", 30))
	if again := sanitizeBase(x); again != x {
		t.Errorf("не идемпотентно: %q -> %q", x, again)
	}
}
