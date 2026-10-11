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

// --- Внешний контракт (совместим с map-reader/legacy) ---

type writeOp struct {
	Op    string `json:"op"`
	Addr  string `json:"addr,omitempty"`  // hex без префикса, напр. "E3D"
	Data  string `json:"data,omitempty"`  // hex-строка, 2 символа на байт
	Code  string `json:"code,omitempty"`  // hex без префикса (команда)
	Relay int    `json:"relay,omitempty"` // 1..3 (op=relay)
	Mode  string `json:"mode,omitempty"`  // "on"/"off" (op=relay)
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

type kind int

const (
	kindReg kind = iota
	kindCmd
	kindRelay
)

// validateWrite — чистая проверка контракта (без обращения к устройству).
// Разрешение: либо операции с ячейками (RAM/EEPROM), либо одна команда, либо одна
// relay-операция. Смешивать нельзя.
func validateWrite(wr writeRequest) (kind, error) {
	if len(wr.Ops) == 0 {
		return 0, errors.New("пустой список операций")
	}
	if len(wr.Ops) > maxOps {
		return 0, fmt.Errorf("слишком много операций (максимум %d)", maxOps)
	}
	nCmd, nReg, nRelay := 0, 0, 0
	for _, op := range wr.Ops {
		switch op.Op {
		case "cmd":
			nCmd++
			c, err := parseHexUint(op.Code, 8)
			if err != nil {
				return 0, fmt.Errorf("cmd code: %v", err)
			}
			if c < 1 || c > 9 {
				return 0, errors.New("команда вне диапазона 1..9")
			}
		case "reg":
			nReg++
			a, err := parseHexUint(op.Addr, 16)
			if err != nil {
				return 0, fmt.Errorf("reg addr: %v", err)
			}
			d, err := parseHexBytes(op.Data)
			if err != nil {
				return 0, err
			}
			if len(d) < 1 || len(d) > 256 {
				return 0, errors.New("размер блока должен быть 1..256 байт")
			}
			if a+len(d)-1 > 0xFFFF {
				return 0, errors.New("адрес вне области 0x0000..0xFFFF")
			}
		case "relay":
			nRelay++
			if op.Relay < 1 || op.Relay > 3 {
				return 0, errors.New("реле должно быть 1..3")
			}
			if op.Mode != "on" && op.Mode != "off" && op.Mode != "1" && op.Mode != "0" {
				return 0, errors.New("режим реле: on/off/1/0")
			}
		default:
			return 0, fmt.Errorf("неизвестная операция %q", op.Op)
		}
	}
	total := 0
	if nCmd > 0 {
		total++
	}
	if nReg > 0 {
		total++
	}
	if nRelay > 0 {
		total++
	}
	if total != 1 {
		return 0, errors.New("смешение сущностей reg/cmd/relay в одном запросе")
	}
	if nCmd > 1 {
		return 0, errors.New("допустима ровно одна cmd-операция")
	}
	if nRelay > 1 {
		return 0, errors.New("допустима ровно одна relay-операция")
	}
	switch {
	case nCmd == 1:
		return kindCmd, nil
	case nRelay == 1:
		return kindRelay, nil
	default:
		return kindReg, nil
	}
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
func buildSequence(wr writeRequest, k kind) []element {
	if k == kindCmd {
		c, _ := parseHexUint(wr.Ops[0].Code, 8)
		return []element{{opIdx: 0, isCmd: true, code: c}}
	}
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
	k, err := validateWrite(wr)
	if err != nil {
		return fail("bad_request", err.Error())
	}
	e := findEntry(entries, wr.ID, wr.Gen)
	if e == nil {
		return fail("no_device", "устройство не найдено (несовпадение id/gen)")
	}

	if k == kindRelay {
		lastErr := writeRelay(cfg, p, wr.Ops[0])
		publishAfterWrite(cfg, reg, p, e)
		if lastErr == nil {
			return writeResponse{OK: true}
		}
		return writeErrResp(lastErr, -1)
	}

	seq := buildSequence(wr, k)
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

// Адреса реле КЭС (docs/kes-reader.md §4.3, §7.7):
//
//	состояние (RAM) 0x3E3 → адрес устройства 0x5D3 (+0x1F0);
//	команда (legacy) 0x0E3D — уточнить на железе.
const (
	kesRelayStateAddr = 0x05D3
	kesRelayCmdAddr   = 0x0E3D
)

// writeRelay — RMW состояния реле: читаем Relay_C, меняем бит, пишем команду.
func writeRelay(cfg *config, p *Port, op writeOp) error {
	data, err := microartRead(p, kesRelayStateAddr, 0, cfg.probeEcho, cfg.probeResponse)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("нет ответа при чтении состояния реле")
	}
	cur := data[0]
	bit := byte(1) << uint(op.Relay-1)
	on := op.Mode == "on" || op.Mode == "1"
	if on {
		cur |= bit
	} else {
		cur &^= bit
	}
	cmd := []byte{cur}
	if err := microartWrite(p, 0x0000, []byte{3}, cfg.probeEcho, cfg.probeResponse); err != nil {
		return err
	}
	return microartWrite(p, kesRelayCmdAddr, cmd, cfg.probeEcho, cfg.probeResponse)
}

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
