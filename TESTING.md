# Testing Zaparoo Core

How to pick a testing layer and use it. Helper reference: [`pkg/testing/README.md`](pkg/testing/README.md). Simulator reference: [`docs/sim.md`](docs/sim.md).

## Never test with the real binary

Do not run the Linux `zaparoo` binary on a developer machine to try a change. It discovers the real emulators, Steam and the browser, indexes the real ROM folders, opens real readers, plays sounds, and shares port 7497 and the XDG data and config directories (`~/.local/share/zaparoo`, `~/.config/zaparoo`) with any Core the user runs. Use the sim layers below instead.

## Choosing a layer

| Layer | Use it for | Covers | Does not cover |
| --- | --- | --- | --- |
| Unit tests: `pkg/testing/mocks`, `fixtures`, `helpers`, `sqlmock`, afero, clockwork | One function or package | Logic behind a mocked platform, reader, database, filesystem or clock | Anything across the service boundary |
| In-process harness: `pkg/testing/harness` | A Go test that needs the whole service | Full Core service on the sim platform inside `go test`: API, reader pipeline, ZapScript, launch lifecycle, databases, offline fakes | The sim gaps below |
| testscript scenarios: `pkg/platforms/sim/testdata/scripts/*.txtar` | Regression tests for behavior (preferred) | Same as the harness; run by `TestScripts` in `task test` | The sim gaps below |
| `zaparoo-sim` binary: `cmd/sim` | Exploring by hand, agent sessions, `zaparoo-cli` against an isolated Core | Same as the harness, as a long-running process under one throwaway root | The sim gaps below |
| MiSTer VM rig: `scripts/mister-vm` | MiSTer launch, MGL, reader, restart and persistence behavior | Real ARM build in a headless MiSTer VM | FPGA cores, HDMI, physical readers, a real ROM corpus |
| Real hardware | Anything the layers above can't reach | Everything | Repeatability and speed |

The sim (harness, scenarios and binary) runs the real service with simulated readers, sim launchers that start an invisible `sleep` child, silent audio and offline network fakes (`libretro`, `romm`, `logs`, `online`). It has no real launchers or emulators, platform scanners, FPGA cores, screens or input effects, USB/NFC hardware, or updates. There is no fake update server on purpose: update URLs and keys can't be overridden, to protect OTA signature checks. Update flows are covered only by `pkg/service/updater` tests.

The harness and sim platform build only on Linux (`linux && !android`), so scenarios and harness tests don't run elsewhere.

For the sim workflow and the Core behavior to assert on, read [`.claude/skills/core-sim/SKILL.md`](.claude/skills/core-sim/SKILL.md). For the MiSTer rig, read [`.claude/skills/mister-vm/SKILL.md`](.claude/skills/mister-vm/SKILL.md) and [`scripts/mister-vm/README.md`](scripts/mister-vm/README.md).

## Commands

Tests need the build tags `validator_novalidatefn,expr_static_methods`. `task test` adds them; pass them yourself when you call `go test` directly.

| Task | Command |
| --- | --- |
| All tests with race detection (also runs the `mister/` module tests) | `task test` |
| One package tree | `task test -- ./pkg/service/...` |
| One test | `go test -race -tags=validator_novalidatefn,expr_static_methods ./pkg/api/ -run TestName` |
| One scenario | `go test -race -count=1 -tags=validator_novalidatefn,expr_static_methods ./pkg/platforms/sim/ -v -run TestScripts/<file-name>` |
| All fuzz targets (30s each) | `task fuzz`, or `FUZZ_TIME=2m task fuzz` |
| One fuzz target | `go test -run '^$' -fuzz=FuzzParseVirtualPathStr -fuzztime=30s ./pkg/helpers/virtualpath/` |
| Benchmarks | `task bench`, `task bench-db`; `task bench-baseline`, then `task bench-compare` |
| Lock-order checks (go-deadlock) | `task deadlock` |
| Build and serve the sim | `task sim:build` (writes `_build/sim/zaparoo-sim`); `task sim:run` (foreground, root `_build/sim/root` unless `ROOT=...`) |

Use `-v`: without it a passing run, and a `-run` name that matches nothing, both print only `ok`. Output is noisy, so send it to a file and grep for `--- PASS`, `--- FAIL` and `>`.

## Unit tests

Mock hardware, network, process and platform boundaries. Tests must not need a physical reader or device. Reuse `pkg/testing/` instead of building parallel infrastructure; `pkg/testing/examples/` has working tests for each pattern.

### Platform mock

`SetupBasicMock` registers `mocks.BasicMockSettings()`: data, config, temp and log dirs in one temporary directory shared by the whole test process. testify uses the first matching expectation, so register your own `Settings` first when a test writes files and needs isolation.

```go
pl := mocks.NewMockPlatform()
pl.On("Settings").Return(platforms.Settings{
	DataDir:   t.TempDir(),
	ConfigDir: t.TempDir(),
	TempDir:   t.TempDir(),
	LogDir:    t.TempDir(),
})
pl.SetupBasicMock() // ID "mock-platform", no readers, launchers or scrapers
```

`GetLaunchedMedia`, `GetLaunchedSystems`, `GetKeyboardPresses` and `GetGamepadPresses` return what the code under test did.

### Reader mock

Readers send scans on the channel passed to `Open`. `MockReader` simulates that:

```go
r := mocks.NewMockReader()
r.SetupBasicMock()
scans := make(chan readers.Scan, 1)
tok := fixtures.NewNFCToken()
r.SimulateTokenScan(scans, tok, "mock://test")
scan := <-scans
require.Equal(t, tok.UID, scan.Token.UID)
```

`SimulateError` sends an error scan. Set `On("Write", ...)` or `On("WriteTarget", ...)` for write paths.

### Databases

Use a testify mock for call expectations, or a real SQLite database for behavior.

```go
// Mock
userDB := helpers.NewMockUserDBI()
userDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
db := &database.Database{UserDB: userDB, MediaDB: helpers.NewMockMediaDBI()}

// Real, in memory
db, cleanup := helpers.NewTestDatabase(t) // or NewInMemoryUserDB(t), NewInMemoryMediaDB(t)
defer cleanup()
```

`HistoryEntryMatcher` needs a non-zero `Time` and a `TokenID`; `fixtures.NewHistoryEntry` builds one. To fill a MediaDB through the production scan pipeline, use `scantest.IndexMediaPaths(t, db, systemID, paths...)`. For raw SQL, `testsqlmock.NewSQLMock()` (package `pkg/testing/sqlmock`) returns a `*sql.DB` and a regex-matching `sqlmock.Sqlmock`.

### Filesystem (afero)

Production code that touches files takes an `afero.Fs`. Tests pass an in-memory one:

```go
fs := helpers.NewMemoryFS() // fs.Fs is an afero.Fs
require.NoError(t, fs.WriteFile("/roms/snes/game.sfc", []byte("x"), 0o600))
n, err := countFiles(fs.Fs, "/roms/snes") // code under test
```

Use `filepath.Join` for paths in tests too.

### Time (clockwork)

Inject a `clockwork.Clock` (`clockwork.NewRealClock()` in production) and drive a fake in tests instead of sleeping:

```go
clock := clockwork.NewFakeClock()
go worker(clock) // code under test; waits on clock.After(time.Minute)
require.NoError(t, clock.BlockUntilContext(ctx, 1)) // worker is waiting
clock.Advance(time.Minute)
```

More in [`pkg/testing/TIME_TESTING.md`](pkg/testing/TIME_TESTING.md). Fuzz and property tests: [`FUZZ_TESTING.md`](pkg/testing/FUZZ_TESTING.md), [`PROPERTY_TESTING.md`](pkg/testing/PROPERTY_TESTING.md).

### API

Tests must never listen on 7497. Listen on loopback and pass port 0 for an OS-assigned port:

```go
cfg, err := helpers.NewTestConfigWithListenAndPort(helpers.NewMemoryFS(), t.TempDir(), "127.0.0.1", 0)
```

When a test needs the port before the server starts, bind `127.0.0.1:0` with `net.ListenConfig`, read the port from `listener.Addr()` and pass it in (see `pkg/api/server_startup_test.go`). For a handler-level WebSocket test, use `helpers.NewWebSocketTestServer(t, handler)`, `CreateWebSocketClient()` and `helpers.SendJSONRPCRequest(conn, method, params)` with `AssertJSONRPCSuccess` or `AssertJSONRPCError`. To test real API methods end to end, use the harness.

## Harness test

`harness.New` boots a sim Core for the test and closes it at cleanup. Instances in one process run one at a time.

```go
func TestTapLaunches(t *testing.T) {
	in := harness.New(t, &harness.Options{}) // one sim reader, "sim0"

	rom := filepath.Join(in.Layout.ROMs, "snes", "Alpha.sfc")
	require.NoError(t, os.MkdirAll(filepath.Dir(rom), 0o750))
	require.NoError(t, os.WriteFile(rom, nil, 0o600))

	dev, err := in.Reader("") // "" is the first reader
	require.NoError(t, err)
	since := in.Events.Len()
	_, err = dev.Tap(simreader.Tag{UID: "04aa11", Text: rom}, 0) // pkg/readers/sim
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err = in.Events.Wait(ctx, since, "media.started", "systemId=SNES")
	require.NoError(t, err)

	_, err = in.API.Call(ctx, "media.active", nil)
	require.NoError(t, err)
}
```

`Options` also takes `Fixtures` (a `simfixtures.Options` ROM tree), `Fakes` (fakeset names), `Index`, the `Values` and `Config` hooks, `Readers` and `Root`. `Instance` exposes `Platform`, `Config`, `API`, `Control`, `Fakes`, `Audio`, `Events` and `Layout`.

## testscript scenario

Prefer a scenario for a behavior regression: it's short, deterministic and runs in `task test`. Add a `.txtar` file to `pkg/platforms/sim/testdata/scripts/`, copying the closest existing one.

```txtar
# Tapping a ROM launches it; removing the token in hold mode stops it.
sim-start -scan-mode hold
rom snes/Alpha.sfc

tap -uid 04aa11 $ROMS/snes/Alpha.sfc
notification media.started systemId=SNES
event tracked launcher=SimSNES

remove
notification media.stopped systemId=SNES
api media.active
! stdout systemId
```

The commands are defined in `pkg/testing/harness/script.go` and listed in the core-sim skill.

## The zaparoo-sim binary

Use it to explore by hand or to drive Core from an agent or `zaparoo-cli`. Every path lives under one root, and the API listens on loopback only and refuses port 7497. Start with the quick start in [`.claude/skills/core-sim/SKILL.md`](.claude/skills/core-sim/SKILL.md); the full command reference is [`docs/sim.md`](docs/sim.md).
