package web

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/galiy/malina-26/web/internal/accumulator"
)

type archNetwork struct {
	Protocol  string `json:"protocol"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Path      string `json:"path"`
	User      string `json:"user"`
	Password  string `json:"password_enc"`
	Domain    string `json:"domain"`
	HostKey   string `json:"host_key"`
	AccessSec int    `json:"access_timeout_sec"`
}
type archFlash struct {
	UUID string `json:"uuid"`
}
type archConfig struct {
	Enabled    bool        `json:"enabled"`
	Target     string      `json:"target"`
	AvgScanMin int         `json:"avg_scan_min"`
	LogLevel   string      `json:"log_level"`
	Network    archNetwork `json:"network"`
	Flash      archFlash   `json:"flash"`
}

func defaultArchConfig() archConfig {
	return archConfig{Target: "network", AvgScanMin: 5, LogLevel: "info", Network: archNetwork{Protocol: "sftp", Port: 22, AccessSec: 10}}
}

func loadArchConfig(path string) archConfig {
	c := defaultArchConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	if c.AvgScanMin <= 0 {
		c.AvgScanMin = 5
	}
	if c.Target == "" {
		c.Target = "network"
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		c.LogLevel = "info"
	}
	return c
}

func saveArchConfig(path string, c archConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// archAAD — детерминированный AAD (совпадает с daemons/malina-arch).
func archAAD(protocol, field string) string {
	return "malina-arch/" + protocol + "/" + field + "/v1"
}

func archEncrypt(keyPath, field, plain string) (string, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}
	if len(key) != 32 {
		return "", errors.New("bad arch.key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, g.Seal(nil, nonce, []byte(plain), []byte(field))...)), nil
}

type flashCandidate struct {
	UUID  string
	Size  string
	Model string
}

func blockParent(dev string) string {
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", dev))
	if err != nil {
		return dev
	}
	if _, err := os.Stat(filepath.Join(p, "partition")); err == nil {
		return filepath.Base(filepath.Dir(p))
	}
	return dev
}

func rootBlockParents() (map[string]bool, error) {
	out := map[string]bool{}
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return out, err
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || (f[1] != "/" && f[1] != "/malina") {
			continue
		}
		link, err := filepath.EvalSymlinks(f[0])
		if err != nil {
			continue
		}
		out[blockParent(filepath.Base(link))] = true
	}
	return out, nil
}

func externalBlock(parent string) bool {
	if b, err := os.ReadFile(filepath.Join("/sys/block", parent, "removable")); err == nil {
		if strings.TrimSpace(string(b)) == "1" {
			return true
		}
	}
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/block", parent, "device"))
	if err != nil {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, "usb") {
			return true
		}
	}
	return false
}

func flashCandidates() []flashCandidate {
	roots, err := rootBlockParents()
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir("/dev/disk/by-uuid")
	if err != nil {
		return nil
	}
	var out []flashCandidate
	for _, e := range ents {
		uuid := e.Name()
		link, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", uuid))
		if err != nil {
			continue
		}
		dev := filepath.Base(link)
		parent := blockParent(dev)
		if roots[parent] || !externalBlock(parent) {
			continue
		}
		size := ""
		if b, err := os.ReadFile(filepath.Join("/sys/class/block", dev, "size")); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
				size = fmt.Sprintf("%.1f GiB", float64(n)*512/1024/1024/1024)
			}
		}
		model := ""
		if b, err := os.ReadFile(filepath.Join("/sys/block", parent, "device/model")); err == nil {
			model = strings.TrimSpace(string(b))
		}
		out = append(out, flashCandidate{UUID: uuid, Size: size, Model: model})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func archCall(sock string, req map[string]any) map[string]any {
	return archCallTimeout(sock, req, 35*time.Second)
}

func archCallTimeout(sock string, req map[string]any, d time.Duration) map[string]any {
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(d))
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	var out map[string]any
	if err := json.NewDecoder(c).Decode(&out); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return out
}

func sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Host == r.Host
	}
	// Без Origin и Referer запрос не может быть признан своим: браузерные
	// POST-формы эти заголовки присылают. Не-браузерные клиенты должны
	// использовать API, а не HTML-форму.
	return false
}

func (s *Server) handleStatsSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	cfg := loadArchConfig(s.opts.ArchConfigPath)
	if r.Method == http.MethodPost {
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_ = r.ParseForm()
		action := r.FormValue("action")
		switch action {
		case "save":
			oldProto := cfg.Network.Protocol
			hadPassword := cfg.Network.Password != ""
			// enabled меняется только командами enable/disable (диалог активации).
			cfg.Target = strings.TrimSpace(r.FormValue("target"))
			if cfg.Target != "flash" {
				cfg.Target = "network"
			}
			if n, _ := strconv.Atoi(r.FormValue("avg_scan_min")); n > 0 {
				cfg.AvgScanMin = n
			}
			cfg.LogLevel = strings.TrimSpace(r.FormValue("log_level"))
			switch cfg.LogLevel {
			case "debug", "info", "warn", "error":
			default:
				cfg.LogLevel = "info"
			}
			cfg.Network.Protocol = strings.TrimSpace(r.FormValue("protocol"))
			if cfg.Network.Protocol != "smb2" {
				cfg.Network.Protocol = "sftp"
			}
			cfg.Network.Host = strings.TrimSpace(r.FormValue("host"))
			if n, _ := strconv.Atoi(r.FormValue("port")); n > 0 {
				cfg.Network.Port = n
			}
			cfg.Network.Path = strings.TrimSpace(r.FormValue("path"))
			cfg.Network.User = strings.TrimSpace(r.FormValue("user"))
			cfg.Network.Domain = strings.TrimSpace(r.FormValue("domain"))
			cfg.Network.HostKey = strings.TrimSpace(r.FormValue("host_key"))
			if n, _ := strconv.Atoi(r.FormValue("access_timeout_sec")); n > 0 {
				cfg.Network.AccessSec = n
			}
			cfg.Flash.UUID = strings.TrimSpace(r.FormValue("flash_uuid"))
			pw := r.FormValue("password")
			if pw != "" {
				enc, err := archEncrypt(s.opts.ArchKeyPath, archAAD(cfg.Network.Protocol, "password"), pw)
				if err != nil {
					s.redirectStatsSettings(w, r, "err=key")
					return
				}
				cfg.Network.Password = enc
			}
			// AAD привязан к протоколу: старый шифртекст после смены
			// протокола не расшифруется, поэтому пароль нужно ввести заново.
			if cfg.Target == "network" && pw == "" && hadPassword && oldProto != cfg.Network.Protocol {
				s.redirectStatsSettings(w, r, "err=protocol_password")
				return
			}
			if cfg.Target == "network" {
				if cfg.Network.Host == "" || cfg.Network.Path == "" || cfg.Network.User == "" {
					s.redirectStatsSettings(w, r, "err=validation")
					return
				}
				if cfg.Network.Port < 1 || cfg.Network.Port > 65535 {
					s.redirectStatsSettings(w, r, "err=validation")
					return
				}
				if cfg.Network.AccessSec < 1 || cfg.Network.AccessSec > 600 {
					s.redirectStatsSettings(w, r, "err=validation")
					return
				}
			} else {
				ok := false
				for _, c := range flashCandidates() {
					if c.UUID == cfg.Flash.UUID {
						ok = true
						break
					}
				}
				if !ok {
					s.redirectStatsSettings(w, r, "err=validation")
					return
				}
			}
			if cfg.AvgScanMin < 1 || cfg.AvgScanMin > 1440 {
				s.redirectStatsSettings(w, r, "err=validation")
				return
			}
			if err := saveArchConfig(s.opts.ArchConfigPath, cfg); err != nil {
				s.redirectStatsSettings(w, r, "err=save")
				return
			}
			s.audit.Event("arch_settings", map[string]any{"what": "save", "result": "ok", "ip": clientIP(r)})
			res := archCall(s.opts.ArchControlSock, map[string]any{"cmd": "reload"})
			if ok, _ := res["ok"].(bool); !ok {
				s.redirectStatsSettings(w, r, "err=ctrl")
				return
			}
			s.redirectStatsSettings(w, r, "ok=save")
			return
		case "test", "disable", "enable_restore", "enable_reset", "enable_auto", "cancel":
			var req map[string]any
			switch action {
			case "test":
				req = map[string]any{"cmd": "test"}
			case "disable":
				req = map[string]any{"cmd": "disable"}
			case "enable_restore":
				req = map[string]any{"cmd": "enable", "mode": "restore"}
			case "enable_reset":
				req = map[string]any{"cmd": "enable", "mode": "reset"}
			case "enable_auto":
				req = map[string]any{"cmd": "enable", "mode": "auto"}
			case "cancel":
				req = map[string]any{"cmd": "cancel"}
			}
			res := archCall(s.opts.ArchControlSock, req)
			okv, _ := res["ok"].(bool)
			if action == "enable_auto" {
				if !okv {
					why, _ := res["error"].(string)
					s.redirectStatsSettings(w, r, "err=ctrl&why="+url.QueryEscape(clipErr(why)))
					return
				}
				st, _ := res["state"].(string)
				switch st {
				case "restore_required":
					s.redirectStatsSettings(w, r, "ok=need_restore")
				case "continued":
					s.redirectStatsSettings(w, r, "ok=continued")
				default:
					s.redirectStatsSettings(w, r, "ok=enabled")
				}
				return
			}
			if action == "test" {
				if okv {
					raw, _ := res["raw_archive"].(bool)
					avg, _ := res["avg_archive"].(bool)
					rstat, _ := res["raw_status"].(string)
					astat, _ := res["avg_status"].(string)
					if rstat == "error" || astat == "error" {
						s.redirectStatsSettings(w, r, "err=test_read")
						return
					}
					switch {
					case raw && avg:
						s.redirectStatsSettings(w, r, "ok=test_both")
					case raw:
						s.redirectStatsSettings(w, r, "ok=test_raw_only")
					case avg:
						s.redirectStatsSettings(w, r, "ok=test_avg_only")
					default:
						s.redirectStatsSettings(w, r, "ok=test_none")
					}
				} else {
					why, _ := res["error"].(string)
					s.redirectStatsSettings(w, r, "err=test&why="+url.QueryEscape(clipErr(why)))
				}
				return
			}
			if !okv {
				why, _ := res["error"].(string)
				s.redirectStatsSettings(w, r, "err=ctrl&why="+url.QueryEscape(clipErr(why)))
				return
			}
			s.audit.Event("arch_settings", map[string]any{"what": action, "result": "ok", "ip": clientIP(r)})
			s.redirectStatsSettings(w, r, "ok="+action)
			return
		}
	}
	data := s.statsBaseData(r)
	data["Active"] = "stats_settings"
	data["Cfg"] = cfg
	data["Status"] = archCallTimeout(s.opts.ArchControlSock, map[string]any{"cmd": "status"}, 3*time.Second)
	data["FlashCandidates"] = flashCandidates()
	// Аккумулятор телеметрии (усреднение) живёт на странице настройки статистики.
	acc := accumulator.Load(s.opts.AccConfigPath)
	data["AccEnabled"] = acc.Enabled
	data["AccPeriod"] = acc.PeriodMin
	data["AccPeriods"] = []int{1, 2, 3, 4, 5, 6, 10}
	data["Ok"] = r.URL.Query().Get("ok")
	data["Err"] = r.URL.Query().Get("err")
	data["ErrText"] = r.URL.Query().Get("why")
	data["RawArchive"] = r.URL.Query().Get("raw") == "1"
	data["AvgArchive"] = r.URL.Query().Get("avg") == "1"
	s.render(w, http.StatusOK, "stats_settings", data)
}

func b01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func clipErr(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

func (s *Server) redirectStatsSettings(w http.ResponseWriter, r *http.Request, q string) {
	http.Redirect(w, r, "/stats/settings?"+q, http.StatusSeeOther)
}
