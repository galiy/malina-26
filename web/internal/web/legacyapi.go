package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// legacyService — фоновый расчёт снимка read_json.php?device=map.
// Состояние только в памяти; выбор МАП — по серийнику.
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
	selLetter byte
	lostSince int64
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

func (ls *legacyService) run() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ls.stop:
			return
		case job := <-ls.cmds:
			ls.doWrite(job) // запись + немедленное перечитывание
		case <-t.C:
			ls.tick()
		}
	}
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

// targetID — МАП, за которым закреплён legacy-модуль (по серийнику). Если
// привязки нет — false (писать некуда).
func (ls *legacyService) targetID() (int, uint64, bool) {
	ls.mu.RLock()
	sel, ser, letter := ls.selected, ls.selSerial, ls.selLetter
	ls.mu.RUnlock()
	if !sel {
		return 0, 0, false
	}
	for _, m := range loadMapList().Maps {
		if m.Serial == ser && letterByte(m.SerialLetter) == letter {
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
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	out := make([]byte, len(ls.mem))
	copy(out, ls.mem)
	return out
}

// available — есть ли найденный МАП или сохранённое состояние (для check_daemons).
func (ls *legacyService) available() bool {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.active || ls.haveState
}

// pick возвращает слот обслуживаемого МАП или -1.
func (ls *legacyService) pick(ml MapList, now int64) int {
	if ls.selected {
		for _, m := range ml.Maps {
			if m.Serial == ls.selSerial && letterByte(m.SerialLetter) == ls.selLetter {
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
	ls.selLetter = letterByte(m.SerialLetter)
	ls.lostSince = 0
}

func letterByte(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

func (ls *legacyService) snapshot() []byte {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.snap
}

// handleCheckDaemons — legacy-совместимый статус демонов (открыт, без авторизации).
// Значения как в legacy check_daemons.php: демон есть → 0, нет → 1.
// mapd отражает реальную доступность: 0, если есть найденный МАП или сохранённое
// состояние; иначе 1. Остальные демоны (КЭС/charging/batmon/db/emd/emon) — 1.
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
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprintf(w,
		`{"0":%d,"1":1,"2":1,"3":1,"4":1,"5":1,"6":1,"7":1,"mapd":%d,"mpptd0":1,"mpptd1":1,"mpptd2":1,"mpptd3":1,"chargerd":1,"dbed":1,"batmond":1,"emd":1,"emon":1,"dbe_flash":1}`,
		mapd, mapd)
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
		snap := kesSnapshotJSON()
		if snap == nil {
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(snap)
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported device"}`))
	}
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
