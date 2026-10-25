package library

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (l *Library) syncTombstones() (Record, error) {
	rows, err := l.rows("SELECT id FROM items WHERE deleted_at IS NOT NULL")
	if err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		for _, row := range rows {
			item, e := l.Get(str(row, "id"), true, false)
			if e != nil {
				return e
			}
			if e = l.tombstoneItem(item); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	row, err := l.row("SELECT count(DISTINCT item_id) AS count FROM source_tombstones")
	if err != nil {
		return nil, err
	}
	return Record{"protected_items": row["count"]}, nil
}

var legacyIgnore = map[string]bool{"interview": true, "values": true, "engineering": true, "manager": true, "software": true, "engineer": true, "senior": true, "staff": true, "distributed": true, "systems": true, "noram": true, "emea": true, "transcript": true, "meeting": true, "saved": true, "closed": true, "caption": true, "google": true, "docs": true, "roundup": true, "for": true}

func nameWords(title string) map[string]bool {
	words := map[string]bool{}
	for _, word := range transcriptWord.FindAllString(caseFold(title), -1) {
		if !legacyIgnore[word] {
			words[word] = true
		}
	}
	return words
}
func allDigits(text string) bool {
	for _, r := range text {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return text != ""
}
func (l *Library) matchIntake(source Record) (string, any, error) {
	key, text, kind := str(source, "source_key"), str(source, "original_text"), str(source, "type")
	identities := map[string]bool{"key:" + key: true}
	if kind == "zoom" && text != "" {
		identities["hash:"+hashText(text)] = true
		words := transcriptWords(text)
		if len(words) > 0 {
			identities["normalized:"+hashText(strings.Join(words, " "))] = true
		}
	}
	// Stable traversal removes map iteration nondeterminism when several identities
	// point at the same removed session; IDs are never merged on partial evidence.
	keys := []string{}
	for key := range identities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, identity := range keys {
		row, err := l.row("SELECT item_id FROM source_tombstones WHERE identity=?", identity)
		if err != nil {
			return "", nil, err
		}
		if row != nil {
			return "blocked", row["item_id"], nil
		}
	}
	row, err := l.row("SELECT id,deleted_at,source_hash FROM items WHERE source_key=?", key)
	if err != nil {
		return "", nil, err
	}
	if row != nil {
		if truth(row["deleted_at"]) {
			return "blocked", row["id"], nil
		}
		if kind == "zoom" && truth(row["source_hash"]) && row["source_hash"] != hashText(text) {
			return "changed", row["id"], nil
		}
		return "duplicate", row["id"], nil
	}
	query := "SELECT id FROM items WHERE kind IN ('meeting','one-on-one','interview')"
	if kind == "google_doc" {
		query = "SELECT id FROM items WHERE kind IN ('document','reference')"
	}
	rows, err := l.rows(query)
	if err != nil {
		return "", nil, err
	}
	incoming := transcriptWords(text)
	possible := []any{}
	for _, row := range rows {
		item, e := l.Get(str(row, "id"), true, false)
		if e != nil {
			return "", nil, e
		}
		itemKeys, e := l.sourceIdentities(item)
		if e != nil {
			return "", nil, e
		}
		matches := false
		if kind == "google_doc" {
			for _, identity := range itemKeys {
				if identity == "key:"+key {
					matches = true
				}
			}
		} else {
			for _, identity := range itemKeys {
				if identities[identity] {
					matches = true
					break
				}
			}
		}
		if matches {
			status := "duplicate"
			if truth(item["deleted_at"]) {
				status = "blocked"
			}
			return status, item["id"], nil
		}
		if kind == "google_doc" || !truth(source["date"]) || item["date"] != source["date"] {
			continue
		}
		names, known := nameWords(str(source, "title")), nameWords(str(item, "title"))
		overlap := 0
		for word := range names {
			if known[word] && utf8.RuneCountInString(word) > 2 && !allDigits(word) {
				overlap++
			}
		}
		if overlap < 2 {
			continue
		}
		old, e := l.itemSourceText(item)
		if e != nil {
			return "", nil, e
		}
		existing := strings.Join(transcriptWords(old), " ")
		if len(incoming) >= 60 {
			offsets := []int{len(incoming) / 4, len(incoming) / 2, max(0, len(incoming)-30)}
			hits := 0
			for _, offset := range offsets {
				end := min(len(incoming), offset+20)
				if strings.Contains(existing, strings.Join(incoming[offset:end], " ")) {
					hits++
				}
			}
			if hits >= 2 {
				status := "duplicate"
				if truth(item["deleted_at"]) {
					status = "blocked"
				}
				return status, item["id"], nil
			}
		}
		possible = append(possible, item["id"])
	}
	if len(possible) > 0 {
		return "review", possible[0], nil
	}
	return "pending", nil, nil
}
func (l *Library) SubmitSources(sources []Record) (Record, error) {
	if _, err := l.syncTombstones(); err != nil {
		return nil, err
	}
	result := []Record{}
	pending := 0
	for _, candidate := range sources {
		source := Record{}
		for key, value := range candidate {
			source[key] = value
		}
		kind := str(source, "type")
		switch kind {
		case "google_doc":
			urls := normalizedDocURLs([]string{str(source, "url")})
			id := ""
			if len(urls) > 0 {
				id = googleDocID(urls[0])
			}
			title := strings.Join(strings.Fields(str(source, "title")), " ")
			if id == "" || id == "e" || title == "" || strings.Contains(caseFold(title), "untitled document") {
				result = append(result, Record{"status": "filtered", "title": title})
				continue
			}
			source["source_key"] = "google-doc:" + id
			source["file_id"] = id
			source["url"] = urls[0]
			source["title"] = title
		case "zoom":
			if !strings.HasPrefix(str(source, "source_key"), "zoom-transcript:") || strings.TrimSpace(str(source, "original_text")) == "" {
				return nil, errors.New("Zoom source requires a relative source key and complete text.")
			}
			source["source_hash"] = hashText(str(source, "original_text"))
		default:
			return nil, errors.New("Unsupported source type.")
		}
		status, itemID, err := l.matchIntake(source)
		if err != nil {
			return nil, err
		}
		existing, err := l.row("SELECT id FROM source_intake WHERE source_key=?", source["source_key"])
		if err != nil {
			return nil, err
		}
		var id string
		if existing != nil {
			id = str(existing, "id")
		} else {
			id, err = newID()
			if err != nil {
				return nil, err
			}
		}
		err = l.transaction(func() error {
			return l.exec("INSERT INTO source_intake VALUES(?,?,?,?,?,?,?) ON CONFLICT(source_key) DO UPDATE SET payload=excluded.payload,status=excluded.status,item_id=excluded.item_id,updated=excluded.updated", id, source["source_key"], kind, jsonText(source), status, itemID, l.now())
		})
		if err != nil {
			return nil, err
		}
		result = append(result, Record{"intake_id": id, "title": str(source, "title"), "status": status, "item_id": itemID})
		if status == "pending" {
			pending++
		}
	}
	return Record{"sources": result, "pending": pending}, nil
}
func (l *Library) ListIntake(sourceType any, limit int64) ([]Record, error) {
	if (sourceType != nil && sourceType != "google_doc" && sourceType != "zoom") || limit < 1 || limit > 200 {
		return nil, errors.New("Invalid intake filter or limit.")
	}
	filter := ""
	args := []any{}
	if truth(sourceType) {
		filter = "AND source_type=? "
		args = append(args, sourceType)
	}
	args = append(args, limit)
	rows, err := l.rows("SELECT * FROM source_intake WHERE status='pending' "+filter+"ORDER BY COALESCE(json_extract(payload,'$.date'),'') DESC,updated DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range rows {
		payload, e := parseJSON(row["payload"])
		if e != nil {
			return nil, e
		}
		item := Record{"intake_id": row["id"]}
		for key, value := range payload {
			if key != "original_text" {
				item[key] = value
			}
		}
		result = append(result, item)
	}
	return result, nil
}
func (l *Library) GetIntake(id string) (Record, error) {
	row, err := l.row("SELECT * FROM source_intake WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Intake source not found.")
	}
	payload, err := parseJSON(row["payload"])
	if err != nil {
		return nil, err
	}
	result := Record{"intake_id": id, "status": row["status"]}
	for key, value := range payload {
		result[key] = value
	}
	return result, nil
}

var fullSpeaker = regexp.MustCompile(`(?m)^\[([^\]\n]+)\]\s+\d{1,2}:\d{2}`)
var excludedSpeaker = map[string]bool{"guest": true, "user": true, "unknown": true, "zoom": true, "room": true, "bot": true, "assistant": true, "participant": true}

func (l *Library) confirmedSpeakers(text string) []Record {
	people := []Record{}
	seen := map[string]bool{}
	for _, match := range fullSpeaker.FindAllStringSubmatch(text, -1) {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		parts := strings.Fields(name)
		if caseFold(name) == caseFold(l.OwnerName) || len(parts) < 2 {
			continue
		}
		valid := true
		for _, word := range parts {
			runes := []rune(word)
			if len(runes) == 0 || !unicode.IsUpper(runes[0]) || excludedSpeaker[caseFold(word)] {
				valid = false
				break
			}
			for _, r := range runes {
				if !unicode.IsLetter(r) && r != '-' && r != '’' && r != '\'' {
					valid = false
					break
				}
			}
		}
		if valid {
			people = append(people, Record{"name": name, "evidence": "Full-name speaker label in the preserved transcript."})
		}
	}
	return people
}
func (l *Library) CompleteIntake(id string, payload, source Record, expectedHash any) (Record, error) {
	queued, err := l.GetIntake(id)
	if err != nil {
		return nil, err
	}
	if _, err = l.syncTombstones(); err != nil {
		return nil, err
	}
	status, itemID, err := l.matchIntake(queued)
	if err != nil {
		return nil, err
	}
	if status != "pending" {
		err = l.transaction(func() error {
			return l.exec("UPDATE source_intake SET status=?,item_id=? WHERE id=?", status, itemID, id)
		})
		return Record{"status": status, "id": itemID}, err
	}
	args := Record{}
	for key, value := range payload {
		args[key] = value
	}
	documentPeople := args["document_people"]
	delete(args, "document_people")
	delete(args, "explicit")
	delete(args, "automatic")
	args["source_key"] = queued["source_key"]
	zoom := queued["type"] == "zoom"
	mentions := []Record{}
	if zoom {
		if expectedHash != queued["source_hash"] {
			return nil, errors.New("Queued transcript changed; re-read it and regenerate the summary.")
		}
		args["original_text"] = queued["original_text"]
		args["original_name"] = queued["original_name"]
		if truth(queued["date"]) {
			args["date"] = queued["date"]
		}
	} else {
		trashed, verified := source["trashed"].(bool)
		if source == nil || source["file_id"] != queued["file_id"] || source["mime_type"] != "application/vnd.google-apps.document" || !verified || trashed {
			return nil, errors.New("Verified native Google Doc metadata is required.")
		}
		title := strings.Join(strings.Fields(str(source, "title")), " ")
		if title == "" || strings.Contains(caseFold(title), "untitled document") {
			return Record{"status": "filtered", "id": nil}, nil
		}
		if strings.TrimSpace(str(args, "original_text")) == "" {
			return nil, errors.New("Complete Google source text is required.")
		}
		args["kind"] = "document"
		args["title"] = source["title"]
		args["doc_urls"] = []string{str(queued, "url")}
		sync := Record{}
		for _, key := range []string{"file_id", "title", "created_time", "modified_time", "version"} {
			if value, ok := source[key]; ok {
				sync[key] = value
			}
		}
		sync["content_hash"] = documentHash(str(args, "original_text"))
		args["metadata"] = Record{"google_sync": sync}
		created := str(source, "created_time")
		if len(created) > 10 {
			created = created[:10]
		}
		args["date"] = created
	}
	if zoom && (args["kind"] == "meeting" || args["kind"] == "one-on-one") && !truth(args["participants"]) && !truth(args["candidates"]) {
		interview := false
		tags, e := recordsArg(args["tags"])
		if e != nil {
			return nil, e
		}
		for _, tag := range tags {
			if tag["category"] == "type" && caseFold(str(tag, "name")) == "interview" {
				interview = true
			}
		}
		if !interview {
			people := l.confirmedSpeakers(str(queued, "original_text"))
			if len(people) > 0 {
				args["participants"] = people
			}
		}
	}
	if !zoom {
		mentions, err = recordsArg(documentPeople)
		if err != nil {
			return nil, errors.New("Use document_people for named mentions; Google owners/creators belong in source metadata.")
		}
		for _, person := range mentions {
			if person["role"] != "mentioned" {
				return nil, errors.New("Use document_people for named mentions; Google owners/creators belong in source metadata.")
			}
		}
		if _, _, _, err = l.googlePeopleInput(source, mentions); err != nil {
			return nil, err
		}
	}
	args["automatic"] = true
	result, err := l.Ingest(args)
	if err != nil {
		return nil, err
	}
	var peopleResult Record
	if !zoom && !truth(result["trashed"]) {
		peopleResult, err = l.applyGooglePeople(str(result, "id"), source, mentions)
		if err != nil {
			return nil, err
		}
	}
	item, err := l.Get(str(result, "id"), truth(result["trashed"]), false)
	if err != nil {
		return nil, err
	}
	if !truth(result["duplicate"]) && item["source_hash"] != hashText(str(args, "original_text")) {
		return nil, errors.New("Imported source verification failed.")
	}
	status = "imported"
	if truth(result["trashed"]) {
		status = "blocked"
	} else if truth(result["duplicate"]) {
		status = "duplicate"
	}
	err = l.transaction(func() error {
		return l.exec("UPDATE source_intake SET status=?,item_id=? WHERE id=?", status, result["id"], id)
	})
	if err != nil {
		return nil, err
	}
	response := Record{"status": status, "id": result["id"], "title": item["title"]}
	if peopleResult != nil {
		response["people_issues"] = peopleResult["issues"]
		response["ownership_status"] = mapping(mapping(item["metadata"])["document_people"])["ownership_status"]
	}
	return response, nil
}
