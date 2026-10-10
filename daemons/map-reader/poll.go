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
//
// cmds — очередь команд записи к этому порту: пока она непуста, пулер исполняет
// команду (порт занят только этой горутиной, поэтому обмен сериализован), затем
// возвращается к опросу.
func pollPort(cfg *config, reg *Registry, stop chan struct{}, p *Port, port, proto string, baud int, entries []*MapEntry, cmds chan *writeJob) {
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
		case job := <-cmds:
			// Прерываем пулинг, исполняем команду (внутри — полный цикл чтения
			// и публикация в shm), отвечаем и возвращаемся к опросу.
			job.resp <- executeWrite(cfg, reg, p, proto, entries, job.req)
			continue
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
			var (
				mem      memMap
				err      error
				firstErr error
			)
			t0 := time.Now()
			for attempt := 0; attempt < pollRetries; attempt++ {
				mem, err = readEntry(cfg, p, proto, e)
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
			maybeSyncTime(cfg, reg, p, proto, entries, e, mem, time.Now()) // синхронизация времени МАП
			alive = append(alive, e)
		}
		entries = alive
		if len(entries) == 0 {
			return
		}
	}
}

// readEntry читает все документированные области одного МАП по его протоколу.
func readEntry(cfg *config, p *Port, proto string, e *MapEntry) (memMap, error) {
	regions := regionsFor(e.info.Model)
	if proto == "modbus" {
		return readRegionsModbus(p, e.unit, regions, cfg.probeResponse)
	}
	return readRegionsNative(p, regions, cfg.probeEcho, cfg.probeResponse)
}
