package main

import (
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// kesPollRetries — число попыток чтения КЭС за один цикл опроса.
const kesPollRetries = 3

// pollPort опрашивает один КЭС: раз в pollInterval читает окна, публикует слот.
// Потеря — maxMiss неудачных ЦИКЛОВ подряд (цикл неудачен, только если провалились
// все попытки).
func pollPort(cfg *config, reg *Registry, stop chan struct{}, p *transport.Port, e *kesEntry, cmds chan *writeJob) {
	defer p.Close()

	t := time.NewTicker(cfg.pollInterval)
	defer t.Stop()

	miss := 0
	for {
		select {
		case <-stop:
			reg.Remove(e)
			return
		case job := <-cmds:
			job.resp <- executeWrite(cfg, reg, p, []*kesEntry{e}, job.req)
			continue
		case <-t.C:
		}

		var (
			img      kesImg
			err      error
			firstErr error
		)
		for attempt := 0; attempt < kesPollRetries; attempt++ {
			img, err = readAllWindows(p, cfg.probeEcho, cfg.probeResponse)
			if err == nil {
				break
			}
			if attempt == 0 {
				firstErr = err
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			miss++
			if miss >= cfg.maxMiss {
				lx.Warnf("КЭС %s потерян (%d неудачных циклов: %v)", e.port, miss, firstErr)
				reg.Remove(e)
				return
			}
			cfg.debugf("poll %s: цикл неудачен (%v), промахов %d/%d", e.port, firstErr, miss, cfg.maxMiss)
			continue
		}
		miss = 0
		reg.SetInfo(e, parseKes(img))
		reg.Touch(e)
		meta := reg.Meta(e)
		d := parseData(img)
		e.slot.Publish(buildPayload(meta, d))
		publishRaw(meta.Serial, time.Now().UnixMilli(), d) // raw-хранилище (§14)
	}
}
