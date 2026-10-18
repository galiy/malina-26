package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStatusFresh(t *testing.T) {
	now := int64(1000)
	if statusFresh(now-10, now) != 1 || statusFresh(now-61, now) != 0 || statusFresh(0, now) != 0 {
		t.Fatal("freshness")
	}
}

func TestSettingsStatusAuthAndBadType(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleAPISettingsStatus(w, httptest.NewRequest("GET", "/api/settings/status?type=map", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handleAPISettingsStatus(w, authedReq("GET", "/api/settings/status?type=x", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad type code=%d", w.Code)
	}
}

func TestSettingsStatusI18nParity(t *testing.T) {
	keys := []string{"settings.status_title", "settings.status.empty", "settings.status.id", "settings.status.serial", "settings.status.state", "settings.status.last_seen", "settings.status.proto_microart", "settings.status.proto_modbus"}
	for _, lang := range []string{"ru", "en", "be", "zh", "ka", "kk", "uz", "tg"} {
		b, err := os.ReadFile(filepath.Join("../../assets/i18n", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range keys {
			if m[k] == "" {
				t.Fatalf("%s: нет ключа %s", lang, k)
			}
		}
	}
}
