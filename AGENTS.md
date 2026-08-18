# Agent Instructions

## Repository purpose

- Go daemon that turns physical token scans into cross-platform game and media actions through ZapScript.
- Public interfaces include JSON-RPC over WebSocket/HTTP, configuration schemas, UserDB and MediaDB schemas, reader and platform interfaces, and update metadata.
- Use the Go version declared in `go.mod`. Read `docs/ARCHITECTURE.md` when a task crosses subsystem boundaries.

## Commands

- Build current platform: `task build`
- Targeted tests: `task test -- ./pkg/path/...`
- Full tests with race detection: `task test`
- Non-mutating lint: `task lint`
- Apply lint and formatting fixes deliberately: `task lint-fix`
- Supported cross-platform lint: `task cross-lint:all`
- Vulnerabilities: `task vulncheck`
- Nil analysis: `task nilcheck`
- Deadlock detection: `task deadlock`
- Fuzz targets: `task fuzz`
- Benchmarks: `task bench`, `task bench-db`, `task bench-compare`

Use targeted package tests while iterating, then `task test` and `task lint` before finishing normal Go changes. Do not use `task lint-fix` as a read-only check because it can rewrite unrelated files.

Do not run ad hoc `GOOS=...` builds, tests, or lints. Core has CGO and platform-native dependencies; use `task cross-lint:all` for supported cross-platform analysis and leave native execution to CI.

## Testing

- Read `TESTING.md` and `pkg/testing/README.md` before adding substantial test infrastructure.
- Add or update tests for behavior changes and bug fixes. Prefer a regression test that fails before a bug fix.
- Mock hardware, network, process, and platform boundaries. Tests must not require a physical reader or target device.
- Reuse mocks, fixtures, database helpers, and examples under `pkg/testing/` instead of creating parallel infrastructure.
- Use afero for testable filesystem code and fake clocks for time-dependent behavior. Avoid sleeps when deterministic synchronization is possible.
- Use `filepath.Join` for filesystem paths, including tests. Do not encode host-OS separators into production path logic.
- Treat useful behavior coverage as the goal; do not weaken assertions merely to make a test pass.

## Code and compatibility rules

- Use `syncutil.Mutex` and `syncutil.RWMutex`, never the standard mutex types. Deadlock instrumentation depends on these wrappers.
- Use zerolog, not the standard `log` package or direct print statements.
- Preserve backward compatibility for JSON-RPC methods, notifications, API models, configuration files, mappings, launchers, and persisted database data unless a breaking change is explicitly approved.
- Keep UserDB and MediaDB access behind their interfaces. Existing applied migrations in `pkg/database/{userdb,mediadb}/migrations/` are immutable; add a new migration instead.
- Configuration schema changes need migration and compatibility handling for existing installations.
- Treat token contents, NDEF records, barcodes, MQTT messages, ZapScript, API requests, archives, and update metadata as untrusted input.
- Do not hand-edit generated files such as `pkg/database/mediadb/stat1_seed_data.go`; use their documented generator.
- Do not add dependencies, platforms, readers, launchers, or public API surface without discussion.

## High-risk areas

- Authentication and authorization: `pkg/api/middleware/`, `pkg/config/auth.go`, client pairing, encryption, and profile permissions.
- Parsing and execution: `pkg/readers/shared/ndef/`, `pkg/zapscript/`, command execution, archive extraction, and launcher handling.
- Durable state: database migrations, backups, config migration, and profile data swapping.
- Updates: `pkg/service/updater/`, `pkg/service/updater/otameta/`, `.github/workflows/ota-*.yml`, `.github/actions/ota-metadata/`, and `scripts/generate-update-manifest/`.

For updater, signing, promotion, rollout, withdrawal, or key changes, read `docs/ota-runbook.md` first. Never publish OTA metadata, alter a rollout, rotate trust keys, tag a release, or run deployment workflows without explicit approval. Preserve signature verification, monotonic manifest generations, asset digests, rollback protections, and fail-closed install behavior.

Do not read, print, or commit `.env`, API keys, private signing material, local databases, logs containing user data, or generated runtime state.

## Specialized validation

- Concurrency, lifecycle, or lock changes: `task deadlock` and relevant race tests.
- Untrusted parsers or protocol changes: run the relevant fuzz target; use `task fuzz` when the scope warrants the full set.
- Dependency or security-sensitive changes: `task vulncheck`.
- Performance work: read `docs/optimization-targets.md`, use allocation-reporting benchmarks with `b.Loop()`, and include `task bench-compare` evidence. Do not replace committed baselines without approval.
- Workflow changes: run `actionlint` on the changed workflow or all workflows.
- Before pushing cross-platform changes: `task cross-lint:all` and the repository's normal test/lint checks.

## Git and pull requests

- PR titles use Conventional Commit types enforced by `.github/workflows/pr-title.yml`.
- PR descriptions must not include a test-plan section.
- Do not commit, push, rewrite history, update benchmark baselines, or modify release state unless explicitly asked.

## Completion

Report exactly which checks ran and any that were skipped. Do not claim completion while a relevant check is failing or the implementation is partial.
