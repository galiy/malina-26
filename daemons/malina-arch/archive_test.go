package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveRawRoundTrip(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	dst := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(raw, "bms", "x", "0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "bms", "x", "0", "seg-000001.ts"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	tg := &localTarget{root: dst}
	runID := "run-1"
	if _, err := archiveRawOnce(tg, "dev", runID, raw, ""); err != nil {
		t.Fatal(err)
	}
	// добавили хвост — должен уехать delta
	f, _ := os.OpenFile(filepath.Join(raw, "bms", "x", "0", "seg-000001.ts"), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("ghi")
	_ = f.Close()
	if _, err := archiveRawOnce(tg, "dev", runID, raw, ""); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(dir, "restored")
	gotRun, ok, err := restoreRaw(tg, "dev", restored)
	if err != nil || !ok || gotRun != runID {
		t.Fatalf("restore: %q %v %v", gotRun, ok, err)
	}
	b, err := os.ReadFile(filepath.Join(restored, "bms", "x", "0", "seg-000001.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "abcdefghi" {
		t.Fatalf("restored=%q", b)
	}
	// локальное удаление — remote тоже должен исчезнуть
	if err := os.Remove(filepath.Join(raw, "bms", "x", "0", "seg-000001.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "bms", "x", "0", "seg-000002.ts"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveRawOnce(tg, "dev", runID, raw, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "dev", "raw", "files", "bms", "x", "0", "seg-000001.ts")); !os.IsNotExist(err) {
		t.Fatalf("remote file not removed: %v", err)
	}
}

func TestBackupAvgSealedOnly(t *testing.T) {
	dir := t.TempDir()
	avg := filepath.Join(dir, "avg")
	if err := os.MkdirAll(filepath.Join(avg, "bms", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	sealed := make([]byte, 32)
	copy(sealed[len(sealed)-8:], []byte{'M', 'T', 'S', 'X', 0, 0, 0, 0})
	if err := os.WriteFile(filepath.Join(avg, "bms", "x", "seg-000001.ts"), sealed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avg, "bms", "x", "seg-000002.ts"), []byte("active"), 0o644); err != nil {
		t.Fatal(err)
	}
	tg := &localTarget{root: filepath.Join(dir, "target")}
	if _, err := backupAvgOnce(tg, "dev", avg, false); err != nil {
		t.Fatal(err)
	}
	var m avgManifest
	ok, err := readJSON(tg, "dev/avg/manifest.json", &m)
	if err != nil || !ok || !m.Completed {
		t.Fatalf("manifest: %v %v", ok, err)
	}
	if _, ok := m.Files["bms/x/seg-000001.ts"]; !ok {
		t.Fatalf("sealed missing: %+v", m.Files)
	}
	if _, ok := m.Files["bms/x/seg-000002.ts"]; ok {
		t.Fatalf("active must be excluded: %+v", m.Files)
	}
}

func TestArchiveRawRunIDMismatch(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw")
	if err := os.MkdirAll(filepath.Join(raw, "bms", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "bms", "x", "seg-000001.ts"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	tg := &localTarget{root: filepath.Join(dir, "target")}
	if _, err := archiveRawOnce(tg, "dev", "run-1", raw, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveRawOnce(tg, "dev", "run-2", raw, ""); !errors.Is(err, errRunIDMatch) {
		t.Fatalf("ожидался errRunIDMatch, получено %v", err)
	}
	// чужой manifest не перезаписан
	var m rawManifest
	if ok, err := readJSON(tg, "dev/raw/manifest.json", &m); err != nil || !ok || m.RunID != "run-1" {
		t.Fatalf("manifest: %+v %v %v", m, ok, err)
	}
}

func TestAvgMirrorPrune(t *testing.T) {
	dir := t.TempDir()
	avg := filepath.Join(dir, "avg")
	if err := os.MkdirAll(filepath.Join(avg, "bms", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	sealed := make([]byte, 16)
	copy(sealed[len(sealed)-8:], []byte{'M', 'T', 'S', 'X', 0, 0, 0, 0})
	for _, n := range []string{"seg-000001.ts", "seg-000002.ts"} {
		if err := os.WriteFile(filepath.Join(avg, "bms", "x", n), sealed, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tg := &localTarget{root: filepath.Join(dir, "target")}
	if _, err := backupAvgOnce(tg, "dev", avg, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(avg, "bms", "x", "seg-000001.ts")); err != nil {
		t.Fatal(err)
	}
	if _, err := backupAvgOnce(tg, "dev", avg, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "target", "dev", "avg", "files", "bms", "x", "seg-000001.ts")); !os.IsNotExist(err) {
		t.Fatalf("orphan не удалён: %v", err)
	}
	var m avgManifest
	if ok, err := readJSON(tg, "dev/avg/manifest.json", &m); err != nil || !ok {
		t.Fatal(err)
	}
	if _, ok := m.Files["bms/x/seg-000001.ts"]; ok {
		t.Fatalf("manifest содержит удалённый файл: %+v", m.Files)
	}
}

func TestSecretAADCompatibility(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	aad := archAAD("sftp", "password")
	enc, err := encryptSecret(key, aad, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decryptSecret(key, aad, enc); err != nil || got != "secret" {
		t.Fatalf("новый AAD: %q %v", got, err)
	}
	legacy, err := encryptSecret(key, "malina-arch/sftp/password", "old")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decryptSecret(key, aad, legacy); err != nil || got != "old" {
		t.Fatalf("legacy AAD: %q %v", got, err)
	}
}

func TestAvgHasData(t *testing.T) {
	dir := t.TempDir()
	if avgHasData(dir) {
		t.Fatal("пустой каталог")
	}
	if err := os.MkdirAll(filepath.Join(dir, "bms", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bms", "x", "seg-000001.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !avgHasData(dir) {
		t.Fatal("сегмент не найден")
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteJSONAtomicKeepsOld(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	v1 := rawManifest{RunID: "run-1", Updated: 1, Files: map[string]int64{"a": 1}}
	if _, err := writeJSONAtomic(tg, "dev/raw/manifest.json", &v1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dev", "raw", "manifest.json.old")); !os.IsNotExist(err) {
		t.Fatalf("при первой записи .old не должен появляться: %v", err)
	}
	v2 := rawManifest{RunID: "run-2", Updated: 2, Files: map[string]int64{}}
	if _, err := writeJSONAtomic(tg, "dev/raw/manifest.json", &v2); err != nil {
		t.Fatal(err)
	}
	var old rawManifest
	if ok, err := readJSON(tg, "dev/raw/manifest.json.old", &old); err != nil || !ok || old.RunID != "run-1" {
		t.Fatalf("old: %+v %v %v", old, ok, err)
	}
	var cur rawManifest
	if ok, err := readJSON(tg, "dev/raw/manifest.json", &cur); err != nil || !ok || cur.RunID != "run-2" {
		t.Fatalf("current: %+v %v %v", cur, ok, err)
	}
}

func TestReadRawManifestFallback(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/raw/manifest.json"
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json"), `{"run_id":`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.old"), `{"run_id":"old","updated":5,"files":{"x":1}}`)

	m, ok, err := readRawManifest(tg, base)
	if err != nil || !ok || m.RunID != "old" {
		t.Fatalf("fallback .old: %+v %v %v", m, ok, err)
	}

	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.old"), `{`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.new"), `{"run_id":"new","updated":7,"files":{}}`)
	m, ok, err = readRawManifest(tg, base)
	if err != nil || !ok || m.RunID != "new" {
		t.Fatalf("fallback .new: %+v %v %v", m, ok, err)
	}

	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.new"), `{`)
	if _, _, err := readRawManifest(tg, base); !errors.Is(err, errArchiveInvalid) {
		t.Fatalf("ожидался errArchiveInvalid, получено %v", err)
	}
}

func TestRestoreRawReconstructsManifest(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "files", "bms", "x", "0", "seg-000001.ts"), "abcdef")
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json"), `{"run_id":`)

	restored := filepath.Join(dir, "restored")
	runID, ok, err := restoreRaw(tg, "dev", restored)
	if err != nil || !ok || runID == "" {
		t.Fatalf("restore после реконструкции: %q %v %v", runID, ok, err)
	}
	b, err := os.ReadFile(filepath.Join(restored, "bms", "x", "0", "seg-000001.ts"))
	if err != nil || string(b) != "abcdef" {
		t.Fatalf("restored=%q err=%v", b, err)
	}
	m, ok, err := readRawManifest(tg, "dev/raw/manifest.json")
	if err != nil || !ok || m.RunID != runID {
		t.Fatalf("manifest после реконструкции: %+v %v %v", m, ok, err)
	}
}

func TestRestoreRawUsesOldManifest(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "files", "bms", "x", "0", "seg-000001.ts"), "abc")
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json"), `{`)
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json.old"),
		`{"run_id":"old-run","updated":5,"files":{"bms/x/0/seg-000001.ts":3}}`)

	runID, ok, err := restoreRaw(tg, "dev", filepath.Join(dir, "restored"))
	if err != nil || !ok || runID != "old-run" {
		t.Fatalf("restore из .old: %q %v %v", runID, ok, err)
	}
}

func TestArchiveRawOnceReconstructsManifest(t *testing.T) {
	dir := t.TempDir()
	// Локальный raw пуст: это отличает реконструкцию от resetRaw, который
	// стёр бы files/ и дал пустой manifest.
	if err := os.MkdirAll(filepath.Join(dir, "raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "files", "bms", "x", "0", "seg-000001.ts"), "abcdef")
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json"), `{"run_id":`)

	if _, err := archiveRawOnce(tg, "dev", "run-9", filepath.Join(dir, "raw"), ""); err != nil {
		t.Fatal(err)
	}
	m, ok, err := readRawManifest(tg, "dev/raw/manifest.json")
	if err != nil || !ok || m.RunID != "run-9" {
		t.Fatalf("manifest: %+v %v %v", m, ok, err)
	}
	if _, ok := m.Files["bms/x/0/seg-000001.ts"]; !ok {
		t.Fatalf("файл не попал в восстановленный manifest: %+v", m.Files)
	}
}

func TestRestoreAvgReconstructsManifest(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "avg", "files", "bms", "x", "seg-000001.ts"), "sealed")
	writeTestFile(t, filepath.Join(dir, "target", "dev", "avg", "manifest.json"), `{"copy_id":`)

	restored := filepath.Join(dir, "restored-avg")
	_, ok, err := restoreAvg(tg, "dev", restored, nil)
	if err != nil || !ok {
		t.Fatalf("restore avg после реконструкции: %v %v", ok, err)
	}
	b, err := os.ReadFile(filepath.Join(restored, "bms", "x", "seg-000001.ts"))
	if err != nil || string(b) != "sealed" {
		t.Fatalf("restored=%q err=%v", b, err)
	}
	if m, ok, err := readAvgManifest(tg, "dev/avg/manifest.json"); err != nil || !ok || !m.Completed {
		t.Fatalf("manifest: %+v %v %v", m, ok, err)
	}
}

func TestReadRawManifestFreshWins(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/raw/manifest.json"
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json"),
		`{"run_id":"main","updated":5,"files":{}}`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.old"),
		`{"run_id":"old","updated":9,"files":{}}`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.new"),
		`{"run_id":"new","updated":7,"files":{}}`)

	m, ok, err := readRawManifest(tg, base)
	if err != nil || !ok || m.RunID != "old" {
		t.Fatalf("ожидался самый свежий (old, updated=9): %+v %v %v", m, ok, err)
	}
}

func TestReadAvgManifestFallback(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/avg/manifest.json"
	writeTestFile(t, filepath.Join(dir, "dev", "avg", "manifest.json"), `{`)
	writeTestFile(t, filepath.Join(dir, "dev", "avg", "manifest.json.new"),
		`{"copy_id":"c1","updated":3,"completed":true,"files":{"a":1}}`)

	m, ok, err := readAvgManifest(tg, base)
	if err != nil || !ok || m.CopyID != "c1" || !m.Completed {
		t.Fatalf("fallback avg .new: %+v %v %v", m, ok, err)
	}
}

func TestReadRawManifestMissingMain(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/raw/manifest.json"
	// Обрыв между двумя rename: основной уже уехал в .old, .new не переименован.
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.old"),
		`{"run_id":"old","updated":2,"files":{}}`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.new"),
		`{"run_id":"new","updated":4,"files":{}}`)

	m, ok, err := readRawManifest(tg, base)
	if err != nil || !ok || m.RunID != "new" {
		t.Fatalf("ожидался .new при отсутствии основного: %+v %v %v", m, ok, err)
	}
}

func TestRestoreRawEmptyFilesInvalid(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: filepath.Join(dir, "target")}
	if err := os.MkdirAll(filepath.Join(dir, "target", "dev", "raw", "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json"), `{`)

	if _, ok, err := restoreRaw(tg, "dev", filepath.Join(dir, "restored")); !errors.Is(err, errArchiveInvalid) || ok {
		t.Fatalf("ожидался errArchiveInvalid при пустом files/: ok=%v err=%v", ok, err)
	}
}

func TestRestoreRawRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "raw", "manifest.json"),
		`{"run_id":"x","updated":1,"files":{"../evil":1}}`)

	if _, ok, err := restoreRaw(tg, "dev", filepath.Join(dir, "restored")); !errors.Is(err, errArchiveInvalid) || ok {
		t.Fatalf("небезопасный путь должен отклоняться: ok=%v err=%v", ok, err)
	}
}

func TestBackupAvgOnceReconstructsManifest(t *testing.T) {
	dir := t.TempDir()
	avg := filepath.Join(dir, "avg")
	sealed := make([]byte, 16)
	copy(sealed[len(sealed)-8:], []byte{'M', 'T', 'S', 'X', 0, 0, 0, 0})
	if err := os.MkdirAll(filepath.Join(avg, "bms", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avg, "bms", "x", "seg-000001.ts"), sealed, 0o644); err != nil {
		t.Fatal(err)
	}
	tg := &localTarget{root: filepath.Join(dir, "target")}
	writeTestFile(t, filepath.Join(dir, "target", "dev", "avg", "files", "bms", "x", "seg-000001.ts"), string(sealed))
	writeTestFile(t, filepath.Join(dir, "target", "dev", "avg", "manifest.json"), `{"copy_id":`)

	if _, err := backupAvgOnce(tg, "dev", avg, false); err != nil {
		t.Fatal(err)
	}
	m, ok, err := readAvgManifest(tg, "dev/avg/manifest.json")
	if err != nil || !ok || !m.Completed {
		t.Fatalf("manifest после реконструкции: %+v %v %v", m, ok, err)
	}
	if m.CopyID == "" {
		t.Fatal("copy_id пуст: реконструкция не выполнялась (без неё copy_id остался бы пустым)")
	}
	if _, ok := m.Files["bms/x/seg-000001.ts"]; !ok {
		t.Fatalf("sealed-сегмент не попал в manifest: %+v", m.Files)
	}
}

func TestReadRawManifestEqualUpdatedPrefersMain(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/raw/manifest.json"
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json"),
		`{"run_id":"main","updated":5,"files":{}}`)
	writeTestFile(t, filepath.Join(dir, "dev", "raw", "manifest.json.new"),
		`{"run_id":"new","updated":5,"files":{}}`)

	m, ok, err := readRawManifest(tg, base)
	if err != nil || !ok || m.RunID != "main" {
		t.Fatalf("при равном updated должен побеждать основной: %+v %v %v", m, ok, err)
	}
}

func TestReadAvgManifestFreshWins(t *testing.T) {
	dir := t.TempDir()
	tg := &localTarget{root: dir}
	base := "dev/avg/manifest.json"
	writeTestFile(t, filepath.Join(dir, "dev", "avg", "manifest.json"),
		`{"copy_id":"main","updated":5,"completed":true,"files":{}}`)
	writeTestFile(t, filepath.Join(dir, "dev", "avg", "manifest.json.old"),
		`{"copy_id":"old","updated":9,"completed":true,"files":{}}`)

	m, ok, err := readAvgManifest(tg, base)
	if err != nil || !ok || m.CopyID != "old" {
		t.Fatalf("ожидался самый свежий avg (old, updated=9): %+v %v %v", m, ok, err)
	}
}
