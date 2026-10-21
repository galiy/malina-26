package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// bms-proxy — демон-мост: находит порт, где инвертор опрашивает BMS, и отдаёт
// ему телеметрию живых BMS из shm bms-reader (docs/bms-proxy.md).
//
// Состояния:
//
//	C — выключен (enabled=false): длинный сон, ждёт reload (docs/bms-proxy.md §8);
//	A — поиск порта (docs/bms-proxy.md §6);
//	B — слушатель порта и ответ по стабильному слоту (docs/bms-proxy.md §7).
func main() {
	log.SetPrefix("bms-proxy: ")
	log.SetFlags(log.LstdFlags)

	cfg := newConfig()
	cfg.settings = loadSettings(cfg.cfgPath)
	applyLogLevel(cfg.settings.LogLevel)
	infof("запуск: %s", cfg)

	stop := make(chan struct{})
	reload := make(chan struct{}, 1)

	// сигналы: TERM/INT — завершение, HUP — reload настроек (systemd ExecReload).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for {
			select {
			case <-stop:
				return
			case s := <-sig:
				switch s {
				case syscall.SIGHUP:
					select {
					case reload <- struct{}{}:
					default:
					}
				default:
					close(stop)
					return
				}
			}
		}
	}()

	ctl := &control{stop: stop, reload: reload}
	go ctl.serve()

	d := &daemon{cfg: cfg, stop: stop, reload: reload, sc: newPortScanner(stop), udp: &udpManager{}}

	// Второй слушатель — UDP: включается настройкой `udp_enabled` + адрес `udp_listen`
	// (по умолчанию выключен). Независим от COM/состояний (docs/bms-proxy.md §7а).
	d.udp.sync(cfg.settings.udpListenAddr(), d)

	d.run() // блокируется до stop

	// Корректное завершение: остановить UDP-слушателя (если был).
	d.udp.stop()
	infof("остановлен")
}

// daemon — оркестратор состояний.
type daemon struct {
	cfg    *config
	stop   chan struct{}
	reload chan struct{}
	sc     *portScanner
	udp    *udpManager
	port   *transport.Port // найденный порт (состояние B)
	mu     sync.Mutex
}

// settings возвращает снимок настроек под мьютексом.
func (d *daemon) settings() settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.settings
}

// applySettings перечитывает настройки с диска, подменяет текущие и применяет
// уровень логирования (без рестарта сервиса).
func (d *daemon) applySettings() settings {
	s := loadSettings(d.cfg.cfgPath)
	d.mu.Lock()
	d.cfg.settings = s
	d.mu.Unlock()
	applyLogLevel(s.LogLevel)
	// UDP-слушатель переключается по reload (udp_enabled + udp_listen).
	if d.udp != nil {
		d.udp.sync(s.udpListenAddr(), d)
	}
	return s
}

func (d *daemon) run() {
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		if !d.settings().Enabled {
			d.stateStopped()
			continue
		}
		if !d.stateFindPort() {
			return
		}
		d.stateServe()
	}
}

// stateStopped — состояние C: выключен, длинный сон до reload (включения).
// Возврат по `stop`/включению; вызывающий (run) проверяет stop.
func (d *daemon) stateStopped() {
	infof("состояние C: выключен — жду reload (enabled=true)")
	for {
		select {
		case <-d.stop:
			return
		case <-d.reload:
			s := d.applySettings()
			infof("reload: enabled=%v baud=%d log=%s addr=%s", s.Enabled, s.Baud, s.LogLevel, s.AddrMode)
			if s.Enabled {
				return // включился → поиск порта
			}
		case <-time.After(30 * time.Second):
			// длинный сон — периодически перечитываем настройки даже без сигнала
			s := d.applySettings()
			if s.Enabled {
				return
			}
		}
	}
}

// stateFindPort — состояние A: поиск «своего» порта. Возвращает false, если
// демон останавливают; true — порт найден либо переключение состояния.
func (d *daemon) stateFindPort() bool {
	infof("состояние A: поиск порта (baud=%d)", d.settings().Baud)
	for {
		select {
		case <-d.stop:
			return false
		default:
		}
		// при выключении по reload — уходим в C
		if !d.settings().Enabled {
			return true
		}
		p := d.sc.findPort(d.settings().Baud)
		if p != nil {
			d.sc.setPrefer(p.Path())
			d.port = p
			return true
		}
		select {
		case <-d.stop:
			return false
		case <-d.reload:
			s := d.applySettings()
			if !s.Enabled {
				infof("reload: выключен → состояние C")
				return true
			}
			infof("reload: enabled=%v baud=%d log=%s addr=%s", s.Enabled, s.Baud, s.LogLevel, s.AddrMode)
		case <-time.After(scanCycle):
		}
	}
}

// stateServe — состояние B: слушатель порта и ответы по стабильному слоту.
func (d *daemon) stateServe() {
	p := d.port
	if p == nil {
		return
	}
	d.port = nil
	s := d.settings()
	infof("состояние B: слушаем на %s (baud=%d, адресация %s, адрес инвертора → слот shm)",
		p.Path(), s.Baud, s.AddrMode)

	// childStop позволяет остановить слушателя и дождаться его выхода ДО
	// закрытия порта (иначе ReadOneErr может залипнуть/крутиться на закрытом fd).
	childStop := make(chan struct{})
	done := listener(p, s.Baud, d.cfg.shmPath, s.AddrMode, childStop)
	shutdown := func() {
		close(childStop)
		<-done
		p.Close()
	}

	for {
		select {
		case <-d.stop:
			shutdown()
			return
		case <-d.reload:
			ns := d.applySettings()
			if !ns.Enabled {
				infof("reload: выключен — останавливаю слушателя → состояние C")
				shutdown()
				return
			}
			if ns.Baud != s.Baud || ns.AddrMode != s.AddrMode {
				// Скорость и адресация применяются к порту/слушателю — перезапускаем
				// (возврат в состояние A заново найдёт порт с новыми параметрами).
				infof("reload: смена baud %d→%d / адресации %s→%s — перезапускаю слушателя",
					s.Baud, ns.Baud, s.AddrMode, ns.AddrMode)
				shutdown()
				return
			}
			infof("reload: enabled=%v baud=%d log=%s addr=%s", ns.Enabled, ns.Baud, ns.LogLevel, ns.AddrMode)
		case <-done:
			infof("состояние B: слушатель завершился (idle/EOF/ошибка) → поиск порта")
			p.Close()
			return
		}
	}
}
