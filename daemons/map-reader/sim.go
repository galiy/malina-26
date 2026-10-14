package main

import (
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"sync"
	"syscall"
	"time"
)

// ВРЕМЕННЫЙ код эмуляции КЭС/BMS для проверки UI на стенде. Включается ТОЛЬКО при
// MALINA_SIM_KES_BMS=1. map-reader сам ничего не считает:
//   - КЭС берутся из shm ведущего эмулятора kes-emu (/dev/shm/malina-kes-state);
//     нет сегмента — ничего не эмулируем;
//   - BMS строится от РЕАЛЬНЫХ полей АКБ МАП: напряжение пакета = UAcc_med/10,
//     температура ячеек ≈ Temp_Grad0, ток ячеек (одинаковый) = Σтока КЭС − ток АКБ МАП,
//     ячейки (16) плавно и слабо девиируют друг относительно друга.

// Свой (отдельный) сегмент kes-emu; читает только встроенный эмулятор здесь.
const kesEmuShmPath = "/dev/shm/malina-kes-emu"

const (
	emuMagic = 0x554D454B // "KEMU"
	emuHdr   = 16
	emuRec   = 4 // i16 ток (A×16) + i16 температура (°C)
	emuMaxCh = 32
	emuSize  = emuHdr + emuMaxCh*emuRec
)

var (
	simMu  sync.Mutex
	simRnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	simOff []float64 // per-cell дрейф (В), очень медленный/малый
)

func simInjectKesBms(mem memMap) {
	if os.Getenv("MALINA_SIM_KES_BMS") != "1" {
		return
	}
	chans, ok := readKesChannels()
	if !ok {
		return // нет shm kes-emu — не эмулируем
	}
	var sumKes float64
	for _, c := range chans {
		sumKes += c.i
	}

	vPack := 0.0 // напряжение пакета = напряжение АКБ МАП
	if vh, ok1 := mem[0x405]; ok1 {
		if vl, ok2 := mem[0x406]; ok2 {
			vPack = float64(uint(vh)<<8|uint(vl)) / 10
		}
	}
	tBat := 25.0 // температура ячеек ≈ температура АКБ МАП
	if g0, ok := mem[0x42E]; ok {
		tBat = float64(int(g0)) - 50
	}
	iAcc := 0.0 // ток АКБ МАП (со знаком: заряд «+», иначе «−»)
	if l, ok1 := mem[0x432]; ok1 {
		if h, ok2 := mem[0x433]; ok2 {
			u := uint(l) | uint(h)<<8
			iAcc = float64(u) / 16
			if !(modeRAM(mem) > 3) {
				iAcc = -iAcc
			}
		}
	}

	simMu.Lock()
	defer simMu.Unlock()
	if simOff == nil {
		simOff = make([]float64, 16)
		for i := range simOff {
			simOff[i] = (simRnd.Float64()*2 - 1) * 0.004
		}
	}
	for i := range simOff { // очень медленный, малый дрейф + слабый возврат к среднему
		simOff[i] += (simRnd.Float64()*2 - 1) * 0.0003
		simOff[i] *= 0.999
		simOff[i] = clampF(simOff[i], -0.008, 0.008)
	}
	// Ток ячеек (одинаковый для всех) = заряд от КЭС + ток АКБ МАП (знаковый:
	// заряд «+», разряд «−»). Т.е. то, что забирает МАП, уменьшает заряд в батарею.
	iCell := sumKes + iAcc

	putB := func(addr int, v byte) { mem[addr] = v }
	put16 := func(addr int, v uint16) { mem[addr] = byte(v & 0xff); mem[addr+1] = byte(v >> 8) }

	putB(0x156, 3) // LCD_BMSFunk: BMS + MPPT
	putB(0x100, 16)
	putB(0x157, byte(len(chans)))
	// КЭС-каналы (MPPTs панели МАП): из shm kes-emu (A×16), Takb_MPPT (°C+50).
	for i, c := range chans {
		if i >= 16 {
			break
		}
		put16(0x530+2*i, uint16(clampU(math.Abs(c.i)*16))&0x7FFF) // панель знак маскирует
		putB(0x560+i, byte(int8(c.t+0.5)+50))
	}
	for i := 0; i < 16; i++ {
		u := vPack/16 + simOff[i]
		put16(0x480+2*i, uint16(clampU(u*100))&0x7FFF)
		putB(0x4C0+i, byte(clampI(tBat+simOff[i]*20+50)))
		putB(0x4E0+i, byte(clampI(iCell))) // Q_Cell — ток ячейки, А
	}
}

func modeRAM(mem memMap) int {
	if v, ok := mem[0x400]; ok {
		return int(v)
	}
	return 0
}

type kesChan struct{ i, t float64 } // ток (А) и температура (°C) канала КЭС

// readKesChannels читает shm kes-emu (только чтение) и возвращает каналы КЭС
// (ток A×16 → A, TempBat °C). ok=false, если сегмента нет/невалиден.
func readKesChannels() ([]kesChan, bool) {
	f, err := os.OpenFile(kesEmuShmPath, os.O_RDONLY, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < int64(emuSize) {
		return nil, false
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, emuSize, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, false
	}
	defer syscall.Munmap(data)
	if binary.LittleEndian.Uint32(data[0:4]) != emuMagic {
		return nil, false
	}
	n := int(binary.LittleEndian.Uint32(data[8:12]))
	if n > emuMaxCh {
		n = emuMaxCh
	}
	out := make([]kesChan, 0, n)
	for i := 0; i < n; i++ {
		off := emuHdr + i*emuRec
		ikb := float64(int16(binary.LittleEndian.Uint16(data[off:]))) / 16 // ток АКБ, A
		t := float64(int16(binary.LittleEndian.Uint16(data[off+2:])))      // °C
		out = append(out, kesChan{i: ikb, t: t})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func clampF(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func clampU(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 65535 {
		return 65535
	}
	return x
}

func clampI(x float64) int {
	v := int(x) // вниз (Q_Cell целое, не завышаем)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
