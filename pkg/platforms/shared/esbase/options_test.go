//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package esbase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOptions mirrors Batocera, the platform the generic behaviour was
// first written for.
func testOptions() Options {
	return Options{
		ID:             "batocera",
		DataDir:        "/userdata/system/.local/share/zaparoo",
		ConfigDir:      "/userdata/system/.config/zaparoo",
		LogDir:         "/userdata/system/.local/share/zaparoo/logs",
		DefaultROMRoot: "/userdata/roms",
		ESConfigDirs:   []string{"/userdata/system/configs/emulationstation"},
		ExcludedSystems: map[string]struct{}{
			"doom": {},
		},
		ExitGame:     HotkeygenExit,
		Controls:     HotkeygenControls,
		ControlNames: HotkeygenControlNames,
	}
}

// fakeAPI is an EmulationStation that reports a game until something kills
// it, without binding the real API port.
type fakeAPI struct {
	err      error
	game     *esapi.RunningGameResponse
	launched []string
	knownErr error
	notified []string
	// unlisted makes ES not list any game until it reloads, then list it
	// from the listAfter-th lookup after the reload.
	unlisted  bool
	listAfter int
	lookups   int
	reloads   int
	emuKills  int
}

func (f *fakeAPI) RunningGame() (esapi.RunningGameResponse, bool, error) {
	if f.err != nil {
		return esapi.RunningGameResponse{}, false, f.err
	}
	if f.game == nil {
		return esapi.RunningGameResponse{}, false, nil
	}
	return *f.game, true, nil
}

func (f *fakeAPI) EmuKill() error {
	f.emuKills++
	return nil
}

func (f *fakeAPI) Launch(path string) error {
	f.launched = append(f.launched, path)
	return nil
}

func (f *fakeAPI) Notify(msg string) error {
	f.notified = append(f.notified, msg)
	return nil
}

func (f *fakeAPI) GameKnown(_, _ string) (bool, error) {
	if f.knownErr != nil {
		return false, f.knownErr
	}
	if !f.unlisted {
		return true, nil
	}
	if f.reloads == 0 {
		return false, nil
	}
	f.lookups++
	return f.listAfter > 0 && f.lookups >= f.listAfter, nil
}

func (f *fakeAPI) ReloadGames() error {
	f.reloads++
	return nil
}

func runningPlatform(t *testing.T, opts *Options) (*Platform, *[]*models.ActiveMedia) {
	t.Helper()
	p := New(opts)
	media := models.NewActiveMedia("NES", "Nintendo Entertainment System", "/roms/nes/a.nes", "A", "")
	var published []*models.ActiveMedia
	p.activeMedia = func() *models.ActiveMedia { return media }
	p.setActiveMedia = func(m *models.ActiveMedia) {
		media = m
		published = append(published, m)
	}
	return p, &published
}

func TestNew_AppliesOptions(t *testing.T) {
	t.Parallel()

	opts := testOptions()
	opts.ManagedByPackageManager = func() bool { return true }
	p := New(&opts)

	assert.Equal(t, "batocera", p.ID())
	settings := p.Settings()
	assert.Equal(t, opts.DataDir, settings.DataDir)
	assert.Equal(t, opts.ConfigDir, settings.ConfigDir)
	assert.Equal(t, opts.LogDir, settings.LogDir)
	assert.True(t, p.ManagedByPackageManager())
	assert.Nil(t, p.UpdatePayload())
}

func TestNew_NilHooksAreSafe(t *testing.T) {
	t.Parallel()

	p := New(&Options{ID: "x"})
	assert.False(t, p.ManagedByPackageManager())
	assert.Nil(t, p.UpdatePayload())
	assert.Equal(t, []string{platforms.ControlStop}, p.controlNames())
	assert.Contains(t, p.controls(), platforms.ControlStop)
	assert.Len(t, p.controls(), 1)
}

func TestStopActiveLauncher_UsesKillGameUntilTheGameIsGone(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	kills := 0
	opts := Options{
		ID:  "rocknix",
		API: api,
		KillGame: func() error {
			kills++
			if kills == 2 {
				api.game = nil
			}
			return nil
		},
	}
	p, published := runningPlatform(t, &opts)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 2, kills)
	assert.Zero(t, api.emuKills, "a KillGame hook replaces /emukill")
	require.NotEmpty(t, *published)
	assert.Nil(t, (*published)[len(*published)-1])
}

func TestStopActiveLauncher_ExitGameRunsFirst(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	var order []string
	opts := Options{
		ID:  "amberelec",
		API: api,
		ExitGame: func(context.Context) error {
			order = append(order, "exit")
			api.game = nil
			return nil
		},
		KillGame: func() error {
			order = append(order, "kill")
			return nil
		},
	}
	p, _ := runningPlatform(t, &opts)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, []string{"exit"}, order, "a game that exited needs no kill")
}

func TestStopActiveLauncher_FailsWhenTheGameSurvives(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	kills := 0
	opts := Options{
		ID:             "rocknix",
		API:            api,
		KillRetryDelay: time.Millisecond,
		KillGame: func() error {
			kills++
			return errors.New("still there")
		},
	}
	p, _ := runningPlatform(t, &opts)

	require.Error(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 10, kills)
}

// An ES without /runningGame cannot confirm the game is gone, so a stop has
// to kill it rather than take the failed check as proof it already exited.
func TestStopActiveLauncher_KillsOnceWithoutRunningGameRoute(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{err: esapi.ErrRouteUnsupported}
	kills := 0
	opts := Options{
		ID:  "amberelec",
		API: api,
		KillGame: func() error {
			kills++
			return nil
		},
	}
	p, published := runningPlatform(t, &opts)

	require.NoError(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 1, kills)
	require.NotEmpty(t, *published)
	assert.Nil(t, (*published)[len(*published)-1])
}

func TestStopActiveLauncher_DefaultKillIsEmuKill(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Path: "/roms/nes/a.nes"}}
	opts := Options{ID: "knulli", API: api}
	p, _ := runningPlatform(t, &opts)

	require.Error(t, p.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, 10, api.emuKills)
}

func TestInjectedAPI_LaunchAndNotify(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{}
	opts := Options{ID: "knulli", API: api}
	p := New(&opts)

	_, err := p.esAPILaunch(nil, "/roms/nes/a.nes", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"/roms/nes/a.nes"}, api.launched)

	_, err = p.PresentUI(context.Background(), &models.UIEvent{
		Kind: models.UIEventKindNotice, Message: "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, api.notified)
}

func TestLaunchers_StopOnlyControlsWithoutHotkeygen(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	api := &fakeAPI{}
	opts := Options{ID: "rocknix", API: api, DefaultROMRoot: "/storage/roms"}
	p := New(&opts)
	p.activeMedia = func() *models.ActiveMedia { return nil }

	found := false
	for _, l := range p.Launchers(cfg) {
		if l.SystemID != "NES" {
			continue
		}
		found = true
		require.Len(t, l.Controls, 1)
		require.Contains(t, l.Controls, platforms.ControlStop)
		require.NoError(t, l.Controls[platforms.ControlStop].Func(context.Background(), cfg,
			platforms.ControlParams{}))
	}
	assert.True(t, found)
}

func TestTracker_NamesStopOnlyControlsWithoutHotkeygen(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{SystemName: "nes", Name: "A", Path: "/storage/roms/nes/a.nes"}}
	opts := Options{ID: "rocknix", API: api}
	p := New(&opts)
	p.cfg = mustConfig(t)
	var got *models.ActiveMedia
	p.checkAndUpdateRunningGame(func(m *models.ActiveMedia) { got = m })

	require.NotNil(t, got)
	assert.Equal(t, []string{platforms.ControlStop}, got.LauncherControls)
}

func TestRootDirs_ReadsEveryESConfigDir(t *testing.T) {
	t.Parallel()

	sys := t.TempDir()
	user := t.TempDir()
	writeESSystems(t, sys, "nes", "/media/a/roms/nes")
	writeESSystems(t, user, "nes", "/media/b/roms/nes")
	opts := Options{ID: "rocknix", DefaultROMRoot: "/storage/roms", ESConfigDirs: []string{sys, user}}
	p := New(&opts)

	cfg := mustConfig(t)
	assert.Equal(t, []string{"/media/b/roms", "/storage/roms"}, p.RootDirs(cfg),
		"the later directory overrides the earlier one")
}

func mustConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	return cfg
}

func writeESSystems(t *testing.T, dir, name, path string) {
	t.Helper()
	content := "<systemList><system><name>" + name + "</name><path>" + path +
		"</path><extension>.nes</extension></system></systemList>"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ESSystemsConfigFile), []byte(content), 0o600))
}

func TestTracker_FollowsSystemAliases(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{game: &esapi.RunningGameResponse{
		SystemName: "fbn", Name: "A", Path: "/storage/roms/fbneo/a.zip",
	}}
	opts := Options{ID: "rocknix", API: api, SystemAliases: map[string]string{"fbn": "fbneo"}}
	p := New(&opts)
	p.cfg = mustConfig(t)
	var got *models.ActiveMedia
	p.checkAndUpdateRunningGame(func(m *models.ActiveMedia) { got = m })

	require.NotNil(t, got)
	assert.Equal(t, "Arcade", got.SystemID)
	assert.NotEmpty(t, got.LauncherID)
}

func TestLaunchers_ScanAliasFolders(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	opts := Options{ID: "rocknix", API: &fakeAPI{}, SystemAliases: map[string]string{
		"sfc": "snes", "snesh": "snes",
	}}
	p := New(&opts)

	for _, l := range p.Launchers(cfg) {
		if len(l.Folders) > 0 && l.Folders[0] == "snes" {
			assert.Equal(t, []string{"snes", "sfc", "snesh"}, l.Folders)
			return
		}
	}
	t.Fatal("no snes launcher")
}

func writeESSystemList(t *testing.T, dir string, systems map[string]string) {
	t.Helper()
	var content strings.Builder
	content.WriteString("<systemList>")
	for name, path := range systems {
		content.WriteString("<system><name>" + name + "</name><path>" + path +
			"</path><extension>.nes</extension></system>")
	}
	content.WriteString("</systemList>")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ESSystemsConfigFile), []byte(content.String()), 0o600))
}

// symlinkedROMs lays out AmberELEC's roots: storage/roms, and roms as a
// symlink to it that one ES system names.
func symlinkedROMs(t *testing.T) (storageROMs, linkROMs, esDir string) {
	t.Helper()
	base := t.TempDir()
	storageROMs = filepath.Join(base, "storage", "roms")
	require.NoError(t, os.MkdirAll(filepath.Join(storageROMs, "gamecom"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(storageROMs, "nes"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(storageROMs, "gamecom", "a.bin"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(storageROMs, "nes", "b.nes"), nil, 0o600))
	linkROMs = filepath.Join(base, "roms")
	require.NoError(t, os.Symlink(storageROMs, linkROMs))
	esDir = t.TempDir()
	writeESSystemList(t, esDir, map[string]string{
		"nes":         filepath.Join(storageROMs, "nes"),
		"gamecom":     filepath.Join(linkROMs, "gamecom"),
		"imageviewer": filepath.Join(linkROMs, "screenshots"),
		"tools":       filepath.Join(base, "storage", ".config", "distribution", "modules"),
	})
	return storageROMs, linkROMs, esDir
}

func TestRootDirs_SymlinkedRootIndexedOnce(t *testing.T) {
	t.Parallel()

	storageROMs, _, esDir := symlinkedROMs(t)
	opts := Options{
		ID: "amberelec", DefaultROMRoot: storageROMs, ESConfigDirs: []string{esDir},
		NonGameSystems: []string{"tools"},
	}
	assert.Equal(t, []string{storageROMs}, New(&opts).RootDirs(mustConfig(t)))
}

func TestRootDirs_NonGameSystemsAreNotRoots(t *testing.T) {
	t.Parallel()

	storageROMs, _, esDir := symlinkedROMs(t)
	opts := Options{ID: "amberelec", DefaultROMRoot: storageROMs, ESConfigDirs: []string{esDir}}
	assert.Len(t, New(&opts).RootDirs(mustConfig(t)), 2, "tools adds its parent without NonGameSystems")
}

func TestESAPILaunch_UsesTheSystemsOwnPath(t *testing.T) {
	t.Parallel()

	storageROMs, linkROMs, esDir := symlinkedROMs(t)
	api := &fakeAPI{}
	opts := Options{ID: "amberelec", API: api, DefaultROMRoot: storageROMs, ESConfigDirs: []string{esDir}}
	p := New(&opts)

	for _, path := range []string{
		filepath.Join(storageROMs, "gamecom", "a.bin"),
		filepath.Join(storageROMs, "nes", "b.nes"),
		filepath.Join(storageROMs, "missing", "c.nes"),
	} {
		_, err := p.esAPILaunch(nil, path, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, []string{
		filepath.Join(linkROMs, "gamecom", "a.bin"),
		filepath.Join(storageROMs, "nes", "b.nes"),
		filepath.Join(storageROMs, "missing", "c.nes"),
	}, api.launched)
}

// Two systems can both hold a game when one sits under the other. The
// deepest one is how ES knows the game, whatever order the config map has.
func TestESAPILaunch_NestedSystemsUseTheDeepest(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	storageROMs := filepath.Join(base, "storage", "roms")
	require.NoError(t, os.MkdirAll(filepath.Join(storageROMs, "snes"), 0o750))
	game := filepath.Join(storageROMs, "snes", "x.sfc")
	require.NoError(t, os.WriteFile(game, nil, 0o600))
	rootLink := filepath.Join(base, "roms")
	snesLink := filepath.Join(base, "snesroms")
	require.NoError(t, os.Symlink(storageROMs, rootLink))
	require.NoError(t, os.Symlink(storageROMs, snesLink))
	esDir := t.TempDir()
	writeESSystemList(t, esDir, map[string]string{
		"all":  rootLink,
		"snes": filepath.Join(snesLink, "snes"),
	})

	for range 20 {
		api := &fakeAPI{}
		opts := Options{ID: "amberelec", API: api, DefaultROMRoot: storageROMs, ESConfigDirs: []string{esDir}}
		_, err := New(&opts).esAPILaunch(nil, game, nil)
		require.NoError(t, err)
		require.Equal(t, []string{filepath.Join(snesLink, "snes", "x.sfc")}, api.launched)
	}
}

// A custom launcher with no command launches through ES too, so it needs the
// same path spelling as the system launchers.
func TestCustomESLauncher_UsesTheSystemsOwnPath(t *testing.T) {
	t.Parallel()

	storageROMs, linkROMs, esDir := symlinkedROMs(t)
	fs := helpers.NewMemoryFS()
	require.NoError(t, fs.WriteFile("/launchers/gamecom.toml", []byte(`[[launchers.custom]]
id = "MyGamecom"
system = "GameCom"
media_dirs = ["gamecom"]
`), 0o600))
	cfg, err := helpers.NewTestConfig(fs, "/config")
	require.NoError(t, err)
	require.NoError(t, cfg.LoadCustomLaunchers("/launchers"))

	api := &fakeAPI{}
	opts := Options{ID: "amberelec", API: api, DefaultROMRoot: storageROMs, ESConfigDirs: []string{esDir}}
	p := New(&opts)

	var launcher *platforms.Launcher
	for _, l := range p.Launchers(cfg) {
		if l.ID == "MyGamecom" {
			launcher = &l
			break
		}
	}
	require.NotNil(t, launcher)
	_, err = launcher.Launch(cfg, filepath.Join(storageROMs, "gamecom", "a.bin"), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(linkROMs, "gamecom", "a.bin")}, api.launched)
}

type countingAPI struct {
	fakeAPI
	calls int
}

func (c *countingAPI) RunningGame() (esapi.RunningGameResponse, bool, error) {
	c.calls++
	return c.fakeAPI.RunningGame()
}

func TestStartPost_StopsProbingWithoutRunningGameRoute(t *testing.T) {
	t.Parallel()

	api := &countingAPI{fakeAPI: fakeAPI{err: esapi.ErrRouteUnsupported}}
	opts := Options{ID: "amberelec", API: api}
	p := New(&opts)
	p.clock = clockwork.NewFakeClock()
	p.maxStartupRetries = 5

	var media *models.ActiveMedia
	require.NoError(t, p.StartPost(t.Context(), mustConfig(t), nil,
		func() *models.ActiveMedia { return media },
		func(m *models.ActiveMedia) { media = m }, nil, nil))
	if p.stopTracker != nil {
		_ = p.stopTracker()
	}
	assert.Equal(t, 1, api.calls, "a missing route will not appear on retry")
	assert.Nil(t, media)
}

func launcherFolders(p *Platform, cfg *config.Instance) map[string]bool {
	folders := map[string]bool{}
	launchers := p.Launchers(cfg)
	for i := range launchers {
		if len(launchers[i].Folders) > 0 {
			folders[launchers[i].Folders[0]] = true
		}
	}
	return folders
}

func TestLaunchers_OnlyESSystems(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	dir := t.TempDir()
	writeESSystemList(t, dir, map[string]string{
		"nes":    "/userdata/roms/nes",
		"segacd": "/userdata/roms/segacd",
	})
	opts := Options{
		ID: "knulli", API: &fakeAPI{}, ESConfigDirs: []string{dir}, OnlyESSystems: true,
		SystemAliases: map[string]string{"segacd": "megacd"},
	}
	folders := launcherFolders(New(&opts), cfg)
	assert.True(t, folders["nes"])
	assert.True(t, folders["megacd"], "an aliased ES system keeps its ES-DE folder")
	assert.False(t, folders["3ds"], "a folder with no ES system is left out")

	opts.ESConfigDirs = []string{t.TempDir()}
	assert.True(t, launcherFolders(New(&opts), cfg)["3ds"],
		"without a readable ES config nothing is left out")
}

func TestLaunchers_HasKodi(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	hasKodiLauncher := func(opts *Options) bool {
		for _, l := range New(opts).Launchers(cfg) {
			if isKodiLauncher(l.ID) {
				return true
			}
		}
		return false
	}
	assert.True(t, hasKodiLauncher(&Options{ID: "batocera", API: &fakeAPI{}}))
	assert.False(t, hasKodiLauncher(&Options{ID: "knulli", API: &fakeAPI{}, HasKodi: func() bool { return false }}))
	assert.True(t, hasKodiLauncher(&Options{ID: "knulli", API: &fakeAPI{}, HasKodi: func() bool { return true }}))
}

// esReloadPlatform is a platform whose ES lists the NES system at roms/nes.
func esReloadPlatform(t *testing.T, api *fakeAPI) (p *Platform, game string) {
	t.Helper()
	roms := t.TempDir()
	esDir := t.TempDir()
	writeESSystemList(t, esDir, map[string]string{"nes": filepath.Join(roms, "nes")})
	opts := Options{ID: "batocera", API: api, DefaultROMRoot: roms, ESConfigDirs: []string{esDir}}
	return New(&opts), filepath.Join(roms, "nes", "new.nes")
}

func TestESAPILaunch_ListedGameSkipsReload(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{}
	p, game := esReloadPlatform(t, api)
	_, err := p.esAPILaunch(nil, game, nil)
	require.NoError(t, err)
	assert.Zero(t, api.reloads)
	assert.Equal(t, []string{game}, api.launched)
}

// ES ignores a launch for a game it has not loaded, such as one Core has
// just downloaded, so the launch has it reload and waits for the game.
func TestESAPILaunch_UnlistedGameReloadsFirst(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{unlisted: true, listAfter: 3}
	p, game := esReloadPlatform(t, api)
	fakeClock := clockwork.NewFakeClock()
	p.clock = fakeClock

	done := make(chan error, 1)
	go func() {
		_, err := p.esAPILaunch(nil, game, nil)
		done <- err
	}()
	advanceUntilDone(t, fakeClock, done)
	assert.Equal(t, 1, api.reloads)
	assert.Equal(t, 3, api.lookups)
	assert.Equal(t, []string{game}, api.launched)
}

func TestESAPILaunch_ReloadTimesOutThenLaunches(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{unlisted: true}
	p, game := esReloadPlatform(t, api)
	fakeClock := clockwork.NewFakeClock()
	p.clock = fakeClock

	done := make(chan error, 1)
	go func() {
		_, err := p.esAPILaunch(nil, game, nil)
		done <- err
	}()
	advanceUntilDone(t, fakeClock, done)
	assert.Equal(t, 1, api.reloads)
	assert.Equal(t, int(esReloadWait/esReloadPoll), api.lookups)
	assert.Equal(t, []string{game}, api.launched)
}

// An ES that cannot answer whether it lists a game launches as before.
func TestESAPILaunch_LookupErrorLaunchesWithoutReload(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{unlisted: true, knownErr: esapi.ErrRouteUnsupported}
	p, game := esReloadPlatform(t, api)
	_, err := p.esAPILaunch(nil, game, nil)
	require.NoError(t, err)
	assert.Zero(t, api.reloads)
	assert.Equal(t, []string{game}, api.launched)
}

// A game outside every ES system has no system to ask about.
func TestESAPILaunch_GameOutsideSystemsSkipsReload(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{unlisted: true}
	p, _ := esReloadPlatform(t, api)
	game := filepath.Join(t.TempDir(), "elsewhere.nes")
	_, err := p.esAPILaunch(nil, game, nil)
	require.NoError(t, err)
	assert.Zero(t, api.reloads)
	assert.Equal(t, []string{game}, api.launched)
}

func TestESSystemName_PrefersDeepestAndListName(t *testing.T) {
	t.Parallel()

	roms := t.TempDir()
	p := New(&Options{ID: "batocera", DefaultROMRoot: roms})
	p.esConfigCache = &ESSystemConfig{Systems: map[string]ESSystem{
		"all":  {Name: "all", Path: roms},
		"snes": {Name: "", Path: filepath.Join(roms, "snes")},
		"gb":   {Name: "gameboy", Path: filepath.Join(roms, "gb")},
	}}
	assert.Equal(t, "snes", p.esSystemName(filepath.Join(roms, "snes", "a.sfc")))
	assert.Equal(t, "gameboy", p.esSystemName(filepath.Join(roms, "gb", "a.gb")))
	assert.Equal(t, "all", p.esSystemName(filepath.Join(roms, "a.zip")))
	assert.Empty(t, p.esSystemName(filepath.Join(t.TempDir(), "a.zip")))
}

// Batocera's system list is not one Core reads, and its systems are named
// by their folders under the ROM root.
func TestESSystemName_FolderWithoutSystemList(t *testing.T) {
	t.Parallel()

	roms := t.TempDir()
	p := New(&Options{ID: "batocera", DefaultROMRoot: roms})
	assert.Equal(t, "nes", p.esSystemName(filepath.Join(roms, "nes", "a.nes")))
	assert.Equal(t, "psx", p.esSystemName(filepath.Join(roms, "psx", "Disc Game", "a.cue")))
	assert.Empty(t, p.esSystemName(filepath.Join(roms, "a.nes")), "a game at the root has no system")
	assert.Empty(t, p.esSystemName(filepath.Join(filepath.Dir(roms), "other", "a.nes")))
}

// advanceUntilDone steps the fake clock while a launch waits on it.
func advanceUntilDone(t *testing.T, clock *clockwork.FakeClock, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		select {
		case err := <-done:
			require.NoError(t, err)
			return
		default:
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, 20*time.Millisecond)
		blocked := clock.BlockUntilContext(waitCtx, 1) == nil
		waitCancel()
		require.NoError(t, ctx.Err(), "launch never finished")
		if blocked {
			clock.Advance(esReloadPoll)
		}
	}
}
