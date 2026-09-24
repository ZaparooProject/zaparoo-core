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
	"io"
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/hostmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testReference = "content://org.example.documents/tree/games"

// fakeHost is an in-memory Host: packages are installed unless listed in
// failures, and one source holds nes/Game.nes and psx/Game.chd.
type fakeHost struct {
	failures    map[string]FailureReason
	dispatchErr error
	openErr     error
	substitute  string
	receipt     *DispatchReceipt
	cores       []string
	inspections []string
	dispatched  []dispatchCall
	sessions    []*fakeSession
	mu          syncutil.Mutex
	scanned     bool
}

type dispatchCall struct {
	document   hostmedia.Document
	definition LaunchDefinition
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
	return h.cores, h.scanned
}

func (h *fakeHost) OpenDocuments(context.Context) (DocumentSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.openErr != nil {
		return nil, h.openErr
	}
	session := &fakeSession{host: h}
	h.sessions = append(h.sessions, session)
	return session, nil
}

type fakeSession struct {
	host      *fakeHost
	closed    bool
	cancelled bool
}

func (*fakeSession) Sources(context.Context) ([]hostmedia.Source, error) {
	return []hostmedia.Source{{
		Reference: testReference, Provider: "org.example.documents",
		Root: hostmedia.Entry{ID: "root", Name: "games", Directory: true},
	}}, nil
}

func (*fakeSession) Children(_ context.Context, _, document string) (hostmedia.Directory, error) {
	tree := map[string][]hostmedia.Entry{
		"root": {{ID: "nes", Name: "nes", Directory: true}, {ID: "psx", Name: "psx", Directory: true}},
		"nes":  {{ID: "nes-game", Name: "Game.nes", Size: 16}},
		"psx":  {{ID: "psx-game", Name: "Game.chd", Size: 16}},
	}
	return &fakeDirectory{entries: tree[document]}, nil
}

func (*fakeSession) Open(context.Context, string, string) (hostmedia.ReadSeekCloser, error) {
	return nil, hostmedia.ErrUnavailable
}

func (s *fakeSession) Close() error {
	s.host.mu.Lock()
	defer s.host.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSession) Dispatch(definition *LaunchDefinition, document hostmedia.Document) (DispatchReceipt, error) {
	s.host.mu.Lock()
	defer s.host.mu.Unlock()
	s.host.dispatched = append(s.host.dispatched, dispatchCall{definition: *definition, document: document})
	if s.host.substitute != "" {
		// A host that writes through the pointer it was handed, and reports
		// the component it actually started.
		definition.Package = s.host.substitute
		if len(definition.Extensions) > 0 {
			definition.Extensions[0] = ".substituted"
		}
	}
	if s.host.dispatchErr != nil {
		return DispatchReceipt{}, s.host.dispatchErr
	}
	if s.host.receipt != nil {
		return *s.host.receipt, nil
	}
	return DispatchReceipt{
		Package: definition.Package, Activity: definition.Activity, Strategy: definition.Strategy,
	}, nil
}

func (s *fakeSession) CancelDispatch() error {
	s.host.mu.Lock()
	defer s.host.mu.Unlock()
	s.cancelled = true
	return nil
}

type fakeDirectory struct {
	entries []hostmedia.Entry
	done    bool
}

func (d *fakeDirectory) Next(context.Context) ([]hostmedia.Entry, error) {
	if d.done || len(d.entries) == 0 {
		return nil, io.EOF
	}
	d.done = true
	return d.entries, nil
}

func (*fakeDirectory) Close() error { return nil }

type fixedContext struct{ ctx context.Context }

func (c fixedContext) GetContext() context.Context { return c.ctx }
func (c fixedContext) NewContext() context.Context { return c.ctx }

func identity(t *testing.T, parts ...string) string {
	t.Helper()
	value, err := sourcepath.Format(sourcepath.ID(testReference), parts)
	require.NoError(t, err)
	return value
}

func startedPlatform(ctx context.Context, t *testing.T, host Host) *Platform {
	t.Helper()
	platform, err := newPlatform(platforms.Settings{DataDir: "/data"}, host, afero.NewMemMapFs())
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
	platform, err := newPlatform(settings, nil, afero.NewMemMapFs())
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
	_, err = platform.OpenMediaScan(t.Context())
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	err = platform.LaunchMedia(nil, "source://x/nes/Game.nes", nil, nil, nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
}

func TestLaunchersFollowCatalogOrderAndShareInspections(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)
	launchers := platform.Launchers(nil)

	require.Len(t, launchers, len(platform.entries))
	for i := range launchers {
		assert.Equal(t, platform.entries[i].definition.ID, launchers[i].ID)
		assert.Equal(t, []string{"source"}, launchers[i].Schemes)
		assert.True(t, launchers[i].SkipFilesystemScan)
		assert.Equal(t, platforms.LifecycleExternal, launchers[i].Lifecycle)
		assert.NotEmpty(t, launchers[i].SystemID)
	}
	assert.ElementsMatch(t, []string{
		"com.github.stenzek.duckstation", "org.ppsspp.ppsspp", "org.dolphinemu.dolphinemu", retroArchPackage,
	}, host.inspections, "each target is inspected once per sweep, not once per profile")

	mesen := launcherByID(t, launchers, "RetroArch.Mesen")
	assert.Equal(t, []string{"RetroArch"}, mesen.Groups)
	assert.True(t, mesen.Test(nil, identity(t, "nes", "Game.nes")))
	assert.False(t, mesen.Test(nil, identity(t, "snes", "Game.nes")))
	assert.False(t, mesen.Test(nil, "/storage/emulated/0/nes/Game.nes"))
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

	require.NoError(t, mesen.Preflight(nil, game, nil))
	require.EqualError(t, launcherByID(t, launchers, "RetroArch.FCEUmm").Preflight(nil, game, nil), msgCoreMissing)
	require.EqualError(t, mesen.Preflight(nil, identity(t, "snes", "Game.sfc"), nil), msgWrongMedia)
	require.EqualError(t, mesen.Preflight(nil, game, &platforms.LaunchOptions{SetName: "other"}),
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
			assert.Equal(t, testReference, tt.host.dispatched[0].document.Reference)
			require.Len(t, tt.host.sessions, 1)
			assert.True(t, tt.host.sessions[0].closed, "the document session is always released")
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
	assert.Equal(t, hostmedia.Document{Reference: testReference, ID: "nes-game"}, host.dispatched[0].document)
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
			name: "documents cannot be opened", path: game,
			host: &fakeHost{openErr: hostmedia.ErrUnavailable},
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
			for _, session := range tt.host.sessions {
				assert.True(t, session.closed)
			}
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

func TestOpenMediaScanListsGrantedSystems(t *testing.T) {
	t.Parallel()

	host := &fakeHost{}
	platform := startedPlatform(t.Context(), t, host)

	scan, err := platform.OpenMediaScan(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"NES", "PSX"}, scan.Systems())
	require.NoError(t, scan.Close(false))
	require.Len(t, host.sessions, 1)
	assert.True(t, host.sessions[0].closed)

	host.openErr = hostmedia.ErrUnavailable
	_, err = platform.OpenMediaScan(t.Context())
	require.ErrorIs(t, err, hostmedia.ErrUnavailable)
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
			name: "untyped failure stays a source failure", err: hostmedia.ErrUnavailable,
			want: FailureSourceUnavailable, wantReason: platforms.LaunchRepairMediaUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			host := &fakeHost{openErr: test.err}
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
