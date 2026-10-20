package solarman

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// Хвост кадра может прийти позже ReadTimeout. После первого байта работает
// IdleWindow, а общий лимит MaxTotal не должен обрезать уже начавшийся кадр.
func TestExchangeIdleWindowBeyondReadTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	frame := buildResp(1, []uint16{1, 2, 3}, false)
	req := BuildDeyeReadFrameFn(1, 1, 1, 0, 0x28, 0x03)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, len(req))
		if _, err := io.ReadFull(conn, buf); err != nil {
			done <- err
			return
		}
		// Первый кусок — в пределах ReadTimeout (200 мс).
		time.Sleep(60 * time.Millisecond)
		if _, err := conn.Write(frame[:len(frame)/2]); err != nil {
			done <- err
			return
		}
		// Второй — уже после ReadTimeout, но в пределах MaxTotal.
		time.Sleep(270 * time.Millisecond)
		_, err = conn.Write(frame[len(frame)/2:])
		done <- err
	}()

	cl := &Client{
		Address:     ln.Addr().String(),
		DeviceSN:    1,
		Timeout:     time.Second,
		DialTimeout: time.Second,
		ReadTimeout: 200 * time.Millisecond,
		IdleWindow:  700 * time.Millisecond,
		MaxTotal:    time.Second,
	}
	frames, err := cl.Exchange(context.Background(), req)
	if serr := <-done; serr != nil {
		t.Fatalf("server: %v", serr)
	}
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(frames) != 1 || !frames[0].Valid {
		t.Fatalf("frames: %+v", frames)
	}
}

// Первый байт позже ReadTimeout (но в пределах MaxTotal) — это no data:
// ReadTimeout ограничивает именно ожидание первого байта.
func TestExchangeFirstByteBeyondReadTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	frame := buildResp(1, []uint16{1, 2, 3}, false)
	req := BuildDeyeReadFrameFn(1, 1, 1, 0, 0x28, 0x03)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, len(req))
		if _, err := io.ReadFull(conn, buf); err != nil {
			done <- err
			return
		}
		time.Sleep(300 * time.Millisecond)
		_, err = conn.Write(frame)
		done <- err
	}()

	cl := &Client{
		Address:     ln.Addr().String(),
		DeviceSN:    1,
		Timeout:     time.Second,
		DialTimeout: time.Second,
		ReadTimeout: 200 * time.Millisecond,
		IdleWindow:  400 * time.Millisecond,
		MaxTotal:    time.Second,
	}
	_, err = cl.Exchange(context.Background(), req)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("ожидался ErrNoData, получено %v", err)
	}
	<-done // серверная горутина не должна висеть
}
