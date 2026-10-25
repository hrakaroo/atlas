"""Preservation acceptance through endpoints; filesystem operations only prepare fixtures."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import os
import subprocess
import unittest
from urllib.parse import quote
from harness import EndpointCase, ToolError, exact


class PreservationContracts(EndpointCase):
    def create(self, **extra):
        payload=dict(title='Preservation fixture',kind='document',summary='Initial summary',
                     original_text='Original café\r\n東京',original_name='fixture.txt',
                     source_key='google-doc:preservation',date='2026-01-01',
                     doc_urls=['https://docs.google.com/document/d/preservation/edit'])
        payload.update(extra)
        return self.tool('import_item',**payload)['id']

    def item(self, ident):
        return self.get('/api/item/'+ident)

    def history(self, ident):
        return self.get('/api/history/'+ident)['entries']

    def history_bytes(self, ident, entry):
        status,headers,body=self.request('/api/history/'+ident+'/'+entry['id'])
        self.assertEqual(status,200)
        self.assertEqual(headers['Content-Type'],'text/plain; charset=utf-8')
        return body

    def test_history_reads_current_and_older_full_sources_and_summaries_after_restart(self):
        ident=self.create()
        current=self.item(ident)
        self.post('/api/summary',{'item_id':ident,'summary':'Edited summary',
                                 'expected_revision':current['revision']})
        self.tool('refresh_document',item_id=ident,expected_revision=self.item(ident)['revision'],
            source=dict(file_id='preservation',title='Preservation fixture',
                        created_time='2026-01-01T00:00:00Z',modified_time='2026-02-01T00:00:00Z'),
            original_text='Updated full source\n終',summary='Updated summary')
        entries=self.history(ident)
        self.assertEqual(len(entries),5)
        self.assertEqual(len([e for e in entries if e['current']]),2)
        self.assertEqual({e['revision'] for e in entries if e['kind']=='summary'},{1,2,3})
        expected={b'Initial summary',b'Edited summary',b'Updated summary',
                  'Original café\r\n東京'.encode(),'Updated full source\n終'.encode()}
        self.assertEqual({self.history_bytes(ident,e) for e in entries},expected)
        self.restart()
        exact(entries,self.history(ident))
        self.assertEqual({self.history_bytes(ident,e) for e in entries},expected)

    def test_history_auth_unknown_ids_traversal_and_symlink_fixture(self):
        ident=self.create()
        self.assertEqual(self.request('/api/history/'+ident,headers={'Authorization':None})[0],403)
        for path in ['/api/history/missing','/api/history/'+ident+'/..',
                     '/api/history/'+ident+'/'+'0'*64,'/api/history/'+ident+'/extra/path']:
            self.assertEqual(self.request(path)[0],400)
        # Explicit Gate0 fixture-only setup exception: no application-state assertions use files.
        outside=self.service.library.parent/'outside-history.txt'
        outside.write_text('Outside fixture data')
        link=self.service.library/'items'/ident/'originals'/'unsafe.txt'
        link.symlink_to(outside)
        code,_,body=self.request('/api/history/'+ident)
        self.assertEqual(code,400)
        self.assertNotIn(b'Outside fixture data',body)
        link.unlink()
        record=self.item(ident)
        self.post('/api/delete',{'item_id':ident,'expected_revision':record['revision']})
        self.assertEqual(self.request('/api/history/'+ident)[0],400)

    def test_endpoint_backup_roundtrip_retains_sources_history_relationships_and_trash(self):
        ident=self.create(authors=['Ada Example'],projects=['Fixture Project'])
        self.tool('set_starred',item_id=ident,starred=True)
        self.tool('update_summary',item_id=ident,summary='Before backup',
                  expected_revision=self.item(ident)['revision'])
        removed=self.create(title='Removed fixture',source_key='fixture:removed',doc_urls=[])
        self.tool('delete_item',item_id=removed,expected_revision=self.item(removed)['revision'])
        before=self.item(ident)
        history=self.history(ident)
        saved={e['id']:self.history_bytes(ident,e) for e in history}
        people=self.tool('list_entities')
        trash=self.tool('list_trash')
        backup=self.post('/api/backup',{})['path']
        self.tool('update_summary',item_id=ident,summary='After backup',
                  expected_revision=before['revision'])
        self.service.reopen_backup(backup)
        exact(before,self.item(ident))
        exact(history,self.history(ident))
        exact(people,self.tool('list_entities'))
        exact(trash,self.tool('list_trash'))
        self.assertEqual({e['id']:self.history_bytes(ident,e) for e in history},saved)
        # A deleted automatic source remains blocked after backup recovery.
        self.assertEqual(self.tool('import_item',title='Removed fixture',summary='Ignored',
            original_text='Ignored',kind='document',source_key='fixture:removed').get('trashed'),True)

    def test_external_summary_reconciliation_indexes_change_through_endpoints(self):
        ident=self.create()
        before=self.item(ident)
        # Synthetic fixture-only external write; outcome read only through normal API.
        (self.service.library/'items'/ident/'summary.md').write_text('Externally edited quokka')
        self.get('/api/changes')
        after=self.item(ident)
        self.assertEqual(after['summary'],'Externally edited quokka')
        self.assertEqual(after['revision'],before['revision']+1)
        self.assertEqual([x['id'] for x in self.get('/api/items?q=quokka&search_scope=full_text')],[ident])
        self.assertEqual(self.request('/api/source/'+ident)[2],'Original café\r\n東京'.encode())

    def test_obsidian_import_and_attachment_read_do_not_require_live_sources(self):
        vault=self.service.library.parent/'synthetic-vault'
        vault.mkdir()
        note='---\ntype: meeting\ndate: 2026-01-02\n---\n# Imported fixture\n[[captions.txt]]\n'
        (vault/'note.md').write_text(note)
        (vault/'captions.txt').write_text('Synthetic transcript café')
        result=self.tool('import_obsidian',source=str(vault))
        self.assertEqual(result['created'],1)
        self.assertEqual(result['preserved_files'],2)
        rows=self.get('/api/items?q=Imported&search_scope=title')
        self.assertEqual(len(rows),1)
        ident=rows[0]['id']
        self.assertEqual(self.request('/api/source/'+ident)[2],note.encode())
        self.assertEqual(self.request('/api/attachment/'+ident+'?target=captions.txt')[2],
                         'Synthetic transcript café'.encode())
        self.assertEqual(self.request('/api/attachment/'+ident+'?target=missing.txt')[0],400)
        # Existing attachment matching accepts basenames, so never assert that
        # ../captions.txt is a new rejection contract; only outside data is forbidden.
        outside=self.service.library.parent/'outside-captions.txt'
        outside.write_text('Synthetic outside attachment secret')
        snapshot=self.item(ident)['metadata']['snapshot_root']
        for target in [str(outside),os.path.relpath(outside,snapshot)]:
            status,_,body=self.request('/api/attachment/'+ident+'?target='+quote(target,safe=''))
            self.assertEqual(status,400)
            self.assertNotIn(b'Synthetic outside attachment secret',body)

        self.assertEqual(self.tool('import_obsidian',source=str(vault))['duplicates'],1)
        with self.assertRaises(ToolError):
            self.tool('import_obsidian',source=str(vault/'missing'))
        self.restart()
        self.assertEqual(self.request('/api/attachment/'+ident+'?target=captions.txt')[2],
                         'Synthetic transcript café'.encode())

    def test_second_process_cannot_replace_connection_or_touch_existing_records(self):
        ident=self.create()
        before=self.item(ident)
        pid=self.get('/health')['pid']
        # Process lifecycle orchestration; verify effects entirely through endpoints.
        result=subprocess.run(self.service.command+['--library',str(self.service.library),'--port','0'],
            capture_output=True,timeout=5)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(self.get('/health')['pid'],pid)
        exact(before,self.item(ident))

    def test_committed_write_survives_abrupt_process_exit_and_retry_is_duplicate(self):
        ident=self.create()
        self.tool('update_summary',item_id=ident,summary='Acknowledged durable write',
                  expected_revision=self.item(ident)['revision'])
        before=self.item(ident)
        history=self.history(ident)
        self.service.process.kill()
        self.service.process.wait(timeout=5)
        self.restart()
        exact(before,self.item(ident))
        exact(history,self.history(ident))
        self.assertEqual(self.create(),ident)
        self.assertEqual(self.item(ident)['summary'],'Acknowledged durable write')

    def test_referenced_documents_and_calendars_keep_labels_dates_and_cached_identity(self):
        cached=self.create(title='Model Rollouts',source_key='google-doc:rollouts',
            doc_urls=['https://docs.google.com/document/d/rollouts/edit'])
        original=('Tracked separately by: Project Plan: Model Rollouts <https://docs.google.com/document/d/rollouts/edit?tab=t.0>\n'
                  'Missing resources <https://docs.google.com/document/d/missing/edit>\n'
                  'Oct 13, 2026 | Migration Planning <https://www.google.com/calendar/event?eid=meeting>\n')
        ident=self.create(title='Project Plan',source_key='google-doc:primary',original_text=original,
            doc_urls=['https://docs.google.com/document/d/primary/edit?resourcekey=access'])
        before=self.item(ident)
        links=before['source_links']
        primary=next(link for link in links if link['primary'])
        self.assertEqual(primary['label'],'Project Plan')
        self.assertIn('resourcekey=access',primary['url'])
        model=next(link for link in links if '/rollouts/' in link['url'])
        self.assertEqual((model['atlas_id'],model['label']),(cached,'Model Rollouts'))
        self.assertFalse(model['primary'])
        missing=next(link for link in links if '/missing/' in link['url'])
        self.assertEqual(missing['label'],'Missing resources')
        self.assertNotIn('date',missing)
        calendar=next(link for link in links if link['kind']=='calendar')
        self.assertEqual((calendar['label'],calendar['date']),('Migration Planning','2026-10-13'))
        self.assertFalse(calendar['primary'])
        self.assertEqual(self.item(ident)['revision'],before['revision'])
        self.assertEqual(self.request('/api/source/'+ident)[2],original.encode())
        self.tool('delete_item',item_id=cached,expected_revision=self.item(cached)['revision'])
        reference=next(link for link in self.item(ident)['source_links'] if '/rollouts/' in link['url'])
        self.assertNotIn('atlas_id',reference)
        url='https://www.google.com/calendar/event?eid=associated'
        meeting=self.tool('import_item',title='Planning',kind='meeting',summary='Summary',
            original_text='Meeting text',source_key='reference:meeting',calendar_url=url)['id']
        self.assertTrue(next(link for link in self.item(meeting)['source_links'] if link['url']==url)['primary'])

    def test_history_rejects_parent_directory_symlink_escape_and_recovers(self):
        ident=self.create()
        original_dir=self.service.library/'items'/ident/'originals'
        saved_dir=original_dir.with_name('saved-originals')
        original_dir.rename(saved_dir)
        outside=self.service.library.parent/'outside-originals'
        outside.mkdir()
        (outside/'secret.txt').write_text('Synthetic external secret')
        original_dir.symlink_to(outside,target_is_directory=True)
        status,_,body=self.request('/api/history/'+ident)
        self.assertEqual(status,400)
        self.assertNotIn(b'Synthetic external secret',body)
        original_dir.unlink()
        saved_dir.rename(original_dir)
        self.assertEqual(len(self.history(ident)),2)
        self.assertEqual(self.request('/api/source/'+ident)[2],'Original café\r\n東京'.encode())



if __name__=='__main__':
    unittest.main()
