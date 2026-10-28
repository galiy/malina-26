// retention.go — очистка хранилища целыми (запечатанными) сегментами; активный
// (незапечатанный) сегмент не трогаем. Два механизма:
//   - raw (§14.1): по возрасту (retention) и/или лимиту объёма (limit);
//   - avg (§14): по свободному месту на ФС — суточный проход, при свободном месте
//     ниже порога удаляем старейшие сегменты, освобождая не менее заданного объёма.
package main

import (
	"log"
	"os"
	"sort"
	"syscall"
	"time"
)

// rawFreeReserve — резерв свободного места на ФС raw (tmpfs). При снижении
// ниже этого значения retention удаляет старейшие сегменты, не дожидаясь
// формального превышения лимита объёма. Защита от ENOSPC, когда лимит близок
// к размеру ФС (регрессия 2026-10-28 на /run/malina-ts-raw).
const rawFreeReserve int64 = 32 << 20

// freeSpaceFn — обёртка над freeSpaceBytes для подмены в тестах.
var freeSpaceFn = freeSpaceBytes

func (s *Store) retentionLoop() {	defer s.wg.Done()
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
				// Убираем из учёта только если файл реально удалён.
				if err := os.Remove(ref.path); err == nil {
					continue
				} else {
					log.Printf("retention: удаление %s: %v", ref.path, err)
				}
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
		// Активный (незапечатанный) сегмент тоже занимает место на ФС, хотя и не
		// удаляется. Без его учёта при лимите, близком к размеру ФС, сумма
		// sealed не превышает лимит и очистка не срабатывает — запись упирается
		// в ENOSPC (см. регрессию 2026-10-28 на /run/malina-ts-raw).
		if ser.active != nil {
			total += ser.active.size
		}
	}
	// Дополнительная защита: если свободного места меньше резерва, чистим
	// старейшие сегменты, даже пока сумма формально в пределах лимита.
	free, ferr := freeSpaceFn(s.root)
	overLimit := total > s.limitBytes
	lowFree := ferr == nil && free < rawFreeReserve
	if !overLimit && !lowFree {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ref.maxTime < all[j].ref.maxTime })
	for _, sg := range all {
		if total <= s.limitBytes && (ferr != nil || free >= rawFreeReserve) {
			break
		}
		st, serr := os.Stat(sg.ref.path)
		if serr != nil {
			// Файла нет — просто убираем ссылку из учёта.
			sg.ser.sealed = removeRef(sg.ser.sealed, sg.ref)
			continue
		}
		if err := os.Remove(sg.ref.path); err != nil {
			// Не удалось — оставляем в учёте, не завышаем свободное место.
			log.Printf("retention: удаление %s: %v", sg.ref.path, err)
			continue
		}
		total -= st.Size()
		free += st.Size() // приблизительно: освобождаемое место
		sg.ser.sealed = removeRef(sg.ser.sealed, sg.ref)
	}
	reason := "лимит"
	if !overLimit && lowFree {
		reason = "резерв свободного места"
	}
	log.Printf("retention: очистка raw (%s): total=%d МБ, limit=%d МБ, free=%d МБ",
		reason, total>>20, s.limitBytes>>20, free>>20)
}

// --- обрезка по свободному месту (avg) ---

// freeRetentionLoop — суточный проход очистки по свободному месту: сразу при
// старте и далее раз в сутки (чаще не требуется).
func (s *Store) freeRetentionLoop() {
	defer s.wg.Done()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	s.sweepFree()
	for {
		select {
		case <-t.C:
			s.sweepFree()
		case <-s.stop:
			return
		}
	}
}

// freeSpaceBytes — свободное место на ФС каталога данных.
func freeSpaceBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// sweepFree — если свободного места меньше порога, удаляет старейшие (по maxTime)
// целые сегменты, освобождая не менее max(minFree-free, freeEvict).
func (s *Store) sweepFree() {
	free, err := freeSpaceBytes(s.root)
	if err != nil {
		log.Printf("retention: свободное место: %v", err)
		return
	}
	if free >= s.minFreeBytes {
		return
	}
	target := s.minFreeBytes - free
	if target < s.freeEvictBytes {
		target = s.freeEvictBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	type seg struct {
		ser *series
		ref *segRef
	}
	var all []seg
	for _, ser := range s.series {
		for _, ref := range ser.sealed {
			all = append(all, seg{ser, ref})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ref.maxTime < all[j].ref.maxTime })
	var freed int64
	for _, sg := range all {
		if freed >= target {
			break
		}
		st, serr := os.Stat(sg.ref.path)
		if serr != nil {
			// Файла нет — убираем ссылку из учёта.
			sg.ser.sealed = removeRef(sg.ser.sealed, sg.ref)
			continue
		}
		if err := os.Remove(sg.ref.path); err != nil {
			// Не удалось — оставляем в учёте и не завышаем освобождённое.
			log.Printf("retention: удаление %s: %v", sg.ref.path, err)
			continue
		}
		freed += st.Size()
		sg.ser.sealed = removeRef(sg.ser.sealed, sg.ref)
	}
	log.Printf("retention: свободно было %d МБ (< %d МБ), освобождено %d МБ",
		free>>20, s.minFreeBytes>>20, freed>>20)
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
