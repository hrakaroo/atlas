package library

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (l *Library) feedOptions(id string) (Record, error) {
	row, err := l.row("SELECT * FROM feed_settings WHERE feed_id=?", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return Record{"flow": "append", "presentation": "entries", "entry_label": "Entries", "checked_at": nil}, nil
	}
	delete(row, "feed_id")
	return row, nil
}
func (l *Library) ConfigureFeed(args Record) (Record, error) {
	name, err := requiredString(args, "name")
	if err != nil {
		return nil, err
	}
	flow, presentation := strDefault(args, "flow", "append"), strDefault(args, "presentation", "entries")
	if (flow != "append" && flow != "in_place") || (presentation != "entries" && presentation != "overview") {
		return nil, errors.New("Invalid feed flow or presentation.")
	}
	label := strings.TrimSpace(strDefault(args, "entry_label", "Entries"))
	if label == "" || len(label) > 80 {
		return nil, errors.New("Invalid feed entry label.")
	}
	var id string
	err = l.transaction(func() error {
		var e error
		id, e = l.ensureFeed(name)
		if e != nil {
			return e
		}
		previous, e := l.feedOptions(id)
		if e != nil {
			return e
		}
		count, e := l.row("SELECT count(*) AS count FROM blog_members WHERE feed_id=?", id)
		if e != nil {
			return e
		}
		if number(count["count"]) > 0 && previous["flow"] != flow {
			return errors.New("A populated feed cannot change its update flow.")
		}
		return l.exec(`INSERT INTO feed_settings(feed_id,flow,presentation,entry_label) VALUES(?,?,?,?)
            ON CONFLICT(feed_id) DO UPDATE SET flow=excluded.flow,presentation=excluded.presentation,entry_label=excluded.entry_label`, id, flow, presentation, label)
	})
	if err != nil {
		return nil, err
	}
	return l.GetBlogFeed(id)
}
func (l *Library) RecordFeedCheck(id string) (Record, error) {
	feed, err := l.row("SELECT id FROM blog_feeds WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, errors.New("Blog feed not found.")
	}
	options, err := l.feedOptions(id)
	if err != nil {
		return nil, err
	}
	now := l.now()
	err = l.transaction(func() error {
		return l.exec(`INSERT INTO feed_settings(feed_id,flow,presentation,entry_label,checked_at) VALUES(?,?,?,?,?)
        ON CONFLICT(feed_id) DO UPDATE SET checked_at=excluded.checked_at`, id, options["flow"], options["presentation"], options["entry_label"], now)
	})
	return Record{"feed_id": id, "checked_at": now}, err
}
func validFeedURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && !strings.ContainsAny(raw, "\n\r\t")
}
func (l *Library) UpsertFeedEntry(args Record) (Record, error) {
	id, err := requiredString(args, "feed_id")
	if err != nil {
		return nil, err
	}
	feed, err := l.row("SELECT * FROM blog_feeds WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if feed == nil {
		return nil, errors.New("Blog feed not found.")
	}
	options, err := l.feedOptions(id)
	if err != nil {
		return nil, err
	}
	key, err := requiredString(args, "entry_key")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" || len(key) > 500 || strings.ContainsAny(key, "\n\r") {
		return nil, errors.New("Invalid stable feed entry key.")
	}
	title, err := requiredString(args, "title")
	if err != nil {
		return nil, err
	}
	summary, err := requiredString(args, "summary")
	if err != nil {
		return nil, err
	}
	text, err := requiredString(args, "original_text")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" || strings.TrimSpace(summary) == "" || strings.TrimSpace(text) == "" {
		return nil, errors.New("A feed entry requires a title, summary and complete source text.")
	}
	summary = normalizeDocLinks(summary)
	sourceURL, err := requiredString(args, "source_url")
	if err != nil {
		return nil, err
	}
	if !validFeedURL(sourceURL) {
		return nil, errors.New("Feed sources require an HTTPS URL.")
	}
	observed, err := requiredString(args, "observed_at")
	if err != nil {
		return nil, err
	}
	observedTime, err := time.Parse(time.RFC3339Nano, observed)
	if err != nil {
		return nil, errors.New("Feed observation time must be RFC3339.")
	}
	observed = observedTime.UTC().Format(time.RFC3339Nano)
	context, e := objectArgument(args, "source_context", false)
	if e != nil {
		return nil, e
	}
	if context == nil {
		context = Record{}
	}
	if truth(context["window_start"]) || truth(context["window_end"]) {
		start, e1 := time.Parse(time.RFC3339Nano, str(context, "window_start"))
		end, e2 := time.Parse(time.RFC3339Nano, str(context, "window_end"))
		if e1 != nil || e2 != nil || end.Before(start) {
			return nil, errors.New("Invalid feed source coverage window.")
		}
	}
	fields, err := objectArgument(args, "fields", false)
	if err != nil {
		return nil, err
	}
	if fields == nil {
		fields = Record{}
	}
	if status, exists := fields["status"]; exists && status != "active" && status != "stable" && status != "resolved" && status != "unknown" {
		return nil, errors.New("Feed status must be active, stable, resolved or unknown.")
	}
	if value, exists := fields["severity"]; exists && value != nil {
		_, stringValue := value.(string)
		severity, e := integer(value)
		if e != nil || stringValue || severity < 1 || severity > 99 {
			return nil, errors.New("Feed severity must be a positive integer or null.")
		}
	}
	for _, field := range []string{"incident_url", "channel_url"} {
		if value, exists := fields[field]; exists {
			raw, ok := value.(string)
			if !ok || !validFeedURL(raw) {
				return nil, errors.New("Invalid feed source link.")
			}
		}
	}
	if value, exists := fields["external_id"]; exists {
		if _, ok := value.(string); !ok {
			return nil, errors.New("External feed identity must be a string.")
		}
	}
	revision, err := integer(args["expected_revision"])
	if err != nil || revision < 0 {
		return nil, errors.New("Supply expected_revision=0 for a new entry, or its current revision.")
	}
	sourceKey := "feed-entry:" + id + ":" + key
	existing, err := l.row("SELECT id,deleted_at FROM items WHERE source_key=?", sourceKey)
	if err != nil {
		return nil, err
	}
	if existing != nil && truth(existing["deleted_at"]) {
		return Record{"id": existing["id"], "status": "blocked"}, nil
	}
	entry := Record{"key": key, "fields": fields, "source_url": sourceURL, "observed_at": observed, "meaningful_at": l.now(), "source_context": context}
	if existing == nil {
		if revision != 0 {
			return nil, errors.New("Feed entry no longer exists; read the feed again.")
		}
		date := str(args, "date")
		if date == "" {
			date = l.today(observed)
		}
		imported, e := l.Ingest(Record{"title": title, "summary": summary, "kind": "blog", "blog_feed": feed["name"], "date": date, "source_key": sourceKey, "original_text": text, "original_name": strDefault(args, "original_name", "feed-source.txt"), "metadata": Record{"feed_entry": entry}, "automatic": true})
		if e != nil {
			return nil, e
		}
		if truth(imported["duplicate"]) {
			return Record{"id": imported["id"], "status": "blocked"}, nil
		}
		item, e := l.Get(str(imported, "id"), false, false)
		return Record{"id": imported["id"], "status": "created", "item": item}, e
	}
	item, err := l.Get(str(existing, "id"), false, false)
	if err != nil {
		return nil, err
	}
	if options["flow"] == "append" {
		return Record{"id": item["id"], "status": "duplicate", "item": item}, nil
	}
	if number(item["revision"]) != revision {
		return nil, errors.New("The feed entry changed. Read it again before updating.")
	}
	previous := mapping(mapping(item["metadata"])["feed_entry"])
	if previousTime, e := time.Parse(time.RFC3339Nano, str(previous, "observed_at")); e == nil && observedTime.Before(previousTime) {
		return nil, errors.New("Stale feed observation; newer information is already saved.")
	}
	// A different model phrasing over identical source material is not new information.
	sameSource := str(item, "source_hash") == hashText(text)
	sameFields := jsonText(previous["fields"]) == jsonText(fields) && previous["source_url"] == sourceURL
	if sameSource && sameFields && title == item["title"] {
		summary = str(item, "summary")
	}
	meaningful := title != item["title"] || summary != item["summary"] || !sameFields
	if !meaningful {
		entry["meaningful_at"] = previous["meaningful_at"]
	}
	metadata := cloneGoogleRecord(mapping(item["metadata"]))
	metadata["feed_entry"] = entry
	if !meaningful && sameSource {
		if observed != str(previous, "observed_at") || jsonText(previous["source_context"]) != jsonText(context) {
			// Observation bookkeeping does not create revisions or copy identical sources.
			if err = l.transaction(func() error { return l.exec("UPDATE items SET metadata=? WHERE id=?", jsonText(metadata), item["id"]) }); err != nil {
				return nil, err
			}
			item, err = l.Get(str(item, "id"), false, false)
		}
		return Record{"id": item["id"], "status": "unchanged", "item": item}, err
	}
	folder := filepath.Join(l.Store.Root, "items", str(item, "id"))
	snapshotID, err := newID()
	if err != nil {
		return nil, err
	}
	snapshot := filepath.Join(folder, "originals", "feed-update-"+snapshotID+".txt")
	intentPath := filepath.Join(folder, "pending-refresh.json")
	if _, e := os.Lstat(intentPath); e == nil {
		return nil, errors.New("Pending source recovery requires review.")
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	intent := Record{"version": 1, "snapshot": filepath.Base(snapshot), "old_summary": item["summary"], "new_summary": summary}
	if err = l.atomic(intentPath, jsonText(intent)); err != nil {
		return nil, err
	}
	defer func() { _ = l.recoverPendingRefresh(str(item, "id")) }()
	if err = l.atomic(snapshot, text); err != nil {
		return nil, err
	}
	if err = l.atomic(filepath.Join(folder, "revisions", fmt.Sprint(revision)+".md"), str(item, "summary")); err != nil {
		return nil, err
	}
	if err = l.atomic(filepath.Join(folder, "summary.md"), summary); err != nil {
		return nil, err
	}
	err = l.transaction(func() error {
		if e := l.exec("UPDATE items SET title=?,summary=?,source_path=?,source_hash=?,metadata=?,revision=revision+1,updated=? WHERE id=?", title, summary, snapshot, hashText(text), jsonText(metadata), l.now(), item["id"]); e != nil {
			return e
		}
		if e := l.exec("UPDATE search SET title=?,summary=?,source_text=? WHERE item_id=?", title, summary, text, item["id"]); e != nil {
			return e
		}
		if e := l.indexLinks(str(item, "id"), summary); e != nil {
			return e
		}
		if meaningful {
			if e := l.exec("INSERT INTO feed_events(item_id,feed_id,event_key) VALUES(?,?,?)", item["id"], id, fmt.Sprintf("update:%s:%d", item["id"], revision+1)); e != nil {
				return e
			}
			current, e := l.Get(str(item, "id"), false, false)
			if e != nil {
				return e
			}
			if e = l.activityEvent(current, "updated", "", nil, false); e != nil {
				return e
			}
		} else {
			if e := l.rememberActivityState(str(item, "id")); e != nil {
				return e
			}
		}
		return l.contractPoint("feed.before_commit")
	})
	if err != nil {
		if recovery := l.recoverPendingRefresh(str(item, "id")); recovery != nil {
			return nil, errors.Join(err, recovery)
		}
		return nil, err
	}
	if err = l.contractPoint("feed.after_commit"); err != nil {
		return nil, err
	}
	if err = l.recoverPendingRefresh(str(item, "id")); err != nil {
		return nil, err
	}
	current, err := l.Get(str(item, "id"), false, false)
	status := "unchanged"
	if meaningful {
		status = "updated"
	}
	return Record{"id": item["id"], "status": status, "item": current}, err
}
func (l *Library) feedOverview(result Record, editions []Record) error {
	// Listings intentionally omit summaries. The reader projection needs them,
	// but does not need to hydrate full saved-source references for every row.
	rows, err := l.rows("SELECT i.id,i.summary FROM items i JOIN blog_members m ON m.item_id=i.id WHERE m.feed_id=? AND i.deleted_at IS NULL", result["id"])
	if err != nil {
		return err
	}
	summaries := map[string]string{}
	for _, row := range rows {
		summaries[str(row, "id")] = str(row, "summary")
	}

	cutoff, _ := time.Parse(time.RFC3339Nano, l.now())
	cutoff = cutoff.Add(-24 * time.Hour)
	current, archived := []Record{}, []Record{}
	for _, listed := range editions {
		item := Record{}
		for k, v := range listed {
			item[k] = v
		}
		item["summary"] = summaries[str(item, "id")]
		entry := mapping(mapping(item["metadata"])["feed_entry"])
		fields := mapping(entry["fields"])
		stamp, e := time.Parse(time.RFC3339Nano, str(entry, "meaningful_at"))
		if fields["status"] == "resolved" && e == nil && stamp.Before(cutoff) {
			archived = append(archived, item)
		} else {
			current = append(current, item)
		}
	}
	rank := func(item Record) int {
		switch mapping(mapping(mapping(item["metadata"])["feed_entry"])["fields"])["status"] {
		case "active":
			return 0
		case "stable":
			return 1
		case "unknown":
			return 2
		case "resolved":
			return 3
		}
		return 2
	}
	sort.SliceStable(current, func(i, j int) bool {
		a, b := rank(current[i]), rank(current[j])
		if a != b {
			return a < b
		}
		fa, fb := mapping(mapping(mapping(current[i]["metadata"])["feed_entry"])["fields"]), mapping(mapping(mapping(current[j]["metadata"])["feed_entry"])["fields"])
		sa, sb := number(fa["severity"]), number(fb["severity"])
		if sa == 0 {
			sa = 100
		}
		if sb == 0 {
			sb = 100
		}
		return sa < sb
	})
	result["overview_entries"], result["archived_entries"] = current, archived
	return nil
}
