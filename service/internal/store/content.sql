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
