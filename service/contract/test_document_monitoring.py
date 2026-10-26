"""Adaptive monitoring, all-document updates and seen state through endpoints."""
import datetime as dt
import hashlib
from harness import EndpointCase, Service, ToolError

NOW=dt.datetime(2026,10,25,18,tzinfo=dt.timezone.utc)
def stamp(value):return value.isoformat()

class DocumentMonitoringContracts(EndpointCase):
    def setUp(self):
        self.service=Service(clock=stamp(NOW),scenario=self.id())
        self.addCleanup(self.service.close)

    def make(self,key='doc',age=0,checked=2,text='Original source'):
        source=dict(file_id=key,title='Document '+key,created_time='2025-01-01T00:00:00Z',modified_time=stamp(NOW-dt.timedelta(days=age)))
        sync=dict(source,content_hash=hashlib.sha256(text.replace('\r\n','\n').replace('\r','\n').encode()).hexdigest(),last_checked=stamp(NOW-dt.timedelta(days=checked)))
        ident=self.tool('import_item',title=source['title'],kind='document',summary='Original summary',source_key='google-doc:'+key,
            original_text=text,doc_urls=['https://docs.google.com/document/d/'+key+'/edit'],metadata={'google_sync':sync})['id']
        return ident,source

    def item(self,ident):return self.tool('get_item',item_id=ident)
    def due(self):return {r['id']:r for r in self.tool('list_google_documents',mode='due')}
    def refresh(self,ident,source,text='Original source',summary=None):
        args=dict(item_id=ident,expected_revision=self.item(ident)['revision'],source=source,original_text=text)
        if summary is not None:args['summary']=summary
        return self.tool('refresh_document',**args)

    def test_all_tiers_and_boundaries_use_source_age_not_import_or_star_dates(self):
        for age,cadence in [(0,'daily'),(3,'daily'),(4,'every_other_day'),(14,'every_other_day'),(15,'weekly'),(60,'weekly'),(61,'monthly'),(180,'monthly'),(181,'manual')]:
            ident,source=self.make('age'+str(age),age=age,checked=40)
            if cadence=='manual':
                self.assertNotIn(ident,self.due())
                self.tool('set_refresh_requested',item_id=ident,requested=True)
            self.assertEqual(self.due()[ident]['monitoring']['cadence'],cadence)
            if age==181:self.assertTrue(self.due()[ident]['monitoring']['manual_only'])
        archived=ident
        self.assertEqual(self.tool('list_google_documents',mode='due')[0]['id'],archived)
        self.tool('set_refresh_requested',item_id=archived,requested=False)
        self.tool('set_starred',item_id=archived,starred=True)
        self.assertNotIn(archived,self.due())
        self.assertNotIn(archived,{r['id'] for r in self.tool('list_ownership_lookups',limit=3)['documents']})
        self.restart();self.assertNotIn(archived,self.due())

    def test_successful_metadata_check_advances_due_time_without_resetting_activity(self):
        ident,source=self.make(age=14,checked=3)
        activity=self.due()[ident]['monitoring']['activity_at']
        result=self.tool('check_document_source',item_id=ident,expected_revision=self.item(ident)['revision'],source=source)
        self.assertFalse(result['needs_content'])
        self.assertNotIn(ident,self.due())
        self.restart(clock=stamp(NOW+dt.timedelta(days=2)))
        # It is now weekly because activity aged; daily checks do not keep it active.
        self.assertNotIn(ident,self.due())
        self.restart(clock=stamp(NOW+dt.timedelta(days=7)))
        self.assertEqual(self.due()[ident]['monitoring']['activity_at'],activity)
        self.assertEqual(self.due()[ident]['monitoring']['cadence'],'weekly')

    def test_manual_archive_override_unchanged_stays_archived_changed_wakes_daily(self):
        ident,source=self.make(age=200,checked=90)
        self.tool('set_refresh_requested',item_id=ident,requested=True)
        self.refresh(ident,source)
        self.assertFalse(self.item(ident)['metadata']['refresh_requested'])
        self.assertNotIn(ident,self.due())
        self.tool('set_refresh_requested',item_id=ident,requested=True)
        self.refresh(ident,source,'Real change','Changed summary')
        self.assertNotIn(ident,self.due())
        self.restart(clock=stamp(NOW+dt.timedelta(days=1)))
        self.assertEqual(self.due()[ident]['monitoring']['cadence'],'daily')
        self.assertEqual(self.due()[ident]['monitoring']['activity_at'],stamp(NOW))

    def test_title_timestamp_owner_and_line_ending_changes_do_not_restart_cadence(self):
        ident,source=self.make(age=200,checked=90,text='Original\nsource\n')
        self.tool('set_refresh_requested',item_id=ident,requested=True)
        renamed=dict(source,title='Renamed document',modified_time=stamp(NOW),owners=[])
        self.refresh(ident,renamed,'Original\r\nsource\r\n')
        self.assertNotIn(ident,self.due())
        self.assertEqual(self.get('/api/items?kind=document&sort=updates')[0]['latest_change_id'],0)
        self.tool('set_refresh_requested',item_id=ident,requested=True)
        self.assertEqual(self.due()[ident]['monitoring']['cadence'],'manual')

    def test_unstarred_updates_sort_first_filter_and_seen_cursor_preserve_newer_update(self):
        first,source=self.make('changed',checked=0)
        other,_=self.make('unchanged',checked=0)
        self.tool('set_starred',item_id=other,starred=True)
        self.refresh(first,source,'Updated source','Updated summary')
        rows=self.get('/api/items?kind=document&sort=updates')
        self.assertEqual([r['id'] for r in rows],[first,other])
        change=rows[0]['latest_change_id'];self.assertEqual(rows[0]['unseen_updates'],1)
        self.refresh(first,source,'Another source change','Another summary')
        self.post('/api/changes-seen',{'item_id':first,'through_id':change})
        rows=self.get('/api/items?kind=document&sort=updates')
        self.assertEqual(rows[0]['unseen_updates'],1)
        self.assertEqual([r['id'] for r in self.get('/api/items?kind=document&sort=updates&starred=1')],[other])
        self.tool('set_starred',item_id=first,starred=True)
        self.assertEqual(self.get('/api/items?kind=document&sort=updates&starred=1')[0]['id'],first)
        self.restart();self.assertEqual(self.get('/api/items?kind=document&sort=updates')[0]['unseen_updates'],1)

    def test_failed_refresh_does_not_reset_monitoring_or_acknowledge_request(self):
        ident,source=self.make(age=30,checked=10)
        self.tool('set_refresh_requested',item_id=ident,requested=True)
        self.service.restart(env_overrides={'ATLAS_CONTRACT_FAULT':'{"point":"refresh.before_commit","action":"fail"}'})
        with self.assertRaises(ToolError):self.refresh(ident,source,'Changed source','Changed summary')
        self.assertEqual(self.due()[ident]['monitoring']['cadence'],'weekly')
        self.assertTrue(self.item(ident)['metadata']['refresh_requested'])
        self.service.restart(env_overrides={})
        self.assertEqual(self.due()[ident]['monitoring']['cadence'],'weekly')
