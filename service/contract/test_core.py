"""Core observable behavior. Every library action/assertion goes through an endpoint."""
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import hashlib
import json
import unittest
from urllib.parse import urlencode
from harness import EndpointCase, ToolError, exact


class CoreTests(EndpointCase):
    def import_item(self, key="core", **extra):
        values = dict(title="Café resources", summary="# Blurb\nSource grounded.",
                      kind="document", original_text="Complete café 東京 transcript.\r\nSecond line.\n",
                      source_key="contract:" + key)
        values.update(extra)
        return self.tool("import_item", **values)["id"]

    def test_record_kinds_manual_import_and_lightweight_listing(self):
        kinds = ("document", "reference", "meeting", "one-on-one", "person", "project", "note", "blog", "update")
        ids = []
        for kind in kinds:
            payload = dict(title="Contract " + kind, summary="# Blurb\nSynthetic " + kind,
                           kind=kind, date="2026-08-01", original_text="Full source " + kind,
                           source_key="contract:kind:" + kind)
            if kind == "blog":
                payload["blog_feed"] = "Contract feed"
            if kind == "update":
                payload.update(period_start="2026-08-01", period_end="2026-08-08",
                               source_channel="contract-channel")
            ident = self.post("/api/import", payload)["id"]
            ids.append(ident)
            row = self.tool("get_item", item_id=ident)
            self.assertEqual(row["kind"], kind)
            self.assertEqual(row["date"], "2026-08-01")
            self.assertEqual(self.request("/api/source/" + ident)[2], payload["original_text"].encode())
        listing = self.get("/api/items")
        self.assertEqual(set(ids), {x["id"] for x in listing})
        self.assertEqual(self.get("/api/info")["count"], len(ids))
        self.assertEqual(len(self.tool("list_activity")["events"]), len(ids))

    def test_full_source_cross_surface_and_restart(self):
        text = "Café 東京 😀\r\nsecond\rthird\n"
        ident = self.import_item(original_text=text)
        item = self.get("/api/item/" + ident)
        schema = {
            "id": str, "title": str, "kind": str, "date": str, "summary": str,
            "source_key": str, "source_path": str, "source_hash": str, "metadata": dict,
            "revision": int, "created": str, "updated": str, "period_start": type(None),
            "period_end": type(None), "source_channel": type(None), "inbox": int,
            "deleted_at": type(None), "starred": int, "date_basis": str, "authors": list,
            "tags": list, "is_interview_candidate": bool, "is_interview": bool,
            "links": list, "relationships": list, "participants": list,
            "document_people": list, "projects": list, "series": type(None),
            "blog_feed": type(None), "source_links": list,
        }
        self.assertEqual(set(schema), set(item))
        for field, expected_type in schema.items():
            self.assertIs(type(item[field]), expected_type, field)
        exact(item, self.tool("get_item", item_id=ident))
        self.assertEqual(item["source_hash"], hashlib.sha256(text.encode()).hexdigest())
        code, headers, body = self.request("/api/source/" + ident)
        self.assertEqual(code, 200)
        self.assertEqual(headers["Content-Type"], "text/plain; charset=utf-8")
        exact(text.encode(), body)
        self.assertEqual(item["date_basis"], "added")
        dt.date.fromisoformat(item["date"])
        self.assertIsNone(item["deleted_at"])
        old_pid = self.service.pid
        self.restart()
        self.assertNotEqual(old_pid, self.service.pid)
        exact(item, self.tool("get_item", item_id=ident))
        exact(text.encode(), self.request("/api/source/" + ident)[2])
        self.assertEqual([ident], [x["id"] for x in self.get("/api/items?kind=document")])

    def test_edits_stale_revisions_dates_stars_tags_and_undo(self):
        ident = self.import_item(date="2026-08-02")
        original = self.tool("get_item", item_id=ident)
        self.assertEqual(original["date_basis"], "source")
        edited = self.post("/api/summary", dict(item_id=ident, expected_revision=original["revision"],
                                                summary="# Blurb\nNew summary."))
        self.assertEqual(edited["revision"], original["revision"] + 1)
        self.assertEqual(edited["source_hash"], original["source_hash"])
        before_activity = self.tool("list_activity")
        code, _, _ = self.request("/api/summary", dict(item_id=ident, expected_revision=original["revision"],
                                                       summary="Must not overwrite"))
        self.assertEqual(code, 400)
        exact(edited, self.get("/api/item/" + ident))
        exact(before_activity, self.tool("list_activity"))
        with self.assertRaises(ToolError):
            self.tool("update_summary", item_id=ident, expected_revision=original["revision"], summary="Stale MCP")
        current = self.post("/api/date", dict(item_id=ident, date="2026-08-09", expected_revision=edited["revision"]))
        self.assertEqual(current["date"], "2026-08-09")
        self.assertEqual(current["date_basis"], "manual")
        self.assertEqual(current["created"], original["created"])
        self.assertEqual(self.request("/api/date", dict(item_id=ident, date="invalid", expected_revision=current["revision"]))[0], 400)
        self.post("/api/starred", dict(item_id=ident, starred=True))
        self.assertTrue(self.tool("get_item", item_id=ident)["starred"])
        self.assertEqual([ident], [x["id"] for x in self.get("/api/items?starred=1")])
        tag = self.post("/api/tag", {"name": "Architecture", "category": "topic"})["id"]
        exact({"changed": 1}, self.post("/api/apply", {"item_ids": [ident], "tag_id": tag}))
        self.assertIn(tag, [t["id"] for t in self.get("/api/item/" + ident)["tags"]])
        exact({"changed": 0}, self.tool("apply_tag", item_ids=[ident], tag_id=tag))
        exact({"changed": 1}, self.post("/api/undo", {}))
        self.assertNotIn(tag, [t["id"] for t in self.get("/api/item/" + ident)["tags"]])
        exact({"changed": 0}, self.post("/api/undo", {}))
        with self.assertRaises(ToolError):
            self.tool("apply_tag", item_ids=[ident], tag_id="missing")
        # Request counters may differ by engine, but unchanged reads stay stable.
        first = self.get("/api/changes")
        exact(first, self.get("/api/changes"))
        current = self.get("/api/item/" + ident)
        self.tool("update_summary", item_id=ident, expected_revision=current["revision"], summary="Final summary")
        self.assertNotEqual(first, self.get("/api/changes"))

    def test_google_identity_normalization_and_duplicate_quiet(self):
        ident = self.import_item("google", doc_urls=[
            "https://docs.google.com/document/u/1/d/contract-google/edit?tab=x&resourcekey=abc#heading=h.test"])
        original = self.get("/api/item/" + ident)
        self.assertEqual(original["metadata"]["doc_urls"],
                         ["https://docs.google.com/document/d/contract-google/edit?resourcekey=abc"])
        before = self.tool("list_activity")
        duplicate = self.tool("import_item", title="Do not replace", summary="Discard",
                              original_text="Discard", source_key="another-key", kind="document",
                              doc_urls=["https://docs.google.com/document/d/contract-google/edit?tab=other"])
        self.assertTrue(duplicate["duplicate"])
        self.assertEqual(duplicate["id"], ident)
        after = self.get("/api/item/" + ident)
        self.assertEqual(after["summary"], original["summary"])
        self.assertEqual(after["source_hash"], original["source_hash"])
        exact(before, self.tool("list_activity"))
        self.assertEqual(len(self.get("/api/items?kind=document")), 1)

    def test_search_modes_ranking_prefixes_phrases_filters_and_unicode(self):
        title = self.import_item("title", title="Database Tenant Isolation isolation", date="2026-08-01",
                                 summary="Title match")
        content = self.import_item("content", title="Promotion proposal", date="2026-10-01",
                                   summary="Database Tenant Isolation evidence")
        partial = self.import_item("partial", title="Database Tenant planning", date="2026-09-01",
                                   summary="Isolation")
        query = urlencode({"q": "Database Tenant Isolation", "search_scope": "title"})
        self.assertEqual([title], [x["id"] for x in self.get("/api/items?" + query)])
        full = self.get("/api/items?" + urlencode({"q": "Database Tenant Isolation", "search_scope": "full_text"}))
        self.assertEqual([title, partial, content], [x["id"] for x in full])
        self.assertTrue(full[-1].get("search_excerpt"))
        self.assertEqual([title, partial, content],
                         [x["id"] for x in self.tool("search_items", query="Database Tenant Isolation")])
        phrase = self.get("/api/items?" + urlencode({"q": '"Tenant Isolation"', "search_scope": "title"}))
        self.assertEqual([title], [x["id"] for x in phrase])
        self.assertEqual([], self.get("/api/items?" + urlencode({"q": '"Isolation Tenant"', "search_scope": "full_text"})))
        self.assertEqual([title], [x["id"] for x in self.get("/api/items?" + urlencode({"q": "tena isola", "search_scope": "title"}))])
        unicode_id = self.import_item("unicode", title="Café Résumé 東京", original_text="Naïve façade")
        self.assertEqual([unicode_id], [x["id"] for x in self.get("/api/items?" + urlencode({"q": "cafe resum", "search_scope": "title"}))])
        self.assertEqual([unicode_id], [x["id"] for x in self.get("/api/items?" + urlencode({"q": "東京", "search_scope": "title"}))])
        self.tool("set_starred", item_id=content, starred=True)
        self.assertEqual([title, partial, content], [x["id"] for x in self.get("/api/items?" + urlencode({"q": "Database Tenant Isolation", "search_scope": "full_text"}))])
        self.assertEqual([content], [x["id"] for x in self.get("/api/items?" + urlencode({"q": "Database", "search_scope": "full_text", "starred": "1"}))])
        self.assertEqual([], self.get("/api/items?" + urlencode({"q": "Database", "kind": "meeting"})))

    def test_endpoint_backup_reopen_fixture(self):
        ident = self.import_item("backup")
        before = self.get("/api/item/" + ident)
        backup = self.post("/api/backup", {})["path"]
        self.tool("update_summary", item_id=ident, expected_revision=before["revision"], summary="After backup")
        self.service.restore_backup(backup)
        exact(before, self.get("/api/item/" + ident))
        self.assertEqual(self.request("/api/source/" + ident)[2],
                         "Complete café 東京 transcript.\r\nSecond line.\n".encode())
        with self.assertRaises(ValueError):
            self.service.restore_backup("/tmp")

    def test_serial_revision_race_one_winner(self):
        ident = self.import_item()
        original = self.get("/api/item/" + ident)
        before = self.tool("list_activity")["events"]
        def edit(index):
            return self.service.request("/api/summary", {"item_id": ident, "expected_revision": original["revision"],
                                                         "summary": "Winner " + str(index)})
        with ThreadPoolExecutor(max_workers=2) as pool:
            responses = list(pool.map(edit, [1, 2]))
        self.assertEqual([200, 400], sorted(x[0] for x in responses))
        winner = json.loads(next(x[2] for x in responses if x[0] == 200))
        exact(winner, self.get("/api/item/" + ident))
        self.assertEqual(winner["revision"], original["revision"] + 1)
        self.assertEqual(len(self.tool("list_activity")["events"]), len(before) + 1)
        self.restart()
        exact(winner, self.get("/api/item/" + ident))
