# AGENTS.md - Zaparoo Core

A README for AI coding agents working on Zaparoo Core.

## Project Overview

Zaparoo Core is a hardware-agnostic game launcher that bridges physical tokens (NFC tags, barcodes, RFID) with digital media across 12 gaming platforms. Built in Go, it provides a unified API for launching games on MiSTer, Batocera, Bazzite, ChimeraOS, LibreELEC, Linux, macOS, RetroPie, Recalbox, SteamOS, Windows, and MiSTeX through token scanning. The system uses WebSocket/JSON-RPC for real-time communication, SQLite for dual-database storage, supports 10 reader types, includes cross-platform audio feedback via beep, and features a custom ZapScript language for automation.

**Tech Stack**: Go 1.24.11+, SQLite (dual-DB: UserDB + MediaDB), WebSocket/HTTP with JSON-RPC 2.0, beep/v2 (audio), testify/mock, sqlmock, afero

**Testing Standards**: Comprehensive test coverage required for all new code - we have extensive testing infrastructure with mocks, fixtures, and examples in `pkg/testing/`

## Development Guidelines

### Do

- **Write tests for all new code** - comprehensive coverage required
- **Use `task lint-fix`** to resolve all linting and formatting issues
- **Keep diffs small and focused** - one concern per change
- **Use file-scoped commands** (tests, formatting) for faster feedback
- **Reference existing patterns** before writing new code - consistency matters
- **Use zerolog for all logging** - standard `log` and `fmt.Println` are not allowed
- **Use filepath.Join** for all path construction - ensures cross-platform compatibility
- **Handle all errors explicitly** - use golangci-lint's error handling checks
- **Default to small components** - prefer focused modules over monolithic files

### Don't

- ❌ Use standard `log` or `fmt.Println` (use zerolog instead)
- ❌ Run file-level golangci-lint (use `task lint-fix` or package-level commands)
- ❌ Add new dependencies without discussion
- ❌ Run full test suite unless needed (prefer file-scoped: `go test ./pkg/specific/`)
- ❌ Skip writing tests for new features or bug fixes
- ❌ Make large, unfocused diffs - keep changes small and targeted

### Code Quality

- **Use Go 1.24.11+** with Go modules enabled
- **Write tests for all new features and bug fixes** (see TESTING.md) - high test coverage is required
- **Use table-driven tests** with subtests for multiple scenarios
- **Handle all errors explicitly** - use golangci-lint's error handling checks
- **Use explicit returns** in functions longer than 5 lines (avoid naked returns)
- **Keep functions small** and focused on single responsibility
- **Keep diffs small and focused** - one concern per change
- **Reference existing code patterns** before writing new code - consistency matters

### Logging & Output

- **Use zerolog for all logging** - standard `log` and `fmt.Println` are not allowed (enforced by depguard)
- **Log at appropriate levels** - debug, info, warn, error

### Testing

- **Mock at interface boundaries** - all hardware interactions must be mocked
- **Use existing mocks/fixtures** from `pkg/testing/` instead of creating new ones
- **Write sqlmock tests** for all direct SQL operations
- **Use `t.Parallel()`** in tests when safe to run concurrently
- **Run file-scoped tests** for faster feedback (see Commands section below)

### File Paths & Filesystem

- **Use filepath.Join** for all file path construction - ensures cross-platform compatibility
- **Use afero** for filesystem operations in testable code
- **Use absolute imports** with full module path `github.com/ZaparooProject/zaparoo-core/v2`

### Dependencies & State

- **Discuss new dependencies** before adding them - keep the dependency tree lean
- **Protect global state** with sync.RWMutex or atomic operations
- **Use context** for cancellation and timeouts in long-running operations

### Compatibility & Migration

- **Maintain backward compatibility** in config schema - use migrations for breaking changes
- **Plan migrations** before modifying database schemas (SchemaVersion)

### Code Hygiene

- **Follow GPL-3.0-or-later license** header format on all new files
- **Run `task lint-fix`** before committing to auto-fix linting issues
- **Default to small components** - prefer focused modules over monolithic files

## Commands

### File-scoped checks (preferred for speed)

```bash
# Test a specific file or package
go test ./pkg/service/tokens/
go test -run TestSpecificFunction ./pkg/api/

# Test with race detection
go test -race ./pkg/service/tokens/

# Lint and format - ALWAYS prefer task commands
task lint-fix                          # PREFERRED: Full project lint with auto-fixes

# Package-level linting (only when file-scoped is needed)
golangci-lint run --fix pkg/service/   # Package level OK
golangci-lint run pkg/database/        # Package level OK

# ❌ NEVER use file-level golangci-lint - not well supported
# golangci-lint run pkg/config/config.go  # DON'T DO THIS

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
# Full test suite with race detection
task test

# Full lint with auto-fixes
task lint-fix

# Security vulnerability scan
task vulncheck

# Nil-pointer analysis
task nilcheck

# Platform-specific build examples
task linux:build-amd64
task windows:build-amd64
task mister:build-arm
task batocera:build-arm64
```

## When Stuck

**Don't guess - ask for help or gather more information first.**

- **Ask clarifying questions** - Get requirements clear before coding
- **Propose a plan first** - Outline approach, then implement
- **Use extended thinking** - For complex problems, think through the solution systematically
- **Reference existing patterns** - Check similar code in the codebase for consistency
- **Consult TESTING.md** - For comprehensive testing guidance
- **Check pkg/testing/examples/** - For real-world test patterns
- **Look at git history** - `git log -p filename` shows how code evolved
- **Use subagents** - Delegate exploration and verification tasks when appropriate
- **Keep scope focused** - Small, well-defined changes are easier to review and debug

**Remember**: It's better to ask than to make incorrect assumptions. The project values correctness over speed.

## Project Structure

Key entry points and frequently accessed directories:

```
zaparoo-core/
├── cmd/{platform}/      # Platform-specific entry points (12 platforms)
├── pkg/
│   ├── api/             # WebSocket/HTTP JSON-RPC server
│   │   ├── methods/     # RPC method handlers
│   │   └── models/      # API data models
│   ├── audio/           # Cross-platform audio playback (beep-based)
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

## Audio System

Zaparoo uses **beep** (github.com/gopxl/beep/v2) for cross-platform audio playback of feedback sounds. Beep is a high-level audio library that wraps oto/v3 for hardware output.

### Overview

- **Location**: `pkg/audio/audio.go`
- **Supported platforms**: All 12 platforms (Linux, Windows, macOS, MiSTer, MiSTeX, Batocera, etc.)
- **Audio format**: WAV files (beep handles various formats and sample rates automatically)
- **Playback**: Fire-and-forget asynchronous playback with automatic cleanup
- **Initialization**: Speaker initialized once at application startup with 44100 Hz sample rate

## Good Examples to Follow

**Copy these patterns for new code:**

- **Tests**: `pkg/testing/examples/service_token_processing_test.go` - Complete test pattern with mocks
- **Tests**: `pkg/testing/examples/mock_usage_example_test.go` - How to use mocks and fixtures
- **API**: `pkg/api/methods/` - JSON-RPC method handler pattern
- **Audio**: `pkg/audio/audio.go` - Cross-platform audio playback pattern
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

## Test File Organization

Follow Go community best practices: **big files aren't necessarily bad**. The Go standard library has test files with 6,000+ lines, and Kubernetes has test files with 26,000+ lines. Organize tests by **what makes sense for the code**, not arbitrary file size limits.

### When to Create Separate Test Files

1. **Testing a distinct feature** - `batch_inserter_test.go`, `slug_cache_test.go`
2. **Integration vs unit tests** - Use `_integration_test.go` suffix or combine in same file for smaller suites
3. **Distinct error scenarios** - `concurrent_operations_test.go`, `transaction_concurrency_test.go`
4. **Regression tests** - Document specific bugs: `column_mismatch_regression_test.go`

### File Size Guidelines

- **Small (<200 lines)**: Consider merging with related tests
- **Medium (200-1,000 lines)**: Ideal range
- **Large (1,000-2,500 lines)**: Perfectly acceptable if cohesive
- **Very large (2,500+ lines)**: OK if it makes sense (see Go stdlib)

### Key Principle

**Focus on cohesion over file size** - Keep related tests together. Split only when there's a clear feature/concern separation. The goal is **easy to find, easy to understand** tests - not perfect file sizes.

**See TESTING.md** for detailed examples, naming conventions, and package organization patterns.

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

Zaparoo uses **Conventional Commits** format for automated semantic versioning:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Types** (determines version bump):
- `feat:` - New feature (triggers **minor** version bump, e.g., 1.0.0 → 1.1.0)
- `fix:` - Bug fix (triggers **patch** version bump, e.g., 1.0.0 → 1.0.1)
- `docs:` - Documentation only changes (no version bump)
- `style:` - Code style/formatting changes (no version bump)
- `refactor:` - Code refactoring without behavior change (no version bump)
- `perf:` - Performance improvement (triggers **patch** bump)
- `test:` - Adding or updating tests (no version bump)
- `build:` - Build system or dependency changes (no version bump)
- `ci:` - CI/CD configuration changes (no version bump)
- `chore:` - Other changes that don't modify src or test files (no version bump)

**Breaking changes** (triggers **major** version bump, e.g., 1.0.0 → 2.0.0):
- Add `!` after type/scope: `feat!:` or `fix(api)!:`
- Or add `BREAKING CHANGE:` in footer

**Examples**:

```bash
# Good examples:
git commit -m "feat: add support for new NFC reader type"
git commit -m "fix: resolve token processing race condition"
git commit -m "docs: update API endpoint documentation"
git commit -m "refactor(database): improve batch inserter performance"
git commit -m "feat(api)!: change websocket message format" # Breaking change
git commit -m "fix: correct slug cache invalidation

BREAKING CHANGE: slug cache now clears on all media updates"

# Avoid:
git commit -m "Fixed bug"              # Missing type, too vague
git commit -m "feat: Add feature"       # Not descriptive enough
git commit -m "add reader support"      # Missing type prefix
git commit -m "feat:add reader"         # Missing space after colon
```

**Scopes** (optional but recommended):
- `api`, `database`, `config`, `reader`, `platform`, `zapscript`, etc.
- Use package name or feature area

**Tips**:
- Use lowercase for description (after colon)
- Use imperative mood ("add" not "added", "fix" not "fixed")
- Keep description under 72 characters
- Reference issues in footer: `Fixes #123` or `Closes #456`

Look at recent commits with `git log --oneline -20` to match the style.

### Before committing

**ALWAYS run these commands in order:**

```bash
# 1. Fix all linting and formatting issues (REQUIRED)
task lint-fix

# 2. Run all tests with race detection (REQUIRED)
task test

# 3. Check for vulnerabilities (for security-sensitive changes)
task vulncheck
```

**Note**: `task lint-fix` handles all linting, formatting, and license header checks automatically. You should not need to run golangci-lint manually.

### Commit checklist

- [ ] Tests pass (`task test`)
- [ ] Linting passes (`task lint-fix`)
- [ ] License headers on new files
- [ ] Commit message follows Conventional Commits format
- [ ] Commit type correctly indicates change (feat/fix/docs/etc)
- [ ] Breaking changes marked with `!` or `BREAKING CHANGE:` footer
- [ ] Diff is small and focused on one concern
- [ ] No commented-out code or debug prints
- [ ] Documentation updated if needed

## Safety & Permissions

### Allowed without asking:

- Read any files in the repository
- Run file-scoped tests: `go test ./pkg/specific/`
- Run `task lint-fix` to fix linting and formatting issues
- Run package-level linting: `golangci-lint run pkg/specific/`
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
- **Plan migrations before schema changes** - maintain backward compatibility

### Platform Detection

Each platform has its own entry point in `cmd/{platform}/` with platform-specific configs.

### Reader Auto-Detection

10 supported reader types auto-detect by default:

- acr122pcsc, externaldrive, file, libnfc, mqtt, opticaldrive, pn532, pn532uart, simpleserial, tty2oled

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
task windows:build-amd64

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
