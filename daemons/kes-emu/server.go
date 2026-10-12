package main

import (
	"io"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/galiy/malina-26/daemons/lib/microart"
)

// wire — необязательный дамп обмена (hex) для разведки протокола клиента.
var wire *os.File

func wiref(dir byte, b byte) {
	if wire == nil {
		return
	}
	_, _ = wire.WriteString(string([]byte{dir}) + " " + hexByte(b) + "\n")
}

func hexByte(b byte) string {
	const h = "0123456789abcdef"
	return string([]byte{h[b>>4], h[b&0xf]})
}

// serveDevice обслуживает один виртуальный КЭС на master-стороне PTY.
// Каждый принятый байт запроса эхируется клиенту (как ждёт протокол Микроарт),
// а эхо собственного ответа со стороны клиента игнорируется.
func serveDevice(d *Device, master io.ReadWriteCloser, stop <-chan struct{}) {
	go func() {
		<-stop
		master.Close()
	}()

	buf := make([]byte, 1)
	frame := make([]byte, 0, 64)
	// sent — хвост последнего ответа, чьё эхо может прислать клиент. Сверяем побайтно:
	// если пришёл ожидаемый байт — это эхо (игнор), иначе это новый запрос. Так работает
	// и с клиентом, который НЕ эхует ответы (legacy mpptd), без рассинхрона счётчика.
	var sent []byte
	for {
		n, err := master.Read(buf)
		if n == 1 {
			b := buf[0]
			if len(sent) > 0 && b == sent[0] {
				sent = sent[1:]
				wiref('e', b)
				continue
			}
			sent = nil
			wiref('R', b)
			_, _ = master.Write([]byte{b}) // эхо запроса
			if b == 0x0A {
				req, perr := microart.ParseRequestFrame(frame)
				frame = frame[:0]
				var reply []byte
				if perr != nil {
					reply = microart.BuildErrorReply(0x01) // ErrRS_sum
				} else {
					reply = d.handle(req)
				}
				for _, rb := range reply {
					wiref('T', rb)
				}
				if _, werr := master.Write(reply); werr != nil {
					return
				}
				sent = reply
				continue
			}
			if len(frame) < 512 {
				frame = append(frame, b)
			} else {
				frame = frame[:0] // защита от мусора
			}
		}
		if err != nil {
			// Нет открытого slave → master отдаёт EIO/EOF: это не смерть, ждём клиента.
			if err == io.EOF || err == syscall.EIO {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return
		}
	}
}

func logf(format string, a ...any) { log.Printf(format, a...) }
