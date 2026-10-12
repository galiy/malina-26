package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/galiy/malina-26/daemons/lib/microart"
)

func tmpStore(t *testing.T, n int) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kes-emu.json")
	st, err := openStorePath(p, n)
	if err != nil {
		t.Fatalf("openStorePath: %v", err)
	}
	return st, p
}

func TestStoreCreateAndAdd(t *testing.T) {
	st, p := tmpStore(t, 3)
	if len(st.f.Devices) != 3 {
		t.Fatalf("создано %d, ждали 3", len(st.f.Devices))
	}
	seen := map[uint16]bool{}
	for _, d := range st.f.Devices {
		if d.Serial == 0 || seen[d.Serial] {
			t.Fatalf("серийник нулевой/дубль: %d", d.Serial)
		}
		seen[d.Serial] = true
	}
	first := st.f.Devices[0].Serial
	// Увеличиваем до 5: старые сохраняются, добавляются новые
	st2, err := openStorePath(p, 5)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(st2.f.Devices) != 5 {
		t.Fatalf("после расширения %d, ждали 5", len(st2.f.Devices))
	}
	if st2.f.Devices[0].Serial != first {
		t.Fatalf("серийник №0 изменился: %d != %d", st2.f.Devices[0].Serial, first)
	}
	// Меньше — не урезаем
	st3, _ := openStorePath(p, 2)
	if len(st3.f.Devices) != 5 {
		t.Fatalf("при n=2 файл урезан до %d", len(st3.f.Devices))
	}
}

func TestStorePersistFreeCell(t *testing.T) {
	st, p := tmpStore(t, 1)
	st.Image(0).Set(0x20, 0xAB)
	st.Save()
	b, _ := os.ReadFile(p)
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("json: %v", err)
	}
	if f.Devices[0].MemByte(0x20) != 0xAB {
		t.Fatalf("в файле нет 20=AB: %+v", f.Devices[0].Mem)
	}
	// Релоад
	st2, _ := openStorePath(p, 1)
	if st2.Record(0).MemByte(0x20) != 0xAB {
		t.Fatalf("после релоада нет 20")
	}
}

func TestDeviceWriteProtectionAndPersist(t *testing.T) {
	st, _ := tmpStore(t, 1)
	d := newDevice(0, 36434, 0x63, 0x0E3D, st, 0)

	// Запрещено: 0x00, подтверждённая, RAM
	for _, addr := range []uint16{0x0000, 0x0001, 0x0002, 0x0005, 0x000C, 0x000D, 0x009B, 0x400} {
		if d.Write(addr, []byte{0x77}) {
			t.Fatalf("запись в защищённую %#x не отклонена", addr)
		}
	}
	// Свободная EEPROM: разрешена и в файл
	if !d.Write(0x0020, []byte{0x22}) {
		t.Fatalf("запись в свободную 0x20 отклонена")
	}
	if d.Read(0x0020, 1)[0] != 0x22 {
		t.Fatalf("чтение из памяти не видит 0x20")
	}
	if st.Record(0).MemByte(0x20) != 0x22 {
		t.Fatalf("0x20 не сохранён в файл")
	}
	// Реле: команда разрешена, состояние+команда в файл
	if !d.Write(0x0E3D, []byte{0x05}) {
		t.Fatalf("команда реле отклонена")
	}
	if d.Read(relayStateAddr|0, 1) == nil {
		t.Fatal("no")
	}
	if d.ramAt(relayStateAddr) != 0x05 {
		t.Fatalf("состояние реле в памяти: %#x", d.ramAt(relayStateAddr))
	}
	if got := st.Record(0).MemByte(ramOffset(relayStateAddr)); got != 0x05 {
		t.Fatalf("состояние реле в карте: %#x", got)
	}

}

func TestDeviceSerialFromStore(t *testing.T) {
	st, _ := tmpStore(t, 1)
	serial := st.Record(0).Serial
	d := newDevice(0, serial, 0x63, 0x0E3D, st, 0)
	lo := d.get(0x2C9)
	hi := d.get(0x2CA)
	if uint16(lo)|uint16(hi)<<8 != serial {
		t.Fatalf("серийник в памяти %#x != %#x", uint16(lo)|uint16(hi)<<8, serial)
	}
}

func TestHandleRefusesProtectedWrite(t *testing.T) {
	st, _ := tmpStore(t, 1)
	d := newDevice(0, 1, 0x63, 0x0E3D, st, 0)
	rep := d.handle(microart.Request{Cmd: 'w', Addr: 0x0001, Data: []byte{0x09}})
	_, err := microart.ParseReply(rep, 0) // ответ ошибки 'e'
	var de *microart.DeviceError
	if !errorAs(err, &de) || de.Code != 0x20 {
		t.Fatalf("ожидали ошибку 0x20, получили %v (%x)", err, rep)
	}
}

func errorAs(err error, target **microart.DeviceError) bool {
	e, ok := err.(*microart.DeviceError)
	if ok {
		*target = e
	}
	return ok
}

// TestRestartRestoresState — имитация перезапуска процесса: новый Device
// из той же записи файла должен восстановить свободные ячейки и реле.
func TestRestartRestoresState(t *testing.T) {
	st, p := tmpStore(t, 1)
	d := newDevice(0, st.Record(0).Serial, 0x63, 0x0E3D, st, 0)
	if !d.Write(0x0020, []byte{0x33}) {
		t.Fatal("write 0x20")
	}
	if !d.Write(0x0E3D, []byte{0x03}) {
		t.Fatal("write relay")
	}
	// «Перезапуск»: новый store и новый Device из файла
	st2, err := openStorePath(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	rec := st2.Record(0)
	d2 := newDevice(0, rec.Serial, 0x63, 0x0E3D, st2, 0)
	if d2.Read(0x0020, 1)[0] != 0x33 {
		t.Fatalf("после рестарта 0x20=%#x", d2.Read(0x0020, 1)[0])
	}
	if d2.ramAt(relayStateAddr) != 0x03 {
		t.Fatalf("после рестарта реле=%#x", d2.ramAt(relayStateAddr))
	}

}

// TestTickDoesNotPersist — события тикера меняют только карту в памяти,
// файл не перезаписывается (персист — лишь на записи ячеек/реле).
func TestTickDoesNotPersist(t *testing.T) {
	st, p := tmpStore(t, 1)
	d := newDevice(0, st.Record(0).Serial, 0x63, 0x0E3D, st, 0)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	now := d.lastTick
	for i := 0; i < 120; i++ { // 2 минуты тиков
		now = now.Add(time.Second)
		d.Tick(now)
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("файл изменился после событий тикера")
	}
	// А после записи ячейки — меняется.
	if !d.Write(0x0020, []byte{0x77}) {
		t.Fatal("write 0x20")
	}
	after2, _ := os.ReadFile(p)
	if string(after) == string(after2) {
		t.Fatal("файл не изменился после записи ячейки")
	}
}

// TestStoreFixesDuplicateSerials — дубли/нули серийников в файле исправляются.
func TestStoreFixesDuplicateSerials(t *testing.T) {
	st, p := tmpStore(t, 3)
	st.f.Devices[0].Serial = 111
	st.f.Devices[1].Serial = 111 // дубль
	st.f.Devices[2].Serial = 0   // нулевой
	if err := st.save(); err != nil {
		t.Fatal(err)
	}
	st2, err := openStorePath(p, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint16]bool{}
	for _, d := range st2.f.Devices {
		if d.Serial == 0 || seen[d.Serial] {
			t.Fatalf("серийник не уникален/нулевой: %d", d.Serial)
		}
		seen[d.Serial] = true
	}
	if len(seen) != 3 {
		t.Fatalf("уникальных %d, ждали 3", len(seen))
	}
}
