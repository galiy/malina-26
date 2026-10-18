// Package control — общий каркас синхронного управления демона через Unix
// domain socket (запрос — одна JSON-строка, ответ — одна JSON-строка).
//
// Демон передаёт обработчик: он получает строку запроса (без '\n') и возвращает
// значение для JSON-сериализации в ответ. Каркас отвечает за листенер, лимиты
// параллельных соединений, дедлайны чтения/записи и запись ответа.
package control

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// Serve обслуживает Unix-сокет по пути sockPath до сигнала stop.
// handler(reqLine) возвращает значение ответа; nil — «bad request».
func Serve(sockPath string, stop <-chan struct{}, handler func(reqLine []byte) any) {
	ServeWG(sockPath, stop, nil, handler)
}

// ServeWG — Serve с учётом in-flight обработчиков: wg.Add(1) выполняется до
// запуска горутины, wg.Done — после обработки. nil — без учёта.
func ServeWG(sockPath string, stop <-chan struct{}, wg *sync.WaitGroup, handler func(reqLine []byte) any) {
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		log.Printf("control socket: %v (команды отключены)", err)
		return
	}
	defer ln.Close()
	_ = os.Chmod(sockPath, 0o660)
	log.Printf("control socket: %s", sockPath)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stop:
			ln.Close()
		case <-done:
		}
	}()

	sem := make(chan struct{}, 16) // ограничение параллельных соединений
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case sem <- struct{}{}:
			if wg != nil {
				wg.Add(1)
			}
			go func() {
				defer func() { <-sem }()
				if wg != nil {
					defer wg.Done()
				}
				handle(conn, handler)
			}()
		default:
			conn.Close() // перегрузка — не копим горутины
		}
	}
}

func handle(conn net.Conn, handler func([]byte) any) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var resp any
	if handler == nil {
		resp = map[string]any{"ok": false, "code": "bad_request", "error": "bad request"}
	} else {
		resp = handler(line)
	}
	// Дедлайн на запись ответа ставим после обработки: команда записи в устройство
	// может выполняться заметно дольше 3 с (несколько операций × попытки).
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	WriteJSON(conn, resp)
}

// WriteJSON сериализует v и пишет одной строкой с '\n'.
func WriteJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	_, _ = conn.Write(b)
}
