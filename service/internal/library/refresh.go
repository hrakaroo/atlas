package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var pythonClockPattern = regexp.MustCompile(`^([0-9]{2})(?::([0-9]{2}))?(?::([0-9]{2}))?(?:\.([0-9]{3}|[0-9]{6}))?$`)
var pythonOffsetPattern = regexp.MustCompile(`^([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]{6}))?)?$`)

func parseGoogleTime(value string) (time.Time, error) {
	input := strings.ReplaceAll(value, "Z", "+00:00")
	invalid := func() (time.Time, error) { return time.Time{}, fmt.Errorf("Invalid isoformat string: '%s'", input) }
	runes := []rune(input)
	if len(runes) < 10 {
		return invalid()
	}
	dateText := string(runes[:10])
	if validDate(dateText) != nil {
		return invalid()
	}
	date, _ := time.Parse("2006-01-02", dateText)
	if len(runes) == 10 {
		return time.Time{}, errors.New("Google timestamps need a timezone.")
	}
	clock := string(runes[11:])
	zone := ""
	hasZone := false
	sign := time.Duration(1)
	for index, char := range clock {
		if char == '+' || char == '-' {
			hasZone = true
			zone = clock[index+1:]
			if char == '-' {
				sign = -1
			}
			clock = clock[:index]
			break
		}
	}
	match := pythonClockPattern.FindStringSubmatch(clock)
	if match == nil {
		return invalid()
	}
	numberPart := func(text string) int { value, _ := strconv.Atoi(text); return value }
	hour, minute, second := numberPart(match[1]), numberPart(match[2]), numberPart(match[3])
	if hour > 23 || minute > 59 || second > 59 {
		return invalid()
	}
	micros := numberPart(match[4])
	if len(match[4]) == 3 {
		micros *= 1000
	}
	local := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, second, micros*1000, time.UTC)
	if !hasZone {
		return time.Time{}, errors.New("Google timestamps need a timezone.")
	}
	offset := pythonOffsetPattern.FindStringSubmatch(zone)
	if offset == nil {
		return invalid()
	}
	hours, minutes, seconds := numberPart(offset[1]), numberPart(offset[2]), numberPart(offset[3])
	zoneMicros := numberPart(offset[4])
	// Python 3.9 treats an all-zero H:M:S offset as UTC, even with fractional
	// offset precision; otherwise timedelta normalizes the components.
	if hours == 0 && minutes == 0 && seconds == 0 {
		zoneMicros = 0
	}
	duration := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(zoneMicros)*time.Microsecond
	if duration >= 24*time.Hour {
		return invalid()
	}
	return local.Add(-sign * duration), nil
}

type googleSelectionError struct{ message string }

func (e *googleSelectionError) Error() string { return e.message }

func (l *Library) googleDocument(id string) (Record, string, string, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, "", "", err
	}
	if item["kind"] != "document" && item["kind"] != "reference" {
		return nil, "", "", &googleSelectionError{"Only Google documents can be refreshed."}
	}
	all := records(item["source_links"])
	primary := []Record{}
	for _, link := range all {
		if truth(link["primary"]) && link["kind"] == "document" {
			primary = append(primary, link)
		}
	}
	selected := all
	if len(primary) > 0 {
		selected = primary
	}
	urls := []string{}
	for _, link := range selected {
		if googleDocID(str(link, "url")) != "" {
			urls = append(urls, str(link, "url"))
		}
	}
	urls = normalizedDocURLs(urls)
	identities := map[string]bool{}
	for _, url := range urls {
		identities[googleDocID(url)] = true
	}
	if len(identities) != 1 {
		return nil, "", "", &googleSelectionError{"Refresh requires one unambiguous Google document source."}
	}
	for identity := range identities {
		return item, urls[0], identity, nil
	}
	panic("unreachable")
}
func (l *Library) ListGoogleDocuments(mode string, recentFileIDs []string) ([]Record, error) {
	if mode != "all" && mode != "dirty" && mode != "recent" {
		return nil, errors.New("Choose all, dirty, or recent.")
	}
	cutoff := l.Now().UTC().Add(-30 * 24 * time.Hour)
	allowed := map[string]bool{}
	for _, id := range recentFileIDs {
		allowed[id] = true
	}
	rows, err := l.rows("SELECT id FROM items WHERE deleted_at IS NULL AND kind IN ('document','reference')")
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range rows {
		item, url, fileID, e := l.googleDocument(str(row, "id"))
		if e != nil {
			var selection *googleSelectionError
			if errors.As(e, &selection) {
				continue
			}
			return nil, e
		}
		metadata := mapping(item["metadata"])
		sync := mapping(metadata["google_sync"])
		dirty := truth(metadata["refresh_requested"])
		recent := allowed[fileID]
		for _, field := range []string{"created_time", "modified_time"} {
			if truth(sync[field]) && !recent {
				when, e := parseGoogleTime(s(sync[field]))
				if e != nil {
					return nil, e
				}
				recent = recent || !when.Before(cutoff)
			}
		}
		if mode == "dirty" && !dirty {
			continue
		}
		if mode == "recent" && !dirty && !recent {
			continue
		}
		result = append(result, Record{"id": item["id"], "title": item["title"], "file_id": fileID, "url": url, "revision": item["revision"], "refresh_requested": dirty, "google_sync": sync})
	}
	return result, nil
}
func (l *Library) refreshSource(id string, revision int64, source Record) (Record, error) {
	item, _, fileID, err := l.googleDocument(id)
	if err != nil {
		return nil, err
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The item changed. Read it again before refreshing.")
	}
	if source["file_id"] != fileID {
		return nil, errors.New("Google source identity does not match this document.")
	}
	title, titleOK := source["title"].(string)
	if !truth(source["modified_time"]) || !truth(source["created_time"]) || !titleOK || strings.TrimSpace(title) == "" {
		return nil, errors.New("Google title, creation time and modification time are required.")
	}
	for _, field := range []string{"created_time", "modified_time"} {
		if _, err = parseGoogleTime(str(source, field)); err != nil {
			return nil, err
		}
	}
	return item, nil
}
func (l *Library) SetRefreshRequested(id string, requested bool) (Record, error) {
	item, _, _, err := l.googleDocument(id)
	if err != nil {
		return nil, err
	}
	metadata := mapping(item["metadata"])
	metadata["refresh_requested"] = requested
	if err = l.transaction(func() error {
		return l.exec("UPDATE items SET metadata=?,revision=revision+1 WHERE id=?", jsonText(metadata), id)
	}); err != nil {
		return nil, err
	}
	return l.Get(id, false, false)
}
func googleHasPeople(source Record) bool {
	for _, key := range []string{"owners", "permissions", "creator", "created_by"} {
		if _, exists := source[key]; exists {
			return true
		}
	}
	return false
}
func (l *Library) CheckDocumentSource(id string, revision int64, source Record) (Record, error) {
	item, err := l.refreshSource(id, revision, source)
	if err != nil {
		return nil, err
	}
	if googleHasPeople(source) {
		if _, err = l.applyGooglePeople(id, source, nil); err != nil {
			return nil, err
		}
		item, err = l.Get(id, false, false)
		if err != nil {
			return nil, err
		}
	}
	metadata := mapping(item["metadata"])
	sync := mapping(metadata["google_sync"])
	version := source["version"]
	if _, exists := source["version"]; !exists {
		version = ""
	}
	previousVersion := sync["version"]
	if _, exists := sync["version"]; !exists {
		previousVersion = ""
	}
	same := jsonText(sync["modified_time"]) == jsonText(source["modified_time"]) && jsonText(previousVersion) == jsonText(version) && jsonText(sync["title"]) == jsonText(source["title"]) && truth(sync["content_hash"])
	if truth(metadata["refresh_requested"]) || !same {
		if !truth(metadata["refresh_requested"]) {
			item, err = l.SetRefreshRequested(id, true)
			if err != nil {
				return nil, err
			}
		}
		return Record{"id": id, "revision": item["revision"], "needs_content": true}, nil
	}
	sync["last_checked"] = l.now()
	metadata["google_sync"] = sync
	if err = l.transaction(func() error { return l.exec("UPDATE items SET metadata=? WHERE id=?", jsonText(metadata), id) }); err != nil {
		return nil, err
	}
	return Record{"id": id, "revision": item["revision"], "needs_content": false}, nil
}
func (l *Library) InspectDocumentContent(id, text string) (Record, error) {
	item, _, _, err := l.googleDocument(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("A complete nonempty source snapshot is required.")
	}
	digest := documentHash(text)
	old := mapping(mapping(item["metadata"])["google_sync"])["content_hash"]
	if !truth(old) {
		previous, e := l.row("SELECT source_text FROM search WHERE item_id=?", id)
		if e != nil {
			return nil, e
		}
		if previous != nil {
			old = documentHash(str(previous, "source_text"))
		} else {
			old = nil
		}
	}
	return Record{"id": id, "revision": item["revision"], "content_hash": digest, "content_changed": digest != old}, nil
}
func (l *Library) RefreshDocument(id string, revision int64, source Record, text string, summary any) (Record, error) {
	item, err := l.refreshSource(id, revision, source)
	if err != nil {
		return nil, err
	}
	if googleHasPeople(source) {
		if _, _, _, err = l.googlePeopleInput(source, nil); err != nil {
			return nil, err
		}
	}
	inspected, err := l.InspectDocumentContent(id, text)
	if err != nil {
		return nil, err
	}
	changed := truth(inspected["content_changed"])
	body := str(item, "summary")
	if changed {
		value, ok := summary.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, errors.New("Changed source content requires a refreshed summary.")
		}
		body = normalizeDocLinks(value)
	}
	acknowledgeQuiet := false
	if !changed && source["title"] == item["title"] {
		previous, e := l.row("SELECT content_hash FROM activity_state WHERE item_id=?", id)
		if e != nil {
			return nil, e
		}
		acknowledgeQuiet = previous == nil || str(previous, "content_hash") == activityContentHash(item)
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	metadata := cloneGoogleRecord(mapping(item["metadata"]))
	sync := Record{}
	for _, key := range []string{"file_id", "title", "created_time", "modified_time", "version"} {
		if value, exists := source[key]; exists {
			sync[key] = value
		}
	}
	sync["content_hash"] = inspected["content_hash"]
	sync["last_checked"] = l.now()
	metadata["google_sync"] = sync
	metadata["refresh_requested"] = false
	if source["title"] != item["title"] {
		aliases := []string{}
		if value, ok := metadata["aliases"].(string); ok {
			aliases = append(aliases, value)
		} else {
			aliases, err = stringsArg(metadata["aliases"])
			if err != nil {
				return nil, err
			}
		}
		aliases = append(aliases, str(item, "title"))
		unique := []string{}
		seen := map[string]bool{}
		for _, alias := range aliases {
			if !seen[alias] {
				seen[alias] = true
				unique = append(unique, alias)
			}
		}
		metadata["aliases"] = unique
	}
	snapshotID, err := newID()
	if err != nil {
		return nil, err
	}
	snapshot := filepath.Join(folder, "originals", "google-refresh-"+snapshotID+".txt")
	intent := Record{"version": 1, "snapshot": filepath.Base(snapshot), "old_summary": str(item, "summary"), "new_summary": body}
	if _, e := os.Lstat(filepath.Join(folder, "pending-refresh.json")); e == nil {
		l.recoveryErr = errors.New("Pending refresh recovery requires review")
		return nil, l.recoveryErr
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if err = l.atomic(filepath.Join(folder, "pending-refresh.json"), jsonText(intent)); err != nil {
		return nil, err
	}
	defer func() { _ = l.recoverPendingRefresh(id) }()
	if err = l.atomic(snapshot, text); err != nil {
		return nil, err
	}
	if changed {
		if err = l.atomic(filepath.Join(folder, "revisions", fmt.Sprint(item["revision"])+".md"), str(item, "summary")); err != nil {
			return nil, err
		}
		if err = l.atomic(filepath.Join(folder, "summary.md"), body); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256([]byte(text))
	inbox := item["inbox"]
	if changed {
		inbox = int64(1)
	}
	err = l.transaction(func() error {
		if e := l.exec("UPDATE items SET title=?,summary=?,source_path=?,source_hash=?,metadata=?,inbox=?,revision=revision+1,updated=? WHERE id=?", source["title"], body, snapshot, hex.EncodeToString(sum[:]), jsonText(metadata), inbox, sync["last_checked"], id); e != nil {
			return e
		}
		if e := l.exec("UPDATE search SET title=?,summary=?,source_text=? WHERE item_id=?", source["title"], body, text, id); e != nil {
			return e
		}
		if e := l.indexLinks(id, body); e != nil {
			return e
		}
		if changed || source["title"] != item["title"] {
			current, e := l.Get(id, false, false)
			if e != nil {
				return e
			}
			if e := l.activityEvent(current, "refreshed", "", nil, false); e != nil {
				return e
			}
		}
		if acknowledgeQuiet {
			if e := l.rememberActivityState(id); e != nil {
				return e
			}
		}
		return l.contractPoint("refresh.before_commit")
	})
	if err != nil {
		if recoveryErr := l.recoverPendingRefresh(id); recoveryErr != nil {
			return nil, errors.Join(err, recoveryErr)
		}
		return nil, err
	}
	if err = l.contractPoint("refresh.after_commit"); err != nil {
		return nil, err
	}
	if err = l.recoverPendingRefresh(id); err != nil {
		return nil, err
	}
	if googleHasPeople(source) {
		if _, err = l.applyGooglePeople(id, source, nil); err != nil {
			return nil, err
		}
	}
	current, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	return Record{"id": id, "content_changed": changed, "item": current}, nil
}
