"""Synthetic historical fixture SETUP only; no application behavior or assertions.

These deliberately small schemas model columns/tables actually migrated by
Library.__init__. They are hand-written, not copied from a live database.
All identities/content are synthetic. No SQL is used after service startup.
"""
import hashlib
import json
import sqlite3
from harness import Service

CREATED = "2025-01-02T03:04:05+00:00"
SOURCE = "Café legacy 東京\r\nSecond line\rThird line\n"
SUMMARY = "# Blurb\nLegacy curated summary.\n"
DOCUMENT = "legacy-doc-readable-id"
PERSON = "legacy-person-readable-id"
PROJECT = "legacy-project-readable-id"
TEAM = "legacy-team-readable-id"
DELETED = "legacy-deleted-readable-id"

# Source-state/entity tables and compatibility triggers intentionally absent.
ITEM_SCHEMA = """
CREATE TABLE items (
 id TEXT PRIMARY KEY,title TEXT NOT NULL,kind TEXT NOT NULL,
 date TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL,source_key TEXT UNIQUE,
 source_path TEXT,source_hash TEXT,metadata TEXT NOT NULL DEFAULT '{}',
 revision INTEGER NOT NULL DEFAULT 1,created TEXT NOT NULL,updated TEXT NOT NULL,
 period_start TEXT,period_end TEXT,source_channel TEXT,inbox INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE tags(id TEXT PRIMARY KEY,name TEXT NOT NULL,category TEXT NOT NULL,
 UNIQUE(name,category));
CREATE TABLE item_tags(item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
 tag_id TEXT REFERENCES tags(id) ON DELETE CASCADE,PRIMARY KEY(item_id,tag_id));
CREATE VIRTUAL TABLE search USING fts5(item_id UNINDEXED,title,summary,source_text);
"""

def prepare(root, modern_columns=False, digest_mode="indexed", manual_summary=False):
    """Build an old schema only in the Service-created empty marked directory."""
    root = root.resolve()
    if (root.parent / ".atlas-contract-fixture").read_text() != "atlas-contract-v1":
        raise ValueError("Fixture marker required")
    if any(root.iterdir()):
        raise ValueError("Historical fixture requires an empty owned root")
    db = sqlite3.connect(str(root / "library.sqlite"))
    try:
        db.executescript(ITEM_SCHEMA)
        if modern_columns:
            db.execute("ALTER TABLE items ADD COLUMN deleted_at TEXT")
            db.execute("ALTER TABLE items ADD COLUMN starred INTEGER NOT NULL DEFAULT 0")
        source_path = root / "items" / DOCUMENT / "originals" / "recording.txt"
        source_path.parent.mkdir(parents=True)
        source_path.write_bytes(SOURCE.encode("utf-8"))
        (source_path.parent / "historical.txt").write_bytes(b"Older original\r\n")
        folder = source_path.parent.parent
        (folder / "revisions").mkdir()
        (folder / "revisions" / "3.md").write_bytes(b"# Previous summary\n")
        if manual_summary:
            (folder / "summary.md").write_text("# Blurb\nExternal curated summary.\n")
        meta = {"doc_urls":["https://docs.google.com/document/d/legacy-doc/edit?resourcekey=retained"],
                "date_basis":"manual","legacy_user_context":{"keep":["one",None,7]}}
        if digest_mode == "metadata":
            meta["google_sync"] = {"content_hash":hashlib.sha256(SOURCE.replace("\r\n","\n").replace("\r","\n").encode()).hexdigest()}
        rows = [
            (DOCUMENT,"Legacy Google design","document","2025-01-01",SUMMARY,"google-doc:legacy-doc",
             str(source_path),None if digest_mode == "unverified" else hashlib.sha256(SOURCE.encode()).hexdigest(),json.dumps(meta),7),
            (PERSON,"Dr. Mira Chen","person","2025-01-01","Person overview","legacy:person",None,None,
             json.dumps({"obsidian_stem":"Mira Chen"}),2),
            (PROJECT,"Amber overview","project","2025-01-01","Project overview","legacy:project",None,None,
             json.dumps({"obsidian_stem":"Amber"}),3),
            (TEAM,"Platform","team","2025-01-01","Team overview","legacy:team",None,None,"{}",1),
        ]
        for row in rows:
            values = row + (CREATED,CREATED)
            db.execute("INSERT INTO items(id,title,kind,date,summary,source_key,source_path,source_hash,metadata,revision,created,updated) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",values)
            if digest_mode != "raw" or row[0] != DOCUMENT:
                db.execute("INSERT INTO search VALUES(?,?,?,?)",(row[0],row[1],row[4],SOURCE if row[0]==DOCUMENT else ""))
        for ident,name,category in (("legacy-person-tag","Mira Chen","person"),
                ("legacy-project-tag","Amber","project"),("legacy-team-tag","Platform","team"),
                ("legacy-topic-tag","Amber","topic")):
            db.execute("INSERT INTO tags VALUES(?,?,?)",(ident,name,category))
        for item,tag in ((DOCUMENT,"legacy-person-tag"),(DOCUMENT,"legacy-project-tag"),
                (DOCUMENT,"legacy-topic-tag"),(PERSON,"legacy-person-tag"),
                (PERSON,"legacy-project-tag"),(PERSON,"legacy-team-tag"),
                (PROJECT,"legacy-project-tag"),(TEAM,"legacy-team-tag")):
            db.execute("INSERT INTO item_tags VALUES(?,?)",(item,tag))
        if modern_columns:
            db.execute("UPDATE items SET starred=1 WHERE id=?",(DOCUMENT,))
            db.execute("INSERT INTO items(id,title,kind,date,summary,source_key,metadata,revision,created,updated,deleted_at,starred) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
                (DELETED,"Removed legacy note","note","2025-01-01","Removed summary","legacy:deleted",
                 '{"curated":true}',4,CREATED,CREATED,"2025-02-01T00:00:00+00:00",1))
            db.execute("INSERT INTO search VALUES(?,?,?,?)",(DELETED,"Removed legacy note","Removed summary",""))
        db.commit()
    finally:
        db.close()


class LegacyService(Service):
    """Prepare before the FIRST subprocess; ordinary restarts never reseed."""
    def __init__(self, **options):
        self.fixture_options = options.pop("fixture_options", {})
        self.prepared = False
        super().__init__(**options)

    def start(self):
        if not self.prepared:
            prepare(self.library, **self.fixture_options)
            self.prepared = True
        super().start()
