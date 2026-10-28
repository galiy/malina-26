package main

import (
	"fmt"
	"log"
	"time"
)

// Ячейки времени МАП: _LCD_TimeCyr (EEPROM) и _TimeCyr_MINUT (RAM).
const (
	cellTimeCyr = 0x1B6 // старшие 5 бит — часы, младшие 3 — десятки минут
	cellTimeMin = 0x44B // единицы минут (0..9)
)

// tenMinWindow возвращает идентификатор «круглой» 10-минутной точки и признак,
// что текущий момент попадает в окно w секунд ПОСЛЕ неё: [XX:X0:00 .. XX:X0:0w].
// Обязательна «круглая» минута (minute%10==0) — иначе окно открывалось бы в
// первые секунды ЛЮБОЙ минуты. До точки окно не открывается.
func tenMinWindow(now time.Time, w time.Duration) (int64, bool) {
	if now.Minute()%10 != 0 || now.Second() > int(w.Seconds()) {
		return 0, false
	}
	y, mo, d := now.Date()
	loc := now.Location()
	block := now.Minute() - now.Minute()%10
	boundary := time.Date(y, mo, d, now.Hour(), block, 0, 0, loc).Unix()
	return boundary, true
}

// decodeMapTime читает время МАП (минуты от полуночи) из прочитанной карты.
func decodeMapTime(mem memMap) (int, bool) {
	tc, ok1 := mem[cellTimeCyr]
	tm, ok2 := mem[cellTimeMin]
	if !ok1 || !ok2 {
		return 0, false
	}
	h := int(tc>>3) & 0x1F
	tens := int(tc & 0x07)
	units := int(tm)
	if h > 23 || tens > 5 || units > 9 {
		return 0, false
	}
	return h*60 + tens*10 + units, true
}

// encodeClock — системное время → байты ячеек (timeCyr, minut).
func encodeClock(now time.Time) (byte, byte) {
	h, m := now.Hour(), now.Minute()
	return byte(((h & 0x1F) << 3) | ((m / 10) & 0x07)), byte(m % 10)
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// Ночное окно: с 22:30 до 07:30 локального времени мелкие расхождения
// (< 5 мин) не корректируем, чтобы лишний раз не писать в МАП.
const (
	quietStartMin      = 22*60 + 30 // 22:30
	quietEndMin        = 7*60 + 30  // 07:30
	quietSyncThreshMin = 5          // порог коррекции в ночное окно
)

// inQuietPeriod сообщает, попадает ли момент в ночное окно [22:30, 07:30).
func inQuietPeriod(now time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	return m >= quietStartMin || m < quietEndMin
}

// shouldCorrectTime решает, корректировать ли время МАП при расхождении diffMin.
// Днём — при превышении обычного порога (diff > threshMin); в ночное окно —
// только при diff >= 5 мин (расхождение «менее 5 минут» игнорируем).
func shouldCorrectTime(now time.Time, diffMin, threshMin int) bool {
	if inQuietPeriod(now) {
		return diffMin >= quietSyncThreshMin
	}
	return diffMin > threshMin
}

// maybeSyncTime — проверка/коррекция времени МАП (см. docs/map-reader.md).
// Вне окна не проверяем; в окне — одна попытка на МАП за окно; корректируем
// только при расхождении > timeSyncThreshMin (ночью 22:30–07:30 — при
// расхождении >= 5 мин, см. shouldCorrectTime); после записи — полное
// перечитывание и публикация (это делает executeWrite). Детали — только в лог.
func maybeSyncTime(cfg *config, reg *Registry, p *Port, proto string, entries []*MapEntry, e *MapEntry, mem memMap, now time.Time) {
	if cfg.timeSyncDisabled(now) {
		return // синхронизация выключена файловым флагом (кэш 1 мин)
	}
	boundary, inWindow := tenMinWindow(now, cfg.timeSyncWindow)
	if !inWindow {
		return
	}
	if e.syncWin == boundary {
		return // в этом окне уже проверяли
	}
	e.syncWin = boundary // одна попытка на окно (даже если неуспешно)
	mapMin, ok := decodeMapTime(mem)
	if !ok {
		return
	}
	sysMin := now.Hour()*60 + now.Minute()
	if !shouldCorrectTime(now, absInt(mapMin-sysMin), cfg.timeSyncThreshMin) {
		return
	}
	tc, minut := encodeClock(now)
	wr := writeRequest{Cmd: "write", ID: e.slotIdx, Gen: e.gen, Ops: []writeOp{
		{Op: "reg", Addr: "1B6", Data: fmt.Sprintf("%02X", tc)},
		{Op: "reg", Addr: "44B", Data: fmt.Sprintf("%02X", minut)},
	}}
	resp := executeWrite(cfg, reg, p, proto, entries, wr)
	model, sn := "", ""
	if e.info != nil {
		model = e.info.Model
		sn = fmt.Sprintf("sn=%d", e.info.Serial)
	}
	tag := fmt.Sprintf("slot=%d port=%s unit=%d model=%s %s", e.slotIdx, e.port, e.unit, model, sn)
	if resp.OK {
		log.Printf("синк времени МАП %s: установлено %02d:%02d (было %02d:%02d)", tag, now.Hour(), now.Minute(), mapMin/60, mapMin%60)
	} else {
		log.Printf("синк времени МАП %s: ошибка: %s", tag, resp.Error)
	}
}
