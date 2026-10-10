package sysctl

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	// ХостnameFile — системное имя хоста (меняется через rw-окно).
	HostnameFile = "/etc/hostname"
	// RemoteFile хранит свой syslog-хост (пусто/нет -> использовать DHCP-опцию 7).
	RemoteFile = "/malina/etc/malina/remote-syslog"
	// RemoteApply — общий хелпер: пишет remote-конфиг rsyslog и перезагружает его.
	RemoteApply = "/malina/usr/local/libexec/malina-remote-syslog"
	// RWHelper — хелпер короткого rw-окна корневой ФС (remount rw → run → ro).
	RWHelper = "/malina/usr/local/sbin/malina-rw"
	// DHCPOption7 хранит сырое значение из DHCP (пишет хук dhclient).
	DHCPOption7 = "/run/malina-net/dhcp-option7"
)

var (
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	// хост или IPv4, возможно несколько через пробел
	remoteRe = regexp.MustCompile(`^([A-Za-z0-9._-]+|([0-9]{1,3}\.){3}[0-9]{1,3})([ ]+([A-Za-z0-9._-]+|([0-9]{1,3}\.){3}[0-9]{1,3}))*$`)
)

// ValidHostname проверяет корректность имени хоста.
func ValidHostname(s string) bool { return len(s) <= 63 && hostnameRe.MatchString(s) }

// ValidRemoteHost проверяет непустой список хостов.
func ValidRemoteHost(s string) bool { return remoteRe.MatchString(s) }

// CurrentHostname возвращает текущее (runtime) имя хоста.
func CurrentHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// ХостnameSetting возвращает сохранённое имя хоста (может быть пустым).
func HostnameSetting() string {
	b, err := os.ReadFile(HostnameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetHost применяет имя хоста и часовой пояс прямо в /etc (ro-корень) через
// короткое rw-окно (malina-rw): /etc/hostname, симлинк /etc/localtime, /etc/timezone.
// Значения персистентны на rootfs; на /malina ничего не хранится.
func SetHost(hostname, tz string) error {
	// Пишем через временный файл + mv: заменяем inode, не переписывая существующий
	// «на месте» (защита от случая повреждённого inode /etc/hostname|timezone).
	zone := "/usr/share/zoneinfo/" + tz
	script := "printf '%s\\n' '" + hostname + "' > /etc/hostname.malina.tmp" +
		" && mv -f /etc/hostname.malina.tmp /etc/hostname" +
		" && hostname '" + hostname + "'" +
		" && ln -sfn '" + zone + "' /etc/localtime" +
		" && printf '%s\\n' '" + tz + "' > /etc/timezone.malina.tmp" +
		" && mv -f /etc/timezone.malina.tmp /etc/timezone"
	if err := exec.Command(RWHelper, "run", "sh", "-c", script).Run(); err != nil {
		return err
	}
	// systemd-timedated кэширует зону — заставить перечитать /etc/localtime.
	_ = exec.Command("systemctl", "restart", "systemd-timedated").Run()
	return nil
}

// ListTimezones возвращает доступные часовые пояса (отсортировано).
func ListTimezones() []string {
	out, err := exec.Command("timedatectl", "list-timezones").Output()
	if err != nil {
		return nil
	}
	var list []string
	for _, ln := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			list = append(list, s)
		}
	}
	return list
}

// CurrentTimezone возвращает текущий часовой пояс: из /etc/timezone,
// иначе из симлинка /etc/localtime.
func CurrentTimezone() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	if l, err := os.Readlink("/etc/localtime"); err == nil {
		const p = "/usr/share/zoneinfo/"
		if strings.HasPrefix(l, p) {
			return l[len(p):]
		}
	}
	return ""
}

// ValidTimezone проверяет имя зоны по базе /usr/share/zoneinfo.
func ValidTimezone(tz string) bool {
	if tz == "" || len(tz) > 64 {
		return false
	}
	if strings.ContainsAny(tz, "\n\r\t ") || strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return false
	}
	_, err := os.Stat("/usr/share/zoneinfo/" + tz)
	return err == nil
}

// RemoteSyslogSetting возвращает свой syslog-хост (может быть пустым).
func RemoteSyslogSetting() string {
	b, err := os.ReadFile(RemoteFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetRemoteSyslog сохраняет хост ("" очищает) и применяет сразу.
func SetRemoteSyslog(host string) error {
	host = strings.TrimSpace(host)
	if err := os.MkdirAll("/malina/etc/malina", 0o755); err != nil {
		return err
	}
	if host == "" {
		_ = os.Remove(RemoteFile)
	} else {
		if err := os.WriteFile(RemoteFile, []byte(host+"\n"), 0o644); err != nil {
			return err
		}
	}
	eff := host
	if eff == "" {
		if b, err := os.ReadFile(DHCPOption7); err == nil {
			eff = strings.TrimSpace(string(b))
		}
	}
	// Применяем общим хелпером (реагирует только на изменение).
	_ = exec.Command(RemoteApply, eff).Run()
	return nil
}
