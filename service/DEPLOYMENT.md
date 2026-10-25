# Service deployment and recovery

Atlas runs one Go service behind the native window, HTTP API and MCP interface. Use this guide for a deliberate service update. Building a candidate does not install it or authorize replacing the running service.

## Prepare a candidate

1. Build and test against marked disposable libraries. Never point an experimental service at the live library.
2. Package and sign the app, then verify its signature and run the endpoint suite against the exact packaged executable. See [endpoint validation](contract/README.md) and [operations](../docs/operations.md#isolated-go-preview).
3. Record the source revision, app/binary hashes, stable app location, absolute library root, owner name, IANA timezone and current service label/port. Keep credentials and machine-specific evidence outside Git.
4. Keep the native app and helper at the paths used by the launch specification. Build output under `.work/` is for validation; select a stable location for installation.

A candidate can be built and packaged from the repository root with:

```sh
(cd service && CGO_ENABLED=0 go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
zsh build-go.sh --go-service "$PWD/service/.work/atlas-service"
```

Dependencies and the pinned module graph must be available. Packaging checks embedded web assets and third-party notices, signs inside-out, and verifies the result. It does not launch, register or install the app.

## Preserve existing data

An upgrade reuses the same library root, SQLite database and saved item/import directories. It does not export/re-import or replace the collection. Back up the database and content together through Atlas's backup action; copying only SQLite is insufficient.

Saved source paths are absolute. Cross-root relocation is a separate data migration requiring a reviewed path-rebase procedure. Do not open a real backup at an arbitrary test path or launch a second owner for validation.

With scheduled writers paused, capture private endpoint state before stopping the service: active and Trash identities, dates/stars/revisions, summaries and source hashes, People and entity relationships, series/feeds, Activity, changed-document cursors and pending intake. Verify available source/history bytes through authenticated endpoints. Preserve inaccessible Trash source files in the complete backup. Compare identities, content and organization after deployment; matching counts alone are insufficient.

## Deploy

1. Obtain authorization for production replacement and pause scheduled writers, preserving their prompts, timing, routing and previous enabled state.
2. Create and verify a complete backup while the current service is healthy. Retain its checksums and the previous signed Go app and launch specification privately.
3. Stop the installed job with `/usr/bin/python3 scripts/service.py stop`. Confirm both supervisor and Go child have exited before replacing files or starting another owner.
4. Review the intended specification without installing it:

   ```sh
   /usr/bin/python3 scripts/service.py print-launch-spec --backend go \
     --app /absolute/stable/path/Atlas.app \
     --library /absolute/existing/library \
     --owner-name 'Actual Full Name' --timezone America/Los_Angeles
   ```

5. Install/start the reviewed signed app using the explicit intended app, library, owner and timezone. Handle ordinary macOS Documents consent locally. Do not alter certificate trust automatically or bypass service configuration.
6. Verify authenticated health, the intended library and the new Go PID. Confirm that PID is the selected launchd supervisor's child, rather than accepting an unrelated healthy process. Compare the saved endpoint state and source/history bytes. Check native and MCP/helper connections; retain the existing endpoint/token when unchanged.
7. Validate the intended change through endpoints, using reversible or rejection checks where possible. Verify their effect and restore any intentional test changes. Resume jobs in their previous enabled state and confirm their settings.

The launchd job label is `local.atlas.server`, separate from the signed helper's bundle identifier. Keep lifecycle tooling and the installed label in sync. Killing only a child is insufficient while launchd KeepAlive is loaded; unload the owner before replacing it. The service lifetime lock rejects a competing owner before SQLite opens.

## Recover from a failed update

Stop/unload the failed service first and confirm its child exited. Restore the previous verified signed Go app and launch specification only after checking compatibility with the current schema and saved data format. Reopen the same library root, verify endpoint state and clients, then resume scheduled writers.

If the previous version cannot read the current data format, use a verified consistent full backup at the supported original root. Explicitly reconcile writes made since that backup; do not silently discard them or restore only SQLite.

If recovery reports a conflicting manual summary or incomplete cleanup, preserve the journal and manual text. Resolve the conflict before restarting; do not delete journals or force a summary over unrelated edits.

## Validation limits

Synthetic endpoint and fault tests exercise named process interruption/restart boundaries. They do not prove physical power-loss atomicity or every possible failure. An unacknowledged summary-only edit may recover old or new text, and an interrupted operation may retain an additional exact summary revision.

Runtime tests and signed builds have run on macOS arm64. Intel macOS has compile-only validation. Signing verification does not establish Documents-consent persistence or replace Developer ID/notarization for distribution. Candidate checks do not replace deployment-time backup, state comparison or native consent verification.
