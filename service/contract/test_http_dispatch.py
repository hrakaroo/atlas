"""Exercise UI route dispatch separately from MCP domain workflows."""
from harness import EndpointCase, exact


class HTTPDispatchContracts(EndpointCase):
    def test_read_routes_and_ui_mutations_share_the_same_records(self):
        person=self.post('/api/entity',{'name':'Grace Example','kind':'person'})
        team=self.post('/api/entity',{'name':'Fixture Team','kind':'team'})
        project=self.post('/api/entity',{'name':'Fixture Project','kind':'project'})
        doc=self.post('/api/import',{'title':'UI dispatch fixture','summary':'Before UI edit',
            'kind':'document','original_text':'UI preserved text','source_key':'google-doc:ui-dispatch',
            'doc_urls':['https://docs.google.com/document/d/ui-dispatch/edit']})['id']
        self.post('/api/authors',{'item_id':doc,'authors':['Grace Example']})
        self.post('/api/link-entity',{'item_ids':[doc],'entity_id':project['id']})
        self.post('/api/link-entity',{'item_ids':[person['profile_id']],'entity_id':team['id']})
        self.post('/api/starred',{'item_id':doc,'starred':True})
        self.post('/api/reviewed',{'item_id':doc,'reviewed':True})
        self.assertTrue(self.tool('get_item',item_id=doc)['starred'])
        self.assertFalse(self.tool('get_item',item_id=doc)['inbox'])
        self.post('/api/date',{'item_id':doc,'date':'2026-03-04',
            'expected_revision':self.tool('get_item',item_id=doc)['revision']})
        self.post('/api/summary',{'item_id':doc,'summary':'After UI edit',
            'expected_revision':self.tool('get_item',item_id=doc)['revision']})
        self.post('/api/refresh-request',{'item_id':doc,'requested':True})
        self.assertEqual(self.tool('list_google_documents',mode='dirty')[0]['id'],doc)
        tag=self.post('/api/tag',{'name':'Fixture Topic','category':'topic'})['id']
        self.post('/api/apply',{'item_ids':[doc],'tag_id':tag})
        self.assertIn(tag,[t['id'] for t in self.tool('get_item',item_id=doc)['tags']])
        self.post('/api/undo',{})
        self.assertNotIn(tag,[t['id'] for t in self.tool('get_item',item_id=doc)['tags']])
        feed=self.tool('import_item',title='UI briefing',summary='Briefing summary',
            original_text='Briefing source',source_key='ui:briefing',kind='blog',
            blog_feed='AI',date='2026-01-01')['id']
        meeting=self.tool('import_item',title='UI meeting',summary='Meeting summary',
            original_text='Meeting source',source_key='ui:meeting',kind='meeting',date='2026-01-02')['id']
        self.post('/api/series',{'item_id':meeting,'title':'UI recurring meeting','cadence':'Weekly'})
        series=self.tool('get_item',item_id=meeting)['series']['id']
        feed_id=self.tool('get_item',item_id=feed)['blog_feed']['id']
        for path,tool,args in [
            ('/api/item/'+doc,'get_item',{'item_id':doc}),
            ('/api/person/'+person['profile_id'],'get_person',{'item_id':person['profile_id']}),
            ('/api/team/'+team['profile_id'],'get_team',{'item_id':team['profile_id']}),
            ('/api/project/'+project['profile_id'],'get_project',{'item_id':project['profile_id']}),
            ('/api/entity/'+person['id'],'get_entity',{'entity_id':person['id']}),
            ('/api/entities','list_entities',{}),('/api/tags','list_tags',{}),
            ('/api/blogs','list_blog_feeds',{}),('/api/blogs/'+feed_id,'get_blog_feed',{'feed_id':feed_id}),
            ('/api/series','list_meeting_series',{}),('/api/series/'+series,'get_meeting_series',{'series_id':series}),
            ('/api/delete-preview/'+doc,'preview_delete_item',{'item_id':doc}),
            ('/api/activity','list_activity',{}),
        ]:
            exact(self.tool(tool,**args),self.get(path))
        self.assertEqual(self.request('/api/source/'+doc)[2],b'UI preserved text')
        self.assertGreaterEqual(self.get('/api/info')['count'],6)
        self.assertIsInstance(self.get('/api/changes'),dict)
        self.assertEqual(self.get('/api/changed-documents'),[])
        self.tool('refresh_document',item_id=doc,
            expected_revision=self.tool('get_item',item_id=doc)['revision'],
            source={'file_id':'ui-dispatch','title':'UI dispatch fixture',
                    'created_time':'2026-01-01T00:00:00Z','modified_time':'2026-02-01T00:00:00Z'},
            original_text='Updated UI source',summary='After UI edit')
        change=self.get('/api/changed-documents')[0]
        self.post('/api/changes-seen',{'item_id':doc,'through_id':change['latest_change_id']})
        self.assertEqual(self.get('/api/changed-documents')[0]['unseen_updates'],0)
        self.post('/api/delete',{'item_id':doc,'expected_revision':self.tool('get_item',item_id=doc)['revision']})
        exact(self.tool('list_trash'),self.get('/api/trash'))
        self.assertEqual(self.get('/api/trash/'+doc)['title'],'UI dispatch fixture')
        self.post('/api/restore',{'item_id':doc})
        self.assertEqual(self.tool('get_item',item_id=doc)['summary'],'After UI edit')

    def test_route_negative_cases_reject_arguments_without_creating_records(self):
        before=self.get('/api/info')['count']
        mutations=['/api/delete','/api/restore','/api/series','/api/entity','/api/link-entity',
                   '/api/apply','/api/import','/api/summary','/api/changes-seen','/api/date',
                   '/api/authors','/api/starred','/api/refresh-request','/api/reviewed']
        for path in mutations:
            status,_,body=self.request(path,{})
            self.assertEqual(status,400,path)
            self.assertIn(b'error',body)
        for prefix in ['/api/person/','/api/team/','/api/project/','/api/entity/','/api/item/',
                       '/api/blogs/','/api/series/','/api/delete-preview/','/api/source/',
                       '/api/attachment/','/api/trash/']:
            self.assertEqual(self.request(prefix+'missing')[0],400,prefix)
        self.assertEqual(self.request('/api/tag',{'name':'','category':'topic'})[0],400)
        self.assertEqual(self.request('/api/items?search_scope=invalid')[0],400)
        self.assertEqual(self.request('/api/trash?search_scope=invalid')[0],400)
        self.assertEqual(self.get('/api/info')['count'],before)
