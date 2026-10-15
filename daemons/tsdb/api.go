// api.go — транспорт (только Unix-сокет) и обработчики двоичного API.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

// Server — транспортный слой поверх хранилища.
type Server struct {
	store *Store
}

var errWireFrame = errors.New("неизвестный кадр")

func (srv *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go srv.serveConn(c)
	}
}

func (srv *Server) serveConn(c net.Conn) {
	defer c.Close()
	br := bufio.NewReaderSize(c, 64*1024)
	bw := bufio.NewWriterSize(c, 64*1024)
	for {
		var l uint32
		if err := binary.Read(br, binary.LittleEndian, &l); err != nil {
			return
		}
		if l == 0 || l > maxFrame {
			return
		}
		buf := make([]byte, l)
		if _, err := io.ReadFull(br, buf); err != nil {
			return
		}
		if err := srv.handle(buf, bw); err != nil {
			return
		}
		if err := bw.Flush(); err != nil {
			return
		}
	}
}

func (srv *Server) handle(buf []byte, bw *bufio.Writer) error {
	if len(buf) < 4 {
		return errWireFrame
	}
	switch binary.LittleEndian.Uint32(buf[0:4]) {
	case magicWriteReq:
		return srv.doWrite(buf, bw)
	case magicReadReq:
		return srv.doQuery(buf, bw)
	case magicMetaReq:
		return srv.doMeta(buf, bw)
	default:
		return errWireFrame
	}
}

// doMeta — мета-запросы: список устройств и крайние точки (из памяти, без чтения
// сегментов).
func (srv *Server) doMeta(buf []byte, bw *bufio.Writer) error {
	m, err := decodeMetaReq(buf)
	if err != nil {
		return err
	}
	switch m.Cmd {
	case metaList:
		return writeFrame(bw, encodeMetaList(srv.store.ListDevices()))
	case metaExtremes:
		min, max, ok := srv.store.Extremes(m.Type, m.ID)
		return writeFrame(bw, encodeMetaExtremes(ok, min, max))
	}
	return errWireFrame
}

func writeFrame(bw *bufio.Writer, payload []byte) error {
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := bw.Write(hdr[:]); err != nil {
		return err
	}
	_, err := bw.Write(payload)
	return err
}

func (srv *Server) doWrite(buf []byte, bw *bufio.Writer) error {
	rows, err := decodeWriteReq(buf)
	if err != nil {
		return writeFrame(bw, encodeWriteAck(1, 0))
	}
	n := srv.store.Write(rows) // не блокируется на диск (§7)
	return writeFrame(bw, encodeWriteAck(0, uint32(n)))
}

// validMode проверяет соответствие режима (§1.4) набору устройств/метрик:
//
//	1, 4 (ряд целиком) — метрики не задаются; 2, 3 — метрики обязательны;
//	1, 2 — ровно одно устройство; 3, 4 — одно и более; пустой список — ошибка.
func validMode(q queryReq) bool {
	switch q.Mode {
	case modeWholeOne:
		return len(q.IDs) == 1 && len(q.Metrics) == 0
	case modeSelectedOne:
		return len(q.IDs) == 1 && len(q.Metrics) > 0
	case modeSelectedMul:
		return len(q.IDs) > 0 && len(q.Metrics) > 0
	case modeWholeMul:
		return len(q.IDs) > 0 && len(q.Metrics) == 0
	}
	return false
}

func (srv *Server) doQuery(buf []byte, bw *bufio.Writer) error {
	q, err := decodeReadReq(buf)
	if err != nil {
		return err
	}
	if q.Type == "" || !validMode(q) {
		return writeFrame(bw, encodeReadReply(statusBadMode, nil))
	}
	rows, err := srv.store.Query(Query{Type: q.Type, IDs: q.IDs, From: q.From, To: q.To, Metrics: q.Metrics, Direction: q.Direction, Limit: int(q.Limit), Bucket: int64(q.Bucket)})
	if err != nil {
		status := statusBadMode
		switch {
		case errors.Is(err, errRange):
			status = statusTooWide
		case errors.Is(err, errTooMany):
			status = statusTooMany
		}
		log.Printf("query отказ (status=%d): mode=%d type=%s ids=%d metrics=%d: %v",
			status, q.Mode, q.Type, len(q.IDs), len(q.Metrics), err)
		return writeFrame(bw, encodeReadReply(status, nil))
	}
	log.Printf("query: mode=%d type=%s ids=%d metrics=%d период %s..%s → %d строк",
		q.Mode, q.Type, len(q.IDs), len(q.Metrics),
		time.UnixMilli(q.From).Format(time.RFC3339), time.UnixMilli(q.To).Format(time.RFC3339), len(rows))
	return writeFrame(bw, encodeReadReply(statusOK, rows))
}
