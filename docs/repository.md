# Repository and fresh installations

`service/` contains the Go backend, schema SQL, pinned module checksums, endpoint contracts and dependency notices. `app/` contains the Swift wrapper and HTTP-only Python client. `scripts/`, `tests/`, `web/`, `docs/` and public schedule instructions are source-controlled material. No database dump is needed: the Go store creates the schema for a fresh library.

## Source and private local files

`.gitignore` excludes library data, SQLite databases and sidecars, source imports/backups, credentials and signing keys, generated app/build output, `.work/`, caches and local archives. It also excludes the machine-specific schedule manifest and private briefing, author-blog and incident prompts. Ignore rules do not remove files already committed or erase earlier Git history.

Before publishing, inspect the staged diff and file list. Keep tokens, source snapshots, browser sessions and internal channel/account configuration out of Git. Use generic public instructions and keep personal schedule exports local. Set the intended repository-local Git author identity; authentication keys and commit authorship are separate.

## Another person or computer

1. Clone the source; install Go, Python 3 and macOS developer tools. Configure a reusable signing identity with a private key stored outside the repository.
2. Follow the root README setup commands with an explicit owner name, IANA timezone and consistent absolute library root.
3. A fresh library starts empty. Moving an existing collection to a different root is separate because saved source paths are absolute. Back up SQLite and content together through Atlas and review relocation before opening transferred data.
4. Create fresh local service/MCP credentials and grant ordinary Documents access. Do not copy another machine's `.service` runtime settings, tokens, locks or activation markers.
5. Connect the intended source accounts and recreate jobs from the [schedule instructions](../schedules/README.md), using new destination chat IDs and private local parameters. Copying a prompt does not register a job.
6. Build and validate before enabling unattended collection.

## Validation from a checkout

```sh
(cd service && CGO_ENABLED=0 go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
(cd service && go test ./... && go vet ./...)
/usr/bin/python3 -m unittest discover -s tests
/usr/bin/python3 service/contract/runner.py --capture
```

Run the JavaScript checks in `tests/test_*.js` with Node. `zsh build.sh` produces a signed Go app without installing or registering it. Synthetic fixtures remain in Git; generated reports and disposable libraries do not. See [endpoint contracts](../service/contract/README.md) for packaged/native validation and [operations](operations.md) for controlled deployment.
