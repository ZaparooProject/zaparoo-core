// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package android

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testReference = "content://org.example.documents/tree/games"

// fakeHost is an in-memory Host: packages are installed unless listed in
// failures, and one media folder holds nes/Game.nes and psx/Game.chd.
type fakeHost struct {
	dispatchErr     error
	foldersErr      error
	readErr         error
	foregroundErr   error
	evidenceErr     error
	failures        map[string]FailureReason
	receipt         *DispatchReceipt
	icons           map[string]string
	files           map[string][]byte
	state           *ForegroundState
	evidence        *database.ForegroundEvidence
	substitute      string
	references      []string
	cores           []string
	apps            []AppInfo
	iconCalls       []string
	inspections     []string
	dispatched      []dispatchCall
	evidenceQueries []evidenceQuery
	appCalls        int
	listings        int
	coreCalls       int
	mu              syncutil.Mutex
	notInteractive  bool
	scanned         bool
	appsScanned     bool
}

// calls reports how often each host sweep ran.
func (h *fakeHost) calls() (cores, apps, inspections int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.coreCalls, h.appCalls, len(h.inspections)
}

type evidenceQuery struct {
	launchID string
	target   string
	fromMs   int64
	toMs     int64
}

type dispatchCall struct {
	reference  string
	segments   []string
	definition LaunchDefinition
}

func (h *fakeHost) InstalledApps() ([]AppInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appCalls++
	return h.apps, h.appsScanned
}

func (h *fakeHost) AppIcon(packageName string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.iconCalls = append(h.iconCalls, packageName)
	return h.icons[packageName], nil
}

func (h *fakeHost) DispatchApp(definition *LaunchDefinition) (DispatchReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dispatched = append(h.dispatched, dispatchCall{definition: *definition})
	if h.dispatchErr != nil {
		return DispatchReceipt{}, h.dispatchErr
	}
	if h.receipt != nil {
		return *h.receipt, nil
	}
	pkg := definition.Package
	if h.substitute != "" {
		pkg = h.substitute
	}
	return DispatchReceipt{
		Package:  pkg,
		Activity: definition.Activity,
		Strategy: definition.Strategy,
	}, nil
}

func (h *fakeHost) InspectTarget(definition *LaunchDefinition) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inspections = append(h.inspections, definition.Package)
	if reason, failed := h.failures[definition.Package]; failed {
		return &HostError{Reason: reason}
	}
	return nil
}

func (h *fakeHost) InstalledCores() (files []string, scanned bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.coreCalls++
	return h.cores, h.scanned
}

func (h *fakeHost) MediaFolders(context.Context) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listings++
	if h.foldersErr != nil {
		return nil, h.foldersErr
	}
	if h.references != nil {
		return h.references, nil
	}
	return []string{testReference}, nil
}

func (*fakeHost) ReadMediaDir(_ context.Context, reference string, segments []string) ([]platforms.SourceEntry, error) {
	if reference != testReference {
		return nil, &HostError{Reason: FailureSourceUnavailable}
	}
	tree := map[string][]platforms.SourceEntry{
		"":    {{Name: "nes", Dir: true, Size: -1}, {Name: "psx", Dir: true, Size: -1}},
		"nes": {{Name: "Game.nes", Size: 16}},
		"psx": {{Name: "Game.chd", Size: 16}},
	}
	return tree[strings.Join(segments, "/")], nil
}

func (h *fakeHost) ReadFile(
	_ context.Context, reference string, segments []string, limit int64,
) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.readErr != nil {
		return nil, h.readErr
	}
	if reference != testReference {
		return nil, &HostError{Reason: FailureSourceUnavailable}
	}
	content, found := h.files[strings.Join(segments, "/")]
	if !found {
		return nil, &HostError{Reason: FailureSourceUnavailable}
	}
	if int64(len(content)) > limit {
		content = content[:limit+1]
	}
	return content, nil
}

func (h *fakeHost) exists(reference string, segments []string) bool {
	if len(segments) == 0 {
		return false
	}
	entries, err := h.ReadMediaDir(context.Background(), reference, segments[:len(segments)-1])
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.Dir && entry.Name == segments[len(segments)-1] {
			return true
		}
	}
	return false
}

func (h *fakeHost) Dispatch(
	_ context.Context,
	definition *LaunchDefinition,
	reference string,
	segments []string,
) (DispatchReceipt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dispatched = append(h.dispatched, dispatchCall{
		definition: *definition, reference: reference, segments: segments,
	})
	if !h.exists(reference, segments) {
		return DispatchReceipt{}, &HostError{Reason: FailureSourceUnavailable}
	}
	if h.substitute != "" {
		// A host that writes through the pointer it was handed, and reports
		// the component it actually started.
		definition.Package = h.substitute
		if len(definition.Extensions) > 0 {
			definition.Extensions[0] = ".substituted"
		}
	}
	if h.dispatchErr != nil {
		return DispatchReceipt{}, h.dispatchErr
	}
	if h.receipt != nil {
		return *h.receipt, nil
	}
	return DispatchReceipt{
		Package: definition.Package, Activity: definition.Activity, Strategy: definition.Strategy,
	}, nil
}

// defaultForegroundState is a plausible granted-permission snapshot for
// tests that need one but do not care about its exact values.
var defaultForegroundState = ForegroundState{
	BootID: "boot-1", Permission: "granted", SampledMs: 1_000_000, ElapsedMs: 5_000,
	Interactive: true, Unlocked: true,
}

func (h *fakeHost) ForegroundState() (ForegroundState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.foregroundErr != nil {
		return ForegroundState{}, h.foregroundErr
	}
	if h.state != nil {
		return *h.state, nil
	}
	return defaultForegroundState, nil
}

func (h *fakeHost) Interactive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.notInteractive
}

func (h *fakeHost) ForegroundEvents(
	_ context.Context, launchID, target string, fromMs, toMs int64,
) (database.ForegroundEvidence, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.evidenceQueries = append(h.evidenceQueries,
		evidenceQuery{launchID: launchID, target: target, fromMs: fromMs, toMs: toMs})
	if h.evidenceErr != nil {
		return database.ForegroundEvidence{}, h.evidenceErr
	}
	if h.evidence != nil {
		return *h.evidence, nil
	}
	state := defaultForegroundState
	if h.state != nil {
		state = *h.state
	}
	return database.ForegroundEvidence{
		Version: 1, LaunchID: launchID, BootID: state.BootID, Permission: state.Permission,
		Complete: true, QueryFromMs: fromMs, QueryToMs: toMs,
		ObservedElapsedMs: state.ElapsedMs, ObservedWallMs: toMs,
	}, nil
}

type fixedContext struct{ ctx context.Context }

func (c fixedContext) GetContext() context.Context { return c.ctx }
func (c fixedContext) NewContext() context.Context { return c.ctx }

func identity(t *testing.T, segments ...string) string {
	t.Helper()
	id := strings.TrimPrefix(platforms.SourceRootPath(testReference), platforms.SourceScheme+"://")
	value, err := virtualpath.CreateVirtualPathSegments(platforms.SourceScheme, id, segments)
	require.NoError(t, err)
	return value
}

func startedPlatform(ctx context.Context, t *testing.T, host Host) *Platform {
	t.Helper()
	platform, err := New(platforms.Settings{DataDir: "/data"}, host)
	require.NoError(t, err)
	require.NoError(t, platform.StartPost(ctx, nil, fixedContext{ctx: ctx}, nil, nil, nil, nil))
	return platform
}

func launcherByID(t *testing.T, launchers []platforms.Launcher, id string) *platforms.Launcher {
	t.Helper()
	for i := range launchers {
		if launchers[i].ID == id {
			return &launchers[i]
		}
	}
	require.Failf(t, "launcher not registered", "%s", id)
	return nil
}

func TestPlatformIdentityAndUnsupportedOperations(t *testing.T) {
	t.Parallel()

	settings := platforms.Settings{DataDir: "/data", HostManagedPaths: true, DisableSelfUpdate: true}
	platform, err := New(settings, nil)
	require.NoError(t, err)

	assert.Equal(t, "android", platform.ID())
	assert.Equal(t, settings, platform.Settings())
	assert.True(t, platform.ManagedByPackageManager())
	assert.Empty(t, platform.RootDirs(nil))
	assert.Empty(t, platform.Launchers(nil), "no host means nothing can be started")
	require.NoError(t, platform.ScanHook(nil))

	require.ErrorIs(t, platform.ReturnToMenu(), platforms.ErrNotSupported)
	require.ErrorIs(t, platform.LaunchSystem(nil, "NES"), platforms.ErrNotSupported)
	require.ErrorIs(t, platform.KeyboardPress("a"), platforms.ErrNotSupported)
	_, err = platform.Screenshot()
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	roots, err := platform.SourceRoots(t.Context())
	require.NoError(t, err)
	assert.Empty(t, roots, "no host means no media folders")
	_, err = platform.ReadSourceDir(t.Context(), platforms.SourceRootPath(testReference))
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	err = platform.LaunchMedia(nil, "source://x/nes/Game.nes", nil, nil, nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
}

func TestInteractiveFollowsTheHostAndDefaultsToInteractive(t *testing.T) {
	t.Parallel()

	settings := platforms.Settings{DataDir: "/data", HostManagedPaths: true}

	noHost, err := New(settings, nil)
	require.NoError(t, err)
	assert.True(t, noHost.Interactive(), "no host yet must not make a background feature ineligible")

	host := &fakeHost{}
	platform, err := New(settings, host)
	require.NoError(t, err)
	assert.True(t, platform.Interactive())

	host.mu.Lock()
	host.notInteractive = true
	host.mu.Unlock()
	assert.False(t, platform.Interactive())

	var reader platforms.InteractivityReader = platform
	assert.False(t, reader.Interactive())
}

func TestLaunchersFollowCatalogOrderAndShareInspections(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	launchers := platform.Launchers(nil)

	// Three launchers Core builds (ScummVM standalone, two GameNative) lead
	// the catalog; two trailing launchers offer every installed app no
	// profile describes, split by the host's game classification.
	require.Len(t, launchers, len(platform.entries)+len(gameNativeEntries)+3)
	assert.Equal(t, installedAppsNonGameID, launchers[len(launchers)-1].ID)
	assert.Equal(t, installedAppsID, launchers[len(launchers)-2].ID)
	assert.Equal(t, scummVMStandaloneID, launchers[0].ID)
	assert.Equal(t, gameNativeSteamID, launchers[1].ID)
	assert.Equal(t, gameNativeWindowsID, launchers[2].ID)
	for _, id := range []string{scummVMStandaloneID, gameNativeSteamID, gameNativeWindowsID} {
		built := launcherByID(t, launchers, id)
		assert.Empty(t, built.Schemes)
		assert.NotEmpty(t, built.Folders)
		assert.False(t, built.SkipFilesystemScan, "media folders are indexed like root directories")
		assert.Equal(t, platforms.LifecycleExternal, built.Lifecycle)
	}
	launchers = launchers[len(gameNativeEntries)+1:]
	for i := range platform.entries {
		assert.Equal(t, platform.entries[i].definition.ID, launchers[i].ID)
		if platform.entries[i].definition.Strategy == StrategyApp {
			assert.Equal(t, []string{"android"}, launchers[i].Schemes)
			assert.Equal(t, platforms.LifecycleExternal, launchers[i].Lifecycle)
			assert.NotEmpty(t, launchers[i].SystemID)
			continue
		}
		assert.Empty(t, launchers[i].Schemes)
		assert.NotEmpty(t, launchers[i].Folders)
		assert.False(t, launchers[i].SkipFilesystemScan, "media folders are indexed like root directories")
		assert.Equal(t, platforms.LifecycleExternal, launchers[i].Lifecycle)
		assert.NotEmpty(t, launchers[i].SystemID)
	}
	assert.ElementsMatch(t, []string{
		"com.github.stenzek.duckstation", "org.ppsspp.ppsspp", "org.dolphinemu.dolphinemu",
		"com.seleuco.mame4d2024", "com.armsx2", "com.theboisclub.pokemonred", retroArchPackage,
		scummVMPackage, gameNativePackage,
	}, host.inspections, "each target is inspected once per sweep, not once per profile")

	mesen := launcherByID(t, launchers, "RetroArch.Mesen")
	assert.Equal(t, []string{"RetroArch"}, mesen.Groups)
	cfg := &config.Instance{}
	assert.True(t, helpers.PathIsLauncher(cfg, platform, mesen, identity(t, "nes", "Game.nes")))
	assert.True(t, helpers.PathIsLauncher(cfg, platform, mesen, identity(t, "NES", "Sub", "Game.NES")))
	assert.False(t, helpers.PathIsLauncher(cfg, platform, mesen, identity(t, "snes", "Game.nes")))
	assert.False(t, helpers.PathIsLauncher(cfg, platform, mesen, "/storage/emulated/0/nes/Game.nes"))
}

// A system's media may sit in any of its EmulationStation folders or under its
// ID or an alias, compared without case.
func TestSystemFolders(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"NES"}, systemFolders("NES"))
	assert.Equal(t, []string{"N64", "Nintendo64", "n64dd"}, systemFolders("Nintendo64"))
}

func TestLauncherDetectionIsTriState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host    *fakeHost
		want    map[string]*bool
		reasons map[string]string
		name    string
	}{
		{
			name: "no core scan leaves cores unknown and installed apps detected",
			host: &fakeHost{},
			want: map[string]*bool{"RetroArch.Mesen": nil, "DuckStation.PSX": new(true)},
		},
		{
			name: "a core scan is authoritative per core file",
			host: &fakeHost{scanned: true, cores: []string{"mesen_libretro_android.so"}},
			want: map[string]*bool{"RetroArch.Mesen": new(true), "RetroArch.FCEUmm": new(false)},
		},
		{
			name: "a malformed core listing is not evidence of absence",
			host: &fakeHost{scanned: true, cores: []string{"../mesen_libretro_android.so"}},
			want: map[string]*bool{"RetroArch.Mesen": nil},
		},
		{
			name: "an uninstalled app is known missing",
			host: &fakeHost{
				scanned: true, cores: []string{"mesen_libretro_android.so"},
				failures: map[string]FailureReason{
					"com.github.stenzek.duckstation": FailureNotInstalled,
					retroArchPackage:                 FailureNotInstalled,
				},
			},
			want: map[string]*bool{
				"DuckStation.PSX": new(false), "RetroArch.Mesen": new(false), "PPSSPP.PSP": new(true),
			},
			reasons: map[string]string{
				"DuckStation.PSX": "Install or enable DuckStation and complete its first-run setup.",
			},
		},
		{
			name: "an unreachable host proves nothing about an app",
			host: &fakeHost{failures: map[string]FailureReason{"org.ppsspp.ppsspp": FailureHostUnavailable}},
			want: map[string]*bool{"PPSSPP.PSP": nil},
			reasons: map[string]string{
				"PPSSPP.PSP": repairMessage(FailureHostUnavailable, ""),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			launchers := startedPlatform(t.Context(), t, tt.host).Launchers(nil)
			for id, want := range tt.want {
				assert.Equal(t, want, launcherByID(t, launchers, id).Detected, id)
			}
			for id, reason := range tt.reasons {
				launcher := launcherByID(t, launchers, id)
				assert.False(t, launcher.Available, id)
				assert.Equal(t, reason, launcher.AvailabilityReason, id)
				var repair *platforms.LaunchRepairError
				require.ErrorAs(t, launcher.Availability(nil), &repair, id)
			}
		})
	}
}

func TestLauncherPreflight(t *testing.T) {
	t.Parallel()

	host := &fakeHost{scanned: true, cores: []string{"mesen_libretro_android.so"}}
	launchers := startedPlatform(t.Context(), t, host).Launchers(nil)
	mesen := launcherByID(t, launchers, "RetroArch.Mesen")
	game := identity(t, "nes", "Game.nes")

	cfg := &config.Instance{}
	require.NoError(t, mesen.Preflight(cfg, game, nil))
	require.EqualError(t, launcherByID(t, launchers, "RetroArch.FCEUmm").Preflight(cfg, game, nil), msgCoreMissing)
	require.EqualError(t, mesen.Preflight(cfg, identity(t, "snes", "Game.sfc"), nil), msgWrongMedia)
	require.EqualError(t, mesen.Preflight(cfg, "/storage/emulated/0/nes/Game.nes", nil), msgWrongMedia)
	require.EqualError(t, mesen.Preflight(cfg, game, &platforms.LaunchOptions{SetName: "other"}),
		msgOverridesUnsupported)
}

// The tests below replace helpers.GlobalLauncherCache, so they do not run in
// parallel with each other.
func useLauncherCache(t *testing.T, platform *Platform) {
	t.Helper()
	original := helpers.GlobalLauncherCache
	cache := &helpers.LauncherCache{}
	cache.Initialize(platform, &config.Instance{})
	helpers.GlobalLauncherCache = cache
	t.Cleanup(func() { helpers.GlobalLauncherCache = original })
}

//nolint:paralleltest // replaces the shared launcher cache
func TestLaunchMediaInfersLauncherFromRegistrationOrder(t *testing.T) {
	tests := []struct {
		host *fakeHost
		name string
		want string
		path []string
	}{
		{
			name: "first registered launcher for the system",
			host: &fakeHost{}, path: []string{"psx", "Game.chd"}, want: "DuckStation.PSX",
		},
		{
			name: "a missing app yields to the next launcher",
			host: &fakeHost{failures: map[string]FailureReason{
				"com.github.stenzek.duckstation": FailureNotInstalled,
			}},
			path: []string{"psx", "Game.chd"}, want: "RetroArch.Swanstation.PSX",
		},
		{
			name: "a missing core yields to the next installed core",
			host: &fakeHost{scanned: true, cores: []string{"nestopia_libretro_android.so"}},
			path: []string{"nes", "Game.nes"}, want: "RetroArch.Nestopia.NES",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform := startedPlatform(t.Context(), t, tt.host)
			useLauncherCache(t, platform)

			require.NoError(t, platform.LaunchMedia(&config.Instance{}, identity(t, tt.path...), nil, nil, nil))

			require.Len(t, tt.host.dispatched, 1)
			assert.Equal(t, tt.want, tt.host.dispatched[0].definition.ID)
			assert.Equal(t, testReference, tt.host.dispatched[0].reference)
			assert.Equal(t, tt.path, tt.host.dispatched[0].segments)
		})
	}
}

func TestLaunchMediaDispatchesOnlyCatalogDefinitions(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	game := identity(t, "nes", "Game.nes")
	customRan := false
	custom := func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
		customRan = true
		return nil, nil //nolint:nilnil // a fire-and-forget launch has no process
	}

	err := platform.LaunchMedia(&config.Instance{}, game,
		&platforms.Launcher{ID: "Custom.Script", SystemID: "NES", Launch: custom}, nil, nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	assert.Empty(t, host.dispatched)

	// A custom launcher that borrows a catalog ID gets the catalog's
	// definition and none of its own behaviour.
	err = platform.LaunchMedia(&config.Instance{}, game,
		&platforms.Launcher{ID: "RetroArch.Mesen", SystemID: "NES", Launch: custom}, nil, nil)
	require.NoError(t, err)
	assert.False(t, customRan)
	require.Len(t, host.dispatched, 1)
	assert.Equal(t, platform.entryByID["RetroArch.Mesen"].definition, host.dispatched[0].definition)
	assert.Equal(t, testReference, host.dispatched[0].reference)
	assert.Equal(t, []string{"nes", "Game.nes"}, host.dispatched[0].segments)
}

func TestLaunchMediaReportsRepairAdvice(t *testing.T) {
	t.Parallel()

	game := []string{"nes", "Game.nes"}
	tests := []struct {
		host *fakeHost
		name string
		want string
		path []string
	}{
		{
			name: "unavailable target", path: game,
			host: &fakeHost{failures: map[string]FailureReason{retroArchPackage: FailureStorageDenied}},
			want: repairMessage(FailureStorageDenied, ""),
		},
		{
			name: "host refuses the dispatch", path: game,
			host: &fakeHost{dispatchErr: &HostError{Reason: FailureForegroundRequired}},
			want: repairMessage(FailureForegroundRequired, ""),
		},
		{
			name: "untyped host error", path: game,
			host: &fakeHost{dispatchErr: errors.New("binder died")},
			want: repairMessage(FailureHostUnavailable, ""),
		},
		{
			name: "receipt names another component", path: game,
			host: &fakeHost{receipt: &DispatchReceipt{Package: "org.example.other"}},
			want: msgReceiptMismatch,
		},
		{
			name: "document no longer exists", path: []string{"nes", "Gone.nes"},
			host: &fakeHost{},
			want: repairMessage(FailureSourceUnavailable, ""),
		},
		{
			name: "media folder no longer granted", path: game,
			host: &fakeHost{references: []string{"content://org.example.documents/tree/other"}},
			want: repairMessage(FailureSourceUnavailable, ""),
		},
		{
			name: "media folders cannot be listed", path: game,
			host: &fakeHost{foldersErr: errors.New("provider crashed")},
			want: repairMessage(FailureSourceUnavailable, ""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			platform := startedPlatform(t.Context(), t, tt.host)
			err := platform.LaunchMedia(&config.Instance{}, identity(t, tt.path...),
				&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil)
			var repair *platforms.LaunchRepairError
			require.ErrorAs(t, err, &repair)
			assert.Equal(t, tt.want, repair.Error())
		})
	}
}

func TestLaunchMediaStopsWhenLaunchContextIsCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	host := &fakeHost{}
	platform := startedPlatform(ctx, t, host)

	err := platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Game.nes"),
		&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, host.dispatched)
}

func TestSourceRootsReadHostMediaFolders(t *testing.T) {
	t.Parallel()

	other := "content://org.example.documents/tree/other"
	host := &fakeHost{references: []string{testReference, "", testReference, other}}
	platform := startedPlatform(t.Context(), t, host)

	roots, err := platform.SourceRoots(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{platforms.SourceRootPath(testReference), platforms.SourceRootPath(other)}, roots,
		"empty and repeated references are dropped")

	entries, err := platform.ReadSourceDir(t.Context(), roots[0])
	require.NoError(t, err)
	assert.Equal(t, []platforms.SourceEntry{
		{Name: "nes", Dir: true, Size: -1}, {Name: "psx", Dir: true, Size: -1},
	}, entries)
	entries, err = platform.ReadSourceDir(t.Context(), identity(t, "nes"))
	require.NoError(t, err)
	assert.Equal(t, []platforms.SourceEntry{{Name: "Game.nes", Size: 16}}, entries)

	var hostErr *HostError
	_, err = platform.ReadSourceDir(t.Context(), roots[1])
	require.ErrorAs(t, err, &hostErr, "the host's reason reaches the indexer")
	_, err = platform.ReadSourceDir(t.Context(), "/storage/emulated/0/nes")
	require.ErrorIs(t, err, platforms.ErrNotSourcePath)
	_, err = platform.ReadSourceDir(t.Context(), platforms.SourceRootPath("content://never/granted"))
	require.ErrorIs(t, err, errFolderUnavailable)

	host.foldersErr = errors.New("provider crashed")
	_, err = platform.SourceRoots(t.Context())
	require.ErrorIs(t, err, host.foldersErr)
}

// A launch after a restart names a root the platform has not listed yet; it
// lists the host's folders once and then reuses what it learned.
func TestLaunchListsMediaFoldersOnce(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	for range 2 {
		require.NoError(t, platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Game.nes"),
			&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil))
	}
	assert.Equal(t, 1, host.listings)
	assert.Len(t, host.dispatched, 2)
}

func TestRepairMessagesCoverEveryReason(t *testing.T) {
	t.Parallel()

	for _, reason := range append(declaredFailureReasons(t), "unheard-of") {
		message := repairMessage(reason, "install it")
		assert.NotEmpty(t, message, reason)
		assert.Equal(t, message, platforms.NewLaunchRepairError(message).Error(), "message must pass the repair filter")
	}
	assert.Equal(t, "install it", repairMessage(FailureNotInstalled, "install it"))
	assert.Equal(t, "install it", repairMessage(FailureActivityUnavailable, "install it"))
	// Without a definition's own install hint the fallback must still read.
	assert.Equal(t, msgNotInstalled, repairMessage(FailureNotInstalled, ""))
	assert.Equal(t, msgNotInstalled, repairMessage(FailureActivityUnavailable, ""))
	assert.Equal(t, FailureHostUnavailable, failureReason(errors.New("plain")))
	assert.Equal(t, FailureRefused, failureReason(&HostError{Reason: FailureRefused}))
	assert.Equal(t, "android host: refused", (&HostError{Reason: FailureRefused}).Error())
}

// TestDispatchDefinitionIsNotHostWritable proves the receipt check cannot be
// defeated by the host it checks. The definition handed to Dispatch must be a
// copy, or a host that rewrites it both substitutes a component undetected and
// leaves the catalog entry corrupted for every later launch.
func TestDispatchDefinitionIsNotHostWritable(t *testing.T) {
	t.Parallel()
	host := &fakeHost{substitute: "org.example.substituted"}
	platform := startedPlatform(t.Context(), t, host)
	before := platform.entryByID["RetroArch.Mesen"].definition

	err := platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Game.nes"),
		&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil)

	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, err, &repair, "a substituted component must be reported")
	assert.Equal(t, msgReceiptMismatch, repair.Error())
	assert.Equal(t, platforms.LaunchRepairOutcomeUnknown, repair.Reason())
	assert.Equal(t, before, platform.entryByID["RetroArch.Mesen"].definition,
		"the catalog entry must be unchanged after the host wrote to what it was given")
	assert.Equal(t, retroArchPackage, platform.entryByID["RetroArch.Mesen"].definition.Package)
}

// TestStopDropsLauncherContexts covers a host that reuses one Platform across
// starts: the previous run's context is cancelled, so a launch between the API
// coming up and StartPost must be refused rather than dispatched against it.
func TestStopDropsLauncherContexts(t *testing.T) {
	t.Parallel()
	host := &fakeHost{}
	ctx, cancel := context.WithCancel(t.Context())
	platform := startedPlatform(ctx, t, host)
	require.NoError(t, platform.Stop())
	cancel()

	err := platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Game.nes"),
		&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil)

	require.ErrorIs(t, err, platforms.ErrNotSupported)
	assert.Empty(t, host.dispatched, "no launch may reach the host after a stop")
}

// TestSourceFailureKeepsTheHostReason covers the two source failures a user can
// act on. Flattening them to "unavailable" costs the only advice that helps.
func TestSourceFailureKeepsTheHostReason(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err        error
		name       string
		wantReason platforms.LaunchRepairReason
		want       FailureReason
	}{
		{
			name: "revoked grant", err: &HostError{Reason: FailureStorageDenied},
			want: FailureStorageDenied, wantReason: platforms.LaunchRepairStoragePermissionRequired,
		},
		{
			name: "card removed", err: &HostError{Reason: FailureStorageUnmounted},
			want: FailureStorageUnmounted, wantReason: platforms.LaunchRepairStorageUnavailable,
		},
		{
			name: "untyped failure stays a source failure", err: errors.New("provider crashed"),
			want: FailureSourceUnavailable, wantReason: platforms.LaunchRepairMediaUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeHost{foldersErr: test.err}
			platform := startedPlatform(t.Context(), t, host)

			err := platform.LaunchMedia(&config.Instance{}, identity(t, "nes", "Game.nes"),
				&platforms.Launcher{ID: "RetroArch.Mesen"}, nil, nil)

			var repair *platforms.LaunchRepairError
			require.ErrorAs(t, err, &repair)
			assert.Equal(t, repairMessage(test.want, ""), repair.Error())
			assert.Equal(t, test.wantReason, repair.Reason())
		})
	}
}

// Listing launchers and launching share one host report: a launch sweeps the
// host at most once, and a second launch inside the TTL does not sweep it at
// all.
//
//nolint:paralleltest // replaces the shared launcher cache
func TestHostSnapshotIsSharedAcrossListingAndLaunch(t *testing.T) {
	host := &fakeHost{scanned: true, cores: []string{"mesen_libretro_android.so"}}
	platform := startedPlatform(t.Context(), t, host)
	current := time.Unix(1000, 0)
	platform.clock = func() time.Time { return current }
	useLauncherCache(t, platform)
	cores, _, inspections := host.calls()
	require.Equal(t, 1, cores, "building the launcher cache sweeps the host once")

	game := identity(t, "nes", "Game.nes")
	mesen := launcherByID(t, platform.Launchers(nil), "RetroArch.Mesen")
	require.NoError(t, platform.LaunchMedia(&config.Instance{}, game, mesen, nil, nil))
	require.NoError(t, platform.LaunchMedia(&config.Instance{}, game, mesen, nil, nil))
	gotCores, _, gotInspections := host.calls()
	assert.Equal(t, cores, gotCores, "launches inside the TTL reuse the report")
	assert.Equal(t, inspections, gotInspections, "targets are not inspected again")

	current = current.Add(hostSnapshotTTL)
	platform.Launchers(nil)
	gotCores, _, _ = host.calls()
	assert.Equal(t, cores+1, gotCores, "an expired report is swept again")

	platform.InvalidateHostSnapshot()
	platform.Launchers(nil)
	platform.Launchers(nil)
	gotCores, _, _ = host.calls()
	assert.Equal(t, cores+2, gotCores, "an invalidated report is swept again, once")
}

// A package change the host reports reaches the next listing only through
// invalidation, which the embedding host triggers.
func TestInvalidateHostSnapshotPicksUpInstalledPackages(t *testing.T) {
	t.Parallel()
	host := &fakeHost{failures: map[string]FailureReason{
		"com.github.stenzek.duckstation": FailureNotInstalled,
	}}
	platform := startedPlatform(t.Context(), t, host)
	assert.False(t, launcherByID(t, platform.Launchers(nil), "DuckStation.PSX").Available)

	host.mu.Lock()
	host.failures = nil
	host.mu.Unlock()
	assert.False(t, launcherByID(t, platform.Launchers(nil), "DuckStation.PSX").Available,
		"the memoised report still stands")
	platform.InvalidateHostSnapshot()
	assert.True(t, launcherByID(t, platform.Launchers(nil), "DuckStation.PSX").Available)
}

// launchers.refresh must not wait out the host snapshot TTL to see a change.
func TestRefreshLauncherDependenciesInvalidatesTheSnapshot(t *testing.T) {
	t.Parallel()
	host := &fakeHost{failures: map[string]FailureReason{
		"com.github.stenzek.duckstation": FailureNotInstalled,
	}}
	platform := startedPlatform(t.Context(), t, host)
	assert.False(t, launcherByID(t, platform.Launchers(nil), "DuckStation.PSX").Available)

	host.mu.Lock()
	host.failures = nil
	host.mu.Unlock()
	require.NoError(t, platform.RefreshLauncherDependencies())
	assert.True(t, launcherByID(t, platform.Launchers(nil), "DuckStation.PSX").Available)
}
