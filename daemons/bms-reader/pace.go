package main

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PACE (YD/T 1363, ASCII-hex). docs/bms/enbms-rs485.md §2.
//
//	запрос:  ~ VER(2) ADR(2) 46 CID2(2) LEN(4) INFO(hex) CHK(4) CR
//	ответ:   ~ VER(2) ADR(2) 46 RTN(2) LEN(4) INFO(hex) CHK(4) CR

const paceVER = 0x20

// lchksum — 4-битное дополнение суммы трёх нибблов LENID.
func lchksum(lenID uint16) byte {
	s := int((lenID>>8)&0xF + (lenID>>4)&0xF + lenID&0xF)
	return byte((16 - (s % 16)) % 16)
}

// chksum — 16-битное дополнение суммы ASCII-кодов от VER до конца INFO.
func chksum(body []byte) uint16 {
	var sum int
	for _, b := range body {
		sum += int(b)
	}
	return uint16((-sum) & 0xFFFF)
}

// buildPace собирает PACE-запрос.
func buildPace(adr int, cid2 byte, info []byte) []byte {
	lenID := uint16(len(info) * 2)
	body := fmt.Sprintf("20%02X46%02X%01X%03X%s",
		adr, cid2, lchksum(lenID), lenID, strings.ToUpper(hex.EncodeToString(info)))
	chk := chksum([]byte(body))
	return []byte(fmt.Sprintf("~%s%04X\r", body, chk))
}

// paceFrame — разобранный ответ.
type paceFrame struct {
	ADR  int
	RTN  int
	Info []byte
}

// readPace читает один ответ (до CR) и проверяет CHK с обычным таймаутом
// опроса (arch §10). Для пробы отсутствующего адреса — readPaceTimeout.
func readPace(t Transport) (paceFrame, error) {
	return readPaceTimeout(t, defTimeout(t))
}

// readPaceTimeout читает ответ с заданным таймаутом. Для probe он короче
// (200 мс), чтобы перебор дырок не блокировал шину.
func readPaceTimeout(t Transport, to time.Duration) (paceFrame, error) {
	t.SetReadTimeout(to)
	deadline := time.Now().Add(to)
	var acc []byte
	for time.Now().Before(deadline) {
		tmp := make([]byte, 256)
		n, err := t.Read(tmp)
		if n > 0 {
			acc = append(acc, tmp[:n]...)
			if i := indexByte(acc, '\r'); i >= 0 {
				return parsePace(acc[:i])
			}
		}
		if err == errTimeout {
			// Транспорт уже выждал свой таймаут — ждать второй раз незачем.
			return paceFrame{}, errTimeout
		}
		if err != nil {
			return paceFrame{}, err
		}
	}
	return paceFrame{}, errTimeout
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// parsePace разбирает ответ без завершающего CR.
func parsePace(b []byte) (paceFrame, error) {
	var f paceFrame
	if len(b) < 1 || b[0] != '~' {
		return f, fmt.Errorf("pace: нет ~")
	}
	s := string(b[1:])
	// body = всё до последних 4 символов CHK
	if len(s) < 4 {
		return f, fmt.Errorf("pace: короткий ответ")
	}
	body, chkStr := s[:len(s)-4], s[len(s)-4:]
	var want uint16
	if _, err := fmt.Sscanf(strings.ToUpper(chkStr), "%04X", &want); err != nil {
		return f, fmt.Errorf("pace: chk %q", chkStr)
	}
	if chksum([]byte(body)) != want {
		return f, fmt.Errorf("pace: chk mismatch")
	}
	// body: VER(2) ADR(2) 46 RTN(2) LEN(4) INFO...
	if len(body) < 12 || body[4:6] != "46" {
		return f, fmt.Errorf("pace: формат")
	}
	_, err := fmt.Sscanf(body[0:2], "%02X", new(int))
	if err != nil {
		return f, err
	}
	var adr, rtn int
	fmt.Sscanf(body[2:4], "%02X", &adr)
	fmt.Sscanf(body[6:8], "%02X", &rtn)
	f.ADR, f.RTN = adr, rtn
	var lenFull uint16
	fmt.Sscanf(body[8:12], "%04X", &lenFull)
	lenID := lenFull & 0x0FFF // LEN = LCHKSUM<<12 | LENID
	infoHex := body[12:]      // VER(2)+ADR(2)+46(2)+RTN(2)+LEN(4)
	if infoHex != "" {
		info, err := hex.DecodeString(infoHex)
		if err != nil {
			return f, fmt.Errorf("pace: info hex")
		}
		f.Info = info
	}
	if len(f.Info)*2 != int(lenID) {
		return f, fmt.Errorf("pace: LENID %d != len(INFO)*2 %d", lenID, len(f.Info)*2)
	}
	return f, nil
}
