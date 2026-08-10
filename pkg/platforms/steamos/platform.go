//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package steamos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/steamos/steamruntime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type runtimeBroker interface {
	Start(context.Context, *steamruntime.Command) (*os.Process, error)
	Stop(context.Context) error
	Wait(context.Context, int) error
	Available() bool
	HasActive() bool
	Owns(int) bool
	Clear(int) bool
}

// Platform implements the SteamOS platform (Steam Deck and compatible handhelds).
// Uses console-first approach with direct steam command for Game Mode integration.
type Platform struct {
	*linuxbase.Base
	fs                        afero.Fs
	procScanner               *procscanner.Scanner
	steamTracker              *steamtracker.PlatformIntegration
	emuTracker                *EmulatorTracker
	steamRuntime              runtimeBroker
	setActiveMedia            func(*models.ActiveMedia)
	retroArchAppendConfigPath string
}

// NewPlatform creates a new SteamOS platform instance.
func NewPlatform() *Platform {
	return &Platform{
		Base:         linuxbase.NewBase(platformids.SteamOS),
		steamRuntime: steamruntime.NewBroker(),
	}
}

func steamOSSessionEnvOverrides() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "systemctl", "--user", "show-environment").Output()
	if err != nil {
		log.Debug().Err(err).Msg("failed to read current SteamOS session environment")
		return nil
	}

	return parseSteamOSSessionEnv(string(output))
}

func isSteamOSSessionEnvKey(key string) bool {
	switch key {
	case "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_SESSION_TYPE",
		"XDG_CURRENT_DESKTOP", "DESKTOP_SESSION", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS":
		return true
	default:
		return false
	}
}

func parseSteamOSSessionEnv(output string) []string {
	result := make([]string, 0, 8)
	for line := range strings.SplitSeq(output, "\n") {
		key, _, found := strings.Cut(line, "=")
		if found && isSteamOSSessionEnvKey(key) {
			result = append(result, line)
		}
	}
	return result
}

func steamRuntimeEnvOverrides(env []string) []string {
	result := make([]string, 0, len(env))
	for _, line := range env {
		key, _, found := strings.Cut(line, "=")
		if found && !isSteamOSSessionEnvKey(key) {
			result = append(result, line)
		}
	}
	return result
}

func steamOSLaunchEnvOverrides() []string {
	env := steamOSSessionEnvOverrides()
	if display := steamOSGameMode.GamescopeDisplay(); display != "" {
		env = helpers.MergeEnviron(env, []string{"DISPLAY=" + display})
	}
	return env
}

func steamOSLaunchEnv() []string {
	return helpers.MergeEnviron(os.Environ(), steamOSLaunchEnvOverrides())
}

// StartPre writes the Zaparoo-owned native RetroArch profile.
func (p *Platform) StartPre(cfg *config.Instance) error {
	if err := p.Base.StartPre(cfg); err != nil {
		return fmt.Errorf("start SteamOS base: %w", err)
	}
	if err := sharedretroarch.EnsureConfigProfile(
		p.fileSystem(), p.retroArchConfigPath(), sharedretroarch.ConfigProfileLowLatency,
	); err != nil {
		return fmt.Errorf("write native RetroArch config: %w", err)
	}
	if err := ensureNativeRetroArchSystemConfigs(
		p.fileSystem(),
		filepath.Dir(p.retroArchConfigPath()),
		sharedretroarch.CoreLaunches(sharedretroarch.ProfileDesktop),
	); err != nil {
		return fmt.Errorf("write native RetroArch system configs: %w", err)
	}
	return nil
}

func (p *Platform) fileSystem() afero.Fs {
	if p.fs == nil {
		return afero.NewOsFs()
	}
	return p.fs
}

func (p *Platform) retroArchConfigPath() string {
	if p.retroArchAppendConfigPath != "" {
		return p.retroArchAppendConfigPath
	}
	return defaultRetroArchAppendConfigPath()
}

// SupportedReaders returns the list of enabled readers for SteamOS.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings returns XDG-based settings for SteamOS.
func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

// RootDirs returns configured roots or the neutral ES-DE ROM root.
func (*Platform) RootDirs(cfg *config.Instance) []string {
	if roots := cfg.IndexRoots(); len(roots) > 0 {
		return roots
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return []string{filepath.Join(home, "ROMs")}
}

// StartPost initializes the platform after service startup.
// Starts the game tracker to monitor Steam game lifecycle.
func (p *Platform) StartPost(
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	p.setActiveMedia = setActiveMedia
	// Initialize base platform
	//nolint:wrapcheck // Pass-through to base implementation
	if err := p.Base.StartPost(ctx, cfg, launcherManager, activeMedia, setActiveMedia, db, scheduler); err != nil {
		return err
	}

	// Resolve Steam root once so tracker uses the same configured installation
	// as the Steam launcher.
	steamRoot := steam.NewClient(steam.DefaultSteamOSOptions()).FindSteamDir(cfg)

	// Create shared process scanner for both Steam and emulator tracking
	p.procScanner = procscanner.New()
	if err := p.procScanner.Start(); err != nil {
		log.Warn().Err(err).Msg("process scanner failed to start")
		return nil
	}

	// Start Steam tracker for external Steam game detection
	p.steamTracker = steamtracker.NewPlatformIntegration(
		p.procScanner,
		p.Base,
		activeMedia,
		setActiveMedia,
		steamRoot,
	)
	p.steamTracker.IgnoreExecutable(steamruntime.DefaultInstallPaths().Runtime)
	p.steamTracker.Start()

	// Start emulator tracker for EmuDeck/RetroDECK game detection
	p.emuTracker = NewEmulatorTracker(
		p.procScanner,
		p.onEmulatorStart,
		p.onEmulatorStop,
	)
	p.emuTracker.Start()

	return nil
}

// onEmulatorStart is called when an emulator process is detected.
func (*Platform) onEmulatorStart(name string, pid int, cmdline string) {
	log.Debug().
		Str("name", name).
		Int("pid", pid).
		Str("cmdline", cmdline).
		Msg("emulator started (external to Zaparoo)")
	// Note: We don't set ActiveMedia here because we don't know what game is running.
	// The emulator tracker is primarily for process lifecycle tracking, not game detection.
	// Games launched via Zaparoo will have ActiveMedia set by the launcher.
}

// onEmulatorStop is called when an emulator process exits.
func (*Platform) onEmulatorStop(name string, pid int) {
	log.Debug().
		Str("name", name).
		Int("pid", pid).
		Msg("emulator stopped")
}

// Stop stops the platform and cleans up resources.
func (p *Platform) Stop() error {
	steamOSGameMode.RevertFocus()
	if p.steamRuntime != nil && p.steamRuntime.HasActive() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := p.steamRuntime.Stop(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to stop Steam Runtime session")
		}
		cancel()
	}

	// Stop trackers first (they reference the scanner)
	if p.emuTracker != nil {
		p.emuTracker.Stop()
	}
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}

	// Stop shared scanner last
	if p.procScanner != nil {
		p.procScanner.Stop()
	}

	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.Stop()
}

func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	if p.steamRuntime != nil && p.steamRuntime.HasActive() {
		steamOSGameMode.RevertFocus()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		err := p.steamRuntime.Stop(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("stop Steam Runtime launcher: %w", err)
		}
		return nil
	}
	//nolint:wrapcheck // Pass-through to shared Linux process manager.
	return p.Base.StopActiveLauncher(intent)
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	if p.steamRuntime != nil && proc != nil && p.steamRuntime.Owns(proc.Pid) {
		return
	}
	p.Base.SetTrackedProcess(proc)
}

func (p *Platform) WaitTrackedProcess(proc *os.Process) error {
	if p.steamRuntime != nil && proc != nil && p.steamRuntime.Owns(proc.Pid) {
		//nolint:wrapcheck // Broker provides full runtime wait context.
		return p.steamRuntime.Wait(context.Background(), proc.Pid)
	}
	//nolint:wrapcheck // Pass-through to shared Linux process manager.
	return p.Base.WaitTrackedProcess(proc)
}

func (p *Platform) ClearTrackedProcessMedia(proc *os.Process) bool {
	if p.steamRuntime != nil && proc != nil && p.steamRuntime.Owns(proc.Pid) {
		if !p.steamRuntime.Clear(proc.Pid) {
			return false
		}
		if p.setActiveMedia != nil {
			p.setActiveMedia(nil)
		}
		return true
	}
	return p.Base.ClearTrackedProcessMedia(proc)
}

// ReturnToMenu stops active media on SteamOS. Steam's Game Mode shell remains
// responsible for presenting its menu.
func (p *Platform) ReturnToMenu() error {
	return p.StopActiveLauncher(platforms.StopForMenu)
}

// LaunchMedia launches media using the appropriate launcher.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p)
}

func (p *Platform) wrapSteamRuntime(launcher *platforms.Launcher) {
	if p.steamRuntime == nil || launcher.BuildLaunchCommand == nil {
		return
	}
	builder := launcher.BuildLaunchCommand
	directLaunch := launcher.Launch
	launcherID := launcher.ID
	// Replace direct-launch wrappers while Runtime remains available. Readiness
	// is checked again per launch because integration can be removed after the
	// launcher cache is built.
	launcher.Launch = func(
		cfg *config.Instance,
		path string,
		opts *platforms.LaunchOptions,
	) (*os.Process, error) {
		if !p.steamRuntime.Available() {
			if directLaunch == nil {
				return nil, fmt.Errorf(
					"runtime integration unavailable and launcher %q has no direct launch", launcherID,
				)
			}
			log.Warn().Str("launcher", launcherID).Msg("Steam Runtime unavailable; using direct launch")
			return directLaunch(cfg, path, opts)
		}

		commandSpec, err := builder(cfg, path, opts)
		if err != nil {
			return nil, err
		}
		process, err := p.steamRuntime.Start(context.Background(), &steamruntime.Command{
			Executable: commandSpec.Executable,
			Dir:        commandSpec.Dir,
			Args:       commandSpec.Args,
			Env:        steamRuntimeEnvOverrides(commandSpec.Env),
		})
		if err != nil {
			return nil, fmt.Errorf("start Steam Runtime launch: %w", err)
		}
		return process, nil
	}
	launcher.Lifecycle = platforms.LifecycleBlocking
}

// Launchers returns the available launchers for SteamOS.
// SteamOS uses direct steam command (not xdg-open) for better Game Mode integration.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	steamOpts := steam.DefaultSteamOSOptions()
	runtimePaths := steamruntime.DefaultInstallPaths()
	steamOpts.ExcludedShortcutExecutables = []string{runtimePaths.Runtime, runtimePaths.Desktop}
	steamLauncher := steam.NewSteamLauncher(steamOpts)
	// Steam may present cloud-sync or compatibility UI before starting a game.
	// Publish ActiveMedia only after Steam's process tracker observes a real session.
	steamLauncher.Lifecycle = platforms.LifecycleExternal
	ls := []platforms.Launcher{
		// Kodi launchers (8 types)
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),

		// Steam with Steam Deck optimizations
		steamLauncher,

		// Generic for custom scripts
		launchers.NewGenericLauncher(),
	}

	// Prefer installed standalone emulators for systems where they provide the
	// strongest Steam Deck integration, then fall back to native RetroArch.
	ls = append(ls, nativeStandaloneLaunchers()...)
	retroArchOpts := steamOSRetroArchOptions(p.retroArchConfigPath())
	ls = append(ls, nativeRetroArchLaunchers(&retroArchOpts)...)

	// Add RetroDECK launchers if available
	if retrodeckLaunchers := GetRetroDECKLaunchers(cfg); len(retrodeckLaunchers) > 0 {
		ls = append(ls, retrodeckLaunchers...)
	}

	// Add EmuDeck launchers if available
	if emudeckLaunchers := buildEmuDeckLaunchers(cfg, &retroArchOpts); len(emudeckLaunchers) > 0 {
		ls = append(ls, emudeckLaunchers...)
	}

	allLaunchers := append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
	if p.steamRuntime != nil && p.steamRuntime.Available() && steamOSGameMode.IsGamingMode() {
		for i := range allLaunchers {
			p.wrapSteamRuntime(&allLaunchers[i])
		}
	}
	return allLaunchers
}
