package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestWriteRoundtripNative проверяет запись по нативному протоколу против
// эмулятора МАП (test/fakemap.py): пишем байт и читаем его обратно.
func TestWriteRoundtripNative(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	link := "/tmp/kilo/fakemap"
	os.Remove(link)
	cmd := exec.Command("python3", "test/fakemap.py")
	if err := cmd.Start(); err != nil {
		t.Skipf("fakemap не стартовал: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	ok := false
	for i := 0; i < 100; i++ {
		if _, err := os.Lstat(link); err == nil {
			ok = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		t.Fatalf("ссылка %s не появилась", link)
	}

	p, err := openPort(link)
	if err != nil {
		t.Fatalf("openPort: %v", err)
	}
	defer p.close()

	echo, resp := 50*time.Millisecond, 200*time.Millisecond

	// Чтение до записи — проверка самого обмена.
	if b, err := microartRead(p, 0x1B0, 3, echo, resp); err != nil {
		t.Fatalf("pre-read: %v", err)
	} else {
		t.Logf("pre-read ok: %v", b)
	}

	// Запись одиночного байта в EEPROM-ячейку 0x1B3 (LCD_Sound).
	if err := microartWrite(p, 0x1B3, []byte{0x07}, echo, resp); err != nil {
		t.Fatalf("microartWrite: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	// Блок из 4 байт (проверка P>0 и многобайтной посылки).
	if err := microartWrite(p, 0x1B0, []byte{0x11, 0x22, 0x33, 0x44}, echo, resp); err != nil {
		t.Fatalf("microartWrite block: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	got, err := microartRead(p, 0x1B0, 3, echo, resp)
	if err != nil {
		t.Fatalf("microartRead: %v", err)
	}
	want := []byte{0x11, 0x22, 0x33, 0x44}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("блок: got %v, want %v", got, want)
		}
	}

	// Команда: запись байта по адресу 0.
	if err := microartWrite(p, 0x0000, []byte{0x02}, echo, resp); err != nil {
		t.Fatalf("microartWrite command: %v", err)
	}
}

// TestFakeMapWriteAck — прямой тест разбора ответа на запись.
func TestParseWriteReply(t *testing.T) {
	if err := parseWriteReply([]byte{'o', 0x91, 0x0A}); err != nil {
		t.Fatalf("успех не распознан: %v", err)
	}
	err := parseWriteReply([]byte{'e', 0x10, 0xEF})
	var me *mapError
	if err == nil {
		t.Fatal("ошибка не распознана")
	}
	if !asMapError(err, &me) || me.code != 0x10 {
		t.Fatalf("ожидали mapError 0x10, получили %v", err)
	}
}

func asMapError(err error, target **mapError) bool {
	if e, ok := err.(*mapError); ok {
		*target = e
		return true
	}
	return false
}

// TestValidateWrite — проверка правил контракта.
func TestValidateWrite(t *testing.T) {
	cases := []struct {
		name string
		req  writeRequest
		want kind
		bad  bool
	}{
		{"eeprom", writeRequest{Ops: []writeOp{{Op: "reg", Addr: 0x1B3, Data: []int{1}}}}, kindEEPROM, false},
		{"ram", writeRequest{Ops: []writeOp{{Op: "reg", Addr: 0x400, Data: []int{1}}}}, kindRAM, false},
		{"cmd", writeRequest{Ops: []writeOp{{Op: "cmd", Code: 7}}}, kindCmd, false},
		{"mix eeprom+ram", writeRequest{Ops: []writeOp{{Op: "reg", Addr: 0x1B3, Data: []int{1}}, {Op: "reg", Addr: 0x400, Data: []int{1}}}}, 0, true},
		{"mix reg+cmd", writeRequest{Ops: []writeOp{{Op: "reg", Addr: 0x1B3, Data: []int{1}}, {Op: "cmd", Code: 7}}}, 0, true},
		{"two cmd", writeRequest{Ops: []writeOp{{Op: "cmd", Code: 7}, {Op: "cmd", Code: 2}}}, 0, true},
		{"empty", writeRequest{}, 0, true},
		{"big block", writeRequest{Ops: []writeOp{{Op: "reg", Addr: 0x000, Data: make([]int, 257)}}}, 0, true},
	}
	for _, c := range cases {
		got, err := validateWrite(c.req)
		if c.bad {
			if err == nil {
				t.Errorf("%s: ожидали ошибку", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: неожиданная ошибка %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: kind=%v, want %v", c.name, got, c.want)
		}
	}
}
