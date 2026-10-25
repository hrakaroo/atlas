package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var wikiLinks = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
var explicitDate = regexp.MustCompile(`(?im)^\s*(?:-\s*)?(?:date|created|created[_ -]date|creation date|document date):\s*["']?(\d{4}-\d{2}-\d{2})(?:[T\s"']|$)`)

var errItemNotFound = errors.New("Item not found. It may be in Trash.")

func (l *Library) Get(id string, includeDeleted, lightweight bool) (Record, error) {
	query := "SELECT * FROM items WHERE id=?"
	if !includeDeleted {
		query += " AND deleted_at IS NULL"
	}
	item, err := l.row(query, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errItemNotFound
	}
	if !lightweight && !entityKind(str(item, "kind")) {
		path := filepath.Join(l.Store.Root, "items", id, "summary.md")
		info, e := os.Stat(path)
		if e != nil {
			return nil, e
		}
		current := stamp{Mtime: info.ModTime().UnixNano(), Size: info.Size()}
		if l.summaryStamps[id] != current {
			text, e := l.readText(path)
			if e != nil {
				return nil, e
			}
			if text != str(item, "summary") {
				if e = l.transaction(func() error {
					if e := l.exec("UPDATE items SET summary=?,revision=revision+1 WHERE id=?", text, id); e != nil {
						return e
					}
					if e := l.exec("UPDATE search SET summary=? WHERE item_id=?", text, id); e != nil {
						return e
					}
					return l.indexLinks(id, text)
				}); e != nil {
					return nil, e
				}
				item["summary"] = text
				item["revision"] = number(item["revision"]) + 1
			}
			l.summaryStamps[id] = current
		}
	}
	meta, err := parseJSON(item["metadata"])
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, errors.New("Invalid item metadata.")
	}
	item["metadata"] = meta
	basis := str(meta, "date_basis")
	if basis == "" {
		basis = "source"
		if str(item, "date") == "" {
			basis = "added"
		}
	}
	item["date_basis"] = basis
	if str(item, "date") == "" {
		item["date"] = l.today(str(item, "created"))
		item["date_basis"] = "added"
	}
	authors, err := l.rows("SELECT name FROM item_authors WHERE item_id=? ORDER BY name", id)
	if err != nil {
		return nil, err
	}
	item["authors"] = []Record{}
	for _, author := range authors {
		person, e := l.row("SELECT id FROM items WHERE kind='person' AND deleted_at IS NULL AND (lower(title)=lower(?) OR lower(json_extract(metadata,'$.obsidian_stem'))=lower(?)) ORDER BY created LIMIT 1", author["name"], author["name"])
		if e != nil {
			return nil, e
		}
		var personID any
		if person != nil {
			personID = person["id"]
		}
		item["authors"] = append(item["authors"].([]Record), Record{"name": author["name"], "id": personID})
	}
	tags, err := l.rows("SELECT t.* FROM tags t JOIN item_tags it ON t.id=it.tag_id WHERE it.item_id=? ORDER BY t.category,t.name", id)
	if err != nil {
		return nil, err
	}
	item["tags"] = tags
	candidate := str(item, "kind") == "person" && strings.HasPrefix(str(item, "source_key"), "interview-person:")
	interview := str(item, "kind") == "interview"
	for _, tag := range tags {
		if str(tag, "category") == "topic" && caseFold(str(tag, "name")) == "interview candidate" && str(item, "kind") == "person" {
			candidate = true
		}
		if str(tag, "category") == "type" && caseFold(str(tag, "name")) == "interview" {
			interview = true
		}
	}
	item["is_interview_candidate"] = candidate
	item["is_interview"] = interview
	links := []Record{}
	if !lightweight {
		links, err = l.rows("SELECT target,label FROM links WHERE item_id=?", id)
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			resolved, e := l.resolveTarget(str(link, "target"), false)
			if e != nil {
				return nil, e
			}
			link["id"] = resolved
		}
	}
	item["links"] = links
	relationships, err := l.relationships(item)
	if err != nil {
		return nil, err
	}
	item["relationships"] = relationships
	participants, err := l.rows("SELECT p.person_id AS id,p.name,p.email,p.evidence FROM meeting_people p JOIN items i ON i.id=p.person_id WHERE p.item_id=? AND i.deleted_at IS NULL ORDER BY p.name", id)
	if err != nil {
		return nil, err
	}
	item["participants"] = participants
	people, err := l.rows("SELECT p.person_id AS id,i.title AS name,p.role,p.evidence,p.email FROM document_people p JOIN items i ON i.id=p.person_id WHERE p.item_id=? AND i.deleted_at IS NULL ORDER BY p.role,i.title", id)
	if err != nil {
		return nil, err
	}
	item["document_people"] = people
	projects := []Record{}
	for _, tag := range tags {
		if str(tag, "category") != "project" {
			continue
		}
		profile, e := l.row("SELECT id FROM items WHERE kind='project' AND deleted_at IS NULL AND (title=? OR json_extract(metadata,'$.obsidian_stem')=?) ORDER BY created LIMIT 1", tag["name"], tag["name"])
		if e != nil {
			return nil, e
		}
		var profileID any
		if profile != nil {
			profileID = profile["id"]
		}
		projects = append(projects, Record{"name": tag["name"], "id": profileID})
	}
	item["projects"] = projects
	series, err := l.row("SELECT s.* FROM meeting_series s JOIN series_members m ON m.series_id=s.id WHERE m.item_id=?", id)
	if err != nil {
		return nil, err
	}
	if series == nil {
		item["series"] = nil
	} else {
		item["series"] = series
	}
	feed, err := l.row("SELECT f.* FROM blog_feeds f JOIN blog_members m ON m.feed_id=f.id WHERE m.item_id=?", id)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		item["blog_feed"] = nil
	} else {
		item["blog_feed"] = feed
	}
	if lightweight {
		links := []Record{}
		urls, _ := stringsArg(meta["doc_urls"])
		for _, url := range normalizedDocURLs(urls) {
			links = append(links, Record{"kind": "document", "url": url, "primary": true, "label": item["title"]})
		}
		item["source_links"] = links
		return item, nil
	}
	indexed, err := l.row("SELECT source_text FROM search WHERE item_id=?", id)
	if err != nil {
		return nil, err
	}
	text := str(item, "summary") + "\n"
	if indexed != nil {
		text += str(indexed, "source_text")
	}
	refs, err := l.sourceLinks(item, text)
	if err != nil {
		return nil, err
	}
	item["source_links"] = refs
	return item, nil
}

func (l *Library) indexLinks(id, summary string) error {
	if err := l.exec("DELETE FROM links WHERE item_id=?", id); err != nil {
		return err
	}
	for _, match := range wikiLinks.FindAllStringSubmatch(summary, -1) {
		label := match[2]
		if label == "" {
			label = match[1]
		}
		if err := l.exec("INSERT OR IGNORE INTO links VALUES(?,?,?)", id, match[1], label); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) UpdateSummary(id, summary string, revision int64) (Record, error) {
	summary = normalizeDocLinks(summary)
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The item changed. Read it again before updating.")
	}
	if entityKind(str(item, "kind")) {
		return l.updateEntitySummary(item, summary)
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	old := str(item, "summary")
	if err = l.atomic(filepath.Join(folder, "revisions", fmt.Sprint(revision)+".md"), old); err != nil {
		return nil, err
	}
	if err = l.contractPoint("summary.before_write"); err != nil {
		return nil, err
	}
	if err = l.atomic(filepath.Join(folder, "summary.md"), summary); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		if err := l.exec("UPDATE items SET summary=?,revision=revision+1,updated=? WHERE id=?", summary, l.now(), id); err != nil {
			return err
		}
		if err := l.exec("UPDATE search SET summary=? WHERE item_id=?", summary, id); err != nil {
			return err
		}
		if err := l.indexLinks(id, summary); err != nil {
			return err
		}
		if summary != old {
			current, err := l.Get(id, false, false)
			if err != nil {
				return err
			}
			if err := l.activityEvent(current, "summary_updated", "", nil, false); err != nil {
				return err
			}
			return l.contractPoint("summary.before_commit")
		}
		return nil
	})
	if err != nil {
		_ = l.atomic(filepath.Join(folder, "summary.md"), old)
		return nil, err
	}
	return l.Get(id, false, false)
}

var strictDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func validDate(value string) error {
	if strictDatePattern.MatchString(value) {
		if parsed, err := time.Parse("2006-01-02", value); err == nil && parsed.Year() >= 1 {
			return nil
		}
	}
	return fmt.Errorf("Invalid isoformat string: '%s'", value)
}
func (l *Library) SetDate(id, date string, revision int64) (Record, error) {
	if err := validDate(date); err != nil {
		return nil, err
	}
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The item changed. Read it again before updating.")
	}
	meta := item["metadata"].(Record)
	meta["date_basis"] = "manual"
	if err = l.transaction(func() error {
		return l.exec("UPDATE items SET date=?,metadata=?,revision=revision+1,updated=? WHERE id=?", date, jsonText(meta), l.now(), id)
	}); err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}
func (l *Library) SetStarred(id string, starred bool) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if truth(item["starred"]) != starred {
		if err = l.transaction(func() error {
			return l.exec("UPDATE items SET starred=?,revision=revision+1 WHERE id=?", boolInt(starred), id)
		}); err != nil {
			return nil, err
		}
	}
	return l.Get(id, false, false)
}

func authorNames(value any) ([]string, error) {
	names, err := stringsArg(value)
	if err != nil {
		return nil, errors.New("Authors must be a list of names.")
	}
	result := []string{}
	index := map[string]int{}
	for _, name := range names {
		name = clean(name)
		if name == "" {
			return nil, errors.New("Authors must be a list of names.")
		}
		key := caseFold(name)
		if i, exists := index[key]; exists {
			result[i] = name
		} else {
			index[key] = len(result)
			result = append(result, name)
		}
	}
	return result, nil
}
func recordsArg(value any) ([]Record, error) {
	if value == nil {
		return []Record{}, nil
	}
	if list, ok := value.([]Record); ok {
		return list, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("A list of objects is required.")
	}
	result := []Record{}
	for _, value := range list {
		entry, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("A list of objects is required.")
		}
		result = append(result, entry)
	}
	return result, nil
}
func sourceDate(text string) string {
	matches := explicitDate.FindAllStringSubmatch(text, -1)
	unique := map[string]bool{}
	for _, match := range matches {
		unique[match[1]] = true
	}
	if len(unique) != 1 {
		return ""
	}
	for date := range unique {
		if validDate(date) == nil {
			return date
		}
	}
	return ""
}

func (l *Library) Ingest(args map[string]any) (result Record, err error) {
	allowed := map[string]bool{}
	for _, name := range []string{"title", "summary", "kind", "date", "source_key", "tags", "source_path", "source_text", "metadata", "period_start", "period_end", "source_channel", "original_text", "original_name", "series_title", "series_cadence", "calendar_url", "doc_urls", "blog_feed", "projects", "authors", "candidates", "explicit", "automatic", "participants", "calendar_source", "document_people"} {
		allowed[name] = true
	}
	for name := range args {
		if !allowed[name] {
			return nil, fmt.Errorf("Library.ingest() got an unexpected keyword argument '%s'", name)
		}
	}
	title, ok := args["title"].(string)
	summary, summaryOK := args["summary"].(string)
	if !ok || strings.TrimSpace(title) == "" || !summaryOK {
		return nil, errors.New("A title and Markdown summary are required.")
	}
	explicit, automatic := false, false
	if value, exists := args["explicit"]; exists {
		var valid bool
		explicit, valid = value.(bool)
		if !valid {
			return nil, errors.New("Explicit re-add and automatic import are separate modes.")
		}
	}
	if value, exists := args["automatic"]; exists {
		var valid bool
		automatic, valid = value.(bool)
		if !valid {
			return nil, errors.New("Explicit re-add and automatic import are separate modes.")
		}
	}
	if explicit && automatic {
		return nil, errors.New("Explicit re-add and automatic import are separate modes.")
	}
	kind := strDefault(args, "kind", "note")
	if truth(args["participants"]) && ((kind != "meeting" && kind != "one-on-one") || truth(args["candidates"])) {
		return nil, errors.New("Participants belong to work meetings, separately from interview candidates.")
	}
	participants, err := recordsArg(args["participants"])
	if err != nil {
		return nil, err
	}
	documentPeople, err := recordsArg(args["document_people"])
	if err != nil {
		return nil, err
	}
	documentPeople, err = structuredDocumentPeople(documentPeople)
	if err != nil {
		return nil, err
	}
	if len(documentPeople) > 0 {
		if kind != "document" && kind != "reference" {
			return nil, errors.New("Document people require a document, explicit roles and evidence.")
		}
		for _, person := range documentPeople {
			role := str(person, "role")
			if (role != "owner" && role != "creator" && role != "mentioned") || clean(str(person, "evidence")) == "" {
				return nil, errors.New("Document people require a document, explicit roles and evidence.")
			}
		}
	}
	candidates, err := authorNames(args["candidates"])
	if err != nil {
		return nil, err
	}
	if len(candidates) > 0 && kind != "meeting" && kind != "one-on-one" && kind != "interview" {
		return nil, errors.New("Interview candidates require a meeting or interview.")
	}
	authors, err := authorNames(args["authors"])
	if err != nil {
		return nil, err
	}
	projects, err := stringsArg(args["projects"])
	if err != nil {
		return nil, errors.New("Projects must be a list of names.")
	}
	tags, err := recordsArg(args["tags"])
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if clean(project) == "" {
			return nil, errors.New("Projects must be a list of names.")
		}
		tags = append(tags, Record{"name": clean(project), "category": "project"})
	}
	meta := Record{}
	if value := args["metadata"]; value != nil {
		provided, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Metadata must be an object.")
		}
		for key, value := range provided {
			meta[key] = value
		}
	}
	if kind == "project" {
		name := str(meta, "obsidian_stem")
		if name == "" {
			name = title
		}
		tags = append(tags, Record{"name": clean(name), "category": "project"})
	}
	summary = normalizeDocLinks(summary)
	urls, err := stringsArg(args["doc_urls"])
	if err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		urls, err = stringsArg(meta["doc_urls"])
		if err != nil {
			return nil, err
		}
	}
	urls = normalizedDocURLs(urls)
	var sourceKey any = args["source_key"]
	key := s(sourceKey)
	if kind == "document" && len(urls) == 1 {
		if id := googleDocID(urls[0]); id != "" {
			key = "google-doc:" + id
			sourceKey = key
		} else if id := confluenceIdentity(urls[0]); id != "" {
			key = id
			sourceKey = key
		}
	}
	date := str(args, "date")
	if date != "" {
		if err = validDate(date); err != nil {
			return nil, err
		}
	}
	feedTitle := str(args, "blog_feed")
	seriesTitle := str(args, "series_title")
	if kind == "blog" && date == "" {
		return nil, errors.New("Blog roundups need an edition date.")
	}
	if feedTitle != "" && kind != "blog" {
		return nil, errors.New("Only blog entries can belong to a blog feed.")
	}
	if kind == "update" {
		start, end := str(args, "period_start"), str(args, "period_end")
		if start == "" || end == "" {
			return nil, errors.New("Updates require period_start and period_end (exclusive).")
		}
		if err = validDate(start); err != nil {
			return nil, err
		}
		if err = validDate(end); err != nil {
			return nil, err
		}
		a, _ := time.Parse("2006-01-02", start)
		b, _ := time.Parse("2006-01-02", end)
		if !b.After(a) {
			return nil, errors.New("period_end must follow period_start.")
		}
	}
	incomingText := str(args, "original_text")
	if incomingText == "" {
		incomingText = str(args, "source_text")
	}
	incomingType := "zoom"
	if strings.HasPrefix(key, "google-doc:") {
		incomingType = "google_doc"
	}
	incoming := Record{"type": incomingType, "source_key": key, "original_text": incomingText, "title": title, "date": date}
	if explicit && (strings.HasPrefix(key, "google-doc:") || strings.HasPrefix(key, "zoom-transcript:") || ((kind == "meeting" || kind == "one-on-one" || kind == "interview") && truth(args["original_text"]))) {
		if _, err = l.syncTombstones(); err != nil {
			return nil, err
		}
		status, matched, e := l.matchIntake(incoming)
		if e != nil {
			return nil, e
		}
		if status == "blocked" && matched != nil {
			if _, e = l.RestoreItem(s(matched)); e != nil {
				return nil, e
			}
			return Record{"id": matched, "duplicate": false, "restored": true}, nil
		}
	}
	if key != "" {
		existing, e := l.row("SELECT id,deleted_at FROM items WHERE source_key=?", key)
		if e != nil {
			return nil, e
		}
		if existing != nil {
			if truth(existing["deleted_at"]) && explicit {
				if _, e := l.RestoreItem(str(existing, "id")); e != nil {
					return nil, e
				}
				return Record{"id": existing["id"], "duplicate": false, "restored": true}, nil
			}
			added := int64(0)
			if !truth(existing["deleted_at"]) {
				err = l.transaction(func() error {
					for _, tag := range tags {
						if str(tag, "category") == "project" {
							tagID, e := l.Tag(str(tag, "name"), "project")
							if e != nil {
								return e
							}
							response, e := l.Store.DB.Exec("INSERT OR IGNORE INTO item_tags VALUES(?,?)", existing["id"], tagID)
							if e != nil {
								return e
							}
							n, e := response.RowsAffected()
							if e != nil {
								return e
							}
							added += n
						}
					}
					return nil
				})
				if err != nil {
					return nil, err
				}
				if len(documentPeople) > 0 {
					if _, err = l.LinkDocumentPeople(str(existing, "id"), documentPeople, "", nil); err != nil {
						return nil, err
					}
				}
				if len(authors) > 0 {
					item, e := l.Get(str(existing, "id"), false, false)
					if e != nil {
						return nil, e
					}
					names := []string{}
					for _, author := range records(item["authors"]) {
						names = append(names, str(author, "name"))
					}
					names = append(names, authors...)
					if _, err = l.SetAuthors(str(existing, "id"), names); err != nil {
						return nil, err
					}
				}
				if len(candidates) > 0 {
					if _, err = l.LinkInterview(str(existing, "id"), candidates); err != nil {
						return nil, err
					}
				}
			}
			return Record{"id": existing["id"], "duplicate": true, "trashed": truth(existing["deleted_at"]), "projects_added": added}, nil
		}
	}
	if automatic {
		if _, err = l.syncTombstones(); err != nil {
			return nil, err
		}
		status, matched, e := l.matchIntake(incoming)
		if e != nil {
			return nil, e
		}
		if status != "pending" {
			return Record{"id": matched, "duplicate": true, "trashed": status == "blocked", "status": status}, nil
		}
	}
	if seriesTitle != "" && kind != "meeting" && kind != "one-on-one" {
		return nil, errors.New("Only meetings and one-on-ones can belong to a meeting series.")
	}
	if calendar := str(args, "calendar_url"); calendar != "" {
		if sourceKind(calendar) != "calendar" {
			return nil, errors.New("Use a Google Calendar event URL.")
		}
		meta["calendar_url"] = calendar
	}
	if len(urls) > 0 {
		for _, url := range urls {
			if sourceKind(url) != "document" {
				return nil, errors.New("Use Google Docs document URLs.")
			}
		}
		meta["doc_urls"] = urls
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	entity := entityKind(kind)
	if !entity {
		if err = os.Mkdir(folder, 0755); err != nil {
			return nil, err
		}
	}
	staged := true
	defer func() {
		if err != nil && staged {
			_ = os.RemoveAll(folder)
		}
	}()
	if !entity {
		if err = l.atomic(filepath.Join(folder, "summary.md"), summary); err != nil {
			return nil, err
		}
	}
	var entityOriginal []byte
	entityName := "source.txt"
	entityHasOriginal := false
	var sourcePath any = args["source_path"]
	sourceText := str(args, "source_text")
	if text, exists := args["original_text"]; exists && text != nil {
		content, valid := text.(string)
		if !valid {
			return nil, errors.New("Original text must be a string.")
		}
		name := filepath.Base(strDefault(args, "original_name", "source.txt"))
		if name == "." || name == ".." || name == string(os.PathSeparator) {
			return nil, errors.New("Invalid original filename.")
		}
		path := filepath.Join(folder, "originals", name)
		if entity {
			entityOriginal = []byte(content)
			entityName = name
			entityHasOriginal = true
		} else {
			if err = l.atomic(path, content); err != nil {
				return nil, err
			}
		}
		sourcePath = path
		sourceText = content
	}
	var sourceHash any
	if truth(sourcePath) {
		path, e := filepath.Abs(s(sourcePath))
		if e != nil {
			return nil, e
		}
		if !entityHasOriginal {
			if e = l.safePath(path, false); e != nil {
				return nil, errors.New("Source files must be preserved inside the library.")
			}
		}
		bytes := entityOriginal
		var readErr error
		if !entityHasOriginal {
			bytes, readErr = l.readBytes(path)
		}
		e = readErr
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(bytes)
		sourceHash = hex.EncodeToString(sum[:])
		sourcePath = path
		if entity {
			entityOriginal = bytes
			entityHasOriginal = true
			entityName = filepath.Base(path)
			sourcePath = nil
		}
	} else {
		sourcePath = nil
	}
	now := l.now()
	if date != "" {
		meta["date_basis"] = "source"
	} else {
		if sourceText != "" {
			date = sourceDate(sourceText)
		} else {
			date = sourceDate(summary)
		}
		if date == "" {
			meta["date_basis"] = "added"
			date = l.today(now)
		} else {
			meta["date_basis"] = "source"
		}
	}
	err = l.transaction(func() error {
		if e := l.exec("INSERT INTO items(id,title,kind,date,summary,source_key,source_path,source_hash,metadata,created,updated,period_start,period_end,source_channel) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", id, title, kind, date, summary, sourceKey, sourcePath, sourceHash, jsonText(meta), now, now, args["period_start"], args["period_end"], args["source_channel"]); e != nil {
			return e
		}
		if entity {
			if entityHasOriginal {
				relative := filepath.Join("items", id, "originals", entityName)
				if e := l.saveEntitySnapshot(id, Record{"id": historyID("original", relative), "kind": "original", "name": entityName, "revision": nil, "current": true}, entityOriginal); e != nil {
					return e
				}
			}
			// Legacy snapshot metadata is migrated by ImportObsidian or startup.
			if str(meta, "snapshot_root") == "" {
				if e := l.exec("INSERT INTO entity_storage VALUES(?)", id); e != nil {
					return e
				}
			}
		}
		for _, name := range authors {
			if e := l.exec("INSERT OR IGNORE INTO item_authors VALUES(?,?,?)", id, name, caseFold(name)); e != nil {
				return e
			}
		}
		typeName := strings.ReplaceAll(kind, "-", " ")
		typeName = pythonTitle(typeName)
		if kind == "blog" {
			typeName = "Feed Update"
		}
		tags = append(tags, Record{"name": typeName, "category": "type"})
		for _, tag := range tags {
			tagID, e := l.Tag(str(tag, "name"), strDefault(tag, "category", "topic"))
			if e != nil {
				return e
			}
			if e = l.exec("INSERT OR IGNORE INTO item_tags VALUES(?,?)", id, tagID); e != nil {
				return e
			}
		}
		if e := l.indexLinks(id, summary); e != nil {
			return e
		}
		if e := l.exec("INSERT INTO search VALUES(?,?,?,?)", id, title, summary, sourceText); e != nil {
			return e
		}
		if kind == "blog" {
			name := feedTitle
			if name == "" {
				name = "AI"
			}
			feed, e := l.ensureFeed(name)
			if e != nil {
				return e
			}
			if e = l.exec("INSERT INTO blog_members VALUES(?,?)", id, feed); e != nil {
				return e
			}
		}
		if seriesTitle != "" {
			series, e := l.ensureSeries(seriesTitle, strDefault(args, "series_cadence", ""))
			if e != nil {
				return e
			}
			if e = l.exec("INSERT INTO series_members VALUES(?,?)", id, series); e != nil {
				return e
			}
		}
		item, e := l.Get(id, false, false)
		if e != nil {
			return e
		}
		return l.activityEvent(item, "imported", now, "import:"+id, false)
	})
	if err != nil {
		return nil, err
	}
	staged = false // The record now exists; later relationship errors must not delete its files.
	if len(candidates) > 0 {
		if _, err = l.LinkInterview(id, candidates); err != nil {
			return nil, err
		}
	}
	if len(participants) > 0 {
		calendar := mapping(args["calendar_source"])
		if _, err = l.LinkMeetingPeople(id, participants, nil, calendar); err != nil {
			return nil, err
		}
	}
	if len(documentPeople) > 0 {
		if _, err = l.LinkDocumentPeople(id, documentPeople, "", nil); err != nil {
			return nil, err
		}
	}
	return Record{"id": id, "duplicate": false}, nil
}
func pythonTitle(value string) string {
	parts := strings.Fields(value)
	for i, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) > 0 {
			head := strings.ToUpper(string(runes[0]))
			parts[i] = head + string(runes[1:])
		}
	}
	return strings.Join(parts, " ")
}
