package main

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"
)

// TestTouchLastSeen — пулер обновляет только last_seen; остальные поля записи
// и state сохраняются; чужая генерация не трогается.
func TestTouchLastSeen(t *testing.T) {
	o, _ := newTestOrch(t)
	st := &srcState{src: &source{desc: "serial:/dev/ttyA", tr: &fakeTransport{}}, bt: enBMS{}}
	o.states[st.src.desc] = st
	if !o.addSlot(st, probeSlot{Addr: 0, Base: "BMS-A"}) {
		t.Fatal("слот не добавлен")
	}
	d := st.devs[0]
	o.publishList(d, 1)

	if !o.seg.touchLastSeen(d.slot, d.gen, time.Unix(1234, 0)) {
		t.Fatal("touch отклонён")
	}
	b := o.seg.seg.Item(d.slot).Bytes()
	if got := int64(binary.LittleEndian.Uint64(b[itOffLastSeen : itOffLastSeen+8])); got != 1234 {
		t.Fatalf("last_seen=%d", got)
	}
	if !strings.HasPrefix(string(b[itOffID:itOffID+5]), "BMS-A") {
		t.Fatalf("ID повреждён: %q", b[itOffID:itOffID+50])
	}
	if !strings.HasPrefix(string(b[itOffBase:itOffBase+5]), "BMS-A") {
		t.Fatalf("base повреждён: %q", b[itOffBase:itOffBase+50])
	}
	if !strings.HasPrefix(string(b[itOffSource:itOffSource+13]), "serial:/dev/") {
		t.Fatalf("source повреждён: %q", b[itOffSource:itOffSource+64])
	}
	if gotGen := binary.LittleEndian.Uint64(b[itOffGen : itOffGen+8]); gotGen != d.gen {
		t.Fatalf("gen повреждён: %d", gotGen)
	}
	if b[itOffTypeID] != 2 || b[itOffFraming] != framingNative {
		t.Fatalf("type/framing повреждены: %d/%d", b[itOffTypeID], b[itOffFraming])
	}
	if b[itOffState] != 1 {
		t.Fatalf("state=%d", b[itOffState])
	}
	if o.seg.touchLastSeen(d.slot, d.gen+1, time.Unix(2345, 0)) {
		t.Fatal("чужая генерация принята")
	}
	if got := int64(binary.LittleEndian.Uint64(b[itOffLastSeen : itOffLastSeen+8])); got != 1234 {
		t.Fatalf("last_seen изменён чужой генерацией: %d", got)
	}
}
