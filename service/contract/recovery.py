"""Go process/fault suite; all application outcomes use endpoints."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import time
import unittest

from harness import ROOT, Service, exact
from runner import EvidenceResult
import harness

GO = None

class RecoveryTests(unittest.TestCase):
    def service(self, command):
        service = Service(command=command, scenario=self.id())
        self.addCleanup(service.close)
        return service

    def create(self, service):
        return service.tool('import_item', title='Recovery document', summary='Old summary',
            kind='document', original_text='Old source café\r\n東京', source_key='google-doc:recovery',
            doc_urls=['https://docs.google.com/document/d/recovery/edit'],
            authors=['Ada Example'], projects=['Recovery project'])['id']

    def current(self, service, ident):
        return service.tool('get_item', item_id=ident)

    def arm(self, service, point, action):
        folder=service.library/'.service'/'contract-fault'
        for name in ('ready','release'):
            (folder/name).unlink(missing_ok=True)
        service.restart(env_overrides={'ATLAS_CONTRACT_FAULT':json.dumps(dict(point=point,action=action))})
        return folder

    def wait(self, folder, process):
        deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            if (folder/'ready').is_file():
                return
            if process.poll() is not None:
                self.fail('Service exited before barrier')
            time.sleep(.01)
        self.fail('Fault barrier not reached')

    def refresh(self, service, ident, revision):
        return service.tool('refresh_document',item_id=ident, expected_revision=revision,
            source=dict(file_id='recovery',title='Recovery document',created_time='2026-01-01T00:00:00Z',
                        modified_time='2026-02-01T00:00:00Z'),
            original_text='New complete source 東京',summary='New source-grounded summary')

    def test_failed_summary_write_preserves_state_and_retry(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s)
                before=self.current(s,ident)
                events=s.tool('list_activity')
                self.arm(s,'summary.before_write','fail')
                response=s.request('/api/summary',dict(item_id=ident,expected_revision=before['revision'],summary='Rejected'))
                self.assertEqual(response[0],400)
                exact(before,self.current(s,ident))
                exact(events,s.tool('list_activity'))
                self.assertEqual(s.request('/api/source/'+ident)[2],'Old source café\r\n東京'.encode())
                s.restart(env_overrides={})
                exact(before,self.current(s,ident))
                result=s.tool('update_summary',item_id=ident,expected_revision=before['revision'],summary='Retry succeeds')
                self.assertEqual(result['summary'],'Retry succeeds')

    def test_failed_refresh_commit_preserves_request_source_summary_and_retry(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s)
                s.tool('set_refresh_requested',item_id=ident,requested=True)
                before=self.current(s,ident); events=s.tool('list_activity')
                self.arm(s,'refresh.before_commit','fail')
                with self.assertRaises(harness.ToolError):
                    self.refresh(s,ident,before['revision'])
                exact(before,self.current(s,ident))
                exact(events,s.tool('list_activity'))
                self.assertEqual(s.request('/api/source/'+ident)[2],'Old source café\r\n東京'.encode())
                s.restart(env_overrides={})
                exact(before,self.current(s,ident))
                self.refresh(s,ident,before['revision'])
                after=self.current(s,ident)
                self.assertFalse(after['metadata']['refresh_requested'])
                self.assertEqual(after['summary'],'New source-grounded summary')

    def test_summary_kill_before_commit_retains_source_and_safe_retry(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s); before=self.current(s,ident)
                folder=self.arm(s,'summary.before_commit','pause')
                with ThreadPoolExecutor(max_workers=1) as pool:
                    future=pool.submit(s.request,'/api/summary',dict(item_id=ident,
                        expected_revision=before['revision'],summary='Unacknowledged new summary'))
                    self.wait(folder,s.process); s.process.kill(); s.process.wait(timeout=5)
                    with self.assertRaises(Exception): future.result(timeout=5)
                s.restart(env_overrides={})
                after=self.current(s,ident)
                # Markdown is authoritative: summary-only interruption may recover old or new.
                self.assertIn(after['summary'],('Old summary','Unacknowledged new summary'))
                self.assertEqual(after['source_hash'],before['source_hash'])
                self.assertEqual(after['authors'],before['authors'])
                self.assertEqual(after['projects'],before['projects'])
                self.assertEqual(s.request('/api/source/'+ident)[2],'Old source café\r\n東京'.encode())
                s.tool('update_summary',item_id=ident,expected_revision=after['revision'],summary='Recovered retry')
                s.restart()
                self.assertEqual(self.current(s,ident)['summary'],'Recovered retry')

    def test_refresh_kill_before_commit_rolls_back_consistently_after_restart(self):
        for writer, command in [('go',GO)]:
            for reader, reopen in [('go',GO)]:
                with self.subTest(writer=writer,reader=reader):
                    s=self.service(command); ident=self.create(s)
                    s.tool('set_refresh_requested',item_id=ident,requested=True)
                    before=self.current(s,ident)
                    folder=self.arm(s,'refresh.before_commit','pause')
                    with ThreadPoolExecutor(max_workers=1) as pool:
                        future=pool.submit(self.refresh,s,ident,before['revision'])
                        self.wait(folder,s.process); s.process.kill(); s.process.wait(timeout=5)
                        with self.assertRaises(Exception): future.result(timeout=5)
                    s.restart(command=reopen,env_overrides={})
                    exact(before,self.current(s,ident))
                    self.assertEqual(s.request('/api/source/'+ident)[2],'Old source café\r\n東京'.encode())
                    # No new source/summary text may leak into search after rollback.
                    self.assertEqual(s.get('/api/items?q=grounded&search_scope=full_text'),[])
                    result=self.refresh(s,ident,before['revision'])
                    self.assertEqual(result['id'],ident)
                    after=self.current(s,ident)
                    self.assertFalse(after['metadata']['refresh_requested'])
                    self.assertEqual(after['summary'],'New source-grounded summary')
                    self.assertEqual(after['source_hash'],hashlib.sha256('New complete source 東京'.encode()).hexdigest())
                    s.restart()
                    exact(after,self.current(s,ident))

    def test_refresh_kill_after_commit_preserves_complete_new_snapshot_after_go_restart(self):
        for reader, reopen in [('go',GO)]:
            with self.subTest(reader=reader):
                s=self.service(GO); ident=self.create(s); before=self.current(s,ident)
                folder=self.arm(s,'refresh.after_commit','pause')
                with ThreadPoolExecutor(max_workers=1) as pool:
                    future=pool.submit(self.refresh,s,ident,before['revision'])
                    self.wait(folder,s.process); s.process.kill(); s.process.wait(timeout=5)
                    with self.assertRaises(Exception): future.result(timeout=5)
                s.restart(command=reopen,env_overrides={})
                after=self.current(s,ident)
                self.assertEqual(after['summary'],'New source-grounded summary')
                self.assertEqual(after['revision'],before['revision']+1)
                self.assertFalse(after['metadata']['refresh_requested'])
                self.assertEqual(after['metadata']['google_sync']['modified_time'],'2026-02-01T00:00:00Z')
                self.assertEqual(s.request('/api/source/'+ident)[2],'New complete source 東京'.encode())
                self.assertEqual(after['authors'],before['authors'])
                self.assertEqual(after['projects'],before['projects'])
                self.assertTrue(s.tool('import_item',title='Duplicate',summary='Discard',kind='document',
                    doc_urls=['https://docs.google.com/document/d/recovery/edit'])['duplicate'])
                exact(after,self.current(s,ident))

    def test_obsidian_snapshot_is_used_if_live_files_change_after_verification(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command)
                vault=s.library.parent/'synthetic-vault'; vault.mkdir()
                note='# Snapshot stable title\n[[captions.txt]]\nStable note quokka\n'
                captions='Stable attachment wombat café'
                (vault/'note.md').write_text(note,encoding='utf-8')
                (vault/'captions.txt').write_text(captions,encoding='utf-8')
                folder=self.arm(s,'obsidian.snapshot_verified','pause')
                with ThreadPoolExecutor(max_workers=1) as pool:
                    future=pool.submit(s.tool,'import_obsidian',source=str(vault))
                    self.wait(folder,s.process)
                    (vault/'note.md').write_text('# Changed live title\nLiveonly zebra',encoding='utf-8')
                    (vault/'captions.txt').write_text('Liveonly zebra attachment',encoding='utf-8')
                    (folder/'release').write_text('release')
                    result=future.result(timeout=10)
                self.assertEqual(result['created'],1)
                rows=s.get('/api/items?q=quokka&search_scope=full_text')
                self.assertEqual(len(rows),1)
                ident=rows[0]['id']
                self.assertEqual(self.current(s,ident)['title'],'Snapshot stable title')
                self.assertEqual(s.request('/api/source/'+ident)[2],note.encode())
                self.assertEqual(s.request('/api/attachment/'+ident+'?target=captions.txt')[2],captions.encode())
                self.assertEqual([r['id'] for r in s.get('/api/items?q=wombat&search_scope=full_text')],[ident])
                self.assertEqual(s.get('/api/items?q=zebra&search_scope=full_text'),[])
                s.restart(env_overrides={})
                self.assertEqual(s.request('/api/attachment/'+ident+'?target=captions.txt')[2],captions.encode())

    def test_stopped_items_symlink_cannot_redirect_recovery_writes(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s); before=self.current(s,ident)
                s.stop()
                items=s.library/'items'
                outside=s.library.parent/'outside-items'
                items.rename(outside)
                target=outside/ident
                (target/'pending-refresh.json').write_text(json.dumps(dict(version=1,
                    snapshot='google-refresh-synthetic.txt',old_summary='Old summary',new_summary='Uncommitted summary')))
                (target/'summary.md').write_text('Uncommitted summary')
                (target/'originals'/'google-refresh-synthetic.txt').write_text('Uncommitted source')
                # Fixture safety assertion: failed startup must not mutate outside bytes.
                saved={str(p.relative_to(outside)):p.read_bytes() for p in outside.rglob('*') if p.is_file()}
                items.symlink_to(outside,target_is_directory=True)
                with self.assertRaises(RuntimeError): s.start()
                self.assertEqual(saved,{str(p.relative_to(outside)):p.read_bytes() for p in outside.rglob('*') if p.is_file()})
                items.unlink(); outside.rename(items)
                s.start()
                exact(before,self.current(s,ident))

    def test_manual_summary_conflict_fails_closed_and_can_be_resolved(self):
        for name, command in [('go',GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s); before=self.current(s,ident)
                s.stop()
                folder=s.library/'items'/ident
                intent=folder/'pending-refresh.json'
                intent.write_text(json.dumps(dict(version=1,snapshot='google-refresh-synthetic.txt',
                    old_summary='Old summary',new_summary='Uncommitted summary')))
                summary=folder/'summary.md'
                summary.write_text('Third manual text')
                with self.assertRaises(RuntimeError): s.start()
                # Fixture-only safety assertions, no direct database reads.
                self.assertEqual(summary.read_text(),'Third manual text')
                self.assertTrue(intent.is_file())
                summary.write_text('Uncommitted summary')
                s.start()
                exact(before,self.current(s,ident))
                self.assertEqual(s.request('/api/source/'+ident)[2],'Old source café\r\n東京'.encode())

    def test_inprocess_recovery_conflict_blocks_followup_reads_and_writes_until_restart(self):
        for name, command, reopen in [('go',GO,GO)]:
            with self.subTest(engine=name):
                s=self.service(command); ident=self.create(s); before=self.current(s,ident)
                folder=self.arm(s,'refresh.before_commit','pause_fail')
                with ThreadPoolExecutor(max_workers=1) as pool:
                    future=pool.submit(self.refresh,s,ident,before['revision'])
                    self.wait(folder,s.process)
                    summary=s.library/'items'/ident/'summary.md'
                    summary.write_text('Third manual text',encoding='utf-8')
                    (folder/'release').write_text('release')
                    with self.assertRaises(harness.ToolError): future.result(timeout=5)
                intent=s.library/'items'/ident/'pending-refresh.json'
                saved_intent=intent.read_bytes()
                self.assertEqual(s.get('/health')['service'],'atlas')
                self.assertEqual(s.request('/api/item/'+ident)[0],400)
                with self.assertRaises(harness.ToolError) as error:
                    self.refresh(s,ident,before['revision'])
                self.assertIn('restart',str(error.exception))
                self.assertEqual(summary.read_text(),'Third manual text')
                self.assertEqual(intent.read_bytes(),saved_intent)
                summary.write_text('Old summary')
                # Repair does not bypass the restart-required process boundary.
                self.assertEqual(s.request('/api/item/'+ident)[0],400)
                s.restart(command=reopen,env_overrides={})
                exact(before,self.current(s,ident))
                self.refresh(s,ident,before['revision'])
                self.assertEqual(self.current(s,ident)['summary'],'New source-grounded summary')

    def test_second_process_is_rejected_and_credentials_survive(self):
        for writer, command in [('go',GO)]:
            for contender, other in [('go',GO)]:
                with self.subTest(writer=writer,contender=contender):
                    s=self.service(command); ident=self.create(s); before=self.current(s,ident)
                    token=s.connection['token']; pid=s.pid
                    result=subprocess.run(other+['--library',str(s.library),'--port','0'],capture_output=True,timeout=5)
                    self.assertNotEqual(result.returncode,0)
                    self.assertEqual(s.get('/health')['pid'],pid)
                    exact(before,self.current(s,ident))
                    self.assertEqual(s.connection['token'],token)
                    s.restart(command=other)
                    self.assertEqual(s.connection['token'],token)
                    exact(before,self.current(s,ident))

def main():
    global GO
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-command',required=True)
    args=parser.parse_args()
    GO=json.loads(args.go_command)
    if not isinstance(GO,list) or not GO or not all(isinstance(v,str) for v in GO):
        parser.error('Go command must be a nonempty JSON argv')
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(RecoveryTests)
    result=unittest.TextTestRunner(verbosity=2,resultclass=EvidenceResult).run(suite)
    passed=result.testsRun>0 and result.wasSuccessful() and not result.skipped and all(t['status']=='pass' for t in result.outcomes)
    stamp=dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    path=ROOT/'service/.work/reports'/('recovery-'+stamp+'.json')
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(dict(passed=passed,tests_run=result.testsRun,test_outcomes=result.outcomes,
        observed_requests=harness.OBSERVED,scope='Synthetic process interruption; no power-loss durability claim'),indent=2)+'\n')
    print('Recovery report:',path)
    return 0 if passed else 1

if __name__=='__main__':
    sys.exit(main())
