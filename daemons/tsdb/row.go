// row.go — raw-режим хранилища: сегмент = hdr + поток row-записей + разреженный
// index_footer (§14.4). Пишется сразу (видимость в следующую секунду), без
// колоночных блоков. Используется raw-инстансом (tmpfs).
//
//	segment := hdr row* [index_footer]
//	row     := varint Δt | version u16 | varint payload_len | payload
//	          (Δt — от предыдущей строки; для первой — от hdr.time_base)
//	index_footer — как §5 (idxEntry: first_time|offset|blen), разреженный: каждые rowK строк
package main

import (
	"encoding/binary"
	"os"
	"sort"
)

const rowK = 256 // шаг разреженного индекса (строк)

// encodeRow собирает row-запись.
func encodeRow(dt uint64, version uint16, payload []byte) []byte {
	b := make([]byte, 0, binary.MaxVarintLen64+2+binary.MaxVarintLen64+len(payload))
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], dt)
	b = append(b, tmp[:n]...)
	b = binary.LittleEndian.AppendUint16(b, version)
	n = binary.PutUvarint(tmp[:], uint64(len(payload)))
	b = append(b, tmp[:n]...)
	return append(b, payload...)
}

// decodeRowRow разбирает row-запись из начала b; возвращает Δt, версию, payload и
// длину записи.
func decodeRowRow(b []byte) (dt uint64, version uint16, payload []byte, n int, err error) {
	dt, m := binary.Uvarint(b)
	if m <= 0 {
		return 0, 0, nil, 0, errShort
	}
	pos := m
	if pos+2 > len(b) {
		return 0, 0, nil, 0, errShort
	}
	version = binary.LittleEndian.Uint16(b[pos : pos+2])
	pos += 2
	pl, m := binary.Uvarint(b[pos:])
	if m <= 0 {
		return 0, 0, nil, 0, errShort
	}
	pos += m
	if pos+int(pl) > len(b) {
		return 0, 0, nil, 0, errShort
	}
	return dt, version, b[pos : pos+int(pl)], pos + int(pl), nil
}

// scanRows проходит поток row-записей начиная с off, строя разреженный индекс
// (каждые rowK строк). Возвращает записи, first/last время и смещение за последней
// целой строкой (недописанная строка отбрасывается).
func scanRows(buf []byte, off int64, timeBase int64) (ents []idxEntry, first, last, good int64, n int) {
	ents = make([]idxEntry, 0, 16)
	pos := int(off)
	base := uint64(timeBase)
	first, last = timeBase, timeBase
	for pos < len(buf) {
		start := pos
		dt, _, _, rn, err := decodeRowRow(buf[pos:])
		if err != nil {
			break
		}
		base += dt
		if n == 0 {
			first = int64(base)
		}
		if n%rowK == 0 {
			ents = append(ents, idxEntry{FirstTime: int64(base), Offset: uint64(start)})
		}
		last = int64(base)
		pos += rn
		n++
	}
	return ents, first, last, int64(pos), n
}

// rowRowsEnd — смещение конца row-записей: начало index_footer, если он есть
// (trailer: count u32 | magic MTSX | crc, см. §5/§14.4). Иначе — конец файла.
// Нужно, чтобы сканер/читатель НЕ разбирали футер как row-записи (иначе
// «последнее время» серии улетает в будущее — см. фикс 2026-10-15).
func rowRowsEnd(data []byte) int {
	n := len(data)
	if n < idxFootMin {
		return n
	}
	last := data[n-idxFootMin:]
	if binary.LittleEndian.Uint32(last[4:8]) != idxMagic {
		return n
	}
	footLen := int(binary.LittleEndian.Uint32(last[0:4]))*idxEntSize + idxFootMin
	if footLen > n {
		return n
	}
	return n - footLen
}

// loadRowSegment загружает raw-сегмент: сканирует строки и восстанавливает
// разреженный индекс; при наличии хвоста (index_footer) — sealed, иначе active
// (недописанный «хвост» обрезается).
func (s *Store) loadRowSegment(key seriesKey, seq uint32, path string, size int64, ref *segRef) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	end := rowRowsEnd(data)
	ents, first, last, good, n := scanRows(data[:end], ref.hdrSize, ref.timeBase)
	ref.entries = ents
	ref.minTime = first
	ref.maxTime = last
	ser := s.getSeries(key)
	if good < size { // хвост (index_footer) → запечатан
		ref.sealed = true
		s.putSeg(key, ref)
		s.touchSeq(key, seq)
		return nil
	}
	w, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if _, err := w.Seek(good, 0); err != nil {
		w.Close()
		return err
	}
	ser.active = &activeSeg{ref: ref, f: w, size: good, rows: n, lastTime: last}
	if ref.maxTime > ser.lastTime {
		ser.lastTime = ref.maxTime
	}
	s.touchSeq(key, seq)
	return nil
}

// writeRowsLocked дописывает снапшоты raw-записями в активный сегмент.
func (s *Store) writeRowsLocked(pb *pendBlock) error {
	ser := s.getSeries(pb.key)
	if ser.active == nil {
		if err := s.newActiveLocked(ser, pb.first); err != nil {
			return err
		}
	}
	as := ser.active
	prev := as.lastTime
	for i, t := range pb.times {
		dt := uint64(t - prev)
		rec := encodeRow(dt, pb.vers[i], pb.raw[i])
		if _, err := as.f.WriteAt(rec, as.size); err != nil {
			return err
		}
		off := as.size
		as.size += int64(len(rec))
		if as.rows%rowK == 0 {
			as.ref.entries = append(as.ref.entries, idxEntry{FirstTime: t, Offset: uint64(off)})
		}
		as.rows++
		as.lastTime = t
		prev = t
	}
	as.ref.maxTime = pb.last
	ser.lastTime = pb.last
	if as.size >= s.segMax {
		return s.sealLocked(ser, as)
	}
	return nil
}

// readSegmentRow читает диапазон из raw-сегмента (row-раскладка). Стартовая точка
// берётся по разреженному индексу (ref.entries), далее скан вперёд с ранним
// останоном по limit (§9): forwad — первые limit строк от from; to — последние
// limit строк до to.
func (s *Store) readSegmentRow(ref *segRef, id string, q Query) []Row {
	data, err := os.ReadFile(ref.path)
	if err != nil {
		return nil
	}
	if _, _, err := decodeSegHeader(data); err != nil {
		return nil
	}
	end := rowRowsEnd(data) // строки только до index_footer
	limit := q.Limit
	out := make([]Row, 0, 16)
	pos := int(ref.hdrSize)
	base := uint64(ref.timeBase)
	if n := len(ref.entries); n > 0 {
		target := q.From
		if q.Direction == dirTo {
			target = q.To
		}
		idx := sort.Search(n, func(i int) bool { return ref.entries[i].FirstTime > target }) - 1
		if q.Direction == dirTo && limit > 0 {
			// «назад»: последние limit строк могут лежать до найденной entry —
			// отступаем на гарантированно покрывающее число записей.
			idx -= limit/rowK + 1
		}
		if idx >= 0 {
			e := ref.entries[idx]
			pos = int(e.Offset)
			if dt, _, _, _, derr := decodeRowRow(data[pos:]); derr == nil {
				base = uint64(e.FirstTime) - dt // восстановить базу строки entry
			}
		}
	}
	for pos < end {
		dt, ver, payload, rn, e := decodeRowRow(data[pos:])
		if e != nil || pos+rn > end {
			break
		}
		base += dt
		t := int64(base)
		pos += rn
		if t < q.From {
			continue
		}
		if t > q.To {
			break
		}
		pl := payload
		if len(q.Metrics) > 0 {
			proj, ok := s.reg.Project(q.Type, ver, payload, q.Metrics)
			if !ok {
				continue
			}
			pl = proj
		}
		row := Row{Type: q.Type, ID: id, Time: t, Version: ver, Payload: pl}
		if q.Direction == dirTo {
			if limit > 0 && len(out) >= limit {
				out = out[1:] // держим только последние limit
			}
			out = append(out, row)
		} else {
			out = append(out, row)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}
