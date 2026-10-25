-- Copied from app/library.py Library.__init__; preserve compatibility.
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL,
    date TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL,
    source_key TEXT UNIQUE, source_path TEXT, source_hash TEXT,
    metadata TEXT NOT NULL DEFAULT '{}', revision INTEGER NOT NULL DEFAULT 1,
    created TEXT NOT NULL, updated TEXT NOT NULL,
    period_start TEXT, period_end TEXT, source_channel TEXT,
    inbox INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS meeting_people (
    item_id TEXT NOT NULL REFERENCES items(id), person_id TEXT NOT NULL REFERENCES items(id),
    name TEXT NOT NULL,email TEXT NOT NULL DEFAULT '', evidence TEXT NOT NULL,
    PRIMARY KEY(item_id,person_id));
CREATE TABLE IF NOT EXISTS ownership_lookups (
    item_id TEXT PRIMARY KEY REFERENCES items(id),
    attempts INTEGER NOT NULL, last_attempt TEXT NOT NULL,
    next_attempt TEXT NOT NULL, outcome TEXT NOT NULL, detail TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ownership_provider (
    id INTEGER PRIMARY KEY CHECK(id=1), attempts INTEGER NOT NULL,
    next_attempt TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS document_people (
    item_id TEXT NOT NULL REFERENCES items(id), person_id TEXT NOT NULL REFERENCES items(id),
    role TEXT NOT NULL CHECK(role IN ('owner','creator','mentioned')), evidence TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '', PRIMARY KEY(item_id,person_id,role));
CREATE TABLE IF NOT EXISTS person_emails (
    email TEXT PRIMARY KEY, person_id TEXT NOT NULL REFERENCES items(id));
CREATE TABLE IF NOT EXISTS item_authors (
    item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
    name TEXT NOT NULL, normalized_name TEXT NOT NULL,
    PRIMARY KEY(item_id, normalized_name));
CREATE TABLE IF NOT EXISTS tags (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, category TEXT NOT NULL,
    UNIQUE(name, category));
CREATE TABLE IF NOT EXISTS item_tags (
    item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
    tag_id TEXT REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY(item_id, tag_id));
CREATE TABLE IF NOT EXISTS links (
    item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
    target TEXT NOT NULL, label TEXT NOT NULL,
    PRIMARY KEY(item_id, target, label));
CREATE TABLE IF NOT EXISTS blog_feeds (
    id TEXT PRIMARY KEY, name TEXT NOT NULL, normalized_name TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS blog_members (
    item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    feed_id TEXT NOT NULL REFERENCES blog_feeds(id));
CREATE INDEX IF NOT EXISTS blog_member_index ON blog_members(feed_id);
CREATE TABLE IF NOT EXISTS meeting_series (
    id TEXT PRIMARY KEY, title TEXT NOT NULL, cadence TEXT NOT NULL DEFAULT '',
    normalized_title TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS series_members (
    item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    series_id TEXT NOT NULL REFERENCES meeting_series(id));
CREATE INDEX IF NOT EXISTS series_member_index ON series_members(series_id);
CREATE TABLE IF NOT EXISTS one_on_one_series (
    person_id TEXT PRIMARY KEY REFERENCES items(id),
    series_id TEXT NOT NULL UNIQUE REFERENCES meeting_series(id));

CREATE TABLE IF NOT EXISTS trash_entries (
    item_id TEXT PRIMARY KEY REFERENCES items(id), payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS activity_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT, event_key TEXT UNIQUE,
    item_id TEXT NOT NULL, title TEXT NOT NULL, kind TEXT NOT NULL,
    category TEXT NOT NULL, action TEXT NOT NULL, created TEXT NOT NULL,
    historical INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS activity_item_index ON activity_events(item_id);
CREATE TABLE IF NOT EXISTS source_tombstones (
    identity TEXT PRIMARY KEY, item_id TEXT NOT NULL, deleted_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS source_intake (
    id TEXT PRIMARY KEY, source_key TEXT UNIQUE NOT NULL, source_type TEXT NOT NULL,
    payload TEXT NOT NULL, status TEXT NOT NULL, item_id TEXT, updated TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS document_source_state (
    item_id TEXT PRIMARY KEY REFERENCES items(id), content_hash TEXT);
CREATE TABLE IF NOT EXISTS document_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT, item_id TEXT NOT NULL REFERENCES items(id),
    content_hash TEXT NOT NULL, detected_at TEXT NOT NULL, source_modified_at TEXT);
CREATE INDEX IF NOT EXISTS document_changes_item ON document_changes(item_id,id);
CREATE TABLE IF NOT EXISTS document_changes_seen (
    item_id TEXT PRIMARY KEY REFERENCES items(id), through_id INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS activity_state (
    item_id TEXT PRIMARY KEY, content_hash TEXT NOT NULL, deleted_at TEXT);
CREATE TABLE IF NOT EXISTS history (
    id INTEGER PRIMARY KEY, item_id TEXT, action TEXT, payload TEXT, created TEXT);
CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(item_id UNINDEXED, title, summary, source_text);
