// tsdb — append-only хранилище рядов телеметрии ПАК «Малина».
//
// Отдельный сервис: хранит версионированные двоичные строки телеметрии по ключу
// (тип устройства, идентификатор, время) в сегментных файлах на SD (/malina),
// колоночными блоками, без фоновых оптимизаций — только дозапись. Единая
// writer-горутина, flush сразу при получении записи. Доступ — только локально,
// через Unix domain socket. Дизайн — docs/telemetry-store.md.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

const (
	defRoot     = "/malina/var/malina-ts"
	defSock     = "/run/malina-ts/tsdb.sock"
	defBucket   = 1024
	defSegMax   = 8 * 1024 * 1024
	defMaxRows  = 300_000
	defMaxRange = 400 * 24 * time.Hour
)

func main() {
	root := flag.String("root", envOr("MALINA_TS_ROOT", defRoot), "каталог данных")
	sock := flag.String("sock", envOr("MALINA_TS_SOCK", defSock), "путь Unix-сокета (локальный доступ)")
	bucket := flag.Uint("bucket", defBucket, "сегментов в бакете (лимит файлов в каталоге)")
	segMax := flag.Int64("segmax", defSegMax, "предел размера сегмента (байт)")
	maxRows := flag.Int("maxrows", defMaxRows, "лимит строк в ответе чтения (0 = без лимита)")
	maxRange := flag.Duration("maxrange", defMaxRange, "лимит периода запроса (0 = без лимита)")
	row := flag.Bool("row", false, "raw-режим: row-раскладка сегмента (tmpfs)")
	retention := flag.Duration("retention", 0, "raw: возраст очистки сегментов (0 = выкл.)")
	limitMB := flag.Int64("limit", 0, "raw: лимит объёма данных, МБ (0 = без лимита)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("tsdb: ")

	store, err := NewStore(*root, Options{
		BucketSize: uint32(*bucket),
		SegMax:     *segMax,
		MaxRows:    *maxRows,
		MaxRangeMS: maxRange.Milliseconds(),
		Row:        *row,
		Retention:  *retention,
		LimitBytes: *limitMB << 20,
	}, tscontract.NewDefaultRegistry())
	if err != nil {
		log.Fatalf("открытие хранилища %s: %v", *root, err)
	}
	log.Printf("старт (root=%s sock=%s)", *root, *sock)

	srv := &Server{store: store}
	ln := openUnix(*sock)
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("listener остановлен: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("остановка")
	ln.Close()
	if err := store.Close(); err != nil {
		log.Printf("закрытие хранилища: %v", err)
	}
	os.Remove(*sock)
	log.Printf("остановлен")
}

func openUnix(sock string) net.Listener {
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		log.Printf("mkdir сокета: %v", err)
	}
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Fatalf("unix-сокет %s: %v", sock, err)
	}
	os.Chmod(sock, 0o600)
	log.Printf("слушаю unix://%s", sock)
	return ln
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
