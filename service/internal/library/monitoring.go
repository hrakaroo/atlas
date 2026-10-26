package library

import (
	"time"
)

// Monitoring ages from source activity, never from the most recent check.
// Existing sources bootstrap from observed Google timestamps without inventing updates.
func (l *Library) documentMonitoring(item Record) (Record, error) {
	state, err := l.row("SELECT * FROM document_monitor_state WHERE item_id=?", item["id"])
	if err != nil {
		return nil, err
	}
	sync := mapping(mapping(item["metadata"])["google_sync"])
	activity := str(state, "activity_at")
	if activity == "" {
		activity = str(sync, "modified_time")
	}
	if activity == "" {
		activity = str(sync, "created_time")
	}
	if activity == "" {
		activity = str(item, "created")
	}
	when, err := parseGoogleTime(activity)
	if err != nil {
		return nil, err
	}
	age := l.Now().UTC().Sub(when)
	interval := 24 * time.Hour
	cadence := "daily"
	switch {
	case age > 180*24*time.Hour:
		interval = 0
		cadence = "manual"
	case age > 60*24*time.Hour:
		interval = 30 * 24 * time.Hour
		cadence = "monthly"
	case age > 14*24*time.Hour:
		interval = 7 * 24 * time.Hour
		cadence = "weekly"
	case age > 3*24*time.Hour:
		interval = 2 * 24 * time.Hour
		cadence = "every_other_day"
	}
	checked := str(state, "checked_at")
	if checked == "" {
		checked = str(sync, "last_checked")
	}
	if checked == "" && truth(sync["content_hash"]) {
		checked = str(item, "created")
	}
	var next any
	due := interval > 0
	if interval > 0 && checked != "" {
		last, e := parseGoogleTime(checked)
		if e != nil {
			return nil, e
		}
		next = last.Add(interval).UTC().Format(time.RFC3339Nano)
		due = !l.Now().UTC().Before(last.Add(interval))
	}
	return Record{"cadence": cadence, "activity_at": activity, "checked_at": checked, "next_check_at": next, "manual_only": interval == 0, "due": due}, nil
}
func (l *Library) recordDocumentCheck(item, source Record, changed bool) error {
	state, err := l.row("SELECT activity_at FROM document_monitor_state WHERE item_id=?", item["id"])
	if err != nil {
		return err
	}
	sync := mapping(mapping(item["metadata"])["google_sync"])
	activity := str(state, "activity_at")
	if activity == "" {
		activity = str(sync, "modified_time")
	}
	if activity == "" {
		activity = str(source, "modified_time")
	}
	if activity == "" {
		activity = str(item, "created")
	}
	if changed {
		activity = l.now()
	}
	return l.exec("INSERT INTO document_monitor_state(item_id,activity_at,checked_at) VALUES(?,?,?) ON CONFLICT(item_id) DO UPDATE SET activity_at=excluded.activity_at,checked_at=excluded.checked_at", item["id"], activity, l.now())
}
