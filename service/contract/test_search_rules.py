"""Observable combined-filter and within-tier ordering contracts."""
from harness import EndpointCase


class SearchContracts(EndpointCase):
    def create(self,title,key,date='2026-01-01',kind='document',tags=None,starred=False):
        ident=self.tool('import_item',title=title,kind=kind,date=date,summary='Summary',
            original_text='Preserved source',source_key='search:'+key,tags=tags or [])['id']
        if starred:
            self.tool('set_starred',item_id=ident,starred=True)
        return ident

    def test_multiple_tag_filters_combine_with_query_kind_and_star_using_AND(self):
        tags=[{'name':'First Topic','category':'topic'},{'name':'Second Topic','category':'topic'}]
        wanted=self.create('Roadmap wanted','wanted',tags=tags,starred=True)
        self.create('Roadmap missing tag','missing',tags=tags[:1],starred=True)
        self.create('Roadmap unstarred','unstarred',tags=tags)
        self.create('Roadmap meeting','meeting',kind='meeting',tags=tags,starred=True)
        by_name={tag['name']:tag['id'] for tag in self.tool('list_tags')}
        ids=[by_name[tag['name']] for tag in tags]
        query='/api/items?q=Roadmap&search_scope=title&kind=document&starred=1'+''.join('&tag='+ident for ident in ids)
        self.assertEqual([item['id'] for item in self.get(query)],[wanted])
        self.assertEqual([item['id'] for item in self.tool('search_items',query='Roadmap',
            tag_ids=ids,kind='document',starred_only=True)],[wanted])
        self.post('/api/apply',{'item_ids':[wanted],'tag_id':ids[1],'remove':True})
        self.assertEqual(self.get(query),[])

    def test_stars_dates_and_title_order_are_preserved_within_same_title_match_tier(self):
        oldest=self.create('Roadmap Zeta','old','2026-01-01',starred=True)
        middle=self.create('Roadmap Alpha','mid','2026-02-01')
        newest=self.create('Roadmap Beta','new','2026-03-01')
        self.assertEqual([item['id'] for item in self.tool('search_items',query='Roadm',sort_by='date')],
                         [oldest,newest,middle])
        self.assertEqual([item['id'] for item in self.get('/api/items?q=Roadm&search_scope=title&sort=title')],
                         [oldest,middle,newest])
        self.tool('set_starred',item_id=oldest,starred=False)
        self.assertEqual([item['id'] for item in self.get('/api/items?q=Roadm&search_scope=title&sort=date')],
                         [newest,middle,oldest])
