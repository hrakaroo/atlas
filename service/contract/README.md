# Endpoint contracts

This standard-library Python runner tests independently launched disposable Atlas
services through HTTP and HTTP MCP. It never imports Library, SQLite, application
clients or Go internals for behavioral setup/assertions. Each EndpointCase gets a
fresh synthetic root, fresh credentials and an ephemeral loopback port.

From the repository root:

```sh
/usr/bin/python3 service/contract/runner.py --capture
/usr/bin/python3 service/contract/runner.py --pattern test_core.py
/usr/bin/python3 service/contract/runner.py --backend command --command '["/absolute/path/to/alternate-service"]'
```

A command receives appended `--library <owned-disposable-root> --port 0`.
It must write the protected compatible connection settings and support the
existing endpoints. Commands are argument arrays, never shell text. No default
or command option accepts a live library or external service address.

The default backend is Go at `service/.work/atlas-service`, built with CGO disabled. Every process uses a marked disposable library. `--backend command --command '["/absolute/binary","--contract-mode","--owner-name","Your Name","--timezone","UTC"]'` selects another built Go candidate.

The explicit `--backend python` option requires local `old/contract/python_backend.py` and the ignored archived backend. It fails clearly if that optional archive is absent. Cross-engine specialized packs are historical optional checks; ordinary checkout tests and native Go helper smoke require no archived Python files. Process clocks/fault controls remain confined to marked fixtures.

## Test helper interface

`EndpointCase` provides `request(path, payload=None, method=None, headers=None)`
returning (status, headers, raw bytes), `get`, `post`, `tool(tool_name, **args)`
and `restart(clock=None)`. `tool` decodes the MCP text JSON and raises
`ToolError` for a tool-level error. Use `request` when asserting HTTP errors,
exact source bytes or response headers.

`self.service` exposes `library`, `pid`, `process`, `start`, `stop`,
`restart`, and `restore_backup`/`reopen_backup`. The library Path is for
approved fixture/process orchestration only; never inspect its database/item
files as assertions. Reading connection settings/permissions is operational
connection evidence, not application state inspection.

Backup reopening accepts only an endpoint-returned backup inside that service's
own disposable `library/backups`, rejects symlinks, stops the process, copies
the fixture before replacing the disposable library, and restarts at the same
root. Existing saved source paths are absolute; this proves same-root restore,
not cross-root relocation. All resulting content is checked through endpoints.
Service refuses caller-supplied library roots. Failed startup stops its process
and cleans the temporary directory.

## Scope and comparison rules

The complete advertised MCP input schema map has a frozen SHA-256 fingerprint
captured from the reviewed Python endpoint, including nested types and enums.
Tests do not regenerate it. Tool order is checked against the reviewed inventory.

The core/transport scenarios freeze:
- HTTP authorization, Host/Origin, cookie versus MCP token scope, URL-token scope,
  static delivery/security headers, inherited method errors, malformed/oversized
  requests, protocol negotiation, notification, tool catalog and error envelopes.
- Imports across existing record kinds, exact Unicode/CRLF source bytes/hash,
  full response key/type shape, UI/MCP cross-surface reads/writes, revision conflicts,
  date/star/tag/undo behavior and change-counter semantics.
- Canonical Google ID/resourcekey identity, duplicate quietness, title/full-text
  search, phrase/prefix/accent/CJK behavior, ranking and combined filters.
- Restart persistence, simultaneous same-revision edits (one winner and one
  activity event), endpoint-created backup restore/read-back.

`exact` is recursive and strict about types, keys, absent versus null, list
length/order and bytes. Self-tests deliberately mutate revisions, source bytes
and event counts and prove rejection. `IdMap` offers an explicit bijection for
generated IDs; identity rewriting is never automatic. Dynamic roots/ports/PIDs/
timestamps must be asserted or normalized explicitly in each future comparison.
Do not mask changes by sorting, string coercion or broad golden rewrites.

These are executable expected-behavior scenarios, not a claim of complete
differential coverage. The same scenarios can select another implementation.
Historical migration, all workflow tools, crash/fault recovery, native packaging
and full Python↔Go differential/rollback acceptance remain separate required
scenario packs and gates. Passing this subset does not pass Gate 1 or authorize
Go cutover. Other `test_*.py` files placed in this directory join the runner.

Each run writes an ignored `service/.work/reports/<backend>-<time>.json`
with test counts, individual test IDs and outcomes, and actually observed
method/path/status/tool names.
Reports omit tokens, request/response payloads, process logs and source text.
Observed endpoint invocation is not an assertion of complete tool coverage.
Skipped tests, expected failures and empty discovery produce a failing runner exit. No baseline is rewritten on failure.

## Diagnostic capture versus differential baseline

Add --capture to record per-scenario synthetic requests/responses (parsed JSON or base64 bytes) in the
ignored report. The exact test token is redacted, and credentials/request headers
are never recorded; token query values are explicitly redacted. Dynamic IDs, roots, ports and times stay explicit. These are
diagnostic captures, not accepted normalized golden output.

The complete normalized baseline was delivered in Gate 1 and remains frozen.
Reviewed per-field identity/time/root rules permit differential replay. Broad
UUID/string/timestamp replacement remains absent because it could hide identity
mistakes, broken hashes or changed source bytes.

## Reviewed Python reference and transport audit

Run the complete endpoint suite with /usr/bin/python3 service/contract/runner.py --backend python --capture. The runner fails on empty discovery, failures, errors or skipped/expected-failure cases, and saves ignored reports with individual test IDs/outcomes.

Run /usr/bin/python3 service/contract/reference.py to compare two new disposable runs with fixtures/python-reference.json. The reference uses a fixed process clock and narrowly mapped generated identities/validated paths; source hashes, bytes, revisions, types, ordering and event counts stay exact. Baseline rewriting requires explicit --write-baseline and rejects command/environment overrides. It is never part of validation. The reference is one stateful golden workflow; other endpoint scenarios assert their own behavioral expectations rather than comparing arbitrary raw traces.

Run /usr/bin/python3 service/contract/coverage.py <report.json> to audit actual successful use of every advertised tool and HTTP branch. This is transport coverage, not proof that every domain invariant is asserted; pair it with the scenario evidence ledger and independent review.

The original Gate 1 Python baseline comprised 61 passing contract tests; the current strengthened common suite has 87 tests, including source history, backup recovery at the same library path, external-edit reconciliation, reference labels, combined search filters, ownership cooldowns, ambiguous intake and races. Gate 5 additionally runs explicit cross-engine/configuration, recovery and Trash interruption suites; these require both executables and stay outside ordinary discovery.

## Gate 3 acceptance profile

Run `/usr/bin/python3 service/contract/runner.py --backend python --profile gate3`
to establish this explicit subset against Python, then select the compiled Go
command with the same profile. The reviewed `profiles.json` names 24 individual
cases: six core, two search, five preservation, one core history, five
transport/comparator cases, one JSON-fidelity, one argument-boundary and three
date/source-identity regressions. Reports retain the profile and all selected test IDs.

The new history case uses imports and summary edits, so it validates original
bytes and all summary revisions without requiring Gate 4 document refresh.
Profile selection does not mark other tests skipped or remove them from normal
full-suite discovery. Backup, refresh, relationship workflows and full recovery
remain required in their existing tests and later acceptance gates.
`--pattern` and `--profile` are mutually exclusive. A missing selected test
becomes a failing unittest loader case rather than silently disappearing.

## Complete domain acceptance

Gate 4 historically passed the full 79-test suite twice on Go and once on Python, including the frozen reference workflow. The current Go suite uses the fictional owner `John Smith`; supply `--owner-name "John Smith"` when running it through an explicit command. Historical acceptance records retain their original configuration. The archived Python implementation hardcodes its original owner for meeting grouping, so its owner-sensitive comparisons require the matching historical test revision rather than the current Go fixtures. That historical gate covered domain behavior; persistence, configuration and packaging were accepted separately in Gates 5–6. Historical reports are retained under ignored `old/migration/plan/`.

## Gate 5 persistence and recovery

The common 87 tests pass on both engines. Run gate5_crossengine.py, gate5_recovery.py and gate5_trash_recovery.py with --go-command containing a JSON argv for the absolute compiled executable, --contract-mode, --web-root and the synthetic reference owner. The original Gate 5 suites contained 4, 9 and 4 tests; the strengthened recovery suite now contains 10, including the in-process restart-required boundary. Test loops exercise additional writer/reader combinations without inflating the named test count. Reports retain named outcomes.

Synthetic legacy schema construction is a fixture setup exception; all logical-state assertions use endpoints. Test-only fault orchestration requires marked temporary fixture roots. No fault endpoint exists. Current compatible Python and Go understand the same refresh/Trash recovery intents. Original baseline Python does not. Historical reports are retained under ignored `old/migration/plan/`.

## Gate 6 packaged execution

Run the common suite with the signed packaged executable, then repeat gate5_crossengine.py, gate5_recovery.py and gate5_trash_recovery.py against that same executable. No external web directory is needed: canonical web bytes are compiled into the native service. The real Chrome/Zoom collectors remain Python HTTP clients.

Run gate6_binary.py and gate6_collectors.py with --binary naming the packaged executable. Their three and two tests cover embedded assets/configuration and actual synthetic collector CLI submissions. gate6_helper.py accepts --app, --go-service and --backend to exercise the signed supervisor with the packaged native binary, retained client, child termination and Go library reopen. It performs no registration or launchd activation.

Go build checks pinned offline dependency notices and canonical embedded-web freshness. Historical reports are retained under ignored `old/migration/plan/`.

The default Go unit suite compares schema SQL to a frozen code-only Python reference under `internal/store/testdata`; no backend import is needed. The optional `go test -tags=atlas_python_reference ./internal/store` comparison requires local old/. Other cross-engine/fault packs are explicitly optional historical tests with that same archive requirement.
