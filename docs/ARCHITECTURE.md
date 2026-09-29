# Architecture Reference

Reference material for Zaparoo Core's architecture, APIs, and subsystems. For development guidelines and agent instructions, see [AGENTS.md](../AGENTS.md).

## Key Concepts

- **Tokens**: Physical objects (NFC tags, barcodes, QR codes, optical discs) that carry or are mapped to ZapScript commands. Identified by UID, text content, or raw data.
- **ZapScript**: Command language stored on tokens. Commands prefixed with `**` (e.g., `**launch:path`), chained with `||`. A bare path auto-launches as media. See `pkg/zapscript/` and the advanced args parser in `pkg/zapscript/advargs/`.
- **Mappings**: Rules that override token behavior via pattern matching (exact, partial/wildcard, regex) against UID, text, or data. Essential for read-only tokens like Amiibo. Stored in UserDB or as TOML files in `mappings/`.
- **Launchers**: Per-system programs that launch games/media. Each platform provides built-in launchers. Custom launchers via TOML files in `launchers/`. See `pkg/platforms/`.
- **Systems**: 200+ supported game/computer/media systems (e.g., `SNES`, `Genesis`, `PSX`). IDs are case-insensitive with aliases and fallbacks.
- **Readers**: Hardware or virtual devices that detect tokens. Two scan modes: **tap** (default, free removal) and **hold** (token must stay on reader, removal stops media). The mode in force for a launch resolves in order: the token's `#tap`/`#hold` ZapScript trait, the reader's `[[readers.connect]]` entry, its `[readers.drivers.<id>]` entry, then the global `readers.scan.mode`. Tap-mode reader launches that resolve to the running game are successful no-ops by default; `readers.scan.allow_relaunch = true` restores restart-on-tap. Comparison happens after media resolution, not on token identity or script text.
- **Traits**: Script-level `#key=value` metadata declaring something about a token. Resolved once in `processTokenQueue`, the single point every token carrying script text passes through, and carried on the token from then on. Tokens derived from another — playlist tracks, hook scripts, injected commands — inherit rather than resolving their own, so running a script can never change the traits of the token running it.
- **Global UI events**: Server-owned transient notice, loader, picker, or confirm requests. Host and connected clients render in parallel; first ID-bound response wins or Core times request out. See `pkg/ui/events/`.

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
- **Notifications**: Real-time WebSocket events (readers, tokens, media, indexing, playtime, global UI). See `docs/api/notifications.md`.
- **Full docs**: `docs/api/`

## Embedding

A host application can run Core in its own process instead of as a service binary. Every existing platform keeps its standalone behaviour; each seam below is opt-in.

- **Host-managed paths**: `platforms.Settings.HostManagedPaths` makes the configured data, config, cache and log directories authoritative, with no portable-directory probing next to the executable. `config.NewHostConfig` loads the config from an absolute host directory and ignores the process environment overrides.
- **No self-update**: `platforms.Settings.DisableSelfUpdate` turns off the updater, its watchdog and rollback. `update.check` and `update.status` report `managed`, and `update.apply` is refused: the host updates Core with its own package.
- **Embedded start**: `service.StartEmbedded(pl, cfg, EmbeddedOptions{...})` requires both settings above and absolute directories. The host supplies the parent context, the API listener, the audio player, an optional API key provider and UI renderer, and phase (`starting`, `migrating`, `ready`, `stopped`) and fatal-error callbacks. Cancelling the parent context stops the service, and start and stop can repeat in one process. An embedded start creates no startup server: the host owns the listener and reports its own startup state. Global UI events are drawn only by the host's renderer, never one the platform implements.
- **Supplied listener**: `api.StartWithListener(ListenerOptions{Listener, APIKeys, Network, OnNetwork})` serves the API on a listener the host created, typically an app-private Unix socket. Ready is reported once the listener accepts.
- **Locality and auth on a supplied listener**: a Unix-socket peer is local (`IsLocalRequest`), so it skips the IP filter and rate limiter and may use plaintext WebSocket even when encryption is required: the socket is reachable only by the host app. It still needs a key from `APIKeys` on every private route, even when no standalone keys are configured; `/health` stays open. No TCP peer of a supplied-listener server is local, loopback included, because an embedding app shares loopback with every other app on the device.
- **Network listener**: with `Network`, the server also binds the configured TCP address and serves the same API there exactly as a standalone server would (configured keys, pairing, encryption, IP filter, rate limits, origins); `APIKeys` authenticates nobody there. A failed bind is logged and costs only remote access; the supplied listener keeps serving.
- **Source roots**: media a host granted to Core in folders the operating system cannot open. A platform implementing `platforms.SourceRootReader` lists its roots (`source://<id>`, the ID a hash of the host's reference from `platforms.SourceRootPath`) and reads their directories; it makes no other decision. The indexer treats each root like a `RootDirs` directory: it looks for launcher `Folders` in it, walks the matching system folders with the same rules (hidden folders, `.zaparooignore`, scan excludes, extensions), and stores files as multi-segment virtual paths (`source://<id>/<dir>/<file>`, `virtualpath.CreateVirtualPathSegments`). A launcher matches them by its relative `Folders` and extensions, as for a root directory, and launching is the platform's own. A root the host stops listing loses its media like an unmounted card; a host that fails to list or read a root fails the run or marks the system incomplete, so nothing goes missing because the host did not answer. Custom launchers cannot claim the `source` scheme.
- **Android platform** (`pkg/platforms/android`): ordinary Go behind the `android.Host` interface the embedding app implements (package inspection, installed RetroArch cores, installed apps, the media folders the user granted, and intent dispatch). Launchers come from an embedded, reviewed catalog of launch definitions (`pkg/platforms/android/catalog/`); only a catalog definition, copied, ever reaches the host, and the host's receipt must name the component it asked for. Granted media folders are source roots, and each launcher's `Folders` are its system's ES-DE folders, ID and aliases. An installed app is media too: its identity is `android://<package>[:variant]/<name>`, a version 3 catalog profile can pin one launchable variant of an app, and every other launchable app the host reports is offered generically, without a catalog entry. Launches are `LifecycleExternal`, and a host failure becomes a `launch_repair` reason.

## Global UI Events

`pkg/ui/events/` owns presentation lifecycle, authoritative expiry, monotonic revision, and first-response-wins arbitration. It initially exposes at most one active event but serializes plural `events`/`resolved` arrays so future overlays or priorities need no wire-format break.

Platform UI rendering is optional. MiSTer implements notice, loader, picker, and confirm widgets; Batocera mirrors passive notice/loader text; unsupported platforms rely on App or another API client. Renderer errors never cancel event.

Domain owners keep behavior: reader manager owns staged launch-guard token, playlist code owns selected ZapScript action, and installer owns download lifecycle. Public picker choices contain only label and opaque ID. Global events are broadcast to every permitted client and must never carry PINs, passwords, credentials, or other secrets.

Clients consume `ui.changed`, replace local state using newest `revision`, query `ui` after reconnect, and answer through `ui.respond`. Legacy launch-guard `confirm` and token notifications remain supported. Launch-guard confirmation events deliberately skip host rendering so opening a MiSTer widget cannot interrupt active media; existing sound feedback and card re-tap confirmation remain unchanged.

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

## Crash Evidence

Service startup registers Go's `debug.SetCrashOutput` before native initialization. Crash output goes to `core.crash.log` in the platform's persistent data directory (`/media/fat/zaparoo` on MiSTer), independently of routine logs and stderr capture. A small version header is written at startup; subsequent writes are runtime fatal output, using synchronous file writes.

On the next service start, a crash is renamed to `core.crash.previous.log` before fresh capture opens. Healthy starts leave that previous crash untouched; a newer crash replaces it. Both files travel with log bundles, with crash evidence taking priority over routine logs within upload limits.

When error reporting is enabled, rotation triggers one best-effort Sentry event containing crash kind, original release, and selected code symbols. Raw panic messages, argument values, source paths, and register contents remain local. Reporting never deletes evidence. There is no upload retry queue; disabling telemetry or failed delivery does not prevent local retention.

Coverage includes unrecovered Go panics, runtime fatal errors, and native signals handled by the Go runtime (such as a normal C `abort()` on Linux). Native exits that bypass Go, SIGKILL, power loss, and failures before capture registration are not covered. Synchronous writes reduce reset-related data loss but cannot guarantee SD hardware behavior during abrupt power removal.

## Profiles

Device profiles are named buckets of preferences and limits, with no passwords or accounts. See `pkg/service/profiles/`.

- **Active profile**: one per device, held as a snapshot in service state (`pkg/service/state/`) and persisted in the UserDB `DeviceState` table so it survives restarts. The un-profiled state is the implicit **shared profile** — the device as it behaves when nobody is signed in: global-config limits, unattributed history, default data locations. It is an interpretation, not a database row; deactivating means switching to it.
- **Switching**: via API (`profiles.switch`) or by scanning a card containing `**profile:<switchId>`. The switch ID is a word phrase (e.g. `corn-arm-truck`) generated from an embedded wordlist and is a **bearer credential**: presenting it authorizes a PIN-free switch on every path, so the API only returns switch IDs to privileged (local/admin) clients. The PIN protects pick-from-list switching by `profileId`. PINs gate entry only; deactivating is always free.
- **Playtime limits**: profiles can override the global daily/session limits. `pkg/service/playtime.LimitsManager` reads limits through a `LimitsProvider`; the profile-aware resolver (`pkg/service/profiles.LimitsResolver`) layers the active profile's overrides over global config. Daily usage accounting is scoped to the active profile via the `ProfileID` column on `MediaHistory` (rows are attributed at launch time). Everything about a running game belongs to the profile that launched it: the limits context is pinned at media start, so deactivating mid-game keeps the launch profile's limits until the media stops. The session resets only when the profile *identity* changes (switching to a different person), never on rescans, edits, or deactivation.
- **Playtime extensions**: an administrator can grant extra time to the session currently being limited, without stopping what is playing or editing a limit. Two entry points reach one grant path on `LimitsManager`: the `playtime.extend` API method (gated on the `playtime.extend` capability) and a scanned card holding `**playtime.extend:<amount>?profile=<switchId>`, where the switch ID is an admin profile's bearer credential and the card is rejected from any source but a physical reader. A grant either adds a bounded duration to the session's allowance or waives the session limit until the next local midnight; the daily limit is untouched by both. Grants flow through `effectiveSessionLimit` so every consumer — the periodic check, the pre-launch gate, and status — sees them, and are pinned to the profile that owned the session, persisted in `DeviceState` so a restart inside the cooldown window does not revoke them, and cleared when the session resets. Because both commands carry a switch ID, `pkg/zapscript.RedactScript` strips credentials from logs, stored history, and the token APIs, failing closed on anything it cannot parse.
- **Require-profile gate**: the `[profiles] require_for_launch` config setting stops the shared profile launching media (profile switch commands still run, so scanning a card unparks the device; a combo card that switches then launches passes).
- **Data swapping**: on platforms implementing the optional `platforms.ProfileDataSwapper` capability, the active profile also owns its save files and save states. `pkg/service/profiles.DataSwapCoordinator` drives it: switches apply through a single worker (briefly waited on so combo-card launches see the new data), swaps while media runs are deferred until it stops and coalesce to the last target, and errors only ever notify (`profiles.data`) — the switch itself never fails on file operations. MiSTer implements it with bind mounts (`pkg/platforms/mister/profiledata.go`): zero on-disk mutation, pools under `zaparoo/profiles/<id>/`, main's storage root (`device.bin` SD/USB) resolved per apply, foreign mounts (NAS saves) layered on with the pool inside the share and never touched, ownership proven via a tmpfs ledger (`/run/zaparoo/mounts.json`), and a `/proc/self/mountinfo` watcher re-reconciling when the mount table changes. The `[profiles] swap_data` setting (default on) disables it, converging mounts back to shared.
- **Roles and permissions**: profile roles and client roles are separate. Profiles represent people/kiosk identities; the first profile is explicitly created as `admin` with a mandatory PIN and later profiles default `member`. Paired clients represent trusted devices; the first paired client is explicitly confirmed as `admin` and later clients default `member`. Remote profile management requires an admin client. Sensitive local TUI actions use `profiles.verify` as a client-side nuisance gate before sending ordinary requests; there is no retained unlock session or server-side linkage between verification and action. The last admin profile/client cannot be removed or demoted. Existing databases with profiles but no admin enter local setup recovery, allowing one profile to be promoted with a PIN. While `service.encryption` is off, unpaired remote clients retain legacy admin capability; enabling it requires pairing and makes member restrictions enforceable.

## Reader Auto-Detection

11 reader types: acr122pcsc, externaldrive, file, libnfc, mqtt, operator (MiSTer only), opticaldrive, pn532, rs232barcode, simpleserial, tty2oled
