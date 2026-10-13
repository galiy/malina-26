// Package stat — модуль сохранения сырой телеметрии пулеров.
//
// Пулер после каждого цикла вызывает Publish (неблокирующий, fire-and-forget);
// вся логика (батчинг и отправка в raw-tsdb по Unix-API) — здесь, вне пулера.
// Смысл: в пулере расчётов нет (docs/map-reader.md, docs/kes-reader.md;
// docs/telemetry-store.md §14.2).
package stat

import (
	"log"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tsdbclient"
)

// Sink — приёмник сырья: очередь + горутина записи в raw-tsdb.
type Sink struct {
	cli  *tsdbclient.Client
	ch   chan tsdbclient.Row
	done chan struct{}
	wg   sync.WaitGroup
}

// New создаёт приёмник для raw-tsdb по Unix-сокету.
func New(sock string) *Sink {
	s := &Sink{
		cli:  tsdbclient.New(sock),
		ch:   make(chan tsdbclient.Row, 4096),
		done: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.loop()
	return s
}

// Publish — неблокирующий: кладёт снапшот в очередь, возвращается сразу.
// При переполнении строку отбрасываем (потеря сырья не критична, §14.3).
func (s *Sink) Publish(typ, id string, t int64, version uint16, payload []byte) {
	select {
	case s.ch <- tsdbclient.Row{Type: typ, ID: id, Time: t, Version: version, Payload: payload}:
	default:
	}
}

func (s *Sink) loop() {
	defer s.wg.Done()
	var batch []tsdbclient.Row
	t := time.NewTicker(200 * time.Millisecond) // видимость ≤ 1 c (§14.4)
	defer t.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if _, _, err := s.cli.Write(batch); err != nil {
			log.Printf("stat: запись в raw-tsdb: %v", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case r := <-s.ch:
			batch = append(batch, r)
			if len(batch) >= 512 {
				flush()
			}
		case <-t.C:
			flush()
		case <-s.done:
			flush()
			return
		}
	}
}

// Close останавливает горутину и дожидается финального сброса очереди.
func (s *Sink) Close() {
	close(s.done)
	s.wg.Wait()
}
