package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RecoverPendingTrash repairs interrupted multi-record summary replacements.
// The target's SQLite deleted state is the durable operation commit marker.
func (l *Library) RecoverPendingTrash() error {
	if err := l.Store.Lock(); err != nil {
		return err
	}
	defer l.Store.Unlock()
	rows, err := l.rows("SELECT id FROM items")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err = l.recoverTrashIntent(str(row, "id")); err != nil {
			return err
		}
	}
	return nil
}
func (l *Library) trashIntentPath(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return "", errors.New("Invalid trash recovery identity")
	}
	path := filepath.Join(l.Store.Root, "items", id, "pending-trash.json")
	if err := l.safePath(path, true); err != nil {
		return "", err
	}
	return path, nil
}
func (l *Library) beginTrashIntent(id, operation string, changes []Record) error {
	path, err := l.trashIntentPath(id)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); err == nil {
		return errors.New("Pending trash recovery requires review")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return l.atomic(path, jsonText(Record{"version": 1, "operation": operation, "changes": changes}))
}
func (l *Library) recoverTrashIntent(id string) (err error) {
	defer func() {
		if err != nil {
			l.recoveryErr = err
		}
	}()
	path, err := l.trashIntentPath(id)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
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
	version, versionOK := intent["version"].(float64)
	operation := str(intent, "operation")
	changes, changesOK := intent["changes"].([]any)
	if !versionOK || version != 1 || (operation != "delete" && operation != "restore") || !changesOK {
		return errors.New("Invalid pending trash intent; review required")
	}
	row, err := l.row("SELECT deleted_at FROM items WHERE id=?", id)
	if err != nil {
		return err
	}
	if row == nil {
		return errors.New("Missing trash recovery record")
	}
	committed := truth(row["deleted_at"]) == (operation == "delete")
	type replacement struct{ path, text string }
	prepared := []replacement{}
	seen := map[string]bool{}
	for _, value := range changes {
		change, ok := value.(map[string]any)
		if !ok {
			return errors.New("Invalid pending trash replacement")
		}
		other, idOK := change["id"].(string)
		before, beforeOK := change["before"].(string)
		after, afterOK := change["after"].(string)
		if !idOK || !beforeOK || !afterOK || seen[other] {
			return errors.New("Invalid pending trash replacement")
		}
		seen[other] = true
		otherIntent, err := l.trashIntentPath(other)
		if err != nil {
			return err
		}
		summary := filepath.Join(filepath.Dir(otherIntent), "summary.md")
		if err = l.safePath(summary, true); err != nil {
			return err
		}
		if row, err := l.row("SELECT id FROM items WHERE id=?", other); err != nil {
			return err
		} else if row == nil {
			return errors.New("Missing trash recovery replacement record")
		}
		current := ""
		exists := false
		if _, err = os.Lstat(summary); err == nil {
			current, err = l.readText(summary)
			if err != nil {
				return err
			}
			exists = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if exists && current != before && current != after {
			return errors.New("Pending trash conflicts with a manual summary; review required")
		}
		desired := before
		if committed {
			desired = after
		}
		prepared = append(prepared, replacement{summary, desired})
	}
	// Validate all entries before repairing any neighboring summary.
	for _, change := range prepared {
		if err = l.atomic(change.path, change.text); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
