"""Explicit Gate5 endpoint suite: sequential engines and fresh-user config.

Not part of common discovery: requires --go-command JSON with an absolute native
binary. Process restart/env/root setup are fixture orchestration; all library
state and preserved-byte assertions use HTTP/MCP, never SQLite/file reads.
"""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import sys
import unittest

import harness
from harness import Service, ToolError, exact
from runner import EvidenceResult

GO_COMMAND = None
PYTHON_COMMAND = [sys.executable, str(harness.ROOT / 'old/contract/python_backend.py')]
CLOCK = '2026-10-17T10:30:00+00:00'


class CrossEngineConfiguration(unittest.TestCase):
    def service(self, command, **kwargs):
        instance = Service(command=command, clock=CLOCK, scenario=self.id(), **kwargs)
        self.addCleanup(instance.close)
        return instance

    def get(self, service, path):
        status, _, body = service.request(path)
        self.assertEqual(status, 200)
        return json.loads(body)

    def item(self, service, ident):
        return service.tool('get_item', item_id=ident)

    def source(self, service, ident):
        status, _, body = service.request('/api/source/'+ident)
        self.assertEqual(status, 200)
        return body

    def metadata(self, key='cross-doc', **extra):
        value = dict(file_id=key, title='Cross engine design',
                     created_time='2026-01-01T00:00:00Z',
                     modified_time='2026-10-17T09:00:00Z',
                     mime_type='application/vnd.google-apps.document', trashed=False)
        value.update(extra)
        return value

    def submit(self, service, key, text=None):
        if text is None:
            source = dict(type='google_doc', title='Cross engine '+key,
                          url='https://docs.google.com/document/d/'+key+'/edit')
        else:
            source = dict(type='zoom', title='Cross engine '+key, date='2026-10-16',
                          source_key='zoom-transcript:'+key+'/transcript.txt',
                          original_name='transcript.txt', original_text=text)
        return service.tool('submit_sources', sources=[source])['sources'][0]

    def seed(self, service):
        project = service.tool('create_entity', name='Cross Project', kind='project')
        team = service.tool('create_entity', name='Cross Team', kind='team')
        text = 'Complete cross source café\r\n東京 😀\n'
        queued = self.submit(service, 'cross-doc')['intake_id']
        completed = service.tool('complete_intake', intake_id=queued,
            payload={'summary':'First design summary','original_text':text},
            source=self.metadata(owners=[{'display_name':'Morgan Rivera','email_address':'morgan@example.test'}]))
        self.assertEqual(completed['status'], 'imported')
        document = completed['id']
        service.tool('link_entity', item_ids=[document], entity_id=project['id'])
        service.tool('link_entity', item_ids=[document], entity_id=team['id'])
        service.tool('set_authors', item_id=document, authors=['Morgan Rivera'])
        service.tool('link_document_people', item_id=document,
            people=[{'name':'Taylor Zhang','role':'mentioned','evidence':'Explicit source mention'}])
        service.tool('set_starred', item_id=document, starred=True)
        service.tool('set_date', item_id=document, date='2026-03-04',
                     expected_revision=self.item(service,document)['revision'])
        service.tool('update_summary', item_id=document, summary='Manual design summary',
                     expected_revision=self.item(service,document)['revision'])
        service.tool('refresh_document', item_id=document,
            expected_revision=self.item(service,document)['revision'],
            source=self.metadata(modified_time='2026-10-17T10:00:00Z'),
            original_text='Updated source — preserved in full\n', summary='Updated design summary')
        change = self.get(service, '/api/changed-documents')[0]
        status, _, _ = service.request('/api/changes-seen', {'item_id':document,'through_id':change['latest_change_id']})
        self.assertEqual(status,200)
        self.assertEqual(self.get(service,'/api/changed-documents')[0]['unseen_updates'],0)
        meeting = service.tool('import_item', title='Cross skip level',kind='one-on-one',
            summary='Supported meeting summary', original_text='Full meeting source',
            source_key='cross:meeting', date='2026-10-16',
            participants=[{'name':'Morgan Rivera','email':'morgan@example.test','evidence':'Confirmed transcript speaker'}])['id']
        interview = service.tool('import_item', title='Candidate discussion',kind='meeting',
            summary='Interview evidence, no recommendation', original_text='Full candidate source',
            source_key='cross:interview', candidates=['Robin Patel'])['id']
        edition = service.tool('import_item', title='Cross feed', kind='blog', date='2026-10-17',
            blog_feed='AI', summary='Supported feed summary', source_key='cross:feed', original_text='Full feed source')['id']
        removed = service.tool('import_item', title='Removed source', kind='document',
            summary='Keep this manual summary', source_key='google-doc:cross-removed',
            doc_urls=['https://docs.google.com/document/d/cross-removed/edit'],original_text='Removed full source')['id']
        service.tool('delete_item',item_id=removed,expected_revision=self.item(service,removed)['revision'])
        self.assertEqual(self.submit(service,'cross-removed')['status'],'blocked')
        pending = self.submit(service,'pending-zoom','Full pending transcript — café\n')['intake_id']
        retry = service.tool('import_item',title='Retry source',kind='document',summary='Retry summary',
            source_key='google-doc:cross-retry', doc_urls=['https://docs.google.com/document/d/cross-retry/edit'],
            original_text='Full retry source')['id']
        failed = service.tool('record_ownership_lookup',item_id=retry,
            expected_revision=self.item(service,retry)['revision'], error='Synthetic per-item error',provider_unavailable=False)
        self.assertEqual(failed['next_attempt'],'2026-10-17T16:30:00+00:00')
        return dict(document=document,meeting=meeting,interview=interview,edition=edition,
                    removed=removed,retry=retry,pending=pending,completed=queued)

    def snapshot(self, service, ids):
        items = service.tool('search_items')
        trash = service.tool('list_trash')
        all_ids = [row['id'] for row in items]+[row['id'] for row in trash]
        records, preserved = {}, {}
        for ident in all_ids:
            records[ident] = service.tool('get_item', item_id=ident, include_deleted=True)
            # History/source endpoints intentionally reject removed records.
            if ident in {row['id'] for row in trash}:
                continue
            entries = self.get(service,'/api/history/'+ident)
            contents = {}
            for entry in entries['entries']:
                status,_,body = service.request('/api/history/'+ident+'/'+entry['id'])
                self.assertEqual(status,200)
                contents[entry['id']] = body
            status,_,source = service.request('/api/source/'+ident)
            self.assertEqual(status,200 if records[ident]['source_path'] else 400)
            preserved[ident] = {'history':entries,'contents':contents,'source':{'status':status,'body':source if status==200 else json.loads(source)}}
        entities=service.tool('list_entities')
        return dict(items=items,trash=trash,records=records,preserved=preserved,
            tags=service.tool('list_tags'),entities=entities,
            entity_detail={e['id']:service.tool('get_entity',entity_id=e['id']) for e in entities},
            people={row['id']:service.tool('get_person',item_id=row['id']) for row in items if row['kind']=='person'},
            projects={row['id']:service.tool('get_project',item_id=row['id']) for row in items if row['kind']=='project'},
            series=service.tool('list_meeting_series'),
            series_details={row['id']:service.tool('get_meeting_series',series_id=row['id'])
                            for row in service.tool('list_meeting_series')},
            feeds=service.tool('list_blog_feeds'),
            feed_details={row['id']:service.tool('get_blog_feed',feed_id=row['id'])
                          for row in service.tool('list_blog_feeds')},
            teams={row['id']:service.tool('get_team',item_id=row['id']) for row in items if row['kind']=='team'},
            changed=self.get(service,'/api/changed-documents'),
            intake=service.tool('list_intake'),
            pending=service.tool('get_intake',intake_id=ids['pending']),
            completed=service.tool('get_intake',intake_id=ids['completed']),
            ownership=service.tool('list_ownership_lookups'),
            activity=service.tool('list_activity',limit=200))

    def test_python_go_python_rich_state_and_bidirectional_writes(self):
        service = self.service(PYTHON_COMMAND,library_name='Cross engine Δ library')
        ids = self.seed(service)
        token = service.connection['token']
        before = self.snapshot(service,ids)
        service.restart(command=GO_COMMAND)
        exact(token,service.connection['token'])
        exact(before,self.snapshot(service,ids))
        self.assertEqual(self.submit(service,'cross-removed')['status'],'blocked')
        pending = service.tool('get_intake',intake_id=ids['pending'])
        service.tool('complete_intake',intake_id=ids['pending'],expected_source_hash=pending['source_hash'],
            payload={'title':'Completed on Go','kind':'meeting','summary':'Go completion summary'})
        document=ids['document']
        service.tool('update_summary',item_id=document,summary='Edited on Go',
                     expected_revision=self.item(service,document)['revision'])
        service.tool('restore_item',item_id=ids['removed'])
        service.tool('delete_item',item_id=ids['removed'],expected_revision=self.item(service,ids['removed'])['revision'])
        service.restart(clock='2026-10-17T16:30:00+00:00')
        due=service.tool('list_ownership_lookups')['documents']
        retry=next(row for row in due if row['id']==ids['retry'])
        self.assertEqual(retry['attempts'],1)
        failed=service.tool('record_ownership_lookup',item_id=ids['retry'],
            expected_revision=self.item(service,ids['retry'])['revision'],
            error='Synthetic provider unavailable',provider_unavailable=True)
        # Provider's first failure pauses globally for six hours even though this
        # document's second attempt has a separate one-day retry schedule.
        self.assertEqual(failed['next_attempt'],'2026-10-17T22:30:00+00:00')
        self.assertEqual(service.tool('list_ownership_lookups')['paused_until'],failed['next_attempt'])
        before = self.snapshot(service,ids)
        service.restart(command=PYTHON_COMMAND)
        exact(token,service.connection['token'])
        exact(before,self.snapshot(service,ids))
        self.assertEqual(self.submit(service,'cross-removed')['status'],'blocked')
        service.tool('update_summary',item_id=document,summary='Edited after Python reopen',
                     expected_revision=self.item(service,document)['revision'])
        service.tool('record_ownership_lookup',item_id=ids['retry'],
            expected_revision=self.item(service,ids['retry'])['revision'],
            source=self.metadata('cross-retry',owners=[]))
        self.assertEqual(self.item(service,ids['retry'])['metadata']['document_people']['ownership_status'],'no_individual_owner')
        before = self.snapshot(service,ids)
        service.restart(command=GO_COMMAND)
        exact(before,self.snapshot(service,ids))
        self.assertEqual(self.item(service,document)['summary'],'Edited after Python reopen')
        self.assertEqual(self.source(service,document),b'Updated source \xe2\x80\x94 preserved in full\n')
        self.assertEqual(self.item(service,document)['date_basis'],'manual')

    def configured_command(self,owner,timezone):
        args = list(GO_COMMAND)
        # Last occurrence wins for Go flags, including caller-provided fixture owner.
        return args+['--owner-name',owner,'--timezone',timezone]

    def test_new_owner_timezone_unicode_root_and_credentials_restart(self):
        service=self.service(self.configured_command('Kai Martínez','Pacific/Auckland'),
                             library_name='Atlas 新しい library with spaces')
        doc=service.tool('import_item',title='Fresh personal document',kind='document',
            summary='New user summary',original_text='Full 東京 source',source_key='new-user:doc')['id']
        self.assertEqual(self.item(service,doc)['date'],'2026-10-17')
        # 10:30 UTC is 23:30 in Auckland on Oct17; LA is Oct17 03:30.
        service.restart(clock='2026-10-17T12:30:00+00:00')
        nextdoc=service.tool('import_item',title='After local midnight',kind='document',
            summary='Next local date',original_text='Next full source',source_key='new-user:next')['id']
        self.assertEqual(self.item(service,nextdoc)['date'],'2026-10-18')
        meeting=service.tool('import_item',title='Unspecified skip level',kind='one-on-one',summary='Meeting summary',
            original_text='Meeting complete source',source_key='new-user:meeting',participants=[
                {'name':'Kai Martínez','evidence':'Configured owner speaker'},
                {'name':'Avery Chen','evidence':'Confirmed colleague speaker'}])['id']
        self.assertEqual(self.item(service,meeting)['series']['title'],'Avery ↔ Kai')
        self.assertNotIn('John',json.dumps(self.item(service,meeting),ensure_ascii=False))
        original=self.item(service,doc)
        token=service.connection['token']
        service.restart()
        exact(original,self.item(service,doc))
        exact(token,service.connection['token'])
        self.assertEqual(self.source(service,doc),'Full 東京 source'.encode())
        self.assertEqual(service.request('/api/items',headers={'Authorization':'Bearer wrong-token'})[0],403)
        self.assertIn('Atlas 新しい library with spaces',self.get(service,'/health')['library'])

    def test_empty_owner_never_invents_pair_identity(self):
        service=self.service(self.configured_command('','UTC'))
        meeting=service.tool('import_item',title='Avery skip level',kind='one-on-one',summary='Supported summary',
            source_key='empty-owner:meeting',original_text='Complete transcript',
            participants=[{'name':'Avery Chen','evidence':'Confirmed full-name speaker'}])['id']
        self.assertFalse(self.item(service,meeting)['series'])
        self.assertEqual(service.tool('list_meeting_series'),[])
        self.assertNotIn('John',json.dumps(self.item(service,meeting)))

    def test_native_go_substantial_workflow_with_empty_PATH(self):
        service=self.service(GO_COMMAND,env_overrides={'PATH':''},library_name='Native only Δ')
        ids=self.seed(service)
        before=self.snapshot(service,ids)
        backup_status,_,body=service.request('/api/backup',{})
        self.assertEqual(backup_status,200)
        self.assertIn('path',json.loads(body))
        service.restart()
        exact(before,self.snapshot(service,ids))
        self.assertEqual(self.submit(service,'cross-removed')['status'],'blocked')
        self.assertEqual(self.item(service,ids['document'])['source_hash'],
                         hashlib.sha256(b'Updated source \xe2\x80\x94 preserved in full\n').hexdigest())


def main():
    global GO_COMMAND
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go-command',required=True,help='JSON argv for native Go binary plus contract flags')
    args=parser.parse_args()
    GO_COMMAND=json.loads(args.go_command)
    if (not isinstance(GO_COMMAND,list) or not GO_COMMAND or not all(isinstance(x,str) for x in GO_COMMAND)
            or not Path(GO_COMMAND[0]).is_absolute()):
        parser.error('go-command requires JSON string argv with an absolute executable path')
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(CrossEngineConfiguration)
    result=unittest.TextTestRunner(verbosity=2,resultclass=EvidenceResult).run(suite)
    folder=harness.ROOT/'service/.work/reports'
    folder.mkdir(parents=True,exist_ok=True)
    report={'scope':'Gate5 cross-engine/config endpoint assertions only; no crash/fault/legacy migration claim',
            'go_command':GO_COMMAND,'tests_run':result.testsRun,'test_outcomes':result.outcomes,
            'passed':bool(result.testsRun and result.wasSuccessful() and not result.skipped),
            'observed_requests':harness.OBSERVED}
    path=folder/('gate5-crossengine-'+dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')+'.json')
    path.write_text(json.dumps(report,indent=2)+'\n')
    print('Gate5 endpoint report:',path)
    return 0 if report['passed'] else 1


if __name__=='__main__':
    raise SystemExit(main())
