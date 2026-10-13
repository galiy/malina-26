package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/galiy/malina-26/daemons/lib/tscontract"
)

func mkPayload(t *testing.T, vals ...tscontract.Value) []byte {
	t.Helper()
	c, ok := tscontract.NewDefaultRegistry().Get("map", 1)
	if !ok {
		t.Fatal("нет кодека map v1")
	}
	p, err := c.Encode(vals)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rowT(t *testing.T, typ, id string, tm int64, vals ...tscontract.Value) Row {
	return Row{Type: typ, ID: id, Time: tm, Version: 1, Payload: mkPayload(t, vals...)}
}

func sample(v float64) []tscontract.Value {
	return []tscontract.Value{{ID: 1, Type: tscontract.FTU8, Int: int64(v)}, {ID: 3, Type: tscontract.FTU8, Int: int64(v)}}
}

func TestStoreWriteQueryRange(t *testing.T) {
	st, err := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rows := []Row{
		rowT(t, "map", "5", 1000, sample(1)...),
		rowT(t, "map", "5", 2000, sample(2)...),
		rowT(t, "map", "5", 3000, sample(3)...),
	}
	if n := st.Write(rows); n != 3 {
		t.Fatalf("принято %d", n)
	}
	got, err := st.Query(Query{Type: "map", IDs: []string{"5"}, From: 1500, To: 3500})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Time != 2000 || got[1].Time != 3000 {
		t.Fatalf("диапазон: %+v", got)
	}
	if got[0].ID != "5" || got[0].Type != "map" {
		t.Fatalf("ключ строки: %+v", got[0])
	}
	// Проекция только поля 3.
	got2, _ := st.Query(Query{Type: "map", IDs: []string{"5"}, From: 0, To: 4000, Metrics: []uint16{3}})
	if len(got2) != 3 {
		t.Fatalf("проекция: %d", len(got2))
	}
	c, _ := tscontract.NewDefaultRegistry().Get("map", 1)
	for _, r := range got2 {
		vals, err := c.Decode(r.Payload)
		if err != nil || len(vals) != 1 || vals[0].ID != 3 {
			t.Fatalf("проекция vals=%+v err=%v", vals, err)
		}
	}
}

func TestStoreSealReopen(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root, Options{SegMax: 300}, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		st.Write([]Row{rowT(t, "map", "7", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	st.Close()

	segs, _ := filepath.Glob(filepath.Join(root, "map", "7", "*", "seg-*.ts"))
	if len(segs) < 2 {
		t.Fatalf("ожидалось ≥2 сегмента, нашли %d", len(segs))
	}

	st2, err := NewStore(root, Options{SegMax: 300}, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, err := st2.Query(Query{Type: "map", IDs: []string{"7"}, From: 0, To: 1000 + 19*10 + 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 || got[0].Time != 1000 || got[19].Time != 1000+19*10 {
		t.Fatalf("после переоткрытия: %d строк, %d..%d", len(got), got[0].Time, got[len(got)-1].Time)
	}
}

func TestStoreGapsAndAutoInit(t *testing.T) {
	root := t.TempDir()
	st, _ := NewStore(root, defaultOptions(), tscontract.NewDefaultRegistry())
	if got, _ := st.Query(Query{Type: "map", From: 0, To: 100}); len(got) != 0 {
		t.Fatalf("пустое дерево: %+v", got)
	}
	st.Write([]Row{rowT(t, "map", "3", 500, sample(1)...)})
	st.Wait()
	st.Close()

	// Новая серия/устройство — авто-инициализация.
	st2, _ := NewStore(root, defaultOptions(), tscontract.NewDefaultRegistry())
	st2.Write([]Row{rowT(t, "kes", "abc", 900, sample(2)...)})
	st2.Wait()
	got, _ := st2.Query(Query{Type: "kes", IDs: []string{"abc"}, From: 0, To: 1000})
	if len(got) != 1 || got[0].Time != 900 {
		t.Fatalf("новый тип/устройство: %+v", got)
	}
	st2.Close()
}

func TestStoreMonotonicDrop(t *testing.T) {
	st, _ := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	defer st.Close()
	st.Write([]Row{rowT(t, "map", "1", 2000, sample(2)...)})
	st.Wait()
	st.Write([]Row{rowT(t, "map", "1", 1000, sample(1)...)}) // убывает — отклоняется
	st.Wait()
	got, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 3000})
	if len(got) != 1 || got[0].Time != 2000 {
		t.Fatalf("убывающая строка не должна попасть: %+v", got)
	}
}

// TestStoreTruncatedTailRecovery — недописанный «хвост» (частичный блок/строка)
// при старте отбрасывается: строки восстанавливаются, файл обрезается, запись
// продолжается (§1.8, §6).
func TestStoreTruncatedTailRecovery(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root, defaultOptions(), tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		st.Write([]Row{rowT(t, "map", "9", int64(1000+i), sample(float64(i))...)})
	}
	st.Wait()
	st.Close()

	segs, _ := filepath.Glob(filepath.Join(root, "map", "9", "*", "seg-*.ts"))
	if len(segs) != 1 {
		t.Fatalf("сегментов: %d", len(segs))
	}
	before, _ := os.Stat(segs[0])
	// Эмулируем недописанный хвост: дописываем мусор неполного блока.
	f, err := os.OpenFile(segs[0], os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0xde, 0xad, 0xbe, 0xef, 0x01}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if after, _ := os.Stat(segs[0]); after.Size() <= before.Size() {
		t.Fatal("мусор не дописан")
	}

	st2, err := NewStore(root, defaultOptions(), tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, _ := st2.Query(Query{Type: "map", IDs: []string{"9"}, From: 0, To: 3000})
	if len(got) != 5 {
		t.Fatalf("после восстановления строк: %d", len(got))
	}
	if trunc, _ := os.Stat(segs[0]); trunc.Size() != before.Size() {
		t.Fatalf("хвост не обрезан: %d, ожидалось %d", trunc.Size(), before.Size())
	}
	// Запись после восстановления продолжается.
	st2.Write([]Row{rowT(t, "map", "9", 2000, sample(7)...)})
	st2.Wait()
	if got2, _ := st2.Query(Query{Type: "map", IDs: []string{"9"}, From: 0, To: 3000}); len(got2) != 6 {
		t.Fatalf("после дописи: %d", len(got2))
	}
}

func TestQueryLimits(t *testing.T) {
	st, _ := NewStore(t.TempDir(), Options{MaxRows: 2, MaxRangeMS: 100_000}, tscontract.NewDefaultRegistry())
	defer st.Close()
	for i := 0; i < 5; i++ {
		st.Write([]Row{rowT(t, "map", "1", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	// Период больше лимита.
	if _, err := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 1 << 40}); err == nil {
		t.Fatal("ожидался отказ по периоду")
	}
	// Период в норме, но строк больше лимита.
	if _, err := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 9999}); err == nil {
		t.Fatal("ожидался отказ по числу строк")
	}
}

func TestValidKeyDotDot(t *testing.T) {
	st, _ := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	defer st.Close()
	st.Write([]Row{
		{Type: "..", ID: "1", Time: 1, Version: 1, Payload: mkPayload(t, sample(1)...)},
		{Type: "map", ID: "..", Time: 2, Version: 1, Payload: mkPayload(t, sample(1)...)},
	})
	st.Wait()
	if got, _ := st.Query(Query{Type: "map", IDs: []string{".."}, From: 0, To: 100}); len(got) != 0 {
		t.Fatalf("id '..' не должен приниматься: %+v", got)
	}
	if got, _ := st.Query(Query{Type: "..", From: 0, To: 100}); len(got) != 0 {
		t.Fatalf("type '..' не должен приниматься: %+v", got)
	}
}

func TestStoreSegmentSkip(t *testing.T) {
	root := t.TempDir()
	st, _ := NewStore(root, Options{SegMax: 300}, tscontract.NewDefaultRegistry())
	for i := 0; i < 20; i++ {
		st.Write([]Row{rowT(t, "map", "7", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	st.Close()
	st2, _ := NewStore(root, Options{SegMax: 300}, tscontract.NewDefaultRegistry())
	defer st2.Close()
	got, _ := st2.Query(Query{Type: "map", IDs: []string{"7"}, From: 1000 + 15*10, To: 1000 + 19*10})
	if len(got) != 5 {
		t.Fatalf("узкий диапазон (отсечение сегментов): %d, ожидалось 5", len(got))
	}
}
