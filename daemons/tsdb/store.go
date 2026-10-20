// store.go — append-only хранилище рядов телеметрии (колоночные блоки).
//
// Раскладка: <root>/<type>/<device_id>/<bucket>/seg-NNNNNN.ts
// Запись — единая writer-горутина: клиент кладёт строки в очередь и не
// блокируется; flush — сразу при получении (без таймера); запечатывание и fsync —
// по размеру/границе бакета. Никаких фоновых оптимизаций.
//
// Контракты полей — общий пакет lib/tscontract (общий с продюсером).
package main

import (
	"encoding/binary"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

// Row — строка ряда: ключ (type, id, time) + версия контракта + payload.
type Row struct {
	Type    string
	ID      string
	Time    int64
	Version uint16
	Payload []byte
}

var (
	errBadKey   = errors.New("пустой тип/идентификатор или содержит '/'")
	errTimeBack = errors.New("время в ряду убывает")
	errNoCodec  = errors.New("нет кодека контракта (type,version)")
	errRange    = errors.New("период запроса превышает лимит")
	errTooMany  = errors.New("слишком много строк в ответе")
)

type seriesKey struct{ typ, id string }

type segRef struct {
	seq      uint32
	path     string
	minTime  int64
	maxTime  int64
	timeBase int64
	hdrSize  int64
	sealed   bool
	entries  []idxEntry
}

type activeSeg struct {
	ref      *segRef
	f        *os.File
	size     int64
	rows     int
	lastTime int64
}

type series struct {
	key      seriesKey
	dir      string
	lastSeq  uint32
	lastTime int64
	sealed   []*segRef
	active   *activeSeg
}

// pendBlock — накапливаемый блок (одна серия, одна версия, один набор полей).
type pendBlock struct {
	key     seriesKey
	version uint16
	first   int64
	last    int64
	times   []int64
	ids     []uint16
	rows    []map[uint16]tscontract.Value // колоночный режим
	vers    []uint16                      // row-режим: версия строки
	raw     [][]byte                      // row-режим: payload как есть
}

// Store — хранилище.
type Store struct {
	root       string
	bucketSize uint32
	segMax     int64
	maxRows    int
	maxRangeMS int64
	row        bool          // raw-режим: row-раскладка сегмента
	retention  time.Duration // raw: возраст очистки (0 = выкл.)
	limitBytes int64         // raw: лимит объёма (0 = без лимита)

	minFreeBytes   int64 // порог свободного места на ФС, байт (0 = выкл.)
	freeEvictBytes int64 // минимум освобождаемого за суточный проход, байт

	reg *tscontract.Registry

	mu     sync.RWMutex
	series map[seriesKey]*series

	qmu       sync.Mutex
	qcond     *sync.Cond
	quiescing bool
	inflight  int

	wch  chan writeOp
	stop chan struct{}
	wg   sync.WaitGroup
}

type writeOp struct {
	rows []Row
	done chan struct{}
}

type Options struct {
	BucketSize uint32
	SegMax     int64
	MaxRows    int           // лимит строк в ответе (0 = без лимита)
	MaxRangeMS int64         // лимит длительности периода запроса, мс (0 = без лимита)
	Row        bool          // raw-режим (row-раскладка)
	Retention  time.Duration // raw: возраст очистки (0 = выкл.)
	LimitBytes int64         // raw: лимит объёма (0 = без лимита)

	MinFreeBytes   int64 // порог свободного места на ФС, байт (0 = выкл.)
	FreeEvictBytes int64 // минимум освобождаемого за суточный проход, байт
}

func defaultOptions() Options {
	return Options{
		BucketSize: 1024,
		SegMax:     8 * 1024 * 1024,
		MaxRows:    300_000,
		MaxRangeMS: 400 * 24 * 3600 * 1000, // ~400 суток
	}
}

func NewStore(root string, opt Options, reg *tscontract.Registry) (*Store, error) {
	if opt.BucketSize == 0 {
		opt.BucketSize = 1024
	}
	if opt.SegMax <= 0 {
		opt.SegMax = 8 * 1024 * 1024
	}
	if reg == nil {
		reg = tscontract.NewDefaultRegistry()
	}
	s := &Store{
		root:           root,
		bucketSize:     opt.BucketSize,
		segMax:         opt.SegMax,
		maxRows:        opt.MaxRows,
		maxRangeMS:     opt.MaxRangeMS,
		row:            opt.Row,
		retention:      opt.Retention,
		limitBytes:     opt.LimitBytes,
		minFreeBytes:   opt.MinFreeBytes,
		freeEvictBytes: opt.FreeEvictBytes,
		reg:            reg,
		series:         make(map[seriesKey]*series),
		wch:            make(chan writeOp, 1024),
		stop:           make(chan struct{}),
	}
	if err := s.scan(); err != nil {
		return nil, err
	}
	s.qcond = sync.NewCond(&s.qmu)
	s.wg.Add(1)
	go s.writerLoop()
	if s.retention > 0 || s.limitBytes > 0 {
		s.wg.Add(1)
		go s.retentionLoop()
	}
	if s.minFreeBytes > 0 {
		s.wg.Add(1)
		go s.freeRetentionLoop()
	}
	return s, nil
}

// --- запись (единая writer-горутина) ---

// Write кладёт батч в очередь writer'а и сразу возвращает число принятых строк —
// клиент не блокируется на диск (§7). Во время quiesce новые записи ждут
// снятия блокировки: иначе финальный committed offset не был бы финальным.
func (s *Store) Write(rows []Row) int {
	s.qmu.Lock()
	for s.quiescing {
		select {
		case <-s.stop:
			s.qmu.Unlock()
			return 0
		default:
		}
		s.qcond.Wait()
	}
	// inflight означает «запись прошла шлагбаум, но могла ещё не попасть в
	// канал». BeginQuiesce ждёт inflight==0, поэтому ни одна такая запись не
	// окажется в очереди после барьера Wait().
	s.inflight++
	s.qmu.Unlock()

	var n int
	select {
	case s.wch <- writeOp{rows: rows}:
		n = len(rows)
	case <-s.stop:
		n = 0
	}

	s.qmu.Lock()
	s.inflight--
	if s.inflight == 0 {
		s.qcond.Broadcast()
	}
	s.qmu.Unlock()
	return n
}

// BeginQuiesce переводит хранилище в quiesce: новые Write блокируются, уже
// принятые — попадают в очередь, затем очередь сливается. Возвращённая функция
// снимает блокировку (вызывается при закрытии клиентского соединения или
// остановке процесса).
func (s *Store) BeginQuiesce() func() {
	s.qmu.Lock()
	s.quiescing = true
	for s.inflight > 0 {
		s.qcond.Wait()
	}
	s.qmu.Unlock()
	s.Wait()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.qmu.Lock()
			s.quiescing = false
			s.qcond.Broadcast()
			s.qmu.Unlock()
		})
	}
}

// CommittedOffset возвращает размер активного сегмента (все байты до offset
// записаны в файл tmpfs). quiesce=true сначала дожидается обработки очереди
// writer'а; новые записи при этом не блокируются — вызывающий обязан сам
// остановить producers.
func (s *Store) CommittedOffset(typ, id, segment string, quiesce bool) (uint64, bool) {
	if quiesce {
		s.Wait()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ser := s.series[seriesKey{typ: typ, id: id}]
	if ser == nil {
		return 0, false
	}
	if ser.active != nil {
		if segment == "" || segment == filepath.Base(ser.active.ref.path) || segment == ser.active.ref.path {
			return uint64(ser.active.size), true
		}
	}
	if segment == "" && len(ser.sealed) > 0 {
		// Активный сегмент успел запечататься: отдаём последний sealed.
		ref := ser.sealed[len(ser.sealed)-1]
		st, err := os.Stat(ref.path)
		if err != nil {
			return 0, false
		}
		return uint64(st.Size()), true
	}
	for _, ref := range ser.sealed {
		if segment == filepath.Base(ref.path) || segment == ref.path {
			st, err := os.Stat(ref.path)
			if err != nil {
				return 0, false
			}
			return uint64(st.Size()), true
		}
	}
	return 0, false
}

// Wait — барьер: дождаться обработки всего, что уже в очереди (для тестов/чтения).
func (s *Store) Wait() {
	done := make(chan struct{})
	select {
	case s.wch <- writeOp{done: done}:
		<-done
	case <-s.stop:
	}
}

func (s *Store) writerLoop() {
	defer s.wg.Done()
	var pend *pendBlock
	flush := func() {
		if pend == nil {
			return
		}
		s.mu.Lock()
		err := s.flushLocked(pend)
		s.mu.Unlock()
		if err != nil {
			log.Printf("writer: %v", err)
		}
		pend = nil
	}
	for {
		select {
		case op := <-s.wch:
			if op.done != nil { // барьер
				flush()
				close(op.done)
				continue
			}
			for _, r := range op.rows {
				if !validKey(r.Type) || !validKey(r.ID) {
					log.Printf("writer: %v (%q/%q)", errBadKey, r.Type, r.ID)
					continue
				}
				key := seriesKey{typ: r.Type, id: r.ID}
				// Неубывание времени в серии (в т.ч. между запросами/блоками).
				s.mu.RLock()
				var floor int64
				if ser := s.series[key]; ser != nil {
					floor = ser.lastTime
				}
				s.mu.RUnlock()
				if pend != nil && pend.key == key && len(pend.times) > 0 {
					floor = pend.last
				}
				if r.Time < floor {
					log.Printf("writer: %v (%s/%s %d)", errTimeBack, r.Type, r.ID, r.Time)
					continue
				}
				if s.row { // raw: row-запись сразу, payload как есть
					if _, ok := s.reg.Get(r.Type, r.Version); !ok {
						log.Printf("writer: %v (%s/%s v%d)", errNoCodec, r.Type, r.ID, r.Version)
						continue
					}
					if pend != nil && pend.key != key {
						flush()
					}
					if pend == nil {
						pend = &pendBlock{key: key, first: r.Time}
					}
					pend.last = r.Time
					pend.times = append(pend.times, r.Time)
					pend.vers = append(pend.vers, r.Version)
					pend.raw = append(pend.raw, r.Payload)
					continue
				}
				vals, decErr, ok := s.decodeRow(r)
				if !ok {
					log.Printf("writer: %v (%s/%s v%d): %v", errNoCodec, r.Type, r.ID, r.Version, decErr)
					continue
				}
				if pend != nil && (pend.key != key || pend.version != r.Version || !sameIDs(pend.ids, idsOf(vals))) {
					flush()
				}
				if pend == nil {
					pend = &pendBlock{key: key, version: r.Version, first: r.Time, ids: idsOf(vals)}
				}
				pend.last = r.Time
				pend.times = append(pend.times, r.Time)
				pend.rows = append(pend.rows, vals)
			}
			flush() // flush сразу по завершении запроса (§7)
		case <-s.stop:
			flush()
			return
		}
	}
}

// flushLocked — диспетчер раскладки: row (raw) или колоночный (avg).
func (s *Store) flushLocked(pb *pendBlock) error {
	if s.row {
		return s.writeRowsLocked(pb)
	}
	return s.flushBlockLocked(pb)
}

// decodeRow разбирает payload строки в map[field_id]Value по реестру.
func (s *Store) decodeRow(r Row) (map[uint16]tscontract.Value, error, bool) {
	c, ok := s.reg.Get(r.Type, r.Version)
	if !ok {
		return nil, errNoCodec, false
	}
	vals, err := c.Decode(r.Payload)
	if err != nil {
		return nil, err, false
	}
	m := make(map[uint16]tscontract.Value, len(vals))
	for _, v := range vals {
		m[v.ID] = v
	}
	return m, nil, true
}

func idsOf(m map[uint16]tscontract.Value) []uint16 {
	ids := make([]uint16, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sameIDs(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// flushBlockLocked собирает колонки и пишет блок (flush сразу).
func (s *Store) flushBlockLocked(pb *pendBlock) error {
	ser := s.getSeries(pb.key)
	if ser.active == nil {
		if err := s.newActiveLocked(ser, pb.first); err != nil {
			return err
		}
	}
	c, ok := s.reg.Get(pb.key.typ, pb.version)
	if !ok {
		return errNoCodec
	}
	fieldType := map[uint16]tscontract.FieldType{}
	for _, f := range c.Fields() {
		fieldType[f.ID] = f.Type
	}
	recCount := len(pb.rows)
	cols := []column{{FieldID: 0, Data: encodeTimeColumn(pb)}}
	for _, id := range pb.ids {
		t := fieldType[id]
		col := make([]byte, 0, recCount*4)
		for _, row := range pb.rows {
			col = tscontract.AppendValue(col, t, row[id])
		}
		cols = append(cols, column{FieldID: id, Data: col})
	}
	blk := encodeBlock(pb.first, pb.last, pb.version, recCount, cols)
	as := ser.active
	if _, err := as.f.WriteAt(blk, as.size); err != nil {
		return err
	}
	as.ref.entries = append(as.ref.entries, idxEntry{FirstTime: pb.first, Offset: uint64(as.size), Blen: uint32(len(blk))})
	as.ref.maxTime = pb.last
	as.size += int64(len(blk))
	ser.lastTime = pb.last
	if as.size >= s.segMax {
		return s.sealLocked(ser, as)
	}
	return nil
}

func encodeTimeColumn(pb *pendBlock) []byte {
	b := make([]byte, 0, len(pb.times)*2)
	base := uint64(pb.first)
	var tmp [binary.MaxVarintLen64]byte
	for _, t := range pb.times {
		n := binary.PutUvarint(tmp[:], uint64(t)-base)
		b = append(b, tmp[:n]...)
		base = uint64(t)
	}
	return b
}

// --- сегменты/каталог ---

func (s *Store) bucketOf(seq uint32) uint32 { return seq / s.bucketSize }

func (s *Store) segDir(key seriesKey, seq uint32) string {
	return filepath.Join(s.root, key.typ, key.id, strconv.FormatUint(uint64(s.bucketOf(seq)), 10))
}

func (s *Store) segPath(key seriesKey, seq uint32) string {
	return filepath.Join(s.segDir(key, seq), "seg-"+pad6(seq)+".ts")
}

func pad6(v uint32) string {
	s := strconv.FormatUint(uint64(v), 10)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}

func validKey(s string) bool {
	return s != "" && s != "." && s != ".." && len(s) <= 50 &&
		!strings.ContainsRune(s, '/') && !strings.ContainsRune(s, 0)
}

func (s *Store) getSeries(key seriesKey) *series {
	ser := s.series[key]
	if ser == nil {
		ser = &series{key: key, dir: filepath.Join(s.root, key.typ, key.id)}
		s.series[key] = ser
	}
	return ser
}

func (s *Store) newActiveLocked(ser *series, timeBase int64) error {
	seq := ser.lastSeq + 1
	if err := os.MkdirAll(s.segDir(ser.key, seq), 0o755); err != nil {
		return err
	}
	path := s.segPath(ser.key, seq)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	hdr := encodeSegHeader(segHeader{FormatVer: formatVer, DeviceType: ser.key.typ, DeviceID: ser.key.id, TimeBase: timeBase})
	if _, err := f.Write(hdr); err != nil {
		f.Close()
		return err
	}
	ref := &segRef{seq: seq, path: path, minTime: timeBase, timeBase: timeBase, hdrSize: int64(len(hdr))}
	ser.lastSeq = seq
	ser.active = &activeSeg{ref: ref, f: f, size: int64(len(hdr)), lastTime: timeBase}
	return nil
}

func (s *Store) sealLocked(ser *series, as *activeSeg) error {
	foot := encodeIndexFooter(as.ref.entries)
	if _, err := as.f.WriteAt(foot, as.size); err != nil {
		return err
	}
	as.size += int64(len(foot))
	if err := as.f.Sync(); err != nil {
		return err
	}
	as.f.Close()
	as.ref.sealed = true
	ser.sealed = append(ser.sealed, as.ref)
	ser.active = nil
	return nil
}

func (s *Store) scan() error {
	if _, err := os.Stat(s.root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "seg-") || !strings.HasSuffix(name, ".ts") {
			return nil
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 4 {
			return nil
		}
		seq, err := parseSeq(name)
		if err != nil {
			return nil
		}
		key := seriesKey{typ: parts[0], id: parts[1]}
		_ = s.loadSegment(key, seq, path)
		return nil
	})
}

func parseSeq(name string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, "seg-"), ".ts"), 10, 32)
	return uint32(v), err
}

func (s *Store) loadSegment(key seriesKey, seq uint32, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	// заголовок: читаем первые байты целиком (мал).
	hdr := make([]byte, 64*1024)
	n, _ := f.ReadAt(hdr, 0)
	if n <= 0 {
		return errShort
	}
	sh, hsz, err := decodeSegHeader(hdr[:n])
	if err != nil {
		return err
	}
	// Заголовок дублирует ключ из пути — сверяем (проверка целостности).
	if sh.DeviceType != key.typ || sh.DeviceID != key.id {
		log.Printf("сегмент %s: ключ заголовка (%q/%q) ≠ пути (%q/%q) — пропуск",
			path, sh.DeviceType, sh.DeviceID, key.typ, key.id)
		return nil
	}
	hsz64 := int64(hsz)
	ref := &segRef{seq: seq, path: path, timeBase: sh.TimeBase, hdrSize: hsz64}
	if s.row {
		f.Close()
		return s.loadRowSegment(key, seq, path, size, ref)
	}
	if size > hsz64 {
		if sealed, ents := readFooterFromFile(f, size, hsz64); sealed && len(ents) > 0 {
			ref.sealed = true
			ref.entries = ents
			ref.minTime = ents[0].FirstTime
			ref.maxTime = lastBlockLastTime(f, ents)
			s.putSeg(key, ref)
			s.touchSeq(key, seq)
			return nil
		}
	}
	// Нет/битый index_footer — пересобираем индекс сканом с проверкой CRC тела
	// каждого блока; неполный (недописанный) блок отбрасывается (§1.8, §6).
	ents, last, ok := scanActiveBlocks(f, size, hsz64)
	if !ok {
		return errCRC
	}
	good := hsz64
	if len(ents) > 0 {
		ref.entries = ents
		ref.minTime = ents[0].FirstTime
		ref.maxTime = last
		good = int64(ents[len(ents)-1].Offset) + int64(blockTotal(f, ents[len(ents)-1]))
	}
	// Обрезаем недописанный «хвост» (частичный блок/строку) — §1.8.
	if good < size {
		f.Close()
		if err := os.Truncate(path, good); err != nil {
			return err
		}
		size = good
	}
	w, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if _, err := w.Seek(size, 0); err != nil {
		w.Close()
		return err
	}
	ser := s.getSeries(key)
	// Последний (незапечатанный) сегмент держим как active; в sealed его не
	// добавляем, иначе readSeries прочитал бы его дважды.
	ser.active = &activeSeg{ref: ref, f: w, size: size}
	if ref.maxTime > ser.lastTime {
		ser.lastTime = ref.maxTime
	}
	s.touchSeq(key, seq)
	return nil
}

func (s *Store) touchSeq(key seriesKey, seq uint32) {
	ser := s.getSeries(key)
	if seq > ser.lastSeq {
		ser.lastSeq = seq
	}
}

func (s *Store) putSeg(key seriesKey, ref *segRef) {
	ser := s.getSeries(key)
	ser.sealed = append(ser.sealed, ref)
	if ref.maxTime > ser.lastTime {
		ser.lastTime = ref.maxTime
	}
}

func blockTotal(f *os.File, e idxEntry) uint32 {
	var hb [blkHdrSize]byte
	if _, err := f.ReadAt(hb[:], int64(e.Offset)); err != nil {
		return 0
	}
	m, err := decodeBlockHeader(hb[:])
	if err != nil {
		return 0
	}
	return uint32(blkHdrSize+m.ColCount*colEntSize) + m.Blen
}

func lastBlockLastTime(f *os.File, ents []idxEntry) int64 {
	if len(ents) == 0 {
		return 0
	}
	var hb [blkHdrSize]byte
	if _, err := f.ReadAt(hb[:], int64(ents[len(ents)-1].Offset)); err != nil {
		return 0
	}
	m, err := decodeBlockHeader(hb[:])
	if err != nil {
		return 0
	}
	return m.LastTime
}

func readFooterFromFile(f *os.File, size, hsz int64) (bool, []idxEntry) {
	if size < hsz+idxFootMin {
		return false, nil
	}
	var last [idxFootMin]byte
	if _, err := f.ReadAt(last[:], size-idxFootMin); err != nil {
		return false, nil
	}
	count := uint32(last[0]) | uint32(last[1])<<8 | uint32(last[2])<<16 | uint32(last[3])<<24
	if uint32(last[4])|uint32(last[5])<<8|uint32(last[6])<<16|uint32(last[7])<<24 != idxMagic {
		return false, nil
	}
	footLen := int64(count)*idxEntSize + idxFootMin
	if footLen > size-hsz {
		return false, nil
	}
	buf := make([]byte, footLen)
	if _, err := f.ReadAt(buf, size-footLen); err != nil {
		return false, nil
	}
	ents, _, ok := decodeIndexFooter(buf)
	if !ok {
		return false, nil
	}
	return true, ents
}

// scanActiveBlocks пересобирает индекс активного сегмента: читает блоки по
// цепочке blen и проверяет CRC тела каждого. Недописанный/битый блок (в т.ч.
// обрыв строки внутри блока) не принимается — скан обрывается на нём (§1.8).
func scanActiveBlocks(f *os.File, size, hsz int64) ([]idxEntry, int64, bool) {
	var ents []idxEntry
	var last int64
	off := hsz
	for off+blkHdrSize <= size {
		meta, _, _, err := readBlockRaw(f, uint64(off))
		if err != nil {
			break
		}
		total := int64(blkHdrSize+meta.ColCount*colEntSize) + int64(meta.Blen)
		if off+total > size {
			break
		}
		ents = append(ents, idxEntry{FirstTime: meta.FirstTime, Offset: uint64(off), Blen: meta.Blen})
		last = meta.LastTime
		off += total
	}
	return ents, last, true
}

// --- чтение ---

// Направление выборки (§9).
const (
	dirFrom uint8 = 0 // вперёд от from
	dirTo   uint8 = 1 // назад от to
)

type Query struct {
	Type      string
	IDs       []string
	From      int64
	To        int64
	Metrics   []uint16 // пусто — ряд целиком
	Direction uint8    // 0 = из начала периода, 1 = из конца
	Limit     int      // 0 = без лимита
	Bucket    int64    // мини-окно decimation, мс (0 = без прореживания) — §9
}

// decimateRows — прореживание «ближайшая строка к границе мини-окна» (§9).
// От включённой точки t_last строится граница G = t_last ± bucket. Строки внутри
// окна (|t − t_last| ≤ bucket) пропускаются, удерживая prev — последнюю пропущенную.
// Первую строку next за границей сравниваем с prev: включаем ту, что ближе к G.
// Решение только по времени, payload не трогается; по строкам (общие времена).
// direction=to — проход от конца (в порядке убывания времени), затем разворот.
func decimateRows(rows []Row, bucketMS int64, dir uint8) []Row {
	if bucketMS <= 0 || len(rows) < 2 {
		return rows
	}
	// step: +1 — проход по возрастанию (dirFrom), −1 — по убыванию (dirTo).
	// Обе ветви считаются одной формулой: направление учитывается знаком step.
	step := int64(1)
	if dir == dirTo {
		step = -1
	}
	n := len(rows)
	out := make([]Row, 0, n/2+2)
	var lastID string
	var lastT int64
	var haveLast, havePrev bool
	var prevRow Row
	for k := 0; k < n; k++ {
		i := k
		if step < 0 {
			i = n - 1 - k
		}
		r := rows[i]
		if !haveLast || r.ID != lastID {
			out = append(out, r)
			lastID, lastT, haveLast, havePrev = r.ID, r.Time, true, false
			continue
		}
		for {
			G := lastT + step*bucketMS // граница окна (G впереди по направлению)
			if (r.Time-lastT)*step <= bucketMS {
				// внутри окна — держим ближайшего кандидата prev
				prevRow, havePrev = r, true
				break
			}
			if havePrev {
				dp := (G - prevRow.Time) * step // расстояние prev→G (>0)
				dn := (r.Time - G) * step       // расстояние G→next (>0)
				// dp == dn → включаем next (за границей): строгое dp < dn.
				if dp < dn {
					out = append(out, prevRow)
					lastT, havePrev = prevRow.Time, false
					continue // переоценить r относительно новой границы
				}
			}
			out = append(out, r)
			lastT, havePrev = r.Time, false
			break
		}
	}
	if step < 0 {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func (s *Store) Query(q Query) ([]Row, error) {
	s.Wait() // гарантировать обработку очереди (writer — единый)
	// Лимит периода — только для запросов без лимита строк; постраничные (limit>0)
	// узкие по числу строк и период не ограничиваем (§9).
	if s.maxRangeMS > 0 && q.Limit == 0 && q.To-q.From > s.maxRangeMS {
		return nil, errRange
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []seriesKey
	want := map[string]bool{}
	for _, id := range q.IDs {
		want[id] = true
	}
	for k := range s.series {
		if k.typ != q.Type {
			continue
		}
		if len(want) > 0 && !want[k.id] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].id < keys[j].id })
	var out []Row
	for _, k := range keys {
		out = append(out, s.readSeries(s.series[k], q)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Time < out[j].Time
	})
	// Прореживание (§9): сначала decimation, затем limit по отобранным строкам.
	if q.Bucket > 0 {
		out = decimateRows(out, q.Bucket, q.Direction)
	}
	// Лимит строк в ответе — по уже прореженному набору (§1 ревью 2026-10-15):
	// иначе плотное окно падало бы по maxRows до того, как bucket сократит объём.
	if s.maxRows > 0 && len(out) > s.maxRows {
		return nil, errTooMany
	}
	// Лимит «страницы»: direction=from — первые N, direction=to — последние N (§9).
	if q.Limit > 0 && len(out) > q.Limit {
		if q.Direction == dirTo {
			out = out[len(out)-q.Limit:]
		} else {
			out = out[:q.Limit]
		}
	}
	return out, nil
}

func (s *Store) readSeries(ser *series, q Query) []Row {
	var out []Row
	appendSeg := func(ref *segRef) {
		if ref.maxTime < q.From || ref.minTime > q.To {
			return
		}
		out = append(out, s.readSegRef(ref, ser.key.id, q)...)
	}
	// direction=to — идём с конца (активный, затем запечатанные в обратном порядке),
	// останавливаясь, когда набрали достаточно строк для страницы. Итоговый порядок
	// выравнивает Query (сортировка по (ID, time)).
	if q.Direction == dirTo {
		if as := ser.active; as != nil {
			appendSeg(as.ref)
		}
		for i := len(ser.sealed) - 1; i >= 0; i-- {
			appendSeg(ser.sealed[i])
			if q.Limit > 0 && len(out) >= q.Limit {
				break
			}
		}
		return out
	}
	for _, ref := range ser.sealed {
		appendSeg(ref)
	}
	if as := ser.active; as != nil {
		appendSeg(as.ref)
	}
	return out
}

// readSegRef — чтение сегмента по раскладке (row/col).
func (s *Store) readSegRef(ref *segRef, id string, q Query) []Row {
	if s.row {
		return s.readSegmentRow(ref, id, q)
	}
	return s.readSegment(ref, id, q)
}

func (s *Store) readSegment(ref *segRef, id string, q Query) []Row {
	f, err := os.Open(ref.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	ents := ref.entries
	blockLast := func(i int) int64 { // «время конца» блока i
		if i+1 < len(ents) {
			return ents[i+1].FirstTime
		}
		return ref.maxTime
	}
	var out []Row
	if q.Direction == dirTo {
		// Последний блок с FirstTime <= q.To; читаем вниз, стоп на limit (§9).
		start := sort.Search(len(ents), func(i int) bool { return ents[i].FirstTime > q.To }) - 1
		for i := start; i >= 0; i-- {
			if blockLast(i) < q.From {
				break
			}
			out = append(s.readBlock(f, ents[i], id, q), out...)
			if q.Limit > 0 && len(out) >= q.Limit {
				break
			}
		}
		return out
	}
	// direction=from: первый блок, чей интервал достигает q.From, — вперёд, стоп на limit.
	start := sort.Search(len(ents), func(i int) bool { return blockLast(i) >= q.From })
	for i := start; i < len(ents); i++ {
		e := ents[i]
		if e.FirstTime > q.To {
			break
		}
		out = append(out, s.readBlock(f, e, id, q)...)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out
}

func (s *Store) readBlock(f *os.File, e idxEntry, id string, q Query) []Row {
	meta, ents, body, err := readBlockRaw(f, e.Offset)
	if err != nil {
		return nil
	}
	c, ok := s.reg.Get(q.Type, meta.Version)
	if !ok {
		return nil
	}
	fieldType := map[uint16]tscontract.FieldType{}
	for _, fld := range c.Fields() {
		fieldType[fld.ID] = fld.Type
	}
	// колонка времени
	var timeData []byte
	present := map[uint16][]byte{}
	for _, ce := range ents {
		data := body[ce.Offset : ce.Offset+ce.Len]
		if ce.FieldID == 0 {
			timeData = data
		} else {
			present[ce.FieldID] = data
		}
	}
	times, err := timeColumn(timeData, meta.FirstTime, meta.RecCount)
	if err != nil {
		return nil
	}
	// какие поля отдавать
	var ids []uint16
	if len(q.Metrics) == 0 {
		for id := range present {
			ids = append(ids, id)
		}
	} else {
		for _, id := range q.Metrics {
			if _, ok := present[id]; ok {
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	// декодируем значения колонок
	cols := map[uint16][]tscontract.Value{}
	for _, id := range ids {
		t := fieldType[id]
		vals, err := decodeColumn(present[id], t, meta.RecCount)
		if err != nil {
			return nil
		}
		cols[id] = vals
	}
	var out []Row
	for i := 0; i < meta.RecCount; i++ {
		if times[i] < q.From || times[i] > q.To {
			continue
		}
		vals := make([]tscontract.Value, 0, len(ids))
		for _, id := range ids {
			v := cols[id][i]
			v.ID = id
			vals = append(vals, v)
		}
		payload, err := c.Encode(vals)
		if err != nil {
			continue
		}
		out = append(out, Row{Type: q.Type, ID: id, Time: times[i], Version: meta.Version, Payload: payload})
	}
	return out
}

func decodeColumn(data []byte, t tscontract.FieldType, recCount int) ([]tscontract.Value, error) {
	out := make([]tscontract.Value, recCount)
	pos := 0
	for i := 0; i < recCount; i++ {
		v, rest, err := tscontract.DecodeValue(data[pos:], t)
		if err != nil {
			return nil, err
		}
		out[i] = v
		pos = len(data) - len(rest)
	}
	return out, nil
}

// --- список устройств и крайние точки (из памяти) ---

// devMeta — устройство (тип + идентификатор).
type devMeta struct {
	Type string
	ID   string
}

// seriesBounds — [min,max] времени серии по её сегментам; ok=false если данных нет.
func seriesBounds(ser *series) (min, max int64, ok bool) {
	first := true
	add := func(a, b int64) {
		if first {
			min, max, first = a, b, false
			return
		}
		if a < min {
			min = a
		}
		if b > max {
			max = b
		}
	}
	for _, ref := range ser.sealed {
		add(ref.minTime, ref.maxTime)
	}
	if as := ser.active; as != nil {
		add(as.ref.minTime, as.ref.maxTime)
	}
	return min, max, !first
}

// ListDevices — устройства с данными (серии, у которых есть границы); пустые
// (все сегменты вычищены) пропускаются. Из памяти, без чтения сегментов.
func (s *Store) ListDevices() []devMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]devMeta, 0, len(s.series))
	for k, ser := range s.series {
		if _, _, ok := seriesBounds(ser); ok {
			out = append(out, devMeta{Type: k.typ, ID: k.id})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Extremes — [min,max] времени устройства из памяти; found=false если данных нет.
func (s *Store) Extremes(typ, id string) (int64, int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ser := s.series[seriesKey{typ: typ, id: id}]
	if ser == nil {
		return 0, 0, false
	}
	return seriesBounds(ser)
}

// --- flush/sync/close ---

// Flush — форсировать обработку очереди (writer сбрасывает блоки сразу).
func (s *Store) Flush() { s.Wait() }

// Sync — дождаться обработки и fsync активных сегментов (при завершении).
func (s *Store) Sync() error {
	s.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ser := range s.series {
		if ser.active != nil {
			if err := ser.active.f.Sync(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error {
	close(s.stop)
	s.qmu.Lock()
	s.quiescing = false
	s.qcond.Broadcast()
	s.qmu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ser := range s.series {
		if ser.active != nil {
			ser.active.f.Sync()
			ser.active.f.Close()
			ser.active = nil
		}
	}
	return nil
}
