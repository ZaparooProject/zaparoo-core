# Architecture Reference

Reference material for Zaparoo Core's architecture, APIs, and subsystems. For development guidelines and agent instructions, see [AGENTS.md](../AGENTS.md).

## Key Concepts

- **Tokens**: Physical objects (NFC tags, barcodes, QR codes, optical discs) that carry or are mapped to ZapScript commands. Identified by UID, text content, or raw data.
- **ZapScript**: Command language stored on tokens. Commands prefixed with `**` (e.g., `**launch:path`), chained with `||`. A bare path auto-launches as media. See `pkg/zapscript/` and the advanced args parser in `pkg/zapscript/advargs/`.
- **Mappings**: Rules that override token behavior via pattern matching (exact, partial/wildcard, regex) against UID, text, or data. Essential for read-only tokens like Amiibo. Stored in UserDB or as TOML files in `mappings/`.
- **Launchers**: Per-system programs that launch games/media. Each platform provides built-in launchers. Custom launchers via TOML files in `launchers/`. See `pkg/platforms/`.
- **Systems**: 200+ supported game/computer/media systems (e.g., `SNES`, `Genesis`, `PSX`). IDs are case-insensitive with aliases and fallbacks.
- **Readers**: Hardware or virtual devices that detect tokens. Two scan modes: **tap** (default, free removal) and **hold** (token must stay on reader, removal stops media).

## Zaparoo Ecosystem

- **Zaparoo App** ([zaparoo-app](https://github.com/ZaparooProject/zaparoo-app)) - Primary UI (iOS, Android, Web). Web build embedded at `pkg/assets/_app/dist/`, served at `/app/`. Uses Core's JSON-RPC API.
- **go-pn532** - NFC reader driver library for PN532 reader implementations
- **go-zapscript** - ZapScript language parser library

## API

- **WebSocket**: `ws://localhost:7497/api/v0.1` | **HTTP**: `http://localhost:7497/api/v0.1`
- **Port**: 7497 (configurable via config.toml)
- **Protocol**: JSON-RPC 2.0
- **App UI**: served at `/app/` (root `/` redirects)
- **Launch endpoint**: `/l/{zapscript}` - GET-based execution for QR codes
- **Auth**: API keys via `auth.toml`, anonymous access from localhost
- **Discovery**: mDNS (`_zaparoo._tcp`)
- **Notifications**: Real-time WebSocket events (readers, tokens, media, indexing, playtime). See `docs/api/notifications.md`.
- **Full docs**: `docs/api/`

## Database

- **Dual-database design**: UserDB (mappings, history, playlists) + MediaDB (indexed media content)
- **Migrations**: goose, SQL files in `pkg/database/{userdb,mediadb}/migrations/`, auto-applied on startup
- **Thread-safe**: Use database interface methods (`UserDBI`, `MediaDBI`), not direct SQL
- **MediaDB**: WAL mode, busy timeout 5000ms, `syncutil.RWMutex` for serialization

## Configuration

- **Location**: XDG-based (`xdg.ConfigHome/zaparoo/config.toml`, typically `~/.config/zaparoo/config.toml` on Linux)
- **Format**: TOML with schema versioning
- **Thread-safe**: `config.Instance` uses `syncutil.RWMutex`
- Maintain backward compatibility — use migrations for breaking changes

## Reader Auto-Detection

11 reader types: acr122pcsc, externaldrive, file, libnfc, mqtt, opticaldrive, pn532, pn532uart, rs232barcode, simpleserial, tty2oled
