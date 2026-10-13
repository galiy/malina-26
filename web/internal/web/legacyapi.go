package web

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// legacyService — фоновый расчёт снимка read_json.php?device=map.
// Состояние только в памяти; выбор МАП — по числовому серийнику (буква не учитывается).
// Запись legacy-API идёт через этот же модуль: команда ставится в очередь и
// исполняется в основном цикле (run), затем снимок и память пересобираются сразу.
type legacyService struct {
	mu       sync.RWMutex
	snap     []byte
	mem      []byte // образ памяти для read_memory (0x000..0x7FF)
	snapTime int64
	acc      legacyAcc
	cmds     chan *legacyWriteJob
	sock     string
	stop     chan struct{}
	stopOnce sync.Once

	// доступность map-модуля для check_daemons
	active    bool // МАП найден в текущем цикле
	haveState bool // был хотя бы один реальный снимок

	selected  bool
	selSerial uint16
	lostSince int64
	lastTick  int64 // троттл ленивого обновления снимка (unix sec)
}

// legacyWriteJob — команда записи из legacy-API в основной цикл модуля.
type legacyWriteJob struct {
	ops  []map[string]any
	resp chan bool
}

func newLegacyService(sock string) *legacyService {
	ls := &legacyService{cmds: make(chan *legacyWriteJob, 8), sock: sock, stop: make(chan struct{})}
	zero := &legacyFields{
		val:  make([]uint32, len(mapFieldNames)),
		pres: make([]bool, len(mapFieldNames)),
	}
	ls.snap = renderLegacy(zero, &ls.acc)
	ls.mem = make([]byte, legacyMemSize)
	return ls
}

// run обслуживает ТОЛЬКО команды записи; снимок обновляется ЛЕНИВО по запросу
// API (refresh) — фоновый тик 1/с убран, нагрузка возникает лишь при обращениях.
func (ls *legacyService) run() {
	for {
		select {
		case <-ls.stop:
			return
		case job := <-ls.cmds:
			ls.doWrite(job) // запись + немедленное перечитывание
		}
	}
}

// ensureFresh обновляет снимок не чаще 1 раза в секунду (лениво, по запросу API);
// результат эквивалентен прежнему фоновому тику раз в секунду.
func (ls *legacyService) ensureFresh() {
	now := time.Now().Unix()
	ls.mu.Lock()
	if now == ls.lastTick {
		ls.mu.Unlock()
		return
	}
	ls.lastTick = now
	ls.mu.Unlock()
	ls.tick()
}

// stopLoop останавливает основной цикл (для тестов/останова).
func (ls *legacyService) stopLoop() {
	ls.stopOnce.Do(func() { close(ls.stop) })
}

// doWrite исполняет команду записи через map-reader и сразу перечитывает МАП,
// обновляя снимок read_json и карту памяти read_memory. ops уже классифицированы
// по номеру регистра (addr==0 — команда, иначе — запись байта); команды и
// регистры разбиваются на однородные запросы (контракт map-reader это требует).
func (ls *legacyService) doWrite(job *legacyWriteJob) {
	id, gen, ok := ls.targetID()
	if !ok {
		job.resp <- false // не закреплён за МАП — не пишем
		return
	}
	all := true
	for _, reqOps := range groupOps(job.ops) {
		body, _ := json.Marshal(map[string]any{"cmd": "write", "id": id, "gen": gen, "ops": reqOps})
		resp, err := controlCall(ls.sock, body)
		if err != nil {
			all = false
			break
		}
		var mr mapWriteResp
		_ = json.Unmarshal(resp, &mr)
		if !mr.OK {
			all = false
			break
		}
	}
	ls.refresh(id) // перечитать и пересобрать снимок/память немедленно
	job.resp <- all
}

// maxWriteOps — лимит операций на одну заявку в map-reader (синхронно с
// daemons/map-reader/write.go:maxOps).
const maxWriteOps = 64

// groupOps разбивает ops на однородные запросы: команды — по одной, идущие
// подряд регистровые записи — одним запросом (не длиннее maxWriteOps).
func groupOps(ops []map[string]any) [][]map[string]any {
	var reqs [][]map[string]any
	var regs []map[string]any
	flush := func() {
		for len(regs) > 0 {
			n := len(regs)
			if n > maxWriteOps {
				n = maxWriteOps
			}
			reqs = append(reqs, regs[:n])
			regs = regs[n:]
		}
	}
	for _, op := range ops {
		if op["op"] == "cmd" {
			flush()
			reqs = append(reqs, []map[string]any{op})
		} else {
			regs = append(regs, op)
		}
	}
	flush()
	return reqs
}

// targetID — МАП, за которым закреплён legacy-модуль (по числовому серийнику;
// буква серийника не учитывается). Если привязки нет — false (писать некуда).
func (ls *legacyService) targetID() (int, uint64, bool) {
	ls.mu.RLock()
	sel, ser := ls.selected, ls.selSerial
	ls.mu.RUnlock()
	if !sel {
		return 0, 0, false
	}
	for _, m := range loadMapList().Maps {
		if m.Serial == ser {
			return m.ID, m.Gen, true
		}
	}
	return 0, 0, false
}

// refresh немедленно перечитывает выбранный МАП и пересобирает снимок/память.
func (ls *legacyService) refresh(id int) {
	payload := mapRawPayload(id)
	if payload == nil {
		return
	}
	f, ok := decodeLegacy(payload)
	if !ok {
		return
	}
	ls.mu.Lock()
	ls.snap = renderLegacy(f, &ls.acc)
	ls.mem = buildMem(f)
	ls.snapTime = f.meta.LastSeen
	ls.active = true
	ls.haveState = true
	ls.mu.Unlock()
}

func (ls *legacyService) tick() {
	ml := loadMapList()
	now := time.Now().Unix()

	ls.mu.Lock()
	defer ls.mu.Unlock()

	id := ls.pick(ml, now)
	if id < 0 {
		ls.active = false
		return // нет активного МАП — держим последний снимок (старое время)
	}
	payload := mapRawPayload(id)
	if payload == nil {
		ls.active = false
		return
	}
	f, ok := decodeLegacy(payload)
	if !ok {
		ls.active = false
		return
	}
	ls.snap = renderLegacy(f, &ls.acc)
	ls.mem = buildMem(f)
	ls.snapTime = f.meta.LastSeen
	ls.active = true
	ls.haveState = true
}

// memory — копия образа памяти под защитой (чтобы не читать регионы во время
// пересборки в цикле).
func (ls *legacyService) memory() []byte {
	ls.ensureFresh()
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	out := make([]byte, len(ls.mem))
	copy(out, ls.mem)
	return out
}

// available — есть ли найденный МАП или сохранённое состояние (для check_daemons).
func (ls *legacyService) available() bool {
	ls.ensureFresh()
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.active || ls.haveState
}

// pick возвращает слот обслуживаемого МАП или -1.
func (ls *legacyService) pick(ml MapList, now int64) int {
	if ls.selected {
		for _, m := range ml.Maps {
			if m.Serial == ls.selSerial {
				return m.ID
			}
		}
		if ls.lostSince == 0 {
			ls.lostSince = now
		}
		if now-ls.lostSince < 60 {
			return -1 // ждём тот же серийник
		}
		if len(ml.Maps) > 0 { // появился любой — подхватываем
			ls.adopt(ml.Maps[0])
			return ml.Maps[0].ID
		}
		return -1
	}
	if len(ml.Maps) == 0 {
		return -1
	}
	ls.adopt(ml.Maps[0])
	return ml.Maps[0].ID
}

func (ls *legacyService) adopt(m MapInfo) {
	ls.selected = true
	ls.selSerial = m.Serial
	ls.lostSince = 0
}

func (ls *legacyService) snapshot() []byte {
	ls.ensureFresh()
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.snap
}

// countKesInShm — число занятых слотов kes-list в бинарном образе сегмента
// (лимит 4 — число панелей MPPT в legacy-интерфейсе «Мастер-Узел»).
func countKesInShm(data []byte) int {
	if len(data) < kesShmHeader {
		return 0
	}
	n := 0
	for i := 0; i < kesSlots; i++ {
		off := kesShmHeader + i*kesListItemLen
		if off+kesListItemLen > len(data) {
			break
		}
		b := data[off : off+kesListItemLen]
		if binary.LittleEndian.Uint32(b[0:4]) != 1 { // state != 1 → слот не занят
			continue
		}
		if binary.LittleEndian.Uint64(b[8:16])&1 != 0 { // seqlock: идёт запись
			continue
		}
		n++
	}
	if n > 4 {
		n = 4
	}
	return n
}

// presentKesCount — число присутствующих КЭС (из kes-list живого сегмента shm).
func presentKesCount() int {
	data, err := readShmAt(kesShmPath, 0, int64(kesShmHeader+kesSlots*kesListItemLen))
	if err != nil {
		return 0
	}
	return countKesInShm(data)
}

// mapHasBMS — есть ли BMS-банки в текущем снимке МАП (read_bms отдаёт реальные
// банки). Stateless: читает текущий снимок первого МАП (как read_bms.php).
func mapHasBMS() bool {
	ml := loadMapList()
	if len(ml.Maps) == 0 {
		return false
	}
	payload := mapRawPayload(ml.Maps[0].ID)
	if payload == nil {
		return false
	}
	f, ok := decodeLegacy(payload)
	if !ok {
		return false
	}
	bmsf := f.u("LCD_BMSFunk")
	n := int(f.u("BMS_Num"))
	return (bmsf == 1 || bmsf == 3) && n >= 1
}

// handleCheckDaemons — legacy-совместимый статус демонов (открыт, без авторизации).
// Значения как в legacy check_daemons.php: демон есть → 0, нет → 1.
// mapd отражает реальную доступность МАП: 0, если есть найденный МАП или сохранённое
// состояние; иначе 1. mpptd0..3 — 0 для присутствующих КЭС (иначе агрегатор
// «Мастер-Узел» не опрашивает /read_json.php?device=mppt и КЭС не рисуются).
// Остальные демоны (charging/db/emd/emon) — 1.
// batmond — 0 при наличии КЭС (эмулируем batmon для «Мастер-Узла»: иначе агрегатор
// не использует /read_json.php?device=bat и панель показывает NaN из-за строковых
// Pwr_kW); без КЭС — 1 (не маскируем выработку МАП нулём). См. docs/legacy/batmon-api.md.
// bms — доступность /read_bms.php (банки BMS в снимке МАП); 0 — есть, 1 — нет.
// Числовые ключи "0".."7" — байты shm 5000: 0=mapd, 1..4=mpptd0..3, 5=chargerd,
// 6=emon, 7=dbe_flash (0 = подсистема доступна, 1 — нет).
// dbe_flash — не демон, а уровень износа флеша (байт); у нас нет данных → 1.
func (s *Server) handleCheckDaemons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mapd := 1
	if s.legacy.available() {
		mapd = 0
	}
	m := presentKesCount()
	m0, m1, m2, m3 := 1, 1, 1, 1
	if m > 0 {
		m0 = 0
	}
	if m > 1 {
		m1 = 0
	}
	if m > 2 {
		m2 = 0
	}
	if m > 3 {
		m3 = 0
	}
	// Эмулируем batmon только при наличии КЭС: тогда «Мастер-Узел» уходит в ветку
	// bat и берёт mppt_day_E (Вт·ч → кВтч), а не суммирует строковые Pwr_kW (NaN).
	// Для узла без КЭС (только МАП) batmond=1 — не маскируем выработку МАП нулём.
	batmond := 1
	if m > 0 {
		batmond = 0
	}
	// bms — доступность read_bms.php (банки BMS в снимке МАП): 0 — есть, 1 — нет.
	bms := 1
	if mapHasBMS() {
		bms = 0
	}
	// Числовые ключи "0".."7" — статусные байты shm 5000 (см. legacy check_daemons.php):
	// 0=mapd, 1..4=mpptd0..3, 5=chargerd, 6=emon, 7=dbe_flash. У нас эти байты не
	// пишутся, поэтому отдаём честные значения по доступности подсистем (0=есть,1=нет).
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprintf(w,
		`{"0":%d,"1":%d,"2":%d,"3":%d,"4":%d,"5":1,"6":1,"7":1,"mapd":%d,"mpptd0":%d,"mpptd1":%d,"mpptd2":%d,"mpptd3":%d,"chargerd":1,"dbed":1,"batmond":%d,"emd":1,"emon":1,"dbe_flash":1,"bms":%d}`,
		mapd, m0, m1, m2, m3, mapd, m0, m1, m2, m3, batmond, bms)
}

// handleReadJSON — открытый эндпоинт совместимости (без авторизации/сессий).
func (s *Server) handleReadJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Query().Get("device") {
	case "map":
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(s.legacy.snapshot())
	case "mppt":
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Cache-Control", "no-cache")
		snap := kesSnapshotJSON()
		if snap == nil {
			_, _ = w.Write([]byte("[]"))
			return
		}
		_, _ = w.Write(snap)
	case "bat":
		s.handleReadBat(w, r)
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported device"}`))
	}
}

// handleReadBMS — совместимый с legacy read_bms.php: JSON {"updated","devices"}
// (ANT BMS), открытый, без авторизации (как read_json/check_daemons).
// Stateless: считает по требованию из текущего снимка МАП (первый доступный),
// без накопителей и состояния. Собственного bmslistener на узле нет.
func (s *Server) handleReadBMS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")

	body := []byte(`{"updated":0,"devices":[]}`)
	ml := loadMapList()
	if len(ml.Maps) > 0 {
		if payload := mapRawPayload(ml.Maps[0].ID); payload != nil {
			if f, ok := decodeLegacy(payload); ok {
				model := mapModelFull(nil, ml.Maps[0].Model)
				body = renderBMS(f, f.meta.LastSeen, model, ml.Maps[0].Port)
			}
		}
	}
	_, _ = w.Write(body)
}

// handleReadMemory — совместимый с legacy read_memory.php?offset=&count=:
// отдаёт JSON-массив байт из сохранённого образа памяти (0..count-1).
func (s *Server) handleReadMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	cnt, _ := strconv.Atoi(r.URL.Query().Get("count"))
	mem := s.legacy.memory()
	if off < 0 {
		off = 0
	}
	if off > len(mem) {
		off = len(mem)
	}
	if cnt < 0 {
		cnt = 0
	}
	end := off + cnt
	if end > len(mem) {
		end = len(mem)
	}
	arr := make([]int, end-off)
	for i, b := range mem[off:end] {
		arr[i] = int(b)
	}
	body, _ := json.Marshal(arr)
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
