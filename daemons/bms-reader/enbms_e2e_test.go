package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"
)

// buildPaceResp собирает PACE-ответ (RTN + INFO).
func buildPaceResp(adr int, rtn byte, info []byte) []byte {
	lenID := uint16(len(info) * 2)
	body := fmt.Sprintf("20%02X46%02X%01X%03X%s",
		adr, rtn, lchksum(lenID), lenID, strings.ToUpper(hex.EncodeToString(info)))
	return []byte(fmt.Sprintf("~%s%04X\r", body, chksum([]byte(body))))
}

// paceEmu — эмулятор EnBMS RS485 (PACE over TCP): SN на ADR 0, телеметрия 0x42.
func paceEmu(t *testing.T) (host string, port int, stop func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sn := fmt.Sprintf("%-30s", "EMU110110S-XXXX-YYMMDD-NNNNN")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 512)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					frame := buf[:n]
					i := indexByte(frame, '\r')
					if i < 0 {
						continue
					}
					s := string(frame[1:i]) // без ~ и CR
					if len(s) < 8 {
						continue
					}
					var adr int
					fmt.Sscanf(s[2:4], "%02X", &adr)
					cid2 := s[6:8]
					switch {
					case cid2 == "A4" && adr == 0:
						c.Write(buildPaceResp(adr, 0x00, []byte(sn)))
					case cid2 == "42":
						c.Write(buildPaceResp(adr, 0x00, buildEn42()))
					default:
						c.Write(buildPaceResp(adr, 0x04, nil))
					}
				}
			}(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { ln.Close() }
}

// TestEnBmsE2E — probe по SN и опрос телеметрии через TCP-эмулятор.
func TestEnBmsE2E(t *testing.T) {
	host, port, stop := paceEmu(t)
	defer stop()

	tr, err := openTCP(host, port)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	res, err := enBMS{}.Probe(tr)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != probeFound || len(res.Slots) != 1 || res.Slots[0].Addr != 0 {
		t.Fatalf("probe: %+v", res)
	}
	if !strings.HasPrefix(res.Slots[0].Base, "EMU110110S") {
		t.Fatalf("base = %q", res.Slots[0].Base)
	}

	tel, err := enBMS{}.Poll(tr, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tel.CellCount != 2 || tel.CurrentDA != 123 || tel.SocDP != 500 {
		t.Fatalf("poll: %+v", tel)
	}
}
