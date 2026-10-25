"""Google source timestamp precision follows the installed Python reference."""
from harness import EndpointCase, ToolError, exact


class SourceTimeContracts(EndpointCase):
    def test_google_timestamp_precision_rejects_before_mutation_and_accepts_reference_forms(self):
        ident = self.tool(
            "import_item", title="Time contract", summary="Summary", kind="document",
            source_key="google-doc:time-contract", original_text="Full source",
            doc_urls=["https://docs.google.com/document/d/time-contract/edit"],
        )["id"]
        item = self.tool("get_item", item_id=ident)
        events = self.tool("list_activity")
        source = {
            "file_id": "time-contract", "title": "Time contract",
            "created_time": "2026-01-01T00:00:00Z",
        }
        invalid = (
            "2026-10-01T00:00:00.1Z", "2026-10-01T00:00:00.12Z",
            "2026-10-01T00:00:00.1234Z", "2026-10-01T00:00:00.123456789Z",
            "2026-10-01T00:00:00,123Z", "2026-10-01T00:00:00+00",
            "2026-10-01T00:00:00+0000", "2026-10-01T00:00:00+00:00:01.123",
        )
        for modified in invalid:
            with self.assertRaises(ToolError, msg=modified):
                self.tool("check_document_source", item_id=ident,
                          expected_revision=item["revision"],
                          source=dict(source, modified_time=modified))
            exact(item, self.tool("get_item", item_id=ident))
            exact(events, self.tool("list_activity"))

        valid = (
            "2026-10-01T00:00:00.123Z", "2026-10-01T00:00:00.123456Z",
            "2026-10-01T00:00Z", "2026-10-01T00Z",
            "2026-10-01T00.123Z", "2026-10-01T00:00.123Z",
            "2026-10-01 00:00:00+00:00", "2026-10-01T00:00:00+00:00:01",
            "2026-10-01T00:00:00+00:00:01.123456",
        )
        for modified in valid:
            current = self.tool("get_item", item_id=ident)
            result = self.tool("check_document_source", item_id=ident,
                               expected_revision=current["revision"],
                               source=dict(source, modified_time=modified))
            self.assertTrue(result["needs_content"], modified)
            self.assertTrue(self.tool("get_item", item_id=ident)["metadata"]["refresh_requested"])
        exact(events, self.tool("list_activity"))


    def test_recent_selection_short_circuits_already_observed_source_activity(self):
        self.restart(clock="2026-10-16T12:00:00+00:00")

        def document(key, sync):
            return self.tool(
                "import_item", title="Recent " + key, summary="Summary", kind="document",
                source_key="google-doc:" + key, original_text="Complete source " + key,
                doc_urls=["https://docs.google.com/document/d/" + key + "/edit"],
                metadata={"google_sync": sync},
            )["id"]

        recent = document("created-recent", {
            "created_time": "2026-10-15T00:00:00Z", "modified_time": "not-a-time",
        })
        self.assertEqual(
            [row["id"] for row in self.tool("list_google_documents", mode="recent")],
            [recent],
        )
        matched = document("provider-matched", {
            "created_time": "not-a-time", "modified_time": "not-a-time",
        })
        selected = self.tool(
            "list_google_documents", mode="recent",
            recent_file_ids=["created-recent", "provider-matched"],
        )
        self.assertEqual({row["id"] for row in selected}, {recent, matched})
