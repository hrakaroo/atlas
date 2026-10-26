# Atlas Go service

The Go service owns the library and serves authenticated loopback HTTP/MCP and the embedded web interface. Python helpers connect through HTTP; they do not open the database. Native packaging lives in the repository root.

- [Application specification](../docs/specification.md): current product behavior.
- [Validation status](STATUS.md): checks and platform scope.
- [Endpoint contracts](contract/README.md): repeatable isolated validation.
- [Transport inventory](contract/inventory.json): tools and routes used by contract checks.
- [Operations](../docs/operations.md): setup, upgrades, backups and recovery.
- [Deployment safeguards](DEPLOYMENT.md): controlled deployment and optional local rollback.

Build and test from this directory with `go build ./cmd/atlas-service`, `go test ./...` and `go vet ./...`. The root repository guide includes endpoint validation and signed native builds. Generated binaries, reports and disposable libraries belong under ignored `.work/`.

The transport inventory and synthetic fixtures remain with the active tests. Default builds and validation work from a fresh checkout.
