package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/galiy/malina-26/web/internal/accumulator"
	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/events"
	"github.com/galiy/malina-26/web/internal/mailer"
	"github.com/galiy/malina-26/web/internal/masternode"
	"github.com/galiy/malina-26/web/internal/syslogx"
	"github.com/galiy/malina-26/web/internal/web"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetOutput(syslogx.Raw("malina-web"))
	listen := flag.String("listen", envOr("MALINA_WEB_LISTEN", ":8080"), "listen address")
	user := flag.String("user", envOr("MALINA_WEB_USER", "malina"), "web/linux user")
	pamHelper := flag.String("pam-helper", envOr("MALINA_WEB_PAM_HELPER", "/malina/usr/local/libexec/malina-pamcheck"), "PAM helper path")
	assets := flag.String("assets", envOr("MALINA_WEB_ASSETS", "/malina/usr/local/share/malina/web"), "assets directory (templates/static/i18n)")
	helpDir := flag.String("help-dir", envOr("MALINA_WEB_HELP_DIR", "/malina/usr/local/share/malina/manuals"), "user documentation directory")
	authShm := flag.String("auth-shm", envOr("MALINA_WEB_AUTH_SHM", "/dev/shm/malina-auth"), "auth store (shm) path")
	sessionIdle := flag.Duration("session-idle", time.Hour, "session/token idle TTL (sliding)")
	controlSock := flag.String("control-sock", envOr("MALINA_WEB_CONTROL_SOCK", "/run/malina-map/control.sock"), "map-reader control socket")
	auditOn := flag.Bool("audit", envOr("MALINA_WEB_AUDIT", "1") != "0", "аудит-журнал действий (syslog)")
	auditTag := flag.String("audit-tag", envOr("MALINA_WEB_AUDIT_TAG", "malina-web-audit"), "syslog-тег аудита")
	masterConfig := flag.String("master-config", envOr("MALINA_WEB_MASTER_CONFIG", masternode.DefaultPath), "путь к nodes.json мастер-узла")
	masterCache := flag.String("master-cache", envOr("MALINA_WEB_MASTER_CACHE", masternode.DefaultCacheDir), "каталог кэша мастер-узла")
	emailConfig := flag.String("email-config", envOr("MALINA_WEB_EMAIL_CONFIG", mailer.DefaultConfigPath), "путь к email.json")
	emailKey := flag.String("email-key", envOr("MALINA_WEB_EMAIL_KEY", mailer.DefaultKeyPath), "путь к ключу шифрования секрета")
	emailOutbox := flag.String("email-outbox", envOr("MALINA_WEB_EMAIL_OUTBOX", mailer.DefaultOutboxDir), "каталог аутбокса")
	eventsState := flag.String("events-state", envOr("MALINA_WEB_EVENTS_STATE", events.DefaultStatePath), "путь к состоянию правил (tmpfs)")
	snmpPort := flag.Int("snmp-port", 161, "UDP-порт SNMP-агента")
	snmpCommunity := flag.String("snmp-community", "public", "community SNMP-агента")
	snmpMonitoring := flag.String("snmp-monitoring", "/malina/etc/malina/snmp-monitoring.json", "путь к настройке периодических трапов")
	rawRoot := flag.String("ts-raw-root", envOr("MALINA_TS_RAW_ROOT", "/run/malina-ts-raw"), "каталог raw-tsdb (tmpfs)")
	rawSock := flag.String("ts-raw-sock", envOr("MALINA_TS_RAW_SOCK", "/run/malina-ts-raw/tsdb.sock"), "Unix-сокет raw-tsdb")
	avgSock := flag.String("ts-avg-sock", envOr("MALINA_TS_SOCK", "/run/malina-ts/tsdb.sock"), "Unix-сокет avg-tsdb")
	avgRoot := flag.String("ts-avg-root", envOr("MALINA_TS_AVG_ROOT", "/malina/var/malina-ts"), "каталог avg-tsdb")
	accCfg := flag.String("acc-config", envOr("MALINA_WEB_ACC_CFG", accumulator.DefaultConfigPath), "настройка аккумулятора (json)")
	flag.Parse()

	var a auth.Authenticator
	if dev := os.Getenv("MALINA_WEB_DEV_PASSWORD"); dev != "" {
		log.Printf("WARNING: dev auth enabled via MALINA_WEB_DEV_PASSWORD")
		a = auth.NewDev(*user, dev)
	} else {
		a = auth.NewPAM(*pamHelper, *user)
	}

	srv, err := web.New(web.Options{
		Addr:          *listen,
		AssetsDir:     *assets,
		HelpDir:       *helpDir,
		Auth:          a,
		User:          *user,
		SessionIdle:   *sessionIdle,
		AuthStorePath: *authShm,
		ControlSock:   *controlSock,
		AuditEnabled:  *auditOn,
		AuditTag:      *auditTag,

		MasterConfigPath: *masterConfig,
		MasterCacheDir:   *masterCache,

		EmailConfigPath: *emailConfig,
		EmailKeyPath:    *emailKey,
		EmailOutboxDir:  *emailOutbox,

		EventsStatePath: *eventsState,

		SNMPPort:       *snmpPort,
		SNMPCommunity:  *snmpCommunity,
		SNMPMonitoring: *snmpMonitoring,

		AccConfigPath: *accCfg,

		TsdbRawRoot: *rawRoot,
		TsdbRawSock: *rawSock,
		TsdbSock:    *avgSock,
		TsdbAvgRoot: *avgRoot,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var accWG sync.WaitGroup
	accWG.Add(1)
	go func() {
		defer accWG.Done()
		accumulator.Run(ctx, *rawRoot, *rawSock, *avgSock, *accCfg, log.Printf)
	}()

	log.Printf("malina-web listening on %s (user=%s)", *listen, *user)
	if err := srv.Run(ctx); err != nil {
		log.Printf("server: %v", err)
	}
	log.Printf("остановка: завершаем аккумулятор…")
	accWG.Wait()
	log.Printf("stopped")
}
