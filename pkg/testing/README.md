# pkg/testing

Shared test infrastructure for Zaparoo Core. Which layer to use, commands and worked examples: [`TESTING.md`](../../TESTING.md). Simulator reference: [`docs/sim.md`](../../docs/sim.md).

## Packages

| Package | What it is |
| --- | --- |
| `mocks` | testify mocks and fakes for interfaces: platform, reader, audio player, API client, command executor, Kodi client, melody WebSocket, BLE |
| `helpers` | Test utilities: in-memory and mock databases, matchers, afero filesystem, test configs, WebSocket/HTTP API helpers, ES API and Kodi test servers, socket paths |
| `fixtures` | Sample data: tokens, media, playlists, history, mappings, systems, Kodi, Steam shortcuts |
| `sqlmock` | go-sqlmock with regex query matching (import as `testsqlmock`) |
| `scantest` | Fills a MediaDB through the production scan pipeline; separate from `helpers` to avoid an import cycle with `mediascanner` |
| `simfixtures` | Writes a deterministic ES-DE-style ROM tree (`Generate`) and fills a large MediaDB for benchmarks (`BigDB`) |
| `harness` | Boots a full Core service on the sim platform inside `go test` (`New`, `Start`); runs testscript scenarios (`ScriptSetup`, `ScriptCommands`). Linux only |
| `harness` scenarios | testscript scenarios (`*.txtar`) live in `pkg/platforms/sim/testdata/scripts` and run from `TestScripts` there, so they count toward the sim platform's coverage |
| `fakes/fakeset` | Starts the offline fakes by name (`libretro`, `romm`, `logs`, `online`) and points a config at them; `Unreachable` is the address used when no fake runs |
| `fakes/libretro` | Offline libretro thumbnail server |
| `fakes/romm` | Offline RomM server: platforms, ROMs, artwork, downloads, device linking |
| `fakes/logupload` | Offline log paste service for "upload log" |
| `fakes/online` | Contract fake of the Zaparoo Online device API |
| `fakes/fakectl` | Request log and control-call signature shared by the fakes |
| `fakes/internal/fakehttp`, `fakes/internal/library` | Internal plumbing for the fakes (faults, listeners, control routes; reading an ES-DE ROM tree) |
| `examples` | Working example tests for each pattern |

Guides: [`TIME_TESTING.md`](TIME_TESTING.md) (clockwork), [`FUZZ_TESTING.md`](FUZZ_TESTING.md), [`PROPERTY_TESTING.md`](PROPERTY_TESTING.md) (rapid).

## mocks

- `NewMockPlatform()`, then `SetupBasicMock()`: ID `mock-platform`, no readers, launchers or scrapers, and `Settings` from `BasicMockSettings()`, which is a temporary directory shared by the whole test process. Register your own `On("Settings")` before `SetupBasicMock` for per-test directories.
- `MockPlatform` records actions: `GetLaunchedMedia()`, `GetLaunchedSystems()`, `GetKeyboardPresses()`, `GetGamepadPresses()`, `ClearHistory()`.
- `NewMockReader()`, `SetupBasicMock()`; `SimulateTokenScan(ch, token, source)` and `SimulateError(ch, err, source)` send on the `chan<- readers.Scan` a reader gets from `Open`.
- `NewMockPlayer()` (audio; `SetupNoOpMock()`), `NewMockAPIClient()` (`Setup*Response` / `Setup*Error`), `NewMockKodiClient()`, `NewMockMelody()`, `NewMockMelodySession()`.
- `CreateTestJSONRPCMessage`, `CreateTestJSONRPCResponse`, `CreateTestJSONRPCError` build raw JSON-RPC frames.
- BLE: `NewFakeAdapter(roles...)`, `NewFakePeripheral()`, `NewFakeCentral()`, `NewFakeDevice(addr)`, `NewFakeCharacteristic()`.

## helpers

Databases:

- `NewMockUserDBI()`, `NewMockMediaDBI()`: testify mocks of the database interfaces.
- `NewInMemoryUserDB(t)`, `NewInMemoryMediaDB(tb)`, `NewTestDatabase(t)`: real SQLite, each returns `(db, cleanup)`.
- `NewUserDBInDataDir(t)`: a UserDB at `DataDir/user.db`, for backup and restore tests.
- Matchers: `HistoryEntryMatcher()` (needs non-zero `Time` and a `TokenID`), `MappingMatcher()`, `MediaMatcher()`, `SystemMatcher()`, `TextMatcher()`, `SearchResultMatcher()`, `SlugResolutionMatcher(mediaDBID)`.
- sqlmock expectations: `ExpectHistoryInsert`, `ExpectMappingQuery`, `ExpectMappingInsert`, `ExpectMediaSearch`, `ExpectTransactionBegin`, `ExpectTransactionCommit`, `ExpectTransactionRollback`. Get the `*sql.DB` from `testsqlmock.NewSQLMock()`; `helpers.NewSQLMock()` only returns an error.

Filesystem and config:

- `NewMemoryFS()` and `NewOSFS()` return an `*FSHelper` whose `Fs` field is an `afero.Fs`. Methods: `WriteFile(path, data, perm)`, `ReadFile`, `FileExists`, `ListFiles`, `CreateConfigFile(path, map)`, `CreateAuthFile`, `CreateMediaDirectory(base)`, `CreateDirectoryStructure(map)`, `CleanupDir`, `SetupTestConfigEnvironment(base)`.
- `NewTestConfig(fs, dir)`, `NewTestConfigWithPort(fs, dir, port)`, `NewTestConfigWithListenAndPort(fs, dir, host, port)`. All disable BLE. A nil `fs` uses the real filesystem.

API:

- `NewWebSocketTestServer(t, handler)` serves `/api/v0.1`; `CreateWebSocketClient()`, `GetMessages()`, `ClearMessages()`, `Close()`.
- `SendJSONRPCRequest(conn, method, params)`, `AssertJSONRPCSuccess(t, resp)`, `AssertJSONRPCError(t, resp, code)`.
- `NewHTTPTestHelper(handler)` with `PostJSONRPC(method, params)`; `NewMockWebSocketConnection()`; `CreateTestContext(timeout)`.

Other: `NewMockESAPIServer(t)`, `NewMockKodiServer(t)`, `NewMockCommandExecutor()`, `TempSocketPath(t, name)`, `AssertValidActiveMedia(t, m)`, `AssertValidActiveMediaOrNil(t, m)`, `MockLevelWriter` (zerolog).

## Ports

Tests must never use port 7497, the real Core's port. Pass port 0 to the config helpers for an OS-assigned port, and prefer `NewTestConfigWithListenAndPort(fs, dir, "127.0.0.1", 0)` so nothing listens beyond loopback. When a test needs the port up front, bind `127.0.0.1:0` with `net.ListenConfig`, read `listener.Addr().(*net.TCPAddr).Port` and pass that (see `pkg/api/server_startup_test.go`). Don't hard-code port numbers; concurrent tests and a running Core collide on them.

## fixtures

- Tokens: `SampleTokens()`, `NewNFCToken()`, `NewMifareToken()`, `NewAmiiboToken()`, `NewEmptyToken()`, `NewAPIToken()`, `NewUnsafeToken()`, `NewCustomToken(...)`, `NewTokenCollection()`.
- Media (`*models.ActiveMedia`): `SampleMedia()`, `NewRetroGame()`, `NewModernGame()`, `NewArcadeGame()`, `NewConsoleGame()`, `NewHandheldGame()`, `NewCustomMedia(...)`, `NewMediaCollection()`.
- Playlists: `SamplePlaylists()`, `NewSamplePlaylist()`, `NewRetroPlaylist()`, `NewEmptyPlaylist()`.
- Database: `NewHistoryEntry(type, id, value, success)`, `NewMapping(...)`, `NewSystem(id, name)`, `GetTestSystemDefs()`, `GetSystemDefsByID()`; sample sets `HistoryEntries`, `Mappings`, `Systems`, `MediaTitles`.
- Kodi: `TestMovies`, `TestTVShows`, `TestEpisodes`, `TestActivePlayers`. Steam: `BuildShortcutsVDF(shortcuts)`. `NewStopNotificationBroker()`.

## scantest

`IndexMediaPaths(tb, db, systemID, paths...)`, `IndexMediaPathsWithOpts(tb, db, systemID, opts, paths...)`, `IndexScanResults(tb, db, systemID, opts, results...)` and `Index(ctx, db, systemID, opts, results...)` stage media through the production scan pipeline.

## harness

`harness.New(t, &harness.Options{...})` starts a sim Core and closes it at test cleanup; `Start(ctx, opts)` is the non-test form. One instance runs per process at a time. Options: `Fixtures`, `Values`, `Config`, `Root`, `Readers` (default one reader, `DefaultReader` = `sim0`), `Fakes`, `Index`, `NoWaitReaders`. `Instance` fields: `Platform`, `Config`, `API` (`Call`, `CallInto`, `WaitNotification`, `Index`, `WaitIdle`), `Control`, `Fakes`, `Audio`, `Fixtures`, `Events` (`Len`, `All`, `Wait`), `Layout`, `Root`; methods `Reader(name)`, `WaitReaders`, `Close`. Example and scenario syntax: [`TESTING.md`](../../TESTING.md) and [`.claude/skills/core-sim/SKILL.md`](../../.claude/skills/core-sim/SKILL.md).

## examples

`api_example_test.go`, `database_example_test.go`, `filesystem_example_test.go`, `mock_usage_example_test.go`, `service_state_management_test.go`, `service_token_processing_test.go`, `service_zapscript_test.go`.
