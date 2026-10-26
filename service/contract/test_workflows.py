"""Observable intake, refresh, ownership and recovery contracts over HTTP/MCP.

Every item is synthetic and every assertion uses a public endpoint. The test
service owns all database/filesystem work; this module never opens the library.
"""
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import json
import threading
import hashlib

from harness import EndpointCase, ToolError


class WorkflowCase(EndpointCase):
    def doc(self, key='roadmap', **extra):
        args = dict(title='Roadmap '+key, summary='Original summary', kind='document',
                    source_key='google-doc:'+key, original_text='Complete original source',
                    doc_urls=['https://docs.google.com/document/d/'+key+'/edit'])
        args.update(extra)
        return self.tool('import_item', **args)['id']

    def item(self, ident):
        return self.tool('get_item', item_id=ident)

    def source(self, ident):
        status, _, body = self.request('/api/source/'+ident)
        self.assertEqual(status, 200)
        return body

    def metadata(self, key='roadmap', **extra):
        result = dict(file_id=key, title='Roadmap '+key,
                      created_time='2026-01-01T00:00:00Z',
                      modified_time='2026-10-01T00:00:00Z',
                      mime_type='application/vnd.google-apps.document', trashed=False)
        result.update(extra)
        return result

    def google(self, key='roadmap', **extra):
        result = dict(type='google_doc', title='Roadmap '+key,
                      url='https://docs.google.com/document/d/'+key+'/edit?tab=t.0')
        result.update(extra)
        return result

    def zoom(self, key='2026-10-01 10.00.00 Example/transcript.txt', text='Complete transcript'):
        return dict(type='zoom', title='Example planning session', date='2026-10-01',
                    source_key='zoom-transcript:'+key, original_text=text,
                    original_name='transcript.txt')

    def queue(self, source):
        result = self.tool('submit_sources', sources=[source])['sources'][0]
        self.assertEqual(result['status'], 'pending')
        return result['intake_id']

    def delete(self, ident):
        return self.tool('delete_item', item_id=ident, expected_revision=self.item(ident)['revision'])

    def refresh(self, ident, text, summary=None, metadata=None):
        args = dict(item_id=ident, expected_revision=self.item(ident)['revision'],
                    source=metadata or self.metadata(), original_text=text)
        if summary is not None:
            args['summary'] = summary
        return self.tool('refresh_document', **args)

    def events(self):
        return self.tool('list_activity', limit=200)['events']


class IntakeContracts(WorkflowCase):
    def test_google_canonical_identity_duplicate_tombstone_and_explicit_restore(self):
        queued = self.queue(self.google())
        again = self.tool('submit_sources', sources=[self.google(url=
            'https://docs.google.com/document/u/1/d/roadmap/view?resourcekey=access#heading=one')])
        self.assertEqual(again['sources'][0]['intake_id'], queued)
        self.assertEqual(len(self.tool('list_intake', source_type='google_doc')), 1)
        committed = self.tool('complete_intake', intake_id=queued,
            payload={'summary':'Kept summary','original_text':'Entire source'}, source=self.metadata())
        self.assertEqual(committed['status'], 'imported')
        ident = committed['id']
        before = self.item(ident)
        self.assertEqual(self.source(ident), b'Entire source')
        self.assertEqual(before['source_key'], 'google-doc:roadmap')
        self.assertEqual(self.tool('submit_sources', sources=[self.google()])['sources'][0]['status'], 'duplicate')
        self.assertEqual(self.item(ident)['revision'], before['revision'])
        self.delete(ident)
        self.restart()
        self.assertEqual(self.tool('submit_sources', sources=[self.google()])['sources'][0]['status'], 'blocked')
        self.assertEqual(self.tool('complete_intake', intake_id=queued,
            payload={'summary':'Overwrite','original_text':'Overwrite'}, source=self.metadata())['status'], 'blocked')
        self.assertEqual(self.tool('search_items', kind='document'), [])
        restored = self.tool('import_item', title='Ignored', summary='Ignored', kind='document',
            source_key='google-doc:roadmap', original_text='Ignored',
            doc_urls=[self.google()['url']], explicit=True)
        self.assertTrue(restored['restored'])
        self.assertEqual(restored['id'], ident)
        self.assertEqual(self.item(ident)['summary'], 'Kept summary')
        self.assertEqual(self.source(ident), b'Entire source')
        self.assertEqual([e['action'] for e in self.events()], ['restored','removed','imported'])

    def test_concurrent_same_queue_completion_one_item_one_event(self):
        queued = self.queue(self.google())
        text = 'Complete concurrent source — café\nSecond line'
        arguments = dict(intake_id=queued,
            payload={'summary':'One summary','original_text':text}, source=self.metadata())
        ready = threading.Barrier(2)

        def complete(request_id):
            # Raw independent JSON-RPC requests avoid the harness RPC-ID counter.
            ready.wait(timeout=5)
            status, _, body = self.request('/mcp', {
                'jsonrpc':'2.0','id':request_id,'method':'tools/call',
                'params':{'name':'complete_intake','arguments':arguments}})
            self.assertEqual(status, 200)
            response = json.loads(body)
            self.assertEqual(response['id'], request_id)
            self.assertNotIn('error', response)
            result = response['result']
            self.assertFalse(result.get('isError', False), result)
            return json.loads(result['content'][0]['text'])

        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(complete, ('complete-left','complete-right')))
        self.assertEqual(sorted(r['status'] for r in results), ['duplicate','imported'])
        identities = {r['id'] for r in results}
        self.assertEqual(len(identities), 1)
        ident = identities.pop()
        self.assertEqual([r['id'] for r in self.tool('search_items', kind='document')], [ident])
        self.assertEqual(self.item(ident)['summary'], 'One summary')
        self.assertEqual(self.source(ident), text.encode())
        self.assertEqual([(e['item_id'],e['action']) for e in self.events()], [(ident,'imported')])
        self.assertEqual(self.tool('list_intake'), [])
        self.assertEqual(self.tool('get_intake', intake_id=queued)['status'], 'duplicate')
        self.restart()
        retried = self.tool('complete_intake', **arguments)
        self.assertEqual((retried['status'],retried['id']), ('duplicate',ident))
        self.assertEqual(self.source(ident), text.encode())
        self.assertEqual([(e['item_id'],e['action']) for e in self.events()], [(ident,'imported')])

    def test_zoom_preserves_full_queue_text_and_deduplicates_renamed_hash(self):
        text = 'Full UTF-8 transcript — café\nSecond line\n'
        queued = self.queue(self.zoom(text=text))
        record = self.tool('get_intake', intake_id=queued)
        self.assertEqual(record['original_text'], text)
        self.assertEqual(record['source_hash'], hashlib.sha256(text.encode()).hexdigest())
        self.assertNotIn('original_text', self.tool('list_intake', source_type='zoom')[0])
        self.restart()
        committed = self.tool('complete_intake', intake_id=queued,
            expected_source_hash=record['source_hash'],
            payload={'title':'Planning','kind':'meeting','summary':'Supported summary',
                     'original_text':'Incorrect truncated text'})
        self.assertEqual(committed['status'], 'imported')
        ident = committed['id']
        self.assertEqual(self.source(ident), text.encode())
        self.assertEqual(self.item(ident)['date'], '2026-10-01')
        self.assertEqual(self.item(ident)['kind'], 'meeting')
        renamed = self.zoom(key='renamed/transcript.txt', text=text)
        result = self.tool('submit_sources', sources=[renamed])['sources'][0]
        self.assertEqual((result['status'], result['item_id']), ('duplicate', ident))
        self.delete(ident)
        self.assertEqual(self.tool('submit_sources', sources=[renamed])['sources'][0]['status'], 'blocked')
        self.assertEqual(len(self.events()), 2)

    def test_changed_pending_zoom_rejects_stale_summary_and_reports_existing_change(self):
        source = self.zoom()
        queued = self.queue(source)
        old_hash = self.tool('get_intake', intake_id=queued)['source_hash']
        source['original_text'] = 'Different complete transcript'
        self.tool('submit_sources', sources=[source])
        with self.assertRaises(ToolError):
            self.tool('complete_intake', intake_id=queued, expected_source_hash=old_hash,
                payload={'title':'Meeting','kind':'meeting','summary':'Stale summary'})
        self.assertEqual(self.tool('search_items'), [])
        self.assertEqual(self.events(), [])
        latest = self.tool('get_intake', intake_id=queued)
        result = self.tool('complete_intake', intake_id=queued, expected_source_hash=latest['source_hash'],
            payload={'title':'Meeting','kind':'meeting','summary':'Fresh summary'})
        source['original_text'] = 'Changed after successful import'
        changed = self.tool('submit_sources', sources=[source])['sources'][0]
        self.assertEqual(changed['status'], 'changed')
        self.assertEqual(self.item(result['id'])['summary'], 'Fresh summary')
        self.assertEqual(self.source(result['id']), b'Different complete transcript')
        self.assertEqual(len(self.events()), 1)

    def test_deleted_after_queue_cannot_be_completed_or_force_restored(self):
        queued = self.queue(self.google())
        ident = self.doc()
        self.delete(ident)
        result = self.tool('complete_intake', intake_id=queued,
            payload={'summary':'Do not import','original_text':'Do not import','explicit':True},
            source=self.metadata())
        self.assertEqual((result['status'], result['id']), ('blocked', ident))
        self.assertEqual(self.tool('search_items', kind='document'), [])
        self.assertEqual([i['id'] for i in self.tool('list_trash')], [ident])

    def test_untitled_blank_foreign_and_unverified_sources_never_commit(self):
        for title in ('', '   ', ' UNTITLED   DOCUMENT ', 'Draft Untitled document copy'):
            result = self.tool('submit_sources', sources=[self.google(title=title)])
            self.assertEqual(result['sources'][0]['status'], 'filtered')
        result = self.tool('submit_sources', sources=[self.google(url='https://example.com/document/d/id/edit')])
        self.assertEqual(result['sources'][0]['status'], 'filtered')
        queued = self.queue(self.google())
        for changes in ({'trashed':True}, {'file_id':'another'}, {'mime_type':'text/plain'}):
            with self.assertRaises(ToolError):
                self.tool('complete_intake', intake_id=queued,
                    payload={'summary':'Summary','original_text':'Text'}, source=self.metadata(**changes))
        self.assertEqual(self.tool('complete_intake', intake_id=queued,
            payload={'summary':'Summary','original_text':'Text'},
            source=self.metadata(title='Untitled document'))['status'], 'filtered')
        with self.assertRaises(ToolError):
            self.tool('complete_intake', intake_id=queued,
                payload={'summary':'Summary','original_text':'  '}, source=self.metadata())
        with self.assertRaises(ToolError):
            self.tool('submit_sources', sources=[{'type':'zoom','source_key':'invalid','original_text':'text'}])
        with self.assertRaises(ToolError):
            self.tool('list_intake', source_type='unknown')
        with self.assertRaises(ToolError):
            self.tool('get_intake', intake_id='missing')
        self.assertEqual(self.tool('search_items'), [])
        self.assertEqual(self.events(), [])
        self.assertEqual(len(self.tool('list_intake')), 1)

    def test_ambiguous_legacy_session_requires_review_without_duplicate_or_new_import(self):
        existing = []
        for index in (1,2):
            existing.append(self.tool('import_item', title='Example planning session '+str(index),
                summary='Existing manually curated session '+str(index), kind='meeting',
                date='2026-10-01', original_text='Different historical transcript '+str(index),
                source_key='obsidian:ambiguous-'+str(index))['id'])
        before = {ident:self.item(ident) for ident in existing}
        events = self.events()
        result = self.tool('submit_sources', sources=[self.zoom(text='Unmatched new transcript text')])
        self.assertEqual(result['pending'], 0)
        decision = result['sources'][0]
        self.assertEqual(decision['status'], 'review')
        self.assertIn(decision['item_id'], existing)
        queued = self.tool('get_intake', intake_id=decision['intake_id'])
        self.assertEqual(queued['status'], 'review')
        self.assertEqual(queued['original_text'], 'Unmatched new transcript text')
        self.assertEqual(self.tool('list_intake'), [])
        attempted = self.tool('complete_intake', intake_id=decision['intake_id'],
            expected_source_hash=queued['source_hash'],
            payload={'title':'Must not import','kind':'meeting','summary':'Do not overwrite'})
        self.assertEqual(attempted['status'], 'review')
        self.assertIn(attempted['id'], existing)
        self.restart()
        self.assertEqual(self.tool('get_intake', intake_id=decision['intake_id'])['status'], 'review')
        self.assertEqual({r['id'] for r in self.tool('search_items', kind='meeting')}, set(existing))
        self.assertEqual({ident:self.item(ident) for ident in existing}, before)
        self.assertEqual(self.events(), events)

    def test_legacy_formatted_transcript_matches_existing_interview(self):
        text = ' '.join('word'+str(i) for i in range(100))
        old = self.tool('import_item', title='Example planning session values interview',
            summary='Historical summary', kind='interview', date='2026-10-01',
            original_text='# Heading\n'+text+'\nFooter', source_key='obsidian:legacy')['id']
        result = self.tool('submit_sources', sources=[self.zoom(text=text)])['sources'][0]
        self.assertEqual((result['status'], result['item_id']), ('duplicate', old))
        self.assertEqual(len(self.tool('search_items')), 1)


class RefreshContracts(WorkflowCase):
    def test_unchanged_metadata_and_line_endings_do_not_replace_manual_summary(self):
        ident = self.doc(original_text='Line one\r\nLine two')
        self.refresh(ident, 'Line one\nLine two')
        before = self.item(ident)
        self.tool('update_summary', item_id=ident, summary='Manual wording', expected_revision=before['revision'])
        before = self.item(ident)
        checked = self.tool('check_document_source', item_id=ident,
            expected_revision=before['revision'], source=self.metadata())
        self.assertFalse(checked['needs_content'])
        self.assertFalse(self.tool('inspect_document_content', item_id=ident,
            original_text='Line one\rLine two')['content_changed'])
        self.assertEqual(self.item(ident)['summary'], 'Manual wording')
        self.assertEqual(self.get('/api/changed-documents'), [])
        self.assertEqual([e['action'] for e in self.events()], ['summary_updated','imported'])

    def test_refresh_flags_conflicts_and_changed_content_preserve_manual_organization(self):
        ident = self.doc(authors=['Confirmed Author'], projects=['Project Amber'],
                         tags=[{'name':'Architecture','category':'topic'}], date='2026-02-02')
        self.tool('set_starred', item_id=ident, starred=True)
        self.tool('set_refresh_requested', item_id=ident, requested=True)
        before = self.item(ident)
        events_before = self.events()
        with self.assertRaises(ToolError):
            self.refresh(ident, 'New source without summary')
        self.assertTrue(self.item(ident)['metadata']['refresh_requested'])
        self.assertEqual(self.source(ident), b'Complete original source')
        self.assertEqual(self.events(), events_before)
        self.tool('update_summary', item_id=ident, summary='Concurrent manual edit', expected_revision=before['revision'])
        with self.assertRaises(ToolError):
            self.tool('refresh_document', item_id=ident, expected_revision=before['revision'],
                source=self.metadata(), original_text='New source', summary='Stale replacement')
        self.assertEqual(self.item(ident)['summary'], 'Concurrent manual edit')
        self.assertTrue(self.item(ident)['metadata']['refresh_requested'])
        result = self.refresh(ident, 'New source', summary='Source-grounded replacement')
        after = self.item(ident)
        self.assertTrue(result['content_changed'])
        self.assertFalse(after['metadata']['refresh_requested'])
        self.assertEqual(after['summary'], 'Source-grounded replacement')
        self.assertEqual(self.source(ident), b'New source')
        for field in ('tags','authors','projects','date','starred'):
            self.assertEqual(after[field], before[field], field)
        self.restart()
        self.assertEqual(self.item(ident)['source_hash'], hashlib.sha256(b'New source').hexdigest())
        self.assertEqual(self.tool('list_google_documents', mode='dirty'), [])

    def test_wrong_source_identity_and_invalid_times_cannot_change_or_acknowledge_document(self):
        ident = self.doc(summary='Manual summary that must survive')
        self.tool('set_refresh_requested', item_id=ident, requested=True)
        before = self.item(ident)
        before_events = self.events()
        for source in (self.metadata('different-document'), self.metadata(modified_time='invalid'),
                       self.metadata(created_time='2026-01-01T00:00:00'), self.metadata(title=' ')):
            for tool in ('check_document_source','refresh_document'):
                arguments = dict(item_id=ident, expected_revision=before['revision'], source=source)
                if tool=='refresh_document':
                    arguments.update(original_text='Do not save this source',summary='Do not replace manual summary')
                with self.assertRaises(ToolError):
                    self.tool(tool, **arguments)
                self.assertEqual(self.item(ident), before)
                self.assertEqual(self.source(ident), b'Complete original source')
                self.assertEqual(self.events(), before_events)
        self.restart()
        self.assertEqual(self.item(ident), before)
        self.assertEqual([r['id'] for r in self.tool('list_google_documents', mode='dirty')], [ident])

    def test_recent_selection_uses_source_activity_and_includes_flagged_old_documents(self):
        old = self.doc('old', date='2099-01-01')
        other = self.doc('other', date='2099-01-01')
        historical = self.metadata('old', created_time='2020-01-01T00:00:00Z', modified_time='2020-01-01T00:00:00Z')
        self.refresh(old, 'Complete original source', metadata=historical)
        self.refresh(other, 'Complete original source', metadata=dict(historical, file_id='other', title='Roadmap other'))
        self.assertEqual(self.tool('list_google_documents', mode='recent'), [])
        self.tool('set_refresh_requested', item_id=other, requested=True)
        recent = self.tool('list_google_documents', mode='recent', recent_file_ids=['old'])
        self.assertEqual({i['id'] for i in recent}, {old,other})
        self.assertEqual([i['id'] for i in self.tool('list_google_documents', mode='dirty')], [other])
        self.assertEqual(len(self.tool('list_google_documents', mode='all')), 2)
        with self.assertRaises(ToolError):
            self.tool('list_google_documents', mode='invalid')
        with self.assertRaises(ToolError):
            self.tool('inspect_document_content', item_id=old, original_text=' ')
        self.assertTrue(self.item(other)['metadata']['refresh_requested'])

    def test_starred_changes_seen_cursor_does_not_swallow_newer_versions(self):
        ident = self.doc()
        self.refresh(ident, 'Before star', summary='Summary one')
        self.assertEqual(self.get('/api/changed-documents'), [])
        self.tool('set_starred', item_id=ident, starred=True)
        self.refresh(ident, 'Version two', summary='Summary two')
        first = self.get('/api/changed-documents')[0]
        self.refresh(ident, 'Version three', summary='Summary three')
        self.post('/api/changes-seen', {'item_id':ident,'through_id':first['latest_change_id']})
        rows = self.get('/api/changed-documents')
        self.assertEqual([(r['id'],r['update_count'],r['unseen_updates']) for r in rows], [(ident,3,1)])
        self.post('/api/changes-seen', {'item_id':ident,'through_id':rows[0]['latest_change_id']})
        self.post('/api/changes-seen', {'item_id':ident,'through_id':first['latest_change_id']})
        self.restart()
        self.assertEqual(self.get('/api/changed-documents')[0]['unseen_updates'], 0)
        self.delete(ident)
        self.assertEqual(self.get('/api/changed-documents'), [])


class OwnershipContracts(WorkflowCase):
    def test_permissions_owner_creator_mentions_and_author_stay_distinct(self):
        queued = self.queue(self.google())
        source = self.metadata(permissions=[
            {'type':'user','role':'owner','displayName':'Owner Person','emailAddress':'owner@example.com'},
            {'type':'user','role':'writer','pendingOwner':True,'displayName':'Future Owner'},
            {'type':'user','role':'owner','deleted':True,'displayName':'Removed Owner'},
            {'type':'group','role':'owner','displayName':'Team Alias'}],
            creator={'displayName':'Creator Person'})
        result = self.tool('complete_intake', intake_id=queued, source=source,
            payload={'summary':'Summary','original_text':'Full original',
                     'document_people':[{'name':'Mentioned Person','email':'mentioned@example.com','role':'mentioned','person_chip':{'personId':'chip-mentioned','personProperties':{'name':'Mentioned Person','email':'mentioned@example.com'}}}]})
        ident = result['id']
        item = self.item(ident)
        self.assertEqual({p['role']:p['name'] for p in item['document_people']},
                         {'owner':'Owner Person','creator':'Creator Person','mentioned':'Mentioned Person'})
        self.assertEqual(item['authors'], [])
        owner = next(p for p in item['document_people'] if p['role']=='owner')
        self.assertIn('permissions role=owner', owner['evidence'])
        self.assertEqual([d['id'] for d in self.tool('get_person', item_id=owner['id'])['owned_documents']], [ident])
        self.tool('set_authors', item_id=ident, authors=['Owner Person'])
        profile = self.tool('get_person', item_id=owner['id'])
        self.assertEqual([d['id'] for d in profile['documents']], [ident])
        self.assertEqual(profile['owned_documents'], [])
        self.assertEqual(profile['documents'][0]['connection_roles'], ['owner'])
        self.assertEqual(self.source(ident), b'Full original')

    def test_owner_only_lookup_preserves_content_flags_and_uses_email_identity(self):
        first = self.doc()
        self.tool('set_refresh_requested', item_id=first, requested=True)
        before = self.item(first)
        result = self.tool('record_ownership_lookup', item_id=first, expected_revision=before['revision'],
            source=self.metadata(permissions=[{'type':'user','role':'owner','displayName':'Canonical Person',
                                               'emailAddress':'person@example.com'}]))
        self.assertEqual(result['outcome'], 'resolved')
        after = self.item(first)
        for field in ('summary','source_hash','source_path','date','tags','authors'):
            self.assertEqual(after[field], before[field], field)
        self.assertTrue(after['metadata']['refresh_requested'])
        second = self.doc('second')
        self.tool('record_ownership_lookup', item_id=second, expected_revision=self.item(second)['revision'],
            source=self.metadata('second', owners=[{'displayName':'Different Display', 'emailAddress':'person@example.com'}]))
        self.assertEqual(self.item(second)['document_people'][0]['id'], after['document_people'][0]['id'])
        self.assertEqual(self.tool('list_ownership_lookups')['documents'], [])
        with self.assertRaises(ToolError):
            self.tool('record_ownership_lookup', item_id=first, expected_revision=before['revision'], source=self.metadata(owners=[]))
        self.assertEqual(self.item(first)['document_people'][0]['name'], 'Canonical Person')

    def test_retry_backoff_is_bounded_persistent_and_provider_pause_is_not_ownerlessness(self):
        self.restart(clock='2026-10-16T12:00:00+00:00')
        ids = [self.doc('owner-'+str(i)) for i in range(5)]
        due = self.tool('list_ownership_lookups', limit=100)
        self.assertEqual(len(due['documents']), 3)
        ident = due['documents'][0]['id']
        key = self.item(ident)['source_key'].split(':',1)[1]
        result = self.tool('record_ownership_lookup', item_id=ident, expected_revision=self.item(ident)['revision'],
            source=self.metadata(key))
        self.assertEqual(result['outcome'], 'unavailable')
        self.assertTrue(result['notify'])
        self.assertTrue(result['stop_owner_pass'])
        self.assertEqual(result['next_attempt'], '2026-10-16T18:00:00+00:00')
        self.assertEqual(self.tool('list_ownership_lookups')['documents'], [])
        self.assertEqual(self.item(ident)['document_people'], [])
        self.restart(clock='2026-10-16T12:30:00+00:00')
        self.assertEqual(self.tool('list_ownership_lookups')['documents'], [])
        for delay in (24,72,168):
            now = dt.datetime.fromisoformat(result['next_attempt'])
            self.restart(clock=now.isoformat())
            self.assertTrue(self.tool('list_ownership_lookups')['documents'])
            result = self.tool('record_ownership_lookup', item_id=ident, expected_revision=self.item(ident)['revision'], source=self.metadata(key))
            self.assertEqual(dt.datetime.fromisoformat(result['next_attempt'])-now, dt.timedelta(hours=delay))
            self.assertFalse(result['notify'])
        self.assertEqual({i['id'] for i in self.tool('list_google_documents')}, set(ids))

    def test_per_item_failure_backoff_does_not_pause_other_documents(self):
        now = dt.datetime(2026,10,16,12,tzinfo=dt.timezone.utc)
        self.restart(clock=now.isoformat())
        failed = self.doc('failed-owner')
        other = self.doc('other-owner')
        before = self.item(failed)
        before_events = self.events()
        for attempt, hours in enumerate((6,24,72,168,168), start=1):
            result = self.tool('record_ownership_lookup', item_id=failed,
                expected_revision=before['revision'], error='Access denied', provider_unavailable=False)
            self.assertEqual(result['outcome'], 'failed')
            self.assertFalse(result['stop_owner_pass'])
            self.assertEqual(result['notify'], attempt==1)
            next_attempt = dt.datetime.fromisoformat(result['next_attempt'])
            self.assertEqual(next_attempt-now, dt.timedelta(hours=hours))
            due = self.tool('list_ownership_lookups')
            self.assertIsNone(due['paused_until'])
            self.assertEqual([r['id'] for r in due['documents']], [other])
            self.restart(clock=(next_attempt-dt.timedelta(seconds=1)).isoformat())
            self.assertEqual([r['id'] for r in self.tool('list_ownership_lookups')['documents']], [other])
            self.restart(clock=next_attempt.isoformat())
            self.assertEqual({r['id'] for r in self.tool('list_ownership_lookups')['documents']}, {failed,other})
            failed_row = next(r for r in self.tool('list_ownership_lookups')['documents'] if r['id']==failed)
            self.assertEqual(failed_row['attempts'], attempt)
            self.assertEqual(self.item(failed), before)
            self.assertEqual(self.events(), before_events)
            now = next_attempt

    def test_owner_link_activity_is_exact_and_repeated_metadata_is_quiet(self):
        ident = self.doc()
        source = self.metadata(permissions=[{'type':'user','role':'owner',
            'displayName':'Verified Owner','emailAddress':'verified@example.com'}])
        result = self.tool('record_ownership_lookup', item_id=ident,
            expected_revision=self.item(ident)['revision'], source=source)
        self.assertEqual(result['outcome'], 'resolved')
        self.assertTrue(result['changed'])
        owner = self.item(ident)['document_people'][0]['id']
        events = self.events()
        self.assertEqual([(e['item_id'],e['action']) for e in events],
                         [(ident,'people_linked'),(owner,'imported'),(ident,'imported')])
        revision = self.item(ident)['revision']
        repeated = self.tool('record_ownership_lookup', item_id=ident,
            expected_revision=revision, source=source)
        self.assertEqual(repeated['outcome'], 'resolved')
        self.assertFalse(repeated['changed'])
        self.assertEqual(self.item(ident)['revision'], revision)
        self.assertEqual(self.events(), events)
        self.restart()
        self.assertEqual(self.events(), events)
        self.assertEqual(self.tool('get_person', item_id=owner)['owned_documents'][0]['id'], ident)

    def test_invalid_owner_metadata_and_trashed_person_never_create_links(self):
        entity = self.tool('create_entity', name='Removed Person', kind='person')
        self.delete(entity['profile_id'])
        ident = self.doc()
        before = self.item(ident)
        for source in (self.metadata('wrong', owners=[]), self.metadata(owners='invalid'), self.metadata(trashed=True, owners=[])):
            with self.assertRaises(ToolError):
                self.tool('record_ownership_lookup', item_id=ident, expected_revision=before['revision'], source=source)
        result = self.tool('record_ownership_lookup', item_id=ident, expected_revision=before['revision'],
            source=self.metadata(owners=[{'displayName':'Removed Person'}]))
        self.assertEqual(result['outcome'], 'unresolved')
        self.assertIn('Trash', result['detail'])
        self.assertEqual(self.item(ident)['document_people'], [])
        self.assertEqual(self.tool('search_items', collection='people'), [])
        with self.assertRaises(ToolError):
            self.tool('link_document_people', item_id=ident, people=[{'name':'Fake','role':'author','evidence':'Unsupported role'}])
        self.assertEqual(self.item(ident)['authors'], [])

    def test_explicit_role_linking_and_replacement_do_not_assert_authorship(self):
        ident = self.doc()
        self.tool('link_document_people', item_id=ident,
            people=[{'name':'First Owner','role':'owner','evidence':'Observed source ownership'}], ownership_status='available')
        self.tool('link_document_people', item_id=ident,
            people=[{'name':'Second Owner','role':'owner','evidence':'Observed transfer'},
                    {'name':'Mention Person','email':'mention@example.com','role':'mentioned','person_link':'mailto:mention@example.com'}], replace_roles=['owner'])
        item = self.item(ident)
        self.assertEqual({p['role']:p['name'] for p in item['document_people']}, {'owner':'Second Owner','mentioned':'Mention Person'})
        self.assertEqual(item['authors'], [])
        self.assertEqual(item['participants'], [])
        self.restart()
        self.assertEqual(self.item(ident)['document_people'], item['document_people'])


class RecoveryContracts(WorkflowCase):
    def test_trash_cleans_backlinks_restores_unedited_and_preserves_edited_notes(self):
        target = self.doc()
        original = 'Read [[Roadmap roadmap|the plan]] and [direct](atlas://item/'+target+').'
        inbound = self.tool('import_item', title='Reader', summary=original, kind='note',
                            original_text='Source', source_key='reader')['id']
        edited = self.tool('import_item', title='Edited reader', summary='See [[Roadmap roadmap]]', kind='note',
                           original_text='Source', source_key='edited-reader')['id']
        preview = self.tool('preview_delete_item', item_id=target)
        self.assertEqual({i['id'] for i in preview['affected_notes']}, {inbound,edited})
        with self.assertRaises(ToolError):
            self.tool('delete_item', item_id=target, expected_revision=preview['revision']+50)
        self.assertEqual(self.item(inbound)['summary'], original)
        removed = self.tool('delete_item', item_id=target, expected_revision=preview['revision'])
        self.assertEqual(removed['cleaned_notes'], 2)
        self.assertNotIn('[[', self.item(inbound)['summary'])
        self.assertNotIn('atlas://', self.item(inbound)['summary'])
        row = self.item(edited)
        self.tool('update_summary', item_id=edited, summary='Manual new wording', expected_revision=row['revision'])
        self.restart()
        with self.assertRaises(ToolError):
            self.item(target)
        self.assertEqual([r['id'] for r in self.tool('list_trash', query='Roadmap')], [target])
        result = self.tool('restore_item', item_id=target)
        self.assertEqual(result['unchanged_edited_notes'], 1)
        self.assertEqual(self.item(inbound)['summary'], original)
        self.assertEqual(self.item(edited)['summary'], 'Manual new wording')
        self.assertEqual(self.item(inbound)['links'][0]['id'], target)
        with self.assertRaises(ToolError):
            self.tool('restore_item', item_id=target)
        self.assertEqual(self.tool('list_trash'), [])

    def test_activity_pagination_is_exact_and_duplicates_and_metadata_checks_are_quiet(self):
        ids = [self.doc('page-'+str(i)) for i in range(4)]
        initial = self.events()
        self.assertEqual(len(initial), 4)
        self.assertEqual({e['item_id'] for e in initial}, set(ids))
        self.assertTrue(all(e['action']=='imported' for e in initial))
        page = self.tool('list_activity', limit=2)
        self.assertEqual(page['events'], initial[:2])
        next_page = self.tool('list_activity', limit=2, before_id=page['events'][-1]['id'])
        self.assertEqual(next_page['events'], initial[2:])
        duplicate = self.tool('import_item', title='Discard', summary='Discard', kind='document',
            original_text='Discard', source_key='google-doc:page-0')
        self.assertTrue(duplicate['duplicate'])
        self.assertEqual(self.events(), initial)
        self.restart()
        self.assertEqual(self.events(), initial)
        with self.assertRaises(ToolError):
            self.tool('list_activity', limit=0)
