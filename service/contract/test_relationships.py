"""Entity, person, meeting/feed, metadata and Trash contracts over endpoints."""
from harness import EndpointCase, ToolError


class RelationshipContracts(EndpointCase):
    def make(self, title='Design', key='design', **extra):
        args = dict(title=title, summary='Supported summary', kind='document',
                    source_key='synthetic:'+key, original_text='Complete preserved source')
        args.update(extra)
        return self.tool('import_item', **args)['id']

    def item(self, ident):
        return self.tool('get_item', item_id=ident)

    def delete(self, ident):
        return self.tool('delete_item', item_id=ident, expected_revision=self.item(ident)['revision'])

    def test_entities_relationships_legacy_adapter_and_undo_preserve_content(self):
        project = self.tool('create_entity', name='Project Amber', kind='project')
        team = self.tool('create_entity', name='Platform Team', kind='team')
        person = self.tool('create_entity', name='Jane Doe', kind='person')
        doc = self.make(projects=['Project Amber'])
        before = self.item(doc)
        self.tool('link_entity', item_ids=[person['profile_id']], entity_id=team['id'])
        self.assertEqual([p['id'] for p in self.tool('get_team', item_id=team['profile_id'])['members']], [person['profile_id']])
        self.assertEqual([d['id'] for d in self.tool('get_project', item_id=project['profile_id'])['documents']], [doc])
        self.assertEqual(self.tool('get_entity', entity_id=project['id'])['entity']['profile_id'], project['profile_id'])
        self.assertEqual({e['id'] for e in self.tool('list_entities')}, {project['id'],team['id'],person['id']})
        self.tool('apply_tag', item_ids=[doc], tag_id=team['legacy_tag_id'])
        self.assertIn(team['id'], [r['id'] for r in self.item(doc)['relationships']])
        self.post('/api/undo', {})
        self.assertNotIn(team['id'], [r['id'] for r in self.item(doc)['relationships']])
        self.tool('link_entity', item_ids=[doc], entity_id=project['id'], remove=True)
        self.assertEqual(self.tool('get_project', item_id=project['profile_id'])['documents'], [])
        self.assertEqual(self.item(doc)['summary'], before['summary'])
        self.assertEqual(self.item(doc)['source_hash'], before['source_hash'])
        self.assertEqual(len(self.tool('search_items', kind='document')), 1)
        with self.assertRaises(ToolError):
            self.tool('get_entity', entity_id='missing')
        with self.assertRaises(ToolError):
            self.tool('create_entity', name='Invalid', kind='unknown')
        with self.assertRaises(ToolError):
            self.tool('link_entity', item_ids=[person['profile_id']], entity_id=person['id'])

    def test_generic_person_connection_is_not_authorship_or_attendance(self):
        person = self.tool('create_entity', name='Jane Doe', kind='person')
        related = self.make()
        self.tool('link_entity', item_ids=[related], entity_id=person['id'])
        row = self.item(related)
        self.assertEqual(row['authors'], [])
        self.assertEqual(row['participants'], [])
        self.assertEqual(row['relationships'][0]['relation'], 'related_to')
        authored = self.make('Authored design', 'authored', authors=['Jane Doe'])
        profile = self.tool('get_person', item_id=person['profile_id'])
        self.assertEqual([d['id'] for d in profile['related_documents']], [related])
        self.assertEqual([d['id'] for d in profile['documents']], [authored])
        self.assertEqual(profile['meetings'], [])
        self.tool('set_authors', item_id=authored, authors=[])
        self.assertEqual(self.tool('get_person', item_id=person['profile_id'])['documents'], [])
        self.assertEqual({i['id'] for i in self.tool('search_items', tag_ids=[person['id']])}, {related})
        with self.assertRaises(ToolError):
            self.tool('set_authors', item_id='missing', authors=['Jane Doe'])
        self.assertEqual(self.item(related)['authors'], [])

    def test_trashed_entity_hidden_and_never_recreated_until_explicit_restore(self):
        person = self.tool('create_entity', name='Jane Doe', kind='person')
        reused = self.tool('create_entity', name='jane doe', kind='person')
        self.assertEqual(reused['profile_id'], person['profile_id'])
        doc = self.make()
        self.tool('link_entity', item_ids=[doc], entity_id=person['id'])
        self.delete(person['profile_id'])
        self.assertEqual(self.item(doc)['relationships'], [])
        self.assertFalse(any(e['id']==person['id'] for e in self.tool('list_entities')))
        with self.assertRaises(ToolError):
            self.tool('create_entity', name='Jane Doe', kind='person')
        with self.assertRaises(ToolError):
            self.tool('link_entity', item_ids=[doc], entity_id=person['id'])
        self.restart()
        self.tool('restore_item', item_id=person['profile_id'])
        self.assertEqual(self.item(doc)['relationships'][0]['id'], person['id'])
        self.assertEqual(len(self.tool('search_items', collection='people')), 1)

    def test_same_named_topic_is_independent_from_project_membership(self):
        entity = self.tool('create_entity', name='Architecture', kind='project')
        topic = self.post('/api/tag', {'name':'Architecture','category':'topic'})['id']
        doc = self.make(tags=[{'name':'Architecture','category':'topic'}])
        self.assertEqual(self.item(doc)['relationships'], [])
        self.tool('link_entity', item_ids=[doc], entity_id=entity['id'])
        self.assertEqual(len(self.item(doc)['relationships']), 1)
        self.tool('link_entity', item_ids=[doc], entity_id=entity['id'], remove=True)
        self.assertIn(topic, [t['id'] for t in self.item(doc)['tags']])
        self.assertEqual(self.item(doc)['relationships'], [])
        tags = self.tool('list_tags')
        self.assertEqual({t['category'] for t in tags if t['name']=='Architecture'}, {'project','topic'})
        with self.assertRaises(ToolError):
            self.tool('apply_tag', item_ids=[doc], tag_id='missing')
        self.assertIn(topic, [t['id'] for t in self.item(doc)['tags']])

    def test_duplicate_document_merges_membership_without_overwriting_manual_material(self):
        first = self.tool('create_entity', name='Project Amber', kind='project')
        second = self.tool('create_entity', name='Project Blue', kind='project')
        ident = self.make(projects=['Project Amber'], authors=['Confirmed Author'],
            doc_urls=['https://docs.google.com/document/d/shared/view?resourcekey=access#heading=one'])
        self.tool('set_starred', item_id=ident, starred=True)
        before = self.item(ident)
        duplicate = self.tool('import_item', title='Ignore title', summary='Ignore summary', kind='document',
            original_text='Ignore original', source_key='different-key', projects=['Project Blue'],
            doc_urls=['https://docs.google.com/document/u/0/d/shared/edit?usp=sharing'])
        self.assertEqual(duplicate['id'], ident)
        self.assertTrue(duplicate['duplicate'])
        after = self.item(ident)
        for field in ('title','summary','source_hash','source_path','date','starred','authors'):
            self.assertEqual(after[field], before[field], field)
        self.assertEqual({p['id'] for p in after['projects']}, {first['profile_id'],second['profile_id']})
        self.assertEqual(len(self.tool('search_items', kind='document')), 1)
        self.assertEqual(after['metadata']['doc_urls'], ['https://docs.google.com/document/d/shared/edit?resourcekey=access'])

    def test_meeting_participants_create_people_and_group_one_on_ones_by_identity(self):
        participant = {'name':'Jane Doe','email':'jane@example.com','evidence':'Confirmed full-name transcript speaker'}
        first = self.make('Jane and John skip-level','first-meeting', kind='one-on-one', date='2026-10-01')
        before = self.item(first)
        result = self.tool('link_meeting_people', item_id=first, participants=[participant],
                           expected_revision=before['revision'])
        person = result['people'][0]['id']
        row = self.item(first)
        self.assertEqual(row['series']['title'], 'Jane ↔ John')
        self.assertEqual(row['series']['cadence'], '')
        for field in ('summary','source_hash','date','authors'):
            self.assertEqual(row[field], before[field], field)
        revision = row['revision']
        self.assertFalse(self.tool('link_meeting_people', item_id=first, participants=[participant])['changed'])
        self.assertEqual(self.item(first)['revision'], revision)
        second = self.make('Another wording','second-meeting', kind='one-on-one', date='2026-10-15', participants=[participant])
        self.assertEqual(self.item(second)['series']['id'], row['series']['id'])
        series = self.tool('get_meeting_series', series_id=row['series']['id'])
        self.assertEqual([s['id'] for s in series['sessions']], [second,first])
        self.assertEqual(series['latest']['id'], second)
        self.assertEqual({m['id'] for m in self.tool('get_person', item_id=person)['meetings']}, {first,second})
        with self.assertRaises(ToolError):
            self.tool('link_meeting_people', item_id=first, participants=[{'name':'Jane','evidence':'First name only'}])
        with self.assertRaises(ToolError):
            self.tool('link_meeting_people', item_id=first, participants=[participant], expected_revision=before['revision'])
        self.assertEqual(len(self.tool('search_items', collection='people')), 1)

    def test_zoom_confirmed_speakers_create_colleague_not_owner_or_author(self):
        text = '[Jane Doe] 06:31:01\nHello\n[John Smith] 06:31:03\nHi\n[Zoom Room] 06:32:00\nNoise'
        source = dict(type='zoom', source_key='zoom-transcript:jane/transcript.txt',
            title='Jane skip-level', original_name='transcript.txt', original_text=text)
        queued = self.tool('submit_sources', sources=[source])['sources'][0]['intake_id']
        record = self.tool('get_intake', intake_id=queued)
        result = self.tool('complete_intake', intake_id=queued, expected_source_hash=record['source_hash'],
            payload={'title':'Jane skip-level','kind':'one-on-one','summary':'Meeting summary'})
        people = self.tool('search_items', collection='people')
        self.assertEqual([p['title'] for p in people], ['Jane Doe'])
        self.assertEqual(self.tool('get_person', item_id=people[0]['id'])['meetings'][0]['id'], result['id'])
        row = self.item(result['id'])
        self.assertEqual(row['authors'], [])
        self.assertEqual(row['document_people'], [])
        self.assertEqual(row['series']['title'], 'Jane ↔ John')

    def test_manual_series_preserves_cadence_and_ungrouping_keeps_session(self):
        first = self.make('Older session','older', kind='meeting', date='2026-10-01')
        before = self.item(first)
        self.tool('assign_meeting_series', item_id=first, title='Cloud Sync', cadence='Every two weeks')
        newer = self.make('New session','newer', kind='meeting', date='2026-10-15', series_title=' cloud sync ')
        listing = self.tool('list_meeting_series')
        self.assertEqual(len(listing), 1)
        series = self.tool('get_meeting_series', series_id=listing[0]['id'])
        self.assertEqual([s['id'] for s in series['sessions']], [newer,first])
        self.assertEqual(series['latest']['id'], newer)
        self.assertEqual(self.item(first)['series']['cadence'], 'Every two weeks')
        self.assertEqual(self.item(first)['source_hash'], before['source_hash'])
        self.tool('assign_meeting_series', item_id=newer)
        self.assertIsNone(self.item(newer)['series'])
        self.assertEqual(self.tool('get_meeting_series', series_id=listing[0]['id'])['sessions'][0]['id'], first)
        with self.assertRaises(ToolError):
            self.tool('get_meeting_series', series_id='missing')
        self.assertEqual(self.item(newer)['summary'], 'Supported summary')

    def test_candidates_are_separate_from_colleagues_and_preserve_interview_sources(self):
        colleague = self.tool('create_entity', name='Colleague Person', kind='person')['profile_id']
        meeting = self.make('Team sync','team-sync', kind='meeting')
        interview = self.make('Candidate interview','interview', kind='meeting', candidates=['Adrian Ciobanu'])
        candidate = self.tool('search_items', collection='interviews', kind='person')[0]['id']
        self.assertEqual([p['id'] for p in self.tool('search_items', collection='people')], [colleague])
        self.assertEqual([m['id'] for m in self.tool('search_items', collection='meetings')], [meeting])
        self.assertEqual({i['id'] for i in self.tool('search_items', collection='interviews')}, {candidate,interview})
        self.assertEqual(self.tool('get_person', item_id=candidate)['interviews'][0]['id'], interview)
        before = self.item(meeting)
        linked = self.tool('link_interview', item_id=meeting, candidates=['Adrian Ciobanu'])
        self.assertEqual(linked['people'][0]['id'], candidate)
        for field in ('summary','source_hash','authors','date'):
            self.assertEqual(self.item(meeting)[field], before[field], field)
        self.delete(candidate)
        with self.assertRaises(ToolError):
            self.tool('link_interview', item_id=interview, candidates=['Adrian Ciobanu'])
        self.assertEqual(self.tool('search_items', collection='people')[0]['id'], colleague)
        self.assertNotIn(candidate, [p['id'] for p in self.tool('search_items', collection='interviews')])
        doc = self.make('Ordinary document','ordinary')
        with self.assertRaises(ToolError):
            self.tool('link_interview', item_id=doc, candidates=['Other Candidate'])
        self.assertEqual(self.item(doc)['authors'], [])

    def test_feed_editions_order_by_date_stars_never_override_and_trash_restores_latest(self):
        feeds = self.tool('list_blog_feeds')
        self.assertEqual([(f['name'],f['edition_count']) for f in feeds], [('AI',0)])
        feed_id = feeds[0]['id']
        self.assertIsNone(self.tool('get_blog_feed', feed_id=feed_id)['latest'])
        newer = self.make('AI newer','ai-newer', kind='blog', date='2026-10-15', blog_feed='AI')
        older = self.make('AI older','ai-older', kind='blog', date='2026-10-01', blog_feed=' ai ')
        self.tool('set_starred', item_id=older, starred=True)
        feed = self.tool('get_blog_feed', feed_id=feed_id)
        self.assertEqual([d['id'] for d in feed['editions']], [newer,older])
        self.assertEqual(feed['latest']['id'], newer)
        self.delete(newer)
        self.assertEqual(self.tool('get_blog_feed', feed_id=feed_id)['latest']['id'], older)
        self.tool('restore_item', item_id=newer)
        self.assertEqual(self.tool('get_blog_feed', feed_id=feed_id)['latest']['id'], newer)
        with self.assertRaises(ToolError):
            self.make('Undated edition','undated', kind='blog', blog_feed='AI')
        self.assertEqual(self.tool('list_blog_feeds')[0]['edition_count'], 2)
        with self.assertRaises(ToolError):
            self.tool('get_blog_feed', feed_id='missing')

    def test_date_revision_summary_and_review_state_survive_restart(self):
        ident = self.make(tags=[{'name':'Architecture','category':'topic'}])
        initial = self.item(ident)
        self.assertEqual(initial['date_basis'], 'added')
        edited = self.tool('set_date', item_id=ident, date='2026-09-05', expected_revision=initial['revision'])
        self.assertEqual(edited['date_basis'], 'manual')
        self.assertEqual(edited['created'], initial['created'])
        with self.assertRaises(ToolError):
            self.tool('set_date', item_id=ident, date='2026-09-06', expected_revision=initial['revision'])
        with self.assertRaises(ToolError):
            self.tool('set_date', item_id=ident, date='2026-02-30', expected_revision=edited['revision'])
        updated = self.tool('update_summary', item_id=ident, summary='New summary', expected_revision=edited['revision'])
        with self.assertRaises(ToolError):
            self.tool('update_summary', item_id=ident, summary='Stale summary', expected_revision=edited['revision'])
        self.tool('set_reviewed', item_id=ident, reviewed=True)
        self.assertEqual(self.get('/api/items?inbox=1'), [])
        unreviewed = self.tool('set_reviewed', item_id=ident, reviewed=False)
        self.assertEqual(self.get('/api/items?inbox=1')[0]['id'], ident)
        self.restart()
        row = self.item(ident)
        self.assertEqual(row['summary'], 'New summary')
        self.assertEqual(row['date'], '2026-09-05')
        self.assertEqual(row['tags'], initial['tags'])
        self.assertEqual(row['source_hash'], initial['source_hash'])
        self.assertEqual(row['created'], initial['created'])
        self.assertEqual(unreviewed['revision'], updated['revision']+2)
        self.assertEqual(row['revision'], unreviewed['revision'])
        self.delete(ident)
        with self.assertRaises(ToolError):
            self.tool('set_reviewed', item_id=ident, reviewed=False)
