package library

import "errors"

// GetPerson presents the existing evidence-backed relations without conflating
// ownership, authorship, mentions, generic connections or meeting attendance.
func (l *Library) GetPerson(id string) (Record, error) {
	person, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if str(person, "kind") != "person" {
		return nil, errors.New("This item is not a person.")
	}
	name := str(mapping(person["metadata"]), "obsidian_stem")
	if name == "" {
		name = str(person, "title")
	}
	authoredRows, err := l.rows("SELECT item_id FROM item_authors WHERE normalized_name=?", caseFold(clean(name)))
	if err != nil {
		return nil, err
	}
	authoredIDs := map[string]bool{}
	for _, row := range authoredRows {
		authoredIDs[str(row, "item_id")] = true
	}
	tag, err := l.row("SELECT id FROM tags WHERE category='person' AND lower(name)=lower(?)", clean(name))
	if err != nil {
		return nil, err
	}
	interviews := []Record{}
	if tag != nil {
		related, e := l.Listing(Record{"tag_ids": []string{str(tag, "id")}, "sort_by": "date"})
		if e != nil {
			return nil, e
		}
		for _, item := range related {
			interview := str(item, "kind") == "interview"
			for _, t := range records(item["tags"]) {
				if str(t, "category") == "type" && str(t, "name") == "Interview" {
					interview = true
				}
			}
			if interview {
				full, e := l.Get(str(item, "id"), false, false)
				if e != nil {
					return nil, e
				}
				interviews = append(interviews, full)
			}
		}
	}
	meetingRows, err := l.rows("SELECT item_id FROM meeting_people WHERE person_id=?", id)
	if err != nil {
		return nil, err
	}
	meetingIDs := map[string]bool{}
	for _, row := range meetingRows {
		meetingIDs[str(row, "item_id")] = true
	}
	meetingListing, err := l.Listing(Record{"collection": "meetings", "sort_by": "date"})
	if err != nil {
		return nil, err
	}
	meetings := []Record{}
	for _, item := range meetingListing {
		if meetingIDs[str(item, "id")] {
			full, e := l.Get(str(item, "id"), false, false)
			if e != nil {
				return nil, e
			}
			meetings = append(meetings, full)
		}
	}
	entity, err := l.row("SELECT id FROM entities WHERE kind='person' AND profile_id=?", id)
	if err != nil {
		return nil, err
	}
	related := []Record{}
	if entity != nil {
		related, err = l.Listing(Record{"tag_ids": []string{str(entity, "id")}, "kind": "document"})
		if err != nil {
			return nil, err
		}
	}
	roleRows, err := l.rows("SELECT item_id,role FROM document_people WHERE person_id=?", id)
	if err != nil {
		return nil, err
	}
	roleMap := map[string][]string{}
	for _, row := range roleRows {
		key := str(row, "item_id")
		roleMap[key] = append(roleMap[key], str(row, "role"))
	}
	documents, err := l.Listing(Record{"kind": "document"})
	if err != nil {
		return nil, err
	}
	authored, owned, mentioned := []Record{}, []Record{}, []Record{}
	relatedOrder := []string{}
	relatedByID := map[string]Record{}
	for _, document := range related {
		key := str(document, "id")
		if _, ok := relatedByID[key]; !ok {
			relatedOrder = append(relatedOrder, key)
		}
		relatedByID[key] = document
	}
	for _, document := range documents {
		key := str(document, "id")
		roles := roleMap[key]
		if roles == nil {
			roles = []string{}
		}
		document["connection_roles"] = roles
		isMentioned, isOwned := false, false
		for _, role := range roles {
			if role == "mentioned" {
				isMentioned = true
			}
			if role == "owner" || role == "creator" {
				isOwned = true
			}
		}
		if isMentioned {
			mentioned = append(mentioned, document)
		}
		if authoredIDs[key] {
			authored = append(authored, document)
			delete(relatedByID, key)
		} else if isOwned {
			owned = append(owned, document)
			delete(relatedByID, key)
		} else if isMentioned {
			if _, ok := relatedByID[key]; !ok {
				relatedOrder = append(relatedOrder, key)
			}
			relatedByID[key] = document
		}
	}
	visibleRelated := []Record{}
	seen := map[string]bool{}
	for _, key := range relatedOrder {
		if document, ok := relatedByID[key]; ok && !seen[key] {
			visibleRelated = append(visibleRelated, document)
			seen[key] = true
		}
	}
	return Record{"id": id, "title": person["title"], "meetings": meetings, "owned_documents": owned, "mentioned_documents": mentioned, "related_documents": visibleRelated, "documents": authored, "interviews": interviews}, nil
}

func (l *Library) GetProject(id string) (Record, error) {
	project, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if str(project, "kind") != "project" {
		return nil, errors.New("This item is not a project.")
	}
	name := str(mapping(project["metadata"]), "obsidian_stem")
	if name == "" {
		name = str(project, "title")
	}
	entity, err := l.row("SELECT id FROM entities WHERE kind='project' AND (profile_id=? OR name=?) ORDER BY profile_id IS NOT NULL DESC LIMIT 1", id, name)
	if err != nil {
		return nil, err
	}
	related := []Record{}
	if entity != nil {
		related, err = l.Listing(Record{"tag_ids": []string{str(entity, "id")}})
		if err != nil {
			return nil, err
		}
	}
	documents, meetings, updates, other := []Record{}, []Record{}, []Record{}, []Record{}
	for _, item := range related {
		if str(item, "id") == id {
			continue
		}
		switch str(item, "kind") {
		case "document", "reference":
			full, e := l.Get(str(item, "id"), false, false)
			if e != nil {
				return nil, e
			}
			documents = append(documents, full)
		case "meeting", "one-on-one":
			meetings = append(meetings, item)
		case "update", "blog":
			updates = append(updates, item)
		default:
			other = append(other, item)
		}
	}
	return Record{"id": id, "title": project["title"], "documents": documents, "meetings": meetings, "updates": updates, "other_notes": other}, nil
}

func (l *Library) LinkInterview(id string, candidates []string) (Record, error) {
	names, err := authorNames(candidates)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("Supply at least one interview candidate.")
	}
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	kind := str(item, "kind")
	if kind != "meeting" && kind != "one-on-one" && kind != "interview" {
		return nil, errors.New("Interview candidates require a meeting or interview.")
	}
	people := []Record{}
	for _, candidate := range names {
		name := candidate
		rows, e := l.rows("SELECT id,deleted_at FROM items WHERE kind='person' AND (lower(title)=lower(?) OR lower(json_extract(metadata,'$.obsidian_stem'))=lower(?)) ORDER BY deleted_at IS NOT NULL,created", name, name)
		if e != nil {
			return nil, e
		}
		if len(rows) > 0 && truth(rows[0]["deleted_at"]) {
			return nil, errors.New("Candidate is in Trash; restore the person explicitly: " + name)
		}
		personID := ""
		if len(rows) > 0 {
			personID = str(rows[0], "id")
			person, e := l.Get(personID, false, false)
			if e != nil {
				return nil, e
			}
			name = str(mapping(person["metadata"]), "obsidian_stem")
			if name == "" {
				name = str(person, "title")
			}
		} else {
			created, e := l.Ingest(Record{"title": name, "summary": "## Overview\nInterview candidate. See linked interviews for discussion, evidence, and source transcripts. No hiring status is inferred.\n", "kind": "person", "source_key": "interview-person:" + caseFold(name), "tags": []Record{{"category": "person", "name": name}, {"category": "topic", "name": "Interview Candidate"}}})
			if e != nil {
				return nil, e
			}
			personID = str(created, "id")
		}
		people = append(people, Record{"id": personID, "name": name})
	}
	err = l.transaction(func() error {
		changes := 0
		interview, e := l.Tag("Interview", "type")
		if e != nil {
			return e
		}
		added, e := l.linkOrganizationTag(id, interview)
		if e != nil {
			return e
		}
		if added {
			changes++
		}
		for _, person := range people {
			tag, e := l.Tag(str(person, "name"), "person")
			if e != nil {
				return e
			}
			added, e := l.linkOrganizationTag(id, tag)
			if e != nil {
				return e
			}
			if added {
				changes++
			}
		}
		if changes > 0 {
			return l.exec("UPDATE items SET revision=revision+1 WHERE id=?", id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Record{"item_id": id, "people": people}, nil
}

func (l *Library) linkOrganizationTag(id, tag string) (bool, error) {
	present, err := l.row("SELECT 1 FROM item_tags WHERE item_id=? AND tag_id=?", id, tag)
	if err != nil {
		return false, err
	}
	if err = l.exec("INSERT OR IGNORE INTO item_tags VALUES(?,?)", id, tag); err != nil {
		return false, err
	}
	return present == nil, nil
}
