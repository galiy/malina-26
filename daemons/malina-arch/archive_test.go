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
