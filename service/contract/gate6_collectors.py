"""Gate6 real collector CLI acceptance; synthetic session/transcript files only.

Fixture byte constructors follow tests/test_chrome_documents.py. No parser,
Library or SQLite code is imported: outcomes are authenticated HTTP/MCP reads.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import sys
import time
import unittest
from harness import ROOT, Service, exact
import harness
from runner import EvidenceResult

BINARY=None

def command(ident,payload):
    return struct.pack("<H",len(payload)+1)+bytes([ident])+payload

def pair(ident,a,b):
    return command(ident,struct.pack("<ii",a,b))

def navigation(tab,index,url,title):
    encoded=url.encode()
    value=struct.pack("<iii",tab,index,len(encoded))+encoded
    value+=b"\0"*(-len(value)%4)
    value+=struct.pack("<i",len(title.encode("utf-16-le"))//2)+title.encode("utf-16-le")
    return command(6,struct.pack("<i",len(value))+value)

def session(rows):
    data=b"SNSS\x03\0\0\0"
    for tab,(url,title) in enumerate(rows,11):
        data+=pair(0,1,tab)+navigation(tab,0,url,title)+pair(7,tab,0)
    return data


class CollectorAcceptance(unittest.TestCase):
    def setUp(self):
        self.service=Service(command=[BINARY,"--contract-mode","--owner-name","John Smith"],
                             scenario=self.id())
        self.addCleanup(self.service.close)
        # Activation fixture ensures collector outages never open a second Library.
        (self.service.library/".service"/"enabled").write_text("synthetic contract fixture")
        self.zoom=self.service.library.parent/"synthetic-zoom"
        self.chrome=self.service.library.parent/"synthetic-chrome"
        self.zoom.mkdir()
        self.sessions=self.chrome/"Default"/"Sessions"
        self.sessions.mkdir(parents=True)
        self.session_file=self.sessions/"Session_12345"
        self.session_file.write_bytes(session([]))

    def collect(self,kind,success=True):
        argv=[sys.executable,str(ROOT/"scripts"/"collect_sources.py"),kind,
              "--library",str(self.service.library),"--zoom-root",str(self.zoom),
              "--chrome-root",str(self.chrome),"--profile","Default"]
        result=subprocess.run(argv,cwd=self.service.library.parent,
                              capture_output=True,text=True,timeout=15)
        if not success:
            self.assertNotEqual(result.returncode,0)
            return
        self.assertEqual(result.returncode,0,result.stderr)
        return json.loads(result.stdout)

    def intake(self,kind):
        return self.service.tool("list_intake",source_type=kind,limit=50)

    def test_real_chrome_collector_filters_saved_tabs_and_uses_go_intake_deduplication(self):
        self.session_file.write_bytes(session([
            ("https://docs.google.com/document/d/collector-alpha/edit?tab=t.0","Résumé 東京 - Google Docs"),
            ("https://docs.google.com/document/u/2/d/collector-alpha/edit?resourcekey=retained&tab=t.1","Résumé 東京 - Google Docs"),
            ("https://docs.google.com/document/d/collector-beta/edit","Second titled plan - Google Docs"),
            ("https://docs.google.com/document/d/collector-blank/edit","  "),
            ("https://docs.google.com/document/d/collector-untitled/edit","Untitled document - Google Docs"),
            ("https://docs.google.com/document/d/collector-copy/edit","Copy of Untitled document - Google Docs"),
            ("https://docs.google.com/spreadsheets/d/sheet/edit","Other Google type"),
            ("https://example.test/non-doc","Ordinary site"),
            ("https://docs.google.com/document/d/e/published/pub","Published doc"),
        ]))
        first=self.collect("chrome")
        exact(2,first["submitted"])
        exact([],first["issues"])
        queue=self.intake("google_doc")
        exact({"google-doc:collector-alpha","google-doc:collector-beta"},{r["source_key"] for r in queue})
        full={r["source_key"]:self.service.tool("get_intake",intake_id=r["intake_id"]) for r in queue}
        exact("Résumé 東京",full["google-doc:collector-alpha"]["title"])
        exact("https://docs.google.com/document/d/collector-alpha/edit?resourcekey=retained",
              full["google-doc:collector-alpha"]["url"])
        exact([],self.service.tool("list_activity")["events"])
        again=self.collect("chrome")
        exact(2,again["submitted"])
        exact({r["intake_id"] for r in queue},{r["intake_id"] for r in self.intake("google_doc")})
        exact([],self.service.tool("list_activity")["events"])
        queued=full["google-doc:collector-alpha"]
        result=self.service.tool("complete_intake",intake_id=queued["intake_id"],
            source={"file_id":"collector-alpha","title":"Résumé 東京",
                    "created_time":"2026-01-01T00:00:00Z","modified_time":"2026-01-02T00:00:00Z",
                    "mime_type":"application/vnd.google-apps.document","trashed":False},
            payload={"original_text":"Complete synthetic Google source","summary":"Supported collector summary"})
        exact("imported",result["status"])
        ident=result["id"]
        before=self.service.tool("get_item",item_id=ident)
        events=self.service.tool("list_activity")
        self.collect("chrome")
        exact(before,self.service.tool("get_item",item_id=ident))
        exact(events,self.service.tool("list_activity"))
        # A new saved tab during an outage must not use the pre-activation fallback.
        self.service.stop()
        self.session_file.write_bytes(session([
            ("https://docs.google.com/document/d/offline-new/edit","Offline new doc - Google Docs")]))
        self.collect("chrome",success=False)
        self.service.start()
        self.assertNotIn("google-doc:offline-new",{r["source_key"] for r in self.intake("google_doc")})
        exact(events,self.service.tool("list_activity"))

    def test_real_zoom_collector_submits_stable_full_sources_and_repeats_quietly(self):
        folder=self.zoom/"2026-10-01 09.15.00 Synthetic planning"
        folder.mkdir()
        text="Full café 東京 😀\r\nSecond line\rThird line\n"+"complete text "*4000
        captions="UTF-16 caption café 東京\nSecond caption"
        vtt="WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nRecognizable caption wombat\n"
        sources={"meeting_transcript.txt":text,"meeting_saved_closed_caption.txt":captions,"session.vtt":vtt}
        for name,value in sources.items():
            path=folder/name
            path.write_bytes(value.encode("utf-16" if name=="meeting_saved_closed_caption.txt" else "utf-8"))
            os.utime(path,(time.time()-1800,time.time()-1800))
        for name,value in {"empty_transcript.txt":"","recent_transcript.txt":"Still changing",
                           "chat.txt":"Do not ingest","random.txt":"Unrelated",
                           "not-transcript.vtt":"No caption timing"}.items():
            path=folder/name
            path.write_text(value)
            if name!="recent_transcript.txt":
                os.utime(path,(time.time()-1800,time.time()-1800))
        first=self.collect("zoom")
        exact(3,first["submitted"])
        exact([],first["issues"])
        queue=self.intake("zoom")
        exact(3,len(queue))
        records={Path(r["source_key"]).name:self.service.tool("get_intake",intake_id=r["intake_id"]) for r in queue}
        exact(set(sources),set(records))
        for name,value in sources.items():
            record=records[name]
            exact(value,record["original_text"])
            exact(hashlib.sha256(value.encode()).hexdigest(),record["source_hash"])
            exact("zoom-transcript:"+folder.name+"/"+name,record["source_key"])
            exact("2026-10-01",record["date"])
            exact("2026-10-01T09:15:00 America/Los_Angeles",record["session_time"])
        exact([],self.service.tool("list_activity")["events"])
        self.collect("zoom")
        exact({r["intake_id"] for r in queue},{r["intake_id"] for r in self.intake("zoom")})
        record=records["meeting_transcript.txt"]
        imported=self.service.tool("complete_intake",intake_id=record["intake_id"],
            expected_source_hash=record["source_hash"],
            payload={"title":"Synthetic planning","kind":"meeting","summary":"Supported transcript summary"})
        exact("imported",imported["status"])
        ident=imported["id"]
        exact(text.encode(),self.service.request("/api/source/"+ident)[2])
        before=self.service.tool("get_item",item_id=ident)
        events=self.service.tool("list_activity")
        self.service.restart()
        self.collect("zoom")
        exact(before,self.service.tool("get_item",item_id=ident))
        exact(events,self.service.tool("list_activity"))
        exact(text.encode(),self.service.request("/api/source/"+ident)[2])


def main():
    global BINARY
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary",required=True)
    BINARY=str(Path(parser.parse_args().binary).resolve())
    result=unittest.TextTestRunner(verbosity=2,resultclass=EvidenceResult).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(CollectorAcceptance))
    passed=(result.testsRun>0 and result.wasSuccessful() and not result.skipped
            and all(r["status"]=="pass" for r in result.outcomes))
    stamp=dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    path=ROOT/"service/.work/reports"/("gate6-collectors-"+stamp+".json")
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(dict(passed=passed,tests_run=result.testsRun,
        test_outcomes=result.outcomes,observed_requests=harness.OBSERVED),indent=2)+"\n")
    print("Collector acceptance report:",path)
    return 0 if passed else 1

if __name__=="__main__":
    sys.exit(main())
