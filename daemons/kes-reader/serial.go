package main

import (
	"time"

	"github.com/galiy/malina-26/daemons/lib/microart"
	"github.com/galiy/malina-26/daemons/lib/transport"
)

// Port — общий последовательный порт (см. lib/transport).
type Port = transport.Port

// microartRead — чтение страницы кадровым слоем MicroArt (ошибки → kesError).
func microartRead(p *Port, addr uint16, page byte, echoTO, respTO time.Duration) ([]byte, error) {
	data, err := microart.Read(p, addr, page, echoTO, respTO)
	if err != nil {
		return nil, trMicroart(err)
	}
	return data, nil
}

// microartWrite — запись страницы кадровым слоем MicroArt (ошибки → kesError).
func microartWrite(p *Port, addr uint16, data []byte, echoTO, respTO time.Duration) error {
	return trMicroart(microart.Write(p, addr, data, echoTO, respTO))
}
