"""Go deletion/restore interruption contracts over endpoints, synthetic only."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import json
import os
import sys
import time
import unittest
from harness import ROOT, Service, exact
from runner import EvidenceResult
import harness

GO = None

class TrashRecoveryContracts(unittest.TestCase):
    def service(self,command):
        service=Service(command=command,scenario=self.id())
        self.addCleanup(service.close)
        return service

    def seed(self,s):
        target=s.tool("import_item",title="Trash recovery target",kind="document",
            summary="Target preserved summary",original_text="Original source café\r\n東京",
            source_key="google-doc:trash-recovery",
            doc_urls=["https://docs.google.com/document/d/trash-recovery/edit"])["id"]
        neighbors=[]
        for number in range(2):
            ident=s.tool("import_item",title="Neighbor "+str(number),kind="note",
                summary="Keep [[Trash recovery target|labeled reference]] and context "+str(number),
                original_text="Neighbor source "+str(number),source_key="synthetic:neighbor:"+str(number))["id"]
            neighbors.append(ident)
        return target,neighbors

    def item(self,s,ident):
        return s.tool("get_item",item_id=ident)

    def snapshot(self,s,target,neighbors):
        return dict(items=[self.item(s,n) for n in neighbors],
                    target=s.get("/api/items?kind=document"),
                    trash=s.tool("list_trash"),
                    events=s.tool("list_activity"),
                    sources=[s.request("/api/source/"+n)[2] for n in neighbors],
                    history=[s.get("/api/history/"+n) for n in neighbors])

    def assert_rollback(self,s,target,neighbors,before):
        after=self.snapshot(s,target,neighbors)
        exact({k:v for k,v in before.items() if k!="history"},
              {k:v for k,v in after.items() if k!="history"})
        # Revision snapshots precede SQLite commit before commit. A failed
        # operation may retain an additional exact copy of its old summary;
        # existing history must remain unchanged and no attempted text may leak.
        for index,(old,new) in enumerate(zip(before["history"],after["history"])):
            old_entries={e["id"]:e for e in old["entries"]}
            new_entries={e["id"]:e for e in new["entries"]}
            for ident,entry in old_entries.items():
                exact(entry,new_entries[ident])
            for ident,entry in new_entries.items():
                if ident not in old_entries:
                    exact("summary",entry["kind"])
                    exact(before["items"][index]["summary"].encode(),
                          s.request("/api/history/"+neighbors[index]+"/"+ident)[2])
        return after

    def barrier(self,s,operation,when):
        folder=s.library/".service"/"contract-fault"
        for name in ("ready","release"):
            (folder/name).unlink(missing_ok=True)
        s.restart(env_overrides={"ATLAS_CONTRACT_FAULT":json.dumps(
            {"point":operation+"."+when,"action":"pause"})})
        return folder

    def interrupt(self,s,target,operation,when):
        revision=self.item(s,target)["revision"] if operation=="delete" else None
        folder=self.barrier(s,operation,when)
        with ThreadPoolExecutor(max_workers=1) as pool:
            kwargs={"item_id":target}
            if revision is not None:
                kwargs["expected_revision"]=revision
            future=pool.submit(s.tool,"delete_item" if operation=="delete" else "restore_item",**kwargs)
            deadline=time.monotonic()+10
            while time.monotonic()<deadline and not (folder/"ready").is_file():
                if s.process.poll() is not None:
                    self.fail("Service exited before trash barrier")
                time.sleep(.01)
            self.assertTrue((folder/"ready").is_file(),"Trash fault barrier was not reached")
            s.process.kill()
            s.process.wait(timeout=5)
            with self.assertRaises(Exception):
                future.result(timeout=5)

    def test_delete_before_commit_rolls_back_links_and_tombstones_after_restart(self):
        for writer,command in (("go",GO),):
            for reader,reopen in (("go",GO),):
                with self.subTest(writer=writer,reader=reader):
                    s=self.service(command);target,neighbors=self.seed(s)
                    before=self.snapshot(s,target,neighbors)
                    self.interrupt(s,target,"delete","before_commit")
                    s.restart(command=reopen,env_overrides={})
                    stable=self.assert_rollback(s,target,neighbors,before)
                    for _ in range(2):
                        s.restart()
                        exact(stable,self.snapshot(s,target,neighbors))
                    duplicate=s.tool("import_item",title="Ignored",summary="Ignored",kind="document",
                        doc_urls=["https://docs.google.com/document/d/trash-recovery/edit"])
                    self.assertTrue(duplicate["duplicate"])
                    self.assertEqual(duplicate["id"],target)
                    s.tool("delete_item",item_id=target,expected_revision=self.item(s,target)["revision"])
                    self.assertEqual([x["id"] for x in s.tool("list_trash")],[target])

    def test_restore_before_commit_keeps_trash_and_cleaned_links_after_restart(self):
        for writer,command in (("go",GO),):
            for reader,reopen in (("go",GO),):
                with self.subTest(writer=writer,reader=reader):
                    s=self.service(command);target,neighbors=self.seed(s)
                    s.tool("delete_item",item_id=target,expected_revision=self.item(s,target)["revision"])
                    before=self.snapshot(s,target,neighbors)
                    self.interrupt(s,target,"restore","before_commit")
                    s.restart(command=reopen,env_overrides={})
                    stable=self.assert_rollback(s,target,neighbors,before)
                    for _ in range(2):
                        s.restart()
                        exact(stable,self.snapshot(s,target,neighbors))
                    s.tool("restore_item",item_id=target)
                    for n in neighbors:
                        self.assertIn("[[Trash recovery target|labeled reference]]",self.item(s,n)["summary"])
                    self.assertEqual(s.tool("list_trash"),[])

    def test_delete_after_commit_preserves_complete_cleanup_after_restart(self):
        for reader,reopen in (("go",GO),):
            with self.subTest(reader=reader):
                s=self.service(GO);target,neighbors=self.seed(s)
                self.interrupt(s,target,"delete","after_commit")
                s.restart(command=reopen,env_overrides={})
                self.assertEqual([x["id"] for x in s.tool("list_trash")],[target])
                for n in neighbors:
                    text=self.item(s,n)["summary"]
                    self.assertNotIn("[[",text)
                    self.assertIn("labeled reference",text)
                stable=self.snapshot(s,target,neighbors)
                s.restart()
                exact(stable,self.snapshot(s,target,neighbors))
                s.tool("restore_item",item_id=target)
                for n in neighbors:
                    self.assertIn("[[Trash recovery target|labeled reference]]",self.item(s,n)["summary"])

    def test_restore_after_commit_preserves_links_and_explicit_restore_after_restart(self):
        for reader,reopen in (("go",GO),):
            with self.subTest(reader=reader):
                s=self.service(GO);target,neighbors=self.seed(s)
                s.tool("delete_item",item_id=target,expected_revision=self.item(s,target)["revision"])
                self.interrupt(s,target,"restore","after_commit")
                s.restart(command=reopen,env_overrides={})
                self.assertEqual(s.tool("list_trash"),[])
                for n in neighbors:
                    self.assertIn("[[Trash recovery target|labeled reference]]",self.item(s,n)["summary"])
                stable=self.snapshot(s,target,neighbors)
                s.restart()
                exact(stable,self.snapshot(s,target,neighbors))

def main():
    global GO
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-command",required=True)
    args=parser.parse_args()
    GO=json.loads(args.go_command)
    if not isinstance(GO,list) or not GO or not all(isinstance(v,str) for v in GO):
        parser.error("Go command must be a nonempty JSON argv")
    result=unittest.TextTestRunner(verbosity=2,resultclass=EvidenceResult).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(TrashRecoveryContracts))
    passed=(result.testsRun>0 and result.wasSuccessful() and not result.skipped
            and all(r["status"]=="pass" for r in result.outcomes))
    stamp=dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    path=ROOT/"service/.work/reports"/("trash-recovery-"+stamp+".json")
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(dict(passed=passed,tests_run=result.testsRun,
        test_outcomes=result.outcomes,observed_requests=harness.OBSERVED,
        scope="Synthetic process interruption; no power-loss durability claim"),indent=2)+"\n")
    print("Trash recovery report:",path)
    return 0 if passed else 1

if __name__=="__main__":
    sys.exit(main())
