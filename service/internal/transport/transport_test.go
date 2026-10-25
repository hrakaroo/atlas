package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const token = "transport-test-private-token"

func configuration(t *testing.T, dispatch Dispatch) Config {
	t.Helper()
	root := t.TempDir()
	for name, text := range map[string]string{"index.html": "<main>Atlas __TOKEN__</main>", "app.js": "Atlas JS", "style.css": "Atlas CSS"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return Config{Token: token, Library: root, WebRoot: root, Dispatch: dispatch}
}
func request(h http.Handler, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:47831"+path, bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 47831}))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		if k == "Host" {
			r.Host = v
		} else if v == "" {
			r.Header.Del(k)
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func jsonRequest(t *testing.T, h http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return request(h, "POST", path, body, nil)
}
func object(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("%d: %v: %s", w.Code, err, w.Body)
	}
	return result
}
func TestAuthenticationUsesActualListenerAddress(t *testing.T) {
	h := New(configuration(t, nil))
	for _, headers := range []map[string]string{
		{"Authorization": ""}, {"Authorization": "Bearer wrong"}, {"Host": "evil.example"},
		{"Host": "127.0.0.1:47832"}, {"Host": "localhost:47832"},
		{"Origin": "http://127.0.0.1:47832"}, {"Origin": "http://localhost:47832"}, {"Origin": "https://evil.example"},
	} {
		if w := request(h, "GET", "/health", nil, headers); w.Code != 403 {
			t.Fatalf("headers%v: %d", headers, w.Code)
		}
	}
	for _, headers := range []map[string]string{{"Host": "localhost:47831", "Origin": "http://localhost:47831"}, {"Authorization": "", "X-Atlas-Token": token}} {
		if w := request(h, "GET", "/health", nil, headers); w.Code != 200 {
			t.Fatalf("valid local headers rejected:%d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:47831/health", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("request without listener context must be rejected")
	}
	// These tests also traverse a real listener so no artificial request context can hide a failure.
	server := httptest.NewServer(h)
	defer server.Close()
	real, err := http.NewRequest("GET", server.URL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	real.Header.Set("Authorization", "Bearer "+token)
	response, err := server.Client().Do(real)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("actual listener status:%d", response.StatusCode)
	}
}
func TestCookieAndURLTokenScope(t *testing.T) {
	h := New(configuration(t, nil))
	w := request(h, "GET", "/?token="+token, nil, map[string]string{"Authorization": ""})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := strings.Split(w.Header().Get("Set-Cookie"), ";")[0]
	if !strings.Contains(w.Header().Get("Set-Cookie"), "HttpOnly; SameSite=Strict; Path=/") {
		t.Fatal(w.Header())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(token)) || bytes.Contains(w.Body.Bytes(), []byte("__TOKEN__")) {
		t.Fatal("UI token substitution failed")
	}
	for _, test := range []struct {
		path   string
		status int
	}{{"/app.js", 200}, {"/mcp", 403}, {"/health?token=" + token, 403}} {
		headers := map[string]string{"Authorization": ""}
		if test.path != "/health?token="+token {
			headers["Cookie"] = cookie
		}
		if response := request(h, "GET", test.path, nil, headers); response.Code != test.status {
			t.Fatalf("%s:%d", test.path, response.Code)
		}
	}
	if w := request(h, "GET", "/health", nil, map[string]string{"Authorization": "Bearer wrong", "Cookie": cookie}); w.Code != 403 {
		t.Fatal("cookie must not replace nonempty wrong bearer")
	}
}
func TestRawBytesStaticHeadersAndUnsupportedMethods(t *testing.T) {
	original := []byte{'c', 'a', 'f', 0xc3, 0xa9, '\r', '\n', 0xff}
	h := New(configuration(t, func(name string, args map[string]any) (any, error) { return Raw{Bytes: original, MIME: textMIME}, nil }))
	w := request(h, "GET", "/api/source/item", nil, nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), original) || w.Header().Get("Content-Type") != textMIME {
		t.Fatal("raw source was changed")
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w = request(h, "GET", path, nil, nil)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") != contentSecurityPolicy {
			t.Fatalf("static headers:%s:%v", path, w.Header())
		}
	}
	for _, method := range []string{"HEAD", "PUT", "OPTIONS", "PATCH"} {
		w = request(h, method, "/health", nil, map[string]string{"Authorization": ""})
		if w.Code != 501 || w.Header().Get("Content-Type") != "text/html;charset=utf-8" {
			t.Fatalf("%s:%d", method, w.Code)
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD response contained a body")
		}
	}
	if w = request(h, "DELETE", "/missing", nil, nil); w.Code != 405 || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
	if w = request(h, "DELETE", "/missing", nil, map[string]string{"Authorization": ""}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w = request(h, "GET", "/%61pp.js", nil, nil); w.Code != 404 {
		t.Fatal("escaped route unexpectedly normalized")
	}
}
func TestRPCIDsNotificationAndFrozenCatalog(t *testing.T) {
	h := New(configuration(t, nil))
	for _, id := range []string{`"abc"`, `""`, `1234567890123456789012345678901234567890`, `-123456789012345678901234567890`} {
		w := request(h, "POST", "/mcp", []byte(`{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`), nil)
		decoder := json.NewDecoder(w.Body)
		decoder.UseNumber()
		var result map[string]any
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result["id"])
		if w.Code != 200 || string(encoded) != id {
			t.Fatalf("ID loss:%s %s", id, encoded)
		}
	}
	for _, id := range []string{"true", "null", "1.0", "1e3"} {
		w := request(h, "POST", "/mcp", []byte(`{"jsonrpc":"2.0","id":`+id+`,"method":"ping"}`), nil)
		if object(t, w)["error"].(map[string]any)["code"] != float64(-32600) {
			t.Fatal(id)
		}
	}
	for _, method := range []string{"notifications/initialized", "missing", "tools/call"} {
		w := jsonRequest(t, h, "/mcp", map[string]any{"jsonrpc": "2.0", "method": method})
		if w.Code != 202 || w.Body.Len() != 0 {
			t.Fatalf("notification:%s %d", method, w.Code)
		}
	}
	w := jsonRequest(t, h, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	tools := object(t, w)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 41 {
		t.Fatal(len(tools))
	}
	schemas := map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		schemas[tool["name"].(string)] = tool["inputSchema"]
	}
	// Go's encoder orders object keys, matching the Python schema-only canonical digest.
	canonical, _ := json.Marshal(schemas)
	digest := sha256.Sum256(canonical)
	if hex.EncodeToString(digest[:]) != "dcf58ad061c7455093884df2c2b32dcbaa77a0159420ff220879e5157a338b53" {
		t.Fatalf("catalog schema changed:%x", digest)
	}
	for _, params := range []string{"null", "[]", "true"} {
		w = request(h, "POST", "/mcp", []byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":`+params+`}`), nil)
		if object(t, w)["error"].(map[string]any)["code"] != float64(-32600) {
			t.Fatal(params)
		}
	}
}
func TestParsingLimitsAndMCPHeaderErrors(t *testing.T) {
	h := New(configuration(t, nil))
	ping := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	for _, test := range []struct {
		headers map[string]string
		status  int
	}{
		{map[string]string{"MCP-Protocol-Version": "bad"}, 400},
		{map[string]string{"Content-Type": "text/plain"}, 415},
		{map[string]string{"Accept": "application/json"}, 406},
	} {
		if w := request(h, "POST", "/mcp", ping, test.headers); w.Code != test.status {
			t.Fatal(w.Code)
		}
	}
	for _, body := range [][]byte{[]byte("{"), {0xff}, []byte(`{"jsonrpc":"2.0"} {}`)} {
		w := request(h, "POST", "/mcp", body, nil)
		if w.Code != 400 || object(t, w)["error"].(map[string]any)["code"] != float64(-32700) {
			t.Fatalf("bad parse:%d", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:47831/mcp", io.NopCloser(failReader{t}))
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 47831}))
	r.Header.Set("Authorization", "Bearer "+token)
	r.ContentLength = MaxBodyBytes + 1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Import exceeds 20 MB.") {
		t.Fatal(w.Code, w.Body)
	}
	// Explicitly present empty version differs from an absent version in Python.
	r = httptest.NewRequest("GET", "http://127.0.0.1:47831/mcp", nil)
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 47831}))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("MCP-Protocol-Version", "")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("empty version accepted", w.Code)
	}
}

type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("oversized declared body must not be read")
	return 0, io.EOF
}
func TestEveryGETRouteDispatchAndQuerySemantics(t *testing.T) {
	var called string
	var arguments map[string]any
	h := New(configuration(t, func(name string, args map[string]any) (any, error) {
		called = name
		arguments = args
		return map[string]any{"dispatched": name}, nil
	}))
	routes := map[string]string{
		"/api/items": "search_items", "/api/person/id": "get_person", "/api/project/id": "get_project",
		"/api/blogs": "list_blog_feeds", "/api/blogs/id": "get_blog_feed", "/api/trash": "list_trash", "/api/trash/id": "get_item",
		"/api/delete-preview/id": "preview_delete_item", "/api/series": "list_meeting_series", "/api/series/id": "get_meeting_series",
		"/api/entity/id": "get_entity", "/api/team/id": "get_team", "/api/entities": "list_entities", "/api/tags": "list_tags",
		"/api/item/id": "get_item", "/api/history/id": "get_history", "/api/history/id/entry": "get_history_content",
		"/api/source/id": "get_source", "/api/attachment/id": "get_attachment", "/api/changed-documents": "changed_documents",
		"/api/changes": "changes", "/api/activity": "list_activity", "/api/info": "info",
	}
	for path, expected := range routes {
		called = ""
		w := request(h, "GET", path, nil, nil)
		if w.Code != 200 || called != expected {
			t.Fatalf("%s dispatch:%s code:%d", path, called, w.Code)
		}
	}
	request(h, "GET", "/api/items?q=&q=Roadmap&tag=&tag=a&tag=b&kind=document&inbox=1&starred=1&sort=title&collection=people", nil, nil)
	expected := map[string]any{"query": "Roadmap", "tag_ids": []string{"a", "b"}, "kind": "document", "inbox": true, "series_id": nil, "blog_feed_id": nil, "starred_only": true, "sort_by": "title", "collection": "people", "search_scope": "title"}
	if !reflect.DeepEqual(arguments, expected) {
		t.Fatalf("query:%#v", arguments)
	}
	request(h, "GET", "/api/trash/id", nil, nil)
	if !reflect.DeepEqual(arguments, map[string]any{"item_id": "id", "include_deleted": true}) {
		t.Fatal(arguments)
	}
	request(h, "GET", "/api/attachment/id?target=folder%2Fcaption.txt%23timestamp", nil, nil)
	if arguments["target"] != "folder/caption.txt" {
		t.Fatal(arguments)
	}
	if w := request(h, "GET", "/api/history/id/entry/extra", nil, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestEveryPOSTRouteAndExplicitImport(t *testing.T) {
	var called string
	var arguments map[string]any
	h := New(configuration(t, func(name string, args map[string]any) (any, error) {
		called = name
		arguments = args
		return map[string]any{}, nil
	}))
	routes := map[string]string{"/api/tag": "tag", "/api/delete": "delete_item", "/api/restore": "restore_item", "/api/series": "assign_meeting_series", "/api/entity": "create_entity", "/api/link-entity": "link_entity", "/api/apply": "apply_tag", "/api/undo": "undo", "/api/backup": "backup", "/api/import": "import_item", "/api/summary": "update_summary", "/api/changes-seen": "mark_document_changes_seen", "/api/date": "set_date", "/api/authors": "set_authors", "/api/starred": "set_starred", "/api/refresh-request": "set_refresh_requested", "/api/reviewed": "set_reviewed"}
	for path, expected := range routes {
		called = ""
		w := jsonRequest(t, h, path, map[string]any{"name": "Topic", "explicit": false, "extra": "ignored only where Python ignores it"})
		if w.Code != 200 || called != expected {
			t.Fatalf("%s:%s %d", path, called, w.Code)
		}
	}
	jsonRequest(t, h, "/api/import", map[string]any{"explicit": false})
	if arguments["explicit"] != true {
		t.Fatal("UI import must be explicit")
	}
	jsonRequest(t, h, "/api/tag", map[string]any{"name": "Topic", "extra": true})
	if !reflect.DeepEqual(arguments, map[string]any{"name": "Topic", "category": "topic"}) {
		t.Fatal(arguments)
	}
	for _, path := range []string{"/api/undo", "/api/backup"} {
		jsonRequest(t, h, path, map[string]any{"extra": true})
		if len(arguments) != 0 {
			t.Fatal(arguments)
		}
	}
	if w := jsonRequest(t, h, "/api/tag", map[string]any{}); w.Code != 400 || object(t, w)["error"] != "'name'" {
		t.Fatal(w.Code, w.Body)
	}
}
func TestDispatchErrorsAndGlobalSerialization(t *testing.T) {
	h := New(configuration(t, nil))
	if w := request(h, "GET", "/api/info", nil, nil); w.Code != 400 || !strings.Contains(w.Body.String(), "not configured") {
		t.Fatal(w.Code, w.Body)
	}
	w := jsonRequest(t, h, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "get_item"}})
	if object(t, w)["result"].(map[string]any)["isError"] != true {
		t.Fatal(w.Body)
	}
	var active, maximum atomic.Int32
	h = New(configuration(t, func(name string, args map[string]any) (any, error) {
		count := active.Add(1)
		if count > maximum.Load() {
			maximum.Store(count)
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
		return nil, errors.New("Expected domain error")
	}))
	var wait sync.WaitGroup
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if w := request(h, "GET", "/api/info", nil, nil); w.Code != 400 {
				t.Errorf("domain error:%d", w.Code)
			}
		}()
	}
	wait.Wait()
	if maximum.Load() != 1 {
		t.Fatal("dispatch requests ran concurrently", maximum.Load())
	}
}

func TestInternalUIDispatchIsNotImplicitlyAnMCPTool(t *testing.T) {
	calls := []string{}
	h := New(configuration(t, func(name string, args map[string]any) (any, error) {
		calls = append(calls, name)
		return map[string]any{"called": name}, nil
	}))
	for _, name := range []string{"info", "get_source", "get_attachment", "get_history", "get_history_content", "changes", "tag", "undo", "backup"} {
		response := jsonRequest(t, h, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name}})
		result := object(t, response)["result"].(map[string]any)
		if result["isError"] != true || result["content"].([]any)[0].(map[string]any)["text"] != "Unknown tool." {
			t.Fatalf("UI operation exposed:%s %v", name, result)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("unexpected calls:%v", calls)
	}
	response := jsonRequest(t, h, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "import_obsidian", "arguments": map[string]any{"source": "fixture"}}})
	if response.Code != 200 || len(calls) != 1 || calls[0] != "import_obsidian" {
		t.Fatal("legacy MCP compatibility route missing")
	}
	if response := request(h, "GET", "/api/info", nil, nil); response.Code != 200 || calls[len(calls)-1] != "info" {
		t.Fatal("UI route was blocked")
	}
}
