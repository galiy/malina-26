package main

import (
	"log"
	"time"
)

// pollRetries — число попыток чтения МАП за один цикл опроса.
const pollRetries = 3

// pollPort опрашивает все МАП одного порта (последовательно, одной горутиной).
// Период — 1 раз в секунду; потеря МАП — maxMiss неудачных ЦИКЛОВ подряд.
// Цикл считается неудачным, только если провалились все pollRetries попытки;
// успех любой попытки сбрасывает счётчик. Так короткий сбой обмена не роняет МАП.
func pollPort(cfg *config, reg *Registry, stop chan struct{}, p *Port, port, proto string, baud int, entries []*MapEntry) {
	defer p.close()

	t := time.NewTicker(cfg.pollInterval)
	defer t.Stop()
	miss := make(map[*MapEntry]int, len(entries))

	for {
		select {
		case <-stop:
			for _, e := range entries {
				reg.Remove(e)
			}
			return
		case <-t.C:
		}

		alive := entries[:0]
		for _, e := range entries {
			select {
			case <-stop: // остановка — не начинаем новый обмен
				for _, x := range entries {
					reg.Remove(x)
				}
				return
			default:
			}
			regions := regionsFor(reg.Model(e))
			var (
				mem      memMap
				err      error
				firstErr error
			)
			t0 := time.Now()
			for attempt := 0; attempt < pollRetries; attempt++ {
				if proto == "modbus" {
					mem, err = readRegionsModbus(p, e.unit, regions, cfg.probeResponse)
				} else {
					mem, err = readRegionsNative(p, regions, cfg.probeEcho, cfg.probeResponse)
				}
				if err == nil {
					if attempt > 0 {
						cfg.debugf("poll %s unit=%d: OK с попытки %d за %s (первая: %v)", port, e.unit, attempt+1, time.Since(t0), firstErr)
					}
					break
				}
				if attempt == 0 {
					firstErr = err
				}
				time.Sleep(100 * time.Millisecond) // пауза перед повтором
			}
			if err != nil {
				// Промах ЦИКЛА: не удалась ни одна из попыток.
				miss[e]++
				if miss[e] >= cfg.maxMiss {
					log.Printf("МАП %s unit=%d потерян (%d неудачных циклов подряд: %v)", port, e.unit, miss[e], firstErr)
					delete(miss, e)
					reg.Remove(e)
					continue
				}
				cfg.debugf("poll %s unit=%d: цикл неудачен (%v), промахов %d/%d", port, e.unit, firstErr, miss[e], cfg.maxMiss)
				alive = append(alive, e)
				continue
			}
			miss[e] = 0
			reg.Touch(e)
			simInjectKesBms(mem) // ВРЕМЕННО: эмуляция КЭС/BMS для UI (удалить)
			e.slot.Publish(buildContract(mem, reg.Meta(e)))
			alive = append(alive, e)
		}
		entries = alive
		if len(entries) == 0 {
			return
		}
	}
}
