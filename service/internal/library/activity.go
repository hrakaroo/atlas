package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func records(value any) []Record {
	switch x := value.(type) {
	case []Record:
		return x
	case []any:
		result := []Record{}
		for _, v := range x {
			if r, ok := v.(map[string]any); ok {
				result = append(result, r)
			}
		}
		return result
	default:
		return []Record{}
	}
}
func mapping(value any) Record {
	if x, ok := value.(map[string]any); ok {
		return x
	}
	return Record{}
}
func activityContentHash(item Record) string {
	sum := sha256.Sum256([]byte(jsonText([]any{item["title"], item["summary"], item["source_hash"]})))
	return hex.EncodeToString(sum[:])
}
func (l *Library) rememberActivityState(id string) error {
	current, err := l.row("SELECT title,summary,source_hash,deleted_at FROM items WHERE id=?", id)
	if err != nil {
		return err
	}
	return l.exec("INSERT OR REPLACE INTO activity_state VALUES(?,?,?)", id, activityContentHash(current), current["deleted_at"])
}
func (l *Library) activityEvent(item Record, action, created string, eventKey any, historical bool) error {
	category := "Material"
	key := str(item, "source_key")
	kind := str(item, "kind")
	switch {
	case kind == "blog":
		feed, err := l.row("SELECT f.name FROM blog_feeds f JOIN blog_members m ON m.feed_id=f.id WHERE m.item_id=?", item["id"])
		if err != nil {
			return err
		}
		name := "AI"
		if feed != nil {
			name = str(feed, "name")
		}
		category = name + " feed"
	case strings.HasPrefix(key, "zoom-transcript:"):
		category = "Zoom transcript"
	case strings.HasPrefix(key, "google-doc:"):
		category = "Google Doc"
	default:
		for _, link := range records(item["source_links"]) {
			if link["kind"] == "document" {
				category = "Google Doc"
				break
			}
		}
		if category == "Material" {
			switch {
			case truth(item["is_interview_candidate"]) || strings.HasPrefix(key, "interview-person:"):
				category = "Candidate"
			case kind == "person":
				category = "Person"
			case kind == "project":
				category = "Project"
			case kind == "interview":
				category = "Interview"
			}
		}
	}
	if created == "" {
		created = l.now()
	}
	if err := l.exec("INSERT OR IGNORE INTO activity_events(event_key,item_id,title,kind,category,action,created,historical) VALUES(?,?,?,?,?,?,?,?)", eventKey, item["id"], item["title"], kind, category, action, created, boolInt(historical)); err != nil {
		return err
	}
	if kind == "document" || kind == "reference" {
		sync := mapping(mapping(item["metadata"])["google_sync"])
		digest := sync["content_hash"]
		if !truth(digest) && truth(item["source_hash"]) {
			source, err := l.row("SELECT source_text FROM search WHERE item_id=?", item["id"])
			if err != nil {
				return err
			}
			digest = item["source_hash"]
			if source != nil {
				digest = documentHash(str(source, "source_text"))
			}
		}
		previous, err := l.row("SELECT content_hash FROM document_source_state WHERE item_id=?", item["id"])
		if err != nil {
			return err
		}
		if (action == "refreshed" || action == "updated") && previous != nil && truth(previous["content_hash"]) && truth(digest) && previous["content_hash"] != digest && truth(item["starred"]) && !truth(item["deleted_at"]) {
			if err = l.exec("INSERT INTO document_changes(item_id,content_hash,detected_at,source_modified_at) VALUES(?,?,?,?)", item["id"], digest, created, sync["modified_time"]); err != nil {
				return err
			}
		}
		if err = l.exec("INSERT OR REPLACE INTO document_source_state VALUES(?,?)", item["id"], digest); err != nil {
			return err
		}
	}
	return l.rememberActivityState(str(item, "id"))
}
func (l *Library) Activity(limit int64, before any) (Record, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("Activity limit must be between 1 and 200.")
	}
	var cursor int64
	if before != nil {
		var err error
		cursor, err = integer(before)
		if err != nil || cursor <= 0 {
			return nil, errors.New("Invalid activity cursor.")
		}
	}
	missing, err := l.rows("SELECT id FROM items WHERE id NOT IN (SELECT item_id FROM activity_events WHERE action='imported') ORDER BY created,id")
	if err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		for _, row := range missing {
			item, e := l.Get(str(row, "id"), true, true)
			if e != nil {
				return e
			}
			if e = l.activityEvent(item, "imported", str(item, "created"), "import:"+str(item, "id"), true); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	all, err := l.rows("SELECT id FROM items")
	if err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		for _, row := range all {
			item, e := l.Get(str(row, "id"), true, true)
			if e != nil {
				return e
			}
			previous, e := l.row("SELECT * FROM activity_state WHERE item_id=?", item["id"])
			if e != nil {
				return e
			}
			if previous == nil {
				if e = l.rememberActivityState(str(item, "id")); e != nil {
					return e
				}
			} else if truth(previous["deleted_at"]) != truth(item["deleted_at"]) {
				action := "restored"
				if truth(item["deleted_at"]) {
					action = "removed"
				}
				if e = l.activityEvent(item, action, "", nil, false); e != nil {
					return e
				}
			} else if str(previous, "content_hash") != activityContentHash(item) {
				if e = l.activityEvent(item, "updated", "", nil, false); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	where := ""
	args := []any{}
	if before != nil {
		row, e := l.row("SELECT created FROM activity_events WHERE id=?", cursor)
		if e != nil {
			return nil, e
		}
		if row == nil {
			return nil, errors.New("Activity cursor not found.")
		}
		where = "WHERE (a.created<? OR (a.created=? AND a.id<?))"
		args = append(args, row["created"], row["created"], cursor)
	}
	args = append(args, limit+1)
	events, err := l.rows("SELECT a.*,i.deleted_at FROM activity_events a LEFT JOIN items i ON i.id=a.item_id "+where+" ORDER BY a.created DESC,a.id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	hasMore := int64(len(events)) > limit
	var next any
	if hasMore {
		events = events[:limit]
		next = events[len(events)-1]["id"]
	}
	return Record{"events": events, "has_more": hasMore, "next_before_id": next}, nil
}
func (l *Library) ChangedDocuments(args Record) ([]Record, error) {
	argsCopy := Record{}
	for k, v := range args {
		argsCopy[k] = v
	}
	argsCopy["kind"] = "document"
	argsCopy["starred_only"] = true
	argsCopy["stars_first"] = false
	if _, ok := argsCopy["search_scope"]; !ok {
		argsCopy["search_scope"] = "title"
	}
	items, err := l.Listing(argsCopy)
	if err != nil {
		return nil, err
	}
	changes, err := l.rows("SELECT c.item_id,max(c.id) AS latest_change_id,max(c.detected_at) AS changed_at,count(*) AS update_count,sum(CASE WHEN c.id>COALESCE(s.through_id,0) THEN 1 ELSE 0 END) AS unseen_updates FROM document_changes c LEFT JOIN document_changes_seen s ON s.item_id=c.item_id GROUP BY c.item_id")
	if err != nil {
		return nil, err
	}
	byID := map[string]Record{}
	for _, row := range changes {
		byID[str(row, "item_id")] = row
	}
	result := []Record{}
	for _, item := range items {
		if change, ok := byID[str(item, "id")]; ok {
			for k, v := range change {
				item[k] = v
			}
			result = append(result, item)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a["changed_at"] != b["changed_at"] {
			return str(a, "changed_at") > str(b, "changed_at")
		}
		return number(a["latest_change_id"]) > number(b["latest_change_id"])
	})
	return result, nil
}
func (l *Library) MarkDocumentChangesSeen(id string, through int64) (Record, error) {
	if _, err := l.Get(id, false, true); err != nil {
		return nil, err
	}
	row, err := l.row("SELECT id FROM document_changes WHERE item_id=? AND id=?", id, through)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Document update not found.")
	}
	err = l.transaction(func() error {
		return l.exec("INSERT INTO document_changes_seen VALUES(?,?) ON CONFLICT(item_id) DO UPDATE SET through_id=max(through_id,excluded.through_id)", id, through)
	})
	return Record{"item_id": id, "through_id": through}, err
}
func (l *Library) reconcileSummaryFiles() error {
	rows, err := l.rows("SELECT id,summary FROM items WHERE deleted_at IS NULL")
	if err != nil {
		return err
	}
	for _, row := range rows {
		id := str(row, "id")
		path := filepath.Join(l.Store.Root, "items", id, "summary.md")
		if err = l.safePath(path, true); err != nil {
			return err
		}
		info, e := os.Stat(path)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		current := stamp{Mtime: info.ModTime().UnixNano(), Size: info.Size()}
		if l.summaryStamps[id] == current {
			continue
		}
		text, e := l.readText(path)
		if e != nil {
			return e
		}
		if text != str(row, "summary") {
			if e = l.transaction(func() error {
				if e := l.exec("UPDATE items SET summary=?,revision=revision+1 WHERE id=?", text, id); e != nil {
					return e
				}
				if e := l.exec("UPDATE search SET summary=? WHERE item_id=?", text, id); e != nil {
					return e
				}
				return l.indexLinks(id, text)
			}); e != nil {
				return e
			}
		}
		l.summaryStamps[id] = current
	}
	return nil
}
func (l *Library) Changes() (Record, error) {
	if err := l.reconcileSummaryFiles(); err != nil {
		return nil, err
	}
	external, err := l.row("PRAGMA data_version")
	if err != nil {
		return nil, err
	}
	local, err := l.row("SELECT total_changes() AS local")
	if err != nil {
		return nil, err
	}
	return Record{"external": external["data_version"], "local": local["local"]}, nil
}
