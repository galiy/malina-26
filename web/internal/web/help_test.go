package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpDocFor(t *testing.T) {
	cases := map[string]string{
		"/":                "README",
		"/login":           "start",
		"/info":            "pages/info",
		"/logs":            "pages/logs",
		"/map":             "pages/map",
		"/map/settings":    "pages/map-settings",
		"/kes":             "pages/kes",
		"/kes/settings":    "pages/kes-settings",
		"/bms":             "pages/bms",
		"/bms/settings":    "pages/bms-settings",
		"/grid":            "pages/grid",
		"/grid/settings":   "pages/grid-settings",
		"/network":         "pages/network",
		"/system":          "pages/system",
		"/system/email":    "pages/email",
		"/system/events":   "pages/events",
		"/services":        "pages/services",
		"/stats/charts":    "pages/stats-charts",
		"/stats/tables":    "pages/stats-tables",
		"/master":          "pages/master",
		"/master/settings": "pages/master-settings",
		"/unknown":         "",
	}
	for path, want := range cases {
		if got := helpDocFor(path); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

func TestRenderHelpMarkdown(t *testing.T) {
	src := "# Заголовок\n\nАбзац с **жирным** и [ссылкой](services.md).\n\n" +
		"- один\n- два\n\n| Поле | Значение |\n|---|---|\n| IP | 10.0.0.1 |\n\n" +
		"```\nssh malina@host\n```\n"
	got := renderHelpMarkdown(src, "pages/info")
	for _, want := range []string{
		"<h1>Заголовок</h1>",
		"<strong>жирным</strong>",
		`href="/help?doc=pages/services"`,
		"<ul>",
		"<table>",
		"<th>Поле</th>",
		"<code>",
		"ssh malina@host",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет фрагмента %q в:\n%s", want, got)
		}
	}
}

func TestHelpImageURL(t *testing.T) {
	cases := map[string]string{
		"../img/pages/info.png":     "/help-asset?path=img/pages/info.png",
		"img/main/home.png":         "/help-asset?path=img/main/home.png",
		"/img/x/y.png":              "/help-asset?path=img/x/y.png",
		"https://example.org/a.png": "https://example.org/a.png",
	}
	base := map[string]string{
		"../img/pages/info.png":     "pages/info",
		"img/main/home.png":         "README",
		"/img/x/y.png":              "pages/info",
		"https://example.org/a.png": "pages/info",
	}
	for src, want := range cases {
		if got := helpImageURL(base[src], src); got != want {
			t.Errorf("%s: got %q, want %q", src, got, want)
		}
	}
}

func TestHelpHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pages", "info.md"), []byte("# Тест\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{helpDir: dir}

	rr := httptest.NewRecorder()
	s.handleHelp(rr, httptest.NewRequest(http.MethodGet, "/help?doc=pages/info", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("без входа: got %d, want 404", rr.Code)
	}

	ctx := context.WithValue(context.Background(), authKey, true)
	rr = httptest.NewRecorder()
	s.handleHelp(rr, httptest.NewRequest(http.MethodGet, "/help?doc=pages/info", nil).WithContext(ctx))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "<h1>Тест</h1>") {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}

	for _, bad := range []string{"", "../secret", "pages/../../etc/passwd", "pages/nope"} {
		rr = httptest.NewRecorder()
		s.handleHelp(rr, httptest.NewRequest(http.MethodGet, "/help?doc="+bad, nil))
		if rr.Code != http.StatusNotFound {
			t.Errorf("doc=%q: got %d, want 404", bad, rr.Code)
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, "img", "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "pages", "x.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	s.handleHelpAsset(rr, httptest.NewRequest(http.MethodGet, "/help-asset?path=img/pages/x.png", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("asset без входа: got %d, want 404", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.handleHelpAsset(rr, httptest.NewRequest(http.MethodGet, "/help-asset?path=img/pages/x.png", nil).WithContext(ctx))
	if rr.Code != http.StatusOK {
		t.Errorf("asset: got %d", rr.Code)
	}
	for _, bad := range []string{"../img/../../etc/passwd", "img/../../etc/passwd", "static/app.css"} {
		rr = httptest.NewRecorder()
		s.handleHelpAsset(rr, httptest.NewRequest(http.MethodGet, "/help-asset?path="+bad, nil).WithContext(ctx))
		if rr.Code != http.StatusNotFound {
			t.Errorf("asset %q: got %d, want 404", bad, rr.Code)
		}
	}
}
