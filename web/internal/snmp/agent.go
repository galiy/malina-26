package snmp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Handler отдаёт значения по OID для запросов агента.
//
// Get — точечное значение OID (GET): ok=false, если объект отсутствует
// (→ noSuchInstance). Next — первый OID строго больше запрошенного (GETNEXT/WALK):
// ok=false за последним объектом (→ endOfMibView).
type Handler interface {
	Get(oid []int) (Value, bool)
	Next(oid []int) (next []int, v Value, ok bool)
}

// Agent — UDP-слушатель SNMP-агента (v2c). Stateless: каждый запрос читает
// актуальный снимок у Handler; никакого состояния между запросами не хранится.
type Agent struct {
	addr      string
	community string
	handler   Handler

	mu   sync.Mutex
	conn *net.UDPConn
	wg   sync.WaitGroup
}

// NewAgent создаёт агент (сокет будет открыт в Serve/Start).
func NewAgent(addr, community string, h Handler) *Agent {
	return &Agent{addr: addr, community: community, handler: h}
}

// Start открывает UDP-сокет и запускает цикл обработки в фоне.
func (a *Agent) Start() error {
	ua, err := net.ResolveUDPAddr("udp", a.addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	a.wg.Add(1)
	go a.serveLoop(conn)
	return nil
}

// Run — блокирующий цикл (для тестов/одноразового запуска).
func (a *Agent) Run(ctx context.Context) error {
	if err := a.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	a.Close()
	return nil
}

// Close закрывает сокет и ожидает завершения цикла.
func (a *Agent) Close() {
	a.mu.Lock()
	c := a.conn
	a.conn = nil
	a.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
	a.wg.Wait()
}

func (a *Agent) serveLoop(conn *net.UDPConn) {
	defer a.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, raddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// сокет закрыт — выходим
			return
		}
		resp := a.handlePacket(buf[:n])
		if resp == nil {
			continue
		}
		_, _ = conn.WriteToUDP(resp, raddr)
	}
}

// handlePacket разбирает запрос и возвращает ответ (или nil — игнор).
func (a *Agent) handlePacket(pkt []byte) []byte {
	msg, err := DecodeMessage(pkt)
	if err != nil {
		return nil
	}
	if msg.Version != Version2c || msg.PDU == nil || msg.Community != a.community {
		return nil
	}
	pd := msg.PDU
	switch pd.Type {
	case PDUGetRequest, PDUGetNextRequest:
		resp := &PDU{
			Type:        PDUGetResponse,
			RequestID:   pd.RequestID,
			ErrorStatus: 0,
			ErrorIndex:  0,
		}
		if pd.Type == PDUGetRequest {
			for _, vb := range pd.Varbinds {
				if v, ok := a.handler.Get(vb.OID); ok {
					resp.Varbinds = append(resp.Varbinds, Varbind{OID: vb.OID, Value: EncodeValue(v)})
				} else {
					resp.Varbinds = append(resp.Varbinds, Varbind{OID: vb.OID, Value: EncodeValue(ExceptionValue(ExcepNoSuchInstance))})
				}
			}
		} else {
			for _, vb := range pd.Varbinds {
				next, v, ok := a.handler.Next(vb.OID)
				if !ok {
					resp.Varbinds = append(resp.Varbinds, Varbind{OID: vb.OID, Value: EncodeValue(ExceptionValue(ExcepEndOfMibView))})
					continue
				}
				resp.Varbinds = append(resp.Varbinds, Varbind{OID: next, Value: EncodeValue(v)})
			}
		}
		return EncodeMessage(&Message{Version: Version2c, Community: a.community, PDU: resp})
	default:
		return nil
	}
}

// SendTrap отправляет v2c-trap на приёмник. uptimeMillis — аптайм устройства в
// миллисекундах (кладётся в sysUpTime). Возвращает ошибку отправки.
func SendTrap(addr, community string, trapOID []int, uptimeMillis int64, extras []Varbind) error {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("snmp: приёмник trap: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, ua)
	if err != nil {
		return fmt.Errorf("snmp: dial trap: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	pd := &PDU{
		Type:      PDUTrapV2,
		RequestID: int(time.Now().UnixNano() & 0x7FFFFFFF),
		Varbinds:  TrapVarbinds(uptimeMillis, trapOID, extras...),
	}
	pkt := EncodeMessage(&Message{Version: Version2c, Community: community, PDU: pd})
	_, err = conn.Write(pkt)
	return err
}
