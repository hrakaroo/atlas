"""Unchanged source checks stay quiet without hiding pending external edits."""
from harness import EndpointCase, ToolError, exact


class QuietRefreshContracts(EndpointCase):
    def create(self):
        ident=self.tool("import_item",title="Quiet refresh",summary="Initial summary",
            kind="document",date="2026-01-01",original_text="First\r\nSecond",
            source_key="google-doc:quiet",doc_urls=["https://docs.google.com/document/d/quiet/edit"])["id"]
        self.tool("set_starred",item_id=ident,starred=True)
        return ident

    def refresh(self, ident):
        return self.tool("refresh_document",item_id=ident,
            expected_revision=self.get("/api/item/"+ident)["revision"],
            source={"file_id":"quiet","title":"Quiet refresh",
                    "created_time":"2026-01-01T00:00:00Z","modified_time":"2026-02-01T00:00:00Z"},
            original_text="First\nSecond")

    def test_line_ending_only_refresh_preserves_raw_source_without_activity_noise(self):
        ident=self.create()
        before=self.get("/api/item/"+ident)
        events=self.tool("list_activity")
        result=self.refresh(ident)
        exact(False,result["content_changed"])
        after=self.get("/api/item/"+ident)
        self.assertNotEqual(before["source_hash"],after["source_hash"])
        exact("Initial summary",after["summary"])
        exact(b"First\nSecond",self.request("/api/source/"+ident)[2])
        exact(events,self.tool("list_activity"))
        exact([],self.get("/api/changed-documents"))
        self.restart()
        exact(events,self.tool("list_activity"))
        exact([],self.get("/api/changed-documents"))

    def test_quiet_refresh_retains_pending_external_summary_activity(self):
        ident=self.create()
        events=self.tool("list_activity")["events"]
        # Approved external-summary fixture write. Its effects are only asserted
        # through ordinary source, record and Activity endpoints.
        (self.service.library/"items"/ident/"summary.md").write_text("Externally curated summary")
        item=self.get("/api/item/"+ident)
        exact("Externally curated summary",item["summary"])
        result=self.refresh(ident)
        exact(False,result["content_changed"])
        after=self.get("/api/item/"+ident)
        exact("Externally curated summary",after["summary"])
        exact(b"First\nSecond",self.request("/api/source/"+ident)[2])
        current=self.tool("list_activity")["events"]
        exact(len(events)+1,len(current))
        exact((ident,"updated"),(current[0]["item_id"],current[0]["action"]))
        exact([],self.get("/api/changed-documents"))
        self.restart()
        exact(current,self.tool("list_activity")["events"])


    def test_invalid_google_people_metadata_cannot_commit_source_or_clear_refresh_request(self):
        ident=self.create()
        self.tool("set_refresh_requested",item_id=ident,requested=True)
        before=self.get("/api/item/"+ident)
        history=self.get("/api/history/"+ident)
        events=self.tool("list_activity")
        source={"file_id":"quiet","title":"Changed title",
                "created_time":"2026-01-01T00:00:00Z","modified_time":"2026-02-01T00:00:00Z"}
        for invalid in ({"owners":"invalid"},{"owners":[123]},{"owners":None},
                        {"permissions":"invalid"},{"permissions":[123]},{"creator":"invalid"}):
            with self.assertRaises(ToolError,msg=str(invalid)):
                self.tool("refresh_document",item_id=ident,expected_revision=before["revision"],
                    source=dict(source,**invalid),original_text="Do not commit changed source",
                    summary="Do not commit changed summary")
            exact(before,self.get("/api/item/"+ident))
            exact(True,self.get("/api/item/"+ident)["metadata"]["refresh_requested"])
            exact(b"First\r\nSecond",self.request("/api/source/"+ident)[2])
            exact(history,self.get("/api/history/"+ident))
            exact(events,self.tool("list_activity"))
        self.restart()
        exact(before,self.get("/api/item/"+ident))
        exact(history,self.get("/api/history/"+ident))
