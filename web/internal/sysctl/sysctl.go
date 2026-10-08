package sysctl

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	// ХостnameFile хранит желаемое имя хоста (на rw /malina).
	HostnameFile = "/malina/etc/hostname"
	// RemoteFile хранит свой syslog-хост (пусто/нет -> использовать DHCP-опцию 7).
	RemoteFile = "/malina/etc/malina/remote-syslog"
	// RemoteApply — общий хелпер: пишет remote-конфиг rsyslog и перезагружает его.
	RemoteApply = "/malina/usr/local/libexec/malina-remote-syslog"
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

// SetHostname сохраняет имя хоста и применяет его транзиентно.
func SetHostname(name string) error {
	if err := os.MkdirAll("/malina/etc", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(HostnameFile, []byte(name+"\n"), 0o644); err != nil {
		return err
	}
	if err := exec.Command("hostnamectl", "set-hostname", "--transient", name).Run(); err == nil {
		return nil
	}
	return exec.Command("hostname", name).Run()
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
