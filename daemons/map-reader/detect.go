package main

import (
	"errors"
	"time"
)

// speeds — приоритетный порядок скоростей: 115200 первой.
// 38400 МАП не поддерживает (_LCD_RSBAUD: 2400/9600/19200/57600/115200).
var speeds = []int{115200, 19200, 57600, 9600}

// speedsModbus — включая нестандартные RS485 (_LCD_RSBAUD485: …/312500/625000).
var speedsModbus = []int{115200, 19200, 57600, 9600, 312500, 625000}

// probeDeadline — бюджет одной пробы (не держим порт дольше ~2 с).
const probeDeadline = 2 * time.Second

// detected — найденный на порту МАП (unit=0 для native).
type detected struct {
	unit int
	info *mapInfo
}

// probePort определяет, что на порту, перебирая протоколы и скорости.
// Приоритет: native Микроарт, затем Modbus (с перебором unit=1,2,3…).
// Порт остаётся настроен на найденную скорость.
func probePort(p *Port, cfg *config) (string, int, []detected, error) {
	// Проба всегда идёт в текущем (лёгком) режиме: предкадровый Flush
	// выбирается по модели уже после обнаружения.
	p.SetFlushBeforeFrame(false)
	start := time.Now()

	for _, baud := range speeds {
		if time.Since(start) > probeDeadline {
			return "", 0, nil, errNoMap
		}
		if p.SetBaud(baud) != nil {
			continue
		}
		p.Flush()
		if info, err := probeMicroart(p, baud, cfg.probeEcho, cfg.probeResponse); err == nil {
			return "microart", baud, []detected{{0, info}}, nil
		} else if errors.Is(err, errForeignMicroArt) {
			// Порт занят чужим MicroArt-устройством (например, КЭС/MPPT, _Device==5):
			// не МАП. Немедленно прекращаем пробу — другие боды и Modbus не трогаем,
			// порт надо быстро отпустить (см. scanner.setForeignBackoff).
			return "", 0, nil, errForeignMicroArt
		} else {
			cfg.debugf("probe %s: microart@%d: %v", p.Path(), baud, err)
		}
	}

	for _, baud := range speedsModbus {
		if time.Since(start) > probeDeadline {
			return "", 0, nil, errNoMap
		}
		if p.SetBaud(baud) != nil {
			continue
		}
		p.Flush()
		info, err := probeModbus(p, 1, cfg.probeResponse)
		if err != nil {
			cfg.debugf("probe %s: modbus@%d unit1: %v", p.Path(), baud, err)
			continue
		}
		dets := []detected{{1, info}}
		for unit := 2; unit <= 247; unit++ {
			if time.Since(start) > probeDeadline {
				break
			}
			uinfo, err := probeModbus(p, unit, cfg.probeResponse)
			if err != nil {
				break
			}
			dets = append(dets, detected{unit, uinfo})
		}
		return "modbus", baud, dets, nil
	}
	return "", 0, nil, errNoMap
}
