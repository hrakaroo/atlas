# Endpoint contracts

The standard-library Python runner launches the Go service against disposable synthetic libraries and tests it through HTTP and HTTP MCP. It does not import Library, SQLite, application clients or Go internals for behavioral assertions. Python orchestrates the tests; Go owns the tested application behavior.

Each test gets a fresh synthetic library, fresh credentials and an ephemeral loopback port. The harness refuses caller-supplied live roots and external service addresses. Clock/fault controls are restricted to marked test libraries.

## Build and run

From the repository root:

```sh
(cd service && CGO_ENABLED=0 go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
/usr/bin/python3 service/contract/runner.py --capture
/usr/bin/python3 service/contract/runner.py --pattern test_core.py
```

The default binary is `service/.work/atlas-service`. To test an explicit candidate, including a packaged executable:

```sh
/usr/bin/python3 service/contract/runner.py --backend command \
  --command '["/absolute/path/to/atlas-service","--contract-mode","--owner-name","John Smith","--timezone","America/Los_Angeles"]' \
  --capture
```

The harness appends `--library <owned-disposable-root> --port 0`. Commands are argument arrays, never shell text. Use the fictional owner `John Smith` for the current suite because owner-sensitive assertions expect that identity.

All `test_*.py` files in this directory join normal discovery. `--pattern` selects a focused subset. A selected subset is not a substitute for the full suite. Empty discovery, failures, errors, skipped tests and expected failures produce a failing exit status.

## Test helper interface

`EndpointCase` provides `request(path, payload=None, method=None, headers=None)`, returning `(status, headers, raw_bytes)`, plus `get`, `post`, `tool(tool_name, **args)` and `restart(clock=None)`. `tool` decodes MCP text JSON and raises `ToolError` for a tool-level error. Use `request` for HTTP errors, source bytes and response headers.

`self.service` exposes `library`, `pid`, `process`, `start`, `stop`, `restart`, `restore_backup` and `reopen_backup`. The library path is only for fixture/process orchestration. Never inspect its database or item files to assert application behavior. Reading connection settings and permissions is operational connection evidence.

Backup reopening accepts only an endpoint-returned backup inside the service's own disposable `library/backups`, rejects symlinks, stops the process, restores the fixture and restarts at the same root. This validates same-root restore, not cross-root relocation. Assert restored state through endpoints. Failed startup stops its process and cleans the temporary directory.

## Coverage and comparison rules

The suite checks:

- HTTP/MCP authentication, Host/Origin handling, cookie/token scope, security headers, protocol negotiation, method errors, catalog schemas and error envelopes.
- Imports, canonical source identity, duplicate/tombstone handling, full original bytes, dates, stars, tags, relationships and revision conflicts.
- Title/full-text search, ranking, phrases, prefixes, accents and non-Latin text.
- Ownership, structured document mentions, meeting participants, series, feeds, refresh queues and retry behavior.
- Restart persistence, concurrent writes, saved history, backup restoration and bounded recovery behavior.

`inventory.json` specifies the advertised tools and route branches. Schema checks use a frozen fingerprint; do not regenerate expectations merely to make a failure pass. The `exact` comparator is strict about types, keys, absent versus null, list order and bytes. Self-tests alter revisions, source bytes and event counts to verify rejection.

`IdMap` explicitly maps generated identities. Dynamic roots, ports, PIDs and timestamps require narrow assertions or normalization. Do not hide differences through broad string replacement, sorting or golden-fixture rewrites. Synthetic legacy schema setup is a fixture exception; logical state is still asserted through endpoints.

## Reference fixtures and reports

`reference.py` compares repeated disposable Go runs with the checked-in synthetic fixture `fixtures/python-reference.json`. The filename identifies an established contract fixture, not a runtime dependency. It preserves source/history bytes, revisions, types, ordering and Activity counts, while narrowly mapping generated identities and validated paths. The full runner includes this workflow; fixture rewriting is not part of validation.

Each run writes an ignored report under `service/.work/reports/` with counts, individual test IDs/outcomes and observed method/path/status/tool names. Default reports omit tokens, request/response bodies, logs and source text. `--capture` adds synthetic diagnostic bodies with the exact test token and credential-bearing query values redacted. Keep reports private; captures are diagnostic evidence, not automatically accepted golden output.

Run the transport audit against a report created with `--capture`:

```sh
/usr/bin/python3 service/contract/coverage.py /absolute/path/to/report.json
```

Observed tool/route use establishes transport coverage only. Domain correctness requires the scenario assertions themselves. A passing subset or report does not authorize deployment.

## Packaged and native validation

Run the full runner against the exact signed packaged executable. Go embeds the canonical web assets, so no external web directory is required. Packaging checks embedded-asset freshness and pinned dependency notices.

Additional Go checks use synthetic fixtures:

```sh
/usr/bin/python3 service/contract/gate6_binary.py --binary /absolute/Atlas.app/Contents/Resources/bin/atlas-service
/usr/bin/python3 service/contract/gate6_collectors.py --binary /absolute/Atlas.app/Contents/Resources/bin/atlas-service
/usr/bin/python3 service/contract/gate6_helper.py --app /absolute/Atlas.app \
  --go-service /absolute/Atlas.app/Contents/Resources/bin/atlas-service
```

These cover packaged configuration/assets, real Chrome/Zoom collector submissions against fake source files, and native supervisor startup/child shutdown/lock release. They do not register an app, activate launchd or alter scheduled jobs. Signing and native process checks supplement endpoint assertions.

Go unit tests also compare embedded schema SQL to the independent code-only reference under `internal/store/testdata`. That fixture contains schema definitions, not user data. See [validation status](../STATUS.md) and [deployment safeguards](../CUTOVER.md).
