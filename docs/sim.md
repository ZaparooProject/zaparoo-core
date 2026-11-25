# Zaparoo Core sim

The sim runs a complete Zaparoo Core service on a fake platform. The real service, API, reader pipeline, ZapScript, launch lifecycle and databases all run. The readers are simulated, the launchers start invisible `sleep` children, audio is silent, and the network services are offline fakes. Everything lives under one throwaway root directory.

There are two ways to use it:

- `zaparoo-sim` (`cmd/sim`): a CLI that starts a sim Core and drives it by hand, with scripts or with `zaparoo-cli`.
- `pkg/testing/harness`: the same sim in-process, for Go tests and testscript (`.txtar`) scenarios.

Agent quick start: `.agents/skills/core-sim/SKILL.md`. Testing layers: `TESTING.md`.

## What it is and the safety guarantees

The sim is the platform `pkg/platforms/sim` plus the reader driver `pkg/readers/sim`. The rest is production Core code.

The sim keeps Core away from the host:

| Guarantee | How |
| --- | --- |
| No shared directories | Config, data, logs and temp files are all under `--root`. `CheckRoot` refuses `/`, the home directory, any path above or inside the real Linux Core data, config or log directories, and any non-empty directory that lacks the `.zaparoo-sim` marker. |
| No clash with a real Core | `serve` refuses `--port 7497`. `--port 0` (the default) picks a free loopback port and never picks 7497. The API listens on `127.0.0.1` only. |
| No real launchers or scanners | Only sim launchers exist. Their children are `sleep` processes, and stopping the sim SIGKILLs them all. |
| No real readers | Only the `sim` driver runs. The `file`, `simpleserial` and `mqtt` drivers load only when the config has a `[[readers.connect]]` entry for them, and they never auto-detect. |
| No sound | Audio output is silent (`audio.UseSilentOutput`). The harness records sound names instead. |
| No network | Discovery (mDNS), BLE, the internet check, update checks and installs, and error reporting are off. Online and the libretro thumbnails server point at their loopback fakes, or at `http://127.0.0.1:9` (unreachable) when the fake is not running. Log upload does the same. RomM points at its fake only when it runs. These URLs are written to `config.toml` on every boot, so `settings.update` and `settings.reload` keep them. |
| No self-update | The platform sets `DisableSelfUpdate` and `HostManagedPaths`. |

Power actions (reboot, shutdown and the rest) only write a journal event.

## Quick start

```sh
task sim:build                       # builds _build/sim/zaparoo-sim
export ZAPAROO_SIM_ROOT=$(mktemp -d /tmp/zsim.XXXX)
S=_build/sim/zaparoo-sim

$S serve --fakes libretro            # detached; prints the API URL and socket
$S fixtures gen --systems snes --per-system 5
$S fixtures index                    # real media.generate; prints "indexed N media in D"
$S api media.search '{"query":""}'
$S tap "$ZAPAROO_SIM_ROOT/roms/snes/<file>.smc"
$S launches                          # launch-request, launch, process, tracked, ...
$S status                            # readers, active media, tracked pid, children
$S stop
rm -rf "$ZAPAROO_SIM_ROOT"
```

`task sim:run ROOT=dir -- --fakes libretro` builds and runs `serve --foreground` (default root `_build/sim/root`). A plain `go build` needs the tags `sqlite_omit_load_extension,validator_novalidatefn,expr_static_methods`. Without the `task` ldflags the version reads `DEVELOPMENT` instead of `sim-<sha>`.

## CLI reference

```
zaparoo-sim [--root DIR] [--json] <command> [args]
```

- `--root DIR` (also `--root=DIR`) defaults to `$ZAPAROO_SIM_ROOT`. Every command except `serve` and `fixtures gen` talks to the instance running from that root.
- `--json` prints machine-readable output. It may come before or after the command.
- `-h`, `--help` and `help` print usage.
- Exit code 2 means a usage error. Exit code 1 means any other error.

### Instance

| Command | Flags | Notes |
| --- | --- | --- |
| `serve` | `--port N` (0 = free port), `--online-url URL`, `--fakes libretro,romm,logs,online`, `--readers r1,r2` (default `sim0`), `--foreground` | See below. |
| `status` | | Prints pid, API URL, root, journal sequence, fakes, readers, active media, tracked pid, children and faults. `--json` prints `{"instance": <sim.json>, ...status}`. |
| `stop` | | Asks the instance to stop and waits up to 15s for its process to exit. |

`serve` refuses to start when:

- `--port 7497` is given ("belongs to a real Core");
- `--online-url` and `--fakes online` are both given;
- a sim is already running from the root (the PID in `sim.json` is alive).

Without `--foreground`, `serve` re-runs itself in a new session, sends its output to `<root>/logs/sim-serve.log`, and waits up to 60s for `sim.json` to report ready. It then prints `sim running: pid N, api URL, socket PATH` and one `fake NAME: URL` line per fake.

`serve` has no flags for Core settings. Change them through the API once it runs, for example `api settings.update '{"readersScanMode":"hold","launchGuardEnabled":true}'`. The default scan mode is `tap`, where removing a token does nothing. The sim saves settings to its own `config.toml`, and the fake URLs are kept.

In the foreground, Core logs at debug level (trace when `ZAPAROO_TRACE` is set). The sim prints `sim ready: api ... socket ...` and runs until SIGINT, SIGTERM or `POST /stop`. `sim.json` is removed on exit.

`--port` always wins over the port saved in `config.toml`. `--online-url` overrides the Online base URL for this run only. It must be https, or http to localhost or a private IP.

### Readers

The default reader for `--reader` is the only reader when there is one, else `sim0`.

| Command | Flags | Effect |
| --- | --- | --- |
| `readers ls` (`list`) | | List sim reader devices. |
| `readers add NAME` | `--no-write`, `--no-removable`, `--no-auto-detect`, `--write-wait-ms N` | Plug in a new device. `--no-auto-detect` means only a `[[readers.connect]]` entry opens it. |
| `readers rm NAME` (`remove`) | | Remove a device. |
| `tap [TEXT]` | `--reader`, `--uid`, `--data HEX`, `--type`, `--hold-ms N` (default 200), `--keep` | Present a tag, then remove it after `--hold-ms` unless `--keep`. Needs TEXT, `--uid` or `--data`. |
| `hold [TEXT]` | tap flags | `tap --keep`. |
| `remove` | `--reader` | Take the tag off. |
| `reader-error` | `--reader` | Report a reader error. |
| `scan-error` | `--reader`, `--message M` (default `sim scan error`) | Report a failed scan. |
| `unplug`, `plug` | `--reader` | Disconnect or reconnect the device. |
| `write-complete` | `--reader`, `--uid` | Present a tag to a pending write. The tag stays on the reader. |
| `write-fail` | `--reader`, `--mode error\|removed\|no-tag\|unsupported` (default `error`) | Make the next write fail. |

### Journal

| Command | Flags | Notes |
| --- | --- | --- |
| `launches` | `--since SEQ`, `--follow`, `--timeout D` | Launch lifecycle events only (see Journal event kinds), so sequence numbers have gaps. |
| `journal` | `--since SEQ`, `--kind K,K`, `--follow`, `--timeout D` | All events. `--json` prints an array, or one JSON object per line with `--follow`. |

Text output is one line per event: `#SEQ HH:MM:SS.mmm KIND key=value ...`.

### Faults

| Command | Flags |
| --- | --- |
| `fault ls` (`list`) | |
| `fault set` | `--launcher ID` (default `*`), `--fail-launches N`, `--launch-delay-ms N`, `--crash-after-ms N`, `--exit-after-ms N`, `--fail-stops N`, `--fail-preflight`, `--ready-timeout`, `--not-running`, `--unavailable`, `--fail-kill` |
| `fault clear` | `--launcher ID` (default: every fault) |

### Core API

| Command | Flags | Notes |
| --- | --- | --- |
| `api METHOD [JSON]` | | JSON-RPC call to `<apiUrl>/api/v0.1`. Prints the result as indented JSON (`null` for an empty result). |
| `wait NOTIFICATION` | `--match K=V` (repeatable, dotted keys), `--timeout D` (default 30s) | Wait on the SSE stream `/api/v0.1/events` for a matching notification. It only sees notifications sent after it subscribes. |

`--match` compares JSON text with strings unquoted, so `active=true` and `systemId=SNES` both work. Dotted keys reach nested fields.

### Fixtures

| Command | Flags |
| --- | --- |
| `fixtures gen` | See Fixtures. Needs `--systems` or `--all`. Works before `serve`. |
| `fixtures index` | `--timeout D` (default 10m), `--systems S,S`. Runs a real `media.generate`. |
| `fixtures bigdb` | `--entries N` (default 100000), `--seed N`, `--systems S,S`. Refuses while the sim runs. |

### Fakes

`fake NAME ARGS...` drives a running fake (`libretro`, `romm`, `logs`, `online`). See Fakes for each fake's commands. The fake must have been started with `serve --fakes NAME`.

## Root layout

```
<root>/
  .zaparoo-sim        marker; CheckRoot needs it in a non-empty root
  sim.json            instance state (written once ready, removed on exit)
  sim.sock            control socket (mode 0600)
  journal.jsonl       journal, appended
  config/config.toml  Core config, written on first boot
  data/               media.db, user.db, assets, images, mappings, ...
  logs/               core.log, sim-serve.log (detached serve)
  tmp/                temp dir; tmp/screenshot.png
  roms/               ROM tree; the only index root
  online/state.json   Online fake state (serve only, saved on stop)
```

`sim.json`:

| Field | Meaning |
| --- | --- |
| `fakes` | Map of fake name to base URL. |
| `layout` | `root`, `configDir`, `dataDir`, `logDir`, `tempDir`, `romsDir`, `socket`, `stateFile`, `journal`, `screenshot` |
| `apiUrl` | `http://127.0.0.1:PORT` |
| `version` | Core version string. |
| `pid` | `serve` process ID. |
| `apiPort` | API port. |
| `ready` | True once `/health` answered. |

First-boot `config.toml` settings: the API port, `api_listen = '127.0.0.1'`, discovery, BLE, the internet check, update check and install off, `error_reporting = false`, `debug_logging = true`, `index_root = [<root>/roms]`, and auto-detect off for the `file`, `simpleserial` and `mqtt` drivers. Later boots keep the saved config, except that `--port`, `--online-url` and the fake URLs are rewritten into it each run. They are saved, not held in memory, because `settings.update` and `settings.reload` read `config.toml` back in.

The boot readers are opened by sim-driver auto-detect, not by `[[readers.connect]]` entries.

## Control socket HTTP schema

`<root>/sim.sock` is a Unix socket (mode 0600) serving JSON over HTTP. The CLI uses it for everything except `api` and `wait`. The socket path must fit in 107 bytes; use a shorter `--root` if it doesn't. A stale socket file is replaced; a live one is an error.

```sh
curl --unix-socket "$ZAPAROO_SIM_ROOT/sim.sock" http://sim/status
```

| Route | Body or query | Returns |
| --- | --- | --- |
| `GET /status` | | Status (below) |
| `GET /readers` | | Array of device status |
| `POST /readers` | `{"name", "writeWaitMs", "notRemovable", "noWrite", "noAutoDetect"}` | Device status |
| `DELETE /readers/{name}` | | |
| `POST /readers/{name}/tap` | `{"uid", "text", "data", "type", "holdMs"}` | The tag as presented |
| `POST /readers/{name}/remove` | | |
| `POST /readers/{name}/error` | | |
| `POST /readers/{name}/scan-error` | `{"message"}` | |
| `POST /readers/{name}/unplug` | | |
| `POST /readers/{name}/plug` | | |
| `POST /readers/{name}/write-complete` | `{"uid"}` | The tag presented |
| `POST /readers/{name}/write-fail` | `{"mode"}` | |
| `GET /journal` | `?since=SEQ&kind=a,b&follow=1&timeout_ms=N` | Array of events, or NDJSON with `follow` |
| `GET /launches` | same as `/journal`; `kind` defaults to the launch kinds | |
| `GET /faults` | | Array of faults |
| `POST /faults` | A fault object | Array of faults. Journals a `fault` event with detail `set`. |
| `DELETE /faults` | `?launcher=ID` (none = all) | |
| `POST /stop` | | `{"stopping":true}` |
| `/fakes/{name}/...` | | Each running fake's control API |

Errors are `{"error": "..."}` with status 400, or 404 when there is no such device. A handler with no result returns `{"ok":true}`. With `follow=1`, the stream ends when the client leaves, `timeout_ms` passes or the sim stops.

Status fields: `pid`, `root`, `journalSeq`, `activeMedia` (Core's active media, omitted when none), `trackedPid` (omitted when none), `children` (map of child PID to launcher ID), `readers`, `faults`.

Device status fields: `name`, `readerId`, `plugged`, `open` (Core has the reader open), `writing` (a write is pending), `removable`, `canWrite`, `autoDetect`, `present` (current tag), `writes` (last 100 writes: `time`, `text`, `uid`, `error`), `pendingFault`.

## Journal event kinds

Every event has `seq` and `time`. It may also have `kind`, `launcher`, `system`, `path`, `intent`, `detail`, `error`, `pid` and `args`. The sim keeps the last 5000 events in memory and appends all of them to `journal.jsonl`. A follower that falls behind misses events.

| Kind | Recorded when | Notes |
| --- | --- | --- |
| `launch-request` | Core calls the platform's LaunchMedia | `path` |
| `launch` | A sim launcher runs | `intent` is the launch action. `SimScanner` also records one with detail `scanner` when the indexer runs it. |
| `launch-error` | A launch failed | `error` |
| `process` | A child started | `pid`; `detail` is the `sleep` argument (`infinity` or seconds) |
| `process-exit` | A child ended | `detail`: `stopped`, `crashed`, `exit 0`, `exited` or an error |
| `tracked` | Core tracks the child's PID | |
| `stop` | Core stopped the active launcher | `intent`: `preemption`, `menu`, `console-reset` or `intent-N`; `pid` |
| `stop-error` | A stop failed (`failStops` fault) | |
| `kill` | `SimKill`'s own Kill ran | `detail` lists PIDs |
| `menu` | Return to menu | |
| `launch-system` | LaunchSystem was called | |
| `keyboard`, `gamepad` | Input ZapScript | `detail` is the key or button |
| `screenshot` | A screenshot was taken | |
| `control` | A launcher control ran | `detail`: `save_state`, `load_state`, `reset`, `toggle_pause`, `toggle_menu`, `fast_forward`; `args` |
| `ready` | Ready checks | `detail`: `wait`, `ready` (`SimReady`) or `platform` (the platform's own check) |
| `running` | `SimReady` GameRunning was asked | `detail`: `true` or `false` |
| `external` | `SimExternal` published or cleared its media | `detail`: `published` or `cleared` |
| `finished` | A `SimBlocking` child exited by itself | |
| `fault` | A fault was set or fired | `detail`: `set`, `failLaunches`, `crashAfterMs`, `failPreflight`, `readyTimeout`, `failKill`, `unavailable`, `preflight` |
| `refresh` | Launchers were refreshed | |
| `power` | A power action ran | `detail` is the action |

Launch kinds (what `launches` and `GET /launches` show): `launch-request`, `launch`, `launch-error`, `process`, `process-exit`, `tracked`, `stop`, `stop-error`, `kill`, `external`, `finished`.

The first launch after boot journals `stop intent=preemption` with no launcher. Core stops the active launcher before every launch, even when nothing runs.

## Launcher catalogue

### Per-system launchers

There is one launcher per system in the ES-DE system map, with ID `Sim<SystemID>` (for example `SimSNES`). Its folders are the ES-DE folders for that system and its extensions are the ES-DE extensions, lowercased. It is lifecycle Tracked, and its child is `sleep infinity` unless `exitAfterMs` is set. It has these controls: `save_state`, `load_state`, `reset`, `toggle_pause`, `toggle_menu`, `fast_forward`. Each control journals a `control` event.

### Behaviour launchers

Each behaviour launcher has system `PC`, folder `roms/<lowercase id>` (for example `roms/simblocking`) and extension `.sim`. Fixtures do not create these files. Create them with `touch`, or with `rom` in testscript.

| Launcher | Lifecycle | Behaviour |
| --- | --- | --- |
| `SimFireForget` | FireAndForget | Untracked child that lives 200ms. |
| `SimBlocking` | Blocking | Child lives 2s by default. When it exits by itself the sim journals `finished` and tells Core the media finished, so Core clears the active media. |
| `SimExternal` | External | Untracked child that runs until stopped. After 300ms the sim publishes the active media itself (`external published`). When the child exits the sim clears it (`external cleared`). Its Kill sends SIGTERM and honours `failKill`. |
| `SimRunningInstance` | FireAndForget | Uses a running instance (`UsesRunningInstance = "sim"`). No child. |
| `SimScanner` | Tracked | Scheme `simscanner`. Its scanner adds five virtual entries, `simscanner://extra/1` to `5`, named `Scanner Extra N`. |
| `SimScheme` | Tracked | No folders; skips the filesystem scan. Scheme `simscheme` with entries `simscheme://game/1` to `5`, named `Scheme Game N`. |
| `SimDetails` | Tracked | Supports the details action, which journals a `launch` and starts no child. |
| `SimReady` | Tracked | WaitForReady waits 300ms and journals `ready wait` then `ready ready`. GameRunning journals `running`. |
| `SimUnavailable` | | Always unavailable, with a reason. Journals `fault unavailable`. |
| `SimPreflight` | | Its preflight always refuses. Journals `fault preflight`. |
| `SimNoActive` | FireAndForget | Sets no active media. No child. |
| `SimKill` | Tracked | Its Kill SIGTERMs its children and journals `kill`. With `failKill` it fails, so Core falls back to signalling the PID. |
| `SimDisc` | Tracked | No folders; skips the filesystem scan. Scheme `physicaldisc`; plays `physicaldisc://srN/<System>`, as a MiSTer disc provider does. |
| `SimCDPlayer` | Tracked | Explicit only, so it runs only when named, such as by `audio_cd_launcher`. Scheme `cdaudio`; plays `cdaudio://srN` and `cdaudio://srN/<track>`. |

The `SimScanner` and `SimScheme` entries mean a fresh index always has 10 media more than the ROM files.

### Platform behaviour

- Hostname `zaparoo-sim`. Network: one wired interface `sim0`, no internet. No Bluetooth. Storage is the root's filesystem.
- Stopping the active launcher consumes a `failStops` fault first. If one is queued, the stop fails with `stop-error`.
- ReturnToMenu stops with intent `menu`. `LaunchSystem("menu")` returns to the menu.
- Screenshots are a generated 320x240 PNG.
- All power actions are supported and only journaled.
- When the sim stops, it SIGKILLs every child it started.

## Faults

A fault is a JSON object keyed by launcher ID. `*` applies to every launcher.

| Field | Type | Effect |
| --- | --- | --- |
| `launcher` | string | Launcher ID or `*` (empty means `*`). |
| `failLaunches` | count | The next N launches fail with "sim: injected launch failure". |
| `launchDelayMs` | ms | Sleep before launching. This happens before the failure check. |
| `crashAfterMs` | ms | The child is killed after this long (`process-exit crashed`). |
| `exitAfterMs` | ms | The child exits by itself after this long. |
| `failStops` | count | The next N stops fail. |
| `failPreflight` | bool | Preflight refuses the launch. |
| `readyTimeout` | bool | `SimReady`'s ready wait blocks until Core gives up. |
| `notRunning` | bool | `SimReady`'s GameRunning reports false. |
| `unavailable` | bool | The launcher reports itself unavailable. |
| `failKill` | bool | `SimKill` and `SimExternal`'s Kill fails. |

Rules:

- `fault set` replaces the whole entry for that launcher.
- A launcher's effective fault merges its own entry over `*` field by field: a zero number takes the `*` value, and booleans are OR'd.
- Counters are consumed from the launcher's own entry first, then from `*`. A spent entry stays listed (for example `{"launcher":"SimSNES"}`) until cleared.
- `fault clear` without `--launcher` clears everything.

Example: fail the next SNES launch, then crash every child after 300ms.

```sh
$S fault set --launcher SimSNES --fail-launches 1
$S fault set --crash-after-ms 300
$S fault clear
```

## Sim reader

The driver ID is `sim`. It is enabled and auto-detected by default. Detect returns `sim:NAME` for each device that is plugged in, not open, and does not have `noAutoDetect` set.

Capabilities: write, unless `noWrite`; removable, unless `notRemovable`.

Tags:

- The UID is lowercased. An empty UID becomes a random 7-byte UID starting `04`.
- The type defaults to `NTAG`.
- `data` defaults to the hex of the text.
- A tap replaces any tag already present.
- If a write is pending, a tap goes to the write instead of being scanned.

Events:

| Action | What Core sees |
| --- | --- |
| `remove` | The tag is removed. Errors if the reader is not removable or has no tag. |
| `reader-error` | A reader error. |
| `scan-error` | A scan with an error (the CLI sends `sim scan error` unless `--message` is given). |
| `unplug` | A reader error if a tag was present. Any pending write is cancelled. The reader disconnects. |
| `plug` | The device can be detected and opened again. |

Writes follow PN532 rules:

- A tag already on the reader is written at once.
- Otherwise the write waits for a tap or `write-complete`, up to the device's `writeWaitMs` (default: Core's reader write wait).
- The exclude-UID and target-UID checks apply.
- A `write-fail` mode applies to the next write only:

| Mode | Result |
| --- | --- |
| `error` | "sim write failed" |
| `removed` | Tag removed during the write |
| `no-tag` | No tag detected, immediately |
| `unsupported` | Unsupported tag type |

Error messages: no such sim reader, already exists, not connected to Core, no tag, not removable, no write waiting.

## Fixtures

`fixtures gen` writes a ROM tree under `<root>/roms`. It overwrites existing files but never removes any.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--systems S,S` | | System IDs or folders. |
| `--all` | | Every system. |
| `--per-system N` | 20 | Titles per system. |
| `--seed N` | 1 | Random seed. The same seed gives the same tree. |
| `--source` | `gamedatabase` | Where titles come from: `gamedatabase`, `synthetic` or `dats`. Systems missing from the source fall back to synthetic titles. |
| `--dats DIR` | | DAT directory; implies `--source dats`. |
| `--realistic` | | No-Intro style names: regions, languages, revisions, `[!]`, hacks, translations, and multi-disc sets with an `.m3u` on CD systems. |
| `--zips` | | Some titles as `.zip` on systems that take zips. Arcade is always zipped. |
| `--gamelist` | | A `gamelist.xml` per system. |
| `--media-images` | | Images at `<folder>/media/<kind>/<name>.png`. |
| `--subfolders` | | Nested folders. |
| `--edge-cases` | | CJK and accented names, long names, brackets and ampersands, an uppercase extension, plus files Core must skip: dot-directories, `.zaparooignore`, `__MACOSX`, AppleDouble files and wrong extensions. |

The `--json` report has `root`, `source`, `seed`, `files`, `media` and `systems[]`. Each system has `folder`, `systemId`, `source`, `extension`, `samples`, `titles`, `files`, `media`, `skipped`, `images` and `gamelist`.

`fixtures index` runs `media.generate` through the API and waits for it to finish. It prints `indexed N media in D`.

`fixtures bigdb` is for performance work. It writes `<root>/data/media.db` directly through the production staging pipeline, without walking the filesystem. It refuses while the sim runs. Each system it stages counts as a full scan, so existing media for those systems are marked missing. The default systems are NES, SNES, Genesis, MasterSystem, Gameboy, GameboyColor, GBA, Nintendo64, PSX and Arcade. The report has `path`, `entries`, `elapsedMs` and `systems[]` (`systemId`, `folder`, `entries`, `titlesInserted`, `mediaUpserted`).

## Fakes

Start fakes with `serve --fakes a,b` or `harness.Options.Fakes`. Each fake is an HTTP server on loopback, and its control API is mounted on the control socket at `/fakes/<name>/`. Core's `config.toml` is pointed at the fakes, or at `http://127.0.0.1:9` for a service without a running fake, on every boot.

| Fake | Core setting it replaces | Unset when not running |
| --- | --- | --- |
| `libretro` | Scraper libretro thumbnails base URL | `http://127.0.0.1:9` |
| `romm` | RomM launcher server URL | left as is |
| `logs` | Log upload URL (process-wide, restored on stop) | `http://127.0.0.1:9` |
| `online` | Zaparoo Online base URL | `http://127.0.0.1:9` |

`libretro`, `romm` and `logs` share a control API:

| Route | Effect |
| --- | --- |
| `GET requests`, `DELETE requests` | List or clear the requests the fake received. |
| `GET fault`, `POST fault`, `DELETE fault` | Read, set or clear an HTTP fault: `{"path_prefix", "status", "delay_ms"}`. |

Shared CLI: `requests [clear]`, `fault [set <status> [prefix] | delay <ms> [prefix] | clear]`.

### libretro

Serves the libretro thumbnails server built from the ROM tree: directory listings and generated PNGs for `Named_Boxarts`, `Named_Snaps` and `Named_Titles` under each system's first libretro playlist name.

Extra control: `GET thumbnails`, `POST reload`. CLI: `thumbnails`, `reload`. Run `reload` after changing the ROM tree.

### romm

Serves `/api/platforms`, `/api/roms`, `/api/roms/{id}`, `/api/roms/{id}/content/<file>` (the file from disk), cover images under `/assets/romm/resources/`, and device linking at `/api/auth/device/init` and `/api/auth/device/token`. Links are approved automatically, and approval creates a token.

Control: `platforms`, `roms`, `links`, `links/approve` and `links/deny` (`{"user_code"}`), `token` (GET, PUT, DELETE), `reload`.
CLI: `platforms`, `roms`, `links [approve|deny [user-code]]`, `token [set <token> | clear]`, `reload`.

### logs

Accepts a multipart `POST /` with a `file` field and serves the upload at `/<id>.log`. It keeps the last 100 uploads.

Control: `uploads` (GET, DELETE), `uploads/{id}`. CLI: `uploads [get <id> | clear]`.

### online

A contract fake of Zaparoo Online's device API:

- discovery (`/.well-known/...`)
- device link requests and polling (`/v1/device-link-requests...`)
- claim redeem (`/v1/device-claims/redeem`)
- images (`/images/{name}`)
- with device auth: heartbeat, `me` (GET, DELETE), backups (premium only), backup objects and packs, the play-session watermark and upload, library resolve, inventory and state, decks, saved decks, `remote-sessions/wait`, and remote operation accept and result.

`remote-sessions/wait` only delivers `operation_target` envelopes. `library_changed` is not faked.

Faults have the shape `{"pathPrefix", "kind"}`. The prefix must start with `/`, and the longest matching prefix wins. Kinds:

| Kind | Effect |
| --- | --- |
| `401`, `403`, `429`, `500`, `503` | That HTTP status. |
| `timeout` | Holds the request until the client gives up. |
| `slow` | Holds the request 2s, then serves it. |

State is saved to `<root>/online/state.json` under `serve`, and kept in memory under the harness.

CLI:

```
link-requests
link-approve [id]
link-deny [id]
account [set [--premium] [--limited]]
devices
op enqueue <kind> [json-params]
ops
backups ls
fault set <path-prefix> <401|403|429|500|503|timeout|slow>
fault clear [path-prefix]
faults
requests [clear]
```

Control routes: `GET link-requests`, `POST link-requests/approve` and `link-requests/deny` (`{"id"}`), `GET|POST account`, `GET|POST devices`, `GET|POST ops`, `GET backups`, `GET|POST|DELETE faults` (`?prefix=`), `requests`.

Claiming or unlinking a device calls `ResetOnlineConsent`, which turns off remote control, remote backup, playtime sync and library sync. Turn them on again after a claim if the test needs them.

## In-process harness and testscript

### Go

```go
in := harness.New(t, &harness.Options{
    Fixtures: &simfixtures.Options{Systems: []string{"SNES"}, PerSystem: 5},
    Index:    true,
})
in.Reader("")              // first boot reader, a *simreader.Device
in.API.Call(ctx, "media.active", nil)
n, next, err := in.Events.Wait(ctx, 0, "media.started", "systemId=SNES")
```

`New` and `Start` take `*harness.Options`. `New` fails the test on error and closes the instance at cleanup. `Start(ctx, &opts)` returns an instance the caller must `Close`. Only one Core can run in a process, so instances are serialized: `New` and `Start` block until the previous instance closes.

| Option | Meaning |
| --- | --- |
| `Fixtures` | Generate a ROM tree before boot (its `Root` is filled in). |
| `Values` | Adjust the first-boot config values. |
| `Config` | Adjust the loaded config before the service starts. It is saved afterwards. |
| `Root` | Sim root. Empty makes a short temp dir that `Close` removes; a given root is kept. |
| `Readers` | Boot readers. Nil means `sim0`; an empty slice means none. |
| `Fakes` | Fakes to start, by name. |
| `Index` | Run a real `media.generate` after boot. |
| `NoWaitReaders` | Don't wait for Core to open the boot readers. |

| Instance member | What it is |
| --- | --- |
| `Platform` | The `*sim.Platform`. `Platform.Hub()` has the reader devices. |
| `Config` | The live `*config.Instance`. |
| `API` | `*sim.API` over a Unix socket at `<root>/tmp/api.sock`, with a random API key. |
| `Control` | `*sim.Client` that calls the control handler in-process (same routes as `sim.sock`). |
| `Fakes` | The running fakes. |
| `Audio` | A `Recorder`. `Played()` lists sounds: `success`, `fail`, `limit`, `pending`, `ready`, `bytes` (other), or `file:<path>`. `Reset()` clears it. |
| `Fixtures` | The fixtures report. |
| `Events` | Every API notification since boot: `Len()`, `All(since)`, `Wait(ctx, since, method, matches...)`. |
| `Layout`, `Root` | Paths. |

Other helpers: `WaitReaders(ctx, names...)`, `Reader(name)`, `Close()` (safe to call twice), and `harness.Decode[T](notification)`. `StartTimeout` (2m) bounds boot, including fixtures and indexing.

### testscript

Scenarios live in `pkg/platforms/sim/testdata/scripts/*.txtar` and run from `TestScripts` in `script_test.go` (part of `task test`). To run one:

```sh
go test -tags validator_novalidatefn,expr_static_methods ./pkg/platforms/sim -run TestScripts/lifecycles
```

Use `harness.ScriptSetup` and `harness.ScriptCommands()` to add the commands to your own testscript runner.

| Command | Notes |
| --- | --- |
| `sim-start [flags]` | `-fakes a,b`, `-readers r1,r2` (`none` for no readers), `-scan-mode tap\|hold`, `-exit-delay SECONDS`, `-launch-guard`, `-guard-confirm`, `-disc-order ORDER` and `-audio-cd-launcher ID` (the optical drive's `launch_order` and `audio_cd_launcher`). Sets `$ROOT` and `$ROMS`. |
| `sim-stop` | Close the instance (also done when the script ends). |
| `rom [-size N] PATH...` | Create files under `$ROMS`: empty, or N zero bytes. |
| `fixtures [flags]` | Same flags as `fixtures gen` with one dash. Prints `files N media M`. |
| `index [SYSTEM,...]` | Run `media.generate` and wait. Prints `media N`. |
| `idle` | Wait until no index, optimization or scrape runs. |
| `tap [-reader R] [-uid U] [-data HEX] [-type T] [-prop NAME=VALUE]... [-for MS] TEXT...` | The text is the remaining arguments joined by spaces, so `$ROMS/a b.sfc` works unquoted. `-for` removes the tag after MS; without it the tag stays. `-prop` adds a scan property and repeats, so `-type disc -prop disc.kind=data -prop disc.system=PSX -prop disc.drive=sr0` inserts a disc the way the optical drive reports one. |
| `remove [-reader R]` | Take the tag off. |
| `api [-bg] METHOD [JSON...]` | Result as indented JSON on stdout. `! api` expects an error and prints it on stderr. `-bg` runs a blocking call such as `readers.write` in the background. |
| `api-wait` | Wait for the `-bg` call and report it like `api`. |
| `notification [-timeout D] METHOD [K=V...]` | Wait for a notification after the cursor and move the cursor past it. `! notification` asserts none arrives within 750ms. |
| `event [-peek] [-timeout D] KIND [K=V...]` | The same for journal events. `-peek` leaves the cursor in place, for events whose order is not fixed. |
| `played PATH...` | Wait until play history's newest entry is the path the arguments join to. History is written after `media.started`, so wait with this before a check that reads last played. |
| `mark` | Move both cursors to now. |
| `fault JSON` / `fault clear [LAUNCHER]` | JSON uses the camelCase fault fields. Unknown fields are an error. |
| `write-complete [-reader R] [UID]` | Present a tag to the pending write. |
| `write-fail [-reader R] MODE` | Make the next write fail. |
| `sounds [-wait NAME] [-timeout D]` | Print the sounds since the last `sounds`, one per line, and reset. A sound can play after the notification you waited for, so `-wait` first waits until that sound has played. |
| `status` | Print the control status as JSON. |

Only `api`, `api-wait`, `notification` and `event` accept `!`. `notification` and `event` time out after 15s unless `-timeout` is given.

Existing scenarios: `faults`, `launch_guard`, `launch_random_hold`, `library`, `lifecycles`, `preemption`, `scrape`, `tap_launch_remove`, `write_card`.

```
sim-start -scan-mode hold
rom 'snes/Super Game.sfc'

tap -uid 04aa11 $ROMS/snes/Super Game.sfc
notification tokens.added uid=04aa11
event launch launcher=SimSNES
notification media.started systemId=SNES mediaName='Super Game'
sounds
stdout '^success$'

remove
event stop intent=menu
notification media.stopped systemId=SNES
```

## Using zaparoo-cli against the sim

The API listens on loopback TCP. Core trusts loopback peers without an API key, so `zaparoo-cli` needs no pairing. Point it at the port from `status` or `sim.json`, and give it its own config and credentials files so it doesn't touch your saved devices:

```sh
P=$(jq -r .apiPort "$ZAPAROO_SIM_ROOT/sim.json")
T=$(mktemp -d)
Z="zaparoo-cli --device 127.0.0.1:$P --config $T/config.toml --credentials-path $T/creds.json"

$Z devices ping
$Z --json media active
$Z readers list
$Z media control save_state --yes     # journals: control detail="save_state"
$Z stop --yes                         # journals: stop intent=menu
```

Check the effect of each command with `zaparoo-sim launches` or `journal`. The harness API is different: it uses a Unix socket, and Core always requires a key on its Unix listener.

## Fidelity limits

The sim does not cover:

- Real launchers, emulators, platform scanners, Steam or browsers. Children are `sleep`, and launch arguments are not checked.
- A display or real input. Screenshots are a generated image, and keyboard and gamepad input is only journaled.
- Real reader hardware (PN532, ACR122U and the rest), USB hotplug and timing.
- Audio playback. Sounds are silent, or recorded by name in the harness.
- mDNS discovery, BLE, Bluetooth, the internet check, updates and power actions.
- Real Online, RomM, libretro or log upload services. The fakes follow the contracts Core uses, not every server behaviour. Online's `library_changed` remote envelope is not faked.
- Platform-specific behaviour (MiSTer cores, Batocera, Windows). Use the MiSTer VM or hardware for those.
- Tracked-child crashes. When a Tracked child crashes, Core keeps its active media and tracked PID until something stops or replaces it. That is Core's behaviour, and the sim shows it faithfully.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| `refusing to use ... as a sim root` or `refusing sim root ...` | The root is `/`, your home directory, overlaps the real Core directories, or is a non-empty directory without `.zaparoo-sim`. Use a fresh `mktemp -d`. |
| `socket path ... use a shorter --root` | Unix socket paths are limited to 107 bytes. Use a root under `/tmp`. |
| `a sim is already running from ...` | The PID in `sim.json` is alive. Run `stop`, or use another root. |
| `serve` times out waiting for ready | Read `<root>/logs/sim-serve.log` and `<root>/logs/core.log`. |
| Reader shows `plugged, not open` | Core has not detected it yet. Auto-detect takes a moment after boot. A device with `--no-auto-detect` needs a `[[readers.connect]]` entry with `driver = "sim"`. |
| `wait` times out though the event happened | `wait` only sees notifications sent after it subscribes. Start it before the action (in the background), or check the journal instead. |
| A fault seems to do nothing | Commands return before Core acts. A `fault clear` straight after a `tap` can land before the launch. Follow the journal (`journal --follow --timeout 3s`) before clearing. |
| A write completes at once | A tag is still on the reader (`write-complete` and `hold` leave it there). Run `remove` first. |
| Index count is 10 higher than the ROM count | The `SimScanner` and `SimScheme` virtual entries. |
| Stray `sleep` processes | Only if the sim was killed with SIGKILL. Find them with `pgrep -ax sleep` and kill them by PID; never `pkill -f`, which matches your own shell. |

