// retention.go — очистка raw-инстанса (§14.1): удаление ЦЕЛЫХ (запечатанных)
// сегментов по возрасту (R) и/или при превышении лимита объёма. Активный
// (незапечатанный) сегмент не трогаем. У avg-инстанса retention выключен.
package main

import (
	"log"
	"os"
	"sort"
	"time"
)

func (s *Store) retentionLoop() {
	defer s.wg.Done()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	s.retentionSweep()
	for {
		select {
		case <-t.C:
			s.retentionSweep()
		case <-s.stop:
			return
		}
	}
}

func (s *Store) retentionSweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	ageMS := int64(s.retention / time.Millisecond)

	// возрастная очистка
	for _, ser := range s.series {
		kept := ser.sealed[:0]
		for _, ref := range ser.sealed {
			if s.retention > 0 && ref.maxTime < now-ageMS {
				os.Remove(ref.path)
				continue
			}
			kept = append(kept, ref)
		}
		ser.sealed = kept
	}
	if s.limitBytes <= 0 {
		return
	}

	// объёмный лимит: удаляем старейшие (по maxTime) целые сегменты
	type seg struct {
		ser *series
		ref *segRef
	}
	var all []seg
	var total int64
	for _, ser := range s.series {
		for _, ref := range ser.sealed {
			if st, err := os.Stat(ref.path); err == nil {
				total += st.Size()
			}
			all = append(all, seg{ser, ref})
		}
	}
	if total <= s.limitBytes {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ref.maxTime < all[j].ref.maxTime })
	for _, sg := range all {
		if total <= s.limitBytes {
			break
		}
		if st, err := os.Stat(sg.ref.path); err == nil {
			total -= st.Size()
		}
		os.Remove(sg.ref.path)
		sg.ser.sealed = removeRef(sg.ser.sealed, sg.ref)
	}
	log.Printf("retention: объём приведён к лимиту %d МБ", s.limitBytes>>20)
}

func removeRef(list []*segRef, ref *segRef) []*segRef {
	out := list[:0]
	for _, r := range list {
		if r != ref {
			out = append(out, r)
		}
	}
	return out
}
