package library

import (
	"errors"
	"sort"
	"strings"
)

func (l *Library) entityRecord(row Record) (Record, error) {
	result := Record{}
	for k, v := range row {
		result[k] = v
	}
	var profile Record
	var err error
	if truth(row["profile_id"]) {
		profile, err = l.row("SELECT deleted_at,source_key FROM items WHERE id=?", row["profile_id"])
		if err != nil {
			return nil, err
		}
	}
	result["deleted"] = profile != nil && truth(profile["deleted_at"])
	result["candidate"] = false
	if profile != nil {
		candidate := strings.HasPrefix(str(profile, "source_key"), "interview-person:")
		if !candidate {
			found, e := l.row("SELECT 1 FROM item_tags it JOIN tags t ON t.id=it.tag_id WHERE it.item_id=? AND t.category='topic' AND lower(t.name)='interview candidate'", row["profile_id"])
			if e != nil {
				return nil, e
			}
			candidate = found != nil
		}
		result["candidate"] = candidate
	}
	return result, nil
}
func (l *Library) relationships(item Record) ([]Record, error) {
	rows, err := l.rows("SELECT e.*,r.relation FROM entity_relationships r JOIN entities e ON e.id=r.entity_id WHERE r.item_id=? ORDER BY e.kind,e.name", item["id"])
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range rows {
		entity, e := l.entityRecord(row)
		if e != nil {
			return nil, e
		}
		if !truth(entity["deleted"]) {
			result = append(result, entity)
		}
	}
	return result, nil
}
func (l *Library) Entities() ([]Record, error) {
	rows, err := l.rows(`SELECT e.*,(SELECT count(*) FROM items i WHERE i.deleted_at IS NULL AND (
 EXISTS(SELECT 1 FROM entity_relationships r WHERE r.entity_id=e.id AND r.item_id=i.id)
 OR (e.kind='person' AND EXISTS(SELECT 1 FROM meeting_people p WHERE p.item_id=i.id AND p.person_id=e.profile_id))
 OR (e.kind='person' AND EXISTS(SELECT 1 FROM item_authors a WHERE a.item_id=i.id AND a.normalized_name=lower(e.name)))
 OR (e.kind='person' AND EXISTS(SELECT 1 FROM document_people d WHERE d.item_id=i.id AND d.person_id=e.profile_id))
 )) AS count FROM entities e ORDER BY e.kind,e.name`)
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range rows {
		entity, e := l.entityRecord(row)
		if e != nil {
			return nil, e
		}
		if !truth(entity["deleted"]) {
			result = append(result, entity)
		}
	}
	return result, nil
}
func (l *Library) CreateEntity(name, kind string) (Record, error) {
	name = clean(name)
	if (kind != "person" && kind != "team" && kind != "project") || name == "" {
		return nil, errors.New("Choose a person, team or project and supply its name.")
	}
	rows, err := l.rows("SELECT id,title,deleted_at FROM items WHERE kind=? AND (lower(title)=lower(?) OR lower(json_extract(metadata,'$.obsidian_stem'))=lower(?)) ORDER BY deleted_at IS NOT NULL,created", kind, name, name)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 && truth(rows[0]["deleted_at"]) {
		return nil, errors.New("This record is in Trash; restore it explicitly.")
	}
	if len(rows) > 1 {
		return nil, errors.New("Multiple records match; resolve the identity first.")
	}
	var id any
	if len(rows) > 0 {
		id = rows[0]["id"]
		name = str(rows[0], "title")
	} else {
		created, e := l.Ingest(Record{"title": name, "summary": "## Overview\n\n", "kind": kind, "source_key": "entity:" + kind + ":" + caseFold(name)})
		if e != nil {
			return nil, e
		}
		id = created["id"]
	}
	var tagID string
	err = l.transaction(func() error {
		var e error
		tagID, e = l.Tag(name, kind)
		if e != nil {
			return e
		}
		return l.exec("UPDATE entities SET profile_id=? WHERE legacy_tag_id=?", id, tagID)
	})
	if err != nil {
		return nil, err
	}
	row, err := l.row("SELECT * FROM entities WHERE legacy_tag_id=?", tagID)
	if err != nil {
		return nil, err
	}
	return l.entityRecord(row)
}
func (l *Library) GetEntity(id string) (Record, error) {
	row, err := l.row("SELECT * FROM entities WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Connection record not found.")
	}
	entity, err := l.entityRecord(row)
	if err != nil {
		return nil, err
	}
	if truth(entity["deleted"]) {
		return nil, errors.New("This record is in Trash.")
	}
	related, err := l.Listing(Record{"tag_ids": []string{id}})
	if err != nil {
		return nil, err
	}
	documents, meetings, members := []Record{}, []Record{}, []Record{}
	for _, item := range related {
		kind := str(item, "kind")
		if kind == "document" || kind == "reference" {
			documents = append(documents, item)
		}
		if (kind == "meeting" || kind == "one-on-one") && !truth(item["is_interview"]) {
			meetings = append(meetings, item)
		}
		if kind == "person" && !truth(item["is_interview_candidate"]) {
			members = append(members, item)
		}
	}
	result := Record{"entity": entity, "title": entity["name"], "documents": []Record{}, "related_documents": []Record{}, "meetings": meetings, "members": members}
	if entity["kind"] == "person" {
		result["related_documents"] = documents
	} else {
		result["documents"] = documents
	}
	return result, nil
}
func (l *Library) GetTeam(id string) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if item["kind"] != "team" {
		return nil, errors.New("This is not a team.")
	}
	row, err := l.row("SELECT id FROM entities WHERE kind='team' AND profile_id=?", id)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return l.GetEntity(str(row, "id"))
	}
	return Record{"title": item["title"], "documents": []Record{}, "meetings": []Record{}, "members": []Record{}}, nil
}
func (l *Library) LinkEntity(ids []string, id string, remove bool) (Record, error) {
	row, err := l.row("SELECT * FROM entities WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Connection record not found.")
	}
	entity, err := l.entityRecord(row)
	if err != nil {
		return nil, err
	}
	if truth(entity["deleted"]) && !remove {
		return nil, errors.New("This record is in Trash; restore it explicitly.")
	}
	if truth(entity["candidate"]) && !remove {
		return nil, errors.New("Use interview candidate membership for this person.")
	}
	for _, itemID := range ids {
		if _, err = l.Get(itemID, false, false); err != nil {
			return nil, err
		}
		if itemID == entity["profile_id"] && !remove {
			return nil, errors.New("A record cannot be connected to itself.")
		}
	}
	result, err := l.ApplyTags(ids, str(entity, "legacy_tag_id"), remove)
	if err != nil {
		return nil, err
	}
	if truth(result["changed"]) {
		err = l.transaction(func() error {
			for _, itemID := range ids {
				if e := l.exec("UPDATE items SET revision=revision+1 WHERE id=?", itemID); e != nil {
					return e
				}
			}
			return nil
		})
	}
	return result, err
}
func (l *Library) SetAuthors(id string, authors []string) (Record, error) {
	names, err := authorNames(authors)
	if err != nil {
		return nil, err
	}
	if _, err = l.Get(id, false, false); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		if e := l.exec("DELETE FROM item_authors WHERE item_id=?", id); e != nil {
			return e
		}
		for _, name := range names {
			if e := l.exec("INSERT INTO item_authors VALUES(?,?,?)", id, name, caseFold(name)); e != nil {
				return e
			}
		}
		return l.exec("UPDATE items SET revision=revision+1 WHERE id=?", id)
	})
	if err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}
func (l *Library) ensureFeed(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("A blog feed needs a name.")
	}
	normalized := strings.Join(strings.Fields(caseFold(name)), " ")
	row, err := l.row("SELECT id FROM blog_feeds WHERE normalized_name=?", normalized)
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
	return id, l.exec("INSERT INTO blog_feeds VALUES(?,?,?)", id, name, normalized)
}
func (l *Library) ensureSeries(title, cadence string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("A meeting series needs a name.")
	}
	normalized := strings.Join(strings.Fields(caseFold(title)), " ")
	row, err := l.row("SELECT id FROM meeting_series WHERE normalized_title=?", normalized)
	if err != nil {
		return "", err
	}
	if row != nil {
		if cadence != "" {
			err = l.exec("UPDATE meeting_series SET cadence=? WHERE id=?", cadence, row["id"])
		}
		return str(row, "id"), err
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	return id, l.exec("INSERT INTO meeting_series VALUES(?,?,?,?)", id, title, cadence, normalized)
}
func (l *Library) BlogFeeds() ([]Record, error) {
	if err := l.syncFeedArrivals(); err != nil {
		return nil, err
	}
	feeds, err := l.rows("SELECT f.*,count(i.id) AS edition_count,max(i.date) AS latest_date FROM blog_feeds f LEFT JOIN blog_members m ON m.feed_id=f.id LEFT JOIN items i ON i.id=m.item_id AND i.deleted_at IS NULL GROUP BY f.id ORDER BY f.name")
	if err != nil {
		return nil, err
	}
	for _, feed := range feeds {
		options, e := l.feedOptions(str(feed, "id"))
		if e != nil {
			return nil, e
		}
		for k, v := range options {
			feed[k] = v
		}
		a, e := l.feedAttention(str(feed, "id"))
		if e != nil {
			return nil, e
		}
		for k, v := range a {
			feed[k] = v
		}
	}
	return feeds, nil
}
func (l *Library) GetBlogFeed(id string) (Record, error) {
	result, err := l.row("SELECT * FROM blog_feeds WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("Blog feed not found.")
	}
	editions, err := l.Listing(Record{"kind": "blog", "blog_feed_id": id, "stars_first": false})
	if err != nil {
		return nil, err
	}
	if err = l.syncFeedArrivals(); err != nil {
		return nil, err
	}
	attention, e := l.feedAttention(id)
	if e != nil {
		return nil, e
	}
	for k, v := range attention {
		result[k] = v
	}
	// Multiple articles may be published on one day. Prefer source publication
	// time when supplied; otherwise retain deterministic arrival order.
	sort.SliceStable(editions, func(i, j int) bool {
		a, b := editions[i], editions[j]
		if str(a, "date") != str(b, "date") {
			return str(a, "date") > str(b, "date")
		}
		stamp := func(item Record) string {
			if value := str(mapping(mapping(item["metadata"])["feed_source"]), "created_at"); value != "" {
				return value
			}
			return str(item, "created")
		}
		if stamp(a) != stamp(b) {
			return stamp(a) > stamp(b)
		}
		return str(a, "id") > str(b, "id")
	})
	options, e := l.feedOptions(id)
	if e != nil {
		return nil, e
	}
	for k, v := range options {
		result[k] = v
	}
	if options["presentation"] == "overview" {
		if err = l.feedOverview(result, editions); err != nil {
			return nil, err
		}
	}
	result["editions"] = editions
	result["latest"] = nil
	if len(editions) > 0 {
		result["latest"], err = l.Get(str(editions[0], "id"), false, false)
	}
	return result, err
}
func (l *Library) AssignSeries(id, title, cadence string) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if item["kind"] != "meeting" && item["kind"] != "one-on-one" {
		return nil, errors.New("Only meetings and one-on-ones can belong to a meeting series.")
	}
	err = l.transaction(func() error {
		if title == "" {
			return l.exec("DELETE FROM series_members WHERE item_id=?", id)
		}
		series, e := l.ensureSeries(title, cadence)
		if e != nil {
			return e
		}
		return l.exec("INSERT INTO series_members VALUES(?,?) ON CONFLICT(item_id) DO UPDATE SET series_id=excluded.series_id", id, series)
	})
	if err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}
func (l *Library) SeriesList() ([]Record, error) {
	return l.rows("SELECT s.*,count(m.item_id) AS session_count,max(i.date) AS latest_date FROM meeting_series s JOIN series_members m ON m.series_id=s.id JOIN items i ON i.id=m.item_id WHERE i.deleted_at IS NULL GROUP BY s.id ORDER BY s.title")
}
func (l *Library) GetSeries(id string) (Record, error) {
	result, err := l.row("SELECT * FROM meeting_series WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("Meeting series not found.")
	}
	sessions, err := l.Listing(Record{"series_id": id, "stars_first": false})
	if err != nil {
		return nil, err
	}
	result["sessions"] = sessions
	tags := []Record{}
	index := map[string]int{}
	for _, item := range sessions {
		for _, tag := range records(item["tags"]) {
			if tag["category"] == "type" {
				continue
			}
			id := str(tag, "id")
			if n, ok := index[id]; ok {
				tags[n] = tag
			} else {
				index[id] = len(tags)
				tags = append(tags, tag)
			}
		}
	}
	result["tags"] = tags
	result["latest"] = nil
	if len(sessions) > 0 {
		result["latest"], err = l.Get(str(sessions[0], "id"), false, false)
	}
	return result, err
}
func (l *Library) SetReviewed(id string, reviewed bool) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	inbox := boolInt(!reviewed)
	if number(item["inbox"]) != int64(inbox) {
		err = l.transaction(func() error { return l.exec("UPDATE items SET inbox=?,revision=revision+1 WHERE id=?", inbox, id) })
	}
	if err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}
