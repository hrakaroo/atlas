// Package store owns the compatible SQLite/file boundary. Callers must first
// acquire .service/server.lock for the complete service lifetime.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

//go:embed content.sql
var contentSQL string

//go:embed relationships.sql
var relationshipsSQL string

// Store retains one serial database connection and the Python-compatible writer
// lock. Domain orchestration takes Lock once; nested Lock calls are unsupported.
type Store struct {
	Root     string
	DB       *sql.DB
	mu       sync.Mutex
	lockFile *os.File
	closed   bool
}

// NewID returns a canonical UUIDv4 without an additional runtime dependency.
func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	h := hex.EncodeToString(value[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// Open initializes only the requested library. The service lifetime lock must
// already be held; this function never starts a service or chooses a default root.
func Open(root string) (_ *Store, err error) {
	if root == "" {
		return nil, errors.New("A library root is required.")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(absolute, 0755); err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	if err = directory(filepath.Join(absolute, "items")); err != nil {
		return nil, err
	}
	if info, e := os.Lstat(filepath.Join(absolute, "library.sqlite")); e == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return nil, errors.New("Invalid library database file.")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	fd, err := syscall.Open(filepath.Join(absolute, ".writer.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	s := &Store{Root: absolute, lockFile: os.NewFile(uintptr(fd), ".writer.lock")}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	if err = s.Lock(); err != nil {
		return nil, err
	}
	defer func() {
		if e := s.Unlock(); err == nil && e != nil {
			err = e
		}
	}()
	query := url.Values{}
	// Driver applies DSN PRAGMAs whenever database/sql replaces a connection.
	query.Add("_pragma", "foreign_keys=1")
	query.Add("_pragma", "journal_mode=WAL")
	query.Add("_pragma", "busy_timeout=5000")
	dsn := (&url.URL{Scheme: "file", Path: filepath.Join(absolute, "library.sqlite"), RawQuery: query.Encode()}).String()
	s.DB, err = sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.DB.SetMaxOpenConns(1)
	s.DB.SetMaxIdleConns(1)
	s.DB.SetConnMaxLifetime(0)
	s.DB.SetConnMaxIdleTime(0)
	if err = s.DB.Ping(); err != nil {
		return nil, err
	}
	if err = s.initialize(); err != nil {
		return nil, err
	}
	return s, nil
}

// Lock serializes an outer operation and coordinates with compatibility writers.
func (s *Store) Lock() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("Store is closed.")
	}
	if err := syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_EX); err != nil {
		s.mu.Unlock()
		return err
	}
	return nil
}
func (s *Store) Unlock() error {
	err := syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
	s.mu.Unlock()
	return err
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var errs []error
	if s.DB != nil {
		errs = append(errs, s.DB.Close())
	}
	if s.lockFile != nil {
		errs = append(errs, s.lockFile.Close())
	}
	return errors.Join(errs...)
}

func (s *Store) initialize() error {
	if _, err := s.DB.Exec(schemaSQL); err != nil {
		return err
	}
	if _, err := s.DB.Exec(contentSQL); err != nil {
		return err
	}
	rows, err := s.DB.Query("PRAGMA table_info(items)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !columns["deleted_at"] {
		if _, err = s.DB.Exec("ALTER TABLE items ADD COLUMN deleted_at TEXT"); err != nil {
			return err
		}
	}
	if !columns["starred"] {
		if _, err = s.DB.Exec("ALTER TABLE items ADD COLUMN starred INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	// Fetch and close rows before issuing another query on our one connection.
	type sourceState struct {
		id, metadata string
		hash         sql.NullString
	}
	var missing []sourceState
	rows, err = s.DB.Query("SELECT id,metadata,source_hash FROM items WHERE kind IN ('document','reference') AND id NOT IN (SELECT item_id FROM document_source_state)")
	if err != nil {
		return err
	}
	for rows.Next() {
		var row sourceState
		if err = rows.Scan(&row.id, &row.metadata, &row.hash); err != nil {
			rows.Close()
			return err
		}
		missing = append(missing, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range missing {
		var metadata map[string]any
		if err = json.Unmarshal([]byte(row.metadata), &metadata); err != nil {
			return err
		}
		if metadata == nil {
			return errors.New("Invalid item metadata.")
		}
		var digest any
		if sync, exists := metadata["google_sync"]; exists {
			syncMap, ok := sync.(map[string]any)
			if !ok {
				return errors.New("Invalid Google synchronization metadata.")
			}
			digest = syncMap["content_hash"]
		}
		if !truthy(digest) && row.hash.Valid && row.hash.String != "" {
			var source sql.NullString
			err = tx.QueryRow("SELECT source_text FROM search WHERE item_id=?", row.id).Scan(&source)
			if errors.Is(err, sql.ErrNoRows) {
				digest = row.hash.String
			} else if err != nil {
				return err
			} else {
				if !source.Valid {
					return errors.New("Invalid indexed source text.")
				}
				digest = documentHash(source.String)
			}
		}
		if _, err = tx.Exec("INSERT INTO document_source_state VALUES(?,?)", row.id, digest); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = s.DB.Exec(relationshipsSQL); err != nil {
		return err
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	if _, err = s.DB.Exec("INSERT OR IGNORE INTO blog_feeds(id,name,normalized_name) VALUES(?,?,?)", id, "AI", "ai"); err != nil {
		return err
	}
	type summary struct{ id, text, kind string }
	var summaries []summary
	rows, err = s.DB.Query("SELECT id,summary,kind FROM items")
	if err != nil {
		return err
	}
	for rows.Next() {
		var row summary
		if err = rows.Scan(&row.id, &row.text, &row.kind); err != nil {
			rows.Close()
			return err
		}
		summaries = append(summaries, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, row := range summaries {
		if row.kind == "person" || row.kind == "team" || row.kind == "project" {
			continue
		}
		if row.id == "" || row.id == "." || row.id == ".." || strings.ContainsAny(row.id, "/\\") {
			return errors.New("Invalid item identity path.")
		}
		folder := filepath.Join(s.Root, "items", row.id)
		if err = directory(folder); err != nil {
			return err
		}
		path := filepath.Join(folder, "summary.md")
		if info, e := os.Lstat(path); e == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return errors.New("Invalid summary file.")
			}
			continue // Existing Markdown remains authoritative; never overwrite on open.
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if err = atomic(path, []byte(row.text)); err != nil {
			return err
		}
	}
	return nil
}
func truthy(value any) bool {
	switch x := value.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case []any:
		return len(x) != 0
	case map[string]any:
		return len(x) != 0
	default:
		return true
	}
}
func documentHash(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func directory(path string) error {
	if err := os.Mkdir(path, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Invalid library directory: %s", filepath.Base(path))
	}
	return nil
}
func atomic(path string, data []byte) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".atlas-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	defer func() { _ = temp.Close() }()
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
