"""Installed Python 3.9 date contract: extended calendar form only."""
from harness import EndpointCase, exact


class CoreDateContracts(EndpointCase):
    def test_invalid_calendar_forms_never_mutate_records_or_activity(self):
        payload = {
            "title": "Date contract", "summary": "Summary", "kind": "document",
            "original_text": "Full original source", "source_key": "contract:date",
            "date": "2026-10-01",
        }
        ident = self.post("/api/import", payload)["id"]
        before = self.get("/api/item/" + ident)
        activity = self.tool("list_activity")
        invalid = (
            "0000-01-01", "20260101", "2026-W41-1", "2026W411",
            "2026-W41", "2026W41", "2026-2-01", "2026-02-1",
            "2023-02-29", "2026-13-01",
        )
        for date in invalid:
            code, _, _ = self.request("/api/date", {
                "item_id": ident, "date": date, "expected_revision": before["revision"],
            })
            self.assertEqual(code, 400, date)
            exact(before, self.get("/api/item/" + ident))
            exact(activity, self.tool("list_activity"))
            code, _, _ = self.request("/api/import", dict(
                payload, source_key="contract:invalid:" + date, title="Invalid " + date, date=date,
            ))
            self.assertEqual(code, 400, date)
            self.assertEqual([ident], [item["id"] for item in self.get("/api/items")])
            exact(activity, self.tool("list_activity"))

        for date in ("0001-01-01", "2024-02-29", "9999-12-31"):
            current = self.get("/api/item/" + ident)
            result = self.post("/api/date", {
                "item_id": ident, "date": date, "expected_revision": current["revision"],
            })
            self.assertEqual(result["date"], date)
            self.assertEqual(result["date_basis"], "manual")
            self.assertEqual(result["created"], before["created"])

        inferred = self.post("/api/import", {
            "title": "Invalid inferred year", "summary": "Date: 0000-01-01",
            "kind": "document", "original_text": "Date: 0000-01-01",
            "source_key": "contract:inferred-invalid-date",
        })["id"]
        self.assertEqual(self.get("/api/item/" + inferred)["date_basis"], "added")


    def test_calendar_reference_never_invents_year_zero(self):
        text = (
            "- [0000-01-01 | Placeholder](https://calendar.google.com/calendar/event?eid=zero)\n"
            "- [2026-10-01 | Valid](https://calendar.google.com/calendar/event?eid=valid)\n"
        )
        ident = self.post("/api/import", {
            "title": "Calendar references", "summary": "Summary", "kind": "document",
            "date": "2026-10-01", "original_text": text, "source_key": "contract:calendar-dates",
        })["id"]
        links = self.get("/api/item/" + ident)["source_links"]
        self.assertEqual(len(links), 2)
        self.assertEqual(links[0]["label"], "Placeholder")
        self.assertNotIn("date", links[0])
        self.assertEqual(links[1]["date"], "2026-10-01")
        self.assertEqual(links[1]["label"], "Valid")
        self.assertEqual(self.request("/api/source/" + ident)[2], text.encode())


class CoreIdentityContracts(EndpointCase):
    def test_encoded_google_paths_never_merge_with_decoded_ids(self):
        documents = (
            ("normal", "foo"),
            ("encoded-slash", "foo%2Fbar"),
            ("normal-underscore", "foo_bar"),
            ("encoded-underscore", "foo%5Fbar"),
        )
        ids = []
        for key, path_id in documents:
            url = "https://docs.google.com/document/d/" + path_id + "/edit"
            result = self.post("/api/import", {
                "title": "Identity " + key, "summary": "Source " + url,
                "kind": "document", "date": "2026-10-01",
                "original_text": "Full source " + key,
                "source_key": "contract:identity:" + key, "doc_urls": [url],
            })
            self.assertFalse(result["duplicate"])
            self.assertNotIn(result["id"], ids)
            ids.append(result["id"])
            item = self.get("/api/item/" + result["id"])
            self.assertEqual(item["metadata"]["doc_urls"], [url])
            self.assertEqual(item["summary"], "Source " + url)
            expected_key = ("contract:identity:" + key if key.startswith("encoded-")
                            else "google-doc:" + path_id)
            self.assertEqual(item["source_key"], expected_key)
            self.assertEqual(self.request("/api/source/" + result["id"])[2],
                             ("Full source " + key).encode())
        self.assertEqual(len(self.get("/api/items?kind=document")), len(documents))
        self.assertEqual(len(self.tool("list_activity")["events"]), len(documents))
        self.restart()
        self.assertEqual({item["id"] for item in self.get("/api/items?kind=document")}, set(ids))
