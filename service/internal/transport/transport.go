// Package transport preserves Atlas's authenticated HTTP and stateless MCP boundary.
package transport

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	Version                     = "0.2.0"
	ProtocolVersion             = "2025-06-18"
	MaxBodyBytes          int64 = 20 * 1024 * 1024
	textMIME                    = "text/plain; charset=utf-8"
	contentSecurityPolicy       = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'"
)

// Dispatch implements one Library operation. Handler serializes all calls.
type Dispatch func(name string, args map[string]any) (any, error)

// Raw preserves original bytes. The domain owner must validate source paths.
type Raw struct {
	Bytes []byte
	MIME  string
}
type Config struct {
	Token, Library, WebRoot string
	Dispatch                Dispatch
	ReadAsset               func(string) ([]byte, error)
}
type handler struct {
	cfg   Config
	mutex sync.Mutex
}

//go:embed catalog.json
var catalogJSON []byte
var integerID = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// Internal UI dispatch operations are not implicitly new MCP tools. Preserve
// Python's actual advertised tool set plus its legacy import compatibility path.
var callableTools = func() map[string]bool {
	var catalog []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		panic(err)
	}
	names := map[string]bool{"import_obsidian": true}
	for _, tool := range catalog {
		names[tool.Name] = true
	}
	return names
}()

// New supplies no database or placeholder domain successes.
func New(cfg Config) http.Handler {
	cfg.Library = canonical(cfg.Library)
	if cfg.WebRoot != "" {
		cfg.WebRoot = canonical(cfg.WebRoot)
	}
	return &handler{cfg: cfg}
}
func canonical(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return filepath.Clean(absolute)
}
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	// Python inherits 501 for these methods before its authentication methods run.
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		unsupported(w, r)
		return
	}
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !h.authorized(r, path) {
		h.reply(w, r, 403, map[string]any{"error": "Unauthorized"})
		return
	}
	if r.Method == http.MethodDelete {
		h.reply(w, r, 405, map[string]any{"error": "Stateless endpoint; no sessions to delete."})
		return
	}
	if r.Method == http.MethodGet {
		h.get(w, r, path)
		return
	}
	h.post(w, r, path)
}
func (h *handler) authorized(r *http.Request, path string) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil || h.cfg.Token == "" {
		return false
	}
	_, port, err := net.SplitHostPort(local.String())
	if err != nil {
		return false
	}
	expected, localhost := "127.0.0.1:"+port, "localhost:"+port
	if r.Host != expected && r.Host != localhost {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != "http://"+expected && origin != "http://"+localhost {
		return false
	}
	bearer, supplied := r.Header.Get("Authorization"), ""
	if strings.HasPrefix(bearer, "Bearer ") {
		supplied = strings.TrimPrefix(bearer, "Bearer ")
	} else {
		supplied = r.Header.Get("X-Atlas-Token")
	}
	if supplied == "" && path == "/" && r.Method == http.MethodGet {
		supplied = first(r.URL.Query(), "token", "")
	}
	if supplied == "" && path != "/mcp" {
		if cookie, err := r.Cookie("atlas_token"); err == nil {
			supplied = cookie.Value
		}
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(h.cfg.Token)) == 1
}
func supported(version string) bool { return version == "2025-03-26" || version == ProtocolVersion }
func (h *handler) mcpHeaders(w http.ResponseWriter, r *http.Request) bool {
	version := "2025-03-26"
	if _, present := r.Header[http.CanonicalHeaderKey("MCP-Protocol-Version")]; present {
		version = r.Header.Get("MCP-Protocol-Version")
	}
	if !supported(version) {
		h.reply(w, r, 400, map[string]any{"error": "Unsupported MCP protocol version."})
		return false
	}
	return true
}
func first(query url.Values, name, fallback string) string {
	for _, value := range query[name] {
		if value != "" {
			return value
		}
	}
	return fallback
}
func optional(query url.Values, name string) any {
	if value := first(query, name, ""); value != "" {
		return value
	}
	return nil
}
func values(query url.Values, name string) []string {
	result := []string{}
	for _, value := range query[name] {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
func tail(path string) string { return path[strings.LastIndex(path, "/")+1:] }
func (h *handler) call(name string, args map[string]any) (any, error) {
	if h.cfg.Dispatch == nil {
		return nil, errors.New("Library dispatch is not configured.")
	}
	return h.cfg.Dispatch(name, args)
}
func (h *handler) result(w http.ResponseWriter, r *http.Request, name string, args map[string]any) {
	output, err := h.call(name, args)
	if err != nil {
		h.reply(w, r, 400, map[string]any{"error": err.Error()})
		return
	}
	h.reply(w, r, 200, output)
}
func (h *handler) get(w http.ResponseWriter, r *http.Request, path string) {
	query := r.URL.Query()
	if path == "/mcp" {
		if !h.mcpHeaders(w, r) {
			return
		}
		h.reply(w, r, 405, map[string]any{"error": "Use POST; server has no SSE stream."})
		return
	}
	if path == "/health" {
		h.reply(w, r, 200, map[string]any{"service": "atlas", "version": Version, "pid": os.Getpid(), "library": h.cfg.Library})
		return
	}
	name := ""
	args := map[string]any{}
	switch {
	case path == "/api/items":
		name = "search_items"
		args = map[string]any{"query": first(query, "q", ""), "tag_ids": values(query, "tag"), "kind": optional(query, "kind"), "inbox": first(query, "inbox", "0") == "1", "series_id": optional(query, "series_id"), "blog_feed_id": optional(query, "blog_feed_id"), "starred_only": first(query, "starred", "0") == "1", "sort_by": first(query, "sort", "date"), "collection": optional(query, "collection"), "search_scope": first(query, "search_scope", "title")}
	case strings.HasPrefix(path, "/api/person/"):
		name = "get_person"
		args["item_id"] = tail(path)
	case strings.HasPrefix(path, "/api/project/"):
		name = "get_project"
		args["item_id"] = tail(path)
	case path == "/api/blogs":
		name = "list_blog_feeds"
	case strings.HasPrefix(path, "/api/blogs/"):
		name = "get_blog_feed"
		args["feed_id"] = tail(path)
	case path == "/api/trash":
		name = "list_trash"
		args = map[string]any{"query": first(query, "q", ""), "search_scope": first(query, "search_scope", "title")}
	case strings.HasPrefix(path, "/api/trash/"):
		name = "get_item"
		args = map[string]any{"item_id": tail(path), "include_deleted": true}
	case strings.HasPrefix(path, "/api/delete-preview/"):
		name = "preview_delete_item"
		args["item_id"] = tail(path)
	case path == "/api/series":
		name = "list_meeting_series"
	case strings.HasPrefix(path, "/api/series/"):
		name = "get_meeting_series"
		args["series_id"] = tail(path)
	case strings.HasPrefix(path, "/api/entity/"):
		name = "get_entity"
		args["entity_id"] = tail(path)
	case strings.HasPrefix(path, "/api/team/"):
		name = "get_team"
		args["item_id"] = tail(path)
	case path == "/api/entities":
		name = "list_entities"
	case path == "/api/tags":
		name = "list_tags"
	case strings.HasPrefix(path, "/api/item/"):
		name = "get_item"
		args["item_id"] = tail(path)
	case strings.HasPrefix(path, "/api/history/"):
		parts := strings.Split(strings.TrimPrefix(path, "/api/history/"), "/")
		if len(parts) == 1 {
			name = "get_history"
			args["item_id"] = parts[0]
		} else if len(parts) == 2 {
			name = "get_history_content"
			args = map[string]any{"item_id": parts[0], "entry_id": parts[1]}
		} else {
			h.reply(w, r, 400, map[string]any{"error": "History entry not found."})
			return
		}
	case strings.HasPrefix(path, "/api/source/"):
		name = "get_source"
		args["item_id"] = tail(path)
	case strings.HasPrefix(path, "/api/attachment/"):
		name = "get_attachment"
		args = map[string]any{"item_id": tail(path), "target": strings.SplitN(first(query, "target", ""), "#", 2)[0]}
	case path == "/api/changed-documents":
		name = "changed_documents"
		args = map[string]any{"query": first(query, "q", ""), "search_scope": first(query, "search_scope", "title"), "tag_ids": values(query, "tag")}
	case path == "/api/changes":
		name = "changes"
	case path == "/api/activity":
		name = "list_activity"
		var limit any = 50
		if value := first(query, "limit", ""); value != "" {
			limit = value
		}
		args = map[string]any{"limit": limit, "before_id": optional(query, "before_id")}
	case path == "/api/info":
		name = "info"
	default:
		static := map[string]struct{ name, mime string }{"/": {"index.html", "text/html; charset=utf-8"}, "/app.js": {"app.js", "text/javascript; charset=utf-8"}, "/style.css": {"style.css", "text/css; charset=utf-8"}}
		file, ok := static[path]
		if !ok {
			h.reply(w, r, 404, map[string]any{"error": "Not found"})
			return
		}
		var body []byte
		var err error
		if h.cfg.ReadAsset != nil {
			body, err = h.cfg.ReadAsset(file.name)
		} else if h.cfg.WebRoot != "" {
			body, err = os.ReadFile(filepath.Join(h.cfg.WebRoot, file.name))
		} else {
			err = errors.New("Web assets are not configured")
		}
		if err != nil {
			h.reply(w, r, 400, map[string]any{"error": err.Error()})
			return
		}
		if file.name == "index.html" {
			body = bytes.ReplaceAll(body, []byte("__TOKEN__"), []byte(h.cfg.Token))
		}
		h.reply(w, r, 200, Raw{Bytes: body, MIME: file.mime})
		return
	}
	h.result(w, r, name, args)
}
func decode(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("Invalid UTF-8 JSON body.")
	}
	if !validUnicodeEscapes(data) {
		return nil, errors.New("Unpaired Unicode escape in JSON body.")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("Extra data after JSON value.")
	}
	return result, nil
}
func (h *handler) post(w http.ResponseWriter, r *http.Request, path string) {
	if r.ContentLength > MaxBodyBytes {
		h.reply(w, r, 400, map[string]any{"error": "Import exceeds 20 MB."})
		return
	}
	if path == "/mcp" {
		if !h.mcpHeaders(w, r) {
			return
		}
		if strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]) != "application/json" {
			h.reply(w, r, 415, map[string]any{"error": "Expected application/json."})
			return
		}
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			h.reply(w, r, 406, map[string]any{"error": "Accept application/json and text/event-stream."})
			return
		}
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err == nil && int64(len(data)) > MaxBodyBytes {
		err = errors.New("Import exceeds 20 MB.")
	}
	if err != nil {
		h.reply(w, r, 400, map[string]any{"error": err.Error()})
		return
	}
	message, err := decode(data)
	if path == "/mcp" {
		if err != nil {
			h.reply(w, r, 400, rpcError(nil, -32700, "Parse error."))
			return
		}
		output, notification := h.mcp(message)
		if notification {
			h.reply(w, r, 202, nil)
			return
		}
		h.reply(w, r, 200, output)
		return
	}
	if err != nil {
		h.reply(w, r, 400, map[string]any{"error": err.Error()})
		return
	}
	args, ok := message.(map[string]any)
	if !ok {
		h.reply(w, r, 400, map[string]any{"error": "Expected an argument object."})
		return
	}
	routes := map[string]string{"/api/tag": "tag", "/api/delete": "delete_item", "/api/restore": "restore_item", "/api/series": "assign_meeting_series", "/api/entity": "create_entity", "/api/link-entity": "link_entity", "/api/apply": "apply_tag", "/api/undo": "undo", "/api/backup": "backup", "/api/import": "import_item", "/api/summary": "update_summary", "/api/changes-seen": "mark_document_changes_seen", "/api/date": "set_date", "/api/authors": "set_authors", "/api/starred": "set_starred", "/api/refresh-request": "set_refresh_requested", "/api/reviewed": "set_reviewed"}
	name, ok := routes[path]
	if !ok {
		h.reply(w, r, 404, map[string]any{"error": "Not found"})
		return
	}
	if name == "import_item" {
		args["explicit"] = true
	}
	if name == "undo" || name == "backup" {
		args = map[string]any{}
	}
	if name == "tag" {
		value, exists := args["name"]
		if !exists {
			h.reply(w, r, 400, map[string]any{"error": "'name'"})
			return
		}
		category, exists := args["category"]
		if !exists {
			category = "topic"
		}
		args = map[string]any{"name": value, "category": category}
	}
	h.result(w, r, name, args)
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func validID(id any) bool {
	switch value := id.(type) {
	case string:
		return true
	case json.Number:
		return integerID.MatchString(string(value))
	default:
		return false
	}
}
func (h *handler) mcp(message any) (any, bool) {
	request, ok := message.(map[string]any)
	if !ok {
		return rpcError(nil, -32600, "Invalid request."), false
	}
	method, methodOK := request["method"].(string)
	params := map[string]any{}
	paramsOK := true
	if raw, exists := request["params"]; exists {
		params, paramsOK = raw.(map[string]any)
	}
	id, hasID := request["id"]
	if request["jsonrpc"] != "2.0" || !methodOK || !paramsOK || (hasID && !validID(id)) {
		return rpcError(nil, -32600, "Invalid request."), false
	}
	if !hasID {
		return nil, true
	}
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	switch method {
	case "initialize":
		version, _ := params["protocolVersion"].(string)
		if !supported(version) {
			version = ProtocolVersion
		}
		response["result"] = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "atlas", "version": Version}}
	case "ping":
		response["result"] = map[string]any{}
	case "tools/list":
		response["result"] = map[string]any{"tools": json.RawMessage(catalogJSON)}
	case "tools/call":
		name, nameOK := params["name"].(string)
		args := map[string]any{}
		argsOK := true
		if raw, exists := params["arguments"]; exists {
			args, argsOK = raw.(map[string]any)
		}
		var output any
		var err error
		if !nameOK || !argsOK {
			err = errors.New("A tool name and argument object are required.")
		} else if !callableTools[name] {
			err = errors.New("Unknown tool.")
		} else {
			output, err = h.call(name, args)
		}
		if err != nil {
			response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
		} else {
			encoded, encodeErr := json.Marshal(output)
			if encodeErr != nil {
				response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": encodeErr.Error()}}, "isError": true}
			} else {
				response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}}
			}
		}
	default:
		return rpcError(id, -32601, "Method not found."), false
	}
	return response, false
}
func (h *handler) reply(w http.ResponseWriter, r *http.Request, status int, output any) {
	mime := "application/json"
	var body []byte
	if status != 202 {
		if raw, ok := output.(Raw); ok {
			body = raw.Bytes
			mime = raw.MIME
			if mime == "" {
				mime = textMIME
			}
		} else {
			var err error
			body, err = json.Marshal(output)
			if err != nil {
				status = 400
				body, _ = json.Marshal(map[string]any{"error": err.Error()})
			}
		}
	}
	header := w.Header()
	header.Set("Content-Type", mime)
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("Content-Length", fmt.Sprint(len(body)))
	if r.URL.EscapedPath() == "/" && status == 200 {
		header.Set("Set-Cookie", "atlas_token="+h.cfg.Token+"; HttpOnly; SameSite=Strict; Path=/")
	}
	if status == 405 {
		header.Set("Allow", "POST")
	}
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}
func unsupported(w http.ResponseWriter, r *http.Request) {
	body := fmt.Sprintf("<!DOCTYPE HTML>\n<html lang=\"en\">\n    <head>\n        <meta charset=\"utf-8\">\n        <title>Error response</title>\n    </head>\n    <body>\n        <h1>Error response</h1>\n        <p>Error code: 501</p>\n        <p>Message: Unsupported method ('%s').</p>\n        <p>Error code explanation: 501 - Server does not support this operation.</p>\n    </body>\n</html>\n", r.Method)
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.Header().Set("Connection", "close")
	w.WriteHeader(501)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}

// JSON's decoder otherwise replaces unpaired UTF-16 escape values with U+FFFD.
// Reject those bodies before dispatch so imported originals cannot be silently
// changed. Literal backslash-u text and properly paired escapes remain valid.
func validUnicodeEscapes(data []byte) bool {
	inString := false
	hex4 := func(at int) (uint16, bool) {
		if at+4 > len(data) {
			return 0, false
		}
		var value uint16
		for _, b := range data[at : at+4] {
			value <<= 4
			switch {
			case b >= '0' && b <= '9':
				value |= uint16(b - '0')
			case b >= 'a' && b <= 'f':
				value |= uint16(b - 'a' + 10)
			case b >= 'A' && b <= 'F':
				value |= uint16(b - 'A' + 10)
			default:
				return 0, false
			}
		}
		return value, true
	}
	for at := 0; at < len(data); {
		b := data[at]
		if b == '"' {
			inString = !inString
			at++
			continue
		}
		if !inString || b != '\\' {
			at++
			continue
		}
		if at+1 >= len(data) {
			return true
		} // The ordinary JSON decoder reports syntax errors.
		if data[at+1] != 'u' {
			at += 2
			continue
		}
		value, ok := hex4(at + 2)
		if !ok {
			return true
		}
		if value >= 0xd800 && value <= 0xdbff {
			if at+12 > len(data) || data[at+6] != '\\' || data[at+7] != 'u' {
				return false
			}
			low, ok := hex4(at + 8)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			at += 12
			continue
		}
		if value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		at += 6
	}
	return true
}
