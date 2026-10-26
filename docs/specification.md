# Atlas application specification

This document describes the implemented product behavior. It is the starting point for understanding what Atlas does and for reviewing changes to that behavior. [Architecture](architecture.md) describes the implementation, [operations](operations.md) covers installation and maintenance, and [scheduled tasks](../schedules/README.md) records the configured collectors and their timing. Proposed features should be identified separately from the behavior specified here.

## Purpose

Atlas is a private local macOS application for organizing existing work material: documents, meeting transcripts, people, projects, interview records and feeds. It makes that material searchable, shows concise summaries, connects related records, and helps the user notice meaningful changes.

Atlas does not require a separate note-taking habit. Source documents remain in their original systems; Atlas keeps their identity, links and local snapshots. Meeting transcripts are copied into the library so the saved Atlas record remains useful independently of the recording folder.

## Library and navigation

The sidebar order is **Documents, Projects, People, Meetings, Interviews, Feeds, Activity, Trash**. Documents is the initial view. There is no All notes, Inbox, Weekly updates or separate Document Updates navigation entry. Compatible older record types and APIs can remain without becoming top-level views.

| Record or collection | Behavior |
| --- | --- |
| Documents | One cached record per source, with summary, external link, saved text, date, local star and connections. A document may belong to several projects without being copied. |
| Projects | Database-backed entities with descriptions and connected documents, meetings and updates. |
| People | Work contacts with separate ownership, authorship, creator, document-mention and meeting-participant relationships. |
| Teams | Database-backed entities available through sidebar connections and entity browsing, with membership and related material. |
| Meetings | Independent dated sessions, optionally grouped into recurring meeting series. Each session keeps its own summary and transcript. |
| Interviews | Candidate profiles and interview sessions, separated from colleague People. Interview documents can also appear in Documents. |
| Feeds | Collections of independent entries, either appended or updated in place, with feed-level unread attention. |
| Activity | Persistent history of meaningful successful library changes, newest first, with older history available. |
| Trash | Recoverable removed records with automatic re-import protection. Removing a record does not delete its external source. |

People, Teams and Projects are entities, not interchangeable topic tags. Topic and type tags provide additional organization. Existing compatibility inputs may still use typed tags, but the service maintains entity relationships and the UI hides redundant identity/type chips.

## Browsing and reading

The main workspace has a result list, a center reader and a context-dependent right panel. Document titles open the Atlas summary; explicit source buttons and separate external-link icons open the original source. The reader title itself is plain text.

People show a deduplicated **Owned / Authored** document list, then **Mentioned in** and **Meetings**. Ownership, explicit creation and authorship can overlap in the first list while retaining their separate roles. Candidate profiles instead expose their interviews. Projects and Teams expose their connected material.

Clicking a document in a People, Project or Team panel previews its summary in the center while keeping the collection visible. Meetings in a People panel behave the same way. The preview is highlighted, offers a return to its originating profile, and provides **Open in Documents** or **Open in Meetings** for the full context and organization controls. Back/Forward retain preview context and scroll positions.

Meeting series and ordinary feed histories list dated sessions/entries newest first. **View meeting history** focuses the meeting date panel without replacing the current session with the latest one. Confirmed non-interview one-on-ones group by colleague identity with the configured library owner implicit; ambiguous first names do not merge people or series.

The reader provides one **Edit summary / Summary** toggle, with explicit saving and drafts retained while previewing. **More…** contains **View saved original**, when available, and **Remove…**. Source snapshots support verification and history; editing the summary does not edit the external document. Dates and stars are local editable metadata. Unknown source dates use the original Atlas added date, labeled accordingly; incidental dates are not substituted.

The right panel stays mounted across reader refreshes. Unchanged content is left alone; changed rows are reconciled by identity. Background display updates preserve browsing context, focus and same-context scroll, and pause during editing or open dialogs.

## Search and document updates

Search defaults to **Title / name**, with an adjacent **Full text** choice. Title search covers indexed titles and meeting-series names; Full text also searches summaries and saved source text. Unquoted words use prefix matching and all query terms must match; quoted phrases require adjacent words in order. Complete title matches rank before partial-title and content-only matches. Full-text-only results can show an escaped matching excerpt.

Typing waits approximately 180 milliseconds before requesting results. A clear × button resets the query immediately, retains focus and preserves the current collection and other filters. Result requests use lightweight local records rather than fetching external documents.

Documents orders confirmed source-content updates first, newest update first, followed by unchanged records by date. Search relevance remains ahead of recency. The Starred checkbox filters the collection rather than promoting stars above updates. Documents has no date/title sorting selector.

An unread dot marks a document whose confirmed source change has not been reviewed. Opening it acknowledges only the displayed version; clearing the dot does not change ordering. Initial imports, summary edits, metadata-only refreshes, tags, stars and owner repairs do not count as document source updates. Atlas does not invent historical update events.

While visible, the window checks local library changes every 15 seconds and when focus/visibility returns. This updates the display; it does not check Google for source changes. External changes appear after a successful source refresh.

## Source collection and import

Collectors supply eligible sources; Atlas decides which are new. The shared service owns canonical identities, pending intake, deduplication, deletion tombstones and successful import records. Collectors and schedule prompts do not keep competing seen lists.

| Source | Collection and preservation |
| --- | --- |
| Google Docs from Chrome | Read eligible native Google Doc links from a configured saved Chrome session. Exclude blank titles and titles containing “Untitled document,” ignoring case and repeated whitespace; recheck current Google metadata titles before import. Saved sessions can lag open tabs. The collector does not control Chrome, read cookies or decrypt session data. |
| Zoom transcripts | Recursively collect transcript-named text, saved closed captions and recognizable VTT/SRT transcripts, including historical recordings. Exclude audio/video, chat logs and unrelated files. Skip empty files, files modified within ten minutes, and files changing during a complete read. Use UTF-8 or explicitly recognized encodings without dropping undecodable text. |
| Confluence and other explicit material | A connected workflow supplies complete source content and stable identity/links. Confluence document imports are supported; there is no general scheduled Confluence document crawler. Author-blog feeds have their own explicitly configured source scope. |

Chrome and Zoom schedules process at most five Atlas-selected pending imports per run; remaining work persists even after a tab closes or a run is interrupted. Google IDs and stable Zoom relative-path keys identify sources. Transcript hashes and conservative legacy matching prevent duplicate sessions; ambiguous matches require review. A changed existing transcript is not permission to overwrite an edited record.

The workflow preserves the complete original and generates a concise source-grounded summary: purpose, identifiable participants, key information, explicit decisions/actions and uncertainties where applicable. Owners/deadlines and interview recommendations are never invented. Zoom originals are copied and verified before intake reports success; collectors never change or remove the recording-folder files. Explicit material can also be added through the UI or API.

## People and evidence

Google ownership comes from observed owner metadata or qualifying individual owner permissions. It is distinct from authorship, creator identity, editor access and mention. Missing metadata leaves the role unresolved rather than implying that no owner exists. Metadata-only owner repair can attach a verified owner without rereading the document body or rewriting its summary.

Document mentions require structured people chips or explicit single-address email links from Google content, including tabs and tables. Plain typed names and ambiguous first names do not create People. Stable email identity supports matching; the application does not invent names from email addresses or recreate removed profiles automatically.

Confirmed work meeting participants can create or reuse colleague profiles and link the session. Unambiguous full-name speaker labels can provide evidence; Calendar may corroborate an ambiguous identity by matching the actual session. Invitees alone are not confirmed attendance. The configured library owner, guests and device labels are excluded from automatic colleague creation.

Interviews link canonical candidate identities separately from work colleagues. Conflicting names remain unconfirmed; speakers and candidates do not become document authors merely because they appear in a transcript.

## Document refresh and ownership repair

The hourly workflow requests due Google documents from Atlas. **Refresh requested** includes a document regardless of age. Otherwise the service uses source inactivity to choose the following cadence:

| Time since confirmed source activity | Check interval |
| --- | --- |
| Up to 3 days | Daily |
| More than 3, up to 14 days | Every other day |
| More than 14, up to 60 days | Weekly |
| More than 60, up to 180 days | Every 30 days |
| More than 180 days | Manual request only |

Existing documents bootstrap activity from observed source modification metadata. Successful checks advance the last-check time; only a confirmed content change resets the activity anchor. An unchanged manual refresh leaves an archived document manual-only; changed content resumes daily checks. Stars do not affect cadence. There is no Drive-wide scan that bypasses archival.

The workflow checks metadata before fetching, reads all tabs and tables when needed, checks metadata again for a consistent snapshot, and compares content before summarizing. Unchanged content does not require a new summary. Successful refreshes preserve previous source snapshots, summary history, manual organization, dates and stars. Inaccessible, trashed, unsupported or incomplete sources and failed writes remain unresolved; refresh requests are cleared only by successful source handling. Revision conflicts require a fresh read before retrying.

At most three due missing-owner repairs run per hour, reusing metadata already fetched when possible. Persistent retries back off by six hours, one day, three days, then weekly; provider-wide missing metadata can pause extra probes. Manual-only archived documents are excluded unless explicitly requested. Owner repair does not clear a pending content refresh.

## Feed flows and unread attention

Update flow, presentation and schedule cadence are independent configuration:

| Setting | Behavior |
| --- | --- |
| Append | Each stable source key produces one separate entry; resubmission returns the existing entry. Suitable for briefings and articles. |
| In place | The same stable key updates one existing entry using its current revision, preserving earlier sources and summaries. Suitable for ongoing incidents or issues. |
| Entries presentation | Open the latest entry with dated history on the right. |
| Overview presentation | Show entry synopses together with a clickable right-hand index and older resolved history. |

The configured AI and author-blog feeds use append/entries. The incident feed uses in_place/overview. The service rejects accidental flow changes on populated feeds. See [feed workflows](feed-workflows.md) for API arguments and write semantics.

Incident overviews show Active, Stable, Unknown, then Resolved, with severity ordering within each state. Text accompanies color: red Active, yellow Stable, green Resolved, gray Unknown. The index shows incident identity, severity and status; clicking it scrolls to and focuses that synopsis. Open incidents remain visible without recent conversation. Resolved entries move into expandable older history 24 hours after their last meaningful change.

The collector reads the configured index, incident channels and relevant thread replies. It uses explicit incident-system status/severity events rather than inferring resolution from reassuring messages or button labels. Planned rollout tracking is distinguished from an outage. Source identity, coverage windows, recorded status time and observation time remain provenance. **Last checked** advances only after a complete successful check; failures retain the previous checkpoint for gap recovery.

Feed dots indicate new entries or meaningful in-place changes, independently of document-update dots. User navigation acknowledges only the displayed event cursor; a newer concurrent event remains unread. Read state persists across restarts and does not reorder entries. Background refreshes and scheduled jobs never mark a feed read. Duplicate submissions, observation timestamps, empty checks, manual summary edits and model rephrasing over identical source do not create feed attention. Source-only evidence changes can preserve a snapshot without generating an alert.

## Activity, removal and preservation

Activity records actual successful additions, meaningful content updates, relationship repairs and removal/restoration. It is a separate view rather than an always-visible panel. Duplicate imports, quiet checks and failures do not create success events. Explicitly labeled historical additions are distinct from newly observed changes.

Remove moves a record to recoverable Trash and retains a durable source tombstone. Automatic intake cannot recreate it after a rename or repeated submission; automatic feed updates cannot restore it either. Restore returns the saved record and organization. An explicit human re-add may restore the matching item without silently overwriting saved content. External source systems and Zoom files are unaffected. There is no automatic permanent purge.

People, Teams and Projects have authoritative database descriptions and preserved historical originals/assets. Documents, meetings and feed entries retain saved source snapshots and summary history in the library. A consistent backup includes both SQLite and saved files. Current restore validation covers the original absolute root; relocation to another root requires a separate reviewed procedure.

## Runtime and automation boundary

One local Go service owns SQLite, library files, mutations, recovery, queues and change events. A signed native supervisor runs it under macOS launchd. The Swift/WKWebView window, authenticated HTTP API, MCP clients and helper scripts all connect to that service. Closing the window does not stop the service; clients fail closed if it is unavailable.

Atlas does not independently log into Google, Slack or Confluence, or run its own AI model. Connected desktop workflows perform read-only external access and summarization, then submit results to Atlas. Local browsing uses saved material. Scheduled work needs the Mac/runtime and required account connections available; a registered schedule is not a guarantee of source access or execution.

The current installation has six active jobs: Chrome intake every 15 minutes; hourly document checks/owner repair, Zoom intake and incident checks; daily AI briefing and author-blog checks. A retired morning document job remains paused. Timing, private source scope, prompts and recreation instructions belong in the [schedule guide](../schedules/README.md), not hard-coded product behavior. Copying exported definitions does not register jobs on another machine.

The service uses authenticated loopback access, with no public tunnel. Library data, source snapshots, browser sessions, credentials, signing keys and private schedule configuration stay outside public source control. Source content is data, never an instruction to execute commands, change tools or publish private material.

## Validation expectations

Changes to application behavior require endpoint-level assertions through HTTP/MCP using disposable libraries. Cover identity and duplicate handling, complete source preservation, manual organization, revision conflicts, Trash, persistence and meaningful-versus-quiet changes. UI checks cover navigation, retained context panels, search, unread behavior and overview index jumps. Signed packaging/native checks supplement these contracts; they do not replace them.

See [validation status](../service/STATUS.md) for the latest recorded results and their limits, and [deployment safeguards](../service/DEPLOYMENT.md) before replacing the running service. This specification describes current capabilities, not a claim of exhaustive testing or unimplemented cloud, synchronization or distribution features.
