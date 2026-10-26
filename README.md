# Atlas

Atlas is a private local macOS library for documents, meetings, projects, work contacts, interviews and feeds.

## Motivation

AI makes it easier to create documents, capture meetings, and generate summaries. The harder problem is keeping that growing body of information organized and useful.

Atlas starts with the material you already have. It brings documents and meeting transcripts into a local library, connects them to people and projects, and makes them easier to find and revisit. It’s built around organizing your work, with no separate note-taking habit required.

## Documentation

- [Application specification](docs/specification.md): purpose, collections, browsing, imports, relationships, updates and feed behavior.
- [Service validation status](service/STATUS.md): accepted checks and supported platforms.

- [Architecture](docs/architecture.md): processes, data ownership and transports.
- [Repository and fresh installations](docs/repository.md): safe commits, excluded data and setup for another person.
- [Operations](docs/operations.md): install, upgrade, diagnose, back up and move Atlas.
- [Scheduled tasks](schedules/README.md): seven registered jobs (six active), source responsibilities, private prompt snapshots and recreation steps.
- [Implementation rules](AGENTS.md): contributor constraints.

## Run and architecture

Atlas uses one shared background service written in Go for the native window, HTTP API, MCP tools and automation helpers. The service is the only production process that opens SQLite and manages saved library files. The native Swift window and helpers connect to the configured service.

The installed deployment uses `dist/Atlas.app` and its bundled Go service. Its database and saved files remain in `library/`. Native clients and scheduled collectors use authenticated HTTP; the service owns all SQLite access.

For a fresh installation, install Go, Python 3 (for the client/build tools) and macOS command-line tools, and configure a reusable signing certificate as described in [operations](docs/operations.md). Then:

```sh
(cd service && go mod download)
zsh build.sh
mkdir -p "$HOME/Documents/Atlas/library"
/usr/bin/python3 scripts/service.py request-access --library "$HOME/Documents/Atlas/library" --app "$PWD/build/Atlas.app"
/usr/bin/python3 scripts/service.py install --library "$HOME/Documents/Atlas/library" --app "$PWD/build/Atlas.app" --owner-name 'Your Full Name' --timezone America/Los_Angeles
/usr/bin/python3 scripts/service.py configure-mcp --library "$HOME/Documents/Atlas/library"
```

Use the same explicit library for all clients when it differs from the project default. Open `build/Atlas.app` for that fresh setup; the existing installed app remains `dist/Atlas.app`. The build never installs, registers or replaces the running `dist` deployment. Changes to the selected Go toolchain may require intentionally regenerating third-party notices before packaging.

macOS launchd runs the installed Atlas service job at login and restarts it if it exits. The signed native supervisor starts one Go service process. Closing the window or ChatGPT does not stop it. Requests execute serially, and a per-library lifetime lock prevents a competing owner.

The service listens only at `http://127.0.0.1:47831`; MCP uses `/mcp`. Both interfaces require authentication. The credential and library identity are stored in `library/.service/connection.json` with user-only permissions; do not publish this file. Startup logs never include the credential. Browser resources use a SameSite/HttpOnly cookie and API headers; MCP requires an explicit authentication header and checks request origins/hosts. No public tunnel is installed.

Service management:

```sh
/usr/bin/python3 scripts/service.py status
/usr/bin/python3 scripts/service.py restart
/usr/bin/python3 scripts/service.py stop
/usr/bin/python3 scripts/service.py start
```

Go backend and web changes require rebuilding/signing/testing a candidate and deliberately deploying it before restarting. The Go service embeds its web assets. Rebuild/reopen the native app for wrapper changes. Logs live in `library/.service/stdout.log` and `stderr.log`; the launch agent definition is in `~/Library/LaunchAgents/` under the installed service label. If macOS denies background access to the project in Documents, allow Documents access for Atlas Service in System Settings → Privacy & Security → Files & Folders during service setup. A failed installer leaves the existing connector registration unchanged.

The backend runs the bundled Go executable. Collectors, lifecycle tools and HTTP clients are Python scripts.

Atlas has no independent external-account credentials or AI runtime. Scheduled desktop workflows use connected Google, Confluence, Slack and web tools as required, generate source-grounded summaries, and submit results to Atlas. Local browsing needs no external account; source collection needs the corresponding connections and available desktop runtime.

## Go service and builds

The installed Go backend uses `dist/Atlas.app`. Its source lives in `service/`, and the executable embeds the web interface. The native window always requires the shared service; HTTP clients fail closed during an outage.

`zsh build-go.sh --go-service /absolute/path/to/atlas-service` creates a signed preview at `service/.work/build/Atlas.app`. It does not launch, register or install it. The preview has a separate bundle identity and requires a configured running service. See [isolated preview operations](docs/operations.md#isolated-go-preview) for validation and deployment safeguards.

The native window selects its library from `--library /absolute/path`, then `ATLAS_LIBRARY`, then the current user's `~/Documents/Atlas/library`. The final default preserves the existing installation location without compiling a username into the app. For a clone elsewhere, pass the same explicit library to both the window and service tools. The backend's owner and timezone are explicit Go service configuration.

## Project layout and local data

| Location | Purpose |
| --- | --- |
| `app/Atlas.swift` | Native window, shared-service connection and library path |
| `service/internal/library/` | Active Go Library API, identity, relationships, intake, history and recovery |
| `service/cmd/atlas-service/` | Active Go service entry point and embedded web serving |
| `app/service_client.py` | Authenticated HTTP-only shared-service client |
| `scripts/service.py` | Service installation/lifecycle, health and local MCP registration |
| `docs/` | Application specification, architecture, workflows and operations runbooks |
| `web/` | Browser interface; no external scripts or network assets |
| `scripts/collect_sources.py` | Chrome and Zoom collectors |
| `scripts/chrome_documents.py` | Reader for Chrome’s saved session snapshot |
| `scripts/atlas_tool.py` | Shared-service tool client for scheduled work |
| `schedules/` | Registered-task snapshot, full prompts and setup and operating guide |
| `library/library.sqlite` | People, Teams and Projects with descriptions and history; item identities, search, relationships, queues, tombstones and activity |
| `library/items/<id>/` | Document and meeting summaries, cached originals, transcripts, attachments and source history |
| `library/backups/` | Consistent database and content backups |

All library mutations must enter through Library. Do not edit SQLite or library files from automation scripts. Summary edits preserve original sources and manual organization. People, Teams and Projects are database-backed and need no item folders. External document/meeting summary Markdown changes are reconciled during background checks; avoid racing application writes.

Back up database and content together using Atlas’s backup action. Do not synchronize an open SQLite database through a file-sync service. Library data, backups, OAuth tokens, local session files and credentials are private material and should not be included in a source-code repository. The machine-specific manifest and private briefing/blog/incident prompts are ignored by Git. Review other exported instructions before publishing; the public source repository must not include private workflow configuration.

Builds use the reusable Keychain code-signing identity named `Atlas Local Development` for both the app and service helper, with no silent ad-hoc fallback. See [stable local signing](docs/operations.md#stable-signing-for-local-builds) for setup and permission checks.

The app bundle includes the Atlas icon for Finder and the Dock even while closed. Builds refresh and verify the bundle without registering it; opening the selected app or requesting native access performs ordinary macOS registration. If a previously pinned shortcut keeps an old/generic icon, remove that Dock shortcut and drag the rebuilt Atlas.app into the Dock once.

The native window uses `--library`, then `ATLAS_LIBRARY`, then the current user’s `~/Documents/Atlas/library`. Selecting a different library does not require editing Swift source; use one consistent explicit root for the window, service and collectors. Relocating existing data is separate: preserved paths are absolute, and cross-root data migration requires a reviewed path-rebase procedure; current backup restore acceptance uses the original root.

## Browsing and relationships

The sidebar contains Documents, Projects, People, Meetings, Interviews, Feeds, Activity and Trash. Inbox and Weekly updates navigation are hidden; existing status records remain searchable.

- Documents are stored once and may belong to several projects. Projects show connected documents and meetings.
- People are work contacts. Authors, document owners, explicitly identified creators, mentioned people and confirmed meeting participants have separate relationships. Google-document mentions require an actual people chip or explicit email link from structured content; typed names do not create mention links.
- Teams and Projects are entities; topic/type tags are separate. Legacy typed-tag APIs remain compatibility adapters.
- Interviews contains candidate profiles and interview sessions. Interview documents can also remain in Documents; candidates are excluded from colleague People browsing.
- Confirmed work one-on-ones automatically group by colleague identity. Other recurring meetings can use Meeting series…; dates remain separate full sessions, newest first.
- Feeds supports separate briefing/article entries and ongoing entries updated in place. The configured feeds cover AI briefings, author blogs and an incident overview with a clickable severity/status index.

A document title opens its cached Atlas summary. In a person, project or team’s right-hand document list, clicking keeps that collection visible and loads the document in the center. Meetings in a person’s column behave the same way: their summary opens in the center, retaining the profile’s documents and meetings. The current document or meeting is highlighted, a named link returns to the profile, and Open in Documents or Open in Meetings switches deliberately to its full view. Back/Forward restore the collection, selected document and scroll positions. Open original Google Doc and the separate ↗ shortcuts open Google. Other source references appear in collapsed Referenced documents/meetings sections; names and dates come from preserved evidence, not background Google requests.

The reader has one Edit summary/Summary toggle. Saving is explicit, and drafts survive preview switching. More… contains View saved original and Remove…. Original sources remain available for verification.

Stars persist locally. Ordinary lists prioritize stars within search relevance tiers; meeting and feed histories remain newest first. Dates use supported source dates or the original Atlas added date, labeled Added; manual corrections preserve the original creation timestamp.

## Search and automatic display updates

Title / name is the default search scope. A clear (×) button appears when the search field contains text; it resets the query immediately and keeps focus in the field, preserving the current view and other filters. Full text includes summaries and preserved source text. Unquoted terms use prefix matching; quoted phrases require adjacent words in order. Complete title matches rank ahead of partial-title and content-only matches.

Search input waits about 180 ms after typing before requesting results. Lightweight result requests reuse sidebar catalogs rather than rereading every source.

While visible, Atlas checks for library changes every 15 seconds and when focus returns. It preserves browse context and scroll position, pausing automatic display refresh during editing or open dialogs. The right-hand context panel remains mounted: unchanged content is untouched, changed rows/controls update in place, and same-item updates retain its scroll position. Display refresh reads local changes; it does not fetch Google documents.

Activity is a dedicated sidebar view after Feeds; the log fills the workspace when opened and preserves loaded history and scroll during updates. Activity records meaningful successful additions, updates, people links and removals/restores. Duplicate imports, failed attempts and empty checks stay out of it. Older additions may be marked historical; Atlas does not invent historical source-update events.

Documents shows confirmed source updates first, with an Updated date and a small unread dot before the title. The Starred checkbox filters that list; unchanged starred documents remain below updated ones. There is no separate Document Updates entry or date/title sorting control in Documents. Search retains title-match relevance ahead of recency. Imports, metadata-only changes, summary edits and tag/star changes do not count as source updates. Updates are recorded for all documents from this change forward; unverified historical updates are not invented. Opening a result acknowledges only the displayed change version. Changes appear after a source refresh, not through Google push delivery.

## Chrome and Zoom intake

The [schedule guide](schedules/README.md) describes all registered tasks and how to recreate them.

Every 15 minutes, the Chrome collector reads eligible links from the configured saved session snapshot. It excludes non-Google-Doc links and blank/untitled titles. It does not control Chrome, read cookies or decrypt sessions. Saved snapshots may lag live tabs.

The collector sends every eligible source to Atlas. Atlas checks canonical Google IDs, existing records, pending intake and deletion tombstones locally. Existing or removed sources do not proceed to Google fetching through this import path.

For selected new Google documents, the scheduled workflow reads metadata and complete text, generates a source-grounded summary and submits it through `complete_intake`. Atlas rechecks identity and tombstones before saving. Each run processes up to five pending sources; the rest persist even if tabs close.

The hourly Zoom collector recursively reads stable transcript/caption text, including historical recordings. Empty files, files modified within ten minutes and files changing during a read are deferred. Audio, video, chat logs and unrelated files are ignored; originals are never modified. Atlas owns hash/legacy matching and selects new work. See [shared processing instructions](schedules/intake-processing.md).

## Google document refresh and ownership

The hourly task calls `list_google_documents(mode="due")`; Atlas selects due sources locally and includes every Refresh requested record regardless of age. Source inactivity determines cadence: up to 3 days daily, 4–14 days every other day, 15–60 days weekly, 61–180 days every 30 days, then manual only. Age uses the last confirmed content change, bootstrapping existing records from observed Google modification timestamps. A successful check advances the last check; only a confirmed content change resets activity to now. Renames, owner repairs and unchanged/line-ending-only snapshots do not restart monitoring. An unchanged manual refresh leaves an archived source manual-only; a changed one resumes daily checking. Stars do not affect cadence. Failed requests stay queued.

The former daily morning search is retired and paused. No Drive-wide discovery scan may bypass archival. Manual-only documents stay in Documents and search. Automatic missing-owner selection also skips them unless Refresh requested is set.

The workflow uses `check_document_source` before content fetching, `inspect_document_content` before summarization and `refresh_document` to commit. Complete text includes all document tabs and tables. Metadata is checked again after reading to avoid inconsistent snapshots. Failures remain queued. Metadata-only changes or unchanged text do not require new summaries. Manual organization, stars, authorship and dates are preserved.

Google metadata requests use `fields="*"`. This connector can omit `owners` while returning individual `permissions` entries with `role="owner"`. Atlas recognizes those verified owners, excluding writers, groups, organizers and deleted identities. A missing owner entry may reflect incomplete permissions; it does not prove there is no owner.

The hourly job additionally processes at most three Atlas-selected missing-owner repairs, using metadata only and reusing metadata already read for content refresh. Retry state persists: 6 hours, 1 day, 3 days, then weekly. Connector-wide missing-field failures pause extra probes; file-specific failures back off individually. Resolved owners leave the queue.

Owner repairs preserve source text, summaries and refresh requests. Positive readable owner permissions can support an owner-only repair when trash state is omitted, without inventing that state; explicitly trashed sources are rejected. Content intake/refresh retain their source and trash verification requirements.

People show a deduplicated Owned / Authored document list (including explicitly identified creators), followed by Mentioned in and Meetings. Mentioned in uses confirmed source mentions; a document can also appear in Owned / Authored. These compact profile document lists remain starred first, and meetings newest first.

Owner, creator and author are distinct. Ownership alone does not prove who wrote a document. Authors need explicit source evidence or direct user confirmation; named mentions never establish authorship or attendance. Removed People are not automatically recreated.

## Removal and explicit re-add

Remove… moves an item to recoverable Trash and records durable source tombstones. Automatic collectors cannot recreate it, even after repeated submissions or a renamed source. External Google documents and Zoom files are never deleted.

Restore brings back the existing item and organization. Cleaned backlinks are restored only where intervening edits have not changed them. A direct human re-add may use `import_item(explicit=true)`; it restores the matching record without overwriting saved content. Automatic jobs must never use that override. There is no automatic permanent purge.

## Local MCP and helper

The existing local Codex MCP registration uses Streamable HTTP:

```toml
[mcp_servers.atlas]
enabled = true
url = "http://127.0.0.1:47831/mcp"
http_headers = { Authorization = "Bearer <private service token>" }
```

Run `scripts/service.py configure-mcp` to write the real credential locally without printing it. This preserves other connector registrations and backs up the original configuration. The endpoint is stateless: initialization, discovery and calls return JSON; notifications return 202, and standalone SSE/GET is unsupported (405). Use native HTTP registration. Cloud-only clients cannot reach the Mac's loopback interface.

The current complete tool catalog is `service/internal/transport/catalog.json`, including:

| Purpose | Main tools |
| --- | --- |
| Browse and edit | `search_items`, `get_item`, `update_summary`, `set_date`, `set_starred` |
| Automated intake | `submit_sources`, `list_intake`, `get_intake`, `complete_intake` |
| Source refresh | `list_google_documents`, `check_document_source`, `inspect_document_content`, `refresh_document` |
| Owner repairs | `list_ownership_lookups`, `record_ownership_lookup`, `link_document_people` |
| Organization | `list_entities`, `create_entity`, `get_entity`, `link_entity`, `set_authors`, `link_meeting_people` |
| Histories | `list_meeting_series`, `get_meeting_series`, `assign_meeting_series`, `list_blog_feeds`, `get_blog_feed` |
| Feed workflows | `configure_feed`, `upsert_feed_entry`, `record_feed_check`, `mark_feed_seen` |
| Recovery/activity | `preview_delete_item`, `delete_item`, `list_trash`, `restore_item`, `list_activity` |

For scripted tool calls or to avoid a stale client tool catalog, invoke `/usr/bin/python3 scripts/atlas_tool.py <tool-name>` with a JSON object on stdin. After activation it calls the shared service over authenticated HTTP and never opens the database. The service client requires a configured shared service and fails closed if it is unavailable. Restart the service after backend updates; the helper does not reload backend code. Scheduled code must use subprocess argument arrays and must not interpolate document text into shell commands. Use current revisions for guarded writes.

## Source ownership and status records

Google Docs and Confluence pages retain their external identity and link, plus a complete local source snapshot for search, history and offline reading. Confluence page-ID URLs can be supplied through `import_item(doc_urls=[...], original_text=...)`; collecting their content remains an explicit connector step, not a new scheduled crawler. Chrome collection continues to select Google Docs only.

Zoom transcripts are copied into Atlas before intake succeeds. Once the saved original has been verified in Atlas, the recording-folder transcript can be deleted without affecting the library. People, Teams and Projects store descriptions in SQLite; any historical source bytes and attachments remain preserved there. Startup migrates older shared source references into database storage or the corresponding item’s own folder, keeping identities and history links intact. Redundant shared source archives are retained as a safety copy; active records no longer depend on them.

Compatibility identity aliases remain internal to prevent duplicate records and preserve existing links.

Dated status records retain start, exclusive end and source-channel identity. There is no separate scheduled weekly-status collection. The daily AI briefing does read its two explicitly configured Slack channels and saves an edition under Feeds → AI; see its exported prompt.

## Maintenance

After changing a registered schedule, run `/usr/bin/python3 scripts/export_schedules.py` and verify the manifest and complete prompts. Exporting does not register or run tasks. Recreate jobs through the desktop automation tools, not by copying scheduler-owned configuration.

For code changes, run `/usr/bin/python3 -m unittest discover -s tests`, the applicable JavaScript tests in `tests/`, and `zsh build.sh`. See [AGENTS.md](AGENTS.md) for implementation constraints.

### Feed notifications

Feeds show a dot for new entries and meaningful in-place source updates. Opening a feed acknowledges only the displayed event cursor; later concurrent changes remain unread. Read state persists across restarts and never changes ordering. Background refreshes and scheduled jobs never mark a feed read. Duplicate submissions, quiet checks, observation bookkeeping and manual summary edits do not create attention. Blog feeds provide brief summaries and direct links to original articles.

### Reusable feed flows

Feeds support separate appended entries for briefings/articles and entries that update in place for ongoing incidents or issues. Overview feeds combine short synopses with a clickable severity/status index; older resolved entries stay available in history. Meaningful changes mark the feed unread, while quiet checks only advance Last checked. See [feed workflows](docs/feed-workflows.md) for the source submission contract.
