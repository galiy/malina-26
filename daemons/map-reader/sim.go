package main

import (
	"math/rand"
	"os"
)

// ВНИМАНИЕ: ВРЕМЕННЫЙ код эмуляции КЭС/BMS для проверки UI на стенде.
// Включается ТОЛЬКО при MALINA_SIM_KES_BMS=1 (на устройстве переменная не
// задана — реальные данные). Каждый вызов (раз в цикл опроса) генерирует
// НОВЫЕ случайные значения: 3 КЭС (малые токи), 16 банок BMS (~3.38..3.46 В).

func simInjectKesBms(mem memMap) {
	if os.Getenv("MALINA_SIM_KES_BMS") != "1" {
		return
	}
	putB := func(addr int, v byte) { mem[addr] = v }

	// Гейты и количество
	putB(0x156, 3)  // LCD_BMSFunk: BMS + MPPT
	putB(0x100, 16) // BMS_Num: 16 банок
	putB(0x157, 3)  // LCD_MPPTNum: 3 КЭС

	// КЭС: 3 шт, ток плавает 0.5..3.0 А (raw = A*16), темп 20..30 °C
	for i := 0; i < 3; i++ {
		raw := 8 + rand.Intn(41) // 8..48
		putB(0x530+2*i, byte(raw&0xff))
		putB(0x531+2*i, byte(raw>>8))
		putB(0x560+i, byte(70+rand.Intn(11))) // Takb_MPPT
	}

	// BMS: 16 банок, напряжения 3.38..3.46 В вразнобой, свежие каждый цикл
	for i := 0; i < 16; i++ {
		raw := 338 + rand.Intn(9) // 338..346 (3.38..3.46 В)
		putB(0x480+2*i, byte(raw&0xff))
		putB(0x481+2*i, byte(raw>>8))
		putB(0x4C0+i, byte(70+rand.Intn(11))) // TakbCell: 20..30 °C
		putB(0x4E0+i, byte(30+rand.Intn(41))) // Q_Cell: 30..70
	}
}
