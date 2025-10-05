# Zaparoo Core - Developer Guide

## Project Overview

Zaparoo Core is a hardware-agnostic game launcher that bridges physical tokens (NFC tags, barcodes, RFID) with digital media across multiple gaming platforms. Built in Go, it provides a unified API for launching games on 12 platforms including MiSTer, Batocera, Bazzite, ChimeraOS, LibreELEC, Linux, macOS, RetroPie, Recalbox, SteamOS, Windows, and MiSTeX through token scanning. The system uses WebSocket/JSON-RPC for real-time communication, SQLite for dual-database storage, supports 8 reader types, and includes a custom ZapScript language for automation.

## Tech Stack

- **Language**: Go 1.24.5+
- **Module**: `github.com/ZaparooProject/zaparoo-core/v2`
- **Database**: SQLite (dual-DB: UserDB for mappings/config, MediaDB for indexed content)
- **API**: WebSocket (Melody) & HTTP with JSON-RPC 2.0 protocol
- **Testing**: testify/mock, sqlmock, afero (see [TESTING.md](TESTING.md))
- **Platforms**: Cross-platform support via platform-specific adapters
- **UI**: TUI and systray components
- **Scripting**: Custom ZapScript language implementation

## Directory Structure

```
zaparoo-core/
├── cmd/                    # Platform-specific entry points
│   ├── batocera/          # Batocera Linux build
│   ├── bazzite/           # Bazzite build
│   ├── chimeraos/         # ChimeraOS build
│   ├── libreelec/         # LibreELEC build
│   ├── linux/             # Generic Linux build
│   ├── mac/               # macOS build
│   ├── mister/            # MiSTer FPGA build
│   ├── mistex/            # MiSTeX build
│   ├── recalbox/          # Recalbox build
│   ├── retropie/          # RetroPie build
│   ├── steamos/           # SteamOS build
│   └── windows/           # Windows build
├── pkg/                    # Core packages
│   ├── api/               # WebSocket/HTTP API server
│   │   ├── methods/       # JSON-RPC method handlers
│   │   ├── models/        # API data models
│   │   ├── middleware/    # HTTP middleware
│   │   ├── client/        # API client
│   │   └── notifications/ # WebSocket notifications
│   ├── assets/            # Web app assets, sounds, system definitions
│   ├── cli/               # CLI implementation
│   ├── config/            # Configuration management
│   ├── database/          # Database interfaces and models
│   │   ├── userdb/        # User mappings and history
│   │   ├── mediadb/       # Media indexing
│   │   ├── mediascanner/  # Media scanning logic
│   │   ├── systemdefs/    # System definitions
│   │   └── tags/          # Tag management
│   ├── groovyproxy/       # Groovy proxy server
│   ├── helpers/           # Utility functions
│   ├── platforms/         # Platform implementations (12 platforms)
│   ├── readers/           # Hardware reader drivers (8 reader types)
│   │   ├── acr122pcsc/    # ACR122U PC/SC reader
│   │   ├── file/          # File-based token simulation
│   │   ├── libnfc/        # libnfc-based readers
│   │   ├── opticaldrive/  # Optical drive barcode reader
│   │   ├── pn532/         # PN532 NFC reader
│   │   ├── pn532uart/     # PN532 UART reader
│   │   ├── simpleserial/  # Simple Serial Protocol
│   │   ├── tty2oled/      # TTY2OLED display reader
│   │   └── shared/        # Shared reader utilities
│   ├── service/           # Core business logic
│   │   ├── tokens/        # Token processing
│   │   ├── playlists/     # Playlist management
│   │   ├── state/         # Application state
│   │   └── queues/        # Processing queues
│   ├── testing/           # TDD infrastructure ⭐
│   │   ├── mocks/         # Interface mocks
│   │   ├── helpers/       # Testing utilities
│   │   ├── fixtures/      # Test data
│   │   ├── examples/      # Example tests
│   │   └── sqlmock/       # SQL mocking utilities
│   ├── ui/                # User interface components
│   │   ├── systray/       # System tray UI
│   │   ├── tui/           # Terminal UI
│   │   └── widgets/       # UI widgets
│   └── zapscript/         # ZapScript language implementation
│       ├── models/        # Script data models
│       ├── parser/        # Script parser
│       └── commands/      # Script commands
├── assets/                 # Platform-specific assets
├── scripts/                # Build and utility scripts
└── docs/                   # Documentation
    └── api/                # API documentation
```

## Development Commands

All development tasks use the `task` command (Taskfile):

```bash
# Core Commands
task test               # Run tests with TDD Guard integration ⭐
task lint-fix           # Auto-fix linting issues
task clean              # Clean build artifacts
task vulncheck          # Security vulnerability scanning
task nilcheck           # Nil-pointer analysis
task get-logs           # Download logs from API

# Testing Commands
task test               # Run all tests
task test -- -v         # Verbose test output
task test -- ./pkg/...  # Test specific package
task test -- -race      # Run with race detector

# Platform Builds (use environment variables)
task build              # Build for current platform
GOOS=linux GOARCH=amd64 task build    # Linux AMD64
GOOS=windows GOARCH=amd64 task build  # Windows AMD64
GOARCH=arm task build                 # ARM build (for MiSTer)

# Platform-specific builds
task linux:build-amd64   # Build for Linux
task windows:build-amd64 # Build for Windows
task mister:arm          # Build for MiSTer
```

## Test-Driven Development (TDD)

This project has comprehensive TDD infrastructure. **Read [TESTING.md](TESTING.md) first** for complete testing guide.

### Quick Testing Setup

```go
import (
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
)

// All major interfaces have mocks ready to use
mockPlatform := mocks.NewMockPlatform()
mockReader := mocks.NewMockReader()
mockUserDB := helpers.NewMockUserDBI()
```

### Key Testing Features

- **Zero hardware dependencies** - All hardware interactions are mocked
- **Fast feedback** - Complete test suite runs in <5 seconds
- **Real API testing** - Tests use actual codebase APIs, not stubs
- **Comprehensive examples** - See `pkg/testing/examples/` for patterns
- **Quick reference guide** - See `pkg/testing/README.md` for all available helpers

### Running Tests

```bash
# Always use 'task test' instead of 'go test' for TDD Guard integration
task test                           # Run all tests
task test -- -run TestTokenProcessing  # Run specific test
task test -- -cover                # With coverage report
```

## Important Development Notes

### API Endpoints

- WebSocket: `ws://localhost:7497/api/v0.1`
- HTTP: `http://localhost:7497/api/v0.1`
- Default port: 7497 (configurable)

### Database Migrations

- Managed via [goose](https://github.com/pressly/goose)
- Migrations in `pkg/database/{userdb,mediadb}/migrations/`
- Auto-applied on startup

### Platform Detection

- Platform-specific configs in `~/.config/zaparoo/`

### Reader Auto-Detection

- 8 reader types supported: acr122pcsc, file, libnfc, opticaldrive, pn532, pn532uart, simpleserial, tty2oled
- Readers auto-detect by default
- Manual connection via config file
- Multiple readers supported simultaneously

### Code Style

- Use `task lint-fix` before committing
- Follow existing patterns in codebase
- Write tests for new features (see TESTING.md)

## Getting Started

1. **Install dependencies**: `go mod download`
2. **Run tests**: `task test`
3. **Build**: `task build`

## Additional Features

### ZapScript Language
- Custom scripting language for automation
- Parser and command execution in `pkg/zapscript/`
- Supports playlist management, input handling, and HTTP requests

### UI Components
- **TUI**: Terminal-based user interface in `pkg/ui/tui/`
- **Systray**: System tray integration in `pkg/ui/systray/`
- **Widgets**: Reusable UI components in `pkg/ui/widgets/`

### Groovy Proxy
- Proxy server for Groovy integration
- Located in `pkg/groovyproxy/`

### Playlist Management
- Advanced playlist system with token mapping
- Support for dynamic playlists and ZapScript integration

## Documentation

For testing patterns and examples, see [TESTING.md](TESTING.md) and `pkg/testing/examples/`.
For a quick reference to all testing helpers, see `pkg/testing/README.md`.
For API documentation, see `docs/api/`.
For ZapScript documentation, see `pkg/zapscript/`.