package main

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/transport"
)

// fakeSerialPort отдаёт заранее заданные пачки и считает вызовы Read.
type fakeSerialPort struct {
	chunks   [][]byte
	err      error
	calls    int
	deadline time.Time
}

func (f *fakeSerialPort) Read(p []byte, deadline time.Time) (int, error) {
	f.calls++
	f.deadline = deadline
	if len(f.chunks) == 0 {
		if f.err != nil {
			return 0, f.err
		}
		return 0, transport.ErrTimeout
	}
	ch := f.chunks[0]
	n := copy(p, ch)
	if n < len(ch) {
		f.chunks[0] = ch[n:]
	} else {
		f.chunks = f.chunks[1:]
	}
	return n, nil
}

func (f *fakeSerialPort) WriteAll(p []byte) error { return nil }
func (f *fakeSerialPort) Close()                  {}

// TestSerialReadBatch — один вызов Port.Read возвращает пачку, а не один байт;
// после исчерпания пачек — errTimeout.
func TestSerialReadBatch(t *testing.T) {
	fp := &fakeSerialPort{chunks: [][]byte{[]byte("abcdef"), []byte("gh")}}
	st := &serialTransport{p: fp, to: 100 * time.Millisecond}

	buf := make([]byte, 16)
	n, err := st.Read(buf)
	if err != nil || n != 6 || string(buf[:n]) != "abcdef" {
		t.Fatalf("первая пачка: n=%d %q err=%v", n, buf[:n], err)
	}
	if fp.calls != 1 {
		t.Fatalf("Read вызван %d раз, ожидался 1 (пакетно)", fp.calls)
	}
	if !fp.deadline.After(time.Now()) {
		t.Fatalf("дедлайн не проброшен: %v", fp.deadline)
	}
	n, err = st.Read(buf)
	if err != nil || n != 2 || string(buf[:n]) != "gh" {
		t.Fatalf("вторая пачка: n=%d %q err=%v", n, buf[:n], err)
	}
	_, err = st.Read(buf)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("ожидали errTimeout, получили %v", err)
	}
}

// TestSerialReadErrorPassThrough — не-таймаутная ошибка (EOF) не должна
// превращаться в errTimeout.
func TestSerialReadErrorPassThrough(t *testing.T) {
	fp := &fakeSerialPort{err: io.EOF}
	st := &serialTransport{p: fp, to: 100 * time.Millisecond}
	_, err := st.Read(make([]byte, 8))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ожидали io.EOF, получили %v", err)
	}
}

// TestSerialReadPartialBuffer — остаток чанка не теряется при коротком буфере.
func TestSerialReadPartialBuffer(t *testing.T) {
	fp := &fakeSerialPort{chunks: [][]byte{[]byte("abcdef")}}
	st := &serialTransport{p: fp, to: 100 * time.Millisecond}
	buf := make([]byte, 4)
	n, err := st.Read(buf)
	if err != nil || n != 4 || string(buf[:n]) != "abcd" {
		t.Fatalf("первая часть: n=%d %q err=%v", n, buf[:n], err)
	}
	n, err = st.Read(buf)
	if err != nil || n != 2 || string(buf[:n]) != "ef" {
		t.Fatalf("остаток: n=%d %q err=%v", n, buf[:n], err)
	}
}
