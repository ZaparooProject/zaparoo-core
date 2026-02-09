# AGENTS.md - Zaparoo Core

A README for AI coding agents working on Zaparoo Core.

## Project Overview

Zaparoo Core is a hardware-agnostic game launcher that bridges physical tokens (NFC tags, barcodes, RFID) with digital media across 12 gaming platforms. Built in Go, it provides a unified API for launching games on MiSTer, Batocera, Bazzite, ChimeraOS, LibreELEC, Linux, macOS, RetroPie, Recalbox, SteamOS, Windows, and MiSTeX through token scanning. The system uses WebSocket/JSON-RPC for real-time communication, SQLite for dual-database storage, supports 11 reader types, includes cross-platform audio feedback, and features a custom ZapScript language for automation.

**Tech Stack**: Go 1.25.7+, SQLite (dual-DB: UserDB + MediaDB), WebSocket/HTTP with JSON-RPC 2.0, malgo+beep/v2 (audio), testify/mock, sqlmock, afero

### Zaparoo Ecosystem

Zaparoo Core is the backend service in a larger ecosystem:

- **Zaparoo App** ([github.com/ZaparooProject/zaparoo-app](https://github.com/ZaparooProject/zaparoo-app)) - The primary user interface (iOS, Android, Web). Its web build is embedded into the Core binary at `pkg/assets/_app/dist/` and served at `/app/`. The App uses Core's JSON-RPC API for all communication. CI automatically downloads the latest App build during Core builds.
- **go-pn532** - NFC reader driver library used by Core's PN532 reader implementations
- **go-zapscript** - ZapScript language parser library used by Core's token processing

When working on the API, notifications, or media features, remember the App is the primary consumer of these interfaces.

### Key Concepts

- **Tokens**: Physical objects (NFC tags, barcodes, QR codes, optical discs) that carry or are mapped to ZapScript commands. Identified by UID, text content, or raw data.
- **ZapScript**: Command language stored on tokens. Format: `**command:arg1,arg2?key=value`, chained with `||`. Supports expressions (`[[variable]]`) and conditions (`?when=`). A bare path (no `**` prefix) auto-launches as media. See `pkg/zapscript/` and official docs.
- **Mappings**: Rules that override what a token does based on pattern matching (exact, partial/wildcard, regex) against UID, text, or data. Essential for read-only tokens like Amiibo. Stored in UserDB or as TOML files in `mappings/`.
- **Launchers**: Per-system programs that launch games/media. Each platform provides built-in launchers. Users can add custom launchers via TOML files in `launchers/`. See `pkg/platforms/`.
- **Systems**: 200+ supported game/computer/media systems (e.g., `SNES`, `Genesis`, `PSX`). IDs are case-insensitive with aliases and fallbacks. See official docs for the full list.
- **Readers**: Hardware or virtual devices that detect tokens. Support two scan modes: **tap** (default, token can be removed freely) and **hold** (token must stay on reader, removal stops media).

## Safety & Permissions

### Allowed without asking

- Read any files in the repository
- Run file-scoped tests: `go test ./pkg/specific/`
- Run `task lint-fix` to fix linting and formatting issues
- Run package-level linting: `golangci-lint run pkg/specific/`
- Format files: `gofumpt -w file.go`
- View git history: `git log`, `git diff`

### Ask before

- Installing new Go dependencies
- Running `git push` or `git commit`
- Deleting files or directories
- Changing the database schema or migrations
- Modifying configuration schema (SchemaVersion)
- Adding new platform support
- Changing API contract (breaking changes)

## Development Guidelines

### Do

- Write tests for all new code - comprehensive coverage required
- Use `task lint-fix` to resolve all linting and formatting issues (enforced by depguard, goheader, etc.)
- Keep diffs small and focused - one concern per change
- Use file-scoped commands (tests, formatting) for faster feedback
- Reference existing patterns before writing new code - consistency matters
- Use zerolog for all logging - `log` and `fmt.Println` are forbidden (depguard)
- Use `filepath.Join` for all path construction - cross-platform compatibility
- Handle all errors explicitly - use golangci-lint's error handling checks
- Use afero for filesystem operations in testable code
- Use absolute imports with full module path `github.com/ZaparooProject/zaparoo-core/v2`
- Add GPL-3.0-or-later license headers on all new files (goheader linter)

### Don't

- Use standard `log` or `fmt.Println` (use zerolog instead)
- Run file-level golangci-lint (use `task lint-fix` or package-level commands)
- Add new dependencies without discussion
- Run full test suite unless needed (prefer file-scoped: `go test ./pkg/specific/`)
- Skip writing tests for new features or bug fixes
- Write comments that restate what the code does - comments should explain *why*, not *what*
- Amend commits - always prefer to create new commits
- **Attempt to run builds, lints, or tests for another OS** (e.g., `GOOS=windows`) - CGO dependencies mean these only work on the current OS. Rely on CI for other platforms.

### Testing

Full guide: [TESTING.md](TESTING.md) | Quick reference: `pkg/testing/README.md`

The goal is useful tests, not coverage metrics. High coverage means nothing if tests don't catch bugs.

**What to test**: Business logic and algorithms, edge cases and error paths, integration points (DB queries, API responses), state transitions, regression scenarios.

**What NOT to test**: Library functions, simple getters/setters, third-party internals, implementation details, obvious code like `if err != nil { return err }`.

**How to test**:
- Mock at interface boundaries - all hardware interactions must be mocked
- Use existing mocks/fixtures from `pkg/testing/` instead of creating new ones
- Write sqlmock tests for all direct SQL operations
- Use `t.Parallel()` unless tests share state
- Use table-driven tests for multiple scenarios
- Verify mock expectations with `AssertExpectations(t)`
- Commit regression files - both rapid `.fail` files and fuzz corpus entries

**Mock setup pattern**:

```go
import (
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
)

mockPlatform := mocks.NewMockPlatform()
mockReader := mocks.NewMockReader()
mockUserDB := helpers.NewMockUserDBI()
mockMediaDB := helpers.NewMockMediaDBI()
```

See Commands section for test invocations.

### Dependencies & State

- Discuss new dependencies before adding them - keep the dependency tree lean
- Protect global state with syncutil.RWMutex or atomic operations
- **Use `syncutil.Mutex`/`syncutil.RWMutex`** instead of `sync.Mutex`/`sync.RWMutex` (forbidigo linter, deadlock detection)
- Use context for cancellation and timeouts in long-running operations

### Compatibility & Migration

- Maintain backward compatibility in config schema - use migrations for breaking changes
- Plan migrations before modifying database schemas (SchemaVersion)

## Commands

### File-scoped checks (preferred for speed)

```bash
# Test a specific file or package
go test ./pkg/service/tokens/
go test -run TestSpecificFunction ./pkg/api/

# Test with race detection
go test -race ./pkg/service/tokens/

# Lint and format - prefer task commands
task lint-fix                          # Full project lint with auto-fixes

# Package-level linting (only when file-scoped is needed)
golangci-lint run --fix pkg/service/   # Package level OK
golangci-lint run pkg/database/        # Package level OK

# DON'T use file-level golangci-lint - not well supported
# golangci-lint run pkg/config/config.go

# Run single test by name
go test -run TestTokenProcessing ./pkg/service/
task test -- -run TestTokenProcessing ./pkg/service/

# Run tests with verbose output
task test -- -v ./pkg/api/

# Format specific files (when you only need formatting)
gofumpt -w pkg/config/config.go

# Security scan specific package
govulncheck ./pkg/api/...
```

### Project-wide commands

```bash
task test           # Full test suite with race detection
task lint-fix       # Full lint with auto-fixes
task vulncheck      # Security vulnerability scan
task nilcheck       # Nil-pointer analysis
```

## Project Structure

Key entry points and frequently accessed directories:

```
zaparoo-core/
├── cmd/{platform}/      # Platform-specific entry points (12 platforms)
├── pkg/
│   ├── api/             # WebSocket/HTTP JSON-RPC server
│   │   ├── methods/     # RPC method handlers
│   │   └── models/      # API data models
│   ├── assets/          # Embedded static files (Zaparoo App web build)
│   ├── audio/           # Cross-platform audio playback (beep-based)
│   ├── config/          # Configuration management (TOML-based)
│   ├── database/        # Dual database system
│   │   ├── userdb/      # User mappings, history, playlists
│   │   ├── mediadb/     # Indexed media content
│   │   └── mediascanner/ # Media indexing engine
│   ├── platforms/       # 12 platform implementations
│   ├── readers/         # 11 reader type drivers
│   ├── service/         # Core business logic
│   │   ├── tokens/      # Token processing
│   │   └── playlists/   # Playlist management
│   ├── testing/         # Testing infrastructure
│   │   ├── README.md    # Quick reference for all testing utilities
│   │   ├── mocks/       # Pre-built mocks for all major interfaces
│   │   ├── helpers/     # Testing utilities (DB, FS, API)
│   │   ├── fixtures/    # Sample test data
│   │   └── examples/    # Example test patterns
│   └── zapscript/       # Custom scripting language
└── Taskfile.dist.yml    # Build and development tasks
```

### Reference Files

Copy these patterns for new code:

- **Tests**: `pkg/testing/examples/service_token_processing_test.go` - Complete test pattern with mocks
- **Tests**: `pkg/testing/examples/mock_usage_example_test.go` - How to use mocks and fixtures
- **API**: `pkg/api/methods/` - JSON-RPC method handler pattern
- **Audio**: `pkg/audio/audio.go` - Cross-platform audio playback pattern
- **Config**: `pkg/config/config.go` - Thread-safe config with RWMutex
- **Database**: `pkg/database/userdb/` and `pkg/database/mediadb/` - Database interface pattern
- **Platform**: `pkg/platforms/linux/platform.go` - Platform implementation pattern
- **Service**: `pkg/service/tokens/tokens.go` - Service layer pattern

## Git & Commit Guidelines

### Commit message format

Zaparoo uses **Conventional Commits** for automated semantic versioning:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Primary types**:
- `feat:` - New feature (minor bump, e.g., 1.0.0 → 1.1.0)
- `fix:` - Bug fix (patch bump, e.g., 1.0.0 → 1.0.1)
- `docs:` - Documentation only (no bump)
- `refactor:` - Code change without behavior change (no bump)

Also: `style`, `perf`, `test`, `build`, `ci`, `chore` (no bump except `perf` → patch)

**Breaking changes** (major bump): Add `!` after type/scope (`feat!:`) or `BREAKING CHANGE:` in footer.

**Examples**:

```bash
# Good:
git commit -m "feat: add support for new NFC reader type"
git commit -m "fix: resolve token processing race condition"
git commit -m "feat(api)!: change websocket message format"

# Bad:
git commit -m "Fixed bug"           # Missing type, too vague
git commit -m "add reader support"  # Missing type prefix
```

**Scopes** (optional): `api`, `database`, `config`, `reader`, `platform`, `zapscript`, etc.

**Tips**: lowercase description, imperative mood ("add" not "added"), under 72 characters. Reference issues in footer: `Fixes #123`. Match style with `git log --oneline -20`.

### Commit checklist

Before committing: run **`task lint-fix`** then **`task test`** (required).

- [ ] Tests pass and linting passes
- [ ] Commit message follows Conventional Commits format
- [ ] Breaking changes marked with `!` or `BREAKING CHANGE:` footer
- [ ] No commented-out code or debug prints

### Pull request descriptions

- Do NOT include test plans in PR descriptions - just summarize what the PR does
- Keep descriptions concise with bullet points
- Reference related issues if applicable

## API & Architecture Notes

### API Endpoints

- WebSocket: `ws://localhost:7497/api/v0.1`
- HTTP: `http://localhost:7497/api/v0.1`
- Default port: 7497 (configurable via config.toml)
- Protocol: JSON-RPC 2.0
- App UI: served at `/app/` (root `/` redirects here)
- Launch endpoint: `/l/{zapscript}` - simplified GET-based execution for QR codes
- Auth: API keys via `auth.toml`, anonymous access from localhost
- Discovery: mDNS (`_zaparoo._tcp`) for automatic network detection
- Notifications: Real-time events broadcast over WebSocket - readers connected/disconnected, tokens scanned/removed, media started/stopped, indexing progress, playtime warnings. See `docs/api/notifications.md`.
- API docs: See `docs/api/`

### Database Architecture

- **Dual-database design**: UserDB (mappings/history) + MediaDB (indexed content)
- **Migrations**: Managed by goose in `pkg/database/{userdb,mediadb}/migrations/`
- **Auto-applied** on startup
- **Thread-safe**: Use database interface methods, not direct SQL

### Configuration

- Location: `~/.config/zaparoo/config.toml`
- Format: TOML with schema versioning
- Thread-safe: config.Instance uses syncutil.RWMutex
- Plan migrations before schema changes - maintain backward compatibility

### Platform Detection

Each platform has its own entry point in `cmd/{platform}/` with platform-specific configs.

### Reader Auto-Detection

11 supported reader types auto-detect by default:

- acr122pcsc, externaldrive, file, libnfc, mqtt, opticaldrive, pn532, pn532uart, rs232barcode, simpleserial, tty2oled

## When Stuck

Don't guess - ask for help or gather more information first.

- **Ask clarifying questions** - get requirements clear before coding
- **Propose a plan first** - outline approach, then implement
- **Reference existing patterns** - check similar code in the codebase for consistency
- **Look at git history** - `git log -p filename` shows how code evolved

It's better to ask than to make incorrect assumptions. The project values correctness over speed.
