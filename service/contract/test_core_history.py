"""Gate 3 history proof independent of later refresh/Trash domain features."""
import hashlib
from harness import EndpointCase, exact


class CoreHistoryContracts(EndpointCase):
    def test_summary_revisions_original_bytes_and_history_errors_survive_restart(self):
        source="Complete original café\r\n東京 😀\n"
        ident=self.tool("import_item",title="Core history",kind="document",
            summary="First summary",date="2026-01-01",original_text=source,
            original_name="original.txt",source_key="gate3:history")["id"]
        for wording in ("Second summary","Third summary"):
            item=self.get("/api/item/"+ident)
            self.tool("update_summary",item_id=ident,summary=wording,
                      expected_revision=item["revision"])
        item=self.get("/api/item/"+ident)
        exact(3,item["revision"])
        exact(hashlib.sha256(source.encode()).hexdigest(),item["source_hash"])
        entries=self.get("/api/history/"+ident)["entries"]
        exact(4,len(entries))
        exact(4,len({entry["id"] for entry in entries}))
        for entry in entries:
            self.assertRegex(entry["id"],r"^[0-9a-f]{64}$")
        summaries=[entry for entry in entries if entry["kind"]=="summary"]
        originals=[entry for entry in entries if entry["kind"]=="original"]
        exact({1,2,3},{entry["revision"] for entry in summaries})
        exact([3],[entry["revision"] for entry in summaries if entry["current"]])
        exact(1,len(originals))
        exact(True,originals[0]["current"])
        exact(None,originals[0]["revision"])
        expected={1:b"First summary",2:b"Second summary",3:b"Third summary"}
        before={}
        for entry in entries:
            status,headers,body=self.request("/api/history/"+ident+"/"+entry["id"])
            exact(200,status)
            exact("text/plain; charset=utf-8",headers["Content-Type"])
            exact(expected[entry["revision"]] if entry["kind"]=="summary" else source.encode(),body)
            before[entry["id"]]=body
        exact(403,self.request("/api/history/"+ident,headers={"Authorization":None})[0])
        for path in ("/api/history/missing","/api/history/"+ident+"/"+"0"*64,
                     "/api/history/"+ident+"/../outside","/api/history/"+ident+"/entry/extra"):
            exact(400,self.request(path)[0])
        self.restart()
        exact(item,self.get("/api/item/"+ident))
        exact(entries,self.get("/api/history/"+ident)["entries"])
        for entry in entries:
            status,headers,body=self.request("/api/history/"+ident+"/"+entry["id"])
            exact(200,status)
            exact(before[entry["id"]],body)
        status,_,body=self.request("/api/source/"+ident)
        exact(200,status)
        exact(source.encode(),body)
