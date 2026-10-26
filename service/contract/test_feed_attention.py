"""Feed-level arrivals, races, deletion and restart through public endpoints."""
from harness import EndpointCase, ToolError

class FeedAttentionContracts(EndpointCase):
    def edition(self,key,feed='Example feed',date='2026-10-25'):
        return self.tool('import_item',title=key,kind='blog',blog_feed=feed,date=date,
            summary='## Blurb\nBrief source-grounded summary.',original_text='Complete source '+key,
            source_key='contract:feed:'+key)
    def feed(self,name='Example feed'):
        return next(f for f in self.get('/api/blogs') if f['name']==name)
    def test_attention_is_per_feed_durable_and_newer_arrivals_survive_stale_ack(self):
        first=self.edition('first');f=self.feed();cursor=f['latest_entry_id']
        self.assertEqual(f['unseen_entries'],1)
        full=self.tool('get_blog_feed',feed_id=f['id'])
        self.assertEqual(full['latest_entry_id'],cursor)
        self.assertEqual(self.feed()['unseen_entries'],1) # A scheduled read is never a review.
        self.edition('second')
        result=self.post('/api/feed-seen',dict(feed_id=f['id'],through_id=cursor))
        self.assertEqual(result['unseen_entries'],1)
        self.assertEqual(self.feed()['unseen_entries'],1)
        self.restart();f=self.feed();self.assertEqual(f['unseen_entries'],1)
        self.tool('mark_feed_seen',feed_id=f['id'],through_id=f['latest_entry_id'])
        self.assertEqual(self.feed()['unseen_entries'],0)
        self.edition('ai','AI');self.assertEqual(self.feed('AI')['unseen_entries'],1)
        self.assertEqual(self.feed()['unseen_entries'],0)
        # Edits, date changes and duplicate calls cannot create arrivals.
        self.tool('set_starred',item_id=first['id'],starred=True)
        item=self.tool('get_item',item_id=first['id'])
        self.tool('update_summary',item_id=first['id'],expected_revision=item['revision'],summary='Edited summary')
        self.assertTrue(self.edition('first')['duplicate'])
        self.assertEqual(self.feed()['unseen_entries'],0)
        self.edition('backdated',date='2020-01-01')
        self.assertEqual(self.feed()['unseen_entries'],1)
    def test_deleted_entries_and_invalid_or_foreign_cursors(self):
        first=self.edition('removed');f=self.feed();old=f['latest_entry_id']
        other=self.edition('other','Other feed');otherfeed=self.feed('Other feed')
        for cursor in (0,-1,otherfeed['latest_entry_id'],99999):
            with self.assertRaises(ToolError):self.tool('mark_feed_seen',feed_id=f['id'],through_id=cursor)
        item=self.tool('get_item',item_id=first['id'])
        self.tool('delete_item',item_id=first['id'],expected_revision=item['revision'])
        self.assertEqual(self.feed()['unseen_entries'],0)
        self.assertEqual(self.feed()['latest_entry_id'],0)
        new=self.edition('new');self.assertGreater(self.feed()['latest_entry_id'],old)
        self.assertEqual(self.feed()['unseen_entries'],1)
        self.tool('mark_feed_seen',feed_id=f['id'],through_id=self.feed()['latest_entry_id'])
        self.restart();self.assertEqual(self.feed()['unseen_entries'],0)

    def test_same_day_posts_use_source_publication_time_and_seen_does_not_reorder(self):
        ids=[]
        for key,hour in [('newest','23'),('oldest','10'),('middle','15')]:
            ids.append(self.tool('import_item',title=key,kind='blog',blog_feed='Example feed',date='2026-10-25',summary='Brief summary',original_text='Text '+key,source_key='contract:order:'+key,
                metadata={'feed_source':{'created_at':'2026-10-25T'+hour+':00:00Z'}})['id'])
        f=self.feed();before=self.tool('get_blog_feed',feed_id=f['id'])
        self.assertEqual(before['latest']['id'],ids[0])
        self.assertEqual([i['id'] for i in before['editions']],[ids[0],ids[2],ids[1]])
        self.tool('mark_feed_seen',feed_id=f['id'],through_id=f['latest_entry_id'])
        after=self.tool('get_blog_feed',feed_id=f['id'])
        self.assertEqual([i['id'] for i in after['editions']],[i['id'] for i in before['editions']])
        self.assertEqual(after['unseen_entries'],0)
