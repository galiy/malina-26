package main

import "time"

// fakeTransport — тестовый транспорт: сценарий чтения + запись в буфер.
type fakeTransport struct {
	rb []byte
	rp int
	wb []byte
	to time.Duration
}

func (f *fakeTransport) Read(p []byte) (int, error) {
	if f.rp >= len(f.rb) {
		return 0, errTimeout
	}
	n := copy(p, f.rb[f.rp:])
	f.rp += n
	return n, nil
}

func (f *fakeTransport) Write(p []byte) error {
	f.wb = append(f.wb, p...)
	return nil
}

func (f *fakeTransport) SetReadTimeout(d time.Duration) { f.to = d }
func (f *fakeTransport) Close() error                   { return nil }
