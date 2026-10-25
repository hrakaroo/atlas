package library

import (
	"encoding/json"
	"errors"
)

// Tag preserves exact case-sensitive name/category identity. Entity-level name
// matching remains separate; legacy typed-tag triggers run in the shared store.
func (l *Library) Tag(name, category string) (string, error) {
	name = clean(name)
	if name == "" || (category != "team" && category != "project" && category != "person" && category != "type" && category != "topic") {
		return "", errors.New("Supply a tag name and a valid category.")
	}
	row, err := l.row("SELECT id FROM tags WHERE name=? AND category=?", name, category)
	if err != nil {
		return "", err
	}
	if row != nil {
		return str(row, "id"), nil
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	if err = l.exec("INSERT INTO tags VALUES(?,?,?)", id, name, category); err != nil {
		return "", err
	}
	return id, nil
}

func (l *Library) Tags() ([]Record, error) {
	tags, err := l.rows("SELECT t.*,count(i.id) AS count FROM tags t LEFT JOIN item_tags it ON it.tag_id=t.id LEFT JOIN items i ON i.id=it.item_id AND i.deleted_at IS NULL GROUP BY t.id ORDER BY t.category,t.name")
	if err != nil {
		return nil, err
	}
	rows, err := l.rows("SELECT i.title,i.metadata FROM items i WHERE i.kind='person' AND i.deleted_at IS NULL AND (i.source_key LIKE 'interview-person:%' OR EXISTS(SELECT 1 FROM item_tags it JOIN tags t ON t.id=it.tag_id WHERE it.item_id=i.id AND t.category='topic' AND lower(t.name)='interview candidate'))")
	if err != nil {
		return nil, err
	}
	candidates := map[string]bool{}
	for _, row := range rows {
		metadata, e := parseJSON(row["metadata"])
		if e != nil {
			return nil, e
		}
		name := str(metadata, "obsidian_stem")
		if name == "" {
			name = str(row, "title")
		}
		candidates[caseFold(clean(name))] = true
	}
	for _, tag := range tags {
		tag["interview_candidate"] = str(tag, "category") == "person" && candidates[caseFold(clean(str(tag, "name")))]
	}
	return tags, nil
}

func (l *Library) ApplyTags(ids []string, tag string, remove bool) (Record, error) {
	row, err := l.row("SELECT 1 FROM tags WHERE id=?", tag)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Tag not found.")
	}
	if len(ids) == 0 {
		return nil, errors.New("Select an item.")
	}
	changes := []Record{}
	err = l.transaction(func() error {
		for _, id := range ids {
			if _, e := l.Get(id, false, false); e != nil {
				return e
			}
			existing, e := l.row("SELECT 1 FROM item_tags WHERE item_id=? AND tag_id=?", id, tag)
			if e != nil {
				return e
			}
			present := existing != nil
			if present != !remove {
				changes = append(changes, Record{"item_id": id, "tag_id": tag, "was_present": present})
			}
			if remove {
				e = l.exec("DELETE FROM item_tags WHERE item_id=? AND tag_id=?", id, tag)
			} else {
				e = l.exec("INSERT OR IGNORE INTO item_tags VALUES(?,?)", id, tag)
			}
			if e != nil {
				return e
			}
		}
		if len(changes) > 0 {
			return l.exec("INSERT INTO history(item_id,action,payload,created) VALUES(NULL,'tags',?,?)", jsonText(changes), l.now())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Record{"changed": len(changes)}, nil
}

// Undo intentionally restores tag batch history only. Trash uses RestoreItem.
func (l *Library) Undo() (Record, error) {
	row, err := l.row("SELECT * FROM history WHERE action='tags' ORDER BY id DESC LIMIT 1")
	if err != nil {
		return nil, err
	}
	if row == nil {
		return Record{"changed": 0}, nil
	}
	changes := []Record{}
	if err = json.Unmarshal([]byte(str(row, "payload")), &changes); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		for _, change := range changes {
			if truth(change["was_present"]) {
				if e := l.exec("INSERT OR IGNORE INTO item_tags VALUES(?,?)", change["item_id"], change["tag_id"]); e != nil {
					return e
				}
			} else {
				if e := l.exec("DELETE FROM item_tags WHERE item_id=? AND tag_id=?", change["item_id"], change["tag_id"]); e != nil {
					return e
				}
			}
		}
		return l.exec("DELETE FROM history WHERE id=?", row["id"])
	})
	if err != nil {
		return nil, err
	}
	return Record{"changed": len(changes)}, nil
}
