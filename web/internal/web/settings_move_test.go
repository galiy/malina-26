package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Проверка переноса настроек из «Параметры системы»: синхронизация времени МАП
// объединена в форму «Настройка МАП», аккумулятор телеметрии — на странице
// «Настройки статистики» сверху, а системная страница их не содержит.
func TestSettingsMoveRender(t *testing.T) {
	dir := t.TempDir()
	mapCfg := filepath.Join(dir, "map-reader.json")
	offPath := filepath.Join(dir, "map-time-sync.off")
	srv, err := New(Options{
		AssetsDir:           "../../assets",
		HelpDir:             filepath.Join(dir, "help"),
		AuthStorePath:       filepath.Join(dir, "auth"),
		MapReaderConfigPath: mapCfg,
		MapTimeSyncOff:      offPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Страница «Настройка МАП» — синхронизация времени МАП объединена в форму.
	w := httptest.NewRecorder()
	srv.handleMapSettings(w, authedReq(http.MethodGet, "/map/settings", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("/map/settings: code=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="map_maptime_enabled"`) {
		t.Fatalf("/map/settings: нет поля синхронизации времени МАП:\n%s", body)
	}
	if strings.Contains(body, `id="sec-acc"`) || strings.Contains(body, `id="sec-maptime"`) {
		t.Fatalf("/map/settings: не должно быть отдельных карточек секций")
	}

	// Объединённое сохранение: один POST сохраняет и опрос/лог, и флаг времени.
	form := url.Values{
		"map_enabled":         {"on"},
		"map_log_level":       {"debug"},
		"map_maptime_enabled": {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/map/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAuthedContext(req).Context())
	w = httptest.NewRecorder()
	srv.handleMapSettings(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /map/settings: code=%d body=%s", w.Code, w.Body.String())
	}
	cfg, err := os.ReadFile(mapCfg)
	if err != nil || !strings.Contains(string(cfg), `"debug"`) {
		t.Fatalf("конфиг map-reader не сохранён: %v %q", err, cfg)
	}
	if timeSyncEnabled(offPath) != true {
		t.Fatalf("синхронизация времени МАП должна быть включена (флаг отсутствует)")
	}

	// Выключение: POST без map_maptime_enabled создаёт флаг-файл.
	form = url.Values{"map_enabled": {"on"}, "map_log_level": {"info"}}
	req = httptest.NewRequest(http.MethodPost, "/map/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(newAuthedContext(req).Context())
	w = httptest.NewRecorder()
	srv.handleMapSettings(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /map/settings (off): code=%d", w.Code)
	}
	if timeSyncEnabled(offPath) != false {
		t.Fatalf("синхронизация времени МАП должна быть выключена (флаг создан)")
	}

	// Страница «Настройки статистики» — аккумулятор телеметрии наверху.
	w = httptest.NewRecorder()
	srv.handleStatsSettings(w, authedReq(http.MethodGet, "/stats/settings", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("/stats/settings: code=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	if !strings.Contains(body, `id="sec-acc"`) || !strings.Contains(body, "acc_period") {
		t.Fatalf("/stats/settings: нет секции аккумулятора телеметрии:\n%s", body)
	}
	posAcc := strings.Index(body, `id="sec-acc"`)
	posArch := strings.Index(body, `action="/stats/settings"`)
	if posAcc < 0 || posArch < 0 || posAcc > posArch {
		t.Fatalf("/stats/settings: аккумулятор телеметрии должен быть выше резервной копии (acc=%d arch=%d)", posAcc, posArch)
	}
	if strings.Contains(body, `id="sec-maptime"`) {
		t.Fatalf("/stats/settings: не должна содержать синхронизацию времени МАП")
	}

	// Системная страница больше не содержит обе перенесённые секции.
	w = httptest.NewRecorder()
	srv.handleSystem(w, authedReq(http.MethodGet, "/system", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("/system: code=%d", w.Code)
	}
	body = w.Body.String()
	if strings.Contains(body, `id="sec-maptime"`) || strings.Contains(body, `id="sec-acc"`) {
		t.Fatalf("/system: не должна содержать перенесённые секции:\n%s", body)
	}
}
