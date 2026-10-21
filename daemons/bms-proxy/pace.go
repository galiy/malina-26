package main

import (
	"fmt"
	"strings"
)

// PACE / YD-T1363.3 (SEPLOS) — ASCII-hex кадры (docs/bms-proxy.md §10).
//
//	запрос:  ~ VER ADR 46 42 E002 <NN> <CHK> CR
//	ответ:   ~ VER ADR 46 RTN LEN <INFO…> CHK CR
const (
	paceVer  = 0x20
	paceCID1 = 0x46 // CID1 = poll
	cid2Tel  = 0x42 // CID2 = TeleMeter (телеметрия пакета)
	rtnOK    = 0x00
)

// paceRequest — разобранный запрос инвертора.
type paceRequest struct {
	Adr  int  // номер BMS инвертора (ADR), мапится на стабильный слот shm
	CID2 int  // вторичный код команды
	OK   bool // валидность CHK (для диагностики)
}

// parseRequest разбирает входящий PACE-кадр. Возвращает nil, если это не валидный
// ASCII-hex кадр `~ … CR`.
func parseRequest(data []byte) *paceRequest {
	if len(data) < 2 || data[0] != '~' || data[len(data)-1] != '\r' {
		return nil
	}
	t := strings.TrimSpace(string(data[1 : len(data)-1]))
	t = strings.ToUpper(t)
	if len(t) < 14 {
		return nil
	}
	// t = VER(2) ADR(2) CID1(2) CID2(2) [E002 NN] CHK(4)
	ver, err1 := parseHexByte(t[0:2])
	adr, err2 := parseHexByte(t[2:4])
	cid1, err3 := parseHexByte(t[4:6])
	cid2, err4 := parseHexByte(t[6:8])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return nil
	}
	if ver != paceVer || cid1 != paceCID1 {
		return nil
	}
	chkOK := checkPaceChk(t)
	return &paceRequest{Adr: int(adr), CID2: int(cid2), OK: chkOK}
}

// buildTelemetryFrame собирает ответный PACE-кадр с INFO.
func buildTelemetryFrame(adr int, info []byte) []byte {
	// INFO — ВЕРХНИЙ регистр hex (как эталон и живой кадр устройства: `0CD7`).
	body := fmt.Sprintf("%02X%02X%02X%02X%04X%X",
		paceVer, adr&0xFF, paceCID1, rtnOK, paceLenField(2*len(info)), info)
	return []byte("~" + body + fmt.Sprintf("%04X", paceChk(body)) + "\r")
}

// paceLenField — 16-битное поле LEN: LCHKSUM<<12 | LENID (lenid = длина INFO
// в hex-символах).
func paceLenField(lenid int) int {
	nib := ((lenid >> 8) & 0xF) + ((lenid >> 4) & 0xF) + (lenid & 0xF)
	lchk := (-nib) & 0xF
	return (lchk << 12) | (lenid & 0x0FFF)
}

// paceChk — 16-битное дополнение суммы ASCII-кодов строки (VER..конец INFO).
func paceChk(s string) int {
	sum := 0
	for _, c := range s {
		sum += int(c)
	}
	return (-sum) & 0xFFFF
}

// checkPaceChk проверяет CHK (4 последних hex-символа) кадра.
func checkPaceChk(t string) bool {
	if len(t) < 4 {
		return false
	}
	v, err := parseHexInt(t[len(t)-4:])
	if err != nil {
		return false
	}
	return (paceChk(t[:len(t)-4]) & 0xFFFF) == v
}

func parseHexByte(s string) (byte, error) {
	v, err := parseHexInt(s)
	if err != nil {
		return 0, err
	}
	return byte(v), nil
}

func parseHexInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%X", &v)
	return v, err
}
