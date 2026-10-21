//go:build linux && !android

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

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/gamehandoff"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// FPGA cores take their game from MiSTer's Main either as a file Main
	// streams into the core (MGL type "f") or as a disk image the core reads
	// on demand (type "s"). A streamed file is closed when the transfer ends;
	// a mounted image stays open for the whole session.
	mglTypeFile = "f"

	// readySettle is how long after the game file goes quiet a core is given
	// to start running it before input and cheats are safe.
	readySettle = 500 * time.Millisecond
	// readyMountSettle is the same for a mounted image, counted from the mount
	// since there is no close to wait for.
	readyMountSettle = 1000 * time.Millisecond

	// readyOpenWindow is how long a probe waits for Main to open the game at
	// all. Measured opens come 1.9 to 5.1 s after a launch; a game still
	// unopened after this is one the core reads some other way, and waiting
	// on it would only stall scripts. It must stay under the service's
	// readiness timeout or the wait ends there first.
	readyOpenWindow = 8 * time.Second
	// readyIdleHandover is how long a streamed game may sit open and unread
	// before it counts as handed over. A core that mounts a game it was
	// expected to stream, such as the Amiga's floppy images, holds the file
	// open for the whole session and never closes it. Reads of a streamed
	// game arrive every few milliseconds until it closes.
	readyIdleHandover = 2 * time.Second
	// readyArcadeBase and readyArcadePerMB bound the wait for an arcade set,
	// whose ROMs take seconds to load: measured from launch to the core
	// starting, 2 MB took 5.8 s, 15 MB 11.6 s, 22 MB 17.2 s and 30 MB 17.2 s,
	// about 0.5 s per MB. The wait ends when the core starts; this only bounds
	// a load that hangs.
	readyArcadeBase  = 15 * time.Second
	readyArcadePerMB = time.Second
	readyArcadeMax   = 3 * time.Minute
	// readyPrimeKey is tapped before a script's first typed input after a core
	// loads. Main sets up the virtual keyboard when its first key arrives after
	// a core loads, and drops the first one or two keys while it does: typing
	// "abc" into a C64 that had just booted produced "BC". An inert modifier
	// tap takes that loss instead of the script's first characters.
	readyPrimeKey = "{shift}"
	// readyPrimeSettle is how long Main is given to finish that setup. Typing
	// 0.3 s after the tap arrived complete; 0 s lost the first key.
	readyPrimeSettle = 400 * time.Millisecond
	// readyLaunchFreshness is how long a launch stays the one a probe belongs
	// to. A probe starts as the launch returns.
	readyLaunchFreshness = 30 * time.Second

	// readyCorePoll is how often an arcade launch is checked for its core to
	// have started, since Main exposes that only through the CORENAME file.
	readyCorePoll = 100 * time.Millisecond
)

// errNoGameFile means the launched path has no file to watch, such as a
// launch through an MGL that names no game.
var errNoGameFile = errors.New("launch has no game file to watch")

// streamedLaunchers lists launchers whose core streams the game although the
// catalog entry they share describes it as mounted.
//
//nolint:gochecknoglobals // static table
var streamedLaunchers = map[string]bool{
	"HybridDVDPlayer": true,
}

// coreAliases names the catalog core that loads a launcher's media when the
// launcher's own system has no catalog entry.
//
//nolint:gochecknoglobals // static table
var coreAliases = map[string]string{
	"NeoGeoMVS": "NeoGeo",
}

// coreReadiness decides when a launched FPGA core is ready for input and
// cheats. Each field is replaceable so tests need no hardware.
type coreReadiness struct {
	clock    clockwork.Clock
	launches *gamehandoff.LaunchLog
	watch    func(ctx context.Context, path string) (<-chan gamehandoff.Kind, error)
	stat     func(path string) (os.FileInfo, error)
	coreName func() (string, time.Time, error)
	pressKey func(string) error
	// pressSequence types a plan's input. It is the platform's own sequence
	// method, which primes the virtual keyboard first.
	pressSequence func([]string, time.Duration) error
	primedAt      time.Time // core load time the keyboard was last primed for
	plans         loadPlans
	keyMu         syncutil.Mutex
}

// launchReadiness is shared by every launcher list. Launchers are rebuilt on
// each Launchers call, so the launch a probe belongs to is recorded in a
// different list than the one the service later asks for the probe.
//
//nolint:gochecknoglobals // one launch is in flight at a time across all rebuilt launcher lists
var launchReadiness = newCoreReadiness()

func newCoreReadiness() *coreReadiness {
	clock := clockwork.NewRealClock()
	return &coreReadiness{
		clock:    clock,
		launches: gamehandoff.NewLaunchLog(clock),
		watch:    gamehandoff.Watch,
		stat:     os.Stat,
		coreName: func() (string, time.Time, error) {
			data, err := os.ReadFile(misterconfig.CoreNameFile)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("read core name: %w", err)
			}
			info, err := os.Stat(misterconfig.CoreNameFile)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("stat core name: %w", err)
			}
			return strings.TrimSpace(string(data)), info.ModTime(), nil
		},
	}
}

// withCoreReadiness gives every launcher that loads a catalog core, or an MRA,
// a readiness probe. Launchers with neither, such as a script, keep none:
// starting is ready. A launcher that already has a probe is left alone.
func withCoreReadiness(pl platforms.Platform, ls []platforms.Launcher) []platforms.Launcher {
	r := launchReadiness
	r.usePlatform(pl)
	for i := range ls {
		if ls[i].Launch == nil || ls[i].WaitForReady != nil {
			continue
		}
		coreID := ls[i].SystemID
		if alias, ok := coreAliases[coreID]; ok {
			coreID = alias
		}
		core, err := cores.GetCore(coreID)
		if err != nil {
			if !slices.Contains(ls[i].Extensions, ".mra") {
				continue
			}
			core = nil
		}
		ls[i].WaitForReady = r.waitForReady(core, ls[i].ID)
		ls[i].ReadyTimeout = r.readyTimeout
		ls[i].Launch = r.noteLaunches(ls[i].Launch)
	}
	return ls
}

// usePlatform sets the keyboard that primeForInput primes.
func (r *coreReadiness) usePlatform(pl platforms.Platform) {
	if pl == nil {
		return
	}
	r.keyMu.Lock()
	defer r.keyMu.Unlock()
	r.pressKey = pl.KeyboardPress
	r.pressSequence = pressSequenceFor(pl)
}

// primeForInput taps the priming key and gives Main time to finish setting up
// the virtual keyboard, once per core load and only when input is about to be
// typed, so a launch that sends no input never gets a stray key. A keyboard
// that cannot be pressed is no reason to hold the script, so a failed tap is
// only logged.
func (r *coreReadiness) primeForInput(ctx context.Context) error {
	name, loaded, err := r.coreName()
	if err != nil {
		log.Debug().Err(err).Msg("core name unreadable; not priming the virtual keyboard")
		return nil
	}
	if name == "" || strings.EqualFold(name, misterconfig.MenuCore) {
		return nil
	}
	r.keyMu.Lock()
	if !loaded.After(r.primedAt) {
		r.keyMu.Unlock()
		return nil
	}
	r.primedAt = loaded
	press := r.pressKey
	r.keyMu.Unlock()
	if press == nil {
		return nil
	}
	if err := press(readyPrimeKey); err != nil {
		log.Debug().Err(err).Msg("could not prime the virtual keyboard; continuing")
		return nil
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for the virtual keyboard: %w", ctx.Err())
	case <-r.clock.After(readyPrimeSettle):
		return nil
	}
}

// noteLaunches wraps launch so the probe can tell a game Core launched from
// one that was already running when the tracker noticed it, which Main
// finished loading before any probe could watch.
func (r *coreReadiness) noteLaunches(
	launch func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error),
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		r.launches.Note(path)
		return launch(cfg, path, opts)
	}
}

// gameFile returns the file on disk behind a launch path, which for a game
// inside an archive names the archive and the entry.
func (r *coreReadiness) gameFile(path string) (string, error) {
	for p := path; p != "" && p != string(filepath.Separator) && p != "."; p = filepath.Dir(p) {
		info, err := r.stat(p)
		if err == nil && info.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errNoGameFile
}

// readyTimeout extends the service's default bound for an arcade set by the
// size of its ROM archives. Other media keeps the default.
func (r *coreReadiness) readyTimeout(media *models.ActiveMedia) time.Duration {
	if plan, ok := r.plans.peek(media.Path, r.clock.Now()); ok {
		return plan.readyTimeout()
	}
	if !strings.EqualFold(filepath.Ext(media.Path), ".mra") {
		return 0
	}
	mra, err := mgls.ReadMRA(media.Path)
	if err != nil {
		return 0
	}

	counted := map[string]struct{}{}
	var total int64
	for i := range mra.Roms {
		for _, zip := range strings.Split(mra.Roms[i].Zip, "|") {
			if _, seen := counted[zip]; seen || zip == "" {
				continue
			}
			for _, dir := range []string{"mame", "hbmame"} {
				info, statErr := r.stat(filepath.Join(misterconfig.SDRootDir, "games", dir, zip))
				if statErr == nil {
					counted[zip] = struct{}{}
					total += info.Size()
					break
				}
			}
			if _, found := counted[zip]; found {
				break // later names are alternatives to the archive found
			}
		}
	}
	megabytes := time.Duration(total>>20 + 1)
	return min(readyArcadeBase+megabytes*readyArcadePerMB, readyArcadeMax)
}

// target works out which file Main opens for the launch at path and whether it
// streams the file into the core or mounts it. The method comes from the
// launch path's extension; an extension the catalog has no slot for takes the
// core's first slot, and a core with none is treated as mounting.
func (r *coreReadiness) target(core *cores.Core, launcherID, path string) (file string, streamed bool, err error) {
	if strings.EqualFold(filepath.Ext(path), ".mgl") {
		return r.mglTarget(core, path)
	}

	// A game inside an image is not a file of its own: Main mounts the image.
	if plan, ok := r.plans.peek(path, r.clock.Now()); ok && plan.target != path {
		return plan.target, false, nil
	}

	file, err = r.gameFile(path)
	if err != nil {
		return "", false, err
	}
	if streamedLaunchers[launcherID] {
		return file, true, nil
	}
	if def, defErr := cores.PathToMGLDef(core, path); defErr == nil {
		return file, def.Method == mglTypeFile, nil
	}
	for i := range core.Slots {
		if core.Slots[i].Mgl != nil {
			return file, core.Slots[i].Mgl.Method == mglTypeFile, nil
		}
	}
	return file, false, nil
}

// mglTarget reads the game an MGL file loads. Main opens the MGL itself first,
// so the MGL is not the file to watch.
func (r *coreReadiness) mglTarget(core *cores.Core, mglPath string) (file string, streamed bool, err error) {
	mgl, err := mgls.ReadMgl(mglPath)
	if err != nil || mgl.File.Path == "" {
		return "", false, errNoGameFile
	}
	candidates := []string{mgl.File.Path}
	if !filepath.IsAbs(mgl.File.Path) {
		homes := append([]string{core.ID}, core.Folders...)
		candidates = candidates[:0]
		for _, home := range homes {
			candidates = append(candidates, filepath.Join(misterconfig.SDRootDir, "games", home, mgl.File.Path))
		}
	}
	for _, candidate := range candidates {
		if file, err = r.gameFile(candidate); err == nil {
			return file, mgl.File.Type == mglTypeFile, nil
		}
	}
	return "", false, errNoGameFile
}

// waitForReady returns the probe for a launcher that loads core. A nil core is
// for launchers that only ever launch MRAs and have no catalog entry.
func (r *coreReadiness) waitForReady(
	core *cores.Core, launcherID string,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return func(ctx context.Context, _ *config.Instance, media *models.ActiveMedia) error {
		launchedAt, ok := r.launches.Since(media.Path, readyLaunchFreshness)
		if !ok {
			log.Debug().Str("path", media.Path).Msg("media was not launched by Core; treating the core as ready")
			return nil
		}
		if strings.EqualFold(filepath.Ext(media.Path), ".mra") {
			return r.waitForArcade(ctx, media.Path, launchedAt, readySettle)
		}
		if core == nil {
			return nil
		}

		file, streamed, err := r.target(core, launcherID, media.Path)
		if err != nil {
			log.Debug().Err(err).Str("path", media.Path).Msg("no game file to watch; treating the core as ready")
			return nil //nolint:nilerr // a launch with no game file has nothing to wait for
		}
		settle := readySettle
		if !streamed {
			settle = readyMountSettle
		}
		log.Debug().Str("file", file).Bool("streamed", streamed).Dur("settle", settle).
			Msg("waiting for the core to take its game")
		if err := r.waitForGameFile(ctx, file, streamed, settle); err != nil {
			return err
		}
		return r.runPlan(ctx, media.Path)
	}
}

// waitForGameFile waits for Main to hand the file to the core, then for settle
// of quiet. A streamed file is handed over once every open of it has closed; a
// mounted image once it has been opened.
func (r *coreReadiness) waitForGameFile(
	ctx context.Context, file string, streamed bool, settle time.Duration,
) error {
	events, err := r.watch(ctx, file)
	if err != nil {
		return fmt.Errorf("%w: watch game file: %w", platforms.ErrReadinessUnavailable, err)
	}
	if err := gamehandoff.Wait(ctx, r.clock, events, gamehandoff.Options{
		Settle:       settle,
		OpenWindow:   readyOpenWindow,
		UntilClose:   streamed,
		IdleHandover: readyIdleHandover,
	}); err != nil {
		return fmt.Errorf("wait for the core to take %s: %w", file, err)
	}
	return nil
}

// waitForArcade waits for the core an MRA names to be running. An arcade core
// only starts once Main has read the MRA and loaded its ROMs, so a rewrite of
// the core name file after the launch is the signal that loading is done.
// Requiring a rewrite keeps a relaunch of the set already running, whose name
// is unchanged, from counting as ready before the reload. An MRA with no
// setname starts a core named after its RBF, so any core other than the menu
// counts.
func (r *coreReadiness) waitForArcade(
	ctx context.Context, path string, launchedAt time.Time, settle time.Duration,
) error {
	mra, err := mgls.ReadMRA(path)
	if err != nil {
		return fmt.Errorf("%w: read MRA: %w", platforms.ErrReadinessUnavailable, err)
	}

	err = platforms.PollReady(ctx, r.clock, readyCorePoll, func(context.Context) (bool, error) {
		name, written, nameErr := r.coreName()
		if nameErr != nil {
			return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, nameErr)
		}
		if written.Before(launchedAt) {
			return false, nil
		}
		if mra.SetName == "" {
			return name != "" && !strings.EqualFold(name, misterconfig.MenuCore), nil
		}
		return strings.EqualFold(name, mra.SetName), nil
	})
	if err != nil {
		return fmt.Errorf("wait for arcade core: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("settle after arcade core start: %w", ctx.Err())
	case <-r.clock.After(settle):
		return nil
	}
}
