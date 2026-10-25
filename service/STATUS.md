# Service validation status

Go is the active backend. Runtime validation and signed app builds have been exercised on macOS arm64; Intel macOS has compile-only validation.

The latest source cleanup passed all 98 isolated endpoint tests, 16 client/packaging tests and nine JavaScript checks. Go unit tests, race checks and vet passed during repository layout verification. A fresh checkout was built and tested without the ignored Python archive. Signed app and native supervisor checks also passed. Re-run applicable checks after changes; historical results do not certify a later build.

See [endpoint validation](contract/README.md), [repository setup](../docs/repository.md) and [operations](../docs/operations.md). Generated reports and candidates stay in ignored `.work/`.

Completed migration plans, acceptance reports, source fingerprints and machine-specific deployment evidence are preserved locally under ignored `old/migration/`. They are not required for a checkout, build or running service. Current endpoint tests and the transport inventory remain in `contract/`.

Structured document mentions are enforced by Go: only observed people-chip identities or single-address mailto links can create mentioned-person links. Six endpoint regressions cover invalid prose/evidence, email identity reuse, conflicting names, missing chip names, Trash and intake retry. The signed packaged service also passed all 98 contracts and the native helper check. The deployment preserved the endpoint inventory, existing relationships and connection settings; private backup/deployment evidence remains ignored under `.work/`.
