package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RecoverPendingRefreshes runs under the shared writer lock before serving any
// reads. Both backends understand this intent; it bridges file/SQLite commits.
func (l *Library) RecoverPendingRefreshes() error {
	if err := l.Store.Lock(); err != nil {
		return err
	}
	defer l.Store.Unlock()
	rows, err := l.rows("SELECT id FROM items")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := l.recoverPendingRefresh(str(row, "id")); err != nil {
			return err
		}
	}
	return nil
}
func (l *Library) recoverPendingRefresh(id string) (err error) {
	defer func() {
		if err != nil {
			l.recoveryErr = err
		}
	}()
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return errors.New("Invalid refresh recovery identity")
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	path := filepath.Join(folder, "pending-refresh.json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := l.readText(path)
	if err != nil {
		return err
	}
	var intent Record
	if err = json.Unmarshal([]byte(raw), &intent); err != nil {
		return err
	}
	name := str(intent, "snapshot")
	old, oldOK := intent["old_summary"].(string)
	next, nextOK := intent["new_summary"].(string)
	version, versionOK := intent["version"].(float64)
	if !versionOK || version != 1 || !oldOK || !nextOK ||
		filepath.Base(name) != name || !strings.HasPrefix(name, "google-refresh-") || !strings.HasSuffix(name, ".txt") {
		return errors.New("Invalid pending refresh intent; review required")
	}
	snapshot := filepath.Join(folder, "originals", name)
	row, err := l.row("SELECT summary,source_path FROM items WHERE id=?", id)
	if err != nil {
		return err
	}
	if row == nil {
		return errors.New("Missing refresh recovery record")
	}
	committed := str(row, "source_path") == snapshot && str(row, "summary") == next
	if !committed {
		summaryPath := filepath.Join(folder, "summary.md")
		current, err := l.readText(summaryPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && current != old && current != next {
			return errors.New("Pending refresh conflicts with a manual summary; review required")
		}
		if err := l.atomic(summaryPath, old); err != nil {
			return err
		}
		if err := l.safePath(snapshot, true); err != nil {
			return err
		}
		if err := os.Remove(snapshot); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Remove(path)
}
