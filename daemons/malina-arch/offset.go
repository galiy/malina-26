package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net"
	"time"
)

const (
	offsetReqMagic   = 0x5053544D // MTSP
	offsetReplyMagic = 0x5153544D // MTSQ
)

func appendStr(b []byte, s string) []byte { b = append(b, byte(len(s))); return append(b, s...) }

func readStr(b []byte, p int) (string, int, bool) {
	if p >= len(b) {
		return "", p, false
	}
	n := int(b[p])
	p++
	if p+n > len(b) {
		return "", p, false
	}
	return string(b[p : p+n]), p + n, true
}

func offsetRequest(typ, id, seg string, quiesce bool) []byte {
	mode := byte(0)
	if quiesce {
		mode = 1
	}
	b := make([]byte, 0, 32)
	b = binary.LittleEndian.AppendUint32(b, offsetReqMagic)
	b = append(b, mode)
	b = appendStr(b, typ)
	b = appendStr(b, id)
	b = appendStr(b, seg)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	return b
}

func parseOffsetReply(b []byte) (uint64, bool, uint8, error) {
	if len(b) < 4+1+8+1+4 || binary.LittleEndian.Uint32(b[:4]) != offsetReplyMagic {
		return 0, false, 0, errors.New("bad MTSQ")
	}
	if crc32.ChecksumIEEE(b[:len(b)-4]) != binary.LittleEndian.Uint32(b[len(b)-4:]) {
		return 0, false, 0, errors.New("bad MTSQ crc")
	}
	code := b[4]
	committed := binary.LittleEndian.Uint64(b[5:13])
	final := b[13] == 1
	return committed, final, code, nil
}

func committedOffset(sock, typ, id, seg string, quiesce bool) (uint64, bool, error) {
	if sock == "" {
		return 0, false, errors.New("raw tsdb socket не задан")
	}
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return 0, false, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	req := offsetRequest(typ, id, seg, quiesce)
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(req)))
	if _, err := bw.Write(hdr[:]); err != nil {
		return 0, false, err
	}
	if _, err := bw.Write(req); err != nil {
		return 0, false, err
	}
	if err := bw.Flush(); err != nil {
		return 0, false, err
	}
	if _, err := readFull(br, hdr[:]); err != nil {
		return 0, false, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || n > 4096 {
		return 0, false, errors.New("bad MTSQ length")
	}
	buf := make([]byte, n)
	if _, err := readFull(br, buf); err != nil {
		return 0, false, err
	}
	committed, final, code, err := parseOffsetReply(buf)
	if err != nil || code != 0 {
		return committed, false, err
	}
	lx.Debugf("MTSP: %s/%s/%s committed=%d final=%v quiesce=%v", typ, id, seg, committed, final, quiesce)
	return committed, true, nil
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
