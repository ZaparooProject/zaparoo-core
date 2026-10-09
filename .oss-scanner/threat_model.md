# Threat model

Guidance for Anthropic's OSS Scanner. `docs/ARCHITECTURE.md` and `docs/api/` describe the
system in more detail and are part of this checkout.

## What this project does and where untrusted input enters

Zaparoo Core is a Go daemon that turns a scanned physical token (NFC tag, barcode, QR code,
optical disc) into an action on the machine it runs on, usually launching a game. Tokens carry
ZapScript, a small command language. Core runs on desktops and on appliance-style retro gaming
devices (MiSTer FPGA, Batocera, SteamOS and others). On the appliance platforms it runs as
root; everywhere it starts at boot and is reachable by anything on the home network. It also
updates itself.

Untrusted input enters through:

- **The network API.** JSON-RPC 2.0 over WebSocket, HTTP POST and SSE on TCP port 7497, bound to
  all interfaces by default and advertised over mDNS. Also the pairing endpoints
  (`/api/pair/start`, `/api/pair/finish`), the GET run endpoints (`/r/*`, `/run/*`, `/l/*`) and
  the embedded web app at `/app/`. Any host on the LAN is an attacker, and so is any web page
  open in a browser on the LAN (cross-origin and DNS rebinding requests).
- **Tokens.** Tag UIDs, NDEF records, barcode text, serial reader lines and MQTT messages. An
  attacker can hand someone a tag or publish to a topic the device subscribes to. Token text is
  parsed as ZapScript and executed.
- **Remote ZapScript.** A token can name a URL (a ZapLink) whose body is fetched and run. The
  server behind it is untrusted. So are the playlists and decks it returns.
- **Update metadata and archives.** The signed manifest, checksum file and release archives the
  updater downloads and extracts.
- **Files on indexed media.** Filenames, gamelist XML, MiSTer MRA files, playlists and scraped
  metadata on storage the user did not necessarily author (shared ROM sets, NAS mounts).
- **Config-adjacent files** a remote client can influence through the API: mappings, launchers,
  settings writes, backups and restores.

## Components that matter most / least

Most important:

- `pkg/api/` and `pkg/api/middleware/`: API key auth, locality checks (`IsLocalRequest`,
  `IsTrustedLoopback`), IP filter, rate limits, origin validation, PAKE pairing, AES-GCM session
  encryption, and the role and capability checks in `pkg/api/permissions/`.
- `pkg/zapscript/`: command dispatch, the `Unsafe` flag that marks script from an untrusted
  source, the `allow_execute`, `allow_run` and `allow_http` allowlists, `**execute`, `**input.*`,
  HTTP commands, ZapLink fetching, playlist and deck trust, and credential redaction
  (`RedactScript`).
- `pkg/readers/`, especially `pkg/readers/shared/ndef/`, `rs232barcode`, `simpleserial` and `mqtt`.
- `pkg/service/updater/` and `pkg/service/updater/otameta/`: ed25519 signature verification,
  the monotonic manifest generation, asset digests, archive extraction, rollback and watchdog.
- `pkg/service/profiles/` and playtime limits: PINs, switch IDs (bearer credentials) and the
  parental limits they protect.
- `pkg/service/backup/`, database migrations, and config loading in `pkg/config/` (including
  `auth.toml` credential handling).
- Launcher path handling in `pkg/platforms/` and `pkg/helpers/`: anything that turns token or API
  text into a file path or a process argument.

In scope, lower priority: media indexing and scraping (`pkg/database/mediascanner/`, scrapers),
the TUI, and the platform packages for systems other than Linux and MiSTer.

Out of scope:

- Third-party code: libnfc, libusb, PC/SC, and Go dependencies. Report a defect only where Core
  uses a dependency unsafely.
- The Zaparoo App web UI. It lives in another repository and is not embedded in this image
  (`pkg/assets/_app/packed/` holds a placeholder).
- `scripts/`, `.github/`, `pkg/testing/`, test files and benchmarks, except where a workflow or
  script could leak or misuse the update signing key.
- `mister/` is in scope as a library, at the same priority as the other platform packages.

## Trust boundaries worth knowing

- **Local is trusted.** A request from loopback (or a Unix socket peer holding a key) has full
  authority. Someone with a shell, or write access to the config directory, already owns the
  device. That is not a finding.
- **Remote depends on platform and config.** With `service.encryption` on (the default on Linux
  and SteamOS), a remote WebSocket client must be paired; paired clients are `admin` or `member`.
  Configured API keys grant admin. A set of released appliance platforms (see
  `legacyCapabilities` in `pkg/api/permissions/permissions.go`) deliberately still admit
  unauthenticated LAN clients with a fixed, reduced capability set. That grant is a known
  compatibility decision; a way to exceed it is a finding.
- **A token launching media is the product.** A scanned tag or an API `run` call starting a game,
  running a ZapScript command, or fetching a ZapLink is intended. What must hold: script marked
  `Unsafe` (API callers that ask for it, remote ZapLink bodies, decks the user does not own)
  cannot run `**execute` or the restricted input commands, and never regains trust by nesting
  through a playlist, deck or link. Script from any non-config source cannot execute a program
  that `allow_execute` does not match.
- **Updates fail closed.** A device must never install an archive that is unsigned, mismatches
  its digest, comes from a manifest older than one it has seen, or is rejected by the rollout
  gate.

## How to exercise it

The image is built by `.oss-scanner/Dockerfile`. Source is at `/src`, with the Go module cache
and build cache already populated, the Go toolchain pinned to the release `go.mod` names,
and `GOPROXY=off` set.

- Service binary: `/src/_build/linux_amd64/zaparoo` (unstripped). The Linux build refuses to
  start as root, so the image has a `zaparoo` user. Run it against a throwaway home:
  `runuser -u zaparoo -- env XDG_DATA_HOME=/tmp/z/data XDG_CONFIG_HOME=/tmp/z/config XDG_CACHE_HOME=/tmp/z/cache /src/_build/linux_amd64/zaparoo -daemon`
  The API is then at `http://127.0.0.1:7497/api/v0.1` and `/health` answers 200. Loopback is
  trusted, so to exercise remote-client rules drive the handlers through the tests in
  `pkg/api/` rather than through this listener.
- Tests: `go test -race -tags=validator_novalidatefn,expr_static_methods ./...`. The `mister/`
  module is separate: `cd mister && go test ./...`.
- Fuzz targets: the `fuzz` task in `Taskfile.dist.yml` lists every target and its package. Run
  one with `go test -run '^$' -fuzz=FuzzParseToText -fuzztime=60s ./pkg/readers/shared/ndef`, or
  all with `task fuzz`.
- No physical reader, FPGA or game launcher is present. Readers and platforms are mocked under
  `pkg/testing/`; use those rather than assuming hardware.

## How we rate severity

- **Critical**: a remote attacker with no credentials, or a scanned token alone, executes
  arbitrary commands or code; or a device installs update code that is unsigned, tampered with,
  or rolled back to an older manifest generation.
- **High**: bypass of API authentication, pairing or session encryption; a `member`, legacy or
  unpaired client gaining a capability it is not granted; `Unsafe` script reaching `**execute`
  or restricted input; execution of a program outside `allow_execute`; a path traversal that
  writes or deletes outside Core's own directories (archive extraction, backup restore, API file
  parameters); recovery of an API key, pairing key, profile PIN or switch ID by a remote client;
  a web page in a LAN browser driving the API cross-origin.
- **Medium**: a remote or token-borne crash, hang or unbounded memory or disk growth of the
  daemon; reading arbitrary local files through the API; credentials written to logs, history
  or notifications; bypass of profile PINs or playtime limits by a paired member client;
  server-side request forgery beyond what a ZapScript HTTP command is meant to do.
- **Low**: hardening gaps with no demonstrated impact; issues that need physical access to the
  device plus a configuration the owner chose; crashes that need a malformed file on local
  storage the attacker would already have to write.

Memory-safety bugs are unlikely in the Go code; where cgo or `unsafe` is involved and input is
attacker-controlled, rate a demonstrated out-of-bounds read or write as high or above.

## Reports and patches

- One report per root cause. Name the entry point, the attacker's position (unauthenticated LAN,
  paired member, token only, malicious ZapLink server) and the platform or config it needs.
- A reproducer as a Go test or a short script against the running daemon is most useful.
- Keep patches minimal and include a regression test that fails before the fix. Follow
  `AGENTS.md`: `syncutil` mutexes, zerolog, no new dependencies, no edits to applied database
  migrations.

## Anything to leave alone

- Unauthenticated access from loopback, and the reduced legacy grant on appliance platforms
  described above.
- Plain HTTP on the LAN as such. Transport confidentiality comes from the paired WebSocket
  session, not TLS.
- `**execute` and custom launchers running what the device owner put in their own config.
- Panics reachable only from test helpers or from `scripts/`.
- Missing rate limits on loopback and Unix socket peers.
- Generated files such as `pkg/database/mediadb/stat1_seed_data.go`.
