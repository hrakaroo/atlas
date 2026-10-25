package library

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"
)

func (l *Library) TrashList(query, scope string) ([]Record, error) {
	if scope != "title" && scope != "full_text" {
		return nil, errors.New("Search titles or full text.")
	}
	groups := []searchGroup{}
	for _, match := range searchGroups.FindAllStringSubmatch(query, -1) {
		value := match[2]
		phrase := match[1] != ""
		if phrase {
			value = match[1]
		}
		words := searchWords.FindAllString(value, -1)
		groups = append(groups, searchGroup{Words: words, Phrase: phrase})
	}
	rows, err := l.rows("SELECT id FROM items WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC")
	if err != nil {
		return nil, err
	}
	result := []Record{}
	for _, row := range rows {
		item, e := l.Get(str(row, "id"), true, false)
		if e != nil {
			return nil, e
		}
		text := str(item, "title")
		if scope == "full_text" {
			source, e := l.itemSourceText(item)
			if e != nil {
				return nil, e
			}
			text += " " + str(item, "summary") + " " + source
		}
		words := searchWords.FindAllString(caseFold(text), -1)
		normalized := " " + strings.Join(words, " ") + " "
		matches := true
		for _, group := range groups {
			if len(group.Words) == 0 {
				continue
			}
			found := false
			if group.Phrase {
				found = strings.Contains(normalized, " "+caseFold(strings.Join(group.Words, " "))+" ")
			} else {
				for _, word := range words {
					if strings.HasPrefix(word, caseFold(group.Words[0])) {
						found = true
						break
					}
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		item["preview"] = truncateRunes(strings.ReplaceAll(trashPreview.ReplaceAllString(str(item, "summary"), ""), "\n", " "), 220)
		item["tags"] = []Record{}
		item["series"] = nil
		item["links"] = []Record{}
		item["source_links"] = []Record{}
		delete(item, "summary")
		result = append(result, item)
	}
	return result, nil
}
func snapshotSuffix() (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(id, "-", "")[:6], nil
}
func copyFile(source, target string, preserve bool) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Only regular source files can be copied.")
	}
	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if preserve {
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	}
	return nil
}
func (l *Library) Backup() (Record, error) {
	if l.depth != 0 {
		return nil, errors.New("A backup cannot start inside a write transaction.")
	}
	suffix, err := snapshotSuffix()
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(l.Store.Root, "backups", l.Now().In(l.Location).Format("20060102-150405")+"-"+suffix)
	if err = l.safePath(dest, true); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dest, 0755); err != nil {
		return nil, err
	}
	if err = l.exec("VACUUM INTO ?", filepath.Join(dest, "library.sqlite")); err != nil {
		return nil, err
	}
	for _, name := range []string{"items", "imports"} {
		source := filepath.Join(l.Store.Root, name)
		info, e := os.Lstat(source)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("Invalid backup source directory.")
		}
		paths, e := walkFiles(source)
		if e != nil {
			return nil, e
		}
		if e = os.MkdirAll(filepath.Join(dest, name), 0755); e != nil {
			return nil, e
		}
		for _, path := range paths {
			if e = l.safePath(path, false); e != nil {
				return nil, e
			}
			relative, _ := filepath.Rel(source, path)
			if e = copyFile(path, filepath.Join(dest, name, relative), true); e != nil {
				return nil, e
			}
		}
	}
	return Record{"path": dest}, nil
}

var frontmatterField = regexp.MustCompile(`^([\pL\pN_-]+):\s*(.*)$`)
var frontmatterList = regexp.MustCompile(`^\s+- `)
var noteHeading = regexp.MustCompile(`(?m)^# (.+)$`)

func parseNote(text, fallback string) (Record, string, string) {
	meta := Record{}
	body := text
	if strings.HasPrefix(text, "---\n") {
		parts := strings.SplitN(text, "---", 3)
		if len(parts) == 3 {
			body = strings.TrimLeft(parts[2], "\n")
			key := ""
			for _, line := range strings.Split(parts[1], "\n") {
				if match := frontmatterField.FindStringSubmatch(line); match != nil {
					key = match[1]
					if match[2] != "" {
						meta[key] = clean(match[2])
					} else {
						meta[key] = []string{}
					}
				} else if key != "" && frontmatterList.MatchString(line) {
					values, err := stringsArg(meta[key])
					if err != nil {
						values = []string{}
					}
					trimmed := strings.TrimSpace(line)
					values = append(values, clean(trimmed[2:]))
					meta[key] = values
				}
			}
		}
	}
	title := fallback
	if match := noteHeading.FindStringSubmatch(body); match != nil {
		title = match[1]
	}
	return meta, body, title
}
func externalText(path string, replaceInvalid bool) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		if !replaceInvalid {
			return "", errors.New("Original text must be UTF-8.")
		}
		data = []byte(decodeReplacingUTF8(data))
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), nil
}
func obsidianPaths(vault string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(vault, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == vault {
			return nil
		}
		relative, e := filepath.Rel(vault, path)
		if e != nil {
			return e
		}
		skip := false
		for _, part := range strings.Split(relative, string(os.PathSeparator)) {
			if strings.HasPrefix(part, ".") || part == "90 Templates" {
				skip = true
			}
		}
		if skip {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "AGENTS.md" || entry.Name() == "Welcome.md" {
			return nil
		}
		info, e := os.Stat(path)
		// Match Python Path.is_file's ignored stat errors. Broken links and
		// non-directory targets are not input files; permission errors and
		// actual copy/read failures must still abort.
		if errors.Is(e, os.ErrNotExist) || errors.Is(e, syscall.ENOTDIR) ||
			errors.Is(e, syscall.ELOOP) || errors.Is(e, syscall.EBADF) {
			return nil
		}
		if e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
func (l *Library) ImportObsidian(vault string) (Record, error) {
	absolute, err := filepath.Abs(vault)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, errors.New("Vault folder not found.")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return nil, errors.New("Vault folder not found.")
	}
	paths, err := obsidianPaths(absolute)
	if err != nil {
		return nil, err
	}
	suffix, err := snapshotSuffix()
	if err != nil {
		return nil, err
	}
	snapshot := filepath.Join(l.Store.Root, "imports", "obsidian-"+l.Now().In(l.Location).Format("20060102-150405")+"-"+suffix)
	if err = l.safePath(snapshot, true); err != nil {
		return nil, err
	}
	for _, path := range paths {
		relative, _ := filepath.Rel(absolute, path)
		target := filepath.Join(snapshot, relative)
		if err = copyFile(path, target, true); err != nil {
			return nil, err
		}
		before, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		after, e := l.readBytes(target)
		if e != nil {
			return nil, e
		}
		if hashText(string(before)) != hashText(string(after)) {
			return nil, errors.New("Source copy verification failed: " + path)
		}
	}
	if err = l.contractPoint("obsidian.snapshot_verified"); err != nil {
		return nil, err
	}
	created, duplicates := 0, 0
	for _, path := range paths {
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			continue
		}
		relativePath, _ := filepath.Rel(absolute, path)
		original, e := externalText(filepath.Join(snapshot, relativePath), false)
		if e != nil {
			return nil, e
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		meta, body, title := parseNote(original, stem)
		kind := "note"
		if value, ok := meta["type"]; ok {
			kind = s(value)
		}
		if meta["meeting_type"] == "1:1" {
			kind = "one-on-one"
		}
		tags := []Record{}
		for _, group := range []struct {
			fields   []string
			category string
		}{{[]string{"team", "teams"}, "team"}, {[]string{"project", "projects"}, "project"}, {[]string{"people"}, "person"}} {
			for _, field := range group.fields {
				value := meta[field]
				if value == nil {
					continue
				}
				values, e := stringsArg(value)
				if e != nil {
					values = []string{s(value)}
				}
				for _, value := range values {
					if value != "" {
						tags = append(tags, Record{"name": value, "category": group.category})
					}
				}
			}
		}
		if kind == "project" || kind == "team" || kind == "person" {
			tags = append(tags, Record{"name": stem, "category": kind})
		}
		sourceText := original
		for _, match := range wikiLinks.FindAllStringSubmatch(body, -1) {
			base := strings.SplitN(match[1], "#", 2)[0]
			for _, candidate := range paths {
				relative, _ := filepath.Rel(absolute, candidate)
				ext := strings.ToLower(filepath.Ext(candidate))
				name := filepath.Base(candidate)
				candidateStem := strings.TrimSuffix(name, filepath.Ext(candidate))
				if (relative == base || name == base || (candidateStem == base && (ext == ".txt" || ext == ".vtt"))) && (ext == ".txt" || ext == ".vtt") {
					text, e := externalText(filepath.Join(snapshot, relative), true)
					if e != nil {
						return nil, e
					}
					sourceText += "\n" + text
				}
			}
		}
		relative, _ := filepath.Rel(absolute, path)
		meta["obsidian_stem"] = stem
		meta["obsidian_relative_path"] = relative
		meta["snapshot_root"] = snapshot
		date := str(meta, "date")
		if _, ok := meta["date"]; !ok {
			date = str(meta, "last_updated")
		}
		result, e := l.Ingest(Record{"title": title, "summary": body, "kind": kind, "date": date, "tags": tags, "source_key": "obsidian:" + path, "source_path": filepath.Join(snapshot, relative), "source_text": sourceText, "metadata": meta})
		if e != nil {
			return nil, e
		}
		if truth(result["duplicate"]) {
			duplicates++
		} else {
			created++
		}
	}
	// Import runs inside dispatch with its outer lock already held.
	rows, e := l.rows("SELECT * FROM items")
	if e != nil {
		return nil, e
	}
	for _, row := range rows {
		if entityKind(str(row, "kind")) {
			e = l.migrateEntity(row)
		} else {
			e = l.ownImportedSource(row)
		}
		if e != nil {
			return nil, e
		}
	}
	return Record{"created": created, "duplicates": duplicates, "preserved_files": len(paths), "snapshot": snapshot}, nil
}

// decodeReplacingUTF8 matches the reference decoder's error-span consumption.
// Consecutive invalid leading bytes each produce a replacement; an incomplete
// valid prefix is consumed together. Original attachment bytes stay untouched.
func decodeReplacingUTF8(data []byte) string {
	var out strings.Builder
	for at := 0; at < len(data); {
		lead := data[at]
		if lead < 0x80 {
			out.WriteByte(lead)
			at++
			continue
		}
		length := 0
		switch {
		case lead >= 0xc2 && lead <= 0xdf:
			length = 2
		case lead >= 0xe0 && lead <= 0xef:
			length = 3
		case lead >= 0xf0 && lead <= 0xf4:
			length = 4
		}
		if length == 0 {
			out.WriteRune('�')
			at++
			continue
		}
		consumed := 1
		valid := true
		for n := 1; n < length; n++ {
			if at+n >= len(data) {
				valid = false
				break
			}
			next := data[at+n]
			allowed := next >= 0x80 && next <= 0xbf
			if n == 1 {
				switch lead {
				case 0xe0:
					allowed = next >= 0xa0 && next <= 0xbf
				case 0xed:
					allowed = next >= 0x80 && next <= 0x9f
				case 0xf0:
					allowed = next >= 0x90 && next <= 0xbf
				case 0xf4:
					allowed = next >= 0x80 && next <= 0x8f
				}
			}
			if !allowed {
				valid = false
				break
			}
			consumed++
		}
		if valid {
			out.Write(data[at : at+length])
			at += length
		} else {
			out.WriteRune('�')
			at += consumed
		}
	}
	return out.String()
}
