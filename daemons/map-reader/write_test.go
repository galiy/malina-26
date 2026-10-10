package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
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

	// Повышенные таймауты: на загруженной машине эхо PTY-эмулятора иногда
	// приходит позже «боевых» 50 мс.
	echo, resp := 300*time.Millisecond, 800*time.Millisecond

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
	time.Sleep(200 * time.Millisecond) // эмулятору — дососать эхо ответа

	// Команда: запись байта по адресу 0.
	if err := microartWrite(p, 0x0000, []byte{0x02}, echo, resp); err != nil {
		t.Fatalf("microartWrite command: %v", err)
	}
}

// TestParseWriteReply — прямой тест разбора ответа на запись.
func TestParseWriteReply(t *testing.T) {
	if err := parseWriteReply([]byte{'o', 0x91, 0x0A}); err != nil {
		t.Fatalf("успех не распознан: %v", err)
	}
	if err := parseWriteReply([]byte{'o', 0x00}); err == nil {
		t.Fatal("битая сумма 'o' должна отвергаться")
	}
	err := parseWriteReply([]byte{'e', 0x10, 0x8B}) // сумма 'e'+0x10+0x8B == 0
	var me *mapError
	if err == nil {
		t.Fatal("ошибка не распознана")
	}
	if !asMapError(err, &me) || me.code != 0x10 {
		t.Fatalf("ожидали mapError 0x10, получили %v", err)
	}
	if got := mapErrorText(0x10); !strings.HasPrefix(got, "ErrRS_NoEEWR") {
		t.Fatalf("текст для 0x10: %q", got)
	}
	if got := mapErrorText(0xAB); got != "МАП вернул неописанную ошибку 0xAB" {
		t.Fatalf("текст для неизвестного кода: %q", got)
	}
}

func asMapError(err error, target **mapError) bool {
	if e, ok := err.(*mapError); ok {
		*target = e
		return true
	}
	return false
}

// TestBuildSequence — обрамление: RAM и EEPROM обрамляются (pre3/post7),
// команда — нет; no_frame снимает обрамление.
func TestBuildSequence(t *testing.T) {
	ram := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "405", Data: "02"}}}
	k, _ := validateWrite(ram)
	seq := buildSequence(ram, k)
	if len(seq) != 3 || !seq[0].isCmd || seq[0].code != 3 || seq[1].isCmd || !seq[2].isCmd || seq[2].code != 7 {
		t.Fatalf("RAM должен обрамляться pre3/post7: %+v", seq)
	}
	two := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B6", Data: "38"}, {Op: "reg", Addr: "44B", Data: "00"}}}
	seq = buildSequence(two, k)
	if len(seq) != 5 {
		t.Fatalf("два блока: ожидали 5 элементов, got %+v", seq)
	}
	want := []int{3, -2, 3, -2, 7} // 3=pre, -2=reg(не cmd), 7=post
	for i, w := range want {
		got := -2
		if seq[i].isCmd {
			got = seq[i].code
		}
		if got != w {
			t.Fatalf("два блока: pre3 перед каждым, затем post7: got %+v", seq)
		}
	}
	nf := writeRequest{NoFrame: true, Ops: []writeOp{{Op: "reg", Addr: "405", Data: "02"}}}
	seq = buildSequence(nf, k)
	if len(seq) != 1 || seq[0].isCmd {
		t.Fatalf("no_frame RAM должен быть без обрамления: %+v", seq)
	}
	cmd := writeRequest{Ops: []writeOp{{Op: "cmd", Code: "02"}}}
	kc, _ := validateWrite(cmd)
	seq = buildSequence(cmd, kc)
	if len(seq) != 1 || !seq[0].isCmd || seq[0].code != 2 {
		t.Fatalf("команда не обрамляется: %+v", seq)
	}
}

func TestValidateWrite(t *testing.T) {
	many := make([]writeOp, maxOps+1)
	for i := range many {
		many[i] = writeOp{Op: "reg", Addr: "1B3", Data: "01"}
	}
	cases := []struct {
		name string
		req  writeRequest
		want kind
		bad  bool
	}{
		{"eeprom", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B3", Data: "01"}}}, kindReg, false},
		{"ram", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "400", Data: "01"}}}, kindReg, false},
		{"cmd", writeRequest{Ops: []writeOp{{Op: "cmd", Code: "07"}}}, kindCmd, false},
		{"eeprom+ram вместе", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B6", Data: "60"}, {Op: "reg", Addr: "44B", Data: "00"}}}, kindReg, false},
		{"mix reg+cmd", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B3", Data: "01"}, {Op: "cmd", Code: "07"}}}, 0, true},
		{"two cmd", writeRequest{Ops: []writeOp{{Op: "cmd", Code: "07"}, {Op: "cmd", Code: "02"}}}, 0, true},
		{"cmd вне 1..9", writeRequest{Ops: []writeOp{{Op: "cmd", Code: "00"}}}, 0, true},
		{"слишком много ops", writeRequest{Ops: many}, 0, true},
		{"empty", writeRequest{}, 0, true},
		{"big block", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "000", Data: strings.Repeat("00", 257)}}}, 0, true},
		{"нечётный hex", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B3", Data: "0"}}}, 0, true},
		{"битый hex", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B3", Data: "0G"}}}, 0, true},
		{"адрес вне 0x7FF", writeRequest{Ops: []writeOp{{Op: "reg", Addr: "800", Data: "01"}}}, 0, true},
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

// startFakeMap поднимает эмулятор МАП на PTY и возвращает порт. Cleanup — через t.
func startFakeMap(t *testing.T) *Port {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нет python3")
	}
	link := "/tmp/kilo/fakemap"
	os.Remove(link)
	cmd := exec.Command("python3", "test/fakemap.py")
	if err := cmd.Start(); err != nil {
		t.Skipf("fakemap не стартовал: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for i := 0; i < 100; i++ {
		if _, err := os.Lstat(link); err == nil {
			p, err := openPort(link)
			if err != nil {
				t.Fatalf("openPort: %v", err)
			}
			return p
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ссылка %s не появилась", link)
	return nil
}

// TestExecuteSequenceNative — интеграционный тест: последовательность из
// buildSequence (обрамление pre3…post7) исполняется через execElement, данные
// долетают; ошибка МАП (ErrRS_NoEEWR) не ретраится и возвращается как mapError.
func TestExecuteSequenceNative(t *testing.T) {
	p := startFakeMap(t)
	defer p.close()
	cfg := &config{probeEcho: 300 * time.Millisecond, probeResponse: 800 * time.Millisecond,
		writeRetries: 5, writePause: 50 * time.Millisecond}

	// EEPROM-блок с обрамлением.
	wr := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "1B0", Data: "11223344"}}}
	k, _ := validateWrite(wr)
	for _, el := range buildSequence(wr, k) {
		if err := execElement(cfg, p, "microart", 0, el); err != nil {
			t.Fatalf("execElement: %v", err)
		}
		time.Sleep(150 * time.Millisecond) // эмулятору — дососать эхо
	}
	got, err := microartRead(p, 0x1B0, 3, 300*time.Millisecond, 800*time.Millisecond)
	if err != nil {
		t.Fatalf("read-back: %v", err)
	}
	want := []byte{0x11, 0x22, 0x33, 0x44}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("read-back: got %v, want %v", got, want)
		}
	}

	// Явная ошибка МАП на read-only 0x004 — mapError 0x10 без ретраев.
	wr2 := writeRequest{Ops: []writeOp{{Op: "reg", Addr: "004", Data: "01"}}}
	k2, _ := validateWrite(wr2)
	var sawErr error
	for _, el := range buildSequence(wr2, k2) {
		if err := execElement(cfg, p, "microart", 0, el); err != nil {
			sawErr = err
			break
		}
	}
	var me *mapError
	if !errors.As(sawErr, &me) || me.code != 0x10 {
		t.Fatalf("ожидали mapError 0x10, получили %v", sawErr)
	}
}
