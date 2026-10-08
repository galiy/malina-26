package main

import (
	"flag"
	"log"
	"os"

	"github.com/galiy/malina-26/web/internal/auth"
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
	flag.Parse()

	var a auth.Authenticator
	if dev := os.Getenv("MALINA_WEB_DEV_PASSWORD"); dev != "" {
		log.Printf("WARNING: dev auth enabled via MALINA_WEB_DEV_PASSWORD")
		a = auth.NewDev(*user, dev)
	} else {
		a = auth.NewPAM(*pamHelper, *user)
	}

	srv, err := web.New(web.Options{
		Addr:      *listen,
		AssetsDir: *assets,
		Auth:      a,
		User:      *user,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	log.Printf("malina-web listening on %s (user=%s)", *listen, *user)
	if err := srv.Run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
