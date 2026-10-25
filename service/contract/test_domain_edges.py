"""Gate 4 invariants for conservative role replacement and source organization."""
from harness import EndpointCase, exact


class DomainEdgeContracts(EndpointCase):
    def document(self, key="edge", **extra):
        args=dict(title="Original title",kind="document",summary="Manual source summary",
                  date="2026-01-01",original_text="First line\r\nSecond line",
                  source_key="google-doc:"+key,
                  doc_urls=["https://docs.google.com/document/d/"+key+"/edit"])
        args.update(extra)
        return self.tool("import_item",**args)["id"]

    def test_unresolved_owner_replacement_keeps_previous_verified_links(self):
        ident=self.document()
        self.tool("link_document_people",item_id=ident,
            people=[{"name":"Original Owner","role":"owner","evidence":"Observed initial ownership"}],
            ownership_status="available")
        before=self.get("/api/item/"+ident)
        replacement=[
            {"name":"Replacement Owner","role":"owner","evidence":"Observed transfer metadata"},
            {"name":"Unresolved","role":"owner","evidence":"Incomplete owner identity"}]
        result=self.tool("link_document_people",item_id=ident,people=replacement,
                         replace_roles=["owner"],ownership_status="available")
        self.assertTrue(result["changed"])
        self.assertTrue(any("Unresolved" in issue for issue in result["issues"]))
        after=self.get("/api/item/"+ident)
        exact({"Original Owner","Replacement Owner"},{p["name"] for p in after["document_people"]})
        self.assertTrue(all(p["role"]=="owner" for p in after["document_people"]))
        for field in ("summary","source_path","source_hash","authors","date","tags","starred"):
            exact(before[field],after[field])
        events=self.tool("list_activity")
        repeated=self.tool("link_document_people",item_id=ident,people=replacement,
                           replace_roles=["owner"],ownership_status="available")
        exact(False,repeated["changed"])
        exact(after,self.get("/api/item/"+ident))
        exact(events,self.tool("list_activity"))
        self.restart()
        exact(after,self.get("/api/item/"+ident))
        # Once every supplied identity resolves, replacement may remove old ownership.
        self.tool("link_document_people",item_id=ident,people=replacement[:1],
                  replace_roles=["owner"],ownership_status="available")
        exact(["Replacement Owner"],[p["name"] for p in self.get("/api/item/"+ident)["document_people"]])

    def test_same_first_name_colleagues_have_distinct_stable_one_on_one_series(self):
        sessions=[]
        for index,name in enumerate(("Avery First","Avery Second","Avery First")):
            ident=self.tool("import_item",title=name+" skip level",kind="one-on-one",
                summary="Supported discussion",date="2026-10-"+str(10+index),
                original_text="Confirmed work meeting",source_key="one-on-one:"+str(index),
                participants=[{"name":name,"email":name.replace(" ",".").lower()+"@example.com",
                               "evidence":"Confirmed full-name transcript speaker"}])["id"]
            sessions.append(self.get("/api/item/"+ident))
        first,other,newer=sessions
        self.assertNotEqual(first["participants"][0]["id"],other["participants"][0]["id"])
        self.assertNotEqual(first["series"]["id"],other["series"]["id"])
        exact(first["series"]["id"],newer["series"]["id"])
        exact("Avery ↔ John",first["series"]["title"])
        exact("Avery Second ↔ John",other["series"]["title"])
        self.restart()
        group=self.tool("get_meeting_series",series_id=first["series"]["id"])
        exact([newer["id"],first["id"]],[row["id"] for row in group["sessions"]])
        other_group=self.tool("get_meeting_series",series_id=other["series"]["id"])
        exact([other["id"]],[row["id"] for row in other_group["sessions"]])
        exact({first["id"],newer["id"]},
              {m["id"] for m in self.tool("get_person",item_id=first["participants"][0]["id"])["meetings"]})

    def test_rename_with_unchanged_content_preserves_summary_aliases_and_starred_quietness(self):
        ident=self.document(metadata={"aliases":"Earlier alias"},
                            tags=[{"name":"Important","category":"topic"}])
        self.tool("set_starred",item_id=ident,starred=True)
        source={"file_id":"edge","title":"Original title","created_time":"2026-01-01T00:00:00Z",
                "modified_time":"2026-02-01T00:00:00Z","version":"1"}
        self.tool("refresh_document",item_id=ident,
            expected_revision=self.get("/api/item/"+ident)["revision"],
            source=source,original_text="First line\r\nSecond line")
        self.tool("set_refresh_requested",item_id=ident,requested=True)
        before=self.get("/api/item/"+ident)
        events=self.tool("list_activity")["events"]
        source=dict(source,title="Renamed document",modified_time="2026-02-02T00:00:00Z")
        del source["version"]
        result=self.tool("refresh_document",item_id=ident,expected_revision=before["revision"],
            source=source,original_text="First line\nSecond line",summary="Do not replace manual summary")
        exact(False,result["content_changed"])
        after=self.get("/api/item/"+ident)
        exact("Renamed document",after["title"])
        exact("Manual source summary",after["summary"])
        exact(["Earlier alias","Original title"],after["metadata"]["aliases"])
        self.assertNotIn("version",after["metadata"]["google_sync"])
        exact(False,after["metadata"]["refresh_requested"])
        for field in ("authors","date","date_basis","tags","starred"):
            exact(before[field],after[field])
        self.assertNotEqual(before["source_hash"],after["source_hash"])
        exact(b"First line\nSecond line",self.request("/api/source/"+ident)[2])
        exact([],self.get("/api/changed-documents"))
        current_events=self.tool("list_activity")["events"]
        exact(len(events)+1,len(current_events))
        exact((ident,"refreshed","Renamed document"),
              (current_events[0]["item_id"],current_events[0]["action"],current_events[0]["title"]))
        self.restart()
        exact(after,self.get("/api/item/"+ident))
        exact([],self.get("/api/changed-documents"))


    def test_pending_owner_flags_never_promote_permissions_to_verified_ownership(self):
        ident=self.document(key="pending-owner")
        source={
            "file_id":"pending-owner","title":"Original title",
            "created_time":"2026-01-01T00:00:00Z","modified_time":"2026-02-01T00:00:00Z",
            "mime_type":"application/vnd.google-apps.document","trashed":False,
            "permissions":[
                {"type":"user","role":"owner","displayName":"Verified Owner"},
                {"type":"user","role":"owner","displayName":"Pending Camel","pendingOwner":True},
                {"type":"user","role":"owner","displayName":"Pending Snake","pending_owner":True},
                {"type":"user","role":"owner","displayName":"Not Pending Camel","pendingOwner":False},
                {"type":"user","role":"owner","displayName":"Not Pending Snake","pending_owner":False},
            ],
        }
        self.tool("record_ownership_lookup",item_id=ident,
            expected_revision=self.get("/api/item/"+ident)["revision"],source=source)
        item=self.get("/api/item/"+ident)
        expected={"Verified Owner","Not Pending Camel","Not Pending Snake"}
        exact(expected,{person["name"] for person in item["document_people"]})
        exact(expected,{person["title"] for person in self.tool("search_items",collection="people")})
        exact([],item["authors"])
        exact([],item["participants"])
        self.restart()
        exact(item,self.get("/api/item/"+ident))


    def test_obsidian_keeps_raw_attachment_bytes_and_python_replacement_text_in_search(self):
        # Approved synthetic external-input fixture; all outcomes are observed
        # through import, source/attachment and search endpoints.
        vault=self.service.library.parent/"replacement-vault"
        vault.mkdir()
        invalid_sequences=[
            bytes((0xff,0xfe)),bytes((0xe1,0x80,0x41)),bytes((0xe1,0x41,0x80)),
            bytes((0xf0,0x90,0x80)),bytes((0xed,0xa0,0x80)),bytes((0xe0,0x80,0x80)),
        ]
        for index,bad in enumerate(invalid_sequences):
            (vault/("note"+str(index)+".md")).write_text(
                "# Decode fixture "+str(index)+"\n[[caption"+str(index)+".txt]]\n")
            (vault/("caption"+str(index)+".txt")).write_bytes(
                b"bad="+bad+(" quokka"+str(index)+" tail\r\n").encode())
        result=self.tool("import_obsidian",source=str(vault))
        exact(len(invalid_sequences),result["created"])
        exact(len(invalid_sequences)*2,result["preserved_files"])
        for index,bad in enumerate(invalid_sequences):
            token="quokka"+str(index)
            rows=self.get("/api/items?q="+token+"&search_scope=full_text")
            exact(1,len(rows))
            ident=rows[0]["id"]
            exact("Decode fixture "+str(index),rows[0]["title"])
            expected="bad="+bad.decode("utf-8",errors="replace")+" "+token+" tail"
            self.assertIn(expected,rows[0]["search_excerpt"])
            status,_,body=self.request("/api/attachment/"+ident+"?target=caption"+str(index)+".txt")
            exact(200,status)
            exact(b"bad="+bad+(" "+token+" tail\r\n").encode(),body)
        self.restart()
        for index,bad in enumerate(invalid_sequences):
            rows=self.get("/api/items?q=quokka"+str(index)+"&search_scope=full_text")
            exact(1,len(rows))
            self.assertIn("bad="+bad.decode("utf-8",errors="replace")+" quokka"+str(index)+" tail",
                          rows[0]["search_excerpt"])
