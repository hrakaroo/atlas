"""Compatibility checks for argument boundaries before application mutations."""
from harness import EndpointCase, ToolError, exact


class ArgumentBoundaryContracts(EndpointCase):
    def test_unknown_mutation_arguments_reject_before_changes_and_legacy_wrappers_ignore(self):
        ident=self.tool("import_item",title="Argument fixture",summary="Summary",kind="document",
            original_text="Full source",source_key="arguments:fixture")["id"]
        before=self.tool("get_item",item_id=ident)
        events=self.tool("list_activity")
        for key in ("unexpected", "", "item_id starred", " expected_revision "):
            for name,args in [
                ("set_starred",dict(item_id=ident,starred=True)),
                ("update_summary",dict(item_id=ident,summary="Must not write",expected_revision=before["revision"])),
                ("set_date",dict(item_id=ident,date="2026-01-01",expected_revision=before["revision"])),
            ]:
                with self.assertRaises(ToolError,msg=(name,key)):
                    self.tool(name,**dict(args,**{key:True}))
                exact(before,self.tool("get_item",item_id=ident))
                exact(events,self.tool("list_activity"))
        # Legacy dispatch deliberately drops arguments for these four wrappers.
        for name in ("list_tags","list_entities","list_blog_feeds","list_meeting_series"):
            exact(self.tool(name),self.tool(name,**{"":False,"ignored":True}))
