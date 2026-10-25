"""Pre-cutover audit regressions; all application observations use endpoints.

Filesystem operations only prepare deliberate synthetic input/fault fixtures.
These cases express the retained Python behavior and must pass on Go before cutover.
"""
import json
from harness import EndpointCase, ToolError


class ParityAuditContracts(EndpointCase):
    def test_obsidian_skips_dangling_file_link_and_imports_remaining_notes(self):
        vault = self.service.library.parent / 'audit-vault'
        vault.mkdir()
        (vault / 'good.md').write_text('# Audit good note\nComplete source.\n')
        (vault / 'dangling.md').symlink_to(vault / 'absent.md')
        (vault / 'non-directory.md').symlink_to(vault / 'good.md' / 'child.md')
        (vault / 'loop.md').symlink_to(vault / 'loop.md')
        result = self.tool('import_obsidian', source=str(vault))
        self.assertEqual(result['created'], 1)
        self.assertEqual(result['preserved_files'], 1)
        rows = self.tool('search_items', query='Audit good note', search_scope='title')
        self.assertEqual(len(rows), 1)
        self.assertEqual(self.request('/api/source/' + rows[0]['id'])[2],
                         b'# Audit good note\nComplete source.\n')

    def test_percent_file_uri_links_are_cleaned_and_restored(self):
        target = self.tool('import_item', title='Availability', summary='Target',
            original_text='Complete target source', original_name='Availability 99.9%.md')['id']
        record = self.tool('get_item', item_id=target)
        from pathlib import Path
        summary = 'See [availability](' + Path(record['source_path']).as_uri() + ').'
        inbound = self.tool('import_item', title='Audit inbound', summary=summary)['id']
        preview = self.tool('preview_delete_item', item_id=target)
        self.assertIn(inbound, [x['id'] for x in preview['affected_notes']])
        self.tool('delete_item', item_id=target, expected_revision=record['revision'])
        self.assertEqual(self.tool('get_item', item_id=inbound)['summary'], 'See availability.')
        self.tool('restore_item', item_id=target)
        self.assertEqual(self.tool('get_item', item_id=inbound)['summary'], summary)

    def test_restore_fails_closed_when_inbound_summary_is_missing(self):
        target = self.tool('import_item', title='Audit restore target', summary='Target',
            source_key='audit:restore')['id']
        inbound = self.tool('import_item', title='Audit inbound',
            summary='See [[Audit restore target]].')['id']
        self.tool('delete_item', item_id=target,
            expected_revision=self.tool('get_item', item_id=target)['revision'])
        (self.service.library / 'items' / inbound / 'summary.md').unlink()
        with self.assertRaises(ToolError):
            self.tool('restore_item', item_id=target)
        self.assertTrue(self.tool('get_item', item_id=target, include_deleted=True)['deleted_at'])
        self.assertTrue(self.tool('import_item', title='Ignored', summary='Ignored',
            source_key='audit:restore').get('trashed'))
        # A corrupt existing file must fail the same way, and a repaired file
        # must permit a clean retry with its original link restored.
        summary_path = self.service.library / 'items' / inbound / 'summary.md'
        summary_path.write_bytes(b'Invalid UTF-8 \xff')
        with self.assertRaises(ToolError):
            self.tool('restore_item', item_id=target)
        self.assertTrue(self.tool('get_item', item_id=target, include_deleted=True)['deleted_at'])
        summary_path.write_text('See Audit restore target.')
        result = self.tool('restore_item', item_id=target)
        self.assertEqual(result['restored_links_in_notes'], 1)
        self.assertEqual(self.tool('get_item', item_id=inbound)['summary'],
                         'See [[Audit restore target]].')

    def test_restore_skips_trashed_inbound_and_restores_valid_neighbor(self):
        target = self.tool('import_item', title='Audit linked target', summary='Target')['id']
        neighbors = [self.tool('import_item', title='Audit neighbor ' + str(n),
            summary='See [[Audit linked target]].')['id'] for n in range(2)]
        self.tool('delete_item', item_id=target,
            expected_revision=self.tool('get_item', item_id=target)['revision'])
        self.tool('delete_item', item_id=neighbors[0],
            expected_revision=self.tool('get_item', item_id=neighbors[0])['revision'])
        result = self.tool('restore_item', item_id=target)
        self.assertEqual(result['unchanged_edited_notes'], 1)
        self.assertEqual(result['restored_links_in_notes'], 1)
        self.assertEqual(self.tool('get_item', item_id=neighbors[1])['summary'],
                         'See [[Audit linked target]].')
        self.assertTrue(self.tool('get_item', item_id=neighbors[0], include_deleted=True)['deleted_at'])

    def test_refresh_recovery_rejects_string_version_before_readiness(self):
        target = self.tool('import_item', title='Audit recovery fixture',
            summary='Original summary', original_text='Original source')['id']
        self.service.stop()
        intent = {'version': '1', 'snapshot': 'google-refresh-audit.txt',
                  'old_summary': 'Original summary', 'new_summary': 'Uncommitted summary'}
        (self.service.library / 'items' / target / 'pending-refresh.json').write_text(json.dumps(intent))
        with self.assertRaises(RuntimeError):
            self.service.restart()
        # Correcting only the invalid field permits recovery with saved data
        # intact; rejection must not consume the malformed intent.
        intent['version'] = 1
        (self.service.library / 'items' / target / 'pending-refresh.json').write_text(json.dumps(intent))
        self.service.restart()
        self.assertEqual(self.tool('get_item', item_id=target)['summary'], 'Original summary')
        self.assertEqual(self.request('/api/source/' + target)[2], b'Original source')
