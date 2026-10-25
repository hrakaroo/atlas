# Repository and fresh installations

`service/` is the Go backend, including schema SQL, module checksums, endpoint contracts and dependency notices. `app/` contains the Swift wrapper and an HTTP-only Python client. `scripts/`, `tests/`, `web/`, `docs/` and `schedules/` remain source-controlled material. No initial database dump is needed: the Go store creates the schema in a fresh library.

`.gitignore` excludes the local `old/` Python archive, private `library/`, SQLite databases/sidecars, backups/imports, credentials and signing keys, app bundles, generated `build/`/`dist/`, `service/.work/`, caches and temporary files. The archive is retained on this Mac; it is not required to build, run or test a checkout. Ignore rules do not rewrite earlier Git history. Before committing, inspect `git status` and the staged diff; keep this repository private because the schedule prompts and project documentation contain internal workflow context.

## Another person or computer

1. Clone the source, install Go, Python 3 and macOS developer tools, and configure an appropriate reusable signing identity. Never copy someone else's private signing key or credentials into the repository.
2. Follow the Go setup commands in the root README. Use an explicit owner name, IANA timezone and consistent absolute library root; there are no owner-specific Python rules to edit.
3. A fresh library starts empty. Relocating an existing collection to another root/account is a separate procedure because preserved source paths are absolute; tested restore/handoff uses the original root. Back up database and saved files together through Atlas.
4. Create fresh local service/MCP credentials and grant ordinary macOS Documents access. Do not copy `.service` settings, activation markers or tokens from another system.
5. Connect the intended Google/Calendar/Slack accounts and recreate the schedules from their exported definitions with new destination chat IDs. Copying a prompt file does not register a job. See the schedules guide.
6. Build and run Go validation below before enabling unattended collection.

## Validation from a checkout

```sh
(cd service && CGO_ENABLED=0 go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
(cd service && go test ./... && go vet ./...)
/usr/bin/python3 -m unittest discover -s tests
/usr/bin/python3 service/contract/runner.py --capture
```

Run the nine JavaScript checks in `tests/test_*.js` with Node. `zsh build.sh` produces a signed Go app without installing or registering it. Frozen synthetic reference fixtures and historical acceptance documents stay in Git. Optional Python differential/recovery suites require the local ignored archive and are not default checkout dependencies. Standard Go endpoint tests have been verified with `old/` entirely absent.

## Repository readiness check — 2026-10-24

Repeated verification passed: 92 isolated Go endpoint tests, 16 Python client/packaging tests, nine JavaScript checks, Go unit tests and vet. The isolated Go app signature verified. Earlier clean-checkout validation also built and tested with the ignored Python archive absent.

The 146 existing Git-eligible files contain no private runtime paths, database files, generated app/build output or detected credential patterns. The current live credential was not present. All 283 historical Git blobs were also checked for common credential patterns and that current credential; no matches were found, and historical paths contained no private library/database/build files. Pattern checks cannot guarantee the absence of every possible secret, so review the final staged diff before pushing.

Current documentation links and diff whitespace checks passed. Schedule recreation instructions were corrected for explicit Go owner/timezone configuration and configurable library paths. The running Go service and live library were unchanged.

Git is already initialized locally. No remote is configured, and this verification did not stage, commit or push changes. Create a **private** remote repository: the source and schedule snapshots contain internal workflow context. Review and commit the rename/deletions together with the new `service/` files; do not force-add ignored material.
