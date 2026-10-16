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

// chunkTransport отдаёт данные мелкими порциями (эмуляция serial-потока AntBMS,
// где кадры приходят раздробленно).
type chunkTransport struct {
	rb    []byte
	rp    int
	chunk int
	to    time.Duration
}

func (c *chunkTransport) Read(p []byte) (int, error) {
	if c.rp >= len(c.rb) {
		return 0, errTimeout
	}
	n := c.chunk
	if n <= 0 || n > len(p) {
		n = len(p)
	}
	if left := len(c.rb) - c.rp; n > left {
		n = left
	}
	copy(p, c.rb[c.rp:c.rp+n])
	c.rp += n
	return n, nil
}

func (c *chunkTransport) Write(p []byte) error           { return nil }
func (c *chunkTransport) SetReadTimeout(d time.Duration) { c.to = d }
func (c *chunkTransport) Close() error                   { return nil }
