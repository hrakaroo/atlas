# Controlled Go cutover and rollback

Go is the active backend. Use these safeguards for deliberate deployments or optional local rollback. Historical migration and machine-specific cutover evidence is retained under ignored `old/migration/`; a fresh checkout does not include it.

## Before a deliberate switch

Build and validate the current candidate in isolation. Record its source and artifact fingerprints locally before selecting a stable deployment location. Its launch specification must use the actual library root as the working directory.

Use the reviewed signed build and the compatible Python source from the completed migration, not the original 7400909 baseline. Both current engines understand pending refresh/Trash recovery intents; the original baseline does not.

Record the implementation revision, app/binary hashes, stable app location, absolute existing library path, actual owner name, IANA timezone and current service label/port. Keep the private connection token out of logs and Git. Preserve the original library root: cross-root relocation of existing absolute source paths is a separate procedure.

The Go preview is built with:

```sh
(cd service && CGO_ENABLED=0 go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
zsh build-go.sh --go-service "$PWD/service/.work/atlas-service"
```

Build-time dependencies and the pinned module graph must be available locally. The packager checks embedded UI and third-party notices, signs inside-out, and does not register or launch anything. For a long-lived deployment, select a stable output location outside scratch work and verify its signature again. The native app and helper must stay at the paths in the launch specification.

## What happens to existing data

The backend switch reuses the **same existing library root, SQLite database and item/import directories**. It is not an export/re-import, relocation or replacement of the user's collection. Preserve all of them together. SQLite contains identities and organization; item/import files contain saved summaries, originals, history and attachments. The safety backup copies both through the service's consistent backup action; copying the database alone is insufficient.

With scheduled writers paused, capture an endpoint inventory before stopping Python: active and Trash item IDs and kinds, dates/stars/revisions, summaries and source hashes, ownership/authorship/attendance and entity links, series/feeds, Activity, changed-document cursors and pending intake. Hash available original/history bytes through authenticated endpoints; Trash source/history routes intentionally reject reads, so preserve their complete files in the full backup and compare supported Trash metadata. Do not publish private snapshots or put them in Git. Compare the same saved identities/content/organization after starting Go, before the controlled write and resuming jobs. Investigate any unexpected discrepancy rather than accepting matching counts alone. No such live data comparison has been performed by isolated acceptance tests.

Candidate endpoint validation does not replace deployment-time backup, inventory comparison or native consent checks.

## Switch procedure

1. Explicitly authorize the production switch and pause source-processing jobs so they cannot write during validation. Do not change their source selection, deduplication or tombstone rules.
2. While the current service is healthy, create a full backup through Atlas's backup action. Retain the SQLite snapshot **and** its item/import files. Stop Python cleanly with `/usr/bin/python3 scripts/service.py stop`. Keep the existing Python launch definition and compatible source available.
3. Verify that the backup action completed and preserve/check the complete backup artifacts and their checksums without reopening them at another root. Endpoint restoration was proven using synthetic fixtures restored at their original root. A real restore drill requires a separate, deliberate stopped-owner procedure at the original root; do not open the real backup at an arbitrary temporary path or start experimental validation against live paths.
4. Produce and review the concrete Go launch specification:

   ```sh
   /usr/bin/python3 scripts/service.py print-launch-spec --backend go \
     --app /absolute/stable/path/Atlas.app \
     --library /absolute/existing/library \
     --owner-name 'Actual Full Name' --timezone America/Los_Angeles
   ```

   The Go-only layout update enables `scripts/service.py install` for reviewed signed Go apps with explicit owner/timezone. Install the intended stable helper deliberately and handle ordinary macOS consent locally. Do not substitute a second owner, change trust settings automatically or delete activation markers.
5. Start only the Go owner. Verify authenticated health reports the intended root and new PID; inspect existing records, source bytes/history, People links, Trash/tombstones and queued work. Confirm the native window and MCP/helper clients use the same connection contract. The service reuses the validated existing token; update clients only if the selected address actually changes.
6. Make one controlled reversible write/read-back through endpoints, verify Activity and source access, then resume the paused jobs. Keep Python and the verified backup through the rollback window. Do not remove the Python implementation.

The active launchd job label is `local.atlas.server`; its signed helper association is a separate bundle identity. Keep the installed label and lifecycle helper in sync. Killing a child is insufficient while launchd KeepAlive is loaded: unload the old owner before loading another. The service lifetime lock rejects competing owners before SQLite opens.

## Rollback

Stop/unload Go first and confirm its child exited. Reopen the **same root** with the compatible retained Python implementation, then verify health, records, exact sources/history, organization, tombstones/intake and Activity through endpoints before resuming jobs. This sequential bidirectional handoff, including interrupted refresh and removal/restoration, is covered by acceptance tests.

If recovery reports a conflicting manual summary or cannot finish cleanup, library operations fail closed and require resolution plus restart. Retain the intent and manual text; do not delete journals or force a summary over unrelated edits.

If returning to original baseline 7400909 or another implementation that cannot read recovery intents, restore a verified consistent full backup instead. Explicitly reconcile any later writes; never silently discard them or restore only SQLite.

## Limits of the evidence

Acceptance uses synthetic local fixtures and named failure barriers. It proves process interruption/restart behavior at those boundaries, not physical power-loss atomicity or removal of every orphan staging folder. An unacknowledged summary-only edit can recover old or new text; an interrupted operation can retain an additional exact old-summary revision.

Runtime tests and signed app builds ran on macOS arm64. Intel macOS compilation passed; Intel runtime execution was not tested. Signing verification does not prove Documents-consent persistence or replace Developer ID/notarization for distribution. No Google/Chrome production access, launchd/MCP registration, schedule update or live cutover occurred during migration acceptance.

## Layout update

The service directory is now `service/`. Retired Python source/native packaging is in ignored `old/`. For local Python rollback stop Go first, then use `old/scripts/service.py install --library /absolute/existing/library --app /absolute/project/old/build/Atlas.app`. Its default library would be old/library, so the explicit real root is required. Saved original launch plists still describe pre-layout paths and must not be installed unchanged. Default build and active installer are Go-only.
