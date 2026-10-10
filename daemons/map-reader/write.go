package main

import (
	"errors"
	"fmt"
	"time"
)

// --- Типы ошибок МАП ---

// mapError — явная ошибка, вернувшаяся от МАП в кадре 'e D S' (код D).
type mapError struct{ code int }

func (e *mapError) Error() string { return fmt.Sprintf("МАП вернул ошибку %d", e.code) }

// modbusError — Modbus-исключение (ответ с установленным старшим битом fc).
type modbusError struct{ code int }

func (e *modbusError) Error() string { return fmt.Sprintf("Modbus exception %d", e.code) }

// errUnsupported — операция не поддержана для данного протокола/случая.
var errUnsupported = errors.New("не поддерживается для этого протокола")

// isRetryable — нужно ли повторять операцию при данной ошибке.
// «Невнятные» ошибки Микроарт (CRC=1, нет эхо=2, неверный код=4), отсутствие
// ответа и таймауты — ретраим. Явные ошибки МАП (напр. 0x10) и Modbus-исключения
// — не ретраим; непреодолимые ограничения — тоже.
func isRetryable(err error) bool {
	var me *mapError
	if errors.As(err, &me) {
		switch me.code {
		case 1, 2, 4:
			return true
		}
		return false
	}
	var mbe *modbusError
	if errors.As(err, &mbe) {
		return false
	}
	if errors.Is(err, errUnsupported) {
		return false
	}
	return true // errNoMap / сетевые/IO — «невнятные», ретраим
}

// --- Внешний контракт ---

type writeOp struct {
	Op   string `json:"op"`
	Addr int    `json:"addr,omitempty"`
	Data []int  `json:"data,omitempty"`
	Code int    `json:"code,omitempty"`
}

type writeRequest struct {
	Cmd     string    `json:"cmd"`
	ID      int       `json:"id"`
	Gen     uint64    `json:"gen"`
	NoFrame bool      `json:"no_frame"`
	Ops     []writeOp `json:"ops"`
}

type writeResponse struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
	Op    *int   `json:"op,omitempty"`
	ECode *int   `json:"ecode,omitempty"`
}

func fail(code, msg string) writeResponse {
	return writeResponse{OK: false, Code: code, Error: msg}
}

// --- Классификация запроса ---

type kind int

const (
	kindReg kind = iota
	kindCmd
)

// validateWrite — чистая проверка контракта (без обращения к МАП).
// Разрешение: один запрос — это либо операции с регистрами (EEPROM и/или RAM
// вместе), либо ровно одна команда. Смешивать регистры и команду нельзя.
func validateWrite(wr writeRequest) (kind, error) {
	if len(wr.Ops) == 0 {
		return 0, errors.New("пустой список операций")
	}
	nCmd, nReg := 0, 0
	for _, op := range wr.Ops {
		switch op.Op {
		case "cmd":
			nCmd++
			if op.Code < 0 || op.Code > 255 {
				return 0, errors.New("code вне диапазона 0..255")
			}
		case "reg":
			nReg++
			if len(op.Data) < 1 || len(op.Data) > 256 {
				return 0, errors.New("размер блока должен быть 1..256 байт")
			}
			if op.Addr < 0 || op.Addr+len(op.Data)-1 > 0x7FF {
				return 0, errors.New("адрес вне области 0x000..0x7FF")
			}
			for _, b := range op.Data {
				if b < 0 || b > 255 {
					return 0, errors.New("значение байта вне диапазона 0..255")
				}
			}
		default:
			return 0, fmt.Errorf("неизвестная операция %q", op.Op)
		}
	}
	if nCmd > 0 && nReg > 0 {
		return 0, errors.New("смешение сущностей reg/cmd в одном запросе")
	}
	if nCmd > 1 {
		return 0, errors.New("допустима ровно одна cmd-операция")
	}
	if nCmd == 1 {
		return kindCmd, nil
	}
	return kindReg, nil
}

// --- Последовательность операций (с обрамлением) ---

type element struct {
	opIdx int    // индекс в Ops; -1 — служебная команда обрамления
	isCmd bool   // команда: байт по адресу 0
	code  int    // код команды
	addr  int    // адрес блока (для reg)
	data  []byte // байты блока (для reg)
}

func buildSequence(wr writeRequest, k kind) []element {
	var seq []element
	if k == kindCmd {
		return []element{{opIdx: 0, isCmd: true, code: wr.Ops[0].Code}}
	}
	// Регистровая запись (EEPROM и RAM): разрешение (pre 3) — ПЕРЕД КАЖДОЙ
	// посылкой записи (МАП потребляет его на одну запись), затем один post 7.
	for i, op := range wr.Ops {
		if !wr.NoFrame {
			seq = append(seq, element{opIdx: -1, isCmd: true, code: 3}) // ComMAP_EEPromWR
		}
		data := make([]byte, len(op.Data))
		for j, b := range op.Data {
			data[j] = byte(b)
		}
		seq = append(seq, element{opIdx: i, addr: op.Addr, data: data})
	}
	if !wr.NoFrame {
		seq = append(seq, element{opIdx: -1, isCmd: true, code: 7}) // ComMAP_Call_load_EEProm
	}
	return seq
}

// --- Исполнение ---

func findEntry(entries []*MapEntry, id int, gen uint64) *MapEntry {
	for _, e := range entries {
		if e.slotIdx == id && e.gen == gen {
			return e
		}
	}
	return nil
}

// executeWrite выполняется в горутине целевого пулера (порт занят только ею).
func executeWrite(cfg *config, reg *Registry, p *Port, proto string, entries []*MapEntry, wr writeRequest) writeResponse {
	k, err := validateWrite(wr)
	if err != nil {
		return fail("bad_request", err.Error())
	}
	e := findEntry(entries, wr.ID, wr.Gen)
	if e == nil {
		return fail("no_device", "устройство не найдено (несовпадение id/gen)")
	}

	// Ограничения Modbus (см. docs/map-write.md §6).
	if proto == "modbus" {
		for _, op := range wr.Ops {
			if op.Op != "reg" {
				continue
			}
			if op.Addr%2 != 0 || len(op.Data)%2 != 0 {
				return fail("bad_request", "Modbus: поддержаны только блоки с чётным адресом и чётной длиной (docs/map-write.md §6)")
			}
			if len(op.Data) > 250 {
				return fail("bad_request", "Modbus: блок больше 250 байт")
			}
		}
	}

	seq := buildSequence(wr, k)
	var lastErr error
	lastOp := -1
	for _, el := range seq {
		if lastErr = execElement(cfg, p, proto, e.unit, el); lastErr != nil {
			lastOp = el.opIdx
			break
		}
	}

	// Полный успешный цикл чтения и публикация в shm — до ответа клиенту.
	publishAfterWrite(cfg, reg, p, proto, e)

	if lastErr == nil {
		return writeResponse{OK: true}
	}
	resp := writeResponse{OK: false, Error: lastErr.Error()}
	if lastOp >= 0 {
		idx := lastOp
		resp.Op = &idx
	}
	var me *mapError
	if errors.As(lastErr, &me) {
		resp.Code = "map_error"
		c := me.code
		resp.ECode = &c
	} else if errors.Is(lastErr, errUnsupported) {
		resp.Code = "bad_request"
	} else {
		resp.Code = "retry_exhausted"
	}
	return resp
}

// execElement — операция с ретраями (5 попыток, пауза 0.5 с).
func execElement(cfg *config, p *Port, proto string, unit int, el element) error {
	var last error
	for attempt := 0; attempt < cfg.writeRetries; attempt++ {
		last = doElement(cfg, p, proto, unit, el)
		if last == nil {
			return nil
		}
		if !isRetryable(last) {
			return last
		}
		if attempt < cfg.writeRetries-1 {
			time.Sleep(cfg.writePause)
		}
	}
	return last
}

func doElement(cfg *config, p *Port, proto string, unit int, el element) error {
	if proto == "modbus" {
		if el.isCmd {
			return modbusWriteSingle(p, unit, 0x0000, uint16(el.code), cfg.probeResponse)
		}
		return modbusWriteBlock(p, unit, uint16(el.addr), el.data, cfg.probeResponse)
	}
	if el.isCmd {
		return microartWrite(p, 0x0000, []byte{byte(el.code)}, cfg.probeEcho, cfg.probeResponse)
	}
	return microartWrite(p, uint16(el.addr), el.data, cfg.probeEcho, cfg.probeResponse)
}

// publishAfterWrite — контрольный цикл чтения и публикация (best-effort).
func publishAfterWrite(cfg *config, reg *Registry, p *Port, proto string, e *MapEntry) {
	for attempt := 0; attempt < pollRetries; attempt++ {
		mem, err := readEntry(cfg, p, proto, e)
		if err == nil {
			reg.Touch(e)
			simInjectKesBms(mem)
			e.slot.Publish(buildContract(mem, reg.Meta(e)))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	cfg.debugf("write: контрольное чтение не удалось (unit=%d)", e.unit)
}

// --- Очередь команд к пулеру ---

type writeJob struct {
	req  writeRequest
	resp chan writeResponse
}
