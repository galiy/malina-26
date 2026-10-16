package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/galiy/malina-26/web/internal/netcfg"
)

const runDir = "/run/malina-net"

// apIdleRestart — если у AP нет ни одной подключённой станции дольше этого
// времени, hostapd перезапускается профилактически: brcmfmac в AP-режиме иногда
// «залипает» (beacon есть, но ассоциации не принимаются) без видимых ошибок.
// Порог намеренно поднят до 2 часов: при пустой сети ежеминутные рестарты
// дают всплеск kworker/сетевого стека вхолостую (см. docs по наблюдаемости).
const apIdleRestart = 2 * time.Hour

func main() {
	log.SetPrefix("malina-netd: ")
	if len(os.Args) < 2 {
		log.Fatalf("usage: malina-netd apply|scan|status|fallback-tick")
	}
	switch os.Args[1] {
	case "apply":
		if err := apply(); err != nil {
			log.Fatalf("apply: %v", err)
		}
	case "scan":
		if err := scan(); err != nil {
			log.Fatalf("scan: %v", err)
		}
	case "status":
		if err := status(); err != nil {
			log.Fatalf("status: %v", err)
		}
	case "monitor":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := monitor(ctx); err != nil {
			log.Fatalf("monitor: %v", err)
		}
	case "wifi-watch":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := wifiWatch(ctx); err != nil {
			log.Fatalf("wifi-watch: %v", err)
		}
	case "probe":
		m := netcfg.LoadMode2()
		f := probeForeignDHCP("eth0", m.LAN.ServerIP, 15*time.Second)
		fmt.Printf("foreign=%v\n", f)
	case "fallback-tick":
		if err := fallbackTick(); err != nil {
			log.Fatalf("fallback-tick: %v", err)
		}
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

// sleepCtx спит d; false — если контекст отменён (graceful stop).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeFile(path, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func sysctl(key, val string) error { return run("sysctl", "-w", key+"="+val) }

// activeMode учитывает транзиентный override (фолбэк) из /run.
func activeMode() string {
	if b, err := os.ReadFile(filepath.Join(runDir, "mode")); err == nil {
		if s := strings.TrimSpace(string(b)); s == "mode1" || s == "mode2" {
			return s
		}
	}
	return netcfg.Active()
}

func apply() error {
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	// IPv6 не поднимаем.
	_ = sysctl("net.ipv6.conf.all.disable_ipv6", "1")
	_ = sysctl("net.ipv6.conf.default.disable_ipv6", "1")

	mode := activeMode()
	log.Printf("applying %s", mode)
	switch mode {
	case "mode1":
		return applyMode1()
	case "mode2":
		return applyMode2()
	}
	return nil
}

func applyMode1() error {
	m := netcfg.LoadMode1()

	// Wi-Fi: разблокировать, регион.
	_ = run("/usr/sbin/rfkill", "unblock", "wifi")
	if m.AP.Country != "" {
		_ = run("/usr/sbin/iw", "reg", "set", m.AP.Country)
	}
	_ = run("ip", "link", "set", "wlan0", "up")

	// Ethernet.
	if err := applyEth(m.Eth, "eth0"); err != nil {
		return err
	}

	// Точка доступа.
	if strings.TrimSpace(m.AP.SSID) == "" {
		return fmt.Errorf("AP SSID пуст")
	}
	if strings.TrimSpace(m.AP.Passphrase) == "" {
		return fmt.Errorf("AP passphrase пуст (задайте в настройках сети)")
	}
	mask := m.LAN.Mask
	ch := m.AP.Channel
	if ch <= 0 {
		ch = 6 // auto -> 6 (ACS на brcmfmac не используем)
	}
	hostapd := fmt.Sprintf(`interface=wlan0
driver=nl80211
ctrl_interface=%s/hostapd
ssid=%s
hw_mode=g
channel=%d
country_code=%s
ieee80211d=1
wpa=2
wpa_passphrase=%s
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
`, runDir, m.AP.SSID, ch, m.AP.Country, m.AP.Passphrase)
	if err := os.MkdirAll(filepath.Join(runDir, "hostapd"), 0o700); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(runDir, "hostapd.conf"), hostapd, 0o600); err != nil {
		return err
	}
	var db strings.Builder
	fmt.Fprintf(&db, "interface=wlan0\nbind-interfaces\nexcept-interface=eth0\nno-dhcp-interface=eth0\n")
	fmt.Fprintf(&db, "dhcp-leasefile=%s/dnsmasq.leases\n", runDir)
	fmt.Fprintf(&db, "dhcp-range=%s,%s,%s,2h\n", m.LAN.Start, m.LAN.End, mask)
	fmt.Fprintf(&db, "dhcp-option=3,%s\ndhcp-option=6,%s\n", m.LAN.ServerIP, m.LAN.ServerIP)
	if m.Portal {
		// RFC 8910 (Captive-Portal URL) + перехват probe-доменов клиентов.
		fmt.Fprintf(&db, "dhcp-option=114,http://%s/\n", m.LAN.ServerIP)
		for _, d := range []string{
			"connectivitycheck.gstatic.com",
			"connectivitycheck.android.com",
			"clients3.google.com",
			"captive.apple.com",
			"www.msftconnecttest.com",
			"msftconnecttest.com",
			"www.msftncsi.com",
			"msftncsi.com",
			"detectportal.firefox.com",
			"network-test.debian.org",
			"nmcheck.gnome.org",
		} {
			fmt.Fprintf(&db, "address=/%s/%s\n", d, m.LAN.ServerIP)
		}
	}
	db.WriteString("log-dhcp\n")
	if err := writeFile(filepath.Join(runDir, "dnsmasq.conf"), db.String(), 0o600); err != nil {
		return err
	}
	_ = run("systemctl", "stop", "malina-wpa.service")
	_ = run("systemctl", "stop", "malina-dhclient-wlan0.service")
	_ = run("systemctl", "stop", "malina-net-monitor.service")
	_ = os.Remove(filepath.Join(runDir, "monitor.state"))
	_ = os.Remove(filepath.Join(runDir, "serving"))
	_ = os.Remove(filepath.Join(runDir, "nolink.ts"))
	_ = run("systemctl", "restart", "malina-hostapd.service")
	// Адрес сервера/шлюза на AP-интерфейсе (после старта hostapd).
	_ = run("ip", "addr", "flush", "dev", "wlan0")
	_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", m.LAN.ServerIP, maskToPrefix(m.LAN.Mask)), "dev", "wlan0")
	// brcmfmac: с включённым power save AP-режим деградирует (низкий RSSI,
	// обрывы клиентов). Отключаем энергосбережение на AP-интерфейсе.
	_ = run("/usr/sbin/iw", "dev", "wlan0", "set", "power_save", "off")
	_ = run("systemctl", "restart", "malina-dnsmasq.service")

	// NAT/транзит.
	if m.NAT {
		_ = sysctl("net.ipv4.ip_forward", "1")
		rules := `table ip malina {
  chain forward { type filter hook forward priority 0; policy accept; }
  chain postrouting { type nat hook postrouting priority srcnat; policy accept; oifname "eth0" masquerade; }
}
`
		if err := writeFile(filepath.Join(runDir, "ruleset.nft"), rules, 0o600); err == nil {
			// Сбрасываем таблицу целиком: nft -f по существующей таблице
			// дописывает правила, а не заменяет их.
			_ = run("nft", "delete", "table", "ip", "malina")
			_ = run("nft", "-f", filepath.Join(runDir, "ruleset.nft"))
		}
	} else {
		_ = sysctl("net.ipv4.ip_forward", "0")
		_ = run("nft", "delete", "table", "ip", "malina")
	}

	// Опция «Выключить Wi‑Fi при подключённом Ethernet» (mode1-only):
	// AP уже поднят выше → помечаем состояние «on», затем, если флаг включён,
	// сразу применяем нужное состояние и запускаем сторожевой цикл.
	_ = writeFile(wifiStateFile, "on", 0o644)
	if m.WifiOffOnEth {
		wifiApApply(wifiApDesired(m), m)
		_ = run("systemctl", "start", "malina-net-wifi.service")
	} else {
		_ = run("systemctl", "stop", "malina-net-wifi.service")
		_ = os.Remove(wifiStateFile)
	}
	return nil
}

func applyMode2() error {
	m := netcfg.LoadMode2()
	if strings.TrimSpace(m.STA.SSID) == "" {
		return fmt.Errorf("STA SSID пуст (задайте сеть в настройках)")
	}
	_ = run("/usr/sbin/rfkill", "unblock", "wifi")
	_ = run("/usr/sbin/iw", "reg", "set", "US")

	// Гасим AP и наш DHCP на wlan0, снимаем адрес AP с интерфейса и чистим
	// конфиги AP: в STA wlan0 уходит в чужую сеть, раздача 10.10.100.x туда
	// недопустима (чужой DHCP/конфликт подсетей).
	_ = run("systemctl", "stop", "--no-block", "malina-hostapd.service")
	_ = run("systemctl", "stop", "--no-block", "malina-dnsmasq.service")
	_ = run("ip", "addr", "flush", "dev", "wlan0")
	_ = os.Remove(filepath.Join(runDir, "dnsmasq.conf"))
	_ = os.Remove(filepath.Join(runDir, "dnsmasq.leases"))
	_ = os.Remove(filepath.Join(runDir, "hostapd.conf"))
	// Опция «Выключать Wi‑Fi при Ethernet» — только для mode1.
	_ = run("systemctl", "stop", "malina-net-wifi.service")
	_ = os.Remove(wifiStateFile)

	// STA (wpa_supplicant).
	wpa := fmt.Sprintf(`ctrl_interface=/run/wpa_supplicant
update_config=1
network={
	ssid="%s"
	psk="%s"
	key_mgmt=WPA-PSK
}
`, m.STA.SSID, m.STA.Passphrase)
	if err := writeFile(filepath.Join(runDir, "wpa_supplicant.conf"), wpa, 0o600); err != nil {
		return err
	}
	_ = run("ip", "link", "set", "wlan0", "up")
	_ = run("systemctl", "restart", "malina-wpa.service")

	wprefix := 24
	if m.STA.Mask != "" {
		wprefix = maskToPrefix(m.STA.Mask)
	}
	if m.STA.IPMode == "static" && m.STA.IP != "" {
		_ = run("ip", "addr", "flush", "dev", "wlan0")
		_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", m.STA.IP, wprefix), "dev", "wlan0")
		if m.STA.Gateway != "" {
			_ = run("ip", "route", "replace", "default", "via", m.STA.Gateway, "dev", "wlan0")
		}
	} else {
		_ = run("systemctl", "restart", "malina-dhclient-wlan0.service")
	}

	// Безопасность: если STA не подключился — откат в режим 1.
	// Для статического IP наличие адреса не означает ассоциацию, поэтому
	// проверяем фактическую привязку (iw link).
	ok := false
	if m.STA.IPMode == "static" && m.STA.IP != "" {
		ok = waitSTA(30 * time.Second)
	} else {
		ok = waitIPv4("wlan0", 30*time.Second)
	}
	if !ok {
		log.Printf("mode2: STA не подключился за 30с — откат в mode1")
		_ = os.Remove(filepath.Join(runDir, "mode"))
		_ = run("systemctl", "stop", "--no-block", "malina-wpa.service")
		_ = run("systemctl", "stop", "--no-block", "malina-dhclient-wlan0.service")
		return applyMode1()
	}
	return applyEthMode2(m)
}

// wifiStateFile — текущее состояние AP в mode1 при опции «Выключить Wi‑Fi»:
// "on" (AP работает) | "off" (AP погашен, есть Ethernet).
const wifiStateFile = runDir + "/wifi.state"

// wifiApDesired: должен ли AP быть включён с учётом флага WifiOffOnEth.
func wifiApDesired(m netcfg.Mode1) bool {
	if !m.WifiOffOnEth {
		return true
	}
	return carrier("eth0") != "1"
}

func wifiApState() string {
	b, err := os.ReadFile(wifiStateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// wifiApApply идемпотентно включает/выключает AP (hostapd+dnsmasq+радио).
func wifiApApply(on bool, m netcfg.Mode1) {
	want := "off"
	if on {
		want = "on"
	}
	if wifiApState() == want {
		return
	}
	if on {
		_ = run("/usr/sbin/rfkill", "unblock", "wifi")
		_ = run("ip", "link", "set", "wlan0", "up")
		_ = run("systemctl", "restart", "malina-hostapd.service")
		_ = run("ip", "addr", "flush", "dev", "wlan0")
		_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", m.LAN.ServerIP, maskToPrefix(m.LAN.Mask)), "dev", "wlan0")
		_ = run("/usr/sbin/iw", "dev", "wlan0", "set", "power_save", "off")
		_ = run("systemctl", "restart", "malina-dnsmasq.service")
		_ = writeFile(wifiStateFile, "on", 0o644)
		log.Printf("wifi: AP поднят (Ethernet не подключён)")
	} else {
		_ = run("systemctl", "stop", "malina-dnsmasq.service")
		_ = run("systemctl", "stop", "malina-hostapd.service")
		_ = run("ip", "addr", "flush", "dev", "wlan0")
		_ = run("ip", "link", "set", "wlan0", "down")
		_ = run("/usr/sbin/rfkill", "block", "wifi")
		_ = writeFile(wifiStateFile, "off", 0o644)
		log.Printf("wifi: AP погашен (подключён Ethernet)")
	}
}

// wifiWatch — сторожевой цикл: держит AP в соответствии с carrier(eth0).
// Debounce: смена состояния только после 3 подряд одинаковых замеров.
func wifiWatch(ctx context.Context) error {
	last, stable := wifiApState() == "on", 0
	for {
		if ctx.Err() != nil {
			log.Printf("wifi-watch: остановка")
			return nil
		}
		m := netcfg.LoadMode1()
		if !m.WifiOffOnEth {
			log.Printf("wifi-watch: флаг выключен — выход")
			return nil
		}
		desired := wifiApDesired(m)
		if desired == (wifiApState() == "on") {
			stable = 0
			last = desired
		} else if desired == last {
			stable++
			if stable >= 3 {
				wifiApApply(desired, m)
				stable = 0
			}
		} else {
			last, stable = desired, 1
		}
		if !sleepCtx(ctx, 4*time.Second) {
			log.Printf("wifi-watch: остановка")
			return nil
		}
	}
}

func waitSTA(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if staConnected() {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func waitIPv4(dev string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("ip", "-4", "addr", "show", dev).Output()
		if strings.Contains(string(out), "inet ") {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func applyEthMode2(m netcfg.Mode2) error {
	// eth0 ВСЕГДА в НАШЕЙ сети (статический IP); чужой DHCP лишь выключает
	// нашу раздачу, но конфигурацию eth0 не меняет (не клиент .13!).
	// Блокирующая остановка: иначе dhclient успевает снять адрес уже после
	// того, как мы назначили статический IP (гонка).
	_ = run("systemctl", "stop", "dhclient-eth0.service")
	_ = run("ip", "addr", "flush", "dev", "eth0")
	_ = run("ip", "link", "set", "eth0", "up")
	eth2Static(m)
	_ = sysctl("net.ipv4.ip_forward", "1")
	natWlan()
	log.Printf("mode2: eth0 configured (our subnet)")

	if !m.Serve {
		dhcpOff()
		_ = run("systemctl", "stop", "--no-block", "malina-net-monitor.service")
		return nil
	}
	// Монитор сам переживает отсутствие carrier и повторно включает раздачу,
	// поэтому при отсутствии кабеля его не глушим.
	if carrier("eth0") != "1" {
		dhcpOff()
		_ = run("systemctl", "restart", "malina-net-monitor.service")
		return nil
	}
	foreign := probeForeignDHCP("eth0", m.LAN.ServerIP, 15*time.Second)
	log.Printf("mode2 eth0: carrier=%s foreign_dhcp=%v", carrier("eth0"), foreign)
	if foreign {
		dhcpOff()
	} else {
		_ = dhcpOn(m)
	}
	_ = run("systemctl", "restart", "malina-net-monitor.service")
	return nil
}

const servingMarker = runDir + "/serving"

const natWlanRules = `table ip malina {
  chain forward { type filter hook forward priority 0; policy accept; }
  chain postrouting { type nat hook postrouting priority srcnat; policy accept; oifname "wlan0" masquerade; }
}
`

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// eth2Static назначает eth0 адрес НАШЕЙ сети (не зависит от чужого DHCP).
func eth2Static(m netcfg.Mode2) {
	prefix := maskToPrefix(m.LAN.Mask)
	_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", m.LAN.ServerIP, prefix), "dev", "eth0")
}

// natWlan — NAT наших клиентов (eth0 10.10…) в STA-аплинк (wlan0).
func natWlan() {
	if err := writeFile(filepath.Join(runDir, "ruleset.nft"), natWlanRules, 0o600); err == nil {
		_ = run("nft", "delete", "table", "ip", "malina")
		_ = run("nft", "-f", filepath.Join(runDir, "ruleset.nft"))
	}
}

// dhcpOn — включить наш DHCP-сервер на eth0 (конфиг eth0 уже настроен).
func dhcpOn(m netcfg.Mode2) error {
	mask := m.LAN.Mask
	var db strings.Builder
	fmt.Fprintf(&db, "interface=eth0\nbind-interfaces\nexcept-interface=wlan0\nno-dhcp-interface=wlan0\n")
	fmt.Fprintf(&db, "dhcp-leasefile=%s/dnsmasq.leases\n", runDir)
	fmt.Fprintf(&db, "dhcp-range=%s,%s,%s,2h\n", m.LAN.Start, m.LAN.End, mask)
	fmt.Fprintf(&db, "dhcp-option=3,%s\ndhcp-option=6,%s\nlog-dhcp\n", m.LAN.ServerIP, m.LAN.ServerIP)
	if err := writeFile(filepath.Join(runDir, "dnsmasq.conf"), db.String(), 0o600); err != nil {
		return err
	}
	_ = run("systemctl", "restart", "malina-dnsmasq.service")
	_ = writeFile(servingMarker, "1", 0o644)
	return nil
}

// dhcpOff — выключить наш DHCP-сервер. IP/NAT/конфигурацию eth0 НЕ меняем.
func dhcpOff() {
	_ = run("systemctl", "stop", "malina-dnsmasq.service")
	_ = os.Remove(servingMarker)
}

// monitor — активный зонд раз в 2 минуты: чужой DHCP → глушим наш;
// повторное включение Serving — только после ≥2 подряд «чистых» проверок.
func monitor(ctx context.Context) error {
	log.Printf("monitor: started (interval 2m, re-arm after >=2 clean probes)")
	cleanCount := 0
	for {
		if ctx.Err() != nil {
			log.Printf("monitor: остановка")
			return nil
		}
		mode := activeMode()
		m := netcfg.LoadMode2()
		if mode == "mode2" && m.Serve {
			if carrier("eth0") != "1" {
				dhcpOff()
				cleanCount = 0
			} else {
				foreign := probeForeignDHCP("eth0", m.LAN.ServerIP, 15*time.Second)
				serving := fileExists(servingMarker)
				switch {
				case foreign:
					cleanCount = 0
					if serving {
						log.Printf("monitor: чужой DHCP -> выключаем нашу раздачу (конфиг eth0 не трогаем)")
						dhcpOff()
						serving = false
					}
				default:
					cleanCount++
					if !serving && cleanCount >= 2 {
						log.Printf("monitor: %d чистых -> включаем нашу раздачу", cleanCount)
						_ = dhcpOn(m)
						serving = true
					}
				}
				st, _ := json.Marshal(map[string]any{
					"ts":          time.Now().Unix(),
					"foreign":     foreign,
					"serving":     serving,
					"clean_count": cleanCount,
				})
				_ = writeFile(filepath.Join(runDir, "monitor.state"), string(st), 0o644)
			}
		}
		if !sleepCtx(ctx, 2*time.Minute) {
			log.Printf("monitor: остановка")
			return nil
		}
	}
}

func carrier(dev string) string {
	b, err := os.ReadFile("/sys/class/net/" + dev + "/carrier")
	if err != nil {
		return "0"
	}
	return strings.TrimSpace(string(b))
}

// probeForeignDHCP: активный зонд через обычный UDP-сокет (:68), собирает ВСЕ
// OFFER/ACK и ищет сервер с server-id != ourIP. На время зонда освобождаем :68
// (кратко останавливаем dhclient-wlan0), затем возвращаем его. dhclient-eth0 в
// режиме 2 намеренно выключен и НЕ поднимается этим зондом.
func probeForeignDHCP(iface, ourIP string, timeout time.Duration) bool {
	_ = run("systemctl", "stop", "--no-block", "malina-dhclient-wlan0.service")
	time.Sleep(700 * time.Millisecond)

	mb, err := os.ReadFile("/sys/class/net/" + iface + "/address")
	if err != nil {
		_ = run("systemctl", "start", "malina-dhclient-wlan0.service")
		return false
	}
	mac, err := net.ParseMAC(strings.TrimSpace(string(mb)))
	if err != nil {
		_ = run("systemctl", "start", "malina-dhclient-wlan0.service")
		return false
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var serr error
		c.Control(func(fd uintptr) {
			if e := syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface); e != nil {
				serr = e
				return
			}
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		})
		return serr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		log.Printf("probe: listen :68: %v", err)
		_ = run("systemctl", "start", "malina-dhclient-wlan0.service")
		return false
	}
	defer pc.Close()
	deadline := time.Now().Add(timeout)
	_ = pc.SetDeadline(deadline)
	payload := buildDHCPDiscover(mac)
	if _, err := pc.WriteTo(payload, &net.UDPAddr{IP: net.IPv4bcast, Port: 67}); err != nil {
		_ = run("systemctl", "start", "malina-dhclient-wlan0.service")
		return false
	}
	buf := make([]byte, 1500)
	foreign := false
	for time.Now().Before(deadline) {
		_ = pc.SetDeadline(deadline)
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			break
		}
		if n <= 240 {
			continue
		}
		opts := parseDHCP(buf[240:n])
		mt, ok := opts[53]
		if !ok || len(mt) == 0 || (mt[0] != 2 && mt[0] != 5) {
			continue
		}
		sid := ""
		if b, ok := opts[54]; ok && len(b) == 4 {
			sid = net.IPv4(b[0], b[1], b[2], b[3]).String()
		}
		if sid != "" && sid == ourIP {
			continue // наш сервер
		}
		log.Printf("probe: OFFER/ACK from %q -> чужой", sid)
		foreign = true
		break
	}
	_ = run("systemctl", "start", "malina-dhclient-wlan0.service")
	return foreign
}

// buildDHCPFrame — Ethernet + IPv4 + UDP(68->67) + DHCPDISCOVER.

func buildDHCPDiscover(mac net.HardwareAddr) []byte {
	p := make([]byte, 236) // BOOTP fixed header (без cookie)
	p[0] = 1               // bootrequest
	p[1] = 1               // ethernet
	p[2] = 6               // hlen
	xid := uint32(time.Now().UnixNano())
	p[4], p[5], p[6], p[7] = byte(xid>>24), byte(xid>>16), byte(xid>>8), byte(xid)
	p[10], p[11] = 0x80, 0x00 // broadcast
	copy(p[28:34], mac)
	p = append(p, 99, 130, 83, 99) // magic cookie (offset 236)
	p = append(p, 53, 1, 1)        // DHCPDISCOVER
	p = append(p, 55, 3, 1, 3, 6)  // request list
	p = append(p, 255)
	for len(p) < 300 {
		p = append(p, 0)
	}
	return p
}

func parseDHCP(b []byte) map[byte][]byte {
	o := map[byte][]byte{}
	i := 0
	for i < len(b) {
		c := b[i]
		if c == 255 {
			break
		}
		if c == 0 {
			i++
			continue
		}
		if i+1 >= len(b) {
			break
		}
		l := int(b[i+1])
		if i+2+l > len(b) {
			break
		}
		o[c] = b[i+2 : i+2+l]
		i += 2 + l
	}
	return o
}

func applyEth(e netcfg.Eth, dev string) error {
	switch e.Mode {
	case "static":
		_ = run("systemctl", "stop", "dhclient-eth0.service")
		_ = run("ip", "addr", "flush", "dev", dev)
		prefix := maskToPrefix(e.Mask)
		_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", e.IP, prefix), "dev", dev)
		_ = run("ip", "link", "set", dev, "up")
		if e.Gateway != "" {
			_ = run("ip", "route", "replace", "default", "via", e.Gateway, "dev", dev)
		}
		if len(e.DNS) > 0 {
			var sb strings.Builder
			for _, d := range e.DNS {
				sb.WriteString("nameserver " + d + "\n")
			}
			_ = writeFile("/run/malina-net/resolv.conf", sb.String(), 0o644)
		}
	default: // dhcp — используем существующий юнит dhclient-eth0.service
		_ = run("ip", "addr", "flush", "dev", dev)
		_ = run("ip", "link", "set", dev, "up")
		_ = run("systemctl", "restart", "dhclient-eth0.service")
	}
	return nil
}

func status() error {
	st := map[string]any{
		"persistent": netcfg.Active(),
		"effective":  activeMode(),
	}
	if b, err := os.ReadFile(filepath.Join(runDir, "mode")); err == nil {
		st["override"] = strings.TrimSpace(string(b))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

func scan() error {
	// Простой скан: требует разблокировки и managed-режима wlan0.
	_ = run("/usr/sbin/rfkill", "unblock", "wifi")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/iw", "dev", "wlan0", "scan").Output()
	if err != nil {
		return fmt.Errorf("iw scan: %w", err)
	}
	type netw struct {
		SSID   string `json:"ssid"`
		Signal string `json:"signal"`
	}
	var res []netw
	var cur netw
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "BSS ") {
			if cur.SSID != "" {
				res = append(res, cur)
			}
			cur = netw{}
		} else if strings.HasPrefix(line, "SSID:") {
			cur.SSID = strings.TrimSpace(strings.TrimPrefix(line, "SSID:"))
		} else if strings.HasPrefix(line, "signal:") {
			cur.Signal = strings.TrimSpace(strings.TrimPrefix(line, "signal:"))
		}
	}
	if cur.SSID != "" {
		res = append(res, cur)
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	return writeFile(filepath.Join(runDir, "scan.json"), string(b), 0o644)
}

// apWatchdog следит за работоспособностью точки доступа (mode1): активность
// hostapd, режим и SSID интерфейса. При отклонении перезапускает hostapd и
// восстанавливает адрес шлюза. Дополнительно удерживает power save выключенным
// (brcmfmac без этого деградирует в AP-режиме). Вызывается по таймеру (1 мин).
func apWatchdog() {
	if activeMode() != "mode1" {
		return
	}
	// Wi-Fi намеренно погашен (опция «Выключить Wi-Fi» + Ethernet): не поднимаем
	// AP и не крутим hostapd/dnsmasq — иначе получаем цикл ошибок hostapd.
	if wifiApState() == "off" {
		_ = run("systemctl", "stop", "--no-block", "malina-hostapd.service")
		_ = run("systemctl", "stop", "--no-block", "malina-dnsmasq.service")
		return
	}
	m := netcfg.LoadMode1()
	healthy := serviceActive("malina-hostapd.service")
	if healthy {
		out, err := exec.Command("/usr/sbin/iw", "dev", "wlan0", "info").Output()
		if err != nil {
			healthy = false
		} else {
			info := string(out)
			if !strings.Contains(info, "type AP") {
				healthy = false
			} else if m.AP.SSID != "" && !strings.Contains(info, "ssid "+m.AP.SSID) {
				healthy = false
			}
		}
	}
	if !healthy {
		log.Printf("watchdog: AP неисправен — перезапуск hostapd")
		_ = run("systemctl", "restart", "malina-hostapd.service")
		ensureAPAddr(m)
		return
	}
	_ = run("/usr/sbin/iw", "dev", "wlan0", "set", "power_save", "off")
	if !serviceActive("malina-dnsmasq.service") {
		log.Printf("watchdog: dnsmasq не активен — перезапуск")
		_ = run("systemctl", "restart", "malina-dnsmasq.service")
	}
	apIdleWatch(m)
}

// apIdleWatch перезапускает hostapd, если станций нет дольше apIdleRestart.
// Пока есть хотя бы одна станция — таймер сбрасывается.
func apIdleWatch(m netcfg.Mode1) {
	tsPath := filepath.Join(runDir, "ap-idle.ts")
	if stationCount() > 0 {
		_ = os.Remove(tsPath)
		return
	}
	first := readTS(tsPath)
	if first.IsZero() {
		writeTS(tsPath, time.Now())
		return
	}
	if time.Since(first) >= apIdleRestart {
		log.Printf("watchdog: нет станций %s — профилактический перезапуск hostapd", apIdleRestart)
		_ = run("systemctl", "restart", "malina-hostapd.service")
		ensureAPAddr(m)
		_ = os.Remove(tsPath)
	}
}

func stationCount() int {
	out, err := exec.Command("/usr/sbin/iw", "dev", "wlan0", "station", "dump").Output()
	if err != nil {
		return 0
	}
	count := 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "Station ") {
			count++
		}
	}
	return count
}

func serviceActive(name string) bool {
	out, _ := exec.Command("systemctl", "is-active", name).Output()
	return strings.TrimSpace(string(out)) == "active"
}

// ensureAPAddr гарантирует наличие адреса шлюза/сервера на wlan0.
func ensureAPAddr(m netcfg.Mode1) {
	out, _ := exec.Command("ip", "-4", "addr", "show", "wlan0").Output()
	if strings.Contains(string(out), m.LAN.ServerIP+"/") {
		return
	}
	_ = run("ip", "addr", "add", fmt.Sprintf("%s/%d", m.LAN.ServerIP, maskToPrefix(m.LAN.Mask)), "dev", "wlan0")
}

// fallbackTick (по таймеру раз в минуту):
//   - персистентный режим mode2 и нет ни STA-линка, ни carrier eth0 ≥10 мин
//     -> транзиентно переключиться в mode1 (override в /run, без изменения конфига);
//   - в резервном AP (=override mode1 при persistent mode2) раз в 10 мин сканировать
//     эфир; при появлении целевой сети — снять override и вернуться в mode2.
func fallbackTick() error {
	// Сначала следим за работоспособностью AP (mode1).
	apWatchdog()

	now := time.Now()
	f := netcfg.LoadFallback()
	nolink := time.Duration(f.NoLinkSec) * time.Second
	scanEvery := time.Duration(f.ScanSec) * time.Second
	override := ""
	if b, err := os.ReadFile(filepath.Join(runDir, "mode")); err == nil {
		override = strings.TrimSpace(string(b))
	}
	persistent := netcfg.Active()

	if override == "" {
		if persistent != "mode2" {
			return nil
		}
		if !staConnected() && carrier("eth0") == "0" {
			first := readTS(filepath.Join(runDir, "nolink.ts"))
			if first.IsZero() {
				writeTS(filepath.Join(runDir, "nolink.ts"), now)
				log.Printf("fallback: нет STA и нет eth-линка — отсчёт %s", nolink)
				return nil
			}
			if now.Sub(first) >= nolink {
				log.Printf("fallback: %s без связи -> транзиентно режим 1 (AP)", nolink)
				_ = writeFile(filepath.Join(runDir, "mode"), "mode1", 0o644)
				_ = os.Remove(filepath.Join(runDir, "nolink.ts"))
				restartNet()
			}
		} else {
			_ = os.Remove(filepath.Join(runDir, "nolink.ts"))
		}
		return nil
	}

	// Резервный AP: ищем целевую сеть STA раз в 10 мин.
	if override == "mode1" && persistent == "mode2" {
		last := readTS(filepath.Join(runDir, "scan.ts"))
		if !last.IsZero() && now.Sub(last) < scanEvery {
			return nil
		}
		writeTS(filepath.Join(runDir, "scan.ts"), now)
		ssid := strings.TrimSpace(netcfg.LoadMode2().STA.SSID)
		if ssid != "" && scanForTarget(ssid) {
			log.Printf("fallback: цель %q найдена -> возврат в mode2", ssid)
			_ = os.Remove(filepath.Join(runDir, "mode"))
			_ = os.Remove(filepath.Join(runDir, "scan.ts"))
			restartNet()
		}
	}
	return nil
}

func restartNet() { _ = run("systemctl", "restart", "--no-block", "malina-net.service") }

func staConnected() bool {
	// Подключение определяем по фактической ассоциации, а не по наличию IP:
	// у статического STA адрес назначается до/без успешной ассоциации.
	out, err := exec.Command("/usr/sbin/iw", "dev", "wlan0", "link").Output()
	if err != nil {
		return false
	}
	s := string(out)
	if strings.Contains(s, "Not connected") {
		return false
	}
	return strings.Contains(s, "Connected to")
}

func readTS(p string) time.Time {
	b, err := os.ReadFile(p)
	if err != nil {
		return time.Time{}
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func writeTS(p string, t time.Time) {
	_ = writeFile(p, strconv.FormatInt(t.Unix(), 10), 0o644)
}

// scanForTarget сканирует эфир, при необходимости кратко снимая AP,
// и проверяет наличие целевого SSID.
func scanForTarget(ssid string) bool {
	hostapdUp := false
	if out, _ := exec.Command("systemctl", "is-active", "malina-hostapd.service").Output(); strings.TrimSpace(string(out)) == "active" {
		hostapdUp = true
		_ = run("systemctl", "stop", "--no-block", "malina-hostapd.service")
		time.Sleep(1200 * time.Millisecond)
	}
	found := scanOnce(ssid)
	if hostapdUp {
		_ = run("systemctl", "start", "malina-hostapd.service")
	}
	return found
}

func scanOnce(ssid string) bool {
	_ = run("/usr/sbin/rfkill", "unblock", "wifi")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/iw", "dev", "wlan0", "scan").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID:") {
			if strings.TrimSpace(strings.TrimPrefix(line, "SSID:")) == ssid {
				return true
			}
		}
	}
	return false
}

// maskToPrefix переводит маску 255.255.255.0 -> 24.
func maskToPrefix(mask string) int {
	parts := strings.Split(mask, ".")
	if len(parts) != 4 {
		return 24
	}
	var m uint32
	for _, p := range parts {
		v, _ := strconv.Atoi(p)
		m = m<<8 | uint32(byte(v))
	}
	n := 0
	for i := 31; i >= 0; i-- {
		if m&(1<<uint(i)) != 0 {
			n++
		} else {
			break
		}
	}
	return n
}
