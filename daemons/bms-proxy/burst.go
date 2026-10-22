package main

import (
	"sync"
	"time"
)

// burstGap — пауза, по которой поток режется на «всплески» (docs/bms-proxy.md §5.1).
const burstGap = 50 * time.Millisecond

// burstMax — предохранитель от бесконечного всплеска (непрерывный поток).
const burstMax = 4096

// burstLogger пишет в debug сырые всплески чтения/записи порта с точным
// временем. Логгер асинхронный: не задерживает ответ/чтение.
type burstLogger struct {
	name string

	mu      sync.Mutex
	dir     byte // 'R' | 'T'
	started time.Time
	buf     []byte
	timer   *time.Timer
	stopped bool
}

func newBurstLogger(name string) *burstLogger { return &burstLogger{name: name} }

// Feed добавляет байты направления dir ('R' чтение, 'T' запись). При смене
// направления текущий всплеск сбрасывается сразу.
func (b *burstLogger) Feed(dir byte, p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	if len(b.buf) > 0 && b.dir != dir {
		b.flushLocked()
	}
	if len(b.buf) == 0 {
		b.started = time.Now()
		b.dir = dir
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) >= burstMax {
		b.flushLocked()
		return
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(burstGap, b.flush)
}

// Flush принудительно сбрасывает накопленный всплеск.
func (b *burstLogger) Flush() {
	b.mu.Lock()
	b.flushLocked()
	b.mu.Unlock()
}

// Stop сбрасывает остаток и больше не принимает данные.
func (b *burstLogger) Stop() {
	b.mu.Lock()
	b.stopped = true
	b.flushLocked()
	b.mu.Unlock()
}

func (b *burstLogger) flush() {
	b.mu.Lock()
	b.flushLocked()
	b.mu.Unlock()
}

func (b *burstLogger) flushLocked() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if len(b.buf) == 0 {
		return
	}
	dir := "RX"
	if b.dir == 'T' {
		dir = "TX"
	}
	debugf("burst %s t=%d dir=%s len=%d hex=%X raw=%q",
		b.name, b.started.UnixMicro(), dir, len(b.buf), b.buf, string(b.buf))
	b.buf = b.buf[:0]
}
