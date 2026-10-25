# Atlas scheduled ChatGPT tasks

This directory holds the portable snapshot of Atlas’s five registered scheduled tasks. The actual jobs are registered in the ChatGPT/Codex desktop scheduler and persisted outside the repository, normally under `$CODEX_HOME/automations` (default `~/.codex/automations`). Copying these files does not activate schedules.

`manifest.json` is the exported inventory: identifiers, names, active/paused state, recurrence, timezone, destination chat names and prompt filenames. Each task Markdown file contains its full registered prompt. Shared intake details live in [intake-processing.md](intake-processing.md). Path placeholders use manifest parameters.

## Task inventory

All five tasks were exported as ACTIVE when this documentation was updated.

| Registered task | Cadence, America/Los_Angeles | Work and limits | Full prompt |
| --- | --- | --- | --- |
| Atlas Chrome document import | Every 15 minutes | Collect titled Google Doc links; Atlas selects new sources; process up to five pending imports | [Chrome import](atlas-chrome-document-import.md) |
| Atlas requested document refresh | Hourly, minute 00 | Refresh explicitly requested documents; additionally repair at most three due missing owners using metadata only | [Hourly refresh and owners](atlas-requested-document-refresh.md) |
| Atlas morning document refresh | Daily, 6 a.m. | Metadata search for sources created/modified in the last 30 days, intersect Atlas IDs, include pending requests | [Morning refresh](atlas-morning-document-refresh.md) |
| Atlas Zoom transcript import | Hourly, minute 10 | Collect all eligible stable transcript text; Atlas selects new sources; process up to five pending imports | [Zoom import](atlas-zoom-transcript-import.md) |
| Daily AI briefing | Daily, 6 a.m. | External AI news plus the preceding 24 hours in two specified Slack channels; save one dated AI feed edition | [AI briefing](daily-ai-briefing.md) |

The first four run in **Atlas scheduled checks**. The briefing retains its separate briefing chat. Destination chat IDs are local bindings and deliberately omitted from the portable manifest.

For process details see [architecture](../docs/architecture.md); for installation, upgrades and recovery see [operations](../docs/operations.md).

## Responsibilities and source access

| Component | Responsibility |
| --- | --- |
| Collector scripts | Read saved Chrome links or stable Zoom text and submit every eligible source |
| Shared Atlas service / Library | Sole production database owner; canonical identities, duplicates, pending work, tombstones, relationships, retry state and Activity |
| Scheduled ChatGPT workflow | Read Atlas-selected Google sources through connected tools, generate summaries, submit results and verify writes |
| Atlas window | Browse local records and display updates; it is not the scheduling engine |

Atlas has no independent Google login or AI service. One launchd-managed shared Atlas service owns the database and serves both the UI and MCP. The native window, MCP connection, collectors and helper call that service; they never open production SQLite themselves. The service runs while the Atlas window is closed. For these local scheduled workflows, keep the Mac awake/available and the desktop scheduler running. Registration alone does not guarantee source access or a successful run.

Google Drive is read-only. Google Calendar can corroborate ambiguous meeting identities when shared processing instructions require it; invitees alone do not prove attendance. The briefing needs the connected Slack account and web access. No task may publish private documents or Slack content.

## Chrome import: the exact sequence

1. Run `scripts/collect_sources.py chrome`. It reads the configured profile’s latest unencrypted saved session, not live browser controls.
2. Filter non-native Google Doc URLs and blank/untitled titles. Normalize Google IDs while preserving resource-key access parameters.
3. Submit every eligible source to Atlas. Atlas checks existing items, queued identities and deletion tombstones locally.
4. Only Atlas-selected pending sources proceed to Google metadata and complete source-text reads. Existing or removed documents do not trigger Google reads through this intake path.
5. Generate a source-grounded summary and call `complete_intake`. Atlas rechecks identity/tombstones under its writer lock, saves the source and owner links, and records the actual addition in Activity.
6. Process at most five per run. Remaining pending sources survive closed tabs and interrupted runs.

Saved Chrome sessions may lag live tabs or remain after Chrome closes. The collector does not read cookies, decrypt browser data, change tabs, or install a browser extension. Unsupported or incomplete sessions fail rather than guessing.

Recheck current Google titles before reading and immediately before import. Blank titles and titles containing Untitled document are excluded, ignoring case and normalized whitespace. A titled draft can be reconsidered later unless tombstoned.

## Zoom collection

Run `scripts/collect_sources.py zoom` against the configured Zoom folder, recursively including historical recordings. Eligible files are transcript-named text, saved closed captions, and recognizable VTT/SRT transcripts. Ignore recordings, chat logs and unrelated files.

Skip empty files and files modified in the last ten minutes. Compare size and modification time before/after full reading; defer changing files. Decode UTF-8 or explicitly recognized BOM encodings without silently dropping text. Never change source files.

Atlas owns source-key/hash matching and conservative legacy-session comparisons. Ambiguous matches stop for review. Complete intake retains the entire queued transcript; summaries never replace the original text. Interview candidates and confirmed work participants use their separate relationship APIs.

## Content refresh versus owner repair

Content refresh has two entry points: explicit user flags hourly, and the daily recent-source metadata search. The daily job follows every search page and matches file IDs to Atlas; it does not import unrelated Drive documents or individually poll dormant ones.

Before fetching content, compare source metadata. Before summarizing, compare complete text hashes. Re-read metadata after content and retry a consistent snapshot at most twice if it changes. Failed/incomplete reads retain refresh requests. Only a successful `refresh_document` may acknowledge a request.

Owner repair is independent of content refresh. Atlas selects at most three due missing-owner records per hourly run; reuse metadata already fetched for the same file. No body download, summary rewrite, or refresh-flag acknowledgment is needed.

Request Google metadata with `get_file_metadata(fileId=...,fields="*",supportsAllDrives=true)`. The working owner representation in this connector is often `permissions` containing `type="user"`, `role="owner"`, name and email. Pass observed data to Library; do not turn editors, groups, organizers or pending owners into owners.

An omitted `owners` field is not a failure if permissions identifies the owner. No owner permission may mean incomplete access; never convert that into `owners=[]`. An observed empty owners list is a different result. Owner is not synonymous with author or original creator. Never recreate a removed People profile.

Persist lookup outcomes with `record_ownership_lookup`. Failures back off 6 hours, 1 day, 3 days, then weekly. File-specific failures affect that document; a connector-wide missing-field failure pauses extra owner probes. Honor the returned stop/cooldown and notification fields. These bounds apply to scheduled runs; an explicitly requested manual backfill can inspect all missing-owner documents.

## Briefing and logging

The AI briefing prompt names its exact Slack channels, source windows and editorial requirements. Read recent real feed editions first, ignore fictional samples, and avoid a second edition for the same Pacific date. Preserve the generated briefing and collected source notes, then verify the saved edition and feed membership.

Atlas writes successful changes to its persistent Activity history. Empty runs, duplicates and failed attempts do not create success events. Collection/refresh tasks stay quiet when nothing meaningful changed; they report actionable failures and unresolved identities without dumping private source contents. Repeated unchanged owner failures are quiet. The briefing posts a short success confirmation or its specified failure fallback in its own chat.

What’s changed is separate from Activity: it reports actual source-content changes detected while a document is starred, after a source refresh.

## Deletion protection and safe writes

All automatic imports use `submit_sources` and `complete_intake`; they never maintain their own seen list or override tombstones. A direct human re-add may use `import_item(explicit=true)` to restore the matching saved item. Automatic jobs cannot use that override.

Use the current Library API through Atlas MCP or `scripts/atlas_tool.py`. The helper accepts tool name as an argument and JSON on stdin. Use subprocess argument arrays; never interpolate source text into shell commands. The helper calls the running service’s current tool catalog. Restart the shared service after backend updates; helpers do not load Library or open SQLite. Never directly modify the database or library files.

Verify writes with `get_item`, including preserved source, dates, organization and relevant People/series/feed links. A duplicate response is a skip, not an addition. Revision conflicts require a fresh read; never blindly overwrite user edits.

## Recreate on another Mac

1. Copy project source and a consistent private library backup separately. Keep library contents and credentials out of a source-code repository.
2. A fresh installation starts with an empty library. Transfer existing data separately only through a reviewed migration: preserved source paths are absolute, and current restore acceptance uses the original root. Do not reuse the old Mac’s `.service` runtime settings, credentials, activation marker or locks. The native window uses `--library`, then `ATLAS_LIBRARY`, then the current user’s `~/Documents/Atlas/library`; no Swift path edit is required.
3. Install/sign in to the desktop scheduler. Connect the intended Google Drive account, Calendar if needed, and Slack for the briefing. No separate Google Cloud project is required for the currently working owner-permission lookup.
4. Request normal Documents access with `scripts/service.py request-access --library <library> --app <absolute-app-path>`, install and health-check with `scripts/service.py install --library <library> --app <absolute-app-path> --owner-name <full-name> --timezone <IANA-timezone>`, then update the existing Atlas MCP registration with `scripts/service.py configure-mcp --library <library>`. Restart ChatGPT to replace old connections; test `get_item` and the helper. Keep the authenticated loopback endpoint private. Collectors submit through the same service, including when invoked with `--library`. Verify local browsing after activation. Follow the detailed operations runbook for migration boundaries and failure recovery.
5. Expand manifest path parameters for the new Atlas, Zoom and optional Obsidian locations. Chrome defaults to `~/Library/Application Support/Google/Chrome/Default/Sessions`; select another profile explicitly if needed.
6. Create **Atlas scheduled checks** and a separate briefing chat. Bind new jobs to those new chat IDs.
7. Use desktop automation tools to recreate each manifest entry and its expanded prompt. Match existing jobs first to avoid duplicates. Preserve timezone, cadence, active/paused status and any notification preferences. Do not install by copying scheduler-owned TOML files.
8. Test the collectors, a new import, a duplicate skip, deletion protection, a metadata owner lookup and a flagged content refresh. Verify Atlas records/Activity before relying on unattended runs.

Organization restrictions and account permissions still apply. The saved snapshot documents intended behavior; it does not grant Google/Slack access.

## Keep this snapshot current

After any registered-task change, run from the project directory:

```sh
/usr/bin/python3 scripts/export_schedules.py
```

The exporter reads actual registrations and writes the manifest and complete parameterized prompts. It does not register, modify or run jobs. Review exported status, destination, cadence and prompt content. Keep this guide and shared processing instructions aligned with behavior; re-exported task prompts are snapshots, not the place to change a registered job.

If a registered job is removed, confirm its deletion before removing its obsolete prompt file. Do not commit credentials, account tokens, browser session files or private source content. The prompts themselves contain internal channel/workflow details and belong in a private repository.

Google document content reads use `get_document`, retaining explicit people-chip identity and mailto links across all tabs and tables. Flattened text is for preservation and summaries, not identity extraction. Plain typed names never create document mention links. Metadata-only checks still avoid content reads. Successful content refreshes may add verified structured mentions through `link_document_people`; existing historical links are not automatically removed.
