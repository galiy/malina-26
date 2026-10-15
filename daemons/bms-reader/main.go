// Command bms-reader — сбор телеметрии BMS (docs/bms-reader-arch.md,
// docs/bms-reader.md). Реализация по этапам плана docs/bms-reader-plan.md.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/galiy/malina-26/daemons/lib/stat"
)

func main() {
	debug := flag.Bool("debug", false, "подробные логи")
	flag.Parse()
	_ = debug

	gws, err := loadGateways()
	if err != nil {
		log.Fatalf("bms-reader: %v", err)
	}
	ident, err := loadIdent()
	if err != nil {
		log.Fatalf("bms-reader: %v", err)
	}
	log.Printf("bms-reader: шлюзов=%d, записей реестра=%d", len(gws.Gateways), len(ident.Entries))

	seg, err := openShm()
	if err != nil {
		log.Fatalf("bms-reader: shm: %v", err)
	}
	defer seg.Close()

	stop := make(chan struct{})

	sink := stat.New(rawSock)
	defer sink.Close()
	idr := newIDRegistry(ident)
	orch := newOrchestrator(seg, sink, idr, stop)
	orch.setGateways(gws)
	go orch.run()

	ctl := &control{seg: seg, stop: stop}
	ctl.reload = func() {
		if g, err := loadGateways(); err != nil {
			log.Printf("bms-reader: reload: %v", err)
		} else {
			gws = g
			orch.setGateways(g)
			log.Printf("bms-reader: reload ок, шлюзов=%d", len(gws.Gateways))
		}
	}
	go ctl.serve()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	for s := range sigc {
		if s == syscall.SIGHUP {
			log.Printf("bms-reader: SIGHUP → reload")
			ctl.reload()
			continue
		}
		log.Printf("bms-reader: %s → останов", s)
		break
	}
	close(stop)
}
