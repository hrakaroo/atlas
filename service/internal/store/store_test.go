package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	must(t, err)
}
func intQuery(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	must(t, db.QueryRow(query, args...).Scan(&n))
	return n
}
func textQuery(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var text string
	must(t, db.QueryRow(query, args...).Scan(&text))
	return text
}
func projectRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
}
func insertItem(t *testing.T, db *sql.DB, id, kind, metadata string) {
	t.Helper()
	execSQL(t, db, "INSERT INTO items(id,title,kind,summary,metadata,created,updated) VALUES(?,?,?,?,?,?,?)", id, "Title "+id, kind, "Summary "+id, metadata, "2026-10-01T00:00:00+00:00", "2026-10-01T00:00:00+00:00")
}
func pragmas(t *testing.T, s *Store) {
	t.Helper()
	if got := intQuery(t, s.DB, "PRAGMA foreign_keys"); got != 1 {
		t.Fatalf("foreign_keys=%d", got)
	}
	if got := textQuery(t, s.DB, "PRAGMA journal_mode"); got != "wal" {
		t.Fatalf("journal_mode=%s", got)
	}
	if got := intQuery(t, s.DB, "PRAGMA busy_timeout"); got != 5000 {
		t.Fatalf("busy_timeout=%d", got)
	}
}
func TestFreshSchemaReopenAndConnectionReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Unicode café # root")
	s, err := Open(root)
	must(t, err)
	pragmas(t, s)
	if s.DB.Stats().MaxOpenConnections != 1 {
		t.Fatal("Multiple connections permitted")
	}
	if got := intQuery(t, s.DB, "SELECT count(*) FROM blog_feeds WHERE name='AI' AND normalized_name='ai'"); got != 1 {
		t.Fatalf("AI feeds=%d", got)
	}
	ai := textQuery(t, s.DB, "SELECT id FROM blog_feeds WHERE normalized_name='ai'")
	execSQL(t, s.DB, "INSERT INTO search VALUES('probe','Café résumé','Alpha beta','東京')")
	if got := intQuery(t, s.DB, "SELECT count(*) FROM search WHERE search MATCH 'cafe resume'"); got != 1 {
		t.Fatal("FTS5 unavailable")
	}
	if got := intQuery(t, s.DB, `SELECT json_extract('{"a":2}', '$.a')`); got != 2 {
		t.Fatal("JSON unavailable")
	}
	conn, err := s.DB.Conn(context.Background())
	must(t, err)
	if err = conn.Raw(func(any) error { return driver.ErrBadConn }); err != driver.ErrBadConn {
		t.Fatalf("discard connection: %v", err)
	}
	_ = conn.Close()
	pragmas(t, s)
	if _, err = s.DB.Exec("INSERT INTO item_tags VALUES('missing','missing')"); err == nil {
		t.Fatal("replacement lost FK enforcement")
	}
	must(t, s.Close())
	must(t, s.Close())
	s, err = Open(root)
	must(t, err)
	defer s.Close()
	pragmas(t, s)
	if got := textQuery(t, s.DB, "SELECT id FROM blog_feeds WHERE normalized_name='ai'"); got != ai {
		t.Fatal("Default feed recreated")
	}
	if got := textQuery(t, s.DB, "PRAGMA integrity_check"); got != "ok" {
		t.Fatal(got)
	}
	must(t, s.Lock())
	must(t, s.Unlock())
}
func TestLegacyColumnsBackfillTriggersAndMissingSummary(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "library.sqlite"))
	must(t, err)
	execSQL(t, db, schemaSQL)
	insertItem(t, db, "doc", "document", "{}")
	execSQL(t, db, "UPDATE items SET source_hash='raw-source-hash' WHERE id='doc'")
	execSQL(t, db, "INSERT INTO search VALUES('doc','Title doc','Summary doc',?)", "Café\r\n東京\rthird\n")
	insertItem(t, db, "explicit", "reference", `{"google_sync":{"content_hash":"known-digest"}}`)
	insertItem(t, db, "fallback", "reference", "{}")
	insertItem(t, db, "null-hash", "reference", "{}")
	execSQL(t, db, "UPDATE items SET source_hash='fallback-digest' WHERE id='fallback'")
	execSQL(t, db, "INSERT INTO tags VALUES('team-id','Ingest','team')")
	execSQL(t, db, "INSERT INTO item_tags VALUES('doc','team-id')")
	must(t, db.Close())
	s, err := Open(root)
	must(t, err)
	if got := intQuery(t, s.DB, "SELECT count(*) FROM pragma_table_info('items') WHERE name IN ('deleted_at','starred')"); got != 2 {
		t.Fatal("Legacy columns absent")
	}
	if got := textQuery(t, s.DB, "SELECT content_hash FROM document_source_state WHERE item_id='doc'"); got != documentHash("Café\n東京\nthird\n") {
		t.Fatal("Backfill normalization mismatch")
	}
	if got := textQuery(t, s.DB, "SELECT content_hash FROM document_source_state WHERE item_id='explicit'"); got != "known-digest" {
		t.Fatal(got)
	}
	if got := textQuery(t, s.DB, "SELECT content_hash FROM document_source_state WHERE item_id='fallback'"); got != "fallback-digest" {
		t.Fatal(got)
	}
	if got := textQuery(t, s.DB, "SELECT relation FROM entity_relationships WHERE item_id='doc' AND entity_id='team-id'"); got != "related_to" {
		t.Fatal(got)
	}
	body, err := os.ReadFile(filepath.Join(root, "items", "doc", "summary.md"))
	must(t, err)
	if string(body) != "Summary doc" {
		t.Fatal("Missing summary not repaired")
	}
	execSQL(t, s.DB, "INSERT INTO tags VALUES('project-id','Project','project')")
	execSQL(t, s.DB, "INSERT INTO item_tags VALUES('doc','project-id')")
	if got := textQuery(t, s.DB, "SELECT relation FROM entity_relationships WHERE item_id='doc' AND entity_id='project-id'"); got != "belongs_to" {
		t.Fatal("Legacy link trigger")
	}
	execSQL(t, s.DB, "DELETE FROM item_tags WHERE item_id='doc' AND tag_id='project-id'")
	if got := intQuery(t, s.DB, "SELECT count(*) FROM entity_relationships WHERE item_id='doc' AND entity_id='project-id'"); got != 0 {
		t.Fatal("Legacy unlink trigger")
	}
	must(t, s.Close())
	path := filepath.Join(root, "items", "doc", "summary.md")
	must(t, os.WriteFile(path, []byte("Existing user Markdown"), 0600))
	s, err = Open(root)
	must(t, err)
	defer s.Close()
	body, err = os.ReadFile(path)
	must(t, err)
	if string(body) != "Existing user Markdown" {
		t.Fatal("Existing summary overwritten")
	}
	if got := intQuery(t, s.DB, "SELECT count(*) FROM document_source_state WHERE item_id='null-hash' AND content_hash IS NULL"); got != 1 {
		t.Fatal("NULL source hash changed")
	}
	if got := intQuery(t, s.DB, "SELECT count(*) FROM document_source_state"); got != 4 {
		t.Fatal("Idempotent backfill duplicated")
	}
}
func TestWriterLockAndClosedState(t *testing.T) {
	s, err := Open(t.TempDir())
	must(t, err)
	must(t, s.Lock())
	lockPath := filepath.Join(s.Root, ".writer.lock")
	command := exec.Command("/usr/bin/python3", "-c", `import fcntl,sys
with open(sys.argv[1],"a") as f:
 try: fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
 except BlockingIOError: sys.exit(7)
`, lockPath)
	err = command.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 7 {
		t.Fatalf("Writer lock not shared with independent process: %v", err)
	}
	must(t, s.Unlock())
	fd, err := syscall.Open(lockPath, syscall.O_RDWR, 0600)
	must(t, err)
	must(t, syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB))
	must(t, syscall.Flock(fd, syscall.LOCK_UN))
	must(t, syscall.Close(fd))
	must(t, s.Close())
	if err = s.Lock(); err == nil {
		t.Fatal("Closed store acquired lock")
	}
}
func TestRejectUnsafeLayoutAndBadBackfill(t *testing.T) {
	for _, target := range []string{"items", "library.sqlite", ".writer.lock"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			path := outside
			if target != "items" {
				path = filepath.Join(outside, "file")
				must(t, os.WriteFile(path, []byte{}, 0600))
			}
			must(t, os.Symlink(path, filepath.Join(root, target)))
			if s, err := Open(root); err == nil {
				s.Close()
				t.Fatal("Symlink accepted")
			}
		})
	}
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "library.sqlite"))
	must(t, err)
	execSQL(t, db, schemaSQL)
	insertItem(t, db, "bad", "document", "not-json")
	must(t, db.Close())
	for i := 0; i < 2; i++ {
		if s, err := Open(root); err == nil {
			s.Close()
			t.Fatal("Bad metadata accepted")
		}
	}
	fd, err := syscall.Open(filepath.Join(root, ".writer.lock"), syscall.O_RDWR, 0600)
	must(t, err)
	must(t, syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB))
	must(t, syscall.Close(fd))
}
func TestNewIDAndCopiedSQLSource(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		id, err := NewID()
		must(t, err)
		if !pattern.MatchString(id) || seen[id] {
			t.Fatal("Invalid or repeated UUID")
		}
		seen[id] = true
	}
	out, err := os.ReadFile(filepath.Join("testdata", "schema-reference.json"))
	must(t, err)
	var reference struct {
		Scripts []string `json:"scripts"`
	}
	must(t, json.Unmarshal(out, &reference))
	expected := reference.Scripts
	if len(expected) != 2 {
		t.Fatal("Expected both frozen schema scripts")
	}
	for i, actual := range []string{schemaSQL, relationshipsSQL} {
		actual = strings.SplitN(actual, "\n", 2)[1]
		if strings.TrimSpace(actual) != expected[i] {
			t.Fatal("Embedded schema differs from frozen reference; review compatibility explicitly")
		}
	}
}

func TestSummaryRepairRejectsUnsafeItemPaths(t *testing.T) {
	for _, identity := range []string{"../escape", "a/b", "a\\b", ".", ".."} {
		t.Run(identity, func(t *testing.T) {
			root := t.TempDir()
			db, err := sql.Open("sqlite", filepath.Join(root, "library.sqlite"))
			must(t, err)
			execSQL(t, db, schemaSQL)
			insertItem(t, db, identity, "note", "{}")
			must(t, db.Close())
			if s, err := Open(root); err == nil {
				s.Close()
				t.Fatal("Unsafe item path accepted")
			}
		})
	}
	t.Run("symlink-directory", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		must(t, os.Mkdir(filepath.Join(root, "items"), 0755))
		must(t, os.Symlink(outside, filepath.Join(root, "items", "legacy")))
		db, err := sql.Open("sqlite", filepath.Join(root, "library.sqlite"))
		must(t, err)
		execSQL(t, db, schemaSQL)
		insertItem(t, db, "legacy", "note", "{}")
		must(t, db.Close())
		if s, err := Open(root); err == nil {
			s.Close()
			t.Fatal("Symlink item directory accepted")
		}
		if _, err = os.Stat(filepath.Join(outside, "summary.md")); !os.IsNotExist(err) {
			t.Fatal("Repair escaped root")
		}
	})
}
