"""Structured mention identities over public endpoints; prose is never identity proof."""
from test_workflows import WorkflowCase
from harness import ToolError


def chip(name='Jane Doe', email='jane@example.com', ident='chip-jane'):
    return dict(name=name, email=email, role='mentioned',
                person_chip=dict(personId=ident, personProperties=dict(name=name, email=email)))


class StructuredMentionContracts(WorkflowCase):
    def test_plain_names_and_unproven_emails_reject_without_creating_items_or_people(self):
        before = self.tool('list_activity')
        for person in [dict(name='Johnny Smith', role='mentioned', evidence='Named in prose'),
                       dict(name='John Smith', email='john@example.com', role='mentioned', evidence='Named in prose')]:
            with self.assertRaises(ToolError):
                self.tool('import_item', title='Unproven mention', kind='document', summary='Summary',
                          original_text='John Smith and Johnny Smith are typed text.', document_people=[person])
        self.assertEqual(self.tool('search_items'), [])
        self.assertEqual(self.tool('list_activity'), before)

    def test_invalid_structured_batch_rejects_before_any_profile_or_link(self):
        ident = self.doc()
        before = self.item(ident)
        variants = [dict(chip(), email='other@example.com'),
                    dict(chip(), name='Johnny Doe'),
                    dict(chip(), person_chip={'personProperties':{'email':'jane@example.com'}}),
                    dict(name='Wrong Person',email='wrong@example.com',role='mentioned',person_link='https://example.com/person/wrong'),
                    dict(name='Wrong Person',email='wrong@example.com',role='mentioned',person_link='mailto:other@example.com')]
        for invalid in variants:
            with self.assertRaises(ToolError):
                self.tool('link_document_people',item_id=ident,people=[chip('Valid Person','valid@example.com','valid-chip'),invalid])
        self.assertEqual(self.tool('search_items', collection='people'), [])
        self.assertEqual(self.item(ident), before)

    def test_chips_match_email_across_spellings_and_conflicting_names_are_unresolved(self):
        first, second = self.doc('first'), self.doc('second')
        self.tool('link_document_people',item_id=first,people=[chip()])
        self.tool('link_document_people',item_id=second,people=[chip('Janey Doe')])
        person = self.item(first)['document_people'][0]
        self.assertEqual(self.item(second)['document_people'][0]['id'],person['id'])
        self.assertEqual(self.item(second)['document_people'][0]['name'],'Jane Doe')
        profile = self.tool('get_person',item_id=person['id'])
        self.assertEqual({d['id'] for d in profile['mentioned_documents']},{first,second})
        result = self.tool('link_document_people',item_id=second,people=[chip('Jane Doe','different@example.com','different-chip')])
        self.assertIn('Conflicting identity',str(result))
        self.assertEqual(len(self.tool('search_items', collection='people')),1)

    def test_duplicate_chip_email_in_batch_reuses_profile_and_mailto_links_are_supported(self):
        ident = self.doc()
        self.tool('link_document_people',item_id=ident,people=[chip(),chip('Janey Doe')])
        self.assertEqual(len(self.item(ident)['document_people']),1)
        self.assertEqual(len(self.tool('search_items',collection='people')),1)
        self.tool('link_document_people',item_id=ident,people=[dict(name='Alex Taylor',email='alex@example.com',role='mentioned',person_link='mailto:alex%40example.com')])
        self.assertEqual({p['name'] for p in self.item(ident)['document_people']},{'Jane Doe','Alex Taylor'})
        self.assertEqual(self.item(ident)['authors'],[])
        self.restart()
        self.assertEqual({p['email'] for p in self.item(ident)['document_people']},{'jane@example.com','alex@example.com'})

    def test_unknown_chip_without_name_does_not_guess_name_or_restore_removed_profile(self):
        ident = self.doc()
        nameless = chip(); del nameless['person_chip']['personProperties']['name']
        self.tool('link_document_people',item_id=ident,people=[nameless])
        self.assertEqual(self.tool('search_items',collection='people'),[])
        self.tool('link_document_people',item_id=ident,people=[chip()])
        person = self.item(ident)['document_people'][0]['id']
        self.tool('delete_item',item_id=person,expected_revision=self.item(person)['revision'])
        result=self.tool('link_document_people',item_id=ident,people=[chip('Different Display Name')])
        self.assertIn('Trash',str(result))
        self.assertEqual(self.tool('search_items',collection='people'),[])

    def test_intake_rejects_plain_names_and_accepts_structured_chip_with_full_source(self):
        queued=self.queue(self.google())
        with self.assertRaises(ToolError):
            self.tool('complete_intake',intake_id=queued,source=self.metadata(),payload=dict(summary='Summary',original_text='Typed John Smith',document_people=[dict(name='John Smith',role='mentioned',evidence='Prose')]))
        self.assertEqual(self.tool('search_items',collection='people'),[])
        result=self.tool('complete_intake',intake_id=queued,source=self.metadata(),payload=dict(summary='Summary',original_text='Typed John Smith and a linked person.',document_people=[chip()]))
        self.assertEqual([p['name'] for p in self.item(result['id'])['document_people']],['Jane Doe'])
        self.assertEqual(self.source(result['id']),b'Typed John Smith and a linked person.')
