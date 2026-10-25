package library

import (
	"errors"
	"sort"
	"strings"
)

func (l *Library) LinkMeetingPeople(id string, participants []Record, expectedRevision any, calendarSource Record) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	kind := str(item, "kind")
	if (kind != "meeting" && kind != "one-on-one") || truth(item["is_interview"]) {
		return nil, errors.New("Work participants require a non-interview meeting.")
	}
	if expectedRevision != nil {
		revision, e := integer(expectedRevision)
		if e != nil || revision != number(item["revision"]) {
			return nil, errors.New("Meeting changed; re-read before linking participants.")
		}
	}
	if len(participants) == 0 {
		return nil, errors.New("Supply confirmed participants.")
	}
	type participant struct{ name, email, evidence, id string }
	prepared := []participant{}
	for _, person := range participants {
		name, email, evidence := clean(str(person, "name")), caseFold(clean(str(person, "email"))), clean(str(person, "evidence"))
		if len(strings.Fields(name)) < 2 || evidence == "" {
			return nil, errors.New("Use a confirmed full name and identity evidence; do not guess.")
		}
		rows, e := l.rows("SELECT DISTINCT i.id,i.deleted_at FROM items i LEFT JOIN meeting_people p ON p.person_id=i.id WHERE i.kind='person' AND (lower(i.title)=lower(?) OR lower(json_extract(i.metadata,'$.obsidian_stem'))=lower(?) OR (?<>'' AND lower(p.email)=?)) ORDER BY i.deleted_at IS NOT NULL", name, name, email, email)
		if e != nil {
			return nil, e
		}
		if len(rows) > 0 && truth(rows[0]["deleted_at"]) {
			return nil, errors.New("Person is in Trash; restore explicitly: " + name)
		}
		live := 0
		for _, row := range rows {
			if !truth(row["deleted_at"]) {
				live++
			}
		}
		if live > 1 {
			return nil, errors.New("Ambiguous person identity: " + name)
		}
		personID := ""
		if len(rows) > 0 {
			personID = str(rows[0], "id")
			existing, e := l.Get(personID, false, false)
			if e != nil {
				return nil, e
			}
			if truth(existing["is_interview_candidate"]) {
				return nil, errors.New("Existing candidate identity requires explicit work-contact review: " + name)
			}
		}
		prepared = append(prepared, participant{name, email, evidence, personID})
	}
	metadata := Record{}
	for key, value := range mapping(item["metadata"]) {
		metadata[key] = value
	}
	if len(calendarSource) > 0 {
		if sourceKind(str(calendarSource, "url")) != "calendar" {
			return nil, errors.New("Use a confirmed Google Calendar event URL.")
		}
		metadata["calendar_url"] = calendarSource["url"]
		match := Record{}
		for _, key := range []string{"event_id", "url", "start", "end", "evidence"} {
			if value, present := calendarSource[key]; present {
				match[key] = value
			}
		}
		metadata["calendar_match"] = match
	}
	linked := []Record{}
	changed := jsonText(metadata) != jsonText(item["metadata"])
	for _, person := range prepared {
		name, personID := person.name, person.id
		if personID == "" {
			key := person.email
			if key == "" {
				key = caseFold(name)
			}
			created, e := l.Ingest(Record{"title": name, "summary": "## Overview\n\nWork contact. Confirmed participant in [[" + str(item, "title") + "]].\n", "kind": "person", "source_key": "meeting-person:" + key, "tags": []Record{{"name": name, "category": "person"}}})
			if e != nil {
				return nil, e
			}
			personID = str(created, "id")
		} else {
			existing, e := l.Get(personID, false, false)
			if e != nil {
				return nil, e
			}
			name = str(existing, "title")
		}
		err = l.transaction(func() error {
			previous, e := l.row("SELECT name,email,evidence FROM meeting_people WHERE item_id=? AND person_id=?", id, personID)
			if e != nil {
				return e
			}
			if previous == nil || str(previous, "name") != name || str(previous, "email") != person.email || str(previous, "evidence") != person.evidence {
				if e = l.exec("INSERT OR REPLACE INTO meeting_people VALUES(?,?,?,?,?)", id, personID, name, person.email, person.evidence); e != nil {
					return e
				}
				changed = true
			}
			tag, e := l.Tag(name, "person")
			if e != nil {
				return e
			}
			added, e := l.linkOrganizationTag(id, tag)
			if e != nil {
				return e
			}
			changed = changed || added
			return nil
		})
		if err != nil {
			return nil, err
		}
		linked = append(linked, Record{"id": personID, "name": name})
	}
	if changed {
		err = l.transaction(func() error {
			if e := l.exec("UPDATE items SET metadata=?,revision=revision+1 WHERE id=?", jsonText(metadata), id); e != nil {
				return e
			}
			current, e := l.Get(id, false, false)
			if e != nil {
				return e
			}
			return l.activityEvent(current, "people_linked", "", nil, false)
		})
		if err != nil {
			return nil, err
		}
	}
	grouping, err := l.groupOneOnOne(id)
	if err != nil {
		return nil, err
	}
	return Record{"item_id": id, "people": linked, "changed": changed || truth(grouping["changed"])}, nil
}

// groupOneOnOne uses confirmed colleague identity, with the configured library
// owner implicit in the pair. An unconfigured owner never invents a pair name.
func (l *Library) groupOneOnOne(id string) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if str(item, "kind") != "one-on-one" || truth(item["is_interview"]) {
		return Record{"item_id": id, "status": "not_applicable", "changed": false}, nil
	}
	ownerName := strings.TrimSpace(l.OwnerName)
	ownerFields := strings.Fields(ownerName)
	if len(ownerFields) == 0 {
		return Record{"item_id": id, "status": "unresolved", "changed": false}, nil
	}
	rows, err := l.rows("SELECT person_id FROM meeting_people WHERE item_id=?", id)
	if err != nil {
		return nil, err
	}
	people := []Record{}
	for _, row := range rows {
		person, e := l.Get(str(row, "person_id"), true, false)
		if e != nil {
			return nil, e
		}
		if caseFold(str(person, "title")) != caseFold(ownerName) {
			people = append(people, person)
		}
	}
	if len(people) != 1 || truth(people[0]["deleted_at"]) || truth(people[0]["is_interview_candidate"]) {
		return Record{"item_id": id, "status": "unresolved", "changed": false}, nil
	}
	person := people[0]
	existing, err := l.row("SELECT series_id FROM one_on_one_series WHERE person_id=?", person["id"])
	if err != nil {
		return nil, err
	}
	current := mapping(item["series"])
	seriesID := ""
	changed, review := false, false
	err = l.transaction(func() error {
		if existing != nil {
			seriesID = str(existing, "series_id")
		} else if len(current) > 0 {
			members, e := l.rows("SELECT item_id FROM series_members WHERE series_id=?", current["id"])
			if e != nil {
				return e
			}
			for _, member := range members {
				other, e := l.Get(str(member, "item_id"), true, false)
				if e != nil {
					return e
				}
				if truth(other["deleted_at"]) {
					continue
				}
				participantRows, e := l.rows("SELECT person_id FROM meeting_people WHERE item_id=?", other["id"])
				if e != nil {
					return e
				}
				ids := map[string]bool{}
				for _, row := range participantRows {
					ids[str(row, "person_id")] = true
				}
				if str(other, "kind") != "one-on-one" || truth(other["is_interview"]) || len(ids) != 1 || !ids[str(person, "id")] {
					review = true
					return nil
				}
			}
			seriesID = str(current, "id")
		} else {
			names := strings.Fields(str(person, "title"))
			if len(names) == 0 {
				review = true
				return nil
			}
			title := names[0] + " ↔ " + ownerFields[0]
			normalized := func(value string) string { return strings.Join(strings.Fields(caseFold(value)), " ") }
			collision, e := l.row("SELECT 1 FROM meeting_series WHERE normalized_title=?", normalized(title))
			if e != nil {
				return e
			}
			if collision != nil {
				title = str(person, "title") + " ↔ " + ownerFields[0]
			}
			collision, e = l.row("SELECT 1 FROM meeting_series WHERE normalized_title=?", normalized(title))
			if e != nil {
				return e
			}
			if collision != nil {
				review = true
				return nil
			}
			seriesID, e = l.ensureSeries(title, "")
			if e != nil {
				return e
			}
		}
		if e := l.exec("INSERT OR IGNORE INTO one_on_one_series VALUES(?,?)", person["id"], seriesID); e != nil {
			return e
		}
		changed = len(current) == 0 || str(current, "id") != seriesID
		if changed {
			if e := l.exec("INSERT INTO series_members VALUES(?,?) ON CONFLICT(item_id) DO UPDATE SET series_id=excluded.series_id", id, seriesID); e != nil {
				return e
			}
			if e := l.exec("UPDATE items SET revision=revision+1 WHERE id=?", id); e != nil {
				return e
			}
			updated, e := l.Get(id, false, false)
			if e != nil {
				return e
			}
			return l.activityEvent(updated, "series_linked", "", nil, false)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if review {
		return Record{"item_id": id, "status": "review", "changed": false}, nil
	}
	updated, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	return Record{"item_id": id, "status": "grouped", "changed": changed, "series": updated["series"]}, nil
}

func (l *Library) BackfillOneOnOneSeries() ([]Record, error) {
	rows, err := l.rows("SELECT id FROM items WHERE kind='one-on-one' AND deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	type session struct {
		id        string
		hasSeries bool
	}
	sessions := []session{}
	for _, row := range rows {
		item, e := l.Get(str(row, "id"), false, false)
		if e != nil {
			return nil, e
		}
		sessions = append(sessions, session{str(row, "id"), len(mapping(item["series"])) > 0})
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].hasSeries && !sessions[j].hasSeries })
	result := []Record{}
	for _, session := range sessions {
		grouped, e := l.groupOneOnOne(session.id)
		if e != nil {
			return nil, e
		}
		result = append(result, grouped)
	}
	return result, nil
}
