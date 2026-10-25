package library

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var googleIDPattern = regexp.MustCompile(`^/document/(?:u/\d+/)?d/([A-Za-z0-9_-]+)(?:/.*)?$`)
var normalizeURLPattern = regexp.MustCompile(`https?://[^\s<>"\)\]]+`)
var sourceURLPattern = regexp.MustCompile(`https?://[^\s<>"\)]+`)

func googleDocID(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || strings.ToLower(parsed.Hostname()) != "docs.google.com" {
		return ""
	}
	match := googleIDPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) < 2 || match[1] == "e" {
		return ""
	}
	return match[1]
}

// confluenceIdentity accepts page-ID links; display-name URLs remain ordinary links.
func confluenceIdentity(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return ""
	}
	if !strings.HasSuffix(strings.ToLower(u.Hostname()), ".atlassian.net") && !strings.HasPrefix(u.Path, "/wiki/") {
		return ""
	}
	id := ""
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, part := range parts {
		if part == "pages" && i+1 < len(parts) && digitsPattern.MatchString(parts[i+1]) {
			id = parts[i+1]
			break
		}
	}
	if strings.HasSuffix(u.Path, "/pages/viewpage.action") {
		id = u.Query().Get("pageId")
	}
	if !digitsPattern.MatchString(id) {
		return ""
	}
	return "confluence:" + strings.ToLower(u.Host) + ":" + id
}
func documentLinkIdentity(raw string) string {
	if id := googleDocID(raw); id != "" {
		return id
	}
	return confluenceIdentity(raw)
}
func normalizeGoogleURL(raw string) string {
	id := googleDocID(raw)
	if id == "" {
		return raw
	}
	parsed, _ := url.Parse(raw)
	keys := []string{}
	for _, part := range strings.Split(parsed.RawQuery, "&") {
		pair := strings.SplitN(part, "=", 2)
		key, err := url.QueryUnescape(pair[0])
		if err != nil || strings.ToLower(key) != "resourcekey" {
			continue
		}
		value := ""
		if len(pair) > 1 {
			value, _ = url.QueryUnescape(pair[1])
		}
		if value == "" {
			continue
		}
		keys = append(keys, url.QueryEscape(key)+"="+url.QueryEscape(value))
	}
	normalized := "https://docs.google.com/document/d/" + id + "/edit"
	if len(keys) > 0 {
		normalized += "?" + strings.Join(keys, "&")
	}
	return normalized
}
func normalizeDocLinks(text string) string {
	return normalizeURLPattern.ReplaceAllStringFunc(text, normalizeGoogleURL)
}
func normalizedDocURLs(urls []string) []string {
	result := []string{}
	index := map[string]int{}
	for _, raw := range urls {
		normalized := normalizeGoogleURL(raw)
		id := googleDocID(raw)
		if id == "" {
			id = normalized
		}
		if i, ok := index[id]; ok {
			if strings.Contains(strings.ToLower(normalized), "resourcekey=") {
				result[i] = normalized
			}
		} else {
			index[id] = len(result)
			result = append(result, normalized)
		}
	}
	return result
}
func sourceKind(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "calendar.google.com" || ((host == "www.google.com" || host == "google.com") && strings.HasPrefix(parsed.EscapedPath(), "/calendar")) {
		return "calendar"
	}
	if host == "docs.google.com" && strings.HasPrefix(parsed.EscapedPath(), "/document/") {
		return "document"
	}
	if confluenceIdentity(raw) != "" {
		return "document"
	}
	return ""
}
func linkIdentity(raw, kind string) string {
	if kind == "document" {
		if id := documentLinkIdentity(raw); id != "" {
			return "document\x00" + id
		}
	}
	return raw
}
func observedSourceLinks(text string, meta Record) []Record {
	candidates := []string{}
	if value, ok := meta["calendar_url"].(string); ok {
		candidates = append(candidates, value)
	}
	urls, _ := stringsArg(meta["doc_urls"])
	candidates = append(candidates, urls...)
	candidates = append(candidates, sourceURLPattern.FindAllString(text, -1)...)
	result := []Record{}
	seen := map[string]bool{}
	for _, raw := range candidates {
		raw = normalizeGoogleURL(raw)
		kind := sourceKind(raw)
		identity := linkIdentity(raw, kind)
		if kind != "" && !seen[identity] {
			seen[identity] = true
			result = append(result, Record{"url": raw, "kind": kind})
		} else if kind == "document" && strings.Contains(strings.ToLower(raw), "resourcekey=") {
			for _, entry := range result {
				if googleDocID(str(entry, "url")) == googleDocID(raw) {
					entry["url"] = raw
					break
				}
			}
		}
	}
	return result
}

var markdownLabel = regexp.MustCompile(`\[([^\]\n]+)\]\($`)
var pastURL = regexp.MustCompile(`https?://[^\s>\)]+[>\)]`)
var labelTrim = regexp.MustCompile(`^[#*\-\s]+`)
var namedLabel = regexp.MustCompile(`Project Plan:|Problem Statement:|RFC:`)
var datedLabel = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|[A-Za-z]+\s+\d{1,2},\s*\d{4})\s*\|\s*(.*)`)

func referenceLabels(text string) map[string]Record {
	result := map[string]Record{}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		for _, span := range sourceURLPattern.FindAllStringIndex(line, -1) {
			raw := normalizeGoogleURL(line[span[0]:span[1]])
			kind := sourceKind(raw)
			if kind == "" {
				continue
			}
			prefix := line[:span[0]]
			label := ""
			if match := markdownLabel.FindStringSubmatch(prefix); match != nil {
				label = match[1]
			} else {
				parts := pastURL.Split(prefix, -1)
				label = strings.TrimSpace(strings.TrimRight(parts[len(parts)-1], " <"))
				label = labelTrim.ReplaceAllString(label, "")
				positions := namedLabel.FindAllStringIndex(label, -1)
				if len(positions) > 0 {
					label = label[positions[len(positions)-1][0]:]
				} else if index := strings.LastIndex(label, ": "); index >= 0 {
					label = label[index+2:]
				} else if index := strings.LastIndex(label, " in "); index >= 0 {
					label = label[index+4:]
				}
			}
			var date any
			if match := datedLabel.FindStringSubmatch(label); match != nil && kind == "calendar" {
				label = match[2]
				for _, layout := range []string{"2006-01-02", "Jan 2, 2006", "January 2, 2006"} {
					if parsed, err := time.Parse(layout, match[1]); err == nil && validDate(parsed.Format("2006-01-02")) == nil {
						date = parsed.Format("2006-01-02")
						break
					}
				}
			}
			label = strings.Trim(strings.Join(strings.Fields(label), " "), " -*|")
			if label == "" || strings.HasPrefix(label, "http://") || strings.HasPrefix(label, "https://") || utf8.RuneCountInString(label) > 200 {
				continue
			}
			generic := false
			switch caseFold(label) {
			case "source", "google doc", "google document", "calendar", "notes", "link":
				generic = true
			}
			key := linkIdentity(raw, kind)
			old, exists := result[key]
			if !exists || (!generic && truth(old["generic"])) {
				result[key] = Record{"label": label, "date": date, "generic": generic}
			}
		}
	}
	return result
}
func docIdentity(row Record, meta Record) string {
	key := str(row, "source_key")
	if strings.HasPrefix(key, "confluence:") {
		return key
	}
	if strings.HasPrefix(key, "google-doc:") {
		return strings.TrimPrefix(key, "google-doc:")
	}
	if sync, ok := meta["google_sync"].(map[string]any); ok {
		if id := str(sync, "file_id"); id != "" {
			return id
		}
	}
	urls, _ := stringsArg(meta["doc_urls"])
	if len(urls) == 1 {
		return documentLinkIdentity(urls[0])
	}
	return ""
}
func (l *Library) sourceLinks(item Record, text string) ([]Record, error) {
	meta := item["metadata"].(Record)
	links := observedSourceLinks(text, meta)
	labels := referenceLabels(text)
	docs, err := l.rows("SELECT id,title,source_key,metadata FROM items WHERE kind IN ('document','reference') AND deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	known := map[string][]Record{}
	for _, doc := range docs {
		metadata, e := parseJSON(doc["metadata"])
		if e != nil {
			return nil, e
		}
		if id := docIdentity(doc, metadata); id != "" {
			known[id] = append(known[id], Record{"id": doc["id"], "title": doc["title"]})
		}
	}
	primary := docIdentity(item, meta)
	for _, link := range links {
		raw, kind := str(link, "url"), str(link, "kind")
		id := ""
		if kind == "document" {
			id = documentLinkIdentity(raw)
		}
		context := labels[linkIdentity(raw, kind)]
		label := str(context, "label")
		if label == "" {
			if kind == "document" {
				short := id
				if len(short) > 10 {
					short = short[:10] + "…"
				} else if short != "" {
					short += "…"
				} else {
					short = "docs.google.com"
				}
				label = "Google Doc · " + short
				if confluenceIdentity(raw) != "" {
					label = "Confluence page"
				}
			} else {
				label = "Calendar event"
			}
		}
		link["label"] = label
		if truth(context["date"]) {
			link["date"] = context["date"]
		}
		isPrimary := false
		if id != "" {
			isPrimary = (item["kind"] == "document" || item["kind"] == "reference") && id == primary
		} else {
			isPrimary = (item["kind"] == "meeting" || item["kind"] == "one-on-one" || item["kind"] == "interview") && raw == str(meta, "calendar_url")
		}
		link["primary"] = isPrimary
		if matches := known[id]; len(matches) == 1 && id != "" {
			link["atlas_id"] = matches[0]["id"]
			link["label"] = matches[0]["title"]
		}
		if isPrimary && id != "" {
			link["label"] = item["title"]
		}
	}
	return links, nil
}
func (l *Library) resolveTarget(target string, includeDeleted bool) (any, error) {
	target = strings.TrimSpace(target)
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	if strings.HasPrefix(target, "file://") {
		// target is already decoded, as in Python's unquote + urlsplit.
		// Quote literal percent signs for Go's stricter URL parser; its Path
		// then retains them for the reference's second path-unquote step.
		if parsed, err := url.Parse(strings.ReplaceAll(target, "%", "%25")); err == nil {
			target = parsed.Path
			if decoded, e := url.PathUnescape(target); e == nil {
				target = decoded
			}
		}
	}
	for _, prefix := range []string{"http://", "https://", "mailto:", "#"} {
		if strings.HasPrefix(target, prefix) {
			return nil, nil
		}
	}
	if strings.HasPrefix(target, "atlas://item/") {
		target = strings.TrimPrefix(target, "atlas://item/")
	} else if strings.HasPrefix(target, "/api/item/") {
		target = strings.TrimPrefix(target, "/api/item/")
	}
	target = strings.SplitN(strings.SplitN(target, "#", 2)[0], "?", 2)[0]
	target = strings.TrimRight(strings.TrimPrefix(target, "./"), "/")
	if target == "" {
		return nil, nil
	}
	stem := strings.TrimSuffix(target, ".md")
	query := "SELECT id,title,metadata,source_key,source_path FROM items"
	if !includeDeleted {
		query += " WHERE deleted_at IS NULL"
	}
	rows, err := l.rows(query)
	if err != nil {
		return nil, err
	}
	matches := map[string]bool{}
	for _, row := range rows {
		meta, e := parseJSON(row["metadata"])
		if e != nil {
			return nil, e
		}
		relative := str(meta, "obsidian_relative_path")
		names := []string{str(row, "id"), str(row, "title"), str(meta, "obsidian_stem"), relative, strings.TrimSuffix(relative, ".md"), str(row, "source_path"), filepath.Join(l.Store.Root, "items", str(row, "id"), "summary.md")}
		aliases, e := l.rows("SELECT path FROM source_path_aliases WHERE item_id=?", row["id"])
		if e != nil {
			return nil, e
		}
		for _, alias := range aliases {
			names = append(names, str(alias, "path"))
		}
		if key := str(row, "source_key"); strings.HasPrefix(key, "obsidian:") {
			names = append(names, strings.TrimPrefix(key, "obsidian:"))
		}
		if alias, ok := meta["aliases"].(string); ok {
			names = append(names, alias)
		} else if aliases, e := stringsArg(meta["aliases"]); e == nil {
			names = append(names, aliases...)
		}
		for _, name := range names {
			if target == name || stem == name {
				matches[str(row, "id")] = true
				break
			}
		}
	}
	if len(matches) == 1 {
		for id := range matches {
			return id, nil
		}
	}
	return nil, nil
}
func (l *Library) Source(id string) ([]byte, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	if entityKind(str(item, "kind")) {
		return l.entityOriginal(id)
	}
	path := str(item, "source_path")
	if path == "" {
		return nil, errors.New("No preserved source available.")
	}
	if err = l.safePath(path, false); err != nil {
		return nil, errors.New("No preserved source available.")
	}
	return l.readBytes(path)
}
func (l *Library) Attachment(id, target string) ([]byte, error) {
	item, err := l.Get(id, false, false)
	if err != nil {
		return nil, err
	}
	target = strings.SplitN(target, "#", 2)[0]
	if entityKind(str(item, "kind")) {
		rows, e := l.rows("SELECT path,content FROM entity_assets WHERE item_id=? ORDER BY path", id)
		if e != nil {
			return nil, e
		}
		for _, row := range rows {
			path := strings.TrimPrefix(strings.TrimPrefix(str(row, "path"), "snapshot/"), "item/")
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".txt" && ext != ".vtt" {
				continue
			}
			if path == target || filepath.Base(path) == target || strings.TrimSuffix(filepath.Base(path), ext) == target {
				return []byte(str(row, "content")), nil
			}
		}
		return nil, errors.New("Attachment not found.")
	}

	root := str(item["metadata"].(Record), "snapshot_root")
	if root == "" {
		root = filepath.Join(l.Store.Root, "items", id)
	}
	if err = l.safePath(root, false); err != nil {
		return nil, errors.New("Invalid source.")
	}
	paths, err := walkFiles(root)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		suffix := strings.ToLower(filepath.Ext(path))
		if suffix != ".txt" && suffix != ".vtt" {
			continue
		}
		relative, _ := filepath.Rel(root, path)
		name := filepath.Base(path)
		if filepath.ToSlash(relative) == target || name == target || strings.TrimSuffix(name, filepath.Ext(name)) == target {
			return l.readBytes(path)
		}
	}
	return nil, errors.New("Attachment not found.")
}
