package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func entityKind(kind string) bool { return kind == "person" || kind == "team" || kind == "project" }
func historyID(kind, relative string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + filepath.ToSlash(relative)))
	return hex.EncodeToString(digest[:])
}
func (l *Library) databaseEntity(id string) (bool, error) {
	row, err := l.row("SELECT item_id FROM entity_storage WHERE item_id=?", id)
	return row != nil, err
}
func (l *Library) saveEntitySnapshot(id string, entry Record, content []byte) error {
	return l.exec("INSERT OR IGNORE INTO entity_snapshots VALUES(?,?,?,?)", id, entry["id"], jsonText(entry), content)
}
func (l *Library) entityDescriptionBytes(item Record) ([]byte, error) {
	row, err := l.row("SELECT summary,content FROM entity_description_bytes WHERE item_id=?", item["id"])
	if err != nil {
		return nil, err
	}
	if row != nil && row["summary"] == item["summary"] {
		return []byte(str(row, "content")), nil
	}
	return []byte(str(item, "summary")), nil
}
func (l *Library) rememberEntityDescription(id, summary string, content []byte) error {
	return l.exec("INSERT OR REPLACE INTO entity_description_bytes VALUES(?,?,?)", id, summary, content)
}
func (l *Library) entityRevision(item Record) error {
	relative := filepath.Join("items", str(item, "id"), "revisions", fmt.Sprint(item["revision"])+".md")
	content, err := l.entityDescriptionBytes(item)
	if err != nil {
		return err
	}
	return l.saveEntitySnapshot(str(item, "id"), Record{"id": historyID("summary", relative), "name": filepath.Base(relative), "kind": "summary", "revision": item["revision"], "current": false}, content)
}
func (l *Library) entityHistory(row Record) ([]historyEntry, error) {
	id := str(row, "id")
	relative := filepath.Join("items", id, "summary.md")
	content, err := l.entityDescriptionBytes(row)
	if err != nil {
		return nil, err
	}
	result := []historyEntry{{entry: Record{"id": historyID("summary", relative), "name": "summary.md", "kind": "summary", "revision": row["revision"], "current": true}, data: content, database: true}}
	rows, err := l.rows("SELECT entry,content FROM entity_snapshots WHERE item_id=? ORDER BY entry_id", id)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		entry, e := parseJSON(r["entry"])
		if e != nil {
			return nil, e
		}
		result = append(result, historyEntry{entry: entry, data: []byte(str(r, "content")), database: true})
	}
	return result, nil
}
func (l *Library) entityOriginal(id string) ([]byte, error) {
	rows, err := l.rows("SELECT entry,content FROM entity_snapshots WHERE item_id=?", id)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		e, err := parseJSON(row["entry"])
		if err != nil {
			return nil, err
		}
		if e["kind"] == "original" && truth(e["current"]) {
			return []byte(str(row, "content")), nil
		}
	}
	return nil, errors.New("No preserved source available.")
}
func (l *Library) updateEntitySummary(item Record, summary string) (Record, error) {
	id := str(item, "id")
	err := l.transaction(func() error {
		if err := l.entityRevision(item); err != nil {
			return err
		}
		if err := l.contractPoint("summary.before_write"); err != nil {
			return err
		}
		if err := l.exec("UPDATE items SET summary=?,revision=revision+1,updated=? WHERE id=?", summary, l.now(), id); err != nil {
			return err
		}
		if err := l.rememberEntityDescription(id, summary, []byte(summary)); err != nil {
			return err
		}
		if err := l.exec("UPDATE search SET summary=? WHERE item_id=?", summary, id); err != nil {
			return err
		}
		if err := l.indexLinks(id, summary); err != nil {
			return err
		}
		if summary != item["summary"] {
			current, err := l.Get(id, false, false)
			if err != nil {
				return err
			}
			if err = l.activityEvent(current, "summary_updated", "", nil, false); err != nil {
				return err
			}
		}
		return l.contractPoint("summary.before_commit")
	})
	if err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}

// MigrateOwnedStorage is executed by the sole owner after recovery, before serving.
// Entity copies commit to SQLite before files are removed. Other imported sources
// move to per-item originals/attachments; all writes are verified before repointing.
func (l *Library) MigrateOwnedStorage() error {
	if err := l.Store.Lock(); err != nil {
		return err
	}
	defer l.Store.Unlock()
	rows, err := l.rows("SELECT * FROM items ORDER BY id")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if entityKind(str(row, "kind")) {
			if err = l.migrateEntity(row); err != nil {
				return fmt.Errorf("Entity storage migration %s: %w", row["id"], err)
			}
		} else {
			if err = l.ownImportedSource(row); err != nil {
				return fmt.Errorf("Source storage migration %s: %w", row["id"], err)
			}
		}
	}
	return nil
}
func (l *Library) migrateEntity(row Record) error {
	id := str(row, "id")
	meta, err := parseJSON(row["metadata"])
	if err != nil {
		return err
	}
	migrated, err := l.databaseEntity(id)
	if err != nil {
		return err
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	assets := map[string][]byte{}
	if _, err = os.Lstat(folder); err == nil {
		if err = l.safePath(folder, false); err != nil {
			return err
		}
		files, e := walkFiles(folder)
		if e != nil {
			return e
		}
		for _, path := range files {
			b, e := l.readBytes(path)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(folder, path)
			assets["item/"+filepath.ToSlash(rel)] = b
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Already migrated descriptions never read leftover files as authoritative.
	if migrated {
		for path, data := range assets {
			saved, e := l.row("SELECT content FROM entity_assets WHERE item_id=? AND path=?", id, path)
			if e != nil {
				return e
			}
			if saved == nil || str(saved, "content") != string(data) {
				return errors.New("Unexpected entity files require review before cleanup")
			}
		}
		if len(assets) > 0 {
			return os.RemoveAll(folder)
		}
		return nil
	}
	snapshots := []historyEntry{}
	if _, e := os.Stat(filepath.Join(folder, "summary.md")); e == nil {
		snapshots, err = l.fileHistoryPaths(id)
		if err != nil {
			return err
		}
	} else {
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if path := str(row, "source_path"); path != "" {
			relative, _ := filepath.Rel(l.Store.Root, path)
			snapshots = append(snapshots, historyEntry{entry: Record{"id": historyID("original", relative), "kind": "original", "name": filepath.Base(path), "revision": nil, "current": true}, path: path})
		}
	}
	for i := range snapshots {
		snapshots[i].data, err = l.readBytes(snapshots[i].path)
		if err != nil {
			return err
		}
	}
	if snapshot := str(meta, "snapshot_root"); snapshot != "" {
		if err = l.safePath(snapshot, false); err != nil {
			return err
		}
		paths, e := walkFiles(snapshot)
		if e != nil {
			return e
		}
		for _, path := range paths {
			b, e := l.readBytes(path)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(snapshot, path)
			assets["snapshot/"+filepath.ToSlash(rel)] = b
		}
	}
	summary := str(row, "summary")
	if _, ok := assets["item/summary.md"]; ok {
		summary, err = l.readText(filepath.Join(folder, "summary.md"))
		if err != nil {
			return err
		}
	}
	delete(meta, "snapshot_root")
	changed := summary != str(row, "summary")
	err = l.transaction(func() error {
		if changed {
			if e := l.entityRevision(row); e != nil {
				return e
			}
		}
		if source := str(row, "source_path"); source != "" {
			if e := l.exec("INSERT OR IGNORE INTO source_path_aliases VALUES(?,?)", id, source); e != nil {
				return e
			}
		}
		if data, ok := assets["item/summary.md"]; ok {
			if e := l.rememberEntityDescription(id, summary, data); e != nil {
				return e
			}
		}
		for _, entry := range snapshots {
			if entry.entry["kind"] == "summary" && truth(entry.entry["current"]) {
				continue
			}
			if e := l.saveEntitySnapshot(id, entry.entry, entry.data); e != nil {
				return e
			}
		}
		for path, data := range assets {
			if e := l.exec("INSERT INTO entity_assets VALUES(?,?,?)", id, path, data); e != nil {
				return e
			}
		}
		delta := 0
		if changed {
			delta = 1
		}
		if e := l.exec("UPDATE items SET summary=?,source_path=NULL,metadata=?,revision=revision+? WHERE id=?", summary, jsonText(meta), delta, id); e != nil {
			return e
		}
		if e := l.exec("UPDATE search SET summary=? WHERE item_id=?", summary, id); e != nil {
			return e
		}
		if !truth(row["deleted_at"]) {
			if e := l.indexLinks(id, summary); e != nil {
				return e
			}
		}
		return l.exec("INSERT INTO entity_storage VALUES(?)", id)
	})
	if err != nil {
		return err
	}
	if err = l.contractPoint("storage.entity.after_commit"); err != nil {
		return err
	}
	// Verify the actual persisted bytes before deleting a redundant file tree.
	for path, data := range assets {
		saved, e := l.row("SELECT content FROM entity_assets WHERE item_id=? AND path=?", id, path)
		if e != nil {
			return e
		}
		if saved == nil || str(saved, "content") != string(data) {
			return errors.New("Entity content verification failed")
		}
	}
	if _, e := os.Stat(folder); e == nil {
		return os.RemoveAll(folder)
	}
	return nil
}
func (l *Library) verifiedCopy(path string, content []byte) error {
	if err := l.safePath(path, true); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		saved, e := l.readBytes(path)
		if e != nil {
			return e
		}
		if string(saved) != string(content) {
			return errors.New("Owned source conflicts with an existing file")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := l.atomicBytes(path, content); err != nil {
		return err
	}
	saved, err := l.readBytes(path)
	if err != nil {
		return err
	}
	if string(saved) != string(content) {
		return errors.New("Owned source verification failed")
	}
	return nil
}
func (l *Library) ownImportedSource(row Record) error {
	id := str(row, "id")
	meta, err := parseJSON(row["metadata"])
	if err != nil {
		return err
	}
	imports := filepath.Join(l.Store.Root, "imports") + string(os.PathSeparator)
	source := str(row, "source_path")
	snapshot := str(meta, "snapshot_root")
	sourceChanged := strings.HasPrefix(source, imports)
	snapshotChanged := strings.HasPrefix(snapshot, imports)
	if !sourceChanged && !snapshotChanged {
		return nil
	}
	owned := source
	if sourceChanged {
		data, e := l.readBytes(source)
		if e != nil {
			return e
		}
		owned = filepath.Join(l.Store.Root, "items", id, "originals", filepath.Base(source))
		if e = l.verifiedCopy(owned, data); e != nil {
			return e
		}
	}
	if snapshotChanged {
		if err = l.safePath(snapshot, false); err != nil {
			return err
		}
		paths, e := walkFiles(snapshot)
		if e != nil {
			return e
		}
		target := filepath.Join(l.Store.Root, "items", id, "attachments")
		for _, path := range paths {
			data, e := l.readBytes(path)
			if e != nil {
				return e
			}
			relative, _ := filepath.Rel(snapshot, path)
			if e = l.verifiedCopy(filepath.Join(target, relative), data); e != nil {
				return e
			}
		}
		meta["snapshot_root"] = target
	}
	return l.transaction(func() error {
		if sourceChanged {
			if e := l.exec("INSERT OR IGNORE INTO source_path_aliases VALUES(?,?)", id, source); e != nil {
				return e
			}
			relative, _ := filepath.Rel(l.Store.Root, source)
			if e := l.exec("INSERT OR IGNORE INTO source_history_aliases VALUES(?,?,?)", id, owned, historyID("original", relative)); e != nil {
				return e
			}
		}
		return l.exec("UPDATE items SET source_path=?,metadata=? WHERE id=?", owned, jsonText(meta), id)
	})
}
