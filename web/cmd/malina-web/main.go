package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/galiy/malina-26/web/internal/auth"
	"github.com/galiy/malina-26/web/internal/masternode"
	"github.com/galiy/malina-26/web/internal/web"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	listen := flag.String("listen", envOr("MALINA_WEB_LISTEN", ":8080"), "listen address")
	user := flag.String("user", envOr("MALINA_WEB_USER", "malina"), "web/linux user")
	pamHelper := flag.String("pam-helper", envOr("MALINA_WEB_PAM_HELPER", "/malina/usr/local/libexec/malina-pamcheck"), "PAM helper path")
	assets := flag.String("assets", envOr("MALINA_WEB_ASSETS", "/malina/usr/local/share/malina/web"), "assets directory (templates/static/i18n)")
	authShm := flag.String("auth-shm", envOr("MALINA_WEB_AUTH_SHM", "/dev/shm/malina-auth"), "auth store (shm) path")
	sessionIdle := flag.Duration("session-idle", time.Hour, "session/token idle TTL (sliding)")
	controlSock := flag.String("control-sock", envOr("MALINA_WEB_CONTROL_SOCK", "/run/malina-map/control.sock"), "map-reader control socket")
	auditOn := flag.Bool("audit", envOr("MALINA_WEB_AUDIT", "1") != "0", "аудит-журнал действий (syslog)")
	auditTag := flag.String("audit-tag", envOr("MALINA_WEB_AUDIT_TAG", "malina-web-audit"), "syslog-тег аудита")
	masterConfig := flag.String("master-config", envOr("MALINA_WEB_MASTER_CONFIG", masternode.DefaultPath), "путь к nodes.json мастер-узла")
	masterCache := flag.String("master-cache", envOr("MALINA_WEB_MASTER_CACHE", masternode.DefaultCacheDir), "каталог кэша мастер-узла")
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
		Auth:          a,
		User:          *user,
		SessionIdle:   *sessionIdle,
		AuthStorePath: *authShm,
		ControlSock:   *controlSock,
		AuditEnabled:  *auditOn,
		AuditTag:      *auditTag,

		MasterConfigPath: *masterConfig,
		MasterCacheDir:   *masterCache,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	log.Printf("malina-web listening on %s (user=%s)", *listen, *user)
	if err := srv.Run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
