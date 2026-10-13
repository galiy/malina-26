package web

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/galiy/malina-26/web/internal/events"
)

// blacklistTTL — насколько запоминаем ненайденный серийник (тип:серийник).
const blacklistTTL = time.Minute

// mapEventsProvider — источник данных для менеджера событий: текущий МАП из
// map-reader (сырой payload по серийнику). КЭС/MPPT — заглушка (нет источника).
// Ненайденные серийники кладём в локальный блэклист (чтобы не лезть в shm зря).
type mapEventsProvider struct {
	s  *Server
	mu sync.Mutex
	bl map[string]time.Time // "kind:serial" → протухание
}

func (p *mapEventsProvider) blacklisted(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bl == nil {
		return false
	}
	exp, ok := p.bl[key]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(p.bl, key)
		return false
	}
	return true
}

func (p *mapEventsProvider) addBlack(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bl == nil {
		p.bl = map[string]time.Time{}
	}
	p.bl[key] = time.Now().Add(blacklistTTL)
}

func (p *mapEventsProvider) clearBlack(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.bl, key)
}

// validSerial — серийник 0 и 0xFFFF считаем «отсутствующим устройством».
func validSerial(serial uint16) bool { return serial != 0 && serial != 0xFFFF }

func findMapBySerial(serial string) (MapInfo, bool) {
	for _, m := range loadMapList().Maps {
		if !validSerial(m.Serial) {
			continue
		}
		if strconv.FormatUint(uint64(m.Serial), 10) == serial {
			return m, true
		}
	}
	return MapInfo{}, false
}

func (p *mapEventsProvider) Snapshot(serial, kind string) (events.Snapshot, bool) {
	if kind != "" && kind != "map" {
		return events.Snapshot{}, false // КЭС — пока не реализован
	}
	key := kind + ":" + serial
	if p.blacklisted(key) {
		return events.Snapshot{}, false
	}
	m, ok := findMapBySerial(serial)
	if !ok {
		p.addBlack(key)
		return events.Snapshot{}, false
	}
	f := decodeMapFields(mapRawPayload(m.ID))
	if f == nil {
		return events.Snapshot{}, false
	}
	p.clearBlack(key)
	return events.Snapshot{Serial: serial, Kind: "map", Model: m.Model, Fields: f, OK: true}, true
}

// Available — существует ли устройство (target); не найден → блэклист.
func (p *mapEventsProvider) Available(serial, kind string) bool {
	if kind != "" && kind != "map" {
		return false // КЭС — заглушка
	}
	key := kind + ":" + serial
	if p.blacklisted(key) {
		return false
	}
	if _, ok := findMapBySerial(serial); ok {
		p.clearBlack(key)
		return true
	}
	p.addBlack(key)
	return false
}

// decodeMapFields — разбор бинарного payload MapData (см. docs/map-contract.md)
// в карту имя→значение по mapFieldNames.
func decodeMapFields(payload []byte) map[string]uint32 {
	if len(payload) < 64 || binary.LittleEndian.Uint32(payload[0:4]) != 0x4441504D {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(payload[6:8]))
	blen := (n + 7) / 8
	vbase := 64 + blen
	if n <= 0 || 64+blen > len(payload) || vbase+n*4 > len(payload) {
		return nil
	}
	f := make(map[string]uint32, n)
	for i := 0; i < n; i++ {
		if payload[64+(i>>3)]&(1<<(uint(i)%8)) == 0 {
			continue
		}
		name := "field_" + strconv.Itoa(i)
		if i < len(mapFieldNames) {
			name = mapFieldNames[i]
		}
		f[name] = binary.LittleEndian.Uint32(payload[vbase+i*4 : vbase+i*4+4])
	}
	return f
}

// relayCell — регистр состояния реле (как на странице управления).
const relayCell = "586"

var (
	errNotImpl  = errors.New("действие не реализовано")
	errNoDevice = errors.New("устройство не найдено")
)

// eventsActions — исполнители действий менеджера событий через наш стек.
type eventsActions struct{ s *Server }

func (a *eventsActions) Email(subject, body, recipient string) error {
	_, err := a.s.email.SendTextTo(subject, body, recipient)
	return err
}

// mapIDGen — id/gen МАП по серийнику.
func (a *eventsActions) mapIDGen(serial string) (int, uint64, bool) {
	for _, m := range loadMapList().Maps {
		if strconv.FormatUint(uint64(m.Serial), 10) == serial {
			return m.ID, m.Gen, true
		}
	}
	return 0, 0, false
}

// write — команда write в control.sock map-reader.
func (a *eventsActions) write(id int, gen uint64, ops []map[string]any) error {
	req := map[string]any{"cmd": "write", "id": id, "gen": gen, "ops": ops}
	b, _ := json.Marshal(req)
	resp, err := controlCall(a.s.opts.ControlSock, b)
	if err != nil {
		return err
	}
	var mr mapWriteResp
	_ = json.Unmarshal(resp, &mr)
	if !mr.OK {
		if mr.Error != "" {
			return errors.New(mr.Error)
		}
		return errors.New(mr.Code)
	}
	return nil
}

// Relay — включить/выключить реле МАП (read-modify-write байта состояния, чтобы
// второе реле сохранило состояние). КЭС — заглушка.
func (a *eventsActions) Relay(serial, kind string, relayNo int, on bool) error {
	if kind != "" && kind != "map" {
		return errNotImpl
	}
	if relayNo < 1 || relayNo > 3 {
		return errors.New("номер реле 1..3")
	}
	id, gen, ok := a.mapIDGen(serial)
	if !ok {
		return errNoDevice
	}
	st := decodeMapFields(mapRawPayload(id))
	cur := st["Status_RELEdop"]
	bit := uint32(1) << uint(relayNo-1)
	nb := cur &^ bit
	if on {
		nb = cur | bit
	}
	return a.write(id, gen, []map[string]any{{"op": "reg", "addr": relayCell, "data": fmt.Sprintf("%02X", nb)}})
}

// MacCmd — команды МАП: стоп генерации (DischOff=9, только fw≥26), стоп (OFF=1),
// старт (ON=2).
func (a *eventsActions) MacCmd(serial, kind, cmd string) error {
	if kind != "" && kind != "map" {
		return errNotImpl
	}
	id, gen, ok := a.mapIDGen(serial)
	if !ok {
		return errNoDevice
	}
	var code string
	switch cmd {
	case "mac_stop":
		code = "01"
	case "mac_start":
		code = "02"
	case "mac_stop_gen":
		fw := decodeMapFields(mapRawPayload(id))["VerPO"] & 0x1F
		if fw < 26 {
			return nil // на прошивках <26 команду не выполняем
		}
		code = "09"
	default:
		return errors.New("неизвестная команда")
	}
	return a.write(id, gen, []map[string]any{{"op": "cmd", "code": code}})
}

func (a *eventsActions) SNMPTrap(host, field string, value float64, mpptNo int, ok bool) error {
	if host == "" {
		return errors.New("не указан приёмник trap")
	}
	return sendEventTrap(host, field, value, mpptNo, ok)
}
