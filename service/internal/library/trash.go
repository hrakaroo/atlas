package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	trashWiki           = regexp.MustCompile(`!?\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	trashMarkdown       = regexp.MustCompile(`!?\[([^\]]+)\]\(([^\s)]+)\)`)
	trashDefinition     = regexp.MustCompile(`(?m)^ {0,3}\[([^\]]+)\]:\s*<?([^\s>]+)>?[^\n]*$`)
	trashReference      = regexp.MustCompile(`!?\[([^\]]+)\]\[([^\]]*)\]`)
	trashShortReference = regexp.MustCompile(`\[([^\]\n]+)\]`)
	transcriptNoise     = regexp.MustCompile(`^\s*(?:WEBVTT|\d+|\d{1,2}:\d{2}:\d{2}[.,]\d+\s*-->.*)\s*$`)
	transcriptTime      = regexp.MustCompile(`^\s*\[?\d{1,2}:\d{2}(?::\d{2})?\]?\s*`)
	transcriptWord      = regexp.MustCompile(`[\p{L}\p{N}_]+`)
	trashPreview        = regexp.MustCompile("[#*\\[\\]\x60_]")
)

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func transcriptWords(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n") {
		if transcriptNoise.MatchString(line) {
			continue
		}
		lines = append(lines, transcriptTime.ReplaceAllString(line, ""))
	}
	return transcriptWord.FindAllString(caseFold(strings.Join(lines, " ")), -1)
}
func (l *Library) itemSourceText(item Record) (string, error) {
	row, err := l.row("SELECT source_text FROM search WHERE item_id=?", item["id"])
	if err != nil {
		return "", err
	}
	if row != nil {
		return str(row, "source_text"), nil
	}
	row, err = l.row("SELECT payload FROM trash_entries WHERE item_id=?", item["id"])
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", nil
	}
	payload, err := parseJSON(row["payload"])
	return str(payload, "source_text"), err
}
func (l *Library) sourceIdentities(item Record) ([]string, error) {
	keys := map[string]bool{}
	key := str(item, "source_key")
	if key != "" {
		keys["key:"+key] = true
	}
	kind := str(item, "kind")
	if kind == "document" || kind == "reference" {
		ids := map[string]bool{}
		for _, link := range records(item["source_links"]) {
			if link["kind"] == "document" && truth(link["primary"]) {
				if id := googleDocID(str(link, "url")); id != "" {
					ids[id] = true
				}
			}
		}
		if len(ids) == 0 {
			for _, link := range records(item["source_links"]) {
				if link["kind"] == "document" {
					if id := googleDocID(str(link, "url")); id != "" {
						ids[id] = true
					}
				}
			}
		}
		if len(ids) == 1 {
			for id := range ids {
				keys["key:google-doc:"+id] = true
			}
		} else {
			urls, _ := stringsArg(mapping(item["metadata"])["doc_urls"])
			if len(urls) == 1 {
				if id := googleDocID(urls[0]); id != "" {
					keys["key:google-doc:"+id] = true
				}
			}
		}
	}
	if kind == "meeting" || kind == "one-on-one" || kind == "interview" {
		if truth(item["source_hash"]) {
			keys["hash:"+str(item, "source_hash")] = true
		}
		text, err := l.itemSourceText(item)
		if err != nil {
			return nil, err
		}
		if text != "" {
			keys["hash:"+hashText(text)] = true
			words := transcriptWords(text)
			if len(words) > 0 {
				keys["normalized:"+hashText(strings.Join(words, " "))] = true
			}
		}
	}
	result := []string{}
	for key := range keys {
		result = append(result, key)
	}
	return result, nil
}
func (l *Library) tombstoneItem(item Record) error {
	keys, err := l.sourceIdentities(item)
	if err != nil {
		return err
	}
	created := str(item, "deleted_at")
	if created == "" {
		created = l.now()
	}
	for _, identity := range keys {
		if err = l.exec("INSERT OR REPLACE INTO source_tombstones VALUES(?,?,?)", identity, item["id"], created); err != nil {
			return err
		}
	}
	return nil
}
func (l *Library) withoutLinksTo(summary, id string, includeDeleted bool) (string, error) {
	var failure error
	resolves := func(target string) bool {
		resolved, err := l.resolveTarget(target, includeDeleted)
		if err != nil {
			failure = err
		}
		return resolved == id
	}
	summary = trashWiki.ReplaceAllStringFunc(summary, func(text string) string {
		parts := trashWiki.FindStringSubmatch(text)
		if !resolves(parts[1]) {
			return text
		}
		if parts[2] != "" {
			return parts[2]
		}
		return strings.TrimSuffix(filepath.Base(strings.SplitN(parts[1], "#", 2)[0]), ".md")
	})
	summary = trashMarkdown.ReplaceAllStringFunc(summary, func(text string) string {
		parts := trashMarkdown.FindStringSubmatch(text)
		if resolves(parts[2]) {
			return parts[1]
		}
		return text
	})
	references := map[string]bool{}
	for _, parts := range trashDefinition.FindAllStringSubmatch(summary, -1) {
		if resolves(parts[2]) {
			references[caseFold(parts[1])] = true
			summary = strings.ReplaceAll(summary, parts[0], "")
		}
	}
	if len(references) > 0 {
		summary = trashReference.ReplaceAllStringFunc(summary, func(text string) string {
			parts := trashReference.FindStringSubmatch(text)
			key := parts[2]
			if key == "" {
				key = parts[1]
			}
			if references[caseFold(key)] {
				return parts[1]
			}
			return text
		})
		matches := trashShortReference.FindAllStringSubmatchIndex(summary, -1)
		var output strings.Builder
		last := 0
		for _, match := range matches {
			start, end := match[0], match[1]
			if start > 0 && summary[start-1] == '!' {
				continue
			}
			if end < len(summary) && (summary[end] == '[' || summary[end] == '(') {
				continue
			}
			label := summary[match[2]:match[3]]
			if references[caseFold(label)] {
				output.WriteString(summary[last:start])
				output.WriteString(label)
				last = end
			}
		}
		output.WriteString(summary[last:])
		summary = output.String()
	}
	return summary, failure
}
func (l *Library) DeletionPreview(id string) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	rows, err := l.rows("SELECT id FROM items WHERE deleted_at IS NULL AND id<>?", id)
	if err != nil {
		return nil, err
	}
	affected := []Record{}
	for _, row := range rows {
		other, e := l.Get(str(row, "id"), false, false)
		if e != nil {
			return nil, e
		}
		cleaned, e := l.withoutLinksTo(str(other, "summary"), id, false)
		if e != nil {
			return nil, e
		}
		if cleaned != other["summary"] {
			affected = append(affected, Record{"id": other["id"], "title": other["title"]})
		}
	}
	return Record{"id": id, "title": item["title"], "revision": item["revision"], "affected_notes": affected}, nil
}
func (l *Library) writeReplacement(item Record, summary string) error {
	if entityKind(str(item, "kind")) {
		if err := l.entityRevision(item); err != nil {
			return err
		}
		if err := l.rememberEntityDescription(str(item, "id"), summary, []byte(summary)); err != nil {
			return err
		}
	} else {
		folder := filepath.Join(l.Store.Root, "items", str(item, "id"))
		if err := l.atomic(filepath.Join(folder, "revisions", s(item["revision"])+".md"), str(item, "summary")); err != nil {
			return err
		}
		if err := l.atomic(filepath.Join(folder, "summary.md"), summary); err != nil {
			return err
		}
	}
	if err := l.exec("UPDATE items SET summary=?,revision=revision+1,updated=? WHERE id=?", summary, l.now(), item["id"]); err != nil {
		return err
	}
	if err := l.exec("UPDATE search SET summary=? WHERE item_id=?", summary, item["id"]); err != nil {
		return err
	}
	return l.indexLinks(str(item, "id"), summary)
}
func (l *Library) DeleteItem(id string, revision int64) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The note changed. Review it again before deleting.")
	}
	rows, err := l.rows("SELECT id FROM items WHERE deleted_at IS NULL AND id<>?", id)
	if err != nil {
		return nil, err
	}
	type replacement struct {
		item  Record
		after string
	}
	changes := []replacement{}
	inbound := []Record{}
	for _, row := range rows {
		other, e := l.Get(str(row, "id"), false, false)
		if e != nil {
			return nil, e
		}
		cleaned, e := l.withoutLinksTo(str(other, "summary"), id, false)
		if e != nil {
			return nil, e
		}
		if cleaned != other["summary"] {
			changes = append(changes, replacement{other, cleaned})
			inbound = append(inbound, Record{"id": other["id"], "before": other["summary"], "after": cleaned})
		}
	}
	text, err := l.itemSourceText(item)
	if err != nil {
		return nil, err
	}
	payload := Record{"source_text": text, "inbound": inbound}
	if err = l.beginTrashIntent(id, "delete", inbound); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		for _, change := range changes {
			if e := l.writeReplacement(change.item, change.after); e != nil {
				return e
			}
		}
		if e := l.tombstoneItem(item); e != nil {
			return e
		}
		if e := l.exec("UPDATE source_intake SET status='blocked',item_id=? WHERE source_key=? OR item_id=?", id, item["source_key"], id); e != nil {
			return e
		}
		if e := l.exec("INSERT INTO trash_entries VALUES(?,?)", id, jsonText(payload)); e != nil {
			return e
		}
		if e := l.exec("UPDATE items SET deleted_at=?,revision=revision+1 WHERE id=?", l.now(), id); e != nil {
			return e
		}
		if e := l.exec("DELETE FROM search WHERE item_id=?", id); e != nil {
			return e
		}
		if e := l.exec("DELETE FROM links WHERE item_id=?", id); e != nil {
			return e
		}
		if e := l.activityEvent(item, "removed", "", nil, false); e != nil {
			return e
		}
		return l.contractPoint("delete.before_commit")
	})
	if err != nil {
		if recoveryErr := l.recoverTrashIntent(id); recoveryErr != nil {
			return nil, errors.Join(err, recoveryErr)
		}
		return nil, err
	}
	if err = l.contractPoint("delete.after_commit"); err != nil {
		return nil, err
	}
	if err = l.recoverTrashIntent(id); err != nil {
		return nil, err
	}
	return Record{"id": id, "title": item["title"], "cleaned_notes": len(changes)}, nil
}
func (l *Library) RestoreItem(id string) (Record, error) {
	item, err := l.Get(id, true, false)
	if err != nil {
		return nil, err
	}
	if !truth(item["deleted_at"]) {
		return nil, errors.New("This note is not in Trash.")
	}
	row, err := l.row("SELECT payload FROM trash_entries WHERE item_id=?", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Trash recovery record is missing.")
	}
	var payload Record
	if err = json.Unmarshal([]byte(str(row, "payload")), &payload); err != nil {
		return nil, err
	}
	restored := str(item, "summary")
	deleted, err := l.rows("SELECT id FROM items WHERE deleted_at IS NOT NULL AND id<>?", id)
	if err != nil {
		return nil, err
	}
	for _, row := range deleted {
		restored, err = l.withoutLinksTo(restored, str(row, "id"), true)
		if err != nil {
			return nil, err
		}
	}
	type replacement struct {
		item  Record
		after string
	}
	changes := []replacement{}
	skipped := 0
	for _, inbound := range records(payload["inbound"]) {
		other, e := l.Get(str(inbound, "id"), false, false)
		if errors.Is(e, errItemNotFound) {
			skipped++
			continue
		}
		if e != nil {
			return nil, e
		}
		if other["summary"] == inbound["after"] {
			changes = append(changes, replacement{other, str(inbound, "before")})
		} else {
			skipped++
		}
	}
	replacements := []Record{}
	if restored != item["summary"] {
		replacements = append(replacements, Record{"id": id, "before": item["summary"], "after": restored})
	}
	for _, change := range changes {
		replacements = append(replacements, Record{"id": change.item["id"], "before": change.item["summary"], "after": change.after})
	}
	if err = l.beginTrashIntent(id, "restore", replacements); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		if e := l.exec("UPDATE items SET deleted_at=NULL,revision=revision+1 WHERE id=?", id); e != nil {
			return e
		}
		if e := l.exec("INSERT INTO search VALUES(?,?,?,?)", id, item["title"], restored, payload["source_text"]); e != nil {
			return e
		}
		if restored != item["summary"] {
			if e := l.writeReplacement(item, restored); e != nil {
				return e
			}
		} else if e := l.indexLinks(id, restored); e != nil {
			return e
		}
		for _, change := range changes {
			if e := l.writeReplacement(change.item, change.after); e != nil {
				return e
			}
		}
		if e := l.exec("DELETE FROM source_tombstones WHERE item_id=?", id); e != nil {
			return e
		}
		if e := l.exec("DELETE FROM trash_entries WHERE item_id=?", id); e != nil {
			return e
		}
		current, e := l.Get(id, false, false)
		if e != nil {
			return e
		}
		if e := l.activityEvent(current, "restored", "", nil, false); e != nil {
			return e
		}
		return l.contractPoint("restore.before_commit")
	})
	if err != nil {
		if recoveryErr := l.recoverTrashIntent(id); recoveryErr != nil {
			return nil, errors.Join(err, recoveryErr)
		}
		return nil, err
	}
	if err = l.contractPoint("restore.after_commit"); err != nil {
		return nil, err
	}
	if err = l.recoverTrashIntent(id); err != nil {
		return nil, err
	}
	return Record{"id": id, "restored_links_in_notes": len(changes), "unchanged_edited_notes": skipped}, nil
}
