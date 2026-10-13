package main

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

func encodeWriteReqForTest(rows []Row) []byte {
	b := binary.LittleEndian.AppendUint32(nil, magicWriteReq)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(rows)))
	for _, r := range rows {
		b = appendStr(b, r.Type)
		b = appendStr(b, r.ID)
		b = binary.LittleEndian.AppendUint64(b, uint64(r.Time))
		b = binary.LittleEndian.AppendUint16(b, r.Version)
		var tmp [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(tmp[:], uint64(len(r.Payload)))
		b = append(b, tmp[:n]...)
		b = append(b, r.Payload...)
	}
	return finish(b)
}

func encodeReadReqForTest(mode uint8, typ string, ids []string, metrics []uint16, from, to int64) []byte {
	b := binary.LittleEndian.AppendUint32(nil, magicReadReq)
	b = append(b, mode)
	b = appendStr(b, typ)
	b = append(b, byte(len(ids)))
	for _, id := range ids {
		b = appendStr(b, id)
	}
	b = append(b, byte(len(metrics)))
	for _, m := range metrics {
		b = binary.LittleEndian.AppendUint16(b, m)
	}
	b = binary.LittleEndian.AppendUint64(b, uint64(from))
	b = binary.LittleEndian.AppendUint64(b, uint64(to))
	return finish(b)
}

func writeFrameConn(t *testing.T, w io.Writer, payload []byte) {
	t.Helper()
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
}

func readFrameConn(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, binary.LittleEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func parseReadReply(t *testing.T, b []byte) []Row {
	t.Helper()
	if len(b) < 9 || binary.LittleEndian.Uint32(b[0:4]) != magicReadReply {
		t.Fatalf("нет заголовка ответа: % x", b[:min(9, len(b))])
	}
	if !checkCRC(b) {
		t.Fatal("плохой CRC ответа")
	}
	if b[4] != statusOK {
		t.Fatalf("статус ответа = %d, ожидался OK", b[4])
	}
	var rows []Row
	pos := 5
	for pos < len(b)-8 {
		var r Row
		var err error
		if r.Type, pos, err = readStrAt(b, pos); err != nil {
			t.Fatal(err)
		}
		if r.ID, pos, err = readStrAt(b, pos); err != nil {
			t.Fatal(err)
		}
		if pos+10 > len(b) {
			t.Fatal("обрыв строки")
		}
		r.Time = int64(binary.LittleEndian.Uint64(b[pos : pos+8]))
		r.Version = binary.LittleEndian.Uint16(b[pos+8 : pos+10])
		pos += 10
		pl, n := binary.Uvarint(b[pos:])
		if n <= 0 {
			t.Fatal("обрыв длины")
		}
		pos += n
		if pos+int(pl) > len(b) {
			t.Fatal("обрыв payload")
		}
		r.Payload = b[pos : pos+int(pl)]
		pos += int(pl)
		rows = append(rows, r)
	}
	if binary.LittleEndian.Uint32(b[pos:pos+4]) != magicReadEnd {
		t.Fatal("нет маркера конца")
	}
	return rows
}

func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	st, err := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{store: st}, st
}

func TestAPITransport(t *testing.T) {
	srv, _ := newTestServer(t)
	cli, srvC := net.Pipe()
	defer cli.Close()
	go srv.serveConn(srvC)
	cli.SetDeadline(time.Now().Add(5 * time.Second))

	writeFrameConn(t, cli, encodeWriteReqForTest([]Row{
		rowT(t, "map", "5", 1000, sample(1)...),
		rowT(t, "map", "5", 2000, sample(2)...),
	}))
	ack := readFrameConn(t, cli)
	if binary.LittleEndian.Uint32(ack[0:4]) != magicWriteAck || ack[4] != 0 {
		t.Fatalf("ack: % x", ack)
	}

	writeFrameConn(t, cli, encodeReadReqForTest(modeWholeOne, "map", []string{"5"}, nil, 0, 4000))
	rows := parseReadReply(t, readFrameConn(t, cli))
	if len(rows) != 2 || rows[0].Time != 1000 || rows[0].ID != "5" {
		t.Fatalf("ряд целиком: %+v", rows)
	}
}

func TestAPIProjection(t *testing.T) {
	srv, _ := newTestServer(t)
	cli, srvC := net.Pipe()
	defer cli.Close()
	go srv.serveConn(srvC)
	cli.SetDeadline(time.Now().Add(5 * time.Second))

	writeFrameConn(t, cli, encodeWriteReqForTest([]Row{
		rowT(t, "map", "5", 1000, sample(1)...),
		rowT(t, "map", "5", 2000, sample(2)...),
	}))
	readFrameConn(t, cli) // ack

	writeFrameConn(t, cli, encodeReadReqForTest(modeSelectedOne, "map", []string{"5"}, []uint16{3}, 0, 4000))
	rows := parseReadReply(t, readFrameConn(t, cli))
	c, _ := tscontract.NewDefaultRegistry().Get("map", 1)
	if len(rows) != 2 {
		t.Fatalf("строк: %d", len(rows))
	}
	for _, r := range rows {
		vals, err := c.Decode(r.Payload)
		if err != nil || len(vals) != 1 || vals[0].ID != 3 {
			t.Fatalf("проекция: %+v err=%v", vals, err)
		}
	}
}

// readStatus читает кадр ответа и возвращает его статус.
func readStatus(t *testing.T, b []byte) uint8 {
	t.Helper()
	if len(b) < 9 || binary.LittleEndian.Uint32(b[0:4]) != magicReadReply {
		t.Fatalf("нет заголовка ответа: % x", b[:min(9, len(b))])
	}
	return b[4]
}

func TestAPIBadMode(t *testing.T) {
	srv, _ := newTestServer(t)
	cli, srvC := net.Pipe()
	defer cli.Close()
	go srv.serveConn(srvC)
	cli.SetDeadline(time.Now().Add(5 * time.Second))

	// Режим 2 (selected/один), но два устройства — невалидно.
	writeFrameConn(t, cli, encodeReadReqForTest(modeSelectedOne, "map", []string{"5", "6"}, []uint16{1}, 0, 1000))
	if st := readStatus(t, readFrameConn(t, cli)); st != statusBadMode {
		t.Fatalf("статус = %d, ожидался statusBadMode", st)
	}
}

func TestAPIReadTooWide(t *testing.T) {
	srv, _ := newTestServer(t) // defaultOptions: MaxRangeMS ≈ 400 суток
	cli, srvC := net.Pipe()
	defer cli.Close()
	go srv.serveConn(srvC)
	cli.SetDeadline(time.Now().Add(5 * time.Second))

	// Период ~500 суток > лимита.
	writeFrameConn(t, cli, encodeReadReqForTest(modeWholeOne, "map", []string{"5"}, nil, 0, 500*24*3600*1000))
	if st := readStatus(t, readFrameConn(t, cli)); st != statusTooWide {
		t.Fatalf("статус = %d, ожидался statusTooWide", st)
	}
}
