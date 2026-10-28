package syslogx

import (
	"os"
	"testing"
)

// В тестовом бинарнике syslog отключён — пишем в stderr.
func TestRawFallsBackToStderr(t *testing.T) {
	w := Raw("malina-web")
	if _, ok := w.(*os.File); !ok {
		t.Fatalf("ожидался stderr (os.File) в тестовом бинарнике, получен %T", w)
	}
}

func TestUseSyslogEnvSwitch(t *testing.T) {
	if useSyslog() {
		t.Fatal("в тестовом бинарнике useSyslog должен быть false")
	}
}
