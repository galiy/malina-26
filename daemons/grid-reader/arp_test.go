package main

import (
	"context"
	"encoding/binary"
	"net"
	"syscall"
	"testing"
)

func neighMsg(state uint16, dst net.IP, mac []byte) []byte {
	b := make([]byte, ndmsgSize)
	binary.LittleEndian.PutUint16(b[8:10], state)
	add := func(typ uint16, val []byte) {
		al := attrHdrLen + len(val)
		pad := (al + 3) &^ 3
		a := make([]byte, pad)
		binary.LittleEndian.PutUint16(a[0:2], uint16(al))
		binary.LittleEndian.PutUint16(a[2:4], typ)
		copy(a[attrHdrLen:], val)
		b = append(b, a...)
	}
	add(ndaDst, dst.To4())
	add(ndaLladdr, mac)
	return b
}

func TestNeighborMsgReachable(t *testing.T) {
	ip := net.ParseIP("192.0.2.10")
	mac := []byte{0x02, 0, 0, 0, 0, 1}
	if !neighborMsgReachable(neighMsg(nudReach, ip, mac), ip.To4()) {
		t.Fatal("REACHABLE с MAC должен считаться живым")
	}
	cases := []struct {
		name string
		msg  []byte
	}{
		{"STALE", neighMsg(0x04, ip, mac)},
		{"INCOMPLETE", neighMsg(0x01, ip, mac)},
		{"FAILED", neighMsg(0x20, ip, mac)},
		{"нулевой MAC", neighMsg(nudReach, ip, make([]byte, 6))},
		{"другой IP", neighMsg(nudReach, net.ParseIP("192.0.2.11"), mac)},
	}
	for _, tc := range cases {
		if neighborMsgReachable(tc.msg, ip.To4()) {
			t.Fatalf("%s: не должен считаться живым", tc.name)
		}
	}
}

func TestNeighborMsgMalformed(t *testing.T) {
	ip := net.ParseIP("192.0.2.10").To4()
	if neighborMsgReachable([]byte{1, 2, 3}, ip) {
		t.Fatal("усечённое сообщение не должно считаться живым")
	}
	bad := make([]byte, ndmsgSize)
	binary.LittleEndian.PutUint16(bad[8:10], nudReach)
	bad = append(bad, 1, 0, 1, 0) // alen=1 < заголовка атрибута
	if neighborMsgReachable(bad, ip) {
		t.Fatal("битый атрибут не должен считаться живым")
	}
}

func TestNeighborMessagesMultiple(t *testing.T) {
	ip := net.ParseIP("192.0.2.10")
	other := net.ParseIP("192.0.2.11")
	mac := []byte{0x02, 0, 0, 0, 0, 1}
	msgs := []syscall.NetlinkMessage{
		{Header: syscall.NlMsghdr{Type: syscall.RTM_NEWNEIGH}, Data: neighMsg(nudReach, other, mac)},
		{Header: syscall.NlMsghdr{Type: syscall.RTM_NEWNEIGH}, Data: []byte{0, 0, 0, 0, 0, 0, 0, 0, nudReach, 0, 0, 0}},
		{Header: syscall.NlMsghdr{Type: syscall.RTM_NEWNEIGH}, Data: neighMsg(nudReach, ip, mac)},
	}
	if !neighborMessagesReachable(msgs, ip.To4()) {
		t.Fatal("целевой IP в одном из сообщений должен находиться")
	}
	if neighborMessagesReachable(msgs, net.ParseIP("192.0.2.12").To4()) {
		t.Fatal("отсутствующий IP не должен считаться живым")
	}
	// Запись и NLMSG_DONE в одной датаграмме — типичный малый дамп.
	msgs = append(msgs, syscall.NetlinkMessage{Header: syscall.NlMsghdr{Type: syscall.NLMSG_DONE}})
	reachable, done, err := neighborBatchResult(msgs, ip.To4())
	if err != nil || !reachable || !done {
		t.Fatalf("DONE не должен скрывать найденную запись: reachable=%v done=%v err=%v", reachable, done, err)
	}
}

func TestNeighborIPv6(t *testing.T) {
	if _, err := neighborReachable(context.Background(), net.ParseIP("2001:db8::1")); err == nil {
		t.Fatal("для IPv6 ожидалась ошибка (поддерживается только IPv4)")
	}
}
