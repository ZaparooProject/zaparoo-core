---
name: core-sim
description: Run and test Zaparoo Core on the desktop without hardware. Use for any Core behavior check that is not platform-specific (tap and remove tokens, launch lifecycles, preemption, hold mode, launch guard, writing cards, indexing and search, scraping, faults, the JSON-RPC API, Online flows) through the in-process harness, testscript scenarios or the isolated zaparoo-sim binary. For MiSTer-specific behavior use the mister-vm skill; for real launchers, emulators, screens or readers use real hardware.
license: GPL-3.0-or-later
---

# Core simulation

Repository root is three directories above this skill directory. Run commands from there. The full reference is [docs/sim.md](../../../docs/sim.md); read the section you need before using a command you haven't used yet.

## Never run the real binary on the desktop

Do not test by running `zaparoo` (the Linux build) on a developer machine. It finds the real RetroArch, flatpak emulators, Steam and the browser, indexes the real ROM folders, opens real readers, plays sounds and shares port 7497 and `~/.local/share/zaparoo` / `~/.config/zaparoo` with any Core the user is running. Use one of the three layers below. Each runs the real service, API, reader pipeline, ZapScript, launch lifecycle and databases on the sim platform:

- Every path lives under one root, and a root that overlaps the real Zaparoo directories or the home directory is refused.
- The API listens on loopback, and port 7497 is refused.
- Discovery, Bluetooth, update checks, internet checks and error reporting are off.
- Real reader drivers never autodetect.
- Audio is silent.
- Launchers start an invisible `sleep` child instead of an emulator.
- Network services are offline fakes, or unreachable when no fake runs.

## Pick a layer

| Need | Use |
| --- | --- |
| A regression test for a behavior | testscript scenario in `pkg/platforms/sim/testdata/scripts/` |
| A Go test that needs the whole service | `harness.New(t, &harness.Options{...})` |
| Exploring by hand, an agent session, `zaparoo-cli` | `zaparoo-sim` binary |
| A single function or package | Ordinary unit tests with `pkg/testing/mocks` (see `TESTING.md`) |
| MiSTer launch, MGL, MiSTer restart | `mister-vm` skill |
| Real emulators, screens, FPGA, USB/NFC readers, updates | Real hardware |

Prefer a scenario: it is short, deterministic, runs in `task test` and leaves nothing behind.

## Scenarios (testscript)

```txtar
# Tapping a ROM launches it; removing the token in hold mode stops it.
sim-start -scan-mode hold
rom snes/Alpha.sfc
tap -uid 01 $ROMS/snes/Alpha.sfc
notification media.started systemId=SNES
event tracked launcher=SimSNES
remove
notification media.stopped systemId=SNES
```

Run one with `go test -race -count=1 -tags=validator_novalidatefn,expr_static_methods ./pkg/platforms/sim/ -run TestScripts/<file-name>`. Add `-v`: without it a passing run, and a `-run` name that matches nothing, both print only `ok`. Output is noisy, so send it to a file and grep for `--- PASS`, `--- FAIL` and `>`.

Commands, from `pkg/testing/harness/script.go`:

- **Instance:** `sim-start` (`-fakes`, `-readers`, `-scan-mode`, `-exit-delay`, `-launch-guard`, `-guard-confirm`) sets `$ROOT` and `$ROMS`. `sim-stop` stops it and `status` prints its state.
- **Library:** `rom PATH...`; `fixtures [gen flags]`, which prints `files N media M`; `index [SYS,...]`, which prints `media N`; and `idle`, which waits until no indexing or scraping runs.
- **Tokens:** `tap [-reader] [-uid] [-data] [-for MS] TEXT...` and `remove`.
- **Writing cards:** `write-complete [-reader] [UID]` and `write-fail MODE`.
- **Core API:** `api [-bg] METHOD [JSON]` and `api-wait`.
- **Waiting and checking:**
  - `notification [-timeout] METHOD [k=v...]` waits for an API notification.
  - `event [-peek] [-timeout] KIND [k=v...]` waits for a sim journal event; `-peek` doesn't move the cursor.
  - `played PATH...` waits until play history's newest entry is that path; history is written after `media.started`.
  - `mark` moves both cursors to now.
  - `sounds [-wait NAME]` prints the sounds played and clears the list; `-wait` first waits for that sound, which can play after the notification you waited for.
- **Faults:** `fault JSON` and `fault clear [ID]`.

`api`, `api-wait`, `notification` and `event` accept a leading `!` to assert the opposite. `wait` is a testscript builtin, which is why the notification command is named `notification`. `$VAR` does not expand inside single quotes. The existing scripts cover tap/launch/remove, preemption, launch guard, writing cards, lifecycles, the library, faults and scraping; copy the closest one.

## The zaparoo-sim binary

```sh
task sim:build                                   # _build/sim/zaparoo-sim
export ZAPAROO_SIM_ROOT=$(mktemp -d /tmp/zsim.XXXX)
_build/sim/zaparoo-sim serve --fakes libretro,romm   # detaches; prints api URL and socket
_build/sim/zaparoo-sim fixtures gen --systems snes,nes --per-system 20 --realistic
_build/sim/zaparoo-sim fixtures index
_build/sim/zaparoo-sim api media.search '{"query":"mario"}'
_build/sim/zaparoo-sim api settings.update '{"readersScanMode":"hold"}'   # default is tap: remove does nothing
_build/sim/zaparoo-sim hold "$ZAPAROO_SIM_ROOT/roms/snes/<file>"
_build/sim/zaparoo-sim launches                  # launch, tracked PID, stop intents
_build/sim/zaparoo-sim wait media.stopped --timeout 10s & sleep 0.5   # subscribe first
_build/sim/zaparoo-sim remove; wait              # shell wait: let the background zaparoo-sim wait finish
_build/sim/zaparoo-sim stop && rm -rf "$ZAPAROO_SIM_ROOT"
```

`wait` misses a notification sent before it subscribed, so start it in the background before the action, or read `launches`/`journal` afterwards: the journal keeps every event with a sequence number. Settings go through the API, for example `api settings.update '{"readersScanMode":"hold","launchGuardEnabled":true}'`; the sim saves them to its own `config.toml` and the fake URLs survive. `media.scrape` needs a `scraperId` (`libretro-thumbnails`, `romm`, `gamedatabase`) and answers "scraping already in progress" until the scrape that indexing starts has finished.

Add `--json` before the command for machine-readable output. `status` shows readers, active media and children. `journal --kind K --follow` streams events. `fault set --launcher SimSNES --crash-after-ms 200` injects faults. `fake online link-approve` drives a fake. `zaparoo-cli` works against the port in `<root>/sim.json`; docs/sim.md describes how it authenticates.

## How Core behaves (so a scenario asserts the right thing)

- Core sends `stop intent=preemption` with no launcher before every launch. On preemption the old child's `process-exit` and the new launcher's `launch` race; check one with `event -peek`, which leaves the cursor in place.
- In hold mode, removing the token stops media with intent `menu`.
- If a Tracked child crashes, its media stays active: Core keeps the handle only to stop it. A Blocking child's exit clears the media. Use `SimBlocking` to test "media ends by itself".
- Indexing scrapes the sources it finds right away. Run `idle` before starting a scrape of your own, or the API answers "scraping already in progress". A scrape that isn't forced skips media that was already scraped.
- ZapScript that reads the media database (`launch.random`, `launch.search`, titles) needs an `index` first. Quote tag text in the shell (`'**launch.random:snes'`); testscript needs no quotes.
- `launches` is a filtered view of the journal (launch, process, stop and error events), so its sequence numbers have gaps. `journal` shows everything.
- The behavior launchers add virtual media, so `index` reports more media than there are fixture files.

## Traps

- Never point `--root` at real data. A root must be empty, new, or an earlier sim root (it has a `.zaparoo-sim` marker).
- Never use `pkill -f`: it matches your own shell. `pgrep -af zaparoo-sim` matches your own shell too. Find the sim with `pgrep -ax zaparoo-sim` (or read `pid` from `<root>/sim.json`), then `kill PID`.
- A stale `sim.json` after a crash is harmless. `serve` checks whether its PID is still alive.
- Read the sim databases with the WAL in mind: copy `*.db`, `*.db-wal` and `*.db-shm` together, or use the API.
- There is no fake update server, on purpose: update URLs and keys can't be overridden, to protect OTA signature checks. Update flows are covered only by `pkg/service/updater` tests.
- Logs are in `<root>/logs`. `sim-serve.log` holds a detached serve's output.
