# AGENTS.md - Zaparoo Core

Hardware-agnostic game launcher bridging physical tokens (NFC, barcodes, RFID) with digital media across 12 gaming platforms. Built in Go with WebSocket/JSON-RPC API, dual SQLite databases, and a custom ZapScript command language.

**Tech Stack**: Go 1.25.7+, SQLite (UserDB + MediaDB), WebSocket/HTTP JSON-RPC 2.0, malgo+beep/v2 (audio), testify/mock, sqlmock, afero

For architecture details, API reference, and key concepts: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Safety & Permissions

**Allowed without asking**: Read files, run file-scoped tests (`go test ./pkg/specific/`), run `task lint-fix`, package-level linting, `gofumpt`, view git history.

**Ask before**: Installing dependencies, `git push`/`git commit`, deleting files, changing DB schema/migrations, modifying config schema, adding platform support, breaking API changes.

## Rules

- Write tests for all new code — see [TESTING.md](TESTING.md) and `pkg/testing/README.md`
- Use `task lint-fix` to resolve all linting and formatting issues
- Keep diffs small and focused — one concern per change
- Use file-scoped commands for faster feedback over full-suite runs
- Reference existing patterns before writing new code
- Use `filepath.Join` for path construction — cross-platform compatibility
- Use afero for filesystem operations in testable code
- NEVER use `sync.Mutex`/`sync.RWMutex` — use `syncutil.Mutex`/`syncutil.RWMutex` (forbidigo linter enforces this)
- NEVER use standard `log` or `fmt.Println` — use zerolog (depguard enforces this)
- NEVER run builds, lints, or tests for another OS (e.g., `GOOS=windows`) — CGO dependencies. Rely on CI
- NEVER amend commits — always create new commits
- NEVER add dependencies without discussion

## Testing

Full guide: [TESTING.md](TESTING.md) | Quick reference: `pkg/testing/README.md`

The goal is useful tests, not coverage metrics. Mock at interface boundaries — all hardware interactions must be mocked. Use existing mocks/fixtures from `pkg/testing/` instead of creating new ones.

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

## Commands

```bash
# File-scoped (preferred for speed)
go test ./pkg/service/tokens/             # Test a package
go test -run TestSpecificFunc ./pkg/api/  # Test by name
go test -race ./pkg/service/tokens/       # Race detection
gofumpt -w pkg/config/config.go           # Format a file
golangci-lint run --fix pkg/service/      # Package-level lint

# Project-wide
task test              # Full test suite with race detection
task lint-fix          # Full lint with auto-fixes
task build             # Build binary
task fuzz              # Run fuzz tests
task vulncheck         # Security vulnerability scan
task nilcheck          # Nil-pointer analysis
task deadlock          # Detect lock ordering violations

# DON'T use file-level golangci-lint (not well supported)
# golangci-lint run pkg/config/config.go  # BAD
```

## Project Structure

```
zaparoo-core/
├── cmd/{platform}/        # Platform entry points (12 platforms)
├── pkg/
│   ├── api/               # WebSocket/HTTP JSON-RPC server
│   │   ├── methods/       # RPC method handlers
│   │   └── models/        # API data models
│   ├── assets/            # Embedded static files (App web build)
│   ├── audio/             # Cross-platform audio playback
│   ├── cli/               # CLI interface
│   ├── config/            # Configuration management (TOML)
│   ├── database/          # Dual database system
│   │   ├── userdb/        # User mappings, history, playlists
│   │   ├── mediadb/       # Indexed media content
│   │   └── mediascanner/  # Media indexing engine
│   ├── groovyproxy/       # Groovy scripting proxy
│   ├── helpers/           # Utilities (syncutil, etc.)
│   ├── platforms/         # 12 platform implementations
│   ├── readers/           # 11 reader type drivers
│   ├── service/           # Core business logic
│   │   ├── broker/        # Event brokering
│   │   ├── daemon/        # Background service management
│   │   ├── discovery/     # mDNS service discovery
│   │   ├── inbox/         # Message inbox
│   │   ├── playlists/     # Playlist management
│   │   ├── playtime/      # Play time tracking
│   │   ├── publishers/    # Event publishing
│   │   ├── state/         # Application state
│   │   └── tokens/        # Token processing
│   ├── testing/           # Testing infrastructure
│   │   ├── README.md      # Quick reference
│   │   ├── mocks/         # Pre-built mocks
│   │   ├── helpers/       # Testing utilities (DB, FS, API)
│   │   ├── fixtures/      # Sample test data
│   │   └── examples/      # Example test patterns
│   ├── ui/                # UI components (systray, TUI)
│   └── zapscript/         # ZapScript language + advargs parser
├── docs/                  # Architecture, API docs, plans
├── scripts/               # Build and platform scripts
├── TESTING.md             # Testing guide
└── Taskfile.dist.yml      # Build and development tasks
```

## Reference Files

Copy these patterns for new code:

- **Tests**: `pkg/testing/examples/` — 7 example files covering services, mocks, API, DB, filesystem, state, and ZapScript patterns
- **API**: `pkg/api/methods/` — JSON-RPC method handler pattern
- **Config**: `pkg/config/config.go` — Thread-safe config with RWMutex
- **Database**: `pkg/database/userdb/` and `pkg/database/mediadb/` — Database interface pattern
- **Platform**: `pkg/platforms/linux/platform.go` — Platform implementation pattern
- **Service**: `pkg/service/tokens/tokens.go` — Service layer pattern

## Git & Commits

Zaparoo uses **Conventional Commits**: `<type>[scope]: <description>`

Types: `feat` (minor bump), `fix` (patch), `docs`, `refactor`, `style`, `perf`, `test`, `build`, `ci`, `chore`. Breaking changes: add `!` after type (`feat!:`) or `BREAKING CHANGE:` footer.

```bash
# Good:
git commit -m "feat: add support for new NFC reader type"
git commit -m "fix(api): resolve websocket reconnection issue"
git commit -m "feat(database)!: change migration format"

# Bad:
git commit -m "Fixed bug"           # Missing type
git commit -m "add reader support"  # Missing type prefix
```

Before committing: run `task lint-fix` then `task test`.

## When Stuck

Don't guess — ask for help or gather more information first.

- **Ask clarifying questions** before coding
- **Propose a plan first** — outline approach, then implement
- **Reference existing patterns** — check similar code for consistency
- **Look at git history** — `git log -p filename` shows how code evolved
