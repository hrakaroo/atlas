# Atlas operations

Commands below run from the Atlas project directory. They describe setup and maintenance; reading this guide does not execute them. See [architecture](architecture.md) and [scheduled tasks](../schedules/README.md).

## Installed Go deployment

The signed Go deployment is `dist/Atlas.app`, supervised under the launchd job label `local.atlas.server`. Status/start/stop/restart manage that Go job. The service opens the existing library in place; no export/re-import is involved. Historical deployment records are retained locally under ignored `old/migration/`.

## Fresh Go setup

Use the commands in the root README. `zsh build.sh` now builds/signs Go by default into `build/Atlas.app`; it never modifies or registers the active `dist` app. `scripts/service.py install` now installs a Go supervisor and requires the owner name and timezone. It verifies the app signature before modifying launch configuration. Use `--app` and `--library` consistently; create a new empty library folder before requesting Documents access.

`request-access` opens the selected app's native helper to request ordinary macOS access. `configure-mcp` checks health, preserves unrelated connector settings and writes credentials locally without printing them. Installation does not configure Google/Slack connections or register scheduled jobs.

For upgrades, build/sign/test an isolated candidate first. Back up the complete library, stop the existing owner, deliberately replace the stable app/helper and install/start the intended job, then compare data and clients before resuming scheduled writes. Do not replace a running deployment casually. The Go service embeds its web UI, so editing `web/` alone does not update the installed app.

## Stable signing for local builds

The build signs Atlas and its nested service helper with the Keychain identity named `Atlas Local Development`. It no longer silently signs ad hoc. Keep using the same certificate/private key across rebuilds; creating a different certificate with the same name is not the same signing identity.

For another approved identity, set `ATLAS_SIGNING_IDENTITY` to its unambiguous certificate name or fingerprint when running the build. Signing happens inside-out (helper first, enclosing app second), followed by strict signature verification. No private keys are stored in this repository.

For a local self-signed certificate, create a Code Signing certificate in Keychain Access. If macOS reports `CSSMERR_TP_NOT_TRUSTED`, review that certificate’s Trust settings yourself; any trust grant should apply to Code Signing only, leaving unrelated trust policies at their defaults. Do not broadly trust it for websites or other uses. Keychain may ask you to authorize the signing tool’s use of the private key.

Changing from ad-hoc signing to the certificate can require one more Documents approval. Verify normal relaunch and a subsequent rebuild preserve permission before declaring the repeated-prompt issue resolved. A local self-signed identity is for this Mac’s development use, not a substitute for Developer ID/notarization for distribution. Managed-device policy still applies.

## Status and lifecycle

```sh
/usr/bin/python3 scripts/service.py status
/usr/bin/python3 scripts/service.py restart
/usr/bin/python3 scripts/service.py stop
/usr/bin/python3 scripts/service.py start
```

Status checks authenticated health and reports the service identity, library path, version and backend PID, without printing the token. `stop` unloads the job; `start` loads or starts it again. Closing the Atlas window is not a service stop. The local scheduled workflows also require the desktop scheduler to be running and the Mac available.

## Applying changes

| Changed component | Required action |
| --- | --- |
| `service/` backend / embedded web | Build, test and sign an isolated candidate; deliberately deploy/restart the Go owner |
| Local `old/` archive | Historical reference/explicit rollback only; never a runtime dependency |
| `app/service_client.py` HTTP client | New client invocations use the updated client; no backend replacement |
| Native Swift window or helper / app icon | Build again; reopen Atlas; restart the service when its helper changed |
| `web/` | Go: regenerate embedded assets and rebuild/deploy; Python rollback: reload project-served assets |
| Installation location or Python runtime | Stop the old service, update paths, rebuild if needed, reinstall and verify health |
| Registered scheduled prompt/cadence/state | Update with desktop automation tools, then export schedule definitions |
| Shared intake instructions | Edit `schedules/intake-processing.md`; collection jobs read this file during runs |

A helper invocation no longer reloads backend code. Rebuilding alone does not restart a running shared service. Restarting Atlas alone does not restart it either.

## Troubleshooting

| Symptom | Check and action |
| --- | --- |
| Atlas reports service unavailable | Run status, then start or restart. Read the private service logs if health still fails. |
| Installation cannot read Documents | Allow normal Documents access for Atlas Service under System Settings → Privacy & Security → Files & Folders; rerun request-access/install. Do not reset or bypass privacy controls. |
| MCP still launches an old per-client process | After healthy installation, run configure-mcp and restart ChatGPT. Activated `--mcp` compatibility mode only forwards HTTP. |
| Tool changes do not appear | Restart the service, then reconnect the MCP client. The helper also uses that same service catalog. |
| Port/library mismatch or duplicate service | Confirm the intended library and connection settings without exposing the token. Stop the old job before changing its port/location. Do not start a second database owner. |
| Chrome tab has not imported | Saved sessions can lag. Check collector output and Atlas pending intake; untitled drafts and tombstoned IDs are excluded. Up to five new items are processed per run. |
| Document owner is still missing | Inspect wildcard metadata permissions; incomplete permissions and shared-drive ownership can prevent an individual owner link. Keep uncertainty; do not infer an author or owner. |
| Refresh request remains checked | Failed/incomplete reads stay queued. Inspect the scheduled run’s reason; do not clear the flag to acknowledge an unsuccessful refresh. |
| Generic icon remains in the Dock | Quit Atlas, remove the old shortcut and drag the rebuilt app into the Dock while closed. |

Do not delete `library/.service/enabled` to restore an old fallback during an outage. Repair service availability. After activation, failure rather than a second database connection is intentional.

## Private runtime files

| Path | Purpose |
| --- | --- |
| `library/.service/connection.json` | Loopback endpoint, library identity and private bearer token |
| `library/.service/enabled` | Verified shared-service activation boundary |
| `library/.service/server.lock` | Per-library singleton service lock |
| `library/.service/stdout.log`, `stderr.log` | Service diagnostics; review before sharing |
| `~/Library/LaunchAgents/local.atlas.server.plist` | Per-user login/start/restart definition |
| `$CODEX_HOME/config.toml` | Local MCP registration, including private authentication |
| `$CODEX_HOME/config.toml.before-atlas-http` | Protected pre-migration configuration backup |

Do not commit runtime files, source snapshots, backups, cookies or credentials. Local scheduling exports contain internal workflow/channel details and belong in a private repository. Do not expose the loopback service with a public proxy or tunnel.

## Backups and moving machines

Use Atlas’s backup action for a consistent copy of SQLite plus item files. Do not copy only the database or synchronize a live SQLite file through a file-sync service.

For an existing library, the validated backup restore/handoff reopens at the same original absolute root: saved source paths are absolute. Relocating existing data to another root or username requires a separately reviewed path-rebase procedure and has not been accepted or implemented here. A fresh library for another account, explicit root, owner and timezone is supported and tested. On another Mac, keep the consistent private backup separate from source code and plan that relocation before opening it. Do not reuse the previous Mac’s `.service` connection/activation/lock/log files, launch agent or MCP credentials. Preserve library data, queues and tombstones; run fresh service installation/configuration for that Mac. Set the native `--library`/`ATLAS_LIBRARY` and registered paths consistently first. Do not start the restored app against stale runtime settings.

Recreate scheduled jobs using [the schedule migration instructions](../schedules/README.md), new destination chat IDs and current account connections. Copying the manifest or prompts does not register jobs.

## Checks

For implementation changes run:

```sh
/usr/bin/python3 -m unittest discover -s tests
for test_file in tests/test_*.js; do /opt/homebrew/bin/node "$test_file" || exit; done
zsh build.sh
```

The service tests use a temporary library and exercise HTTP/MCP/client parity, authentication, serial ownership and migration behavior. They do not justify opening production SQLite from client scripts.

For documentation-only updates, verify links, commands against their parsers, and exported prompts/settings against registered definitions. A documentation update does not require restarting the production service.


## Isolated Go preview

The production Go service is active at `dist/Atlas.app`. New candidate builds remain isolated and do not authorize replacing the active owner. The Go executable's release mode is `--serve` with an absolute library, nonblank `--owner-name` and explicit IANA `--timezone`; contract-only clocks/fault controls are rejected in release mode. Do not launch it on the existing library during migration validation.

Build the native preview using an already reviewed Go candidate. With the pinned module cache available, compile the current source first:

```sh
(cd service && CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
```

Then package it:

```sh
zsh build-go.sh --go-service /absolute/path/to/atlas-service
```

The packager verifies generated embedded web bytes against canonical `web/` before compiling the wrapper; regenerate assets intentionally with `go generate ./internal/assets` from `service/` after web changes. The supplied `--go-service` is a trusted prebuilt candidate, so its source/binary fingerprint must be recorded during review. These checks do not claim a bit-for-bit reproducible signed application.

The default output is ignored `service/.work/build/Atlas.app`; `--output-directory /absolute/directory` selects another isolated output. No Launch Services registration, launchd changes, MCP changes, schedules or app launch occur. The preview includes its Go service under `Contents/Resources/bin/atlas-service` and uses the same certificate selection as normal builds, with no ad-hoc fallback. Rebuilding stages a new verified bundle before replacing that output.

The retired Python backend, its original Swift/build scripts and Python-specific tests live in ignored `old/`. Its optional reference tests use `service/contract/runner.py --backend python`. They are not required for a fresh checkout. For a deliberate Python rollback, first stop Go, then use the archived `old/scripts/service.py` with an explicit real library and `old/build/Atlas.app`; do not use saved pre-layout plists containing retired paths. A checkout without the local archive cannot perform that historical Python rollback.

A concrete Go launch specification can be inspected without writes, network requests or registration:

```sh
/usr/bin/python3 scripts/service.py print-launch-spec --backend go \
  --app "$PWD/service/.work/build/Atlas.app" \
  --library /absolute/path/to/an-isolated-library \
  --owner-name 'Your Full Name' --timezone America/Los_Angeles
```

The Go launch specification uses the resolved existing library directory as its working directory; it does not require the source checkout for backend startup. The retained Python specification still uses the checkout. Record source/artifact fingerprints for each new candidate in private local deployment evidence.

`install` now targets Go by default and uses the production job label. A printed launch specification is still read-only; never run an experimental owner against the live library. Pause scheduled writes, create/verify a complete backup, stop the old owner and deliberately install the reviewed signed candidate before checking health, data and clients. Never run both engines or remove activation markers to bypass outages.

Native UI selection uses `--library`, then `ATLAS_LIBRARY`, then `~/Documents/Atlas/library` for the current account. A clone at a different location must use a consistent explicit root for window and service. `--require-service` or the Go preview's built-in requirement makes missing service settings an error rather than starting a separate backend.

For automated native helper smoke tests, use `service/contract/gate6_helper.py --app <absolute-built-app> --go-service <absolute-built-app>/Contents/Resources/bin/atlas-service`. It creates its own marked temporary library, checks the backend is the helper's child, exercises real HTTP clients and full preserved bytes, terminates the helper, verifies child shutdown and lock release, and reopens through Go. It never calls launchd or registers an app. Run the common endpoint contracts directly against the packaged Go executable as well; a helper's PID differs from its child, so the ordinary harness readiness check is deliberately unchanged.


Go previews include `Contents/Resources/ThirdPartyNotices.txt`. Reproduce its tracked source and component manifest offline with `/usr/bin/python3 service/tools/notices.py` after caching the pinned module graph. `--check` verifies exact source bytes and component versions without rewriting; preview builds run that check and fail if any required cached notice is unavailable or stale. The scope conservatively includes every pinned module-graph dependency (including build/test-only and optional components) plus the Go toolchain's LICENSE/PATENTS files (including Homebrew’s distribution-parent LICENSE layout); listing a component does not claim it is linked. Notice contents are copied unchanged, and the manifest records file lengths and SHA-256 hashes without local cache paths. This is a build-time tool, not a runtime Python requirement.
