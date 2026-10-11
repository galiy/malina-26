package main

import "github.com/galiy/malina-26/daemons/lib/transport"

// Port — общий последовательный порт (см. lib/transport).
type Port = transport.Port

// openPort открывает порт (общий транспорт).
func openPort(path string) (*Port, error) {
	return transport.Open(path)
}
