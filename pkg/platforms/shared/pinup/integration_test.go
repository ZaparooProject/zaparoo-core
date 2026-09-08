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
package pinup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fakeMenuPID     = 100
	fakeServerPID   = 101
	fakeEmulatorPID = 200
	testPoll        = 500 * time.Millisecond
)

// fakeLister is a scriptable process table.
type fakeLister struct {
	procs []ProcessInfo
	mu    syncutil.Mutex
}

func (f *fakeLister) List() ([]ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ProcessInfo(nil), f.procs...), nil
}

func (f *fakeLister) add(pid int, exe string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.procs = append(f.procs, ProcessInfo{PID: pid, Exe: exe})
}

func (f *fakeLister) remove(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.procs[:0]
	for _, p := range f.procs {
		if p.PID != pid {
			kept = append(kept, p)
		}
	}
	f.procs = kept
}

func (f *fakeLister) has(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.procs {
		if p.PID == pid {
			return true
		}
	}
	return false
}

// fakePopper stands in for PinUpMenu.exe, PuPServer.exe and SendPuPEvent.exe.
// Starting the menu or server adds their processes; the web remote answers
// only while the server is "up"; a launch spawns the emulator process and an
// exit event removes it, unless the test disables that.
type fakePopper struct {
	srv           *httptest.Server
	lister        *fakeLister
	serverPorts   []int
	events        []int
	launches      []int
	mu            syncutil.Mutex
	nextEmuPID    atomic.Int32
	serverUp      atomic.Bool
	ignoreExit    atomic.Bool
	ignoreExits   atomic.Int32
	dropLaunches  atomic.Int32
	spawnOnLaunch atomic.Bool
	menuStarts    atomic.Int32
}

func newFakePopper(t *testing.T, lister *fakeLister) *fakePopper {
	t.Helper()
	fp := &fakePopper{lister: lister}
	fp.spawnOnLaunch.Store(true)
	fp.nextEmuPID.Store(fakeEmulatorPID)
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fp.serverUp.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.URL.Path == "/function/getcuritem":
		case strings.HasPrefix(r.URL.Path, "/function/launchgame/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/function/launchgame/"))
			fp.mu.Lock()
			fp.launches = append(fp.launches, id)
			fp.mu.Unlock()
			if fp.dropLaunches.Load() > 0 {
				fp.dropLaunches.Add(-1)
			} else if fp.spawnOnLaunch.Load() {
				fp.lister.add(int(fp.nextEmuPID.Add(1)-1), "VPinballX.exe")
			}
		case strings.HasPrefix(r.URL.Path, "/pupkey/"):
			id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/pupkey/"))
			fp.recordEvent(id)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

func (fp *fakePopper) recordEvent(event int) {
	fp.mu.Lock()
	fp.events = append(fp.events, event)
	fp.mu.Unlock()
	if event == EventEmuExit && fp.ignoreExits.Load() > 0 {
		fp.ignoreExits.Add(-1)
		return
	}
	if event == EventEmuExit && !fp.ignoreExit.Load() {
		procs, _ := fp.lister.List()
		for _, p := range procs {
			if strings.HasPrefix(strings.ToLower(p.Exe), "vpinballx") {
				fp.lister.remove(p.PID)
			}
		}
	}
}

func (fp *fakePopper) StartMenu(_ context.Context, _ *Install) error {
	fp.menuStarts.Add(1)
	fp.lister.add(fakeMenuPID, menuExeName)
	return nil
}

func (fp *fakePopper) StartServer(_ context.Context, _ *Install, port int) error {
	fp.mu.Lock()
	fp.serverPorts = append(fp.serverPorts, port)
	fp.mu.Unlock()
	fp.lister.add(fakeServerPID, serverExeName)
	fp.serverUp.Store(true)
	return nil
}

func (fp *fakePopper) launched() []int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]int(nil), fp.launches...)
}

func (fp *fakePopper) sentEvents() []int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]int(nil), fp.events...)
}

func (fp *fakePopper) startedServerOn() []int {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]int(nil), fp.serverPorts...)
}

// rewriteTransport sends every request to the fake web remote whatever host
// and port the integration addressed, so probes of Popper's port 80 and of
// Core's own port both reach the fake.
type rewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	return rt.base.RoundTrip(clone) //nolint:wrapcheck // Test transport passes the real error through.
}

// platformHooks records what the integration hands to the platform.
type platformHooks struct {
	media     *models.ActiveMedia
	mediaSets int
	mu        syncutil.Mutex
}

func (h *platformHooks) activeMedia() *models.ActiveMedia {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.media
}

func (h *platformHooks) setActiveMedia(media *models.ActiveMedia) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.media = media
	h.mediaSets++
}

// harness is one integration under test with every boundary faked.
type harness struct {
	integ  *Integration
	popper *fakePopper
	lister *fakeLister
	hooks  *platformHooks
	clock  *clockwork.FakeClock
	dir    string
}

type harnessOptions struct {
	menuRunning   bool
	withServerExe bool
}

func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, menuExeName), []byte("exe"), 0o600))
	require.NoError(t, openFixture(t, filepath.Join(dir, databaseName), false).Close())
	if opts.withServerExe {
		require.NoError(t, os.WriteFile(filepath.Join(dir, serverExeName), []byte("exe"), 0o600))
	}

	lister := &fakeLister{}
	if opts.menuRunning {
		lister.add(fakeMenuPID, menuExeName)
	}
	popper := newFakePopper(t, lister)
	target, err := url.Parse(popper.srv.URL)
	require.NoError(t, err)
	client := popper.srv.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: target}
	hooks := &platformHooks{}
	clock := clockwork.NewFakeClock()
	integ := NewIntegration(&Deps{
		ActiveMedia:    hooks.activeMedia,
		SetActiveMedia: hooks.setActiveMedia,
		Frontend:       popper,
		Processes:      lister,
		Clock:          clock,
		HTTP:           client,
		Locator:        Locator{FS: afero.NewOsFs(), Candidates: []string{dir}},
		Timeouts: Timeouts{
			MenuStart: 10 * time.Second, ServerStart: 5 * time.Second, LaunchAccept: 4 * time.Second,
			LaunchRetry: 2 * time.Second, StopWait: 3 * time.Second, ExitRetry: time.Second,
			LaunchSettle: time.Second, Poll: testPoll,
		},
	})
	t.Cleanup(integ.Stop)
	return &harness{integ: integ, popper: popper, lister: lister, hooks: hooks, clock: clock, dir: dir}
}

// serverRunning makes PuPServer.exe appear as already running and answering,
// the state Popper leaves when its own web remote option is on.
func (h *harness) serverRunning() {
	h.popper.serverUp.Store(true)
	h.lister.add(fakeServerPID, serverExeName)
}

// tick lets the waiting goroutines see one poll interval pass.
func (h *harness) tick(t *testing.T, waiters int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, h.clock.BlockUntilContext(ctx, waiters))
	h.clock.Advance(testPoll)
}

// driveUntil advances the fake clock one poll at a time, whenever the
// expected waiters are parked on it, until the operation reports back.
func (h *harness) driveUntil(t *testing.T, done <-chan error, waiters int) error {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case err := <-done:
			return err
		case <-deadline:
			t.Fatal("operation did not finish")
			return nil
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		blockErr := h.clock.BlockUntilContext(ctx, waiters)
		cancel()
		if blockErr == nil {
			h.clock.Advance(testPoll)
		}
	}
}

func TestIntegrationLaunchAfterShutdown(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	h.integ.Stop()
	require.ErrorIs(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")), context.Canceled)
	require.Empty(t, h.popper.launched(), "shutdown must prohibit new frontend commands")
}

func TestIntegrationLaunchTracksTableLifecycle(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()

	path := TablePath(10, "Attack from Mars")
	require.NoError(t, h.integ.Launch(nil, path))
	assert.Equal(t, []int{10}, h.popper.launched())
	assert.Equal(t, int32(0), h.popper.menuStarts.Load(), "menu was already running")

	require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)
	media := h.hooks.activeMedia()
	require.NotNil(t, media)
	assert.Equal(t, LauncherID, media.LauncherID)
	assert.Equal(t, systemdefs.SystemPinball, media.SystemID)
	assert.Equal(t, path, media.Path)
	assert.Equal(t, "Attack from Mars", media.Name)
	assert.True(t, h.integ.EmulatorRunning())

	// The table exits on its own: the process disappears and the next poll
	// releases the PID and clears the media.
	h.lister.remove(fakeEmulatorPID)
	h.tick(t, 1)
	require.Eventually(t, func() bool { return h.hooks.activeMedia() == nil }, 2*time.Second, 5*time.Millisecond)
	assert.False(t, h.integ.EmulatorRunning())
	require.ErrorIs(t, h.integ.StopTable(), ErrNoActiveTable)
}

func TestIntegrationLaunchStartsMenuAndServer(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{withServerExe: true})

	errCh := make(chan error, 1)
	go func() { errCh <- h.integ.Launch(nil, TablePath(10, "Attack from Mars")) }()

	// StartMenu adds PinUpMenu.exe at once, so the launch only waits out
	// LaunchSettle before starting the web remote and launching.
	require.NoError(t, h.driveUntil(t, errCh, 1))
	assert.Equal(t, int32(1), h.popper.menuStarts.Load(), "Popper was started")
	assert.Equal(t, []int{CoreServerPort}, h.popper.startedServerOn(), "web remote started on Core's port")
	assert.Equal(t, []int{10}, h.popper.launched())
	require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)
}

func TestIntegrationLaunchRefusesUntrackableEmulator(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()

	err := h.integ.Launch(nil, TablePath(20, "Untrackable Table"))
	require.ErrorContains(t, err, "Process Name")
	assert.Empty(t, h.popper.launched())
}

func TestIntegrationLaunchUnknownTable(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()

	require.Error(t, h.integ.Launch(nil, TablePath(999, "Ghost")))
	require.Error(t, h.integ.Launch(nil, "steam://10/x"))
	assert.Empty(t, h.popper.launched())
}

func TestIntegrationLaunchWithoutWebRemote(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true})

	err := h.integ.Launch(nil, TablePath(10, "Attack from Mars"))
	require.ErrorIs(t, err, ErrWebRemoteMissing)
	assert.Empty(t, h.popper.launched())
}

func TestIntegrationLaunchClosesHandStartedTable(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	// A table the user started from the wheel: Core knows nothing about it.
	h.lister.add(150, "VPinballX64.exe")

	require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
	assert.Equal(t, []int{EventEmuExit}, h.popper.sentEvents(), "Popper is asked to close the running table first")
	assert.False(t, h.lister.has(150))
	assert.Equal(t, []int{10}, h.popper.launched())
}

func TestIntegrationLaunchAbortsWhenTableWillNotClose(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	h.popper.ignoreExit.Store(true)
	h.lister.add(150, "VPinballX64.exe")

	errCh := make(chan error, 1)
	go func() { errCh <- h.integ.Launch(nil, TablePath(10, "Attack from Mars")) }()
	err := h.driveUntil(t, errCh, 1)
	require.ErrorIs(t, err, ErrTableRunning)
	assert.GreaterOrEqual(t, len(h.popper.sentEvents()), 2, "Popper is asked more than once while the table loads")
	assert.Empty(t, h.popper.launched())
	assert.True(t, h.lister.has(150), "the emulator is never killed by Core")
}

func TestIntegrationLaunchIsRepeatedWhenPopperDropsIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	h.popper.dropLaunches.Store(1)

	path := TablePath(10, "Attack from Mars")
	require.NoError(t, h.integ.Launch(nil, path))
	// Nothing appears for LaunchRetry, then the request is repeated and the
	// fake starts the emulator.
	for len(h.popper.launched()) < 2 {
		h.tick(t, 1)
	}
	require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, []int{10, 10}, h.popper.launched())
	assert.Equal(t, path, h.hooks.activeMedia().Path)
}

func TestIntegrationStopPendingLaunch(t *testing.T) {
	t.Parallel()

	for _, lateProcess := range []bool{false, true} {
		t.Run(strconv.FormatBool(lateProcess), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
			h.serverRunning()
			h.popper.spawnOnLaunch.Store(false)

			require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, h.clock.BlockUntilContext(ctx, 1))
			h.integ.mu.Lock()
			key := h.integ.active
			h.integ.mu.Unlock()
			require.NoError(t, h.integ.StopTable())
			assert.False(t, h.integ.retryLaunch(key, h.integ.currentRemote()), "a queued retry must recheck ownership")

			// A replacement launcher now owns media. Neither a retry nor a
			// delayed emulator appearance may resurrect the stopped launch.
			replacement := &models.ActiveMedia{Path: "steam://123/Replacement"}
			h.hooks.setActiveMedia(replacement)
			if lateProcess {
				h.lister.add(fakeEmulatorPID, "VPinballX.exe")
			}
			finished := make(chan error, 1)
			go func() {
				h.integ.wg.Wait()
				finished <- nil
			}()
			require.NoError(t, h.driveUntil(t, finished, 1))
			assert.Equal(t, []int{10}, h.popper.launched(), "stopped launches must not be retried")
			assert.Same(t, replacement, h.hooks.activeMedia())
			require.ErrorIs(t, h.integ.StopTable(), ErrNoActiveTable)
		})
	}
}

func TestIntegrationLaunchAcceptTimeoutPublishesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	h.popper.spawnOnLaunch.Store(false)

	require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
	released := make(chan error, 1)
	go func() {
		h.integ.wg.Wait()
		released <- nil
	}()
	require.NoError(t, h.driveUntil(t, released, 1), "the claim is released once the emulator never appears")
	require.ErrorIs(t, h.integ.StopTable(), ErrNoActiveTable)
	assert.Nil(t, h.hooks.activeMedia())
}

func TestIntegrationStaleExitDoesNotClearReplacement(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()

	first := TablePath(10, "Attack from Mars")
	require.NoError(t, h.integ.Launch(nil, first))
	require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)

	// The first table exits and a second VPX table is launched before the
	// first watcher has polled, so the stale exit lands after the replacement
	// has published its media.
	h.lister.remove(fakeEmulatorPID)
	second := TablePath(19, "Null Visible")
	require.NoError(t, h.integ.Launch(nil, second))
	require.Eventually(t, func() bool {
		media := h.hooks.activeMedia()
		return media != nil && media.Path == second
	}, 2*time.Second, 5*time.Millisecond)

	// The first watcher observes its process gone on the next poll; the
	// replacement's media must survive that stale exit.
	h.tick(t, 2)
	h.tick(t, 1)
	media := h.hooks.activeMedia()
	require.NotNil(t, media, "the replacement's media survives the stale exit")
	assert.Equal(t, second, media.Path)
	assert.True(t, h.integ.EmulatorRunning())
}

func TestIntegrationStopTable(t *testing.T) {
	t.Parallel()

	t.Run("popper closes the table", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
		h.serverRunning()
		require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
		require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)

		require.NoError(t, h.integ.StopTable())
		assert.Equal(t, []int{EventEmuExit}, h.popper.sentEvents())
		assert.NotNil(t, h.hooks.activeMedia(), "StopTable leaves media for the platform to clear")
	})

	t.Run("exit request is repeated while the table loads", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
		h.serverRunning()
		h.popper.ignoreExits.Store(2)
		require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
		require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)

		errCh := make(chan error, 1)
		go func() { errCh <- h.integ.StopTable() }()
		require.NoError(t, h.driveUntil(t, errCh, 2))
		assert.Equal(t, []int{EventEmuExit, EventEmuExit, EventEmuExit}, h.popper.sentEvents(),
			"two dropped requests, the third closes the table")
	})

	t.Run("table ignores the exit request", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
		h.serverRunning()
		h.popper.ignoreExit.Store(true)
		require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
		require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)

		errCh := make(chan error, 1)
		go func() { errCh <- h.integ.StopTable() }()
		// Two waiters: the watcher following the process and StopTable.
		err := h.driveUntil(t, errCh, 2)
		require.ErrorIs(t, err, ErrTableRunning)
		assert.NotNil(t, h.hooks.activeMedia(), "media is kept when the stop is not confirmed")
		assert.True(t, h.integ.EmulatorRunning())
	})

	t.Run("nothing active", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
		require.ErrorIs(t, h.integ.StopTable(), ErrNoActiveTable)
	})
}

func TestIntegrationStopForgetsTables(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{menuRunning: true, withServerExe: true})
	h.serverRunning()
	require.NoError(t, h.integ.Launch(nil, TablePath(10, "Attack from Mars")))
	require.Eventually(t, func() bool { return h.hooks.activeMedia() != nil }, 2*time.Second, 5*time.Millisecond)

	h.integ.Stop()
	require.ErrorIs(t, h.integ.StopTable(), ErrNoActiveTable)
	assert.False(t, h.integ.EmulatorRunning())
}

func TestIntegrationScanAndAvailability(t *testing.T) {
	t.Parallel()

	t.Run("installed", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, harnessOptions{})
		require.NoError(t, h.integ.Available(nil))
		results, err := h.integ.Scan(context.Background(), nil)
		require.NoError(t, err)
		assert.Len(t, results, 7)
		assert.Equal(t, TablePath(10, "Attack from Mars"), results[0].Path)
	})

	t.Run("not installed", func(t *testing.T) {
		t.Parallel()
		integ := NewIntegration(&Deps{
			Locator: Locator{FS: afero.NewMemMapFs(), Candidates: DefaultInstallDirs()},
		})
		t.Cleanup(integ.Stop)
		require.ErrorIs(t, integ.Available(nil), ErrNotInstalled)
		results, err := integ.Scan(context.Background(), nil)
		require.NoError(t, err, "an absent install must not fail the media scan")
		assert.Nil(t, results)
		require.ErrorIs(t, integ.Launch(nil, TablePath(10, "x")), ErrNotInstalled)
	})
}

func TestNewLauncher(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	launcher := NewLauncher(h.integ)

	assert.Equal(t, LauncherID, launcher.ID)
	assert.Equal(t, systemdefs.SystemPinball, launcher.SystemID)
	assert.Equal(t, []string{"popper"}, launcher.Schemes)
	assert.True(t, launcher.SkipFilesystemScan)
	assert.NotNil(t, launcher.Kill)
	assert.NotNil(t, launcher.Availability)
	assert.NotNil(t, launcher.Launch)

	results, err := launcher.Scanner(context.Background(), nil, systemdefs.SystemPinball, nil)
	require.NoError(t, err)
	assert.Len(t, results, 7)

	other, err := launcher.Scanner(context.Background(), nil, systemdefs.SystemArcade, nil)
	require.NoError(t, err)
	assert.Empty(t, other, "the scanner only answers for the Pinball system")

	proc, err := launcher.Launch(nil, "steam://1/x", nil)
	require.Error(t, err)
	assert.Nil(t, proc)
}
