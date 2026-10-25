# Service validation status

Go is the active backend. Runtime validation and signed app builds have been exercised on macOS arm64; Intel macOS has compile-only validation.

The owned-storage update passed all 108 isolated endpoint tests, 16 client/packaging tests and nine JavaScript checks. Go unit tests, race checks and vet passed during repository layout verification. Fresh-checkout build and endpoint validation also passed. Signed app and native supervisor checks also passed. Re-run applicable checks after changes; historical results do not certify a later build.

See [endpoint validation](contract/README.md), [repository setup](../docs/repository.md) and [operations](../docs/operations.md). Generated reports and candidates stay in ignored `.work/`.

Current endpoint tests and the transport inventory live in `contract/`. Keep generated validation reports and machine-specific deployment records private and outside source control.

Structured document mentions are enforced by Go: only observed people-chip identities or single-address mailto links can create mentioned-person links. Six endpoint regressions cover invalid prose/evidence, email identity reuse, conflicting names, missing chip names, Trash and intake retry. The signed packaged service also passed all 98 contracts and the native helper check. The deployment preserved the endpoint inventory, existing relationships and connection settings; private backup/deployment evidence remains ignored under `.work/`.

Retired-backend removal: default validation targets Go only. The 98 endpoint tests, 16 client/packaging tests, nine JavaScript checks and Go tests/vet pass without an archived implementation. Additional Go-only checks pass for rich-state restarts and alternate-user configuration (4 tests), interrupted-write/source recovery (10 tests), Trash recovery (4 tests) and process ownership/reopen behavior. Frozen source/schema fixtures remain independent checked-in references; validation does not regenerate them.

Owned storage: People, Teams and Projects keep descriptions, revisions and historical original bytes in SQLite. Ten new endpoint checks cover source ownership, exact legacy description bytes, shared-source relocation, attachments, Trash, backups, independent Zoom transcripts, Confluence identity and interrupted-migration recovery. Validation uses disposable roots; fixture SQL only prepares unopened historical databases, while all state/content assertions use public endpoints. The full library migration is rehearsed against a private API backup before deployment.
