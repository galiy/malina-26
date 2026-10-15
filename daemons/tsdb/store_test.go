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

// TestStoreRowMode — raw-режим (row-раскладка): запись/чтение/переоткрытие/проекция.
func TestStoreRowMode(t *testing.T) {
	root := t.TempDir()
	st, err := NewStore(root, Options{Row: true, SegMax: 300}, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		st.Write([]Row{rowT(t, "map", "5", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	st.Close()

	st2, err := NewStore(root, Options{Row: true}, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, err := st2.Query(Query{Type: "map", IDs: []string{"5"}, From: 0, To: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 || got[0].Time != 1000 || got[19].Time != 1190 {
		t.Fatalf("row: %d строк, %d..%d", len(got), got[0].Time, got[len(got)-1].Time)
	}
	// проекция (1 метрика) в row-режиме (декодирование payload)
	got2, _ := st2.Query(Query{Type: "map", IDs: []string{"5"}, From: 0, To: 2000, Metrics: []uint16{3}})
	c, _ := tscontract.NewDefaultRegistry().Get("map", 1)
	for _, r := range got2 {
		vals, err := c.Decode(r.Payload)
		if err != nil || len(vals) != 1 || vals[0].ID != 3 {
			t.Fatalf("row проекция: %+v err=%v", vals, err)
		}
	}
}

// TestStoreLimitDirection — лимит «страницы»: from — первые N, to — последние N.
func TestStoreLimitDirection(t *testing.T) {
	st, _ := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	defer st.Close()
	for i := 0; i < 10; i++ {
		st.Write([]Row{rowT(t, "map", "1", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	fwd, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 2000000, Direction: dirFrom, Limit: 3})
	if len(fwd) != 3 || fwd[0].Time != 1000 || fwd[2].Time != 1020 {
		t.Fatalf("from+limit: %+v", fwd)
	}
	back, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 2000000, Direction: dirTo, Limit: 3})
	if len(back) != 3 || back[0].Time != 1070 || back[2].Time != 1090 {
		t.Fatalf("to+limit: %+v", back)
	}
}

// TestStoreRowLimitDirection — row-режим: индекс + лимит страницы (from/to),
// старт из середины сегмента (проверка разреженного индекса).
func TestStoreRowLimitDirection(t *testing.T) {
	st, _ := NewStore(t.TempDir(), Options{Row: true, SegMax: 1 << 30}, tscontract.NewDefaultRegistry())
	defer st.Close()
	for i := 0; i < 600; i++ {
		st.Write([]Row{rowT(t, "map", "1", int64(1000+i*10), sample(float64(i))...)})
	}
	st.Wait()
	fwd, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 1000, To: 1 << 40, Direction: dirFrom, Limit: 5})
	if len(fwd) != 5 || fwd[0].Time != 1000 || fwd[4].Time != 1040 {
		t.Fatalf("from+limit: %+v", fwd)
	}
	mid, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 3560, To: 1 << 40, Direction: dirFrom, Limit: 4})
	if len(mid) != 4 || mid[0].Time != 3560 {
		t.Fatalf("индекс из середины: %+v", mid)
	}
	back, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 1 << 40, Direction: dirTo, Limit: 3})
	if len(back) != 3 || back[0].Time != 6970 || back[2].Time != 6990 {
		t.Fatalf("to+limit: %+v", back)
	}
	back2, _ := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 3566, Direction: dirTo, Limit: 3})
	if len(back2) != 3 || back2[2].Time != 3560 {
		t.Fatalf("to из середины: %+v", back2)
	}
}

// TestStoreMeta — список устройств и крайние точки из памяти.
func TestStoreMeta(t *testing.T) {
	st, _ := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	defer st.Close()
	st.Write([]Row{rowT(t, "map", "2", 1000, sample(1.0)...)})
	st.Write([]Row{rowT(t, "kes", "7", 1000, sample(1.0)...)})
	st.Wait()
	devs := st.ListDevices()
	if len(devs) != 2 || devs[0].Type != "kes" || devs[1].Type != "map" {
		t.Fatalf("list: %+v", devs)
	}
	mn, mx, ok := st.Extremes("map", "2")
	if !ok || mn != 1000 || mx != 1000 {
		t.Fatalf("extremes: %d..%d ok=%v", mn, mx, ok)
	}
	if _, _, ok := st.Extremes("map", "99"); ok {
		t.Fatal("extremes: несуществующее устройство найдено")
	}
}

// TestStoreVersionBoundary — граница перехода map v1→v2 в одной серии (БД):
// строки разных версий сосуществуют и читаются с сохранением версии.
func TestStoreVersionBoundary(t *testing.T) {
	st, _ := NewStore(t.TempDir(), defaultOptions(), tscontract.NewDefaultRegistry())
	defer st.Close()
	reg := tscontract.NewDefaultRegistry()
	c1, _ := reg.Get("map", 1)
	c2, _ := reg.Get("map", 2)
	p1, _ := c1.Encode([]tscontract.Value{{ID: 1, Type: tscontract.FTU8, Int: 100}})
	p2, _ := c2.Encode([]tscontract.Value{
		{ID: 1, Type: tscontract.FTU8, Int: 110},
		{ID: 27, Type: tscontract.FTU8, Int: 1},
	})
	st.Write([]Row{{Type: "map", ID: "1", Time: 1000, Version: 1, Payload: p1}})
	st.Write([]Row{{Type: "map", ID: "1", Time: 2000, Version: 2, Payload: p2}})
	st.Write([]Row{{Type: "map", ID: "1", Time: 3000, Version: 2, Payload: p2}})
	st.Wait()
	got, err := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("строк %d, ожидалось 3", len(got))
	}
	if got[0].Version != 1 || got[1].Version != 2 || got[2].Version != 2 {
		t.Fatalf("версии: %d,%d,%d", got[0].Version, got[1].Version, got[2].Version)
	}
	// v2-строка декодируется с полем Relay (27)
	c, _ := reg.Get("map", got[2].Version)
	vals, derr := c.Decode(got[2].Payload)
	if derr != nil {
		t.Fatal(derr)
	}
	if len(vals) != 2 {
		t.Fatalf("v2 полей %d, ожидалось 2", len(vals))
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
	// §1 ревью 2026-10-15: decimation применяется ДО maxRows — плотное окно не
	// падает, если после прореживания укладывается в лимит. Строки 1000,1010,1020,
	// 1030,1040; bucket=25 мс → [1000,1030] (2 строки ≤ MaxRows=2).
	got, err := st.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 9999, Bucket: 25})
	if err != nil {
		t.Fatalf("decimation должна снимать ложный maxRows: %v", err)
	}
	if len(got) != 2 || got[0].Time != 1000 || got[1].Time != 1030 {
		t.Fatalf("decimation+maxRows: %v", got)
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

func TestDecimateRowsNearest(t *testing.T) {
	// Строки 0,100,1100,2100,2200; bucket=1000. Границы: 1000 (от 0), затем 2100
	// (от 1100). К границе 2100 ближе prev=2100 (0 мс), чем next=2200 (100 мс) —
	// включаем 2100 (в отличие от «первой после границы», где было бы 2200).
	rows := []Row{
		{ID: "1", Time: 0}, {ID: "1", Time: 100}, {ID: "1", Time: 1100},
		{ID: "1", Time: 2100}, {ID: "1", Time: 2200},
	}
	got := decimateRows(rows, 1000, dirFrom)
	want := []int64{0, 1100, 2100}
	if len(got) != len(want) {
		t.Fatalf("decimate from: %d строк, ожидалось %d (%v)", len(got), len(want), times(got))
	}
	for i := range want {
		if got[i].Time != want[i] {
			t.Fatalf("decimate from[%d]=%d, ожидалось %d", i, got[i].Time, want[i])
		}
	}
	// direction=to — симметрично, порядок возрастает.
	gotTo := decimateRows(rows, 1000, dirTo)
	wantTo := []int64{100, 1100, 2200}
	if len(gotTo) != len(wantTo) {
		t.Fatalf("decimate to: %d строк, ожидалось %d (%v)", len(gotTo), len(wantTo), times(gotTo))
	}
	for i := range wantTo {
		if gotTo[i].Time != wantTo[i] {
			t.Fatalf("decimate to[%d]=%d, ожидалось %d", i, gotTo[i].Time, wantTo[i])
		}
	}
	if r := decimateRows(rows, 0, dirFrom); len(r) != len(rows) {
		t.Fatalf("bucket=0 не должен резать: %d", len(r))
	}
}

func TestDecimateRowsTie(t *testing.T) {
	// Граница G=1000 (от 0); prev=900 (dp=100) и next=1100 (dn=100) равноудалены —
	// при равенстве включаем next (за границей).
	rows := []Row{{ID: "1", Time: 0}, {ID: "1", Time: 900}, {ID: "1", Time: 1100}}
	got := decimateRows(rows, 1000, dirFrom)
	if len(got) != 2 || got[0].Time != 0 || got[1].Time != 1100 {
		t.Fatalf("dp==dn: ожидалось [0,1100], получено %v", times(got))
	}
}

func TestDecimateRowsIDBoundary(t *testing.T) {
	// Смена ID внутри набора сбрасывает опору (порядок — по (ID, time)).
	rows := []Row{
		{ID: "1", Time: 0}, {ID: "1", Time: 500},
		{ID: "2", Time: 100}, {ID: "2", Time: 600},
	}
	got := decimateRows(rows, 1000, dirFrom)
	if len(got) != 2 || got[0].ID != "1" || got[0].Time != 0 || got[1].ID != "2" || got[1].Time != 100 {
		t.Fatalf("смена ID: ожидалось [1/0, 2/100], получено %v", got)
	}
}

func times(rows []Row) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.Time
	}
	return out
}

func TestRowFooterNotParsedAsRows(t *testing.T) {
	root := t.TempDir()
	opts := Options{Row: true, SegMax: 300} // малый сегмент → запечатывание с index_footer
	st, err := NewStore(root, opts, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := 0; i < 200; i++ {
		tm := int64(1000 + i*1000)
		st.Write([]Row{rowT(t, "map", "1", tm, sample(1)...)})
		last = tm
	}
	st.Wait()
	st.Close()

	// Перезагрузка: index_footer не должен трактоваться как строки (иначе
	// «последнее время» улетает в будущее и запись блокируется).
	st2, err := NewStore(root, opts, tscontract.NewDefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	got, _ := st2.Query(Query{Type: "map", IDs: []string{"1"}, From: 0, To: 1 << 40})
	if len(got) == 0 {
		t.Fatal("нет строк после перезагрузки")
	}
	for _, r := range got {
		if r.Time > last {
			t.Fatalf("index_footer разобран как строка: время %d > последнего %d", r.Time, last)
		}
	}
	// И запись после перезагрузки проходит (не блокируется future-lastTime).
	st2.Write([]Row{rowT(t, "map", "1", last+1000, sample(2)...)})
	st2.Wait()
	if got2, _ := st2.Query(Query{Type: "map", IDs: []string{"1"}, From: last, To: last + 1000}); len(got2) == 0 {
		t.Fatal("запись после перезагрузки отклонена (future lastTime?)")
	}
}
