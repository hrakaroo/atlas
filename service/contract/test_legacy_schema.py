"""Historical schema migration acceptance: SQL SETUP, endpoint-only outcomes."""
import hashlib
from harness import EndpointCase, exact
from legacy_fixtures import (LegacyService, SOURCE, SUMMARY, CREATED,
                             DOCUMENT, PERSON, PROJECT, TEAM, DELETED)


class LegacySchemaContracts(EndpointCase):
    def setUp(self):
        options = {
            "test_source_state_backfill_from_metadata_observes_first_starred_update":
                {"modern_columns":True,"digest_mode":"metadata"},
            "test_source_state_backfill_raw_hash_without_search_row_observes_first_update":
                {"digest_mode":"raw"},
            "test_existing_summary_file_is_authoritative_and_reconciled_once":
                {"manual_summary":True},
            "test_missing_source_hash_does_not_invent_verified_historical_update":
                {"digest_mode":"unverified"},
            "test_existing_star_and_trash_values_survive_migration_and_reopen":
                {"modern_columns":True},
        }
        self.service = LegacyService(scenario=self.id(),fixture_options=options.get(self._testMethodName,{}))
        self.addCleanup(self.service.close)

    def test_pre_stars_deleted_columns_repair_preserves_history_source_and_identity(self):
        item = self.tool("get_item", item_id=DOCUMENT)
        exact(SUMMARY,item["summary"])
        exact(7,item["revision"])
        exact(0,item["starred"])
        self.assertIsNone(item["deleted_at"])
        exact("2025-01-01",item["date"])
        exact("manual",item["date_basis"])
        exact(CREATED,item["created"])
        exact(CREATED,item["updated"])
        exact({"keep":["one",None,7]},item["metadata"]["legacy_user_context"])
        exact(hashlib.sha256(SOURCE.encode()).hexdigest(),item["source_hash"])
        exact(SOURCE.encode(),self.request("/api/source/"+DOCUMENT)[2])
        history = self.get("/api/history/"+DOCUMENT)
        entries = {(e["kind"],e["name"]): e for e in history["entries"]}
        exact({("summary","summary.md"),("summary","3.md"),
               ("original","recording.txt"),("original","historical.txt")},set(entries))
        expected = {("summary","summary.md"):SUMMARY.encode(),
                    ("summary","3.md"):b"# Previous summary\n",
                    ("original","recording.txt"):SOURCE.encode(),
                    ("original","historical.txt"):b"Older original\r\n"}
        for key,entry in entries.items():
            exact(expected[key],self.request("/api/history/"+DOCUMENT+"/"+entry["id"])[2])
        self.assertTrue(entries[("original","recording.txt")]["current"])
        exact(3,entries[("summary","3.md")]["revision"])
        exact([],self.get("/api/changed-documents"))
        events = self.tool("list_activity")
        for _ in range(2):
            self.restart()
            exact(item,self.tool("get_item",item_id=DOCUMENT))
            exact(history,self.get("/api/history/"+DOCUMENT))
            exact(events,self.tool("list_activity"))
            exact(SOURCE.encode(),self.request("/api/source/"+DOCUMENT)[2])
        self.tool("set_starred",item_id=DOCUMENT,starred=True)
        exact(1,self.tool("get_item",item_id=DOCUMENT)["starred"])
        feeds = self.tool("list_blog_feeds")
        exact(1,len([feed for feed in feeds if feed["name"]=="AI"]))

    def test_typed_tag_migration_binds_alias_profiles_without_self_edges_or_authorship(self):
        entities = {e["id"]:e for e in self.tool("list_entities")}
        exact({"legacy-person-tag","legacy-project-tag","legacy-team-tag"},set(entities))
        for ident,profile in (("legacy-person-tag",PERSON),("legacy-project-tag",PROJECT),("legacy-team-tag",TEAM)):
            exact(profile,entities[ident]["profile_id"])
            exact(ident,entities[ident]["legacy_tag_id"])
        doc = self.tool("get_item",item_id=DOCUMENT)
        exact([],doc["authors"])
        exact([],doc["participants"])
        exact({("legacy-person-tag","related_to"),("legacy-project-tag","belongs_to")},
              {(r["id"],r["relation"]) for r in doc["relationships"]})
        person = self.tool("get_item",item_id=PERSON)
        exact({("legacy-team-tag","member_of"),("legacy-project-tag","works_on")},
              {(r["id"],r["relation"]) for r in person["relationships"]})
        exact([],self.tool("get_item",item_id=PROJECT)["relationships"])
        exact([],self.tool("get_item",item_id=TEAM)["relationships"])
        profile = self.tool("get_person",item_id=PERSON)
        exact([],profile["documents"])
        exact([DOCUMENT],[x["id"] for x in profile["related_documents"]])
        self.assertIn("legacy-topic-tag",[t["id"] for t in doc["tags"]])
        for _ in range(2):
            self.restart()
            exact(entities,{e["id"]:e for e in self.tool("list_entities")})
            exact(doc,self.tool("get_item",item_id=DOCUMENT))
        # Exercise migrated compatibility triggers through public adapters.
        exact({"changed":1},self.tool("apply_tag",item_ids=[DOCUMENT],tag_id="legacy-team-tag"))
        self.assertIn("legacy-team-tag",[r["id"] for r in self.tool("get_item",item_id=DOCUMENT)["relationships"]])
        exact({"changed":1},self.post("/api/undo",{}))
        exact(doc,self.tool("get_item",item_id=DOCUMENT))

    def refresh_changed(self, expected_count=1):
        before = self.tool("get_item",item_id=DOCUMENT)
        before = self.tool("set_starred",item_id=DOCUMENT,starred=True)
        exact([],self.get("/api/changed-documents"))
        self.tool("refresh_document",item_id=DOCUMENT,expected_revision=before["revision"],
                  source={"file_id":"legacy-doc","title":before["title"],
                          "created_time":"2025-01-01T00:00:00Z","modified_time":"2026-02-01T00:00:00Z"},
                  original_text="Changed complete legacy source",summary=SUMMARY)
        changes = self.get("/api/changed-documents")
        exact(expected_count,len(changes))
        if changes:
            exact(DOCUMENT,changes[0]["id"])
            exact(1,changes[0]["update_count"])
            exact(1,changes[0]["unseen_updates"])
        self.restart()
        exact(changes,self.get("/api/changed-documents"))

    def test_source_state_backfill_from_index_observes_first_starred_update(self):
        self.refresh_changed()

    def test_source_state_backfill_from_metadata_observes_first_starred_update(self):
        self.refresh_changed()

    def test_source_state_backfill_raw_hash_without_search_row_observes_first_update(self):
        self.refresh_changed()

    def test_existing_summary_file_is_authoritative_and_reconciled_once(self):
        item = self.tool("get_item",item_id=DOCUMENT)
        exact("# Blurb\nExternal curated summary.\n",item["summary"])
        exact(8,item["revision"])
        exact(SOURCE.encode(),self.request("/api/source/"+DOCUMENT)[2])
        history = self.get("/api/history/"+DOCUMENT)
        events = self.tool("list_activity")
        for _ in range(2):
            self.restart()
            exact(item,self.tool("get_item",item_id=DOCUMENT))
            exact(history,self.get("/api/history/"+DOCUMENT))
            exact(events,self.tool("list_activity"))

    def test_missing_source_hash_does_not_invent_verified_historical_update(self):
        exact(None,self.tool("get_item",item_id=DOCUMENT)["source_hash"])
        self.refresh_changed(expected_count=0)

    def test_existing_star_and_trash_values_survive_migration_and_reopen(self):
        item = self.tool("get_item",item_id=DOCUMENT)
        exact(1,item["starred"])
        self.assertIsNone(item["deleted_at"])
        trash = self.tool("list_trash")
        exact([DELETED],[x["id"] for x in trash])
        exact("2025-02-01T00:00:00+00:00",trash[0]["deleted_at"])
        exact(1,trash[0]["starred"])
        exact(4,trash[0]["revision"])
        exact({"curated":True},trash[0]["metadata"])
        self.assertNotIn(DELETED,[x["id"] for x in self.tool("search_items")])
        for _ in range(2):
            self.restart()
            exact(item,self.tool("get_item",item_id=DOCUMENT))
            exact(trash,self.tool("list_trash"))
