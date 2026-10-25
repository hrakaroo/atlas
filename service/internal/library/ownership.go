package library

import (
	"errors"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"
)

func cloneGoogleRecord(input Record) Record {
	result := Record{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

// structuredDocumentPeople accepts mention identities only from explicit Google
// Docs people chips or mailto links. Prose names and free-form evidence are not
// identity evidence. Normalize the complete batch before creating any profiles.
func structuredDocumentPeople(people []Record) ([]Record, error) {
	normalized := make([]Record, 0, len(people))
	for _, input := range people {
		person := cloneGoogleRecord(input)
		if str(person, "role") != "mentioned" {
			normalized = append(normalized, person)
			continue
		}
		email := caseFold(clean(str(person, "email")))
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
			return nil, errors.New("Mention links require an explicit email from a Google Docs people chip or mailto link.")
		}
		chip := mapping(person["person_chip"])
		link := str(person, "person_link")
		if len(chip) > 0 && link != "" {
			return nil, errors.New("Supply one structured identity source per mention.")
		}
		if len(chip) > 0 {
			properties := mapping(chip["personProperties"])
			chipEmail := caseFold(clean(str(properties, "email")))
			chipName := clean(str(properties, "name"))
			if clean(str(chip, "personId")) == "" || chipEmail != email {
				return nil, errors.New("People-chip identity must include its observed personId and matching email.")
			}
			if name := clean(str(person, "name")); chipName != "" && name != "" && caseFold(name) != caseFold(chipName) {
				return nil, errors.New("Mention name must match the observed people-chip name.")
			}
			person["name"] = chipName
			person["evidence"] = "Google Docs people chip " + str(chip, "personId") + " (" + email + ")"
		} else if link != "" {
			target, parseErr := url.Parse(link)
			if parseErr != nil || target.Scheme != "mailto" || target.Host != "" || target.Fragment != "" {
				return nil, errors.New("Person links must be explicit single-address mailto links.")
			}
			address, decodeErr := url.PathUnescape(target.Opaque)
			if decodeErr != nil || caseFold(address) != email {
				return nil, errors.New("Person-link address must match the supplied email.")
			}
			person["evidence"] = "Google Docs person link " + link
		} else {
			return nil, errors.New("Plain-text names cannot create mention links; supply person_chip or person_link from structured Google Docs content.")
		}
		person["email"] = email
		normalized = append(normalized, person)
	}
	return normalized, nil
}

type documentPerson struct{ id, role, evidence, email string }

func (l *Library) LinkDocumentPeople(id string, people []Record, ownershipStatus string, replaceRoles []string) (Record, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if item["kind"] != "document" && item["kind"] != "reference" {
		return nil, errors.New("Document people require a document.")
	}
	people, err = structuredDocumentPeople(people)
	if err != nil {
		return nil, err
	}
	roles := map[string]bool{"owner": true, "creator": true, "mentioned": true}
	for _, person := range people {
		if !roles[str(person, "role")] || clean(str(person, "evidence")) == "" {
			return nil, errors.New("Supply people with owner, creator or mentioned roles and explicit evidence.")
		}
	}
	replace := map[string]bool{}
	for _, role := range replaceRoles {
		if !roles[role] {
			return nil, errors.New("Unknown document relationship.")
		}
		replace[role] = true
	}
	if ownershipStatus != "" && ownershipStatus != "available" && ownershipStatus != "unavailable" && ownershipStatus != "no_individual_owner" {
		return nil, errors.New("Unknown ownership status.")
	}
	prepared := []documentPerson{}
	mentionProfiles := map[string]string{}
	issues := []string{}
	for _, person := range people {
		name, email := clean(str(person, "name")), caseFold(clean(str(person, "email")))
		if str(person, "role") == "mentioned" && mentionProfiles[email] != "" {
			prepared = append(prepared, documentPerson{mentionProfiles[email], "mentioned", str(person, "evidence"), email})
			continue
		}
		rows, e := l.rows(`SELECT DISTINCT i.id,i.deleted_at FROM items i
 LEFT JOIN person_emails e ON e.person_id=i.id LEFT JOIN meeting_people m ON m.person_id=i.id
 WHERE i.kind='person' AND ((?<>'' AND (e.email=? OR lower(m.email)=?))
 OR (?<>'' AND (lower(i.title)=lower(?) OR lower(json_extract(i.metadata,'$.obsidian_stem'))=lower(?))))
 ORDER BY i.deleted_at IS NOT NULL`, email, email, email, name, name, name)
		if e != nil {
			return nil, e
		}
		if str(person, "role") == "mentioned" {
			// Explicit email outranks display spelling. A conflicting same-name
			// profile is unresolved rather than merged or duplicated.
			rows, e = l.rows(`SELECT DISTINCT i.id,i.deleted_at FROM items i
                LEFT JOIN person_emails e ON e.person_id=i.id LEFT JOIN meeting_people m ON m.person_id=i.id
                WHERE i.kind='person' AND (e.email=? OR lower(m.email)=?)
                ORDER BY i.deleted_at IS NOT NULL`, email, email)
			if e != nil {
				return nil, e
			}
			if len(rows) == 0 {
				candidates, lookupErr := l.rows(`SELECT DISTINCT i.id,i.deleted_at FROM items i
                    WHERE i.kind='person' AND (lower(i.title)=lower(?) OR lower(json_extract(i.metadata,'$.obsidian_stem'))=lower(?))`, name, name)
				if lookupErr != nil {
					return nil, lookupErr
				}
				conflict := false
				for _, candidate := range candidates {
					known, lookupErr := l.rows(`SELECT email FROM person_emails WHERE person_id=?
                        UNION SELECT lower(email) AS email FROM meeting_people WHERE person_id=? AND email<>''`, candidate["id"], candidate["id"])
					if lookupErr != nil {
						return nil, lookupErr
					}
					if len(known) > 0 {
						conflict = true
						break
					}
				}
				if conflict {
					issues = append(issues, "Conflicting identity: "+name)
					continue
				}
				rows = candidates
			}
		}
		identity := name
		if identity == "" {
			identity = email
		}
		if len(rows) > 1 {
			issues = append(issues, "Ambiguous identity: "+identity)
			continue
		}
		if len(rows) > 0 && truth(rows[0]["deleted_at"]) {
			issues = append(issues, "Person is in Trash: "+identity)
			continue
		}
		profile := ""
		if len(rows) > 0 {
			profile = str(rows[0], "id")
		} else if len(strings.Fields(name)) >= 2 {
			entity, e := l.CreateEntity(name, "person")
			if e != nil {
				return nil, e
			}
			profile = str(entity, "profile_id")
		} else {
			if identity == "" {
				identity = str(person, "role")
			}
			issues = append(issues, "Full identity unavailable: "+identity)
			continue
		}
		if str(person, "role") == "mentioned" {
			mentionProfiles[email] = profile
		}
		prepared = append(prepared, documentPerson{profile, str(person, "role"), clean(str(person, "evidence")), email})
	}
	changed := false
	err = l.transaction(func() error {
		if len(issues) == 0 {
			for role := range replace {
				rows, e := l.rows("SELECT person_id,role,evidence,email FROM document_people WHERE item_id=? AND role=?", id, role)
				if e != nil {
					return e
				}
				existing, incoming := map[documentPerson]bool{}, map[documentPerson]bool{}
				for _, row := range rows {
					existing[documentPerson{str(row, "person_id"), str(row, "role"), str(row, "evidence"), str(row, "email")}] = true
				}
				for _, person := range prepared {
					if person.role == role {
						incoming[person] = true
					}
				}
				same := len(existing) == len(incoming)
				for key := range existing {
					if !incoming[key] {
						same = false
					}
				}
				if !same {
					if e = l.exec("DELETE FROM document_people WHERE item_id=? AND role=?", id, role); e != nil {
						return e
					}
					changed = true
				}
			}
		}
		for _, person := range prepared {
			previous, e := l.row("SELECT evidence,email FROM document_people WHERE item_id=? AND person_id=? AND role=?", id, person.id, person.role)
			if e != nil {
				return e
			}
			if previous == nil || str(previous, "evidence") != person.evidence || str(previous, "email") != person.email {
				if e = l.exec("INSERT OR REPLACE INTO document_people VALUES(?,?,?,?,?)", id, person.id, person.role, person.evidence, person.email); e != nil {
					return e
				}
				changed = true
			}
			if person.email != "" {
				if e = l.exec("INSERT OR IGNORE INTO person_emails VALUES(?,?)", person.email, person.id); e != nil {
					return e
				}
			}
		}
		metadata := cloneGoogleRecord(mapping(item["metadata"]))
		old := mapping(metadata["document_people"])
		state := cloneGoogleRecord(old)
		if ownershipStatus != "" {
			state["ownership_status"] = ownershipStatus
		}
		if len(people) > 0 || len(replace) > 0 || ownershipStatus != "" {
			state["issues"] = issues
			if jsonText(state) != jsonText(old) {
				metadata["document_people"] = state
				changed = true
			}
		}
		if changed {
			if e := l.exec("UPDATE items SET metadata=?,revision=revision+1 WHERE id=?", jsonText(metadata), id); e != nil {
				return e
			}
			if len(prepared) > 0 || len(replace) > 0 {
				current, e := l.Get(id, false, false)
				if e != nil {
					return e
				}
				if e = l.activityEvent(current, "people_linked", "", nil, false); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Record{"item_id": id, "changed": changed, "issues": issues}, nil
}
func googleIdentityFields(identity Record) (string, string) {
	name, email := "", ""
	for _, key := range []string{"displayName", "display_name", "name"} {
		if truth(identity[key]) {
			name = s(identity[key])
			break
		}
	}
	for _, key := range []string{"emailAddress", "email_address", "email"} {
		if truth(identity[key]) {
			email = s(identity[key])
			break
		}
	}
	return name, email
}
func (l *Library) googlePeopleInput(source Record, people []Record) ([]Record, string, []string, error) {
	prepared, err := structuredDocumentPeople(people)
	if err != nil {
		return nil, "", nil, err
	}
	replace := []string{}
	fileID, exists := source["file_id"]
	if !exists {
		return nil, "", nil, errors.New("'file_id'")
	}
	evidence := "Google Drive owners metadata for " + s(fileID)
	owners := []Record{}
	status := ""
	if value, exists := source["owners"]; exists {
		if value == nil {
			return nil, "", nil, errors.New("Google owners must be a list of observed metadata identities.")
		}
		parsed, err := recordsArg(value)
		if err != nil {
			return nil, "", nil, errors.New("Google owners must be a list of observed metadata identities.")
		}
		owners = parsed
		status = "available"
		if len(owners) == 0 {
			status = "no_individual_owner"
		}
	} else {
		if value := source["permissions"]; value != nil {
			permissions, err := recordsArg(value)
			if err != nil {
				return nil, "", nil, errors.New("Google permissions must be a list of observed metadata objects.")
			}
			for _, permission := range permissions {
				if permission["type"] == "user" && permission["role"] == "owner" && !truth(permission["deleted"]) && !truth(permission["pendingOwner"]) && !truth(permission["pending_owner"]) {
					owners = append(owners, permission)
				}
			}
		}
		status = "available"
		if len(owners) == 0 {
			status = "unavailable"
		}
		evidence = "Google Drive permissions role=owner for " + s(fileID)
	}
	if status != "unavailable" {
		replace = append(replace, "owner")
		for _, owner := range owners {
			name, email := googleIdentityFields(owner)
			prepared = append(prepared, Record{"role": "owner", "name": name, "email": email, "evidence": evidence})
		}
	}
	creator := source["creator"]
	if !truth(creator) {
		creator = source["created_by"]
	}
	if truth(creator) {
		identity, ok := creator.(map[string]any)
		if !ok {
			return nil, "", nil, errors.New("Google creator identity must be a metadata object.")
		}
		replace = append(replace, "creator")
		name, email := googleIdentityFields(identity)
		prepared = append(prepared, Record{"role": "creator", "name": name, "email": email, "evidence": "Explicit Google creator metadata for " + s(fileID)})
	}
	for _, person := range prepared {
		role := str(person, "role")
		if (role != "owner" && role != "creator" && role != "mentioned") || clean(str(person, "evidence")) == "" {
			return nil, "", nil, errors.New("Document people require an explicit role and identity evidence.")
		}
	}
	return prepared, status, replace, nil
}
func (l *Library) applyGooglePeople(id string, source Record, people []Record) (Record, error) {
	parsed, status, replace, err := l.googlePeopleInput(source, people)
	if err != nil {
		return nil, err
	}
	result, err := l.LinkDocumentPeople(id, parsed, status, replace)
	if err != nil {
		return nil, err
	}
	if status != "unavailable" {
		err = l.transaction(func() error { return l.exec("DELETE FROM ownership_provider WHERE id=1") })
	}
	return result, err
}
func pythonGoogleTime(value time.Time) string {
	value = value.UTC()
	result := value.Format("2006-01-02T15:04:05")
	micros := value.Nanosecond() / 1000
	if micros != 0 {
		result += value.Format(".000000")
	}
	return result + "+00:00"
}
func ownershipRetry(now time.Time, attempts int64) string {
	hours := []time.Duration{6, 24, 72, 168}
	index := attempts - 1
	if index > 3 {
		index = 3
	}
	if index < 0 {
		index = 0
	}
	return pythonGoogleTime(now.Add(hours[index] * time.Hour))
}
func (l *Library) ListOwnershipLookups(limit int64) (Record, error) {
	now := l.Now().UTC()
	provider, err := l.row("SELECT * FROM ownership_provider WHERE id=1")
	if err != nil {
		return nil, err
	}
	if provider != nil {
		when, e := parseGoogleTime(str(provider, "next_attempt"))
		if e != nil {
			return nil, e
		}
		if when.After(now) {
			return Record{"documents": []Record{}, "paused_until": provider["next_attempt"]}, nil
		}
	}
	docs, err := l.ListGoogleDocuments("all", nil)
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, doc := range docs {
		item, e := l.Get(str(doc, "id"), false, false)
		if e != nil {
			return nil, e
		}
		state := mapping(mapping(item["metadata"])["document_people"])
		if state["ownership_status"] == "no_individual_owner" {
			continue
		}
		owner := false
		for _, person := range records(item["document_people"]) {
			if person["role"] == "owner" {
				owner = true
			}
		}
		if owner && !truth(state["issues"]) {
			continue
		}
		lookup, e := l.row("SELECT * FROM ownership_lookups WHERE item_id=?", doc["id"])
		if e != nil {
			return nil, e
		}
		if lookup != nil {
			when, e := parseGoogleTime(str(lookup, "next_attempt"))
			if e != nil {
				return nil, e
			}
			if when.After(now) {
				continue
			}
		}
		row := cloneGoogleRecord(doc)
		row["attempts"] = int64(0)
		row["last_attempt"] = nil
		if lookup != nil {
			row["attempts"] = lookup["attempts"]
			row["last_attempt"] = lookup["last_attempt"]
		}
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := str(result[i], "last_attempt"), str(result[j], "last_attempt")
		if a != b {
			return a < b
		}
		return str(result[i], "id") < str(result[j], "id")
	})
	if limit < 0 {
		limit = 0
	}
	if limit > 3 {
		limit = 3
	}
	if int64(len(result)) > limit {
		result = result[:limit]
	}
	return Record{"documents": result, "paused_until": nil}, nil
}
func (l *Library) RecordOwnershipLookup(id string, revision int64, source Record, lookupError any, providerUnavailable bool) (Record, error) {
	item, _, fileID, err := l.googleDocument(id)
	if err != nil {
		return nil, err
	}
	if truth(item["deleted_at"]) {
		return nil, errors.New("Removed documents cannot receive automatic ownership updates.")
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The item changed. Read it again before linking ownership.")
	}
	if providerUnavailable && lookupError == nil {
		return nil, errors.New("Provider unavailability requires an explicit lookup error.")
	}
	if (source == nil) == (lookupError == nil) {
		return nil, errors.New("Supply observed source metadata or a lookup error.")
	}
	now := l.Now().UTC()
	old, err := l.row("SELECT * FROM ownership_lookups WHERE item_id=?", id)
	if err != nil {
		return nil, err
	}
	attempts := int64(1)
	if old != nil {
		attempts += number(old["attempts"])
	}
	provider, err := l.row("SELECT attempts FROM ownership_provider WHERE id=1")
	if err != nil {
		return nil, err
	}
	changed := false
	outcome, detail, status := "", "", ""
	if lookupError != nil {
		text, ok := lookupError.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, errors.New("A lookup error needs a reason.")
		}
		outcome = "failed"
		if providerUnavailable {
			outcome = "unavailable"
		}
		detail = strings.TrimSpace(text)
	} else {
		if source["file_id"] != fileID {
			return nil, errors.New("Google source identity does not match this document.")
		}
		_, parsedStatus, _, e := l.googlePeopleInput(source, nil)
		if e != nil {
			return nil, e
		}
		status = parsedStatus
		_, hasOwners := source["owners"]
		permissionOwner := status == "available" && !hasOwners && source["source_visibility_status"] == "permission_metadata_available"
		mime := source["mime_type"]
		if _, exists := source["mime_type"]; !exists {
			mime = source["mimeType"]
		}
		trashed, hasTrashed := source["trashed"].(bool)
		if mime != "application/vnd.google-apps.document" || (hasTrashed && trashed) || (!(hasTrashed && !trashed) && !permissionOwner) {
			return nil, errors.New("Ownership requires readable native Google Doc metadata; reject known trashed sources.")
		}
		result, e := l.applyGooglePeople(id, source, nil)
		if e != nil {
			return nil, e
		}
		changed = truth(result["changed"])
		after, e := l.Get(id, false, false)
		if e != nil {
			return nil, e
		}
		if status == "unavailable" {
			outcome = "unavailable"
			detail = "Google metadata did not provide owners or an individual owner permission."
		} else if issues, e := stringsArg(result["issues"]); e == nil && len(issues) > 0 {
			outcome = "unresolved"
			detail = strings.Join(issues, "; ")
		} else {
			owner := false
			for _, person := range records(after["document_people"]) {
				if person["role"] == "owner" {
					owner = true
				}
			}
			if status == "no_individual_owner" || owner {
				outcome = "resolved"
			} else {
				outcome = "unresolved"
				detail = "Owner identity could not be linked."
			}
		}
	}
	next := ownershipRetry(now, attempts)
	err = l.transaction(func() error {
		if e := l.exec("INSERT OR REPLACE INTO ownership_lookups VALUES(?,?,?,?,?,?)", id, attempts, pythonGoogleTime(now), next, outcome, detail); e != nil {
			return e
		}
		if outcome == "unavailable" {
			count := int64(1)
			if provider != nil {
				count += number(provider["attempts"])
			}
			next = ownershipRetry(now, count)
			return l.exec("INSERT OR REPLACE INTO ownership_provider VALUES(1,?,?)", count, next)
		} else if source != nil && status != "unavailable" {
			return l.exec("DELETE FROM ownership_provider WHERE id=1")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	after, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	var nextValue any = next
	if outcome == "resolved" {
		nextValue = nil
	}
	notify := detail != "" && (old == nil || str(old, "detail") != detail) && !(outcome == "unavailable" && provider != nil)
	return Record{"id": id, "outcome": outcome, "changed": changed, "detail": detail, "notify": notify, "stop_owner_pass": outcome == "unavailable", "next_attempt": nextValue, "revision": after["revision"]}, nil
}
