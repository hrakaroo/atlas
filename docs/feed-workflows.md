# Feed workflows

A feed's update flow, presentation and collector cadence are independent settings. Atlas owns identity matching, deletion protection, revision checks, saved sources/history, Activity and unread attention. Scheduled tasks read sources and supply summaries; they do not generate layouts or maintain their own seen lists.

| Setting | Behavior | Typical use |
| --- | --- | --- |
| `flow=append` | Each stable source key creates one entry; subsequent submissions return the existing entry without rewriting it | News briefings and author blogs |
| `flow=in_place` | The same stable source key updates the existing entry, with its current expected revision | Incident or issue monitoring |
| `presentation=entries` | Open the latest entry with a dated history in the right rail | Briefings and articles |
| `presentation=overview` | Display current entry summaries together, with a clickable status/severity index and older resolved entries | Incident overview |

Existing AI and author feeds default to append/entries. The existing `import_item(kind="blog", blog_feed=...)` interface remains a compatible append producer. An existing populated feed cannot change flow accidentally. Changing cadence does not change its storage model.

## Source submission

1. Configure once with `configure_feed(name, flow, presentation, entry_label)` (`POST /api/feed-config`). Find it on later runs with `list_blog_feeds`.
2. Read `get_blog_feed(feed_id)` to obtain current entries and revisions. Reads do not mark attention seen.
3. Submit each entry with `upsert_feed_entry` (`POST /api/feed-entry`):
   - `feed_id` and stable `entry_key` identify the entry, independently of title or date.
   - `title`, concise Markdown `summary`, complete fetched `original_text`, optional `original_name` and HTTPS `source_url` preserve the material used.
   - `observed_at` is an RFC3339 source-check timestamp. Older observations cannot overwrite newer ones.
   - `expected_revision=0` is for creation; an existing in-place entry requires its current item revision. On conflict, read again and regenerate against that version, preserving relevant user context.
   - Optional `date` supplies the publication date; otherwise the first observation day is used. Later updates preserve existing dates, stars, tags and relationships.
   - `fields` holds meaningful structured attributes. The overview understands `external_id`, positive integer/null `severity`, `status` (`active`, `stable`, `resolved`, `unknown`), and HTTPS `incident_url`/`channel_url` links.
   - `source_context` holds provenance, such as `channel_id`, `window_start`, `window_end` and `status_updated_at`. Coverage timestamps must be valid and ordered. Observation/coverage metadata is separate from meaningful fields.
4. Verify the returned item, saved full source/hash, feed membership and intended fields. Duplicate or blocked responses are skips. Automatic submissions never restore Trash or accept an explicit override.
5. After a complete successful source check, call `record_feed_check(feed_id)` (`POST /api/feed-check`). This advances the visible Last checked time without creating Activity or unread attention. Failed or incomplete checks retain the previous time.

A source-only change can preserve a new snapshot without generating attention when the synopsis and structured attributes are unchanged. Rephrasing a summary over identical source/fields is ignored. Identical-source checks only update observation bookkeeping, without making another revision or source copy. Meaningful title, synopsis or structured-field changes create one unread event and one Activity event. Manual summary edits retain the normal separate editing interface.

Updates preserve previous original snapshots and summary revisions. The source/summary commit uses the recoverable pending-source journal; a failed transaction does not acknowledge a change. Files and SQLite are only changed by the shared service.

## Overview and attention

Overview feeds show Active, Stable, Unknown, then Resolved entries, with severity ordering within each group. Open entries remain visible regardless of age. Resolved entries leave the main overview 24 hours after their last meaningful change and remain under Older resolved in the overview/index. Selecting an index entry scrolls to and focuses its section, expanding the archive when needed. Individual entries remain available for normal summary editing and recoverable removal.

Status is the most recently recorded source status, not a real-time health guarantee. Display Last checked separately from the recorded status timestamp. Missing severity/status remains unknown; colors accompany text labels.

Feed attention uses a durable ordered event stream for both new entries and meaningful in-place changes. Older arrival cursors migrate without losing read state. `mark_feed_seen(feed_id, through_id)` acknowledges only the displayed snapshot; newer concurrent changes remain unread. Neither source checks, API reads, duplicate submissions nor background UI refreshes mark attention read. Clearing attention never changes ordering.

## Incident collector responsibilities

The private incident schedule uses connected Slack tools read-only. Discover incident IDs from the configured index channel over the last 24 hours (or since the last successful check when recovering a gap). Include bot notifications and exhaust pagination. Also check stored unresolved incidents and recent resolutions so quiet incidents are not dropped and reopened incidents are noticed. A bounded first-run lookback can seed still-open incidents.

Read each incident's own channel and relevant thread replies. Use the latest explicit incident-system status/severity by message timestamp; button labels, suggested next steps and reassuring conversation are not status transitions. Distinguish planned rollout tracking from an outage. Summarize observed impact, mitigation and next steps without inventing owners or completion. Supply the channel identity and read window as source_context. Keep saved message formatting deterministic, and preserve prior synopsis/user context if new messages contain no material information. Do not claim incomplete reads as successful checks.

The registered task's channel, feed name and routing are private configuration in its ignored exported prompt and manifest. See [scheduled tasks](../schedules/README.md). Do not put company channel IDs, incident content or credentials in public source or test fixtures.
