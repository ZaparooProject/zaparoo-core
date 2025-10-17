# AGENTS.md - Zaparoo Core

A README for AI coding agents working on Zaparoo Core.

## Project Overview

Zaparoo Core is a hardware-agnostic game launcher that bridges physical tokens (NFC tags, barcodes, RFID) with digital media across 12 gaming platforms. Built in Go, it provides a unified API for launching games on MiSTer, Batocera, Bazzite, ChimeraOS, LibreELEC, Linux, macOS, RetroPie, Recalbox, SteamOS, Windows, and MiSTeX through token scanning. The system uses WebSocket/JSON-RPC for real-time communication, SQLite for dual-database storage, supports 8 reader types, and includes a custom ZapScript language for automation.

**Tech Stack**: Go 1.24.5+, SQLite (dual-DB: UserDB + MediaDB), WebSocket/HTTP with JSON-RPC 2.0, testify/mock, sqlmock, afero

**Testing Standards**: Comprehensive test coverage required for all new code - we have extensive testing infrastructure with mocks, fixtures, and examples in `pkg/testing/`

## Do

- **Use Go 1.24.5+** with Go modules enabled
- **Write tests for all new features and bug fixes** (see TESTING.md) - high test coverage is required
- **Use table-driven tests** with subtests for multiple scenarios
- **Mock at interface boundaries** - all hardware interactions must be mocked
- **Use zerolog** for all logging (never use standard `log` package)
- **Use afero** for filesystem operations in testable code
- **Keep functions small** and focused on single responsibility
- **Use file-scoped commands** for faster feedback (see Commands section below)
- **Run `task lint-fix`** before committing to auto-fix linting issues
- **Use `t.Parallel()`** in tests when safe to run concurrently
- **Check errors** explicitly - use golangci-lint's error handling checks
- **Use existing mocks/fixtures** from `pkg/testing/` instead of creating new ones
- **Follow the GPL-3.0-or-later license** header format on all new files
- **Keep diffs small and focused** - avoid large refactors unless explicitly needed
- **Use absolute imports** with full module path `github.com/ZaparooProject/zaparoo-core/v2`
- **Default to small components** - prefer focused modules over monolithic files
- **Use context** for cancellation and timeouts in long-running operations

## Don't

- **Do not use the standard `log` package** - use zerolog instead (enforced by depguard)
- **Do not write tests that depend on hardware** - use mocks from `pkg/testing/mocks/`
- **Do not make filesystem assumptions** - use afero for testability
- **Do not skip error handling** - all errors must be handled or explicitly ignored
- **Do not hard-code file paths** - use filepath.Join and handle cross-platform paths
- **Do not add dependencies without discussion** - keep the dependency tree lean
- **Do not use `fmt.Println` for logging** - use zerolog
- **Do not break backward compatibility** in the config schema without migration
- **Do not commit files without GPL license headers** - use the template from .golangci.yml
- **Do not use naked returns** in functions longer than 5 lines
- **Do not create global state** without using sync.RWMutex or atomic
- **Do not use direct SQL** without sqlmock tests
- **Do not guess at conventions** - look at existing code patterns first

## Commands

### File-scoped checks (preferred for speed)

```bash
# Test a specific file or package
go test ./pkg/service/tokens/
go test -run TestSpecificFunction ./pkg/api/

# Test with race detection
go test -race ./pkg/service/tokens/

# Lint specific files (much faster than full project lint)
golangci-lint run --fix pkg/config/config.go
golangci-lint run pkg/service/

# Run single test by name
go test -run TestTokenProcessing ./pkg/service/
task test -- -run TestTokenProcessing ./pkg/service/

# Run tests with verbose output
task test -- -v ./pkg/api/

# Format specific files
gofumpt -w pkg/config/config.go

# Security scan specific package
govulncheck ./pkg/api/...
```

### Project-wide commands (slower, use sparingly)

```bash
# Full test suite with race detection
task test

# Full lint with auto-fixes
task lint-fix

# Security vulnerability scan
task vulncheck

# Nil-pointer analysis
task nilcheck

# Clean build artifacts
task clean

# Download and view logs from running Zaparoo instance
task get-logs

# Build for current platform
task build

# Platform-specific builds
GOOS=linux GOARCH=amd64 task build
GOOS=windows GOARCH=amd64 task build
GOARCH=arm task build  # For MiSTer
```

**Important**: Always prefer file-scoped commands during development. Only run full project commands when explicitly requested or before final commit.

## Project Structure

Key entry points and frequently accessed directories:

```
zaparoo-core/
├── cmd/{platform}/      # Platform-specific entry points (12 platforms)
├── pkg/
│   ├── api/             # WebSocket/HTTP JSON-RPC server
│   │   ├── methods/     # RPC method handlers
│   │   └── models/      # API data models
│   ├── config/          # Configuration management (TOML-based)
│   ├── database/        # Dual database system
│   │   ├── userdb/      # User mappings, history, playlists
│   │   └── mediadb/     # Indexed media content
│   ├── platforms/       # 12 platform implementations
│   ├── readers/         # 8 reader type drivers
│   ├── service/         # Core business logic
│   │   ├── tokens/      # Token processing
│   │   └── playlists/   # Playlist management
│   ├── testing/         # Testing infrastructure ⭐
│   │   ├── README.md    # Quick reference for all testing utilities
│   │   ├── mocks/       # Pre-built mocks for all major interfaces
│   │   ├── helpers/     # Testing utilities (DB, FS, API)
│   │   ├── fixtures/    # Sample test data
│   │   └── examples/    # Example test patterns
│   └── zapscript/       # Custom scripting language
└── Taskfile.dist.yml    # Build and development tasks
```

## Good Examples to Follow

**Copy these patterns for new code:**

- **Tests**: `pkg/testing/examples/service_token_processing_test.go` - Complete test pattern with mocks
- **Tests**: `pkg/testing/examples/mock_usage_example_test.go` - How to use mocks and fixtures
- **API**: `pkg/api/methods/` - JSON-RPC method handler pattern
- **Config**: `pkg/config/config.go` - Thread-safe config with RWMutex
- **Database**: `pkg/database/userdb/` and `pkg/database/mediadb/` - Database interface pattern
- **Platform**: `pkg/platforms/linux/platform.go` - Platform implementation pattern
- **Service**: `pkg/service/tokens/tokens.go` - Service layer pattern

**Reference for testing:**

- `pkg/testing/README.md` - Quick reference guide to all testing utilities
- `TESTING.md` - Comprehensive testing guide with best practices
- All example tests in `pkg/testing/examples/` - Real-world test patterns

## Testing Instructions

**Read [TESTING.md](TESTING.md) first** - it contains comprehensive testing documentation.

### Quick testing patterns:

```go
import (
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
    "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
)

// All major interfaces have mocks ready
mockPlatform := mocks.NewMockPlatform()
mockReader := mocks.NewMockReader()
mockUserDB := helpers.NewMockUserDBI()
mockMediaDB := helpers.NewMockMediaDBI()
```

### Testing rules:

1. **Write tests for all new features and bug fixes** - comprehensive test coverage is required
2. **Use existing mocks and fixtures** from `pkg/testing/`
3. **No hardware dependencies** - all hardware interactions are mocked
4. **Tests must be fast** - aim for <5 seconds for full suite
5. **Use table-driven tests** for multiple scenarios
6. **Always use `t.Parallel()`** unless tests have shared state
7. **Verify mock expectations** with `AssertExpectations(t)`

### Running tests:

```bash
# Run tests via task command
task test                           # All tests with race detection
task test -- -v                     # Verbose output
task test -- -run TestName          # Specific test
task test -- ./pkg/service/...      # Specific package
```

## Code Style & Standards

Following golangci-lint configuration in `.golangci.yml`:

- **Line length**: 120 characters max (revive rule)
- **Function results**: Max 3 return values (revive rule)
- **Error handling**: All errors must be checked (errcheck, wrapcheck)
- **Imports**: Grouped and sorted with gci formatter
- **Formatting**: Use gofumpt (stricter than gofmt)
- **JSON tags**: camelCase (enforced by tagliatelle)
- **TOML tags**: snake_case (enforced by tagliatelle)
- **Nil checks**: Comprehensive (nilnil, nilerr rules)
- **SQL**: Close all rows/statements (sqlclosecheck, rowserrcheck)
- **Concurrency**: Proper context usage (noctx rule)
- **No naked returns** in long functions (nakedret rule)

## Git & Commit Guidelines

### Commit message format

Zaparoo uses **lowercase, imperative, concise** commit messages:

```bash
# Good examples:
git commit -m "fix token processing race condition"
git commit -m "add support for new reader type"
git commit -m "update docs for api endpoints"
git commit -m "refactor platform launcher interface"

# Avoid:
git commit -m "Fixed bug"  # Too vague
git commit -m "feat: Add feature"  # No conventional commits format
git commit -m "Updated things"  # Not descriptive
```

Look at recent commits with `git log --oneline -20` to match the style.

### Before committing

```bash
# 1. Run lint-fix
task lint-fix

# 2. Run tests
task test

# 3. Check for vulnerabilities (for security-sensitive changes)
task vulncheck

# 4. Verify license headers on new files
golangci-lint run
```

### Commit checklist

- [ ] Tests pass (`task test`)
- [ ] Linting passes (`task lint-fix`)
- [ ] License headers on new files
- [ ] Commit message is lowercase, imperative, descriptive
- [ ] Diff is small and focused on one concern
- [ ] No commented-out code or debug prints
- [ ] Documentation updated if needed

## Safety & Permissions

### Allowed without asking:

- Read any files in the repository
- Run file-scoped tests: `go test ./pkg/specific/`
- Run file-scoped linting: `golangci-lint run pkg/specific/`
- Format files: `gofumpt -w file.go`
- View git history: `git log`, `git diff`
- Run vulnerability checks: `govulncheck ./...`

### Ask before:

- Installing new Go dependencies
- Running `git push` or `git commit`
- Deleting files or directories
- Running full `task test` (it's slow - prefer file-scoped)
- Running `task build` (slow - only when needed)
- Changing the database schema or migrations
- Modifying configuration schema (SchemaVersion)
- Adding new platform support
- Changing API contract (breaking changes)

## When Stuck

- **Ask clarifying questions** - don't make assumptions about requirements
- **Propose a plan first** - outline approach before implementing
- **Reference existing patterns** - check similar code in the codebase
- **Consult TESTING.md** - for testing questions
- **Check pkg/testing/examples/** - for testing patterns
- **Look at git history** - `git log -p filename` shows evolution
- **Don't make large speculative changes** - keep scope focused

## API & Architecture Notes

### API Endpoints

- WebSocket: `ws://localhost:7497/api/v0.1`
- HTTP: `http://localhost:7497/api/v0.1`
- Default port: 7497 (configurable via config.toml)
- Protocol: JSON-RPC 2.0

### Database Architecture

- **Dual-database design**: UserDB (mappings/history) + MediaDB (indexed content)
- **Migrations**: Managed by goose in `pkg/database/{userdb,mediadb}/migrations/`
- **Auto-applied** on startup
- **Thread-safe**: Use database interface methods, not direct SQL

### Configuration

- Location: `~/.config/zaparoo/config.toml`
- Format: TOML with schema versioning
- Thread-safe: config.Instance uses sync.RWMutex
- **Don't modify schema** without migration plan

### Platform Detection

Each platform has its own entry point in `cmd/{platform}/` with platform-specific configs.

### Reader Auto-Detection

8 supported reader types auto-detect by default:

- acr122pcsc, file, libnfc, opticaldrive, pn532, pn532uart, simpleserial, tty2oled

## Additional Resources

- **Testing**: See [TESTING.md](TESTING.md) and `pkg/testing/examples/`
- **Testing Quick Reference**: See `pkg/testing/README.md`
- **API Documentation**: See `docs/api/`
- **ZapScript**: See `pkg/zapscript/`
- **License**: GPL-3.0-or-later (see LICENSE file)

## Platform-Specific Build Notes

```bash
# Linux
task linux:build-amd64

# Windows
GOOS=windows GOARCH=amd64 task build

# MiSTer (ARM)
task mister:arm

# See Taskfile.dist.yml for all platform builds
```

## Remember

1. **Write tests** - comprehensive test coverage is required for all new code
2. **Small diffs** - focused changes are easier to review
3. **File-scoped commands** - faster feedback loop
4. **Use existing patterns** - consistency matters
5. **Ask when uncertain** - better than wrong assumptions
