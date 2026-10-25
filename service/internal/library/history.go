package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type historyEntry struct {
	entry Record
	path  string
}

func walkFiles(root string) ([]string, error) {
	result := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("History paths must not contain symlinks.")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			result = append(result, path)
		}
		return nil
	})
	sort.Strings(result)
	return result, err
}
func (l *Library) historySafePath(path string, missing bool) error {
	for _, part := range strings.Split(path, string(os.PathSeparator)) {
		if part == ".." {
			return errors.New("History path is outside the library.")
		}
	}
	rel, err := filepath.Rel(l.Store.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("History path is outside the library.")
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == ".." {
			return errors.New("History path is outside the library.")
		}
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
			return errors.New("History entry is unavailable.")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("History paths must not contain symlinks.")
		}
	}
	if !missing {
		info, e := os.Stat(path)
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("History entry is unavailable.")
		}
	}
	return nil
}
func (l *Library) readBytes(path string) ([]byte, error) {
	if err := l.historySafePath(path, false); err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(l.Store.Root, path)
	parts := strings.Split(rel, string(os.PathSeparator))
	directory, err := unix.Open(l.Store.Root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(directory) }()
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(directory, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if e != nil {
			return nil, e
		}
		_ = unix.Close(directory)
		directory = next
	}
	descriptor, err := unix.Openat(directory, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("History entry is unavailable.")
	}
	return io.ReadAll(file)
}
func (l *Library) historyPaths(id string) ([]historyEntry, error) {
	row, err := l.row("SELECT * FROM items WHERE id=? AND deleted_at IS NULL", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("Item not found. It may be in Trash.")
	}
	folder := filepath.Join(l.Store.Root, "items", id)
	candidates := []historyEntry{{Record{"kind": "summary", "revision": row["revision"], "current": true}, filepath.Join(folder, "summary.md")}}
	for _, pair := range [][2]string{{"revisions", "summary"}, {"originals", "original"}} {
		base := filepath.Join(folder, pair[0])
		if err = l.historySafePath(base, true); err != nil {
			return nil, err
		}
		if _, e := os.Stat(base); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return nil, e
		}
		files, e := walkFiles(base)
		if e != nil {
			return nil, e
		}
		for _, path := range files {
			var revision any
			if pair[1] == "summary" && filepath.Ext(path) == ".md" {
				stem := strings.TrimSuffix(filepath.Base(path), ".md")
				if digitsPattern.MatchString(stem) {
					if parsed, e := strconv.ParseInt(stem, 10, 64); e == nil {
						revision = parsed
					}
				}
			}
			candidates = append(candidates, historyEntry{Record{"kind": pair[1], "revision": revision, "current": false}, path})
		}
	}
	if truth(row["source_path"]) {
		candidates = append(candidates, historyEntry{Record{"kind": "original", "revision": nil, "current": true}, str(row, "source_path")})
	}
	result := []historyEntry{}
	index := map[string]int{}
	for _, candidate := range candidates {
		if err = l.historySafePath(candidate.path, false); err != nil {
			return nil, err
		}
		relative, _ := filepath.Rel(l.Store.Root, candidate.path)
		digest := sha256.Sum256([]byte(str(candidate.entry, "kind") + "\x00" + filepath.ToSlash(relative)))
		identity := hex.EncodeToString(digest[:])
		if i, exists := index[identity]; exists {
			result[i].entry["current"] = truth(result[i].entry["current"]) || truth(candidate.entry["current"])
			continue
		}
		candidate.entry["id"] = identity
		candidate.entry["name"] = filepath.Base(candidate.path)
		index[identity] = len(result)
		result = append(result, candidate)
	}
	return result, nil
}

var digitsPattern = regexp.MustCompile(`^[0-9]+$`)
var historyIdentityPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (l *Library) History(id string) (Record, error) {
	paths, err := l.historyPaths(id)
	if err != nil {
		return nil, err
	}
	entries := []Record{}
	for _, path := range paths {
		entries = append(entries, path.entry)
	}
	return Record{"item_id": id, "entries": entries}, nil
}
func (l *Library) HistoryContent(id, entryID string) ([]byte, error) {
	if !historyIdentityPattern.MatchString(entryID) {
		return nil, errors.New("History entry not found.")
	}
	entries, err := l.historyPaths(id)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if str(entry.entry, "id") == entryID {
			return l.readBytes(entry.path)
		}
	}
	return nil, errors.New("History entry not found.")
}
