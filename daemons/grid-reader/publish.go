package main

import (
	"time"

	"github.com/galiy/malina-26/daemons/lib/stat"
	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

var tsreg = tscontract.NewDefaultRegistry()

type tsdbSink struct{ s *stat.Sink }

func newTsdbSink(sock string) *tsdbSink {
	if sock == "" {
		return nil
	}
	return &tsdbSink{s: stat.New(sock)}
}

func (t *tsdbSink) publish(serial string, payload []byte) {
	if t == nil || t.s == nil || serial == "" {
		return
	}
	t.s.Publish("grid", serial, nowMillis(), 1, payload)
}

func nowMillis() int64 { return time.Now().UnixMilli() }

func (t *tsdbSink) Close() {
	if t != nil && t.s != nil {
		t.s.Close()
	}
}
