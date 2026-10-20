package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type rawManifest struct {
	RunID   string           `json:"run_id"`
	Updated int64            `json:"updated"`
	Files   map[string]int64 `json:"files"`
}

type avgManifest struct {
	CopyID    string           `json:"copy_id"`
	Updated   int64            `json:"updated"`
	Completed bool             `json:"completed"`
	Active    string           `json:"active,omitempty"`
	Files     map[string]int64 `json:"files"`
}

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(b))
}

func localFiles(root string) (map[string]int64, error) {
	// Пропавший корень (не смонтирован tmpfs) — ошибка, а не «пустое окно»:
	// иначе можно принять решение об удалении всего архива.
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = st.Size()
		return nil
	})
	return out, err
}

func readJSON(t target, rel string, v any) (bool, error) {
	f, err := t.Open(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("%w: %v", errArchiveInvalid, err)
	}
	return true, nil
}

func safeRel(rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func clearDir(root string) error {
	es, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(root, 0o755)
	}
	if err != nil {
		return err
	}
	for _, e := range es {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeJSONAtomic(t target, rel string, v any) (int64, error) {
	tmp := rel + ".new"
	w, err := t.Create(tmp)
	if err != nil {
		return 0, err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		w.Close()
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	if err := t.Rename(tmp, rel); err != nil {
		return 0, err
	}
	if st, err := t.Stat(rel); err == nil {
		return st.Size(), nil
	}
	return 0, nil
}

func copyPrefix(t target, rel, local string, remoteSize int64) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(remoteSize, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	for {
		n, er := f.Read(buf)
		if n > 0 {
			if err := t.WriteAt(rel, buf[:n], remoteSize); err != nil {
				return err
			}
			remoteSize += int64(n)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return er
		}
	}
	return nil
}

func copyFull(t target, rel, local string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := t.Create(rel)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	cerr := w.Close()
	if err != nil {
		return err
	}
	return cerr
}

// archiveRawOnce зеркалит текущее окно raw на target.
func archiveRawOnce(t target, device, runID, localRoot, rawSock string) (int64, error) {
	base := pathJoin(device, "raw")
	var m rawManifest
	ok, err := readJSON(t, pathJoin(base, "manifest.json"), &m)
	if err != nil {
		return 0, err
	}
	if !ok {
		// Архива ещё нет: начинаем сессию.
		if err := resetRaw(t, device, runID); err != nil {
			return 0, err
		}
		m = rawManifest{RunID: runID, Files: map[string]int64{}}
	} else if m.RunID == "" {
		return 0, fmt.Errorf("%w: пустой run_id в manifest", errArchiveInvalid)
	} else if m.RunID != runID {
		// Чужой архив при работающем tsdb автоматически не затираем.
		return 0, errRunIDMatch
	}
	if m.Files == nil {
		m.Files = map[string]int64{}
	}
	for rel := range m.Files {
		if _, ok := safeRel(rel); !ok {
			return 0, fmt.Errorf("%w: небезопасный путь %q", errArchiveInvalid, rel)
		}
	}
	local, err := localFiles(localRoot)
	if err != nil {
		return 0, err
	}
	// Пустой локальный raw при непустом архиве — не повод стирать архив:
	// это может быть момент старта до первой записи. Обрезка возобновится,
	// когда появятся данные.
	if len(local) == 0 && len(m.Files) > 0 {
		lx.Warnf("raw: локальное окно пусто, обрезка архива отложена")
		if st, err := t.Stat(pathJoin(base, "manifest.json")); err == nil {
			return st.Size(), nil
		}
		return 0, nil
	}
	// Если raw уже остановлен, tsdb.sock отсутствует и размер файла —
	// committed offset. Если жив — спрашиваем MTSP.
	liveSock := rawSockAlive(rawSock)
	for rel, size := range local {
		if liveSock && !isSealed(filepath.Join(localRoot, filepath.FromSlash(rel))) {
			typ, id, seg, ok := splitRawRel(rel)
			if !ok {
				continue
			}
			committed, ok, err := committedOffset(rawSock, typ, id, seg, false)
			if err != nil || !ok {
				return 0, fmt.Errorf("committed offset %s: %w", rel, err)
			}
			if int64(committed) < size {
				size = int64(committed)
			}
		}
		rem := pathJoin(base, "files", rel)
		remoteSize := int64(0)
		if st, err := t.Stat(rem); err == nil {
			remoteSize = st.Size()
		}
		if remoteSize > size {
			// локальный файл короче — архив не должен отдавать лишнее
			_ = t.Remove(rem)
			remoteSize = 0
		}
		if remoteSize < size {
			lx.Debugf("raw: копия %s [%d..%d)", rel, remoteSize, size)
			if err := copyPrefix(t, rem, filepath.Join(localRoot, filepath.FromSlash(rel)), remoteSize); err != nil {
				return 0, err
			}
		}
		m.Files[rel] = size
	}
	// удалить remote-файлы, которых больше нет локально (обрезка архива)
	for rel := range m.Files {
		if _, ok := local[rel]; !ok {
			lx.Debugf("raw: обрезка архива, удалён %s", rel)
			_ = t.Remove(pathJoin(base, "files", rel))
			delete(m.Files, rel)
		}
	}
	m.Updated = time.Now().Unix()
	return writeJSONAtomic(t, pathJoin(base, "manifest.json"), &m)
}

func splitRawRel(rel string) (typ, id, seg string, ok bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 4 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[len(parts)-1], true
}

func resetRaw(t target, device, runID string) error {
	lx.Infof("raw-архив: переинициализация, run_id=%s", runID)
	base := pathJoin(device, "raw")
	if err := t.MkdirAll(pathJoin(base, "files")); err != nil {
		return err
	}
	if _, err := writeJSONAtomic(t, pathJoin(base, "manifest.json"), &rawManifest{RunID: runID, Files: map[string]int64{}}); err != nil {
		return err
	}
	if err := removeTree(t, pathJoin(base, "files")); err != nil {
		return err
	}
	return t.MkdirAll(pathJoin(base, "files"))
}

func restoreRaw(t target, device, localRoot string) (string, bool, error) {
	return restoreRawCancelable(t, device, localRoot, func() bool { return false })
}

func restoreRawCancelable(t target, device, localRoot string, cancelled func() bool) (string, bool, error) {
	base := pathJoin(device, "raw")
	var m rawManifest
	ok, err := readJSON(t, pathJoin(base, "manifest.json"), &m)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	if m.RunID == "" {
		return "", false, fmt.Errorf("%w: пустой run_id в manifest", errArchiveInvalid)
	}
	for rel := range m.Files {
		if cancelled != nil && cancelled() {
			return "", false, errors.New("restore отменён")
		}
		if _, ok := safeRel(rel); !ok {
			return "", false, fmt.Errorf("%w: небезопасный путь в raw manifest", errArchiveInvalid)
		}
		if _, err := t.Stat(pathJoin(base, "files", rel)); err != nil {
			return "", false, err
		}
	}
	if err := clearDir(localRoot); err != nil {
		return "", false, err
	}
	for rel := range m.Files {
		if cancelled != nil && cancelled() {
			return "", false, errors.New("restore отменён")
		}
		lx.Debugf("restore raw: %s", rel)
		if err := copyRemoteFile(t, pathJoin(base, "files", rel), filepath.Join(localRoot, filepath.FromSlash(rel)), cancelled); err != nil {
			return "", false, err
		}
	}
	lx.Infof("restore raw: восстановлено файлов %d, run_id=%s", len(m.Files), m.RunID)
	return m.RunID, true, nil
}

// copyRemoteFile потоково читает remote-файл и пишет локально, проверяя
// отмену между блоками.
func copyRemoteFile(t target, rel, dst string, cancelled func() bool) error {
	f, err := t.Open(rel)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	buf := make([]byte, 256*1024)
	for {
		if cancelled != nil && cancelled() {
			w.Close()
			return errors.New("restore отменён")
		}
		n, er := f.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				w.Close()
				return err
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			w.Close()
			return er
		}
	}
	return w.Close()
}

func isSealed(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < 12 {
		return false
	}
	b := make([]byte, 8)
	if _, err := f.ReadAt(b, st.Size()-8); err != nil {
		return false
	}
	// magic "MTSX" | crc32
	return len(b) == 8 && b[0] == 'M' && b[1] == 'T' && b[2] == 'S' && b[3] == 'X'
}

// avgFiles возвращает: файлы к копированию (sealed, плюс active при
// includeActive), все существующие .ts (для обрезки manifest) и путь active.
func avgFiles(root string, includeActive bool) (copySet, existing map[string]int64, active string, err error) {
	copySet = map[string]int64{}
	existing = map[string]int64{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".ts") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		st, err := d.Info()
		if err != nil {
			return err
		}
		existing[rel] = st.Size()
		if isSealed(p) {
			copySet[rel] = st.Size()
		} else {
			active = rel
			if includeActive {
				copySet[rel] = st.Size()
			}
		}
		return nil
	})
	return
}

func backupAvgOnce(t target, device, localRoot string, includeActive bool) (int64, error) {
	files, existing, active, err := avgFiles(localRoot, includeActive)
	if err != nil {
		return 0, err
	}
	base := pathJoin(device, "avg")
	if len(files) == 0 {
		// Нет запечатанных сегментов: копировать нечего. Пустой manifest не
		// создаём (раньше он выглядел как «архив есть»), старую пустую
		// заготовку убираем.
		if len(existing) == 0 {
			return 0, errors.New("avg: нет сегментов для копирования")
		}
		var old avgManifest
		if ok, _ := readJSON(t, pathJoin(base, "manifest.json"), &old); ok {
			if len(old.Files) == 0 {
				_ = t.Remove(pathJoin(base, "manifest.json"))
				return 0, nil
			}
			if st, err := t.Stat(pathJoin(base, "manifest.json")); err == nil {
				return st.Size(), nil
			}
		}
		return 0, nil
	}
	var m avgManifest
	ok, _ := readJSON(t, pathJoin(base, "manifest.json"), &m)
	if !ok || m.Files == nil {
		m = avgManifest{Files: map[string]int64{}}
	}
	for rel, size := range files {
		rem := pathJoin(base, "files", rel)
		remoteSize := int64(0)
		if st, err := t.Stat(rem); err == nil {
			remoteSize = st.Size()
		}
		if remoteSize > size {
			_ = t.Remove(rem)
			remoteSize = 0
		}
		if remoteSize < size {
			lx.Debugf("raw: копия %s [%d..%d)", rel, remoteSize, size)
			if err := copyPrefix(t, rem, filepath.Join(localRoot, filepath.FromSlash(rel)), remoteSize); err != nil {
				return 0, err
			}
		}
		m.Files[rel] = size
	}
	// Зеркало: удаляем на target всё, чего уже нет локально (включая
	// active-snapshot, если сегмент удалён или переименован).
	for rel := range m.Files {
		if _, ok := safeRel(rel); !ok {
			return 0, fmt.Errorf("%w: небезопасный путь %q", errArchiveInvalid, rel)
		}
		if _, ok := existing[rel]; !ok {
			lx.Debugf("avg: обрезка зеркала, удалён %s", rel)
			_ = t.Remove(pathJoin(base, "files", rel))
			delete(m.Files, rel)
		}
	}
	if includeActive {
		m.Active = active
	} else if _, ok := m.Files[m.Active]; !ok {
		m.Active = ""
	}
	m.Updated = time.Now().Unix()
	m.Completed = true
	return writeJSONAtomic(t, pathJoin(base, "manifest.json"), &m)
}

func restoreAvg(t target, device, localRoot string, cancelled func() bool) (string, bool, error) {
	var m avgManifest
	ok, err := readJSON(t, pathJoin(device, "avg", "manifest.json"), &m)
	if err != nil || !ok || !m.Completed || len(m.Files) == 0 {
		return "", false, err
	}
	for rel := range m.Files {
		if _, ok := safeRel(rel); !ok {
			return "", false, fmt.Errorf("%w: небезопасный путь в avg manifest", errArchiveInvalid)
		}
		if _, err := t.Stat(pathJoin(device, "avg", "files", rel)); err != nil {
			return "", false, err
		}
	}
	if err := clearDir(localRoot); err != nil {
		return "", false, err
	}
	for rel := range m.Files {
		lx.Debugf("restore avg: %s", rel)
		if err := copyRemoteFile(t, pathJoin(device, "avg", "files", rel), filepath.Join(localRoot, filepath.FromSlash(rel)), cancelled); err != nil {
			return "", false, err
		}
	}
	return "avg", true, nil
}

func removeTree(t target, rel string) error {
	entries, err := t.ReadDir(rel)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		p := pathJoin(rel, e.Name())
		if e.IsDir() {
			if err := removeTree(t, p); err != nil {
				return err
			}
		} else if err := t.Remove(p); err != nil {
			return err
		}
	}
	return t.Remove(rel)
}

func pathJoin(parts ...string) string {
	out := ""
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p == "" {
			continue
		}
		if out == "" {
			out = p
		} else {
			out += "/" + p
		}
	}
	return out
}

func sortedFiles(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// avgHasData сообщает, есть ли на устройстве хотя бы один сегмент avg.
// Восстановление avg выполняется только при полном отсутствии локальных данных.
func avgHasData(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipAll
		}
		if !d.IsDir() && strings.HasSuffix(p, ".ts") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
