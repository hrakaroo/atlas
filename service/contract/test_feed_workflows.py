"""Reusable feed workflows, preservation and attention through endpoints only."""
from harness import EndpointCase, ToolError, Service

class FeedWorkflowContracts(EndpointCase):
    def setUp(self):
        self.service=Service(clock='2026-10-26T00:00:00Z',scenario=self.id())
        self.addCleanup(self.service.close)
    def configure(self,flow='in_place',presentation='overview',name='Service incidents'):
        return self.tool('configure_feed',name=name,flow=flow,presentation=presentation,entry_label='Incidents')
    def payload(self,feed,**extra):
        data=dict(feed_id=feed['id'],entry_key='incident:123',title='Example incident',summary='Short synopsis.',original_text='Full collected source.\r\n東京',source_url='https://example.com/incidents/123',observed_at='2026-10-26T00:00:00Z',expected_revision=0,fields=dict(external_id='123',severity=2,status='active'))
        data.update(extra);return data
    def upsert(self,feed,**extra):return self.tool('upsert_feed_entry',**self.payload(feed,**extra))
    def read(self,feed):return self.tool('get_blog_feed',feed_id=feed['id'])
    def seen(self,feed):
        current=self.read(feed)
        self.tool('mark_feed_seen',feed_id=feed['id'],through_id=current['latest_entry_id'])
    def test_append_skips_duplicate_and_existing_feeds_default_to_append(self):
        feed=self.configure('append','entries')
        first=self.post('/api/feed-entry',self.payload(feed))
        self.assertEqual(first['status'],'created');self.seen(feed)
        again=self.upsert(feed,summary='Different summary',original_text='Changed',fields={'status':'resolved'})
        self.assertEqual(again['status'],'duplicate');self.assertEqual(again['item'],first['item'])
        self.assertEqual(self.read(feed)['unseen_entries'],0)
        with self.assertRaises(ToolError):self.configure('in_place','overview')
        ai=next(f for f in self.get('/api/blogs') if f['name']=='AI')
        self.assertEqual((ai['flow'],ai['presentation']),('append','entries'))
    def test_in_place_preserves_identity_manual_organization_history_and_unread_race(self):
        feed=self.post('/api/feed-config',dict(name='Service incidents',flow='in_place',presentation='overview',entry_label='Incidents'))
        first=self.upsert(feed);item=first['item'];ident=item['id'];self.seen(feed)
        self.tool('set_starred',item_id=ident,starred=True)
        tag=self.post('/api/tag',dict(name='Operations',category='topic'))
        self.tool('apply_tag',item_ids=[ident],tag_id=tag['id'])
        self.tool('set_date',item_id=ident,date='2026-01-01',expected_revision=self.tool('get_item',item_id=ident)['revision'])
        item=self.tool('get_item',item_id=ident)
        update=self.upsert(feed,expected_revision=item['revision'],fields=dict(external_id='123',severity=3,status='stable'),summary='Mitigated, monitoring.',original_text='Full new source')
        self.assertEqual(update['status'],'updated');self.assertEqual(update['id'],ident)
        self.assertEqual(update['item']['date'],'2026-01-01');self.assertEqual(update['item']['starred'],1)
        self.assertTrue(any(t['id']==tag['id'] for t in update['item']['tags']))
        history=self.get('/api/history/'+ident)
        self.assertGreater(len(history['entries']) if isinstance(history,dict) else len(history),1)
        cursor=self.read(feed)['latest_entry_id']
        self.upsert(feed,expected_revision=update['item']['revision'],fields=dict(external_id='123',severity=3,status='resolved'),summary='Recovered.',original_text='Resolved source')
        self.post('/api/feed-seen',dict(feed_id=feed['id'],through_id=cursor))
        self.assertEqual(self.read(feed)['unseen_entries'],1)
        self.assertEqual(len(self.read(feed)['editions']),1)
        self.restart();self.assertEqual(self.read(feed)['unseen_entries'],1)
    def test_quiet_checks_sources_and_model_rephrasing_do_not_create_attention(self):
        feed=self.configure();first=self.upsert(feed);self.seen(feed)
        activity=self.tool('list_activity')
        same=self.upsert(feed,expected_revision=first['item']['revision'],summary='Random new model wording.')
        self.assertEqual(same['status'],'unchanged');self.assertEqual(same['item']['summary'],first['item']['summary'])
        check=self.post('/api/feed-check',dict(feed_id=feed['id']))
        self.assertEqual(self.read(feed)['checked_at'],check['checked_at'])
        self.assertEqual(self.tool('record_feed_check',feed_id=feed['id'])['checked_at'],check['checked_at'])
        quiet=self.upsert(feed,expected_revision=same['item']['revision'],original_text='New source includes only a channel join.',summary=first['item']['summary'])
        self.assertEqual(quiet['status'],'unchanged')
        self.assertNotEqual(quiet['item']['source_hash'],first['item']['source_hash'])
        self.assertEqual(self.read(feed)['unseen_entries'],0)
        self.assertEqual(self.tool('list_activity'),activity)
        history=self.get('/api/history/'+first['id'])
        current=quiet['item']
        checked=self.upsert(feed,expected_revision=current['revision'],original_text='New source includes only a channel join.',summary=current['summary'],observed_at='2026-10-26T00:01:00Z')
        self.assertEqual(checked['item']['revision'],current['revision'])
        self.assertEqual(self.get('/api/history/'+first['id']),history)
        self.assertEqual(self.read(feed)['unseen_entries'],0)

    def test_invalid_stale_and_conflicting_updates_do_not_write(self):
        feed=self.configure();first=self.upsert(feed);item=first['item']
        invalid=[{'fields':{'severity':0}},{'fields':{'severity':'2'}},{'fields':{'status':'probably resolved'}},{'source_url':'javascript:bad'},{'fields':{'channel_url':'http://example.com'}},{'observed_at':'2026-10-25T00:00:00Z'},{'expected_revision':0},{'original_text':''},{'source_context':{'window_start':'2026-10-26T00:00:00Z','window_end':'2026-10-25T00:00:00Z'}}]
        for extra in invalid:
            args=self.payload(feed,expected_revision=item['revision']);args.update(extra)
            with self.assertRaises(ToolError):self.tool('upsert_feed_entry',**args)
            self.assertEqual(self.tool('get_item',item_id=item['id']),item)
    def test_automatic_updates_never_restore_removed_entries(self):
        feed=self.configure();first=self.upsert(feed);item=first['item']
        self.tool('delete_item',item_id=item['id'],expected_revision=item['revision'])
        retry=self.upsert(feed)
        self.assertEqual(retry['status'],'blocked');self.assertEqual(retry['id'],item['id'])
        self.assertEqual(self.read(feed)['editions'],[])
        self.assertEqual(self.read(feed)['unseen_entries'],0)
    def test_overview_carries_open_incidents_and_archives_old_resolutions(self):
        feed=self.configure();self.upsert(feed)
        self.upsert(feed,entry_key='incident:124',title='Recovered',fields=dict(external_id='124',severity=1,status='resolved'))
        self.upsert(feed,entry_key='incident:125',title='Monitoring',fields=dict(external_id='125',severity=4,status='stable'))
        self.restart(clock='2026-10-28T00:00:00Z')
        current=self.read(feed)
        self.assertEqual([r['title'] for r in current['overview_entries']],['Example incident','Monitoring'])
        self.assertTrue(all(r['summary']=='Short synopsis.' for r in current['overview_entries']+current['archived_entries']))
        self.assertEqual([r['title'] for r in current['archived_entries']],['Recovered'])
        item=current['archived_entries'][0]
        self.upsert(feed,entry_key='incident:124',expected_revision=item['revision'],title='Recovered',fields=dict(external_id='124',severity=1,status='resolved'),observed_at='2026-10-28T00:00:00Z')
        self.assertEqual([r['title'] for r in self.read(feed)['archived_entries']],['Recovered'])
    def test_failed_commit_preserves_source_summary_attention_and_retry(self):
        feed=self.configure();first=self.upsert(feed);item=first['item'];self.seen(feed)
        args=self.payload(feed,expected_revision=item['revision'],summary='Changed.',original_text='Changed source',fields={'status':'resolved'})
        self.service.restart(env_overrides={'ATLAS_CONTRACT_FAULT':'{"point":"feed.before_commit","action":"fail"}'})
        with self.assertRaises(ToolError):self.tool('upsert_feed_entry',**args)
        self.service.restart(env_overrides={})
        self.assertEqual(self.tool('get_item',item_id=item['id']),item)
        self.assertEqual(self.read(feed)['unseen_entries'],0)
        self.assertEqual(self.tool('upsert_feed_entry',**args)['status'],'updated')


    def test_committed_update_recovers_once_after_interrupted_response(self):
        feed=self.configure();first=self.upsert(feed);self.seen(feed)
        args=self.payload(feed,expected_revision=first['item']['revision'],summary='Recovered status',original_text='Confirmed new source',fields={'status':'resolved'})
        self.service.restart(env_overrides={'ATLAS_CONTRACT_FAULT':'{"point":"feed.after_commit","action":"fail"}'})
        with self.assertRaises(ToolError):self.tool('upsert_feed_entry',**args)
        self.service.restart(env_overrides={})
        item=self.tool('get_item',item_id=first['id'])
        self.assertEqual(item['summary'],'Recovered status');self.assertEqual(self.read(feed)['unseen_entries'],1)
        self.assertEqual(self.request('/api/source/'+item['id'])[2],b'Confirmed new source')
        args['expected_revision']=item['revision']
        self.assertEqual(self.tool('upsert_feed_entry',**args)['status'],'unchanged')
        self.assertEqual(self.read(feed)['unseen_entries'],1)

    def test_concurrent_writers_cannot_overwrite_the_same_revision(self):
        from concurrent.futures import ThreadPoolExecutor
        feed=self.configure();first=self.upsert(feed)
        def update(label):
            import json
            status,_,body=self.request('/api/feed-entry',self.payload(feed,expected_revision=first['item']['revision'],original_text='Changed '+label,summary='Updated '+label))
            data=json.loads(body)
            if status==400:
                self.assertIn('changed',data['error']);return 'conflict'
            self.assertEqual(status,200);return data['status']
        with ThreadPoolExecutor(max_workers=2) as pool:results=list(pool.map(update,['left','right']))
        self.assertEqual(sorted(results),['conflict','updated'])
        self.assertEqual(len(self.read(feed)['editions']),1)


class LegacyFeedAttentionMigration(EndpointCase):
    def setUp(self):
        from legacy_fixtures import FeedCursorLegacyService
        self.service=FeedCursorLegacyService(scenario=self.id())
        self.addCleanup(self.service.close)
    def test_preserves_old_cursor_and_new_sequence_does_not_collide(self):
        feed=self.tool('get_blog_feed',feed_id='legacy-feed')
        self.assertEqual(feed['latest_entry_id'],30);self.assertEqual(feed['unseen_entries'],1)
        self.tool('mark_feed_seen',feed_id='legacy-feed',through_id=30)
        for _ in range(2):
            self.restart();self.assertEqual(self.tool('get_blog_feed',feed_id='legacy-feed')['unseen_entries'],0)
        ident=self.tool('import_item',title='New edition',kind='blog',blog_feed='Legacy feed',date='2026-02-01',summary='New summary',original_text='New original',source_key='legacy-feed:new')['id']
        feed=self.tool('get_blog_feed',feed_id='legacy-feed')
        self.assertGreater(feed['latest_entry_id'],30);self.assertEqual(feed['unseen_entries'],1)
        self.assertEqual(len(feed['editions']),3)
        self.assertEqual(self.request('/api/source/feed-original-0')[2],b'Saved legacy source 0')
