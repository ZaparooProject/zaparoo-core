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

## Profiles

Device profiles are named buckets of preferences and limits, with no passwords or accounts. See `pkg/service/profiles/`.

- **Active profile**: one per device, held as a snapshot in service state (`pkg/service/state/`) and persisted in the UserDB `DeviceState` table so it survives restarts. The un-profiled state is the implicit **shared profile** — the device as it behaves when nobody is signed in: global-config limits, unattributed history, default data locations. It is an interpretation, not a database row; deactivating means switching to it.
- **Switching**: via API (`profiles.switch`) or by scanning a card containing `**profile:<switchId>`. The switch ID is a word phrase (e.g. `corn-arm-truck`) generated from an embedded wordlist and is a **bearer credential**: presenting it authorizes a PIN-free switch on every path, so the API only returns switch IDs to privileged (local/admin) clients. The PIN protects pick-from-list switching by `profileId`. PINs gate entry only; deactivating is always free.
- **Playtime limits**: profiles can override the global daily/session limits. `pkg/service/playtime.LimitsManager` reads limits through a `LimitsProvider`; the profile-aware resolver (`pkg/service/profiles.LimitsResolver`) layers the active profile's overrides over global config. Daily usage accounting is scoped to the active profile via the `ProfileID` column on `MediaHistory` (rows are attributed at launch time). Everything about a running game belongs to the profile that launched it: the limits context is pinned at media start, so deactivating mid-game keeps the launch profile's limits until the media stops. The session resets only when the profile *identity* changes (switching to a different person), never on rescans, edits, or deactivation.
- **Require-profile gate**: the `[profiles] require_for_launch` config setting stops the shared profile launching media (profile switch commands still run, so scanning a card unparks the device; a combo card that switches then launches passes).
- **Data swapping**: on platforms implementing the optional `platforms.ProfileDataSwapper` capability, the active profile also owns its save files and save states. `pkg/service/profiles.DataSwapCoordinator` drives it: switches apply through a single worker (briefly waited on so combo-card launches see the new data), swaps while media runs are deferred until it stops and coalesce to the last target, and errors only ever notify (`profiles.data`) — the switch itself never fails on file operations. MiSTer implements it with bind mounts (`pkg/platforms/mister/profiledata.go`): zero on-disk mutation, pools under `zaparoo/profiles/<id>/`, main's storage root (`device.bin` SD/USB) resolved per apply, foreign mounts (NAS saves) layered on with the pool inside the share and never touched, ownership proven via a tmpfs ledger (`/run/zaparoo/mounts.json`), and a `/proc/self/mountinfo` watcher re-reconciling when the mount table changes. The `[profiles] swap_data` setting (default on) disables it, converging mounts back to shared.
- **Roles and permissions**: profile roles and client roles are separate. Profiles represent people/kiosk identities; the first profile is explicitly created as `admin` with a mandatory PIN and later profiles default `member`. Paired clients represent trusted devices; the first paired client is explicitly confirmed as `admin` and later clients default `member`. Remote profile management requires an admin client. Sensitive local TUI actions use `profiles.verify` as a client-side nuisance gate before sending ordinary requests; there is no retained unlock session or server-side linkage between verification and action. The last admin profile/client cannot be removed or demoted. Existing databases with profiles but no admin enter local setup recovery, allowing one profile to be promoted with a PIN. While `service.encryption` is off, unpaired remote clients retain legacy admin capability; enabling it requires pairing and makes member restrictions enforceable.

## Reader Auto-Detection

10 reader types: acr122pcsc, externaldrive, file, libnfc, mqtt, opticaldrive, pn532, rs232barcode, simpleserial, tty2oled
