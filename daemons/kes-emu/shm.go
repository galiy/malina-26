package main

import (
	"encoding/binary"
	"os"
	"syscall"
)

// kes-emu — ведущий эмулятор: пишет телеметрию КЭС в СВОЙ shm-сегмент (не общий
// с kes-reader). Читает его только встроенный эмулятор в map-reader.
//
// Формат (простой):
//
//	[magic u32 "KEMU"][version u32=1][count u32][seq u32]
//	[ { I i16 (A×16), T i16 (°C) } × 32 ]
const kesEmuShmPath = "/dev/shm/malina-kes-emu"

const (
	emuMagic   = 0x554D454B // "KEMU"
	emuVersion = 1
	emuMaxCh   = 32
	emuHdr     = 16
	emuRec     = 4 // i16 ток + i16 температура
)

const emuSize = emuHdr + emuMaxCh*emuRec

type emuChan struct{ I, T float64 }

type emuShm struct{ data []byte }

func openEmuShm() (*emuShm, error) {
	f, err := os.OpenFile(kesEmuShmPath, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(emuSize); err != nil {
		f.Close()
		return nil, err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, emuSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}
	binary.LittleEndian.PutUint32(data[0:], emuMagic)
	binary.LittleEndian.PutUint32(data[4:], emuVersion)
	return &emuShm{data: data}, nil
}

func (e *emuShm) publish(chans []emuChan) {
	n := len(chans)
	if n > emuMaxCh {
		n = emuMaxCh
	}
	for i := 0; i < n; i++ {
		off := emuHdr + i*emuRec
		binary.LittleEndian.PutUint16(e.data[off:], uint16(i16(chans[i].I*16)))
		binary.LittleEndian.PutUint16(e.data[off+2:], uint16(i16(chans[i].T+0.5)))
	}
	binary.LittleEndian.PutUint32(e.data[8:], uint32(n))
	binary.LittleEndian.PutUint32(e.data[12:], binary.LittleEndian.Uint32(e.data[12:])+1) // seq
}

func i16(v float64) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32767 {
		return -32767
	}
	return int16(v)
}
