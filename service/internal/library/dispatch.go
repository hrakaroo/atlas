package library

import (
	"errors"
	"fmt"
	"strings"

	"atlas.local/atlas/service/internal/transport"
)

func required(args Record, key string) (any, error) {
	value, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("Missing required argument: %s", key)
	}
	return value, nil
}
func requiredString(args Record, key string) (string, error) {
	value, err := required(args, key)
	if err != nil {
		return "", err
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be text.", key)
	}
	return text, nil
}
func boolArgument(args Record, key string, def bool) (bool, error) {
	value, ok := args[key]
	if !ok {
		return def, nil
	}
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be true or false.", key)
	}
	return b, nil
}
func revision(args Record) (int64, error) {
	value, err := required(args, "expected_revision")
	if err != nil {
		return 0, err
	}
	return integer(value)
}

// Dispatch is the sole public application boundary. Nested methods share this
// operation's lock and connection, including reads that reconcile saved Markdown.
func (l *Library) Dispatch(name string, args map[string]any) (any, error) {
	if err := l.Store.Lock(); err != nil {
		return nil, err
	}
	defer l.Store.Unlock()
	if l.recoveryErr != nil {
		return nil, errors.New("Atlas recovery is incomplete. Resolve the saved intent and restart the service before further operations.")
	}
	if args == nil {
		args = Record{}
	}
	if err := checkArguments(name, args); err != nil {
		return nil, err
	}
	switch name {
	case "info":
		row, err := l.row("SELECT count(*) AS count FROM items WHERE deleted_at IS NULL")
		if err != nil {
			return nil, err
		}
		return Record{"library": l.Store.Root, "count": row["count"]}, nil
	case "get_item":
		id, err := requiredString(args, "item_id")
		if err != nil {
			return nil, err
		}
		return l.Get(id, truth(args["include_deleted"]), truth(args["lightweight"]))
	case "import_item":
		return l.Ingest(args)
	case "search_items":
		return l.Listing(args)
	case "list_tags":
		return l.Tags()
	case "tag":
		id, err := l.Tag(str(args, "name"), strDefault(args, "category", "topic"))
		return Record{"id": id}, err
	case "apply_tag", "link_entity":
		ids, err := stringsArg(args["item_ids"])
		if err != nil {
			return nil, err
		}
		remove, err := boolArgument(args, "remove", false)
		if err != nil {
			return nil, err
		}
		if name == "link_entity" {
			return l.LinkEntity(ids, str(args, "entity_id"), remove)
		}
		return l.ApplyTags(ids, str(args, "tag_id"), remove)
	case "undo":
		return l.Undo()
	case "preview_delete_item":
		return l.DeletionPreview(str(args, "item_id"))
	case "delete_item":
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		return l.DeleteItem(str(args, "item_id"), rev)
	case "restore_item":
		return l.RestoreItem(str(args, "item_id"))
	case "update_summary":
		id, err := requiredString(args, "item_id")
		if err != nil {
			return nil, err
		}
		summary, err := requiredString(args, "summary")
		if err != nil {
			return nil, err
		}
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		return l.UpdateSummary(id, summary, rev)
	case "set_date":
		id, err := requiredString(args, "item_id")
		if err != nil {
			return nil, err
		}
		date, err := requiredString(args, "date")
		if err != nil {
			return nil, err
		}
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		return l.SetDate(id, date, rev)
	case "set_starred", "set_reviewed":
		id, err := requiredString(args, "item_id")
		if err != nil {
			return nil, err
		}
		key := "starred"
		def := false
		if name == "set_reviewed" {
			key = "reviewed"
			def = true
		} else if _, err = required(args, key); err != nil {
			return nil, err
		}
		value, err := boolArgument(args, key, def)
		if err != nil {
			return nil, err
		}
		if name == "set_reviewed" {
			return l.SetReviewed(id, value)
		}
		return l.SetStarred(id, value)
	case "get_source", "get_attachment", "get_history_content":
		id, err := requiredString(args, "item_id")
		if err != nil {
			return nil, err
		}
		var data []byte
		switch name {
		case "get_source":
			data, err = l.Source(id)
		case "get_attachment":
			data, err = l.Attachment(id, str(args, "target"))
		case "get_history_content":
			data, err = l.HistoryContent(id, str(args, "entry_id"))
		}
		if err != nil {
			return nil, err
		}
		return transport.Raw{Bytes: data, MIME: "text/plain; charset=utf-8"}, nil
	case "get_history":
		return l.History(str(args, "item_id"))
	case "list_activity":
		limit := int64(50)
		var err error
		if value, ok := args["limit"]; ok {
			limit, err = integer(value)
			if err != nil {
				return nil, err
			}
		}
		return l.Activity(limit, args["before_id"])
	case "changes":
		return l.Changes()
	case "changed_documents":
		return l.ChangedDocuments(args)
	case "mark_document_changes_seen":
		through, err := integer(args["through_id"])
		if err != nil {
			return nil, err
		}
		return l.MarkDocumentChangesSeen(str(args, "item_id"), through)
	case "list_entities":
		return l.Entities()
	case "get_entity":
		return l.GetEntity(str(args, "entity_id"))
	case "create_entity":
		return l.CreateEntity(str(args, "name"), str(args, "kind"))
	case "get_team":
		return l.GetTeam(str(args, "item_id"))
	case "set_authors":
		value, err := required(args, "authors")
		if err != nil {
			return nil, err
		}
		authors, err := stringsArg(value)
		if err != nil {
			return nil, errors.New("Authors must be a list of names.")
		}
		return l.SetAuthors(str(args, "item_id"), authors)
	case "configure_feed":
		return l.ConfigureFeed(args)
	case "upsert_feed_entry":
		return l.UpsertFeedEntry(args)
	case "record_feed_check":
		return l.RecordFeedCheck(str(args, "feed_id"))
	case "mark_feed_seen":
		through, err := integer(args["through_id"])
		if err != nil {
			return nil, err
		}
		return l.MarkFeedSeen(str(args, "feed_id"), through)
	case "list_blog_feeds":
		return l.BlogFeeds()
	case "get_blog_feed":
		return l.GetBlogFeed(str(args, "feed_id"))
	case "assign_meeting_series":
		return l.AssignSeries(str(args, "item_id"), str(args, "title"), str(args, "cadence"))
	case "list_meeting_series":
		return l.SeriesList()
	case "get_meeting_series":
		return l.GetSeries(str(args, "series_id"))
	case "get_person":
		return l.GetPerson(str(args, "item_id"))
	case "get_project":
		return l.GetProject(str(args, "item_id"))
	case "link_interview":
		value, err := required(args, "candidates")
		if err != nil {
			return nil, err
		}
		candidates, err := stringsArg(value)
		if err != nil {
			return nil, err
		}
		return l.LinkInterview(str(args, "item_id"), candidates)
	case "link_meeting_people":
		value, err := required(args, "participants")
		if err != nil {
			return nil, err
		}
		participants, err := recordsArg(value)
		if err != nil {
			return nil, err
		}
		return l.LinkMeetingPeople(str(args, "item_id"), participants, args["expected_revision"], mapping(args["calendar_source"]))
	case "link_document_people":
		value, err := required(args, "people")
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, errors.New("Supply people with owner, creator or mentioned roles and explicit evidence.")
		}
		people, err := recordsArg(value)
		if err != nil {
			return nil, err
		}
		roles, err := stringsArg(args["replace_roles"])
		if err != nil {
			return nil, err
		}
		if value, exists := args["ownership_status"]; exists && value != nil && str(args, "ownership_status") == "" {
			return nil, errors.New("Unknown ownership status.")
		}
		return l.LinkDocumentPeople(str(args, "item_id"), people, str(args, "ownership_status"), roles)
	case "list_google_documents":
		ids, err := stringsArg(args["recent_file_ids"])
		if err != nil {
			return nil, err
		}
		return l.ListGoogleDocuments(strDefault(args, "mode", "all"), ids)
	case "set_refresh_requested":
		value, err := required(args, "requested")
		if err != nil {
			return nil, err
		}
		requested, ok := value.(bool)
		if !ok {
			return nil, errors.New("Refresh requested must be true or false.")
		}
		return l.SetRefreshRequested(str(args, "item_id"), requested)
	case "check_document_source":
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		source, err := objectArgument(args, "source", true)
		if err != nil {
			return nil, err
		}
		return l.CheckDocumentSource(str(args, "item_id"), rev, source)
	case "inspect_document_content":
		text, err := requiredString(args, "original_text")
		if err != nil {
			return nil, err
		}
		return l.InspectDocumentContent(str(args, "item_id"), text)
	case "refresh_document":
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		source, err := objectArgument(args, "source", true)
		if err != nil {
			return nil, err
		}
		text, err := requiredString(args, "original_text")
		if err != nil {
			return nil, err
		}
		return l.RefreshDocument(str(args, "item_id"), rev, source, text, args["summary"])
	case "list_ownership_lookups":
		limit := int64(3)
		var err error
		if value, ok := args["limit"]; ok {
			limit, err = integer(value)
			if err != nil {
				return nil, err
			}
		}
		return l.ListOwnershipLookups(limit)
	case "record_ownership_lookup":
		rev, err := revision(args)
		if err != nil {
			return nil, err
		}
		source, err := objectArgument(args, "source", false)
		if err != nil {
			return nil, err
		}
		unavailable, err := boolArgument(args, "provider_unavailable", false)
		if err != nil {
			return nil, err
		}
		return l.RecordOwnershipLookup(str(args, "item_id"), rev, source, args["error"], unavailable)
	case "submit_sources":
		value, err := required(args, "sources")
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, errors.New("Submit a list of sources.")
		}
		sources, err := recordsArg(value)
		if err != nil {
			return nil, errors.New("Submit a list of sources.")
		}
		return l.SubmitSources(sources)
	case "list_intake":
		limit := int64(5)
		var err error
		if value, ok := args["limit"]; ok {
			limit, err = integer(value)
			if err != nil {
				return nil, err
			}
		}
		return l.ListIntake(args["source_type"], limit)
	case "get_intake":
		return l.GetIntake(str(args, "intake_id"))
	case "complete_intake":
		payload, err := objectArgument(args, "payload", true)
		if err != nil {
			return nil, err
		}
		source, err := objectArgument(args, "source", false)
		if err != nil {
			return nil, err
		}
		return l.CompleteIntake(str(args, "intake_id"), payload, source, args["expected_source_hash"])
	case "list_trash":
		return l.TrashList(str(args, "query"), strDefault(args, "search_scope", "full_text"))
	case "backup":
		return l.Backup()
	case "import_obsidian":
		return l.ImportObsidian(str(args, "source"))
	default:
		return nil, fmt.Errorf("Operation %q is not implemented in this migration build", name)
	}
}

var allowedArguments = map[string]string{

	"get_person": "item_id", "get_project": "item_id", "link_interview": "item_id candidates", "link_meeting_people": "item_id participants expected_revision calendar_source",
	"link_document_people": "item_id people ownership_status replace_roles", "list_google_documents": "mode recent_file_ids", "set_refresh_requested": "item_id requested",
	"check_document_source": "item_id expected_revision source", "inspect_document_content": "item_id original_text", "refresh_document": "item_id expected_revision source original_text summary",
	"list_ownership_lookups": "limit", "record_ownership_lookup": "item_id expected_revision source error provider_unavailable",
	"submit_sources": "sources", "list_intake": "source_type limit", "get_intake": "intake_id", "complete_intake": "intake_id payload source expected_source_hash",
	"list_trash": "query search_scope", "backup": "",
	"info": "", "get_item": "item_id include_deleted lightweight", "import_item": "title summary kind date tags source_key source_path source_text metadata period_start period_end source_channel original_text original_name series_title series_cadence calendar_url doc_urls blog_feed projects authors candidates explicit automatic participants calendar_source document_people",
	"search_items": "query tag_ids kind inbox series_id blog_feed_id starred_only sort_by stars_first collection search_scope",
	"list_tags":    "", "tag": "name category", "apply_tag": "item_ids tag_id remove", "link_entity": "item_ids entity_id remove", "undo": "",
	"update_summary": "item_id summary expected_revision", "set_date": "item_id date expected_revision", "set_starred": "item_id starred", "set_reviewed": "item_id reviewed",
	"get_source": "item_id", "get_attachment": "item_id target", "get_history": "item_id", "get_history_content": "item_id entry_id",
	"list_activity": "limit before_id", "changes": "", "changed_documents": "query search_scope tag_ids", "mark_document_changes_seen": "item_id through_id",
	"list_entities": "", "get_entity": "entity_id", "create_entity": "name kind", "get_team": "item_id", "set_authors": "item_id authors",
	"configure_feed": "name flow presentation entry_label", "upsert_feed_entry": "feed_id entry_key title summary original_text original_name source_url observed_at date fields source_context expected_revision", "record_feed_check": "feed_id", "mark_feed_seen": "feed_id through_id", "list_blog_feeds": "", "get_blog_feed": "feed_id", "assign_meeting_series": "item_id title cadence", "list_meeting_series": "", "get_meeting_series": "series_id",
	"preview_delete_item": "item_id", "delete_item": "item_id expected_revision", "restore_item": "item_id",
}

func checkArguments(name string, args Record) error {
	// These legacy tool wrappers call no-argument methods and discard supplied arguments.
	switch name {
	case "list_tags", "list_entities", "list_blog_feeds", "list_meeting_series":
		return nil
	}

	allowed, known := allowedArguments[name]
	if !known {
		return nil
	}
	fields := map[string]bool{}
	for _, key := range strings.Fields(allowed) {
		fields[key] = true
	}
	for key := range args {
		if !fields[key] {
			return fmt.Errorf("Unexpected argument: %s", key)
		}
	}
	return nil
}

func objectArgument(args Record, key string, require bool) (Record, error) {
	value, exists := args[key]
	if !exists && require {
		return nil, fmt.Errorf("Missing required argument: %s", key)
	}
	if !exists || value == nil {
		if require {
			return nil, fmt.Errorf("%s must be an object.", key)
		}
		return nil, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object.", key)
	}
	return object, nil
}
