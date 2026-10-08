package web

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/galiy/malina-26/web/internal/netcfg"
)

// validIPv4 — строка является корректным IPv4-адресом.
func validIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// validMask — IPv4-маска (например 255.255.255.0).
func validMask(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return false
	}
	m := net.IPMask(ip.To4())
	ones, bits := m.Size()
	return bits == 32 && ones >= 0
}

// validText — значение без управляющих символов/переносов строк (защита от
// инъекции в hostapd/dnsmasq/wpa_supplicant конфиги).
func validText(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

// validateNet отклоняет опасные/некорректные значения формы сети.
func validateNet(m1 netcfg.Mode1, m2 netcfg.Mode2) error {
	ips := []struct{ n, v string }{
		{"m1_lan_server", m1.LAN.ServerIP}, {"m1_lan_start", m1.LAN.Start}, {"m1_lan_end", m1.LAN.End},
		{"m1_eth_ip", m1.Eth.IP}, {"m1_eth_gw", m1.Eth.Gateway},
		{"m2_eth_server", m2.LAN.ServerIP}, {"m2_eth_start", m2.LAN.Start}, {"m2_eth_end", m2.LAN.End},
		{"m2_sta_ip", m2.STA.IP}, {"m2_sta_gw", m2.STA.Gateway},
	}
	for _, x := range ips {
		if x.v != "" && !validIPv4(x.v) {
			return fmt.Errorf("%s: не IPv4 (%q)", x.n, x.v)
		}
	}
	for _, x := range []struct{ n, v string }{
		{"m1_lan_mask", m1.LAN.Mask}, {"m1_eth_mask", m1.Eth.Mask},
		{"m2_eth_mask", m2.LAN.Mask}, {"m2_sta_mask", m2.STA.Mask},
	} {
		if x.v != "" && !validMask(x.v) {
			return fmt.Errorf("%s: не маска (%q)", x.n, x.v)
		}
	}
	for _, d := range append(append([]string{}, m1.Eth.DNS...), m2.STA.DNS...) {
		if d != "" && !validIPv4(d) {
			return fmt.Errorf("DNS: не IPv4 (%q)", d)
		}
	}
	for _, x := range []struct{ n, v string }{
		{"m1_ap_ssid", m1.AP.SSID}, {"m1_ap_pass", m1.AP.Passphrase},
		{"m2_sta_ssid", m2.STA.SSID}, {"m2_sta_pass", m2.STA.Passphrase},
	} {
		if !validText(x.v) {
			return fmt.Errorf("%s: недопустимые символы", x.n)
		}
	}
	return nil
}

// runSystemctl запускает systemctl (для «перезапустить сеть»/скана).
func runSystemctl(args ...string) error {
	return exec.Command("systemctl", args...).Run()
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	data := s.baseData(r)
	data["Active"] = "network"
	data["Mode"] = netcfg.Active()
	data["Mode1"] = netcfg.LoadMode1()
	data["Mode2"] = netcfg.LoadMode2()
	lang := s.lang(r)
	if ok := r.URL.Query().Get("ok"); ok != "" {
		data["Ok"] = s.tr(lang, "net.msg."+ok+"_ok")
	}
	s.render(w, http.StatusOK, "network", data)
}

func (s *Server) handleNetworkSave(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	f := func(k, def string) string {
		if v := strings.TrimSpace(r.FormValue(k)); v != "" {
			return v
		}
		return def
	}
	ch := 0 // 0 = auto
	if v := strings.TrimSpace(r.FormValue("m1_ap_channel")); v != "" {
		n := 0
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		ch = n
	}
	m1 := netcfg.Mode1{
		AP: netcfg.AP{
			SSID:       f("m1_ap_ssid", "Malina"),
			Passphrase: f("m1_ap_pass", ""),
			Country:    f("m1_ap_country", "US"),
			Channel:    ch,
		},
		LAN: netcfg.DHCP{
			ServerIP: f("m1_lan_server", "10.10.100.254"),
			Mask:     f("m1_lan_mask", "255.255.255.0"),
			Start:    f("m1_lan_start", "10.10.100.1"),
			End:      f("m1_lan_end", "10.10.100.200"),
		},
		Eth: netcfg.Eth{
			Mode:    f("m1_eth_mode", "dhcp"),
			IP:      f("m1_eth_ip", ""),
			Mask:    f("m1_eth_mask", ""),
			Gateway: f("m1_eth_gw", ""),
			DNS:     splitList(r.FormValue("m1_eth_dns")),
		},
		NAT:    r.FormValue("m1_nat") != "",
		Portal: r.FormValue("m1_portal") != "",
	}
	m2 := netcfg.Mode2{
		STA: netcfg.STA{
			SSID:       f("m2_sta_ssid", ""),
			Passphrase: f("m2_sta_pass", ""),
			Security:   f("m2_sta_security", "wpa2"),
			IPMode:     f("m2_sta_ipmode", "dhcp"),
			IP:         f("m2_sta_ip", ""),
			Mask:       f("m2_sta_mask", ""),
			Gateway:    f("m2_sta_gw", ""),
			DNS:        splitList(r.FormValue("m2_sta_dns")),
		},
		LAN: netcfg.DHCP{
			ServerIP: f("m2_eth_server", "10.10.100.254"),
			Mask:     f("m2_eth_mask", "255.255.255.0"),
			Start:    f("m2_eth_start", "10.10.100.1"),
			End:      f("m2_eth_end", "10.10.100.200"),
		},
		Serve: r.FormValue("m2_serve") != "",
	}
	if err := validateNet(m1, m2); err != nil {
		http.Error(w, "некорректные параметры сети: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = netcfg.SaveMode1(m1)
	_ = netcfg.SaveMode2(m2)
	_ = netcfg.SetActive(f("mode", netcfg.Active()))
	// «Сохранить» = сохранить и применить (перезапуск сети).
	_ = runSystemctl("restart", "--no-block", "malina-net.service")
	http.Redirect(w, r, "/network?ok=save", http.StatusSeeOther)
}

func (s *Server) handleNetworkScan(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAuth(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = runSystemctl("start", "--no-block", "malina-net-scan.service")
	http.Redirect(w, r, "/network?ok=scan", http.StatusSeeOther)
}

func (s *Server) handleAPIScan(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, err := os.ReadFile("/run/malina-net/scan.json")
	if err != nil {
		_, _ = w.Write([]byte("[]"))
		return
	}
	_, _ = w.Write(b)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
