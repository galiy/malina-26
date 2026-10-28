package logx

import (
	"os"
	"strings"
	"testing"
)

func TestSyslogTag(t *testing.T) {
	cases := map[string]string{
		"map-reader: ":  "map-reader",
		"grid-reader: ": "grid-reader",
		"malina-arch: ": "malina-arch",
		"":              "malina",
		"  ":            "malina",
	}
	for in, want := range cases {
		if got := syslogTag(in); got != want {
			t.Errorf("syslogTag(%q)=%q, want %q", in, got, want)
		}
	}
}

// В тестовом бинарнике syslog отключён — строки идут в stderr (out).
func TestLogWritesToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	l := New("map-reader: ")
	l.out = w // перехватываем fallback-вывод
	if l.sys != nil {
		t.Fatal("в тестовом бинарнике syslog должен быть отключён")
	}
	l.Infof("синк времени МАП slot=1: установлено 10:00 (было 09:55)")
	l.Warnf("пример предупреждения")
	w.Close()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if !strings.Contains(out, "map-reader: ") {
		t.Errorf("нет префикса в выводе: %q", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Errorf("нет уровня INFO: %q", out)
	}
	if !strings.Contains(out, "синк времени МАП") {
		t.Errorf("нет текста сообщения: %q", out)
	}
	if !strings.Contains(out, "WARN ") {
		t.Errorf("нет уровня WARN: %q", out)
	}
}

func TestLevelFilter(t *testing.T) {
	l := New("x: ")
	r, w, _ := os.Pipe()
	l.out = w
	l.SetLevel(Warn)
	l.Infof("не должно быть")
	l.Errorf("должно быть")
	w.Close()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if strings.Contains(out, "не должно быть") {
		t.Errorf("INFO прошёл при пороге WARN: %q", out)
	}
	if !strings.Contains(out, "должно быть") {
		t.Errorf("ERROR отфильтрован при пороге WARN: %q", out)
	}
}
