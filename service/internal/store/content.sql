-- Database-owned entity descriptions use items.summary. Immutable originals,
-- prior descriptions and attachments retain their bytes independently of files.
CREATE TABLE IF NOT EXISTS entity_storage (
    item_id TEXT PRIMARY KEY REFERENCES items(id));
CREATE TABLE IF NOT EXISTS entity_snapshots (
    item_id TEXT NOT NULL REFERENCES items(id), entry_id TEXT NOT NULL,
    entry TEXT NOT NULL, content BLOB NOT NULL,
    PRIMARY KEY(item_id,entry_id));
CREATE TABLE IF NOT EXISTS entity_assets (
    item_id TEXT NOT NULL REFERENCES items(id), path TEXT NOT NULL,
    content BLOB NOT NULL, PRIMARY KEY(item_id,path));
CREATE TABLE IF NOT EXISTS source_history_aliases (
    item_id TEXT NOT NULL REFERENCES items(id), path TEXT NOT NULL,
    entry_id TEXT NOT NULL, PRIMARY KEY(item_id,path));
CREATE TABLE IF NOT EXISTS entity_trash_journals (
    item_id TEXT PRIMARY KEY REFERENCES items(id), intent TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS source_path_aliases (
    item_id TEXT NOT NULL REFERENCES items(id), path TEXT NOT NULL,
    PRIMARY KEY(item_id,path));

CREATE TABLE IF NOT EXISTS entity_description_bytes (
    item_id TEXT PRIMARY KEY REFERENCES items(id), summary TEXT NOT NULL,
    content BLOB NOT NULL);

-- Successful content checks and confirmed changes drive adaptive polling.
CREATE TABLE IF NOT EXISTS document_monitor_state (
    item_id TEXT PRIMARY KEY REFERENCES items(id), activity_at TEXT,
    checked_at TEXT NOT NULL);

-- Feed attention is separate from edition dates and document review state.
CREATE TABLE IF NOT EXISTS feed_arrivals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id TEXT NOT NULL UNIQUE REFERENCES items(id),
    feed_id TEXT NOT NULL REFERENCES blog_feeds(id));
CREATE INDEX IF NOT EXISTS feed_arrivals_feed ON feed_arrivals(feed_id,id);
CREATE TABLE IF NOT EXISTS feed_seen (
    feed_id TEXT PRIMARY KEY REFERENCES blog_feeds(id), through_id INTEGER NOT NULL);

-- Feed modes are independent of collector cadence and presentation.
CREATE TABLE IF NOT EXISTS feed_settings (
    feed_id TEXT PRIMARY KEY REFERENCES blog_feeds(id),
    flow TEXT NOT NULL CHECK(flow IN ('append','in_place')),
    presentation TEXT NOT NULL CHECK(presentation IN ('entries','overview')),
    entry_label TEXT NOT NULL DEFAULT 'Entries', checked_at TEXT);
CREATE TABLE IF NOT EXISTS feed_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT, item_id TEXT NOT NULL REFERENCES items(id),
    feed_id TEXT NOT NULL REFERENCES blog_feeds(id), event_key TEXT NOT NULL UNIQUE);
CREATE INDEX IF NOT EXISTS feed_events_feed ON feed_events(feed_id,id);
CREATE TABLE IF NOT EXISTS feed_event_migration (id INTEGER PRIMARY KEY CHECK(id=1));
-- Preserve existing attention cursors exactly once; later events use their own sequence.
INSERT OR IGNORE INTO feed_events(id,item_id,feed_id,event_key)
    SELECT id,item_id,feed_id,'arrival:'||item_id FROM feed_arrivals
    WHERE NOT EXISTS(SELECT 1 FROM feed_event_migration);
INSERT OR IGNORE INTO feed_event_migration VALUES(1);
