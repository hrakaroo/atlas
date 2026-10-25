"""Storage ownership regression: public endpoint assertions on disposable roots."""
import hashlib
import json
import shutil
import sqlite3
from urllib.parse import quote
from harness import EndpointCase, Service, ToolError, exact
from legacy_fixtures import prepare, PERSON, PROJECT, TEAM, DOCUMENT


class OwnedStorageContracts(EndpointCase):
    def test_entities_have_no_file_dependency_and_keep_description_history(self):
        for kind in ('person','team','project'):
            entity=self.tool('create_entity',name='Fixture '+kind,kind=kind)
            ident=entity['profile_id']
            original=self.get('/api/item/'+ident)
            saved=self.tool('update_summary',item_id=ident,expected_revision=original['revision'],summary='Database description café')
            # Fixture perturbation demonstrates that no item directory is necessary.
            shutil.rmtree(self.service.library/'items'/ident,ignore_errors=True)
            self.restart()
            exact(saved,self.get('/api/item/'+ident))
            entries=self.get('/api/history/'+ident)['entries']
            exact({original['summary'].encode(),'Database description café'.encode()},
                  {self.request('/api/history/'+ident+'/'+e['id'])[2] for e in entries})
        backup=self.post('/api/backup',{})['path']
        before=self.tool('list_entities')
        self.service.reopen_backup(backup)
        exact(before,self.tool('list_entities'))

    def test_entity_failed_description_commit_rolls_back_history_and_activity(self):
        ident=self.tool('create_entity',name='Atomic colleague',kind='person')['profile_id']
        before=self.get('/api/item/'+ident)
        history=self.get('/api/history/'+ident)
        events=self.tool('list_activity')
        self.service.restart(env_overrides={'ATLAS_CONTRACT_FAULT':json.dumps({'point':'summary.before_commit','action':'fail'})})
        with self.assertRaises(ToolError):
            self.tool('update_summary',item_id=ident,expected_revision=before['revision'],summary='Uncommitted description')
        exact(before,self.get('/api/item/'+ident))
        exact(history,self.get('/api/history/'+ident))
        exact(events,self.tool('list_activity'))
        self.service.restart(env_overrides={})
        exact(before,self.get('/api/item/'+ident))

    def test_confluence_page_identity_links_cache_and_tombstone(self):
        url='https://example.atlassian.net/wiki/spaces/WORK/pages/12345/Design'
        args=dict(title='Design',kind='document',summary='Cached overview',original_text='Complete cached page',doc_urls=[url])
        ident=self.tool('import_item',**args)['id']
        row=self.get('/api/item/'+ident)
        exact('confluence:example.atlassian.net:12345',row['source_key'])
        self.assertTrue(row['source_links'][0]['primary'])
        variant='https://example.atlassian.net/wiki/pages/viewpage.action?pageId=12345'
        args['doc_urls']=[variant]
        exact(ident,self.tool('import_item',**args)['id'])
        self.tool('delete_item',item_id=ident,expected_revision=row['revision'])
        self.restart()
        self.assertTrue(self.tool('import_item',**args)['trashed'])
        args['explicit']=True
        self.assertTrue(self.tool('import_item',**args)['restored'])
        exact(b'Complete cached page',self.request('/api/source/'+ident)[2])

    def test_entity_original_and_history_are_database_owned(self):
        text='Original identity context café\r\n東京'
        ident=self.tool('import_item',title='Database colleague',kind='person',summary='Profile',
                        source_key='fixture:person-original',original_text=text)['id']
        item=self.get('/api/item/'+ident)
        self.assertIsNone(item['source_path'])
        exact(text.encode(),self.request('/api/source/'+ident)[2])
        entries=self.get('/api/history/'+ident)['entries']
        shutil.rmtree(self.service.library/'items'/ident,ignore_errors=True)
        self.restart()
        exact(entries,self.get('/api/history/'+ident)['entries'])
        exact(text.encode(),self.request('/api/source/'+ident)[2])

    def test_database_entity_trash_and_restore_repair_links_without_files(self):
        target=self.tool('create_entity',name='Linked colleague',kind='person')['profile_id']
        neighbor=self.tool('create_entity',name='Connected project',kind='project')['profile_id']
        initial=self.get('/api/item/'+neighbor)
        summary='Work with [[Linked colleague]]'
        self.tool('update_summary',item_id=neighbor,expected_revision=initial['revision'],summary=summary)
        self.tool('delete_item',item_id=target,expected_revision=self.get('/api/item/'+target)['revision'])
        self.restart()
        self.assertNotIn('[[',self.get('/api/item/'+neighbor)['summary'])
        self.tool('restore_item',item_id=target)
        self.restart()
        exact(summary,self.get('/api/item/'+neighbor)['summary'])
        backup=self.post('/api/backup',{})['path']
        self.service.reopen_backup(backup)
        exact(summary,self.get('/api/item/'+neighbor)['summary'])

    def test_zoom_preserves_complete_source_after_recording_file_removed(self):
        text='[00:00:00] Jane Doe: Entire transcript café\r\n'+'Evidence. '*10000
        recording=self.service.library.parent/'Zoom'/'transcript.txt'
        recording.parent.mkdir(); recording.write_text(text)
        queued=self.tool('submit_sources',sources=[dict(type='zoom',title='Planning',date='2026-10-01',
             source_key='zoom-transcript:Planning/transcript.txt',original_text=recording.read_bytes().decode(),original_name=recording.name)])['sources'][0]
        result=self.tool('complete_intake',intake_id=queued['intake_id'],expected_source_hash=hashlib.sha256(text.encode()).hexdigest(),payload={'title':'Planning','summary':'Planning summary','kind':'meeting','original_text':text})
        ident=result['id']; recording.unlink()
        self.restart()
        exact(text.encode(),self.request('/api/source/'+ident)[2])
        backup=self.post('/api/backup',{})['path']; self.service.reopen_backup(backup)
        exact(text.encode(),self.request('/api/source/'+ident)[2])


class OwnedLegacyService(Service):
    def start(self):
        if not getattr(self,'prepared',False):
            prepare(self.library,modern_columns=True)
            snapshot=self.library/'imports'/'initial-snapshot'
            snapshot.mkdir(parents=True)
            context=b'Legacy entity context\r\n'
            attachment=bytes((255,0,128,10))
            (snapshot/'profile.md').write_bytes(context)
            (snapshot/'captions.txt').write_bytes(attachment)
            # Synthetic pre-service setup only, never a running library.
            db=sqlite3.connect(str(self.library/'library.sqlite'))
            for ident in (PERSON,PROJECT,TEAM,DOCUMENT):
                meta=json.loads(db.execute('SELECT metadata FROM items WHERE id=?',(ident,)).fetchone()[0])
                meta['snapshot_root']=str(snapshot)
                db.execute('UPDATE items SET source_path=?,source_hash=?,metadata=? WHERE id=?',
                    (str(snapshot/'profile.md'),hashlib.sha256(context).hexdigest(),json.dumps(meta),ident))
                folder=self.library/'items'/ident
                folder.mkdir(exist_ok=True)
                (folder/'summary.md').write_bytes(('Curated '+ident+('\r\n' if ident==PERSON else '')).encode())
                (folder/'revisions').mkdir(exist_ok=True)
                (folder/'revisions'/'1.md').write_text('Earlier '+ident)
            db.execute('UPDATE items SET deleted_at=? WHERE id=?',('2025-02-01T00:00:00Z',TEAM))
            db.execute('CREATE TABLE trash_entries (item_id TEXT PRIMARY KEY, payload TEXT NOT NULL)')
            db.execute('INSERT INTO trash_entries VALUES(?,?)',(TEAM,json.dumps({'inbound':[],'source_text':'Legacy entity context'})))
            db.commit(); db.close()
            self.prepared=True
        super().start()


class LegacyOwnedStorageContracts(EndpointCase):
    def setUp(self):
        self.service=OwnedLegacyService(scenario=self.id())
        self.addCleanup(self.service.close)

    def test_legacy_description_keeps_exact_bytes_in_current_and_revision_history(self):
        raw=('Curated '+PERSON+'\r\n').encode()
        before=self.get('/api/item/'+PERSON)
        entries=self.get('/api/history/'+PERSON)['entries']
        current=next(e for e in entries if e['kind']=='summary' and e['current'])
        exact(raw,self.request('/api/history/'+PERSON+'/'+current['id'])[2])
        self.tool('update_summary',item_id=PERSON,expected_revision=before['revision'],summary='New database description')
        self.restart()
        entries=self.get('/api/history/'+PERSON)['entries']
        previous=next(e for e in entries if e['kind']=='summary' and e['revision']==before['revision'] and not e['current'])
        exact(raw,self.request('/api/history/'+PERSON+'/'+previous['id'])[2])

    def test_migration_preserves_original_history_ids_attachments_trash_and_aliases(self):
        source=b'Legacy entity context\r\n'
        original_id=hashlib.sha256(b'original\x00imports/initial-snapshot/profile.md').hexdigest()
        for ident in (PERSON,PROJECT,DOCUMENT):
            row=self.get('/api/item/'+ident)
            exact('Curated '+ident+('\n' if ident==PERSON else ''),row['summary'])
            if ident!=DOCUMENT: self.assertIsNone(row['source_path'])
            else: self.assertIn('/items/'+ident+'/originals/',row['source_path'])
            exact(source,self.request('/api/source/'+ident)[2])
            exact(source,self.request('/api/history/'+ident+'/'+original_id)[2])
            exact(bytes((255,0,128,10)),self.request('/api/attachment/'+ident+'?target=captions.txt')[2])
        # Remove obsolete shared snapshot as a fixture action; API remains sufficient.
        shutil.rmtree(self.service.library/'imports')
        before=self.get('/api/item/'+PERSON)
        self.restart()
        exact(before,self.get('/api/item/'+PERSON))
        self.tool('restore_item',item_id=TEAM)
        exact(source,self.request('/api/source/'+TEAM)[2])
        exact(source,self.request('/api/history/'+TEAM+'/'+original_id)[2])
        exact(bytes((255,0,128,10)),self.request('/api/attachment/'+TEAM+'?target=captions.txt')[2])
        backup=self.post('/api/backup',{})['path']; self.service.reopen_backup(backup)
        exact(source,self.request('/api/history/'+TEAM+'/'+original_id)[2])


class InterruptedOwnedLegacyService(OwnedLegacyService):
    def start(self):
        if not getattr(self,'prepared',False):
            self.env_overrides={'ATLAS_CONTRACT_FAULT':json.dumps({'point':'storage.entity.after_commit','action':'fail'})}
            try:
                super().start()
            except RuntimeError:
                self.interruption_observed=True
                self.env_overrides={}
                super().start()
            else:
                raise AssertionError('Storage interruption hook was not reached')
        else:
            super().start()


class InterruptedStorageContracts(LegacyOwnedStorageContracts):
    def setUp(self):
        self.service=InterruptedOwnedLegacyService(scenario=self.id())
        self.addCleanup(self.service.close)
        self.assertTrue(self.service.interruption_observed)
