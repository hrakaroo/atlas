"""Wire contracts, independent of application module internals."""
import json
import hashlib
import unittest
from harness import EndpointCase, ROOT, ToolError, exact, IdMap


class ComparisonTests(unittest.TestCase):
    def test_meaningful_mutations_are_detected(self):
        original = {"revision": 2, "source": b"cafe\r\n", "events": [{"id": 1}]}
        exact(original, original.copy())
        for changed in (
            dict(original, revision=3),
            dict(original, source=b"cafe\n"),
            dict(original, events=[{"id": 1}, {"id": 2}]),
            dict(original, revision=True),
            dict(original, missing=None),
        ):
            with self.assertRaises(AssertionError):
                exact(original, changed)
        for left, right in ((None, {}), (1, True), ([1, 2], [2, 1]), ({}, {"x": None})):
            with self.assertRaises(AssertionError):
                exact(left, right)
        mapping = IdMap()
        mapping.bind("a", "x")
        mapping.bind("a", "x")
        self.assertEqual(mapping.resolve("a"), "x")
        for left, right in (("a", "y"), ("b", "x")):
            with self.assertRaises(AssertionError):
                mapping.bind(left, right)


class TransportTests(EndpointCase):
    def test_auth_host_origin_cookie_and_url_token_scope(self):
        token = self.service.connection["token"]
        for headers in (
            {"Authorization": None}, {"Authorization": "Bearer wrong"},
            {"Host": "evil.example"}, {"Origin": "https://evil.example"},
        ):
            code, _, body = self.request("/health", headers=headers)
            self.assertEqual(code, 403)
            exact({"error": "Unauthorized"}, json.loads(body))
        code, headers, body = self.request("/?token=" + token, headers={"Authorization": None})
        self.assertEqual(code, 200)
        self.assertIn(b"Atlas", body)
        cookie = headers["Set-Cookie"]
        self.assertIn("HttpOnly", cookie)
        self.assertIn("SameSite=Strict", cookie)
        cookie = cookie.split(";")[0]
        self.assertEqual(self.request("/app.js", headers={"Authorization": None, "Cookie": cookie})[0], 200)
        self.assertEqual(self.request("/mcp", headers={"Authorization": None, "Cookie": cookie})[0], 403)
        self.assertEqual(self.request("/health?token=" + token, headers={"Authorization": None})[0], 403)
        self.assertEqual(self.request("/health", headers={"Authorization": None, "X-Atlas-Token": token})[0], 200)
        # Operational evidence: only the protected connection file may be read.
        self.assertEqual((self.service.library / ".service" / "connection.json").stat().st_mode & 0o777, 0o600)
        health = self.get("/health")
        self.assertEqual(health["pid"], self.service.pid)
        self.assertEqual(health["library"], str(self.service.library))
        self.assertNotIn(token.encode(), json.dumps(health).encode())

    def test_static_headers_and_method_errors(self):
        for path, mime in (("/", "text/html"), ("/app.js", "text/javascript"), ("/style.css", "text/css")):
            code, headers, body = self.request(path)
            self.assertEqual(code, 200)
            self.assertTrue(headers["Content-Type"].startswith(mime))
            self.assertTrue(body)
            self.assertEqual(headers["Cache-Control"], "no-store")
            self.assertEqual(headers["Referrer-Policy"], "no-referrer")
            self.assertEqual(headers["X-Content-Type-Options"], "nosniff")
            self.assertIn("frame-ancestors 'none'", headers["Content-Security-Policy"])
        for method in ("GET", "POST"):
            self.assertEqual(self.request("/missing", {} if method == "POST" else None, method=method)[0], 404)
        for path in ("/mcp", "/missing"):
            self.assertEqual(self.request(path, method="DELETE")[0], 405)
            self.assertEqual(self.request(path, method="DELETE", headers={"Authorization": None})[0], 403)
        for method in ("HEAD", "PUT", "OPTIONS"):
            self.assertEqual(self.request("/health", method=method)[0], 501)
        self.assertEqual(self.request("/mcp")[0], 405)
        self.assertEqual(self.request("/api/item/missing")[0], 400)

    def test_mcp_protocol_catalog_schema_and_errors(self):
        for version, expected in (("2025-03-26", "2025-03-26"), ("2025-06-18", "2025-06-18"),
                                  ("2099-01-01", "2025-06-18")):
            response = self.service.rpc("initialize", {"protocolVersion": version})
            self.assertEqual(response["result"]["protocolVersion"], expected)
            exact({"tools": {}}, response["result"]["capabilities"])
        exact({}, self.service.rpc("ping")["result"])
        code, _, body = self.request("/mcp", {"jsonrpc": "2.0", "method": "notifications/initialized"})
        self.assertEqual((code, body), (202, b""))
        advertised = self.service.rpc("tools/list")["result"]["tools"]
        inventory = json.loads((ROOT / "service" / "contract" / "inventory.json").read_text())
        expected = {entry["name"]: entry for entry in inventory["mcp_tools"]}
        # Frozen from reviewed Python endpoint output, not regenerated during test runs.
        schemas = {tool["name"]: tool["inputSchema"] for tool in advertised}
        digest = hashlib.sha256(json.dumps(schemas, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        self.assertEqual(digest, "dcf58ad061c7455093884df2c2b32dcbaa77a0159420ff220879e5157a338b53")
        self.assertEqual([tool["name"] for tool in advertised], [entry["name"] for entry in inventory["mcp_tools"]])
        self.assertEqual(len(advertised), 41)
        self.assertEqual({x["name"] for x in advertised}, set(expected))
        for tool in advertised:
            schema = tool["inputSchema"]
            exact("object", schema["type"])
            exact(False, schema["additionalProperties"])
            exact(expected[tool["name"]]["required"], schema["required"])
            self.assertEqual(set(expected[tool["name"]]["properties"]), set(schema["properties"]))
        self.assertNotIn("search_scope", next(t for t in advertised if t["name"] == "search_items")["inputSchema"]["properties"])
        self.assertEqual(self.service.rpc("missing")["error"]["code"], -32601)
        for payload in ([], {"jsonrpc": "1.0", "id": 1, "method": "ping"},
                        {"jsonrpc": "2.0", "id": True, "method": "ping"},
                        {"jsonrpc": "2.0", "id": None, "method": "ping"}):
            self.assertEqual(self.post("/mcp", payload)["error"]["code"], -32600)
        for name, args in (("missing", {}), ("get_item", {}), ("get_item", {"item_id": "missing"})):
            with self.assertRaises(ToolError):
                self.tool(name, **args)
        self.assertEqual(self.get("/api/info")["count"], 0)

    def test_rejected_requests_leave_service_usable(self):
        ping = {"jsonrpc": "2.0", "id": 1, "method": "ping"}
        for headers, status in (({"Content-Type": "text/plain"}, 415),
                                ({"Accept": "application/json"}, 406),
                                ({"MCP-Protocol-Version": "bad"}, 400)):
            self.assertEqual(self.request("/mcp", ping, headers=headers)[0], status)
        for malformed in (b"{", b"\xff"):
            code, _, body = self.request("/mcp", malformed)
            self.assertEqual(code, 400)
            self.assertEqual(json.loads(body)["error"]["code"], -32700)
            self.assertEqual(self.request("/api/import", malformed)[0], 400)
        self.assertEqual(self.request("/api/import", [1])[0], 400)
        self.assertEqual(self.request("/api/import", b"", headers={"Content-Length": str(21*1024*1024)})[0], 400)
        self.assertEqual(self.get("/api/info")["count"], 0)
        exact({}, self.service.rpc("ping")["result"])
