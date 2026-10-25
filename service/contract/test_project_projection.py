"""Project detail keeps the existing per-kind endpoint projections."""
from harness import EndpointCase


class ProjectProjectionContracts(EndpointCase):
    def test_project_mixed_kinds_hydrate_only_documents(self):
        project = self.tool('create_entity', name='Mixed Project', kind='project')
        buckets = {'document':'documents', 'reference':'documents',
                   'meeting':'meetings', 'one-on-one':'meetings',
                   'update':'updates', 'blog':'updates', 'note':'other_notes'}
        created = {}
        for kind in buckets:
            result = self.tool('import_item', title='Mixed '+kind, kind=kind,
                summary='## Blurb\nSupported '+kind+' summary.\n[[Mixed Project|Project link]]',
                source_key='synthetic:mixed-project:'+kind, date='2026-10-15',
                period_start='2026-10-12', period_end='2026-10-19',
                original_text='Complete original '+kind, projects=['Mixed Project'])
            created[result['id']] = kind
        listing = {item['id']:item for item in self.tool('search_items', tag_ids=[project['id']])}
        detail = self.tool('get_project', item_id=project['profile_id'])
        self.assertEqual(detail['id'], project['profile_id'])
        self.assertEqual(detail['title'], 'Mixed Project')
        observed = {}
        for bucket in set(buckets.values()):
            for item in detail[bucket]:
                ident = item['id']
                self.assertNotIn(ident, observed)
                observed[ident] = bucket
                kind = created[ident]
                self.assertEqual(bucket, buckets[kind])
                if bucket == 'documents':
                    self.assertEqual(item, self.tool('get_item', item_id=ident))
                    self.assertIn('summary', item)
                    self.assertNotIn('preview', item)
                    self.assertTrue(item['links'])
                else:
                    self.assertEqual(item, listing[ident])
                    self.assertNotIn('summary', item)
                    self.assertIn('preview', item)
                    self.assertEqual(item['links'], [])
        self.assertEqual(set(observed), set(created))
