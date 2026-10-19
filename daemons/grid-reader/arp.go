// Классификация доступности инвертора по таблице соседей (ARP).
//
// После неудачного TCP-connect ядро уже попыталось разрешить MAC (connect
// инициирует ARP), поэтому отдельный arping не нужен: достаточно прочитать
// netlink-таблицу соседей (RTM_GETNEIGH), как это делает `ip neigh`.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// NDA_* — типы атрибутов struct ndmsg (linux/neighbour.h).
const (
	ndaDst     = 1
	ndaLladdr  = 2
	nudReach   = 0x02 // NUD_REACHABLE
	ndmsgSize  = 12   // sizeof(struct ndmsg) без атрибутов
	attrHdrLen = 4
)

// neighborChecker проверяет, подтверждён ли MAC/IP как живой на L2.
// Возвращает true только для NUD_REACHABLE с ненулевым MAC.
type neighborChecker func(ctx context.Context, ip net.IP) (bool, error)

// neighborReachable читает netlink-таблицу соседей и сообщает, есть ли для ip
// запись NUD_REACHABLE с действительным MAC. Остальные состояния
// (INCOMPLETE/FAILED/STALE/DELAY/PROBE) и отсутствие записи — не подтверждены.
func neighborReachable(ctx context.Context, ip net.IP) (bool, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return false, errors.New("ARP: не IPv4")
	}
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return false, fmt.Errorf("ARP: socket: %w", err)
	}
	defer syscall.Close(fd)
	syscall.CloseOnExec(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return false, fmt.Errorf("ARP: bind: %w", err)
	}
	// Ограничиваем ожидание дампа: зависший netlink не должен клинить опрос.
	tv := syscall.NsecToTimeval(int64(100 * time.Millisecond))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return false, fmt.Errorf("ARP: setsockopt: %w", err)
	}

	req := make([]byte, syscall.NLMSG_HDRLEN+syscall.SizeofRtMsg)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))     // nlmsg_len
	binary.LittleEndian.PutUint16(req[4:6], syscall.RTM_GETNEIGH) // nlmsg_type
	binary.LittleEndian.PutUint16(req[6:8], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
	binary.LittleEndian.PutUint32(req[8:12], 1) // nlmsg_seq
	req[syscall.NLMSG_HDRLEN] = syscall.AF_INET // rtmsg.rtm_family
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return false, fmt.Errorf("ARP: send: %w", err)
	}

	buf := make([]byte, 64*1024)
	// Аварийный предохранитель: обычный дамп приходит мгновенно, а ctx
	// прерывает ожидание. Большой запас исключает ложный «нет на L2».
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return false, errors.New("ARP: таймаут чтения таблицы")
		}
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			return false, fmt.Errorf("ARP: recv: %w", err)
		}
		msgs, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return false, fmt.Errorf("ARP: parse: %w", err)
		}
		reachable, done, err := neighborBatchResult(msgs, ip4)
		if err != nil {
			return false, err
		}
		if reachable {
			return true, nil
		}
		if done {
			return false, nil
		}
	}
}

// neighborBatchResult разбирает один батч netlink-дампа. DONE не прерывает
// сканирование: в одном skb с ним могут прийти записи о соседях.
func neighborBatchResult(msgs []syscall.NetlinkMessage, ip4 net.IP) (reachable, done bool, err error) {
	for _, m := range msgs {
		switch m.Header.Type {
		case syscall.NLMSG_DONE:
			done = true
		case syscall.NLMSG_ERROR:
			if len(m.Data) >= 4 {
				if e := int32(binary.LittleEndian.Uint32(m.Data[:4])); e != 0 {
					return false, false, fmt.Errorf("ARP: netlink: %w", syscall.Errno(-e))
				}
			}
		}
	}
	return neighborMessagesReachable(msgs, ip4), done, nil
}

// neighborMessagesReachable ищет среди сообщений дампа запись о нужном IP.
func neighborMessagesReachable(msgs []syscall.NetlinkMessage, ip4 net.IP) bool {
	for _, m := range msgs {
		if m.Header.Type == syscall.RTM_NEWNEIGH && neighborMsgReachable(m.Data, ip4) {
			return true
		}
	}
	return false
}

// neighborMsgReachable разбирает одно сообщение RTM_NEWNEIGH: struct ndmsg +
// rtattr-атрибуты NDA_DST/NDA_LLADDR.
func neighborMsgReachable(data []byte, ip4 net.IP) bool {
	if len(data) < ndmsgSize {
		return false
	}
	state := binary.LittleEndian.Uint16(data[8:10])
	if state != nudReach {
		return false
	}
	attrs := data[ndmsgSize:]
	var dst, lladdr []byte
	for len(attrs) >= attrHdrLen {
		alen := int(binary.LittleEndian.Uint16(attrs[0:2]))
		atype := binary.LittleEndian.Uint16(attrs[2:4])
		if alen < attrHdrLen || alen > len(attrs) {
			break
		}
		val := attrs[attrHdrLen:alen]
		switch atype {
		case ndaDst:
			dst = val
		case ndaLladdr:
			lladdr = val
		}
		// rtattr выровнен по 4 байтам.
		next := (alen + 3) &^ 3
		if next > len(attrs) {
			break
		}
		attrs = attrs[next:]
	}
	if len(dst) != net.IPv4len || !net.IP(dst).Equal(ip4) {
		return false
	}
	return len(lladdr) >= 6 && !allZero(lladdr)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
