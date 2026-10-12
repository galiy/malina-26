package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Эмулятор — параметры запуска: -n, -baud, -store, -dir, -prefix, -start.
const (
	defN        = 3
	devDir      = "/dev"   // виртуальные устройства создаём в /dev
	devPrefix   = "ttyUSB" // → /dev/ttyUSB9.. (как реальный USB-адаптер)
	defStart    = 9        // старт с 9, чтобы не занять /dev/ttyUSB0 (реальный адаптер)
	defBaud     = 19200
	defFW       = 0x63   // версия ПО КЭС (FW), отображается как 6.3
	defRelayCmd = 0x0E3D // командная ячейка реле (гипотеза, см. docs/kes-reader.md §7.7)
	defUID      = 0x9191
)

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

func main() {
	n := flag.Int("n", defN, "число виртуальных КЭС (устройств)")
	baud := flag.Int("baud", defBaud, "скорость порта, бит/с")
	store := flag.String("store", "", "каталог карты памяти (kes-emu.json); по умолчанию /malina, /settings, /tmp")
	dir := flag.String("dir", devDir, "каталог устройств (симлинков)")
	prefix := flag.String("prefix", devPrefix, "префикс имени устройства")
	start := flag.Int("start", defStart, "начальный индекс устройства")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("kes-emu: ")

	if wp := os.Getenv("KES_EMU_WIRE"); wp == "-" {
		wire = os.Stderr // весь трафик в stderr (hex, строки «R xx»/«T xx»)
	} else if wp != "" {
		f, err := os.Create(wp)
		if err != nil {
			log.Fatalf("wire dump: %v", err)
		}
		wire = f
		log.Printf("wire dump: %s", wp)
	}

	var (
		devices []*Device
		masters []*os.File // сторона эмулятора (читаем/пишем)
		holds   []*os.File // slave O_RDONLY (против EIO у master)
		links   []string
	)
	stop := make(chan struct{})

	sweepStale(*dir, *prefix) // убрать битые симлинки от прошлого запуска

	st, err := openStore(*store, *n)
	if err != nil {
		log.Fatalf("persist: %v", err)
	}
	log.Printf("persist: %s", st.path)

	for i := 0; i < *n; i++ {
		slavePath, master, hold, err := openPTY(*baud)
		if err != nil {
			log.Fatalf("pty %d: %v", i, err)
		}
		if hold != nil {
			holds = append(holds, hold)
		}
		lp, err := ensureLink(*dir, *prefix+itoa(*start+i), slavePath)
		if err != nil {
			log.Fatalf("устройство %d: %v", i, err)
		}
		masters = append(masters, master)
		links = append(links, lp)
		rec := st.Record(i)
		devices = append(devices, newDevice(i, rec.Serial, defFW, defRelayCmd, st, i))
		log.Printf("КЭС %d: %s → %s (serial=%d)", i, lp, slavePath, rec.Serial)
	}

	for i := range devices {
		go serveDevice(devices[i], masters[i], stop)
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	go func() {
		for {
			select {
			case <-stop:
				return
			case now := <-tick.C:
				for _, d := range devices {
					d.Tick(now)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	sigv := <-sig
	log.Printf("signal %v, stopping", sigv)
	close(stop)
	for _, f := range masters {
		_ = f.Close()
	}
	for _, f := range holds {
		_ = f.Close()
	}
	for _, lp := range links { // убрать виртуальные устройства
		_ = os.Remove(lp)
	}
	log.Printf("stopped")
}
