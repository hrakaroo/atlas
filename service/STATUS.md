# Service validation status

The Go service is the active backend. The [application specification](../docs/specification.md) describes current behavior; this document records validation evidence and its limits. Historical results do not certify later code changes.

## Latest feed-workflow release

The signed candidate deployed on October 25, 2026 passed:

- All 127 isolated HTTP/MCP endpoint contracts against the packaged executable.
- Transport coverage of all 45 advertised tools and all inventoried routes, with no missing operations. Coverage establishes exercised transport paths; scenario assertions establish the tested behavior.
- 16 client tests, 11 JavaScript checks and three packaged-binary checks.
- Go unit/transport tests and race checks for the store and transport packages.
- A native WKWebView interaction check using synthetic data: overview rendering, status/severity index navigation and focus, stable right-panel nodes during updates, and background changes remaining unread.

Deployment used a complete API backup and a before/after endpoint comparison. Existing item content and organization, queues, Activity and feed read cursors were preserved. The running service was verified as the selected native supervisor's child, and served assets matched the canonical web files. Initial incident entries were read back and their full source bytes/hashes and structured fields verified. An unchanged resubmission produced no duplicate item, Activity event or unread change.

Generated reports, synthetic native checks, candidates and machine-specific backup/deployment evidence remain private under ignored `service/.work/`. The current suite is discovered from `contract/test_*.py`; the recorded count is evidence for this release, not a fixed future requirement.

## Behavioral coverage

| Area | Accepted regression scope |
| --- | --- |
| Transport and ownership | HTTP/MCP authentication, origin/host boundaries, schemas, errors, single service ownership and guarded test controls. |
| Intake and preservation | Canonical source identity, duplicate/tombstone handling, complete originals, dates, stars, manual organization, queues and retry behavior. |
| Relationships | Entities, distinct ownership/authorship/creation/mention/attendance, structured Google people-chip/email evidence, email identity matching, candidate separation and removed-profile protection. |
| Reading and search | Title/full-text scope, ranking, prefixes, phrases, Unicode, preview navigation and retained context panels. |
| Document monitoring | All cadence boundaries, source-age bootstrapping, manual archival override, unchanged checks, content-change wake-up, failure retention, all-document updates and seen cursors. |
| Feed workflows | Append/in-place identity, revisions and older-observation rejection, concurrent writers, full source/history retention, quiet checks and rephrasing, overview summaries/retention, meaningful events and legacy cursor preservation. |
| Recovery and storage | Restart persistence, database-backed entity descriptions/history, independent source copies and legacy attachments, interrupted source/summary writes, Trash, same-root backup restoration and process recovery. |

Additional configuration, failure-recovery, native-helper and fresh-checkout checks are described in the [endpoint guide](contract/README.md). Synthetic historical schema setup prepares disposable fixtures only; application state and source preservation are asserted through public endpoints.

## Limits and repeatable checks

Runtime validation and signed native builds have been exercised on macOS arm64. Intel macOS has compile-only validation. Native tests and signing verification do not establish notarization, distribution readiness or every managed-device permission policy.

Fault tests cover named interruption boundaries, not every possible physical power-loss outcome. Backup restore validation uses the original absolute root; cross-root relocation remains a separate reviewed procedure. Connector access and unattended scheduler availability depend on the local runtime and source accounts; synthetic contracts do not guarantee those external systems.

Run applicable checks after implementation changes using the [endpoint guide](contract/README.md) and [repository guide](../docs/repository.md). Test signed packaged assets before deployment and follow [deployment safeguards](DEPLOYMENT.md). Documentation-only updates do not require rebuilding or restarting the service.
