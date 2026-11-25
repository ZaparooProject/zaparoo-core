# Agent Instructions

Zaparoo Core is a Go daemon that turns token scans into game and media launches through ZapScript. Use the Go version in `go.mod`. Read `docs/ARCHITECTURE.md` when a task crosses subsystem boundaries.

## Commands

- Build for the current platform: `task build`
- Test one package: `task test -- ./pkg/path/...`
- Run one test: `go test -race -tags=validator_novalidatefn,expr_static_methods ./pkg/path/ -run TestName`
- Full tests with race detection: `task test`
- Lint without changing files: `task lint`
- Lint for every supported OS: `task cross-lint:all`. It runs `golangci-lint --fix`, so review `git diff` afterwards.
- Apply lint and formatting fixes: `task lint-fix`. It can rewrite unrelated files; never use it as a check.
- Vulnerabilities: `task vulncheck`
- Nil analysis: `task nilcheck`
- Deadlock detection: `task deadlock`
- Fuzz targets: `task fuzz`
- Benchmarks: `task bench`, `task bench-db`, `task bench-compare`
- Sim binary: `task sim:build`, `task sim:run`

Run targeted tests while iterating, then `task test` and `task lint` before finishing a Go change.

Do not run ad hoc `GOOS=...` builds, tests or lints. Core has CGO and platform-native dependencies; `task cross-lint:all` covers the other platforms, and CI runs them natively.

## Running Core

Never run the Linux `zaparoo` binary on a developer machine. It finds the real emulators, Steam and browser, indexes real ROM folders, opens real readers, plays sounds, and shares port 7497 and the XDG directories with the user's own Core.

Use the sim instead: the real service, API, reader pipeline, ZapScript, launch lifecycle and databases under one throwaway root, with simulated readers, `sleep` launchers, silent audio and offline network fakes.

- Quick start: `.agents/skills/core-sim/SKILL.md`. Full reference: `docs/sim.md`.
- What each layer (sim, MiSTer VM, hardware) can and cannot test: `TESTING.md`.
- MiSTer behavior: `.agents/skills/mister-vm/SKILL.md`.

## Testing

- Read `TESTING.md` and `pkg/testing/README.md` before adding substantial test infrastructure.
- Add or update tests for every behavior change and bug fix. Prefer a regression test that fails before the fix.
- Put behavior regression tests in `pkg/platforms/sim/testdata/scripts/*.txtar` (run by `task test`), or in Go with `harness.New(t, &harness.Options{...})`.
- Mock hardware, network, process and platform boundaries. Tests must not need a physical reader or target device.
- Reuse the mocks, fixtures and database helpers under `pkg/testing/` instead of building parallel ones.
- Use afero for filesystem code and fake clocks for time-dependent code. Do not sleep where deterministic synchronization is possible.
- Do not weaken an assertion to make a test pass.

### Paths in tests

Core stores media paths with forward slashes on every OS. PR CI runs the full suite on Linux only; the full Windows suite runs only after merge to `main`, so a path mistake shows up there first.

- A media path that never touches disk: write the literal forward-slash string Core stores, such as `"roms/PSX/Game.chd"`.
- A path that touches disk: build it with `filepath.Join`, then pass it once through `pathutil.CanonicalMediaPath` (`pkg/helpers/pathutil`) before comparing it with a stored path or using it in a lookup.
- Production path logic must never hardcode a host-OS separator.

## Code rules

- Use `syncutil.Mutex` and `syncutil.RWMutex` from `pkg/helpers/syncutil`, never the standard library types. Deadlock instrumentation depends on them.
- Log with zerolog. No standard `log` package, no direct prints.
- Keep UserDB and MediaDB access behind their interfaces.
- Treat token contents, NDEF records, barcodes, MQTT messages, ZapScript, API requests, archives and update metadata as untrusted input.
- Regenerate generated files such as `pkg/database/mediadb/stat1_seed_data.go` with their documented generator; never hand-edit them.
- `mister/` is a nested Go module for standalone MiSTer tools. The root `go.mod` builds it from the tree through a `replace`; never re-pin it to a published version. `./...` does not reach it, so its tests and lint run separately, and it releases by its own `mister/vX.Y.Z` tag. See `mister/README.md`.

## Boundaries

### Always

- Preserve backward compatibility for JSON-RPC methods, notifications, API models, configuration files, mappings, launchers and persisted database data.
- Add a new migration for schema changes. Applied migrations in `pkg/database/{userdb,mediadb}/migrations/` are immutable.
- Give configuration schema changes migration and compatibility handling for existing installs.
- Report which checks ran and which were skipped. Do not claim completion while a relevant check fails or the work is partial.

### Ask first

- Breaking a public interface or persisted format.
- Adding a dependency, platform, reader, launcher or public API surface.
- Creating a documentation file. Update existing docs only to keep them accurate; put summaries and verification results in the response.
- Committing, pushing, rewriting history, updating benchmark baselines or changing release state.

### Never

- Publish OTA metadata, alter a rollout, rotate trust keys, tag a release or run deployment workflows without explicit approval.
- Read, print or commit `.env`, API keys, private signing material, local databases, logs with user data or generated runtime state.

## High-risk areas

- Authentication and authorization: `pkg/api/middleware/`, `pkg/config/auth.go`, client pairing, encryption, profile permissions.
- Parsing and execution: `pkg/readers/shared/nfctag/`, `pkg/zapscript/`, command execution, archive extraction, launcher handling.
- Durable state: database migrations, backups, config migration, profile data swapping.
- Updates: `pkg/service/updater/`, `pkg/service/updater/otameta/`, `.github/workflows/ota-*.yml`, `.github/actions/ota-metadata/`, `scripts/generate-update-manifest/`.

For updater, signing, promotion, rollout, withdrawal or key changes, read `docs/ota-runbook.md` first. Preserve signature verification, monotonic manifest generations, asset digests, rollback protection and fail-closed installs.

## Extra checks by change type

- Concurrency, lifecycle or locks: `task deadlock` and the relevant race tests.
- Untrusted parsers or protocols: the relevant fuzz target, or `task fuzz` for wide changes.
- Dependencies or security-sensitive code: `task vulncheck`.
- Performance: read `docs/optimization-targets.md`, benchmark with `b.Loop()` and allocation reporting, and include `task bench-compare` output.
- GitHub workflows: `actionlint`.
- Cross-platform code: `task cross-lint:all` before pushing.

## Pull requests

- PR titles use Conventional Commit types, enforced by `.github/workflows/pr-title.yml`.
- PR descriptions have no test-plan section.

## Skills

Shared agent skills live in `.agents/skills/`. Claude Code reads only `.claude/skills/`, which is gitignored; link it locally with `mkdir -p .claude && ln -s ../.agents/skills .claude/skills`, or link each skill directory if `.claude/skills/` already holds local skills.
