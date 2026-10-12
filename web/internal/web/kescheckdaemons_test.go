package web

import (
	"encoding/binary"
	"testing"
)

// kesShmBlob — бинарный образ сегмента kes-list заданного размера с N занятыми
// слотами (state=1, seqlock чётный). Остальные слоты пустые (state=0).
func kesShmBlob(n int) []byte {
	buf := make([]byte, kesShmHeader+kesSlots*kesListItemLen)
	for i := 0; i < n && i < kesSlots; i++ {
		off := kesShmHeader + i*kesListItemLen
		binary.LittleEndian.PutUint32(buf[off:off+4], 1) // state
		binary.LittleEndian.PutUint64(buf[off+8:off+16], 0)
	}
	return buf
}

func TestCountKesInShm(t *testing.T) {
	cases := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, 1},
		{3, 3},
		{4, 4},
		{5, 4}, // лимит панелей MPPT
		{32, 4},
	}
	for _, c := range cases {
		if got := countKesInShm(kesShmBlob(c.n)); got != c.want {
			t.Errorf("countKesInShm(%d активных) = %d, want %d", c.n, got, c.want)
		}
	}
	if got := countKesInShm(nil); got != 0 {
		t.Errorf("countKesInShm(nil) = %d, want 0", got)
	}
	if got := countKesInShm(make([]byte, kesShmHeader-1)); got != 0 {
		t.Errorf("countKesInShm(короткий буфер) = %d, want 0", got)
	}
}

func TestCountKesInShmSeqlock(t *testing.T) {
	buf := kesShmBlob(2)
	// Помечаем первый слот «идёт запись» (нечётный seqlock) — он не считается.
	off := kesShmHeader
	binary.LittleEndian.PutUint64(buf[off+8:off+16], 1)
	if got := countKesInShm(buf); got != 1 {
		t.Errorf("countKesInShm с нечётным seqlock = %d, want 1", got)
	}
}
