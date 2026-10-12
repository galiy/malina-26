package main

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/galiy/malina-26/daemons/lib/device"
	"github.com/galiy/malina-26/daemons/lib/microart"
)

// Размер образа КЭС: RAM/ID индекс (0..2047). Адрес «устройства» A отображается в
// индекс: A>=0x1F0 → A-0x1F0; иначе A (см. docs/kes-memory.md §3, «+0x1F0»).
const kesImageSize = 2048

// Модель КЭС 200/100: Uпанелей ≤ 200 В (типично 110..160), Iзаряда АКБ ≤ 100 А.
const (
	maxChgCur  = 100.0
	pvVTypMin  = 110.0
	pvVTypMax  = 160.0
	batVMin    = 48.0
	batVMax    = 56.5
	batVMean   = 52.8
	searchDur  = 15 * time.Second
	searchFall = 0.85
)

// Device — виртуальный КЭС (MPPT): модель регистров + «живая» физика.
type Device struct {
	mu       sync.Mutex
	id       int
	img      *MemImage // карта памяти (весь образ: EEPROM + RAM)
	uid      uint16
	fw       byte
	relayCmd uint16
	eewr     bool
	rnd      *rand.Rand
	vBat     float64
	cloud    float64
	tInt     float64
	tBat     float64
	pwrW     uint32
	lastTick time.Time
	nextSrch time.Time
	srchEnd  time.Time
	nextMpp  time.Time
	mpp      byte
	nextMode time.Time
	nextSign time.Time
	modeChar byte
	store    *Store
	idx      int
}

func newDevice(id int, uid uint16, fw byte, relayCmd uint16, store *Store, idx int) *Device {
	img := NewMemImage("")
	if store != nil {
		img = store.Image(idx)
	}
	d := &Device{
		id: id, img: img, uid: uid, fw: fw, relayCmd: relayCmd,
		rnd:  rand.New(rand.NewSource(int64(0x9E3779B9) ^ (int64(id) * int64(2654435761)))),
		vBat: batVMean, cloud: 1.0, tInt: 30, tBat: 24,
	}
	d.mu.Lock()
	d.store, d.idx = store, idx
	d.lastTick = time.Now()
	d.nextSrch = d.lastTick.Add(120*time.Second + time.Duration(d.rnd.Intn(180))*time.Second)
	d.nextMpp = d.lastTick.Add(rand4to15s(d.rnd))
	d.nextMode = d.lastTick.Add(time.Duration(4+d.rnd.Intn(12)) * time.Minute)
	d.nextSign = d.lastTick.Add(rand4to15s(d.rnd))
	d.mpp = 'R'
	d.modeChar = 'b'
	d.seedLocked()
	d.mu.Unlock()
	return d
}

func (d *Device) seedLocked() {
	// Карта памяти уже в образе (считана Store при открытии).
	// Константы (побеждают значения карты).
	d.img.Set(0x0000, device.TypeKes)
	d.img.Set(0x0001, 0x01)
	d.img.Set(0x0002, 0xA8)
	d.img.Set(0x0005, 0x32)
	d.img.Set(0x000C, 0x01)
	d.img.Set(0x000D, 0x01)
	d.img.Set(0x009B, 0x04)            // напряжение модели ×12 → 48 В
	d.img.Set(0x2C9, byte(d.uid&0xFF)) // серийник мл.
	d.img.Set(0x2CA, byte(d.uid>>8))   // серийник ст.
	// Ветрогенератора нет → скорость ветра н/д.
	d.ramSet(0x3F8, 0xFF)
	d.ramSet(0x3F9, 0xFF)
	d.tickLocked(d.lastTick)
}

func rand4to15s(r *rand.Rand) time.Duration {
	return time.Duration(4+r.Intn(12)) * time.Second
}

func (d *Device) Read(addr uint16, n int) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = d.get(addr + uint16(i))
	}
	return out
}

func (d *Device) get(addr uint16) byte { return d.img.At(devOffset(addr)) }

// ramAt/ramSet — доступ к RAM-ячейкам карты (RAM-адрес).
func (d *Device) ramAt(addr uint16) byte     { return d.img.At(ramOffset(addr)) }
func (d *Device) ramSet(addr uint16, v byte) { d.img.Set(ramOffset(addr), v) }

func (d *Device) Write(addr uint16, data []byte) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Исключение: команда реле — разрешена, храним только состояние (RAM 0x3E3);
	// командная ячейка не хранится и не читается.
	if addr == d.relayCmd && len(data) > 0 {
		d.ramSet(relayStateAddr, data[0])
		if d.store != nil {
			d.store.Save()
		}
		return true
	}
	// Запрещено: адрес 0, подтверждённые EEPROM, вся RAM
	if addr == 0x0000 || addr >= 0x400 || confirmedEEPROM[addr] || addr == 0x2C9 || addr == 0x2CA {
		return false
	}
	// Свободная EEPROM: пишем в карту памяти и сохраняем карту целиком
	d.img.Set(addr, data[0])
	if d.store != nil {
		d.store.Save()
	}
	return true
}

func (d *Device) Command(code byte) byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch code {
	case 3:
		d.eewr = true
		return 0
	case 6, 7, 8:
		d.eewr = false
		return 0
	default:
		return 0x04
	}
}

func (d *Device) SetRelay(n int, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n < 1 || n > 3 {
		return
	}
	bit := byte(1) << uint(n-1)
	if on {
		d.ramSet(0x3E3, d.ramAt(0x3E3)|bit)
	} else {
		d.ramSet(0x3E3, d.ramAt(0x3E3)&^bit)
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Tick — обновление «живой» модели (1 Гц).
func (d *Device) Tick(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastTick = now
	d.tickLocked(now)
	d.pwrW += uint32(math.Max(1, d.pOutLocked()/1000.0))
	d.ramSet16(0x39F, uint16(d.pwrW&0xFFFF))
	d.ramSet16(0x3A1, uint16((d.pwrW/1000)&0xFFFF))
}

func (d *Device) pOutLocked() float64 { return float64(d.ram16(0x2F9)) }

// ram16/ramSet16 — 16-битные RAM-ячейки карты (little-endian).
func (d *Device) ram16(addr uint16) uint16 {
	return uint16(d.ramAt(addr)) | uint16(d.ramAt(addr+1))<<8
}

func (d *Device) ramSet16(addr uint16, v uint16) {
	d.ramSet(addr, byte(v&0xFF))
	d.ramSet(addr+1, byte(v>>8))
}

// tickLocked пересчитывает регистры измерений по текущему времени.
func (d *Device) tickLocked(now time.Time) {
	t := float64(now.Unix()%86400) / 86400.0
	day := 0.5 - 0.5*math.Cos(2*math.Pi*(t-0.25))
	if day < 0.20 {
		day = 0.20
	}
	d.cloud += (d.rnd.Float64()*2-1)*0.02 + (1.0-d.cloud)*0.05
	d.cloud = clamp(d.cloud, 0.9, 1.1)
	sun := clamp(day*d.cloud, 0.05, 1.0)

	scale := 1.0
	mode := d.modeChar
	if now.After(d.nextSrch) {
		d.srchEnd = now.Add(searchDur)
		d.nextSrch = now.Add(120*time.Second + time.Duration(d.rnd.Intn(180))*time.Second)
	}
	if now.Before(d.srchEnd) {
		p := 1 - float64(d.srchEnd.Sub(now))/float64(searchDur)
		scale = 1 - searchFall*(1-math.Abs(2*p-1))
		mode = 'S'
	}
	if scale < 0.15 {
		scale = 0.15
	}

	pvV := pvVTypMin + (pvVTypMax-pvVTypMin)*sun
	if mode == 'S' {
		pvV = pvVTypMin + (pvV-pvVTypMin)*0.5
	}
	iCh := clamp(maxChgCur*sun*scale+d.rnd.NormFloat64()*1.5, 0, maxChgCur)
	iPv := iCh * d.vBat / pvV

	pPV := pvV * iPv
	pChg := d.vBat * iCh
	pOut := pChg * 0.98

	d.vBat += (iCh-45)*0.0008 + (batVMean-d.vBat)*0.004 + d.rnd.NormFloat64()*0.001
	d.vBat = clamp(d.vBat, batVMin, batVMax)

	tBatT := 5 + 30*(iCh/maxChgCur)
	tIntT := 20 + 25*(iCh/maxChgCur)
	d.tBat += (tBatT - d.tBat) * 0.02
	d.tInt += (tIntT - d.tInt) * 0.02

	put16 := func(addr uint16, v uint16) { d.ramSet16(addr, v) }
	put16(0x2D9, uint16(pvV*100+0.5))
	put16(0x2DF, uint16(iPv*100+0.5))
	put16(0x2E5, uint16(d.vBat*100+0.5))
	put16(0x2EB, uint16(pPV+0.5))
	put16(0x2ED, uint16(pOut+0.5))
	put16(0x2F5, 0)
	put16(0x2F9, uint16(pChg+0.5))
	put16(0x2FB, uint16(iCh*100+0.5))
	put16(0x2FF, uint16(iCh*100+0.5))
	d.ramSet(0x31D, byte(int8(d.tInt+0.5))) // °C как есть (знаковый)
	d.ramSet(0x31E, byte(int8(d.tBat+0.5))) // °C как есть (знаковый)
	d.ramSet(0x2C2, mode)                   // режим заряда: 'i'/'v'/'B'/'b'/'S'
	// Mode B/b — смена раз в 4..15 минут (кроме сканирования 'S')
	if now.After(d.nextMode) {
		d.nextMode = now.Add(time.Duration(4+d.rnd.Intn(12)) * time.Minute)
		if d.modeChar == 'b' {
			d.modeChar = 'B'
		} else {
			d.modeChar = 'b'
		}
	}
	// Избыток/Нехватка: '+/-', смена раз в 4..15 c; значение — по V_Bat (>53 → избыток '-')
	if now.After(d.nextSign) {
		d.nextSign = now.Add(rand4to15s(d.rnd))
		if d.vBat > 53 {
			d.ramSet(0x37A, '-')
		} else {
			d.ramSet(0x37A, '+')
		}
	}
	// РТ относительно ТММ: 'L'/'R' меняются раз в 2..5 c, '0' — редко
	if now.After(d.nextMpp) {
		d.nextMpp = now.Add(rand4to15s(d.rnd))
		if d.rnd.Intn(100) < 2 {
			d.mpp = '0'
		} else if d.mpp == 'R' {
			d.mpp = 'L'
		} else {
			d.mpp = 'R'
		}
	}
	d.ramSet(0x379, d.mpp)
}

// handle — обработка разобранного запроса → кадр ответа (сторона устройства).
func (d *Device) handle(req microart.Request) []byte {
	switch req.Cmd {
	case 'r':
		return microart.BuildReply(d.Read(req.Addr, int(req.Page)+1))
	case 'w':
		if req.Addr == 0 && len(req.Data) == 1 && req.Data[0] >= 1 && req.Data[0] <= 9 {
			if code := d.Command(req.Data[0]); code != 0 {
				return microart.BuildErrorReply(code)
			}
			return microart.BuildReply(nil)
		}
		if !d.Write(req.Addr, req.Data) {
			return microart.BuildErrorReply(0x20) // запись запрещена
		}
		return microart.BuildReply(nil)
	default:
		return microart.BuildErrorReply(0x04)
	}
}
