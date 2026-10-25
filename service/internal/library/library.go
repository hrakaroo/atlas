// Package library implements Atlas application behavior over the compatible store.
// Public dispatch owns the writer lock; internal methods must never reenter it.
package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"atlas.local/atlas/service/internal/store"
)

type Record = map[string]any
type Config struct {
	Now       func() time.Time
	Location  *time.Location
	OwnerName string
	// TestHook is supplied only by a guarded disposable contract process.
	TestHook func(string) error
}
type stamp struct {
	Mtime int64
	Size  int64
}
type Library struct {
	Store         *store.Store
	Now           func() time.Time
	Location      *time.Location
	OwnerName     string
	depth         int
	testHook      func(string) error
	recoveryErr   error
	summaryStamps map[string]stamp
}

func New(state *store.Store, cfg Config) *Library {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	return &Library{Store: state, Now: cfg.Now, Location: cfg.Location, OwnerName: cfg.OwnerName, testHook: cfg.TestHook, summaryStamps: map[string]stamp{}}
}
func (l *Library) rows(query string, args ...any) ([]Record, error) {
	rows, err := l.Store.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for rows.Next() {
		values := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := Record{}
		for i, name := range names {
			value := values[i]
			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}
			row[name] = value
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
func (l *Library) row(query string, args ...any) (Record, error) {
	rows, err := l.rows(query, args...)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}
func (l *Library) exec(query string, args ...any) error {
	_, err := l.Store.DB.Exec(query, args...)
	return err
}

// BEGIN is issued on the store's sole connection so nested domain helpers can use
// the same connection. No sql.Tx may reserve it while querying through DB.
func (l *Library) transaction(fn func() error) (err error) {
	if l.depth > 0 {
		return fn()
	}
	if err = l.exec("BEGIN"); err != nil {
		return err
	}
	l.depth++
	committed := false
	defer func() {
		l.depth--
		if !committed {
			_ = l.exec("ROLLBACK")
		}
	}()
	if err = fn(); err != nil {
		return err
	}
	err = l.exec("COMMIT")
	committed = err == nil
	return err
}
func newID() (string, error) { return store.NewID() }
func s(value any) string {
	if value == nil {
		return ""
	}
	if x, ok := value.(string); ok {
		return x
	}
	return fmt.Sprint(value)
}
func str(values map[string]any, key string) string { return s(values[key]) }
func strDefault(values map[string]any, key, def string) string {
	if _, ok := values[key]; !ok {
		return def
	}
	return str(values, key)
}
func truth(value any) bool {
	switch x := value.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case json.Number:
		beforeExponent := strings.SplitN(strings.ToLower(string(x)), "e", 2)[0]
		return strings.Trim(beforeExponent, "+-.0") != ""
	case []any:
		return len(x) > 0
	case []string:
		return len(x) > 0
	case []Record:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}
func integer(value any) (int64, error) {
	switch x := value.(type) {
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case json.Number:
		return strconv.ParseInt(string(x), 10, 64)
	case string:
		return strconv.ParseInt(x, 10, 64)
	case float64:
		if x == float64(int64(x)) {
			return int64(x), nil
		}
	}
	return 0, errors.New("An integer is required.")
}
func stringsArg(value any) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	if list, ok := value.([]string); ok {
		return list, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("A list of strings is required.")
	}
	result := []string{}
	for _, v := range list {
		x, ok := v.(string)
		if !ok {
			return nil, errors.New("A list of strings is required.")
		}
		result = append(result, x)
	}
	return result, nil
}

var wikiClean = regexp.MustCompile("\\[\\[([^\\]|]+)(?:\\|[^\\]]+)?\\]\\]")

func clean(text string) string {
	return strings.TrimSpace(wikiClean.ReplaceAllString(strings.Trim(strings.TrimSpace(text), "\"'"), "$1"))
}
func jsonText(value any) string {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		panic(err)
	}
	compact := strings.TrimSuffix(output.String(), "\n")
	compact = regexp.MustCompile(`\\+u202[89]`).ReplaceAllStringFunc(compact, func(token string) string {
		slashes := len(token) - 5
		if slashes%2 == 0 {
			return token
		}
		separator := "\u2028"
		if strings.HasSuffix(token, "9") {
			separator = "\u2029"
		}
		return token[:slashes-1] + separator
	})
	var spaced strings.Builder
	quoted, escaped := false, false
	for _, r := range compact {
		spaced.WriteRune(r)
		if quoted {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '"' {
				quoted = false
			}
		} else if r == '"' {
			quoted = true
		} else if r == ',' || r == ':' {
			spaced.WriteByte(' ')
		}
	}
	return spaced.String()
}
func documentHash(text string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")))
	return hex.EncodeToString(sum[:])
}
func (l *Library) now() string {
	t := l.Now().UTC()
	base := t.Format("2006-01-02T15:04:05")
	if micros := t.Nanosecond() / 1000; micros != 0 {
		base += fmt.Sprintf(".%06d", micros)
	}
	return base + "+00:00"
}
func (l *Library) today(timestamp string) string {
	t, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return ""
	}
	return t.In(l.Location).Format("2006-01-02")
}
func parseJSON(value any) (Record, error) {
	result := Record{}
	decoder := json.NewDecoder(strings.NewReader(s(value)))
	decoder.UseNumber()
	err := decoder.Decode(&result)
	return result, err
}
func number(value any) int64 { n, _ := integer(value); return n }
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// safePath checks each existing component, preserving lexical containment and
// rejecting links. Final reads use os.Root as an additional kernel boundary.
func (l *Library) safePath(path string, missing bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(l.Store.Root, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("Source files must be preserved inside the library.")
	}
	current := l.Store.Root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if errors.Is(e, os.ErrNotExist) && missing {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Library paths must not contain symlinks.")
		}
	}
	return nil
}
func (l *Library) atomic(path, text string) (err error) {
	if !utf8.ValidString(text) {
		return errors.New("Text must be valid UTF-8.")
	}
	return l.atomicBytes(path, []byte(text))
}
func (l *Library) atomicBytes(path string, content []byte) (err error) {
	if err = l.safePath(path, true); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err = l.safePath(filepath.Dir(path), false); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atlas-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(content); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (l *Library) readText(path string) (string, error) {
	data, err := l.readBytes(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("Saved text is not UTF-8.")
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), nil
}

func (l *Library) contractPoint(point string) error {
	if l.testHook != nil {
		return l.testHook(point)
	}
	return nil
}
