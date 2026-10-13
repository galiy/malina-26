package web

import (
	"fmt"
	"time"

	"github.com/galiy/malina-26/web/internal/snmp"
)

// trapDef — сопоставление события (курированное поле) на trap/параметр MIB.
// mult — множитель значения при укладке в trap (по MIB: FloatValue ×10,
// KwhValue ×100, целое ×1).
type trapDef struct {
	mainNotif []int // OID нотификации (фронт)
	okNotif   []int // OID нотификации «Ok» (nil — нет Ok-варианта)
	paramOID  []int // OID параметра, кладётся со значением
	mult      float64
}

// trapDefsForMap — маппинг курированных параметров МАП (events.Params) на
// нотификации MICROART-MIB (docs/snmp.md §0.3, §2.6). Поля без соответствия
// (например, t1 = Temp_Grad1) в таблицу не входят — trap не отправляется.
var trapDefsForMap = map[string]trapDef{
	"unet":  {mainNotif: notifChild(1), okNotif: notifChild(19), paramOID: macChild(1, 1), mult: 1},
	"inet":  {mainNotif: notifChild(2), okNotif: notifChild(20), paramOID: macChild(1, 2), mult: 0.1},
	"pnet":  {mainNotif: notifChild(3), okNotif: notifChild(21), paramOID: macChild(1, 3), mult: 1},
	"uacc":  {mainNotif: notifChild(5), okNotif: notifChild(23), paramOID: macChild(3, 1), mult: 0.1},
	"iacc":  {mainNotif: notifChild(6), okNotif: notifChild(24), paramOID: macChild(3, 2), mult: 0.1},
	"pacc":  {mainNotif: notifChild(7), okNotif: notifChild(25), paramOID: macChild(3, 3), mult: 1},
	"imppt": {mainNotif: notifChild(8), okNotif: notifChild(26), paramOID: macChild(3, 4), mult: 0.1},
	"pmppt": {mainNotif: notifChild(49), okNotif: notifChild(50), paramOID: macChild(3, 5), mult: 1},
	"tmap":  {mainNotif: notifChild(14), okNotif: notifChild(28), paramOID: macChild(5, 7), mult: 1},
	"tbat":  {mainNotif: notifChild(13), okNotif: notifChild(27), paramOID: macChild(5, 6), mult: 1},
}

// trapDefForField — описание trap для события с данным полем (МАП).
func trapDefForField(field string) (trapDef, bool) {
	d, ok := trapDefsForMap[field]
	return d, ok
}

// trapMIBValue — приведение значения параметра к целому по множителю MIB.
func trapMIBValue(v float64, mult float64) int64 {
	return int64(v / mult)
}

// sendEventTrap — отправка v2c-trap (фронт или «Ok») движком событий.
// ok=false — фронт-нотификация; ok=true — «Ok» (только при наличии okNotif).
func sendEventTrap(host, field string, value float64, mpptNo int, ok bool) error {
	d, found := trapDefForField(field)
	if !found {
		return fmt.Errorf("snmp: нет нотификации для поля %q", field)
	}
	notif := d.mainNotif
	if ok {
		if d.okNotif == nil {
			// нет Ok-нотификации в MIB — Ok не шлём (§2.5 п.5)
			return fmt.Errorf("snmp: нет Ok-нотификации для поля %q", field)
		}
		notif = d.okNotif
	}
	// Параметр со значением (с множителем MIB, §2.2 п.5). Номер MPPT для МАП-полей
	// не используется (КЭС-события с mpptNo — отдельная таблица future).
	extras := []snmp.Varbind{{OID: d.paramOID, Value: snmp.EncodeValue(snmp.IntValue(trapMIBValue(value, d.mult)))}}
	return snmp.SendTrap(host, "public", notif, uptimeMillis(), extras)
}

// uptimeMillis — аптайм процесса/устройства в мс (для sysUpTime в trap).
func uptimeMillis() int64 {
	return time.Since(serverStart).Milliseconds()
}

// trapEntry — объект периодической отправки: нотификация + OID значения.
type trapEntry struct {
	notif []int
	param []int
}

// periodicTrapEntries — объекты, для которых есть notification-OID (фронт),
// включаются в периодическую отправку (docs/snmp.md §2.10 №2). МАП + КЭС 1..4.
func periodicTrapEntries() []trapEntry {
	var out []trapEntry
	add := func(notif int, param []int) { out = append(out, trapEntry{notif: notifChild(notif), param: param}) }

	// МАП (только с main-нотификацией).
	add(1, macChild(1, 1))  // inputVoltage
	add(2, macChild(1, 2))  // inputCurrent
	add(3, macChild(1, 3))  // inputPower
	add(4, macChild(2, 2))  // outputVoltage
	add(5, macChild(3, 1))  // batteryVoltage
	add(6, macChild(3, 2))  // batteryCurrent
	add(7, macChild(3, 3))  // batteryPower
	add(8, macChild(3, 4))  // batteryMpptCurrent
	add(9, macChild(4, 1))  // countersENET
	add(10, macChild(4, 2)) // countersEACC
	add(11, macChild(4, 3)) // countersECHG
	add(12, macChild(5, 1)) // statusMODE
	add(13, macChild(5, 6)) // statusTACC
	add(14, macChild(5, 7)) // statusTMAC
	add(49, macChild(3, 5)) // batteryMpptPower

	// КЭС 1..4 (main-нотификации из §0.3; PVVoltage — только Ok, не включаем).
	kesNotif := []struct {
		n     int
		field int
	}{
		{29, 10}, // mpptMode
		{32, 2},  // mpptPVPower
		{34, 3},  // mpptPVCurrent
		{36, 4},  // mpptBatteryVoltage
		{38, 5},  // mpptCurrentSensor1
		{40, 6},  // mpptCurrentSensor2
		{42, 7},  // mpptEnergy
		{43, 8},  // mpptInternalTemperature
		{45, 9},  // mpptBatteryTemperature
	}
	for n := 1; n <= 4; n++ {
		for _, kn := range kesNotif {
			add(kn.n, mpptChild(n, kn.field))
		}
	}
	return out
}

// sendPeriodicTraps — читает снимок и отправляет несколько трапов за тик.
// Возвращает число успешно отправленных (наличие устройства и считывание — здесь).
func sendPeriodicTraps(host string) (int, error) {
	view := snmpView()
	sent := 0
	var firstErr error
	for _, e := range periodicTrapEntries() {
		v, ok := view[oidKey(e.param)]
		if !ok {
			// объект недоступен (нет устройства) — не отправляем (§2.10 №5)
			continue
		}
		if err := snmp.SendTrap(host, "public", e.notif, uptimeMillis(), []snmp.Varbind{{OID: e.param, Value: snmp.EncodeValue(v)}}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent++
	}
	if sent == 0 && firstErr == nil {
		// устройства нет или объекты недоступны — тикаем без ошибки
		return 0, nil
	}
	return sent, firstErr
}
