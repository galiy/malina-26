package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/galiy/malina-26/web/internal/mailer"
)

// providerList — упорядоченный список пресетов для формы.
func providerList() []map[string]any {
	var out []map[string]any
	for _, k := range mailer.ProviderKeys {
		p := mailer.Providers[k]
		out = append(out, map[string]any{
			"Key": k, "Host": p.Host, "Port": p.Port, "Security": string(p.Security),
		})
	}
	return out
}

func (s *Server) emailData(r *http.Request) map[string]any {
	data := s.baseData(r)
	data["Active"] = "email"
	cfg, _ := s.email.LoadConfig()
	if cfg.Provider == "" {
		cfg.Provider = "mailru"
	}
	data["Cfg"] = cfg
	data["SecretSet"] = cfg.SecretEnc != ""
	data["Providers"] = providerList()
	if list, err := s.email.Outbox.List(); err == nil {
		data["Outbox"] = list
	}
	return data
}

// handleEmail — «Система → Настройка E-Mail».
func (s *Server) handleEmail(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	noFrameHeaders(w)
	lang := s.lang(r)
	data := s.emailData(r)
	switch r.URL.Query().Get("ok") {
	case "save":
		data["Ok"] = s.tr(lang, "email.ok_save")
	case "test":
		data["Ok"] = s.tr(lang, "email.ok_test")
	case "outbox":
		data["Ok"] = s.tr(lang, "email.ok_outbox")
	}
	if e := r.URL.Query().Get("err"); e != "" {
		data["Err"] = e
	}
	s.render(w, http.StatusOK, "email", data)
}

// handleEmailSave — сохранить настройки (пустой secret = не менять).
func (s *Server) handleEmailSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	cfg := mailer.Config{
		Provider:  strings.TrimSpace(r.FormValue("provider")),
		From:      strings.TrimSpace(r.FormValue("from")),
		Username:  strings.TrimSpace(r.FormValue("username")),
		Host:      strings.TrimSpace(r.FormValue("host")),
		Security:  strings.TrimSpace(r.FormValue("security")),
		Recipient: strings.TrimSpace(r.FormValue("recipient")),
	}
	cfg.Port, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	// Для пресетов логин = адрес почты (провайдеры требуют полный адрес).
	if cfg.Provider != "custom" {
		cfg.Username = cfg.From
	}
	secret := r.FormValue("secret")
	if secret == "" {
		if old, err := s.email.LoadConfig(); err == nil {
			cfg.SecretEnc = old.SecretEnc
		}
	}
	if err := s.email.SaveConfig(cfg, secret); err != nil {
		s.audit.Event("email_settings", map[string]any{"result": "err", "error": err.Error(), "ip": clientIP(r)})
		data := s.emailData(r)
		data["Cfg"] = cfg
		data["SecretSet"] = cfg.SecretEnc != ""
		data["Err"] = err.Error()
		s.render(w, http.StatusOK, "email", data)
		return
	}
	s.audit.Event("email_settings", map[string]any{
		"result": "ok", "provider": cfg.Provider, "recipient": cfg.Recipient, "ip": clientIP(r),
	})
	http.Redirect(w, r, "/system/email?ok=save", http.StatusSeeOther)
}

// handleEmailTest — отправить тестовое письмо (синхронно).
func (s *Server) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lang := s.lang(r)
	if err := s.email.SendNow(s.tr(lang, "email.test_subject"), s.tr(lang, "email.test_body"), false); err != nil {
		s.audit.Event("email_send", map[string]any{"kind": "test", "result": "err", "error": err.Error(), "ip": clientIP(r)})
		http.Redirect(w, r, "/system/email?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.audit.Event("email_send", map[string]any{"kind": "test", "result": "ok", "ip": clientIP(r)})
	http.Redirect(w, r, "/system/email?ok=test", http.StatusSeeOther)
}

// handleEmailOutbox — удалить письмо или очистить аутбокс.
func (s *Server) handleEmailOutbox(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	switch r.FormValue("action") {
	case "clear":
		_ = s.email.Outbox.Clear()
	case "delete":
		_ = s.email.Outbox.Delete(r.FormValue("id"))
	}
	http.Redirect(w, r, "/system/email?ok=outbox", http.StatusSeeOther)
}
