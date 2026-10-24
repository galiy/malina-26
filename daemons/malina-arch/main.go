package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/control"
	"github.com/galiy/malina-26/daemons/lib/logx"
)

const (
	defRawRoot = "/run/malina-ts-raw"
	defAvgRoot = "/malina/var/malina-ts"
	defSock    = "/run/malina-arch/control.sock"
	defState   = "/run/malina-arch"
)

var (
	lx                = logx.New("malina-arch: ")
	errRunIDMatch     = errors.New("run_id на target не совпадает с текущим")
	errArchiveInvalid = errors.New("архив невалиден")
	errCredentials    = errors.New("неверные учётные данные target")
)

type arch struct {
	mu            sync.Mutex
	cfgPath       string
	keyPath       string
	rawRoot       string
	avgRoot       string
	rawSock       string
	device        string
	cfg           config
	key           []byte
	tgt           target
	enabled       bool
	cleanStart    bool // архив ещё не инициализирован под текущий run_id
	adoptRunID    bool // runID нужно взять из manifest при появлении target
	blocked       bool // нужно решение оператора (restore/reset)
	restoring     bool
	shuttingDown  bool
	forcedDebug   bool // -debug: уровень debug поверх конфига
	loopsRunning  bool
	reconnecting  bool
	state         string
	phase         string
	runID         string
	lastErr       string
	lastRaw       time.Time
	lastAvg       time.Time
	progress      string
	rawMfSize     int64
	avgMfSize     int64
	stop          chan struct{}
	loopWG        sync.WaitGroup
	workMu        sync.Mutex
	restoreMu     sync.Mutex
	restoreWG     sync.WaitGroup
	cancelRestore bool
}

func notifyReady() {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return
	}
	_, _ = c.Write([]byte("READY=1"))
	_ = c.Close()
}

// prepareRawOnInitFailure: если raw-tsdb не работает, частичные данные
// недопустимы; если работает — ничего не трогаем и позже возьмём run_id.
// clean_start здесь не ставится: чужий/старый архив не стирается.
func (a *arch) prepareRawOnInitFailure() {
	if rawSockAlive(a.rawSock) {
		a.mu.Lock()
		a.adoptRunID = true
		a.mu.Unlock()
		return
	}
	if err := clearDir(a.rawRoot); err != nil {
		lx.Errorf("очистка raw: %v", err)
		os.Exit(1)
	}
	a.mu.Lock()
	a.runID = ""
	a.cleanStart = false
	a.mu.Unlock()
}

// clearRawFatal не завершает процесс и не отдаёт READY, пока локальный raw не
// очищен: malina-tsdb-raw ждёт готовности архивариуса, поэтому не стартует на
// частичных данных.
func (a *arch) clearRawFatal() {
	for {
		if err := clearDir(a.rawRoot); err == nil {
			return
		} else {
			lx.Errorf("очистка raw: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func (a *arch) applyLogLevel(level string) {
	if a.forcedDebug {
		lx.SetLevel(logx.Debug)
		return
	}
	if _, ok := logx.ParseLevel(level); !ok {
		level = "info"
	}
	lx.SetLevelString(level)
}

func main() {
	var (
		cfgPath  = flag.String("config", defConfig, "конфиг")
		keyPath  = flag.String("key", defKey, "ключ шифрования")
		rawRoot  = flag.String("raw", defRawRoot, "каталог raw")
		avgRoot  = flag.String("avg", defAvgRoot, "каталог avg")
		sock     = flag.String("sock", defSock, "control-сокет")
		doKeygen = flag.Bool("keygen", false, "сгенерировать arch.key и выйти")
		debug    = flag.Bool("debug", false, "подробный лог")
	)
	flag.Parse()
	if *debug {
		lx.SetLevelString("debug")
	}
	if *doKeygen {
		if err := keygen(*keyPath); err != nil {
			lx.Errorf("%v", err)
			os.Exit(1)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(*sock), 0o755); err != nil {
		lx.Errorf("%v", err)
		os.Exit(1)
	}
	a := &arch{cfgPath: *cfgPath, keyPath: *keyPath, rawRoot: *rawRoot, avgRoot: *avgRoot, rawSock: *rawRoot + "/tsdb.sock", device: deviceID(), state: "disabled", phase: "idle", stop: make(chan struct{}), forcedDebug: *debug}
	lx.Debugf("старт: конфиг=%s raw=%s avg=%s debug=%v", *cfgPath, *rawRoot, *avgRoot, *debug)
	stop := make(chan struct{})
	ctlWG := &sync.WaitGroup{}
	go control.ServeWG(*sock, stop, ctlWG, a.control)
	go a.supervise(stop)

	// Инициализация может идти долго (restore). Control-сокет уже поднят,
	// поэтому status/cancel/reload доступны; мьютекс на всё время restore
	// не держится.
	initDone := make(chan struct{})
	go func() {
		a.init()
		close(initDone)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-initDone:
		<-sig
	case <-sig:
		select {
		case <-initDone:
		default:
			// Прерываем restore, чтобы не ждать бюджет остановки.
			a.mu.Lock()
			a.cancelRestore = true
			a.mu.Unlock()
			<-initDone
		}
	}

	a.mu.Lock()
	a.shuttingDown = true
	a.cancelRestore = true
	a.mu.Unlock()
	close(stop)
	ctlWG.Wait()
	a.shutdown()
}

// shutdown доливает active-сегменты и avg. Если raw уже остановлен, размер
// файла является committed offset; если работает — offset берётся по MTSP.
func (a *arch) shutdown() {
	lx.Infof("остановка: начало flush")
	a.mu.Lock()
	a.cancelRestore = true
	a.mu.Unlock()
	// Дожидаемся ручного restore, иначе flush пойдёт параллельно с ним.
	a.restoreWG.Wait()
	a.restoreMu.Lock()
	a.restoreMu.Unlock()

	stop := make(chan struct{})
	a.mu.Lock()
	old := a.stop
	a.stop = stop
	running := a.loopsRunning
	a.loopsRunning = false
	a.mu.Unlock()
	if running {
		close(old)
		a.loopWG.Wait()
	}

	a.mu.Lock()
	enabled := a.enabled && !a.blocked
	a.mu.Unlock()
	if enabled {
		a.rawOnce()
		lx.Infof("остановка: raw flush завершён")
		a.workMu.Lock()
		a.mu.Lock()
		t, device, encore := a.tgt, a.device, a.enabled
		a.mu.Unlock()
		if t != nil && encore {
			if _, err := backupAvgOnce(t, device, a.avgRoot, true); err != nil {
				lx.Warnf("остановка: avg flush: %v", err)
			} else {
				lx.Infof("остановка: avg flush завершён")
			}
		}
		a.workMu.Unlock()
	}
	a.mu.Lock()
	if a.tgt != nil {
		_ = a.tgt.Close()
		a.tgt = nil
	}
	a.mu.Unlock()
}

// init — init-фаза: восстановление raw или clean_start. Готовность подаётся
// только после того, как локальный raw приведён в согласованное состояние.
func (a *arch) init() {
	defer notifyReady()
	c, err := loadConfig(a.cfgPath)
	if err != nil {
		a.prepareRawOnInitFailure()
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "disabled"
		a.mu.Unlock()
		return
	}
	a.applyLogLevel(c.LogLevel)
	lx.Debugf("конфиг: enabled=%v target=%s avg_scan_min=%d level=%s", c.Enabled, c.Target, c.AvgScanMin, lx.LevelName())
	a.mu.Lock()
	a.cfg = c
	a.enabled = c.Enabled
	if !c.Enabled {
		lx.Infof("архивация выключена настройкой")
		a.state, a.phase = "disabled", "idle"
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	key, err := readKey(a.keyPath)
	if err != nil {
		a.prepareRawOnInitFailure()
		a.mu.Lock()
		a.enabled = false
		a.cfg.Enabled = false
		a.lastErr = "ключ: " + err.Error()
		a.state = "credentials_invalid"
		_ = saveConfig(a.cfgPath, a.cfg)
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.key = key
	cfg := a.cfg
	a.mu.Unlock()

	// Весь target-этап под restoreMu: reload/disable во время openTarget не
	// должны потеряться и не должны перезаписать состояние.
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()

	lx.Debugf("init: подключение к target (%s)", cfg.Target)
	// На старте сеть/цель может быть ещё не готова. Даём ограниченное окно,
	// чтобы не объявлять clean_start и не терять архив из-за гонки с сетью.
	deadline := time.Now().Add(60 * time.Second)
	var t target
	for {
		t, err = openTarget(cfg, key)
		if err == nil || errors.Is(err, errCredentials) || time.Now().After(deadline) {
			break
		}
		lx.Warnf("init: target пока недоступен: %v — повтор через 5 с", err)
		if !sleepCancel(5*time.Second, a.cancelled) {
			break
		}
	}
	if a.cancelled() {
		a.mu.Lock()
		a.enabled = false
		a.state, a.phase = "restore_failed", "idle"
		a.lastErr = "запуск отменён"
		a.mu.Unlock()
		return
	}
	if err != nil {
		lx.Warnf("init: target недоступен: %v", err)
		live := rawSockAlive(a.rawSock)
		if !live {
			a.clearRawFatal()
		}
		a.mu.Lock()
		a.enabled = false
		a.blocked = false
		a.cleanStart = false
		a.cfg.Enabled = false
		a.runID = ""
		a.state, a.phase = "disabled", "idle"
		a.progress = ""
		a.lastErr = err.Error()
		_ = saveConfig(a.cfgPath, a.cfg)
		a.mu.Unlock()
		lx.Warnf("init: архивация выключена: восстановление невозможно")
		return
	}
	a.mu.Lock()
	canceled := a.cancelRestore
	a.mu.Unlock()
	if canceled {
		_ = t.Close()
		a.mu.Lock()
		a.enabled = false
		a.state, a.phase = "restore_failed", "idle"
		a.lastErr = "restore отменён"
		a.mu.Unlock()
		return
	}
	if err := ensureArchiveDirs(t, a.device); err != nil {
		_ = t.Close()
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.tgt = t
	a.mu.Unlock()
	lx.Infof("init: target %s доступен", cfg.Target)

	// Архивариус перезапущен при живом raw-tsdb: restore/очистка запрещены.
	if rawSockAlive(a.rawSock) {
		lx.Infof("raw-tsdb уже работает: restore пропущен, run_id из manifest")
		var m rawManifest
		m, ok, err := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
		a.mu.Lock()
		defer a.mu.Unlock()
		if err != nil || !ok || m.RunID == "" {
			a.enabled = false
			a.cfg.Enabled = false
			a.lastErr = "raw tsdb активен, manifest недоступен — архивация выключена"
			a.state = "disabled"
			a.phase = "idle"
			_ = saveConfig(a.cfgPath, a.cfg)
			return
		}
		a.runID = m.RunID
		a.state, a.phase = "enabled", "archive"
		a.startLoopsLocked()
		return
	}

	a.mu.Lock()
	a.cancelRestore = false
	a.restoring = true
	a.state, a.phase = "restoring", "restore"
	a.progress = "восстановление raw"
	a.mu.Unlock()
	lx.Infof("init: восстановление raw из архива")

	var runID string
	var ok bool
	for {
		runID, ok, err = restoreRawCancelable(t, a.device, a.rawRoot, a.cancelled)
		if err == nil || errors.Is(err, errArchiveInvalid) || a.cancelled() {
			break
		}
		// Обрыв доступа — повтор до успеха; отмена проверяется каждые 500 мс.
		lx.Warnf("restore raw: %v — повтор через 5 с", err)
		a.mu.Lock()
		a.lastErr = err.Error()
		a.progress = "повтор восстановления"
		a.mu.Unlock()
		if !sleepCancel(5*time.Second, a.cancelled) {
			break
		}
	}
	a.mu.Lock()
	a.restoring = false
	canceled = a.cancelRestore
	a.mu.Unlock()
	if canceled {
		lx.Warnf("init: restore отменён, локальный raw очищается")
		a.clearRawFatal()
		a.mu.Lock()
		a.enabled = false
		a.cfg.Enabled = false
		a.state, a.phase = "restore_failed", "idle"
		a.progress = ""
		a.lastErr = "restore отменён"
		_ = saveConfig(a.cfgPath, a.cfg)
		a.mu.Unlock()
		return
	}
	if err != nil {
		// Любой сбой восстановления: raw стартует пустым, avg не трогаем,
		// архивация и её настройка выключаются.
		lx.Warnf("init: восстановление не удалось (%v), raw очищается, архивация выключена", err)
		a.clearRawFatal()
		a.mu.Lock()
		a.enabled = false
		a.cfg.Enabled = false
		a.blocked = false
		a.cleanStart = false
		a.runID = ""
		a.state, a.phase = "restore_failed", "idle"
		a.progress = ""
		a.lastErr = err.Error()
		_ = saveConfig(a.cfgPath, a.cfg)
		a.mu.Unlock()
		return
	}
	if !ok {
		// Валидного архива нет: новая сессия, чужие данные не трогаем.
		a.mu.Lock()
		a.runID = newRunID()
		a.state, a.phase = "enabled", "archive"
		a.progress = ""
		runID = a.runID
		a.mu.Unlock()
		if err := resetRaw(t, a.device, runID); err != nil {
			_ = t.Close()
			a.mu.Lock()
			a.lastErr = err.Error()
			a.state = "degraded"
			a.cleanStart = true
			if a.tgt == t {
				a.tgt = nil
			}
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.startLoopsLocked()
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.runID = runID
	a.state, a.phase = "enabled", "archive"
	a.progress = ""
	a.cleanStart = false
	a.mu.Unlock()
	if !avgHasData(a.avgRoot) {
		if _, avgOK, aerr := restoreAvg(t, a.device, a.avgRoot, a.cancelled); aerr != nil {
			lx.Warnf("init: avg не восстановлена (%v), архивация выключается", aerr)
			a.clearRawFatal()
			if cerr := clearDir(a.avgRoot); cerr != nil {
				lx.Warnf("init: очистка частичной avg: %v", cerr)
			}
			a.mu.Lock()
			a.enabled = false
			a.cfg.Enabled = false
			a.state, a.phase = "restore_failed", "idle"
			a.lastErr = "avg: восстановление не удалось"
			_ = saveConfig(a.cfgPath, a.cfg)
			a.mu.Unlock()
			return
		} else if !avgOK {
			// Avg-архива ещё нет (сессия началась, но запечатанных сегментов
			// не было) — это не ошибка: raw уже принят, архивация продолжается.
			lx.Infof("init: avg-архива нет — восстановление avg пропущено")
		}
	} else {
		lx.Infof("init: локальная avg есть — восстановление avg пропущено, архив продолжается")
	}
	a.mu.Lock()
	a.startLoopsLocked()
	a.mu.Unlock()
	lx.Infof("init: raw восстановлен, run_id=%s", runID)
}

// rawSockAlive проверяет не наличие файла сокета, а живой listener: после
// SIGKILL sock-файл остаётся, но committed offset уже недоступен.
func rawSockAlive(sock string) bool {
	if sock == "" {
		return false
	}
	c, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func sleepCancel(d time.Duration, cancelled func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cancelled() {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !cancelled()
}

func (a *arch) cancelled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelRestore
}

func (a *arch) mutatingAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.shuttingDown
}

// supervise переподключает target после сетевой/флэшевой ошибки. Для clean_start
// при появлении доступа архив переинициализируется (§6.2).
func (a *arch) supervise(stop <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.reconnectTick()
		}
	}
}

func (a *arch) reconnectTick() {
	a.mu.Lock()
	if !a.enabled || a.blocked || a.restoring || a.reconnecting || a.tgt != nil {
		a.mu.Unlock()
		return
	}
	cfg, key := a.cfg, a.key
	a.reconnecting = true
	a.mu.Unlock()

	t, err := openTarget(cfg, key)

	a.mu.Lock()
	a.reconnecting = false
	defer a.mu.Unlock()
	if err != nil {
		a.lastErr = err.Error()
		if a.state == "enabled" {
			a.state = "degraded"
		}
		return
	}
	if !a.enabled || a.blocked || a.restoring || a.tgt != nil {
		_ = t.Close()
		return
	}
	if err := ensureArchiveDirs(t, a.device); err != nil {
		_ = t.Close()
		a.tgt = nil
		a.lastErr = err.Error()
		a.state = "degraded"
		return
	}
	a.tgt = t
	if adopt := a.adoptRunID || a.runID == ""; adopt {
		adoptID := a.adoptRunID
		m, ok, rerr := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
		a.adoptRunID = false
		if rerr != nil {
			_ = t.Close()
			a.tgt = nil
			a.lastErr = rerr.Error()
			if errors.Is(rerr, errArchiveInvalid) {
				a.blocked = true
				a.state = "restore_required"
			} else {
				a.state = "degraded"
			}
			return
		}
		if ok && m.RunID == "" {
			a.blocked = true
			a.state = "restore_required"
			a.lastErr = "пустой run_id в manifest"
			return
		}
		if ok {
			if adoptID {
				a.runID = m.RunID
			} else {
				a.blocked = true
				a.state = "restore_required"
				a.lastErr = "на target есть архив — нужно восстановление или очистка"
				return
			}
		} else {
			// manifest отсутствует — валидного архива нет, можно начать сессию.
			a.cleanStart = true
		}
	}
	if a.cleanStart {
		m, ok, rerr := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
		if rerr != nil {
			_ = t.Close()
			a.tgt = nil
			a.lastErr = rerr.Error()
			a.state = "degraded"
			return
		}
		if ok && m.RunID == "" {
			a.blocked = true
			a.state = "restore_required"
			a.lastErr = "на target есть архив — нужно восстановление или очистка"
			return
		}
		if ok {
			// Пока ждали сеть, на target обнаружился архив: не стираем его.
			a.cleanStart = false
			a.blocked = true
			a.state = "restore_required"
			a.lastErr = "на target есть архив — нужно восстановление или очистка"
			return
		}
		a.runID = newRunID()
		if err := resetRaw(t, a.device, a.runID); err != nil {
			a.lastErr = err.Error()
			a.state = "degraded"
			_ = t.Close()
			a.tgt = nil
			return
		}
		a.cleanStart = false
		a.state, a.phase = "enabled", "archive"
	}
	if a.state == "degraded" {
		a.state = "enabled"
		a.phase = "archive"
	}
	a.startLoopsLocked()
}

// reloadConfig применяет новые настройки: переподключает target, даже если
// сменились host/path/protocol. Во время restore ждёт его отмены.
func (a *arch) reloadConfig() {
	a.mu.Lock()
	a.cancelRestore = true
	a.mu.Unlock()
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	a.stopLoops()

	c, err := loadConfig(a.cfgPath)
	if err != nil {
		lx.Warnf("reload: конфиг: %v", err)
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		if a.tgt != nil {
			_ = a.tgt.Close()
			a.tgt = nil
		}
		a.mu.Unlock()
		return
	}
	key, err := readKey(a.keyPath)
	if err != nil {
		lx.Warnf("reload: ключ: %v", err)
		a.mu.Lock()
		a.lastErr = "ключ: " + err.Error()
		a.state = "credentials_invalid"
		a.blocked = true
		if a.tgt != nil {
			_ = a.tgt.Close()
			a.tgt = nil
		}
		a.mu.Unlock()
		return
	}

	a.applyLogLevel(c.LogLevel)
	lx.Infof("reload: target=%s enabled=%v level=%s", c.Target, c.Enabled, lx.LevelName())
	a.mu.Lock()
	a.cfg, a.key = c, key
	if a.tgt != nil {
		_ = a.tgt.Close()
		a.tgt = nil
	}
	a.enabled = c.Enabled
	if !c.Enabled {
		a.state, a.phase = "disabled", "idle"
		a.blocked = false
		a.mu.Unlock()
		return
	}
	a.blocked = false
	a.mu.Unlock()

	lx.Debugf("reload: подключение к target (%s)", c.Target)
	t, err := openTarget(c, key)
	if err != nil {
		lx.Warnf("reload: target недоступен: %v", err)
		a.mu.Lock()
		a.lastErr = err.Error()
		if errors.Is(err, errCredentials) {
			a.state = "credentials_invalid"
			a.blocked = true
		} else {
			a.state = "degraded"
		}
		a.mu.Unlock()
		return
	}
	if err := ensureArchiveDirs(t, a.device); err != nil {
		a.mu.Lock()
		_ = t.Close()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.tgt = t
	a.state, a.phase = "enabled", "archive"
	runID := a.runID
	needReset := a.cleanStart
	adopt := a.adoptRunID || runID == ""
	a.mu.Unlock()
	if adopt {
		m, ok, rerr := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
		if rerr != nil {
			a.mu.Lock()
			_ = t.Close()
			a.tgt = nil
			a.lastErr = rerr.Error()
			if errors.Is(rerr, errArchiveInvalid) {
				a.blocked = true
				a.state = "restore_required"
			} else {
				a.state = "degraded"
			}
			a.mu.Unlock()
			return
		}
		if ok && m.RunID == "" {
			a.mu.Lock()
			a.blocked = true
			a.state = "restore_required"
			a.lastErr = "пустой run_id в manifest"
			a.mu.Unlock()
			return
		}
		if ok {
			if a.adoptRunID {
				runID = m.RunID
			} else {
				a.mu.Lock()
				a.blocked = true
				a.state = "restore_required"
				a.lastErr = "на target есть архив — нужно восстановление или очистка"
				a.adoptRunID = false
				a.mu.Unlock()
				return
			}
		} else {
			needReset = true
			runID = newRunID()
		}
		a.mu.Lock()
		a.runID = runID
		a.adoptRunID = false
		a.mu.Unlock()
	}
	if needReset {
		if err := resetRaw(t, a.device, runID); err != nil {
			a.mu.Lock()
			a.lastErr = err.Error()
			a.state = "degraded"
			_ = t.Close()
			a.tgt = nil
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		a.cleanStart = false
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.startLoopsLocked()
	a.mu.Unlock()
}

func openTarget(c config, key []byte) (target, error) {
	var t target
	var err error
	switch c.Target {
	case "flash":
		t, err = openFlash(c.Flash.UUID)
	case "network":
		pw, derr := decryptSecret(key, archAAD(c.Network.Protocol, "password"), c.Network.Password)
		if derr != nil {
			return nil, fmt.Errorf("%w: %v", errCredentials, derr)
		}
		switch c.Network.Protocol {
		case "sftp":
			t, err = dialSFTP(c.Network, pw)
		case "smb2":
			t, err = dialSMB(c.Network, pw)
		default:
			err = fmt.Errorf("неизвестный протокол %q", c.Network.Protocol)
		}
	default:
		err = fmt.Errorf("неизвестный target %q", c.Target)
	}
	if err != nil {
		if c.Target == "network" && (strings.Contains(err.Error(), "authenticate") || strings.Contains(err.Error(), "Authentication") || strings.Contains(err.Error(), "LOGON_FAILURE") || strings.Contains(err.Error(), "logon is invalid") || strings.Contains(err.Error(), "bad username") || strings.Contains(err.Error(), "host key") || strings.Contains(err.Error(), "knownhosts")) {
			return nil, fmt.Errorf("%w: %v", errCredentials, err)
		}
		return nil, err
	}
	return t, nil
}

// ensureArchiveDirs создаёт namespace устройства. Вызывается только при
// включённой архивации: read-only test не должен ничего создавать на target.
func ensureArchiveDirs(t target, device string) error {
	if err := t.MkdirAll(pathJoin(device, "raw")); err != nil {
		return err
	}
	return t.MkdirAll(pathJoin(device, "avg"))
}

func (a *arch) startLoopsLocked() {
	if a.enabled && a.tgt != nil && !a.loopsRunning && !a.blocked && !a.shuttingDown {
		a.loopsRunning = true
		stop := a.stop
		a.loopWG.Add(2)
		go a.rawLoop(stop)
		go a.avgLoop(stop)
	}
}

func (a *arch) rawLoop(stop <-chan struct{}) {
	defer a.loopWG.Done()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	a.rawOnce()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.rawOnce()
		}
	}
}

func (a *arch) rawOnce() {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	a.mu.Lock()
	t, device, runID := a.tgt, a.device, a.runID
	active, blocked := a.enabled, a.blocked
	a.mu.Unlock()
	if t == nil || !active || runID == "" || blocked {
		return
	}
	size, err := archiveRawOnce(t, device, runID, a.rawRoot, a.rawSock)
	if err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		if errors.Is(err, errRunIDMatch) {
			// Автоматически чужой архив не затираем (§7.4).
			a.state = "restore_required"
			a.blocked = true
			a.mu.Unlock()
			lx.Warnf("raw: %v", err)
			return
		}
		a.state = "degraded"
		_ = t.Close()
		a.tgt = nil
		a.mu.Unlock()
		lx.Warnf("raw: %v", err)
		return
	}
	lx.Debugf("raw: выгружено, manifest=%d Б, run_id=%s", size, runID)
	a.mu.Lock()
	a.lastRaw = time.Now()
	a.rawMfSize = size
	a.lastErr = ""
	a.mu.Unlock()
}

func (a *arch) avgLoop(stop <-chan struct{}) {
	defer a.loopWG.Done()
	a.avgOnce()
	for {
		d := time.Duration(a.cfg.AvgScanMin) * time.Minute
		if d <= 0 {
			d = 5 * time.Minute
		}
		t := time.NewTimer(d)
		select {
		case <-stop:
			t.Stop()
			return
		case <-t.C:
			a.avgOnce()
		}
	}
}

func (a *arch) avgOnce() {
	a.workMu.Lock()
	defer a.workMu.Unlock()
	a.mu.Lock()
	t, device := a.tgt, a.device
	active, blocked := a.enabled, a.blocked
	a.mu.Unlock()
	if t == nil || !active || blocked {
		return
	}
	size, err := backupAvgOnce(t, device, a.avgRoot, false)
	if err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		if !strings.Contains(err.Error(), "нет сегментов") {
			a.state = "degraded"
			_ = t.Close()
			a.tgt = nil
		}
		a.mu.Unlock()
		lx.Warnf("avg: %v", err)
		return
	}
	lx.Debugf("avg: зеркало обновлено, manifest=%d Б", size)
	a.mu.Lock()
	a.lastAvg = time.Now()
	a.avgMfSize = size
	a.lastErr = ""
	a.mu.Unlock()
}

func (a *arch) control(line []byte) any {
	var req struct {
		Cmd  string `json:"cmd"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return map[string]any{"ok": false, "code": "bad_request", "error": "bad json"}
	}
	lx.Debugf("control: cmd=%s mode=%s", req.Cmd, req.Mode)
	switch req.Cmd {
	case "ping":
		return map[string]any{"ok": true}
	case "status":
		return a.status()
	case "test":
		return a.test()
	case "reload":
		if !a.mutatingAllowed() {
			return map[string]any{"ok": false, "error": "остановка"}
		}
		a.reloadConfig()
		return map[string]any{"ok": true}
	case "disable":
		if !a.mutatingAllowed() {
			return map[string]any{"ok": false, "error": "остановка"}
		}
		a.disable()
		return map[string]any{"ok": true}
	case "cancel":
		a.mu.Lock()
		a.cancelRestore = true
		a.mu.Unlock()
		return map[string]any{"ok": true}
	case "enable":
		if !a.mutatingAllowed() {
			return map[string]any{"ok": false, "error": "остановка"}
		}
		switch req.Mode {
		case "restore":
			a.mu.Lock()
			if a.shuttingDown {
				a.mu.Unlock()
				return map[string]any{"ok": false, "error": "остановка"}
			}
			a.restoreWG.Add(1)
			a.mu.Unlock()
			go func() {
				defer a.restoreWG.Done()
				a.enableRestore()
			}()
			return map[string]any{"ok": true, "state": "restoring"}
		case "reset":
			if err := a.enableReset(); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true, "state": a.state}
		case "auto":
			st, err := a.enableAuto()
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true, "state": st}
		default:
			return map[string]any{"ok": false, "error": "mode: auto|restore|reset"}
		}
	}
	return map[string]any{"ok": false, "error": "unknown cmd"}
}

func (a *arch) status() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return map[string]any{
		"ok": true, "enabled": a.enabled, "target": a.cfg.Target,
		"state": a.state, "phase": a.phase, "run_id": a.runID,
		"last_error": a.lastErr, "last_raw": a.lastRaw.Format(time.RFC3339),
		"last_avg":      a.lastAvg.Format(time.RFC3339),
		"manifest_size": a.rawMfSize + a.avgMfSize,
		"log_level":     strings.ToLower(lx.LevelName()),
		"progress":      a.progress,
	}
}

func (a *arch) test() any {
	a.mu.Lock()
	key, cfg := a.key, a.cfg
	a.mu.Unlock()
	if len(key) == 0 {
		// При выключенной архивации ключ не читается на старте, но test
		// обязан уметь проверить доступ и расшифровать пароль.
		k, err := readKey(a.keyPath)
		if err != nil {
			return map[string]any{"ok": false, "error": "ключ: " + err.Error(), "state": "credentials_invalid"}
		}
		key = k
		a.mu.Lock()
		a.key = k
		a.mu.Unlock()
	}
	t, err := openTarget(cfg, key)
	if t != nil {
		defer t.Close()
	}
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	_, rawOK, rawErr := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
	am, avgOK, avgErr := readAvgManifest(t, pathJoin(a.device, "avg", "manifest.json"))
	avgOK = avgOK && len(am.Files) > 0
	if rawErr != nil || avgErr != nil {
		return map[string]any{"ok": false, "error": "archive read failed"}
	}
	a.mu.Lock()
	if !a.enabled && (rawOK || avgOK) {
		a.blocked = true
		a.state = "restore_required"
	} else if a.state == "restore_required" && !rawOK && !avgOK {
		a.blocked = false
		a.state = "disabled"
	}
	a.mu.Unlock()
	return map[string]any{"ok": true, "raw_archive": rawOK, "avg_archive": avgOK}
}

func (a *arch) disable() {
	lx.Infof("выключение архивации")
	a.mu.Lock()
	a.cancelRestore = true
	a.mu.Unlock()
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	a.stopLoops()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tgt != nil {
		_ = a.tgt.Close()
		a.tgt = nil
	}
	a.enabled = false
	a.blocked = false
	a.cleanStart = false
	a.state = "disabled"
	a.phase = "idle"
	a.cfg.Enabled = false
	_ = saveConfig(a.cfgPath, a.cfg)
}

func (a *arch) stopLoops() {
	a.mu.Lock()
	old := a.stop
	a.stop = make(chan struct{})
	running := a.loopsRunning
	a.loopsRunning = false
	a.mu.Unlock()
	if running {
		close(old)
		a.loopWG.Wait()
	}
}

func (a *arch) ensureTarget() (target, error) {
	a.mu.Lock()
	t := a.tgt
	cfg, key := a.cfg, a.key
	a.mu.Unlock()
	if t != nil {
		return t, nil
	}
	t, err := openTarget(cfg, key)
	if err != nil {
		return nil, err
	}
	if err := ensureArchiveDirs(t, a.device); err != nil {
		_ = t.Close()
		return nil, err
	}
	a.mu.Lock()
	if a.tgt == nil {
		a.tgt = t
	} else {
		_ = t.Close()
		t = a.tgt
	}
	a.mu.Unlock()
	return t, nil
}

func (a *arch) enableReset() error {
	lx.Infof("включение: новая сессия, старый архив будет очищен")
	if !a.restoreMu.TryLock() {
		return errors.New("операция уже выполняется")
	}
	defer a.restoreMu.Unlock()
	a.stopLoops()
	t, err := a.ensureTarget()
	if err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.mu.Unlock()
		return err
	}
	return a.startNewSession(t)
}

// enableAuto включается без пугающего диалога, если архива нет. Если на
// target есть manifest, состояние переводится в restore_required: решение
// (восстановить/стереть) остаётся за оператором.
func (a *arch) enableAuto() (string, error) {
	if !a.restoreMu.TryLock() {
		return "", errors.New("операция уже выполняется")
	}
	defer a.restoreMu.Unlock()
	a.stopLoops()
	t, err := a.ensureTarget()
	if err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.mu.Unlock()
		return "", err
	}
	mraw, rawOK, rawErr := readRawManifest(t, pathJoin(a.device, "raw", "manifest.json"))
	mavg, avgOK, avgErr := readAvgManifest(t, pathJoin(a.device, "avg", "manifest.json"))
	avgOK = avgOK && len(mavg.Files) > 0
	if rawErr != nil && !errors.Is(rawErr, errArchiveInvalid) {
		return "", rawErr
	}
	if avgErr != nil && !errors.Is(avgErr, errArchiveInvalid) {
		return "", avgErr
	}
	a.mu.Lock()
	curRun := a.runID
	a.mu.Unlock()
	if rawOK && mraw.RunID != "" && curRun != "" && mraw.RunID == curRun {
		// Тот же запуск raw: продолжаем архив с текущего окна.
		a.mu.Lock()
		a.blocked = false
		a.cleanStart = false
		a.enabled = true
		a.cfg.Enabled = true
		a.state = "enabled"
		a.phase = "archive"
		_ = saveConfig(a.cfgPath, a.cfg)
		a.startLoopsLocked()
		a.mu.Unlock()
		lx.Infof("включение: продолжение архива run_id=%s", curRun)
		return "continued", nil
	}
	if rawOK || avgOK || errors.Is(rawErr, errArchiveInvalid) || errors.Is(avgErr, errArchiveInvalid) {
		a.mu.Lock()
		a.blocked = true
		a.state = "restore_required"
		a.phase = "idle"
		a.lastErr = "на target есть архив — нужно восстановление или очистка"
		a.mu.Unlock()
		lx.Infof("включение: на target найден архив, нужно решение оператора")
		return "restore_required", nil
	}
	if err := a.startNewSession(t); err != nil {
		return "", err
	}
	return "enabled", nil
}

// startNewSession создаёт новую сессию raw-архива. Вызывается с restoreMu.
func (a *arch) startNewSession(t target) error {
	a.mu.Lock()
	a.cancelRestore = false
	a.blocked = false
	a.cleanStart = false
	a.adoptRunID = false
	a.enabled = true
	a.cfg.Enabled = true
	a.runID = newRunID()
	runID := a.runID
	a.mu.Unlock()

	if err := resetRaw(t, a.device, runID); err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.enabled = false
		a.cfg.Enabled = false
		_ = t.Close()
		if a.tgt == t {
			a.tgt = nil
		}
		a.mu.Unlock()
		return err
	}

	a.mu.Lock()
	a.state = "enabled"
	a.phase = "archive"
	_ = saveConfig(a.cfgPath, a.cfg)
	a.startLoopsLocked()
	a.mu.Unlock()
	return nil
}

func (a *arch) enableRestore() {
	lx.Infof("включение: восстановление из архива")
	if !a.restoreMu.TryLock() {
		return
	}
	defer a.restoreMu.Unlock()
	a.stopLoops()
	a.mu.Lock()
	if a.shuttingDown {
		a.mu.Unlock()
		return
	}
	a.cancelRestore = false
	a.blocked = false
	a.adoptRunID = false
	a.restoring = true
	a.state = "restoring"
	a.phase = "restore"
	a.progress = "подготовка"
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.restoring = false
		a.mu.Unlock()
	}()

	t, err := a.ensureTarget()
	if err != nil {
		a.failRestore(err.Error(), false, false)
		return
	}
	a.mu.Lock()
	device := a.device
	a.mu.Unlock()
	// Останавливаем producers/tsdb; web остаётся. Ошибка stop — не начинаем restore.
	units := []string{"map-reader", "kes-reader", "malina-bms-reader", "malina-bms-proxy", "malina-grid-reader", "malina-tsdb-raw", "malina-tsdb"}
	if err := stopUnits(units); err != nil {
		a.failRestore(err.Error(), false, false)
		return
	}
	runID, ok, err := restoreRawCancelable(t, device, a.rawRoot, a.cancelled)
	if err == nil && ok {
		// Явное восстановление пользователя: заменяем и raw, и avg.
		a.mu.Lock()
		a.progress = "восстановление avg"
		a.mu.Unlock()
		var avgOK bool
		_, avgOK, err = restoreAvg(t, device, a.avgRoot, a.cancelled)
		if err == nil && !avgOK {
			err = errors.New("avg archive отсутствует")
		}
	}
	a.mu.Lock()
	canceled := a.cancelRestore
	a.mu.Unlock()
	if canceled {
		a.failRestore("restore отменён", true, true)
		return
	}
	if err != nil || !ok {
		msg := "в архиве нет raw manifest"
		if err != nil {
			msg = err.Error()
		}
		a.failRestore(msg, true, true)
		return
	}
	a.mu.Lock()
	a.runID = runID
	a.enabled = true
	a.cfg.Enabled = true
	a.blocked = false
	a.cleanStart = false
	a.state = "enabled"
	a.phase = "archive"
	a.progress = ""
	_ = saveConfig(a.cfgPath, a.cfg)
	a.startLoopsLocked()
	a.mu.Unlock()
	a.mu.Lock()
	stopping := a.shuttingDown
	a.mu.Unlock()
	if stopping {
		return
	}
	lx.Infof("restore завершён, run_id=%s; запуск сервисов", runID)
	if err := startUnits([]string{"malina-tsdb-raw", "malina-tsdb", "map-reader", "kes-reader", "malina-bms-reader", "malina-bms-proxy", "malina-grid-reader"}); err != nil {
		a.mu.Lock()
		a.lastErr = err.Error()
		a.state = "degraded"
		a.mu.Unlock()
	}
}

func (a *arch) failRestore(msg string, clearRaw, clearAvg bool) {
	if clearRaw {
		if err := clearDir(a.rawRoot); err != nil {
			a.setRestoreFailed(msg + "; очистка raw: " + err.Error())
			return
		}
	}
	if clearAvg {
		if err := clearDir(a.avgRoot); err != nil {
			a.setRestoreFailed(msg + "; очистка avg: " + err.Error())
			return
		}
	}
	a.mu.Lock()
	stopping := a.shuttingDown
	a.mu.Unlock()
	if !stopping {
		_ = startUnits([]string{"malina-tsdb-raw", "malina-tsdb", "map-reader", "kes-reader", "malina-bms-reader", "malina-bms-proxy", "malina-grid-reader"})
	}
	a.mu.Lock()
	a.enabled = false
	a.blocked = false
	a.cleanStart = false
	a.cfg.Enabled = false
	a.state = "restore_failed"
	a.phase = "idle"
	a.progress = ""
	a.lastErr = msg
	_ = saveConfig(a.cfgPath, a.cfg)
	a.mu.Unlock()
}

func (a *arch) setRestoreFailed(msg string) {
	lx.Errorf("restore: %s", msg)
	a.mu.Lock()
	a.enabled = false
	a.blocked = false
	a.cleanStart = false
	a.cfg.Enabled = false
	a.state = "restore_failed"
	a.phase = "idle"
	a.progress = ""
	a.lastErr = msg
	_ = saveConfig(a.cfgPath, a.cfg)
	a.mu.Unlock()
}

func stopUnits(units []string) error {
	for _, u := range units {
		if err := exec.Command("systemctl", "stop", u).Run(); err != nil {
			return fmt.Errorf("stop %s: %w", u, err)
		}
	}
	return nil
}

func startUnits(units []string) error {
	var errs []string
	for _, u := range units {
		if err := exec.Command("systemctl", "start", u).Run(); err != nil {
			errs = append(errs, fmt.Sprintf("start %s: %v", u, err))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func openFlash(uuid string) (target, error) {
	if uuid == "" {
		return nil, errors.New("flash: не задан UUID раздела")
	}
	if err := checkFlashAllowed(uuid); err != nil {
		return nil, err
	}
	base := "/run/media/malina-arch"
	if v := os.Getenv("MALINA_ARCH_FLASH_ROOT"); v != "" {
		base = v
	}
	if os.Getenv("MALINA_ARCH_FLASH_ROOT") == "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return nil, err
		}
		if err := exec.Command("mount", "/dev/disk/by-uuid/"+strings.TrimSpace(uuid), base).Run(); err != nil {
			return nil, fmt.Errorf("mount flash: %w", err)
		}
		return &localTarget{root: base, mounted: true}, nil
	}
	return &localTarget{root: base}, nil
}
