package main

import (
	"testing"

	"github.com/galiy/malina-26/daemons/lib/tsdbclient"
)

// TestWireCompatClientServer — совместимость кодеков клиента (lib/tsdbclient) и
// сервера (wire.go, §10): кадры клиента декодируются сервером, ответы сервера —
// клиентом. Защищает от расхождения протокола при правках.
func TestWireCompatClientServer(t *testing.T) {
	// запись: клиентский энкодер → серверный декодер
	rows := []tsdbclient.Row{
		{Type: "map", ID: "5", Time: 1000, Version: 1, Payload: []byte{1, 2, 3}},
		{Type: "kes", ID: "7", Time: 2000, Version: 1, Payload: []byte{9}},
	}
	srv, err := decodeWriteReq(tsdbclient.EncodeWriteReq(rows))
	if err != nil || len(srv) != 2 {
		t.Fatalf("decodeWriteReq: err=%v, строк=%d", err, len(srv))
	}
	if srv[0].Type != "map" || srv[0].ID != "5" || srv[0].Time != 1000 || srv[0].Version != 1 || string(srv[0].Payload) != "\x01\x02\x03" {
		t.Fatalf("строка 0: %+v", srv[0])
	}
	if srv[1].Type != "kes" || srv[1].ID != "7" || srv[1].Time != 2000 || string(srv[1].Payload) != "\x09" {
		t.Fatalf("строка 1: %+v", srv[1])
	}

	// чтение: клиентский энкодер → серверный декодер
	q, err := decodeReadReq(tsdbclient.EncodeReadReq(tsdbclient.ModeSelectedOne, "map", []string{"5", "6"}, []uint16{3, 4}, 100, 200, tsdbclient.DirectionTo, 200))
	if err != nil {
		t.Fatalf("decodeReadReq: %v", err)
	}
	if q.Mode != modeSelectedOne || q.Type != "map" || len(q.IDs) != 2 || q.IDs[0] != "5" || q.IDs[1] != "6" ||
		len(q.Metrics) != 2 || q.Metrics[0] != 3 || q.Metrics[1] != 4 || q.From != 100 || q.To != 200 ||
		q.Direction != tsdbclient.DirectionTo || q.Limit != 200 {
		t.Fatalf("запрос: %+v", q)
	}

	// ответ записи: серверный энкодер → клиентский парсер (как в Client.Write)
	ack := encodeWriteAck(0, 2)
	if len(ack) < 9 || string(ack[0:4]) != "MTSK" {
		t.Fatalf("ack: % x", ack)
	}
	if ack[4] != 0 || uint32(ack[5])|uint32(ack[6])<<8|uint32(ack[7])<<16|uint32(ack[8])<<24 != 2 {
		t.Fatalf("ack поля: % x", ack)
	}

	// ответ чтения: серверный энкодер → клиентский парсер
	reply := encodeReadReply(statusOK, []Row{{Type: "map", ID: "5", Time: 1000, Version: 1, Payload: []byte{4, 5}}})
	st, got, err := tsdbclient.ParseReadReply(reply)
	if err != nil {
		t.Fatalf("ParseReadReply: %v", err)
	}
	if st != tsdbclient.StatusOK || len(got) != 1 || got[0].Type != "map" || got[0].ID != "5" ||
		got[0].Time != 1000 || got[0].Version != 1 || string(got[0].Payload) != "\x04\x05" {
		t.Fatalf("ответ: st=%d %+v", st, got)
	}
}
