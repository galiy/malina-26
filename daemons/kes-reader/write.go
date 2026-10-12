package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// maxOps — максимум операций в одном запросе (ограничивает удержание пулера).
const maxOps = 64

// --- Типы ошибок КЭС (Микроарт; коды общие с МАП) ---

// kesError — явная ошибка, вернувшаяся от КЭС в кадре 'e D S' (код D).
type kesError struct{ code int }

func (e *kesError) Error() string { return kesErrorText(e.code) }

var kesErrorNames = map[int]string{
	0x01: "ErrRS_sum: неправильная контрольная сумма",
	0x02: "ErrRS_eco: нет эхо при передаче",
	0x04: "ErrRS_cod: неправильный код при передаче",
	0x10: "ErrRS_NoEEWR: не было предварительной команды разрешения записи в EEPROM",
	0x20: "ErrRS_reserve: используется резерв адресного пространства",
}

func kesErrorText(code int) string {
	if s, ok := kesErrorNames[code]; ok {
		return s
	}
	return fmt.Sprintf("КЭС вернул неописанную ошибку 0x%02X", code)
}

// isRetryable — «невнятные» ошибки Микроарт (CRC/нет эхо/неверный код), отсутствие
// ответа и таймауты — ретраим; явные ошибки КЭС — нет.
func isRetryable(err error) bool {
	var ke *kesError
	if errors.As(err, &ke) {
		switch ke.code {
		case 1, 2, 4:
			return true
		}
		return false
	}
	return true
}

// --- Внешний контракт (целевой: только op=reg, см. docs/kes-reader.md §4.2) ---

type writeOp struct {
	Op   string `json:"op"`
	Addr string `json:"addr,omitempty"` // hex без префикса, напр. "1B6"
	Data string `json:"data,omitempty"` // hex-строка, 2 символа на байт
}

func parseHexUint(s string, bits int) (int, error) {
	if s == "" {
		return 0, errors.New("пустое hex-значение")
	}
	v, err := strconv.ParseUint(s, 16, bits)
	if err != nil {
		return 0, fmt.Errorf("не hex-число: %q", s)
	}
	return int(v), nil
}

func parseHexBytes(s string) ([]byte, error) {
	if len(s) == 0 || len(s)%2 != 0 {
		return nil, errors.New("data: hex-строка должна быть чётной длины")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("data: не hex-строка: %q", s)
	}
	return b, nil
}

type writeRequest struct {
	Cmd     string    `json:"cmd"`
	ID      int       `json:"id"`
	Gen     uint64    `json:"gen"`
	NoFrame bool      `json:"no_frame"`
	Ops     []writeOp `json:"ops"`
}

type writeResponse struct {
	OK    bool    `json:"ok"`
	Code  string  `json:"code,omitempty"`
	Error string  `json:"error,omitempty"`
	Op    *int    `json:"op,omitempty"`
	ECode *string `json:"ecode,omitempty"`
}

func fail(code, msg string) writeResponse { return writeResponse{OK: false, Code: code, Error: msg} }

// validateWrite — чистая проверка контракта (без обращения к устройству).
// Целевая запись КЭС — только ячейки (op=reg); команд (op=cmd) и отдельной операции
// реле (op=relay) нет — реле пишется ячейкой 0x0E3D (см. docs/kes-reader.md §4.2).
func validateWrite(wr writeRequest) error {
	if len(wr.Ops) == 0 {
		return errors.New("пустой список операций")
	}
	if len(wr.Ops) > maxOps {
		return fmt.Errorf("слишком много операций (максимум %d)", maxOps)
	}
	for _, op := range wr.Ops {
		if op.Op != "reg" {
			return fmt.Errorf("неизвестная операция %q (допустима только op=reg)", op.Op)
		}
		a, err := parseHexUint(op.Addr, 16)
		if err != nil {
			return fmt.Errorf("reg addr: %v", err)
		}
		d, err := parseHexBytes(op.Data)
		if err != nil {
			return err
		}
		if len(d) < 1 || len(d) > 256 {
			return errors.New("размер блока должен быть 1..256 байт")
		}
		if a+len(d)-1 > 0xFFFF {
			return errors.New("адрес вне области 0x0000..0xFFFF")
		}
	}
	return nil
}

type element struct {
	opIdx int
	isCmd bool
	code  int
	addr  int
	data  []byte
}

// buildSequence — последовательность посылок. Запись ячеек: перед каждой посылкой
// — команда разрешения записи (ComMAP_EEPromWR=3), затем блок по адресу «как есть»
// (без +0x1F0). Post-команды (7) у КЭС нет (legacy mpptd, docs/legacy/…§1c).
// no_frame — «сырой» отладочный режим: пред-команда 3 НЕ шлётся, поэтому для
// EEPROM/RAM КЭС запись почти гарантированно вернёт ошибку 0x10 (ErrRS_NoEEWR);
// использовать только для диагностики транспорта (см. docs/kes-reader.md §4.2.3).
func buildSequence(wr writeRequest) []element {
	var seq []element
	for i, op := range wr.Ops {
		if !wr.NoFrame {
			seq = append(seq, element{opIdx: -1, isCmd: true, code: 3}) // EEPromWR
		}
		a, _ := parseHexUint(op.Addr, 16)
		d, _ := parseHexBytes(op.Data)
		seq = append(seq, element{opIdx: i, addr: a, data: d})
	}
	return seq
}

func findEntry(entries []*kesEntry, id int, gen uint64) *kesEntry {
	for _, e := range entries {
		if e.slotIdx == id && e.gen == gen {
			return e
		}
	}
	return nil
}

// executeWrite выполняется в горутине пулера порта (порт занят только ею).
func executeWrite(cfg *config, reg *Registry, p *Port, entries []*kesEntry, wr writeRequest) writeResponse {
	if err := validateWrite(wr); err != nil {
		return fail("bad_request", err.Error())
	}
	e := findEntry(entries, wr.ID, wr.Gen)
	if e == nil {
		return fail("no_device", "устройство не найдено (несовпадение id/gen)")
	}

	seq := buildSequence(wr)
	var lastErr error
	lastOp := -1
	for _, el := range seq {
		if lastErr = execElement(cfg, p, el); lastErr != nil {
			lastOp = el.opIdx
			break
		}
	}
	publishAfterWrite(cfg, reg, p, e)
	if lastErr == nil {
		return writeResponse{OK: true}
	}
	return writeErrResp(lastErr, lastOp)
}

func writeErrResp(err error, lastOp int) writeResponse {
	resp := writeResponse{OK: false, Error: err.Error()}
	if lastOp >= 0 {
		idx := lastOp
		resp.Op = &idx
	}
	var ke *kesError
	if errors.As(err, &ke) {
		resp.Code = "kes_error"
		ec := fmt.Sprintf("%02X", ke.code)
		resp.ECode = &ec
	} else {
		resp.Code = "retry_exhausted"
	}
	return resp
}

// execElement — операция с ретраями.
func execElement(cfg *config, p *Port, el element) error {
	if cfg.writeRetries < 1 {
		return errors.New("нет попыток записи (writeRetries < 1)")
	}
	var last error
	for attempt := 0; attempt < cfg.writeRetries; attempt++ {
		if el.isCmd {
			last = microartWrite(p, 0x0000, []byte{byte(el.code)}, cfg.probeEcho, cfg.probeResponse)
		} else {
			last = microartWrite(p, uint16(el.addr), el.data, cfg.probeEcho, cfg.probeResponse)
		}
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

// Адреса реле КЭС (docs/kes-reader.md §4.2.5, §7.7): состояние (RAM) 0x3E3 → адрес
// устройства 0x5D3 (+0x1F0); командная ячейка 0x0E3D — write-only, вне 0x000..0x7FF.
// Реле пишется как обычная ячейка: op=reg {addr:"E3D", data:<маска>}; RMW — на стороне
// клиента. Сама запись — по общему пути, поэтому спец-функций/констант в демоне нет.

// publishAfterWrite — контрольный цикл чтения и публикация (best-effort).
func publishAfterWrite(cfg *config, reg *Registry, p *Port, e *kesEntry) {
	for attempt := 0; attempt < kesPollRetries; attempt++ {
		img, err := readAllWindows(p, cfg.probeEcho, cfg.probeResponse)
		if err == nil {
			reg.SetInfo(e, parseKes(img))
			reg.Touch(e)
			e.slot.Publish(buildPayload(reg.Meta(e), parseData(img)))
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	cfg.debugf("write: контрольное чтение не удалось (%s)", e.port)
}

type writeJob struct {
	req  writeRequest
	resp chan writeResponse
}
