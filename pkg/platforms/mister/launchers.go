//go:build linux

package mister

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/rs/zerolog/log"
)

const (
	f9ConsoleVT       = "1"
	launcherConsoleVT = "7"
	scriptConsoleVT   = "3"
)

func checkInZip(path string) string {
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		return path
	}

	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.IsDir() {
		log.Error().Err(err).Msgf("failed to access the zip file at path: %s", path)
		return path
	}

	zipReader, err := zip.OpenReader(path)
	if err != nil {
		log.Error().Err(err).Msgf("failed to open zip file: %s", path)
		return path
	}
	defer func(zipReader *zip.ReadCloser) {
		err := zipReader.Close()
		if err != nil {
			log.Error().Err(err).Msgf("failed to close zip file: %s", path)
		}
	}(zipReader)

	var firstFilePath string
	matchingFilePath := ""
	zipName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		if firstFilePath == "" {
			firstFilePath = file.Name
		}

		if strings.EqualFold(strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name)), zipName) {
			matchingFilePath = file.Name
			break
		}
	}

	if matchingFilePath != "" {
		log.Debug().Msgf("found matching file: %s", matchingFilePath)
		result := filepath.Join(path, matchingFilePath)
		log.Debug().
			Str("zip", path).
			Str("selected", matchingFilePath).
			Int("total_files", len(zipReader.File)).
			Msg("zip file processed successfully")
		return result
	} else if firstFilePath != "" && len(zipReader.File) == 1 {
		log.Debug().Msgf("found single file in zip archive: %s", firstFilePath)
		result := filepath.Join(path, firstFilePath)
		log.Debug().Str("zip", path).Str("selected", firstFilePath).Msg("single-file zip processed")
		return result
	}

	log.Warn().Str("zip", path).Int("files", len(zipReader.File)).Msgf("no suitable file found in zip archive")
	return path
}

func launch(pl platforms.Platform, coreID string) func(*config.Instance, string) (*os.Process, error) {
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		// Close console if needed - FPGA cores take over display
		if err := pl.ConsoleManager().Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close console before FPGA launch")
		}

		if filepath.Ext(strings.ToLower(path)) == ".mgl" {
			err := mgls.LaunchBasicFile(path)
			if err != nil {
				log.Error().Err(err).Msg("error launching mgl")
				return nil, fmt.Errorf("failed to launch generic file: %w", err)
			}
			err = activegame.SetActiveGame(path)
			if err != nil {
				return nil, fmt.Errorf("failed to set active game: %w", err)
			}
			return nil, nil
		}

		s, err := cores.GetCore(coreID)
		if err != nil {
			return nil, fmt.Errorf("failed to get system %s: %w", coreID, err)
		}

		path = checkInZip(path)

		err = mgls.LaunchGame(cfg, s, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		log.Debug().Msgf("setting active game: %s", path)
		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

func launchSinden(
	systemID string,
	rbfName string,
) func(*config.Instance, string) (*os.Process, error) {
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		s, err := cores.GetCore(systemID)
		if err != nil {
			return nil, fmt.Errorf("failed to get system %s: %w", systemID, err)
		}
		path = checkInZip(path)

		sn := *s

		newRBF := "Light Gun/" + rbfName + "-Sinden"
		oldRBF := "_Sinden/" + rbfName + "_Sinden"

		newMatches, err := filepath.Glob(filepath.Join(misterconfig.SDRootDir, newRBF) + "*")
		if err != nil {
			log.Debug().Err(err).Msg("error checking for new Sinden RBF")
		}
		if len(newMatches) > 0 {
			sn.RBF = newRBF
		} else {
			// just fallback on trying the old path
			sn.RBF = oldRBF
		}

		sn.SetName = rbfName + "_Sinden"
		sn.SetNameSameDir = true

		log.Debug().Str("rbf", sn.RBF).Msgf("launching Sinden: %v", sn)

		err = mgls.LaunchGame(cfg, &sn, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

func launchAggGnw(cfg *config.Instance, path string) (*os.Process, error) {
	s, err := cores.GetCore("GameNWatch")
	if err != nil {
		return nil, fmt.Errorf("failed to get GameNWatch system: %w", err)
	}
	path = checkInZip(path)

	sn := *s
	sn.RBF = "_Console/GameAndWatch"
	sn.Slots = []cores.Slot{
		{
			Exts: []string{".gnw"},
			Mgl: &cores.MGLParams{
				Delay:  1,
				Method: "f",
				Index:  1,
			},
		},
	}

	err = mgls.LaunchGame(cfg, &sn, path)
	if err != nil {
		return nil, fmt.Errorf("failed to launch game: %w", err)
	}

	err = activegame.SetActiveGame(path)
	if err != nil {
		return nil, fmt.Errorf("failed to set active game: %w", err)
	}
	return nil, nil //nolint:nilnil // MiSTer launches don't return a process handle
}

func launchAltCore(
	systemID string,
	rbfPath string,
) func(*config.Instance, string) (*os.Process, error) {
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		s, err := cores.GetCore(systemID)
		if err != nil {
			return nil, fmt.Errorf("failed to get system %s: %w", systemID, err)
		}
		path = checkInZip(path)

		sn := *s
		sn.RBF = rbfPath

		log.Debug().Str("rbf", sn.RBF).Msgf("launching alt core: %v", sn)

		err = mgls.LaunchGame(cfg, &sn, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

//nolint:unused // keeping as reference for future implementation
func launchGroovyCore() func(*config.Instance, string) (*os.Process, error) {
	// Merge into mrext?
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		sn := cores.Core{
			ID:  "Groovy",
			RBF: "_Utility/Groovy",
			Slots: []cores.Slot{
				{
					Label: "GMC",
					Exts:  []string{".gmc"},
					Mgl: &cores.MGLParams{
						Delay:  2,
						Method: "f",
						Index:  1,
					},
				},
			},
		}

		log.Debug().Msgf("launching Groovy core: %v", sn)

		err := mgls.LaunchGame(cfg, &sn, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

func launchDOS() func(*config.Instance, string) (*os.Process, error) {
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		if filepath.Ext(strings.ToLower(path)) == ".mgl" {
			err := mgls.LaunchBasicFile(path)
			if err != nil {
				log.Error().Err(err).Msg("error launching mgl")
				return nil, fmt.Errorf("failed to launch generic file: %w", err)
			}
			err = activegame.SetActiveGame(path)
			if err != nil {
				return nil, fmt.Errorf("failed to set active game: %w", err)
			}
			return nil, nil
		}

		s, err := cores.GetCore("ao486")
		if err != nil {
			return nil, fmt.Errorf("failed to get ao486 system: %w", err)
		}

		path = checkInZip(path)

		err = mgls.LaunchGame(cfg, s, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		log.Debug().Msgf("setting active game: %s", path)
		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

func launchAtari2600() func(*config.Instance, string) (*os.Process, error) {
	return func(cfg *config.Instance, path string) (*os.Process, error) {
		s, err := cores.GetCore("Atari2600")
		if err != nil {
			return nil, fmt.Errorf("failed to get Atari2600 system: %w", err)
		}
		path = checkInZip(path)

		sn := *s
		sn.Slots = []cores.Slot{
			{
				Exts: []string{".a26", ".bin"},
				Mgl: &cores.MGLParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		}

		err = mgls.LaunchGame(cfg, &sn, path)
		if err != nil {
			return nil, fmt.Errorf("failed to launch game: %w", err)
		}

		err = activegame.SetActiveGame(path)
		if err != nil {
			return nil, fmt.Errorf("failed to set active game: %w", err)
		}
		return nil, nil
	}
}

func launchVideo(pl *Platform) func(*config.Instance, string) (*os.Process, error) {
	return func(_ *config.Instance, path string) (*os.Process, error) {
		// videoDivisor controls the framebuffer resolution divisor for video playback.
		// Using fb_cmd0 (scaled mode):
		//   - divisor 3: ~640x360 on 1920x1080, ~853x480 on 2560x1440
		//   - Scales to fill entire screen (no borders)
		const videoDivisor = 3

		if path == "" {
			return nil, errors.New("no path specified")
		}

		log.Info().
			Int("divisor", videoDivisor).
			Str("path", path).
			Msg("video playback starting")

		// Capture launcher context for staleness detection and cancellation
		launcherCtx := pl.launcherManager.GetContext()

		// Setup console environment (FPGA check, console switch, cleanup)
		cm, err := setupConsoleEnvironment(launcherCtx, pl)
		if err != nil {
			return nil, err
		}

		// Set scaled video mode for video playback
		if modeErr := mistermain.SetVideoModeScaled(videoDivisor); modeErr != nil {
			return nil, fmt.Errorf("failed to set scaled video mode (divisor %d): %w",
				videoDivisor, modeErr)
		}

		log.Info().Str("path", path).Msg("launching video with fvp")

		fvpBinary := filepath.Join(misterconfig.LinuxDir, "fvp")
		cmd := exec.CommandContext( //nolint:gosec // Path comes from internal launcher system, not user input
			launcherCtx,
			fvpBinary,
			"-f", // Fullscreen mode
			// "-j", "1", // Jump every 1 video frame for slow machines
			"-u", // Record A/V diff after first few frames
			"-s", // Always synchronize audio/video
			path,
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true, // Create new session for proper TTY control
		}
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+misterconfig.LinuxDir)

		// Build cleanup function that will be called on completion/crash
		restoreFunc := createConsoleRestoreFunc(pl, cm)

		// Start process and manage lifecycle
		return runTrackedProcess(launcherCtx, pl, cmd, restoreFunc, "fvp")
	}
}

// launchScummVM returns a launcher function for ScummVM games on MiSTer.
func launchScummVM(pl *Platform) func(*config.Instance, string) (*os.Process, error) {
	return func(_ *config.Instance, path string) (*os.Process, error) {
		if path == "" {
			return nil, errors.New("no path specified")
		}

		// Extract game target ID from virtual path: scummvm://targetid/Game Name
		targetID := strings.TrimPrefix(path, "scummvm://")
		targetID = strings.SplitN(targetID, "/", 2)[0]

		if targetID == "" {
			return nil, errors.New("no ScummVM target ID specified in path")
		}

		log.Info().Str("target", targetID).Msg("ScummVM game launching")

		// Find ScummVM binary
		scummvmBinary, err := findScummVMBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to find ScummVM binary: %w", err)
		}

		// Capture launcher context for staleness detection and cancellation
		launcherCtx := pl.launcherManager.GetContext()

		// Setup console environment (FPGA check, console switch, cleanup)
		cm, err := setupConsoleEnvironment(launcherCtx, pl)
		if err != nil {
			return nil, err
		}

		// Set video mode for ScummVM (640x480 RGB16)
		// Matches original MiSTer_ScummVM: vmode -r 640 480 rgb16
		if err := mistermain.SetVideoModeExact(640, 480, mistermain.VideoModeFormatRGB16); err != nil {
			return nil, fmt.Errorf("failed to set video mode: %w", err)
		}

		// Start MIDIMeister if available
		midiStarted := false
		if err := startMIDIMeister(); err != nil {
			log.Warn().Err(err).Msg("failed to start MIDIMeister, continuing without MIDI support")
		} else if shouldStartMIDIMeister() {
			midiStarted = true
		}

		log.Info().Str("binary", scummvmBinary).Str("target", targetID).Msg("launching ScummVM")

		// Prepare ScummVM command with taskset for CPU affinity
		cmd := exec.CommandContext( //nolint:gosec // Path validated by findScummVMBinary
			launcherCtx,
			"taskset", "03", // CPU affinity: cores 0-1
			scummvmBinary,
			"--opl-driver=db",
			"--output-rate=48000",
			targetID,
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true, // Create new session for proper TTY control
		}

		// Set environment variables
		cmd.Env = append(os.Environ(),
			"HOME="+scummvmBaseDir,
			"LD_LIBRARY_PATH="+filepath.Join(scummvmBaseDir, "arm-linux-gnueabihf")+":"+
				filepath.Join(scummvmBaseDir, "arm-linux-gnueabihf", "pulseaudio"),
		)

		// Set working directory to ScummVM base
		cmd.Dir = scummvmBaseDir

		// Build cleanup function that will be called on completion/crash
		restoreFunc := createConsoleRestoreFunc(pl, cm)

		// Wrap restore to also stop MIDI if we started it
		restoreWithMIDI := func() {
			if midiStarted {
				stopMIDIMeister()
			}
			restoreFunc()
		}

		// Start process and manage lifecycle (wraps with nice/setsid)
		return runTrackedProcess(launcherCtx, pl, cmd, restoreWithMIDI, "scummvm")
	}
}

// createScummVMLauncher creates a Launcher definition for ScummVM games.
func createScummVMLauncher(pl *Platform) platforms.Launcher {
	return platforms.Launcher{
		ID:                 "ScummVM",
		SystemID:           systemdefs.SystemScummVM,
		Schemes:            []string{"scummvm"},
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleTracked,
		Scanner:            scanScummVMGames,
		Launch:             launchScummVM(pl),
	}
}

// createVideoLauncher creates a Launcher definition for fvp video playback.
func createVideoLauncher(pl *Platform) platforms.Launcher {
	return platforms.Launcher{
		ID:         "GenericVideo",
		SystemID:   systemdefs.SystemVideo,
		Folders:    []string{"Video", "Movies", "TV"},
		Extensions: []string{".mp4", ".mkv", ".avi", ".mov", ".webm"},
		Lifecycle:  platforms.LifecycleTracked,
		Launch:     launchVideo(pl),
	}
}

// CreateLaunchers creates all standard MiSTer launchers for the given platform.
// This is exported for use by MiSTeX and other MiSTer variants.
func CreateLaunchers(pl platforms.Platform) []platforms.Launcher {
	return []platforms.Launcher{
		// Consoles
		{
			ID:         systemdefs.SystemAdventureVision,
			SystemID:   systemdefs.SystemAdventureVision,
			Folders:    []string{"AVision"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemAdventureVision),
		},
		{
			ID:         systemdefs.SystemArcadia,
			SystemID:   systemdefs.SystemArcadia,
			Folders:    []string{"Arcadia"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemArcadia),
		},
		{
			ID:         systemdefs.SystemAmigaCD32,
			SystemID:   systemdefs.SystemAmigaCD32,
			Folders:    []string{"AmigaCD32"},
			Extensions: []string{".cue", ".chd", ".iso"},
			Launch:     launch(pl, systemdefs.SystemAmigaCD32),
		},
		{
			ID:         systemdefs.SystemAstrocade,
			SystemID:   systemdefs.SystemAstrocade,
			Folders:    []string{"Astrocade"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemAstrocade),
		},
		{
			ID:         systemdefs.SystemAtari2600,
			SystemID:   systemdefs.SystemAtari2600,
			Folders:    []string{"ATARI7800", "Atari2600"},
			Extensions: []string{".a26"},
			Launch:     launchAtari2600(),
			Test: func(_ *config.Instance, path string) bool {
				lowerPath := strings.ToLower(path)
				// TODO: really, this should specifically check on the root dirs,
				// 		 but we'd need to modify the test function to have access
				//       to the platform interface. it's probably a safe enough bet
				//       that something in an atari2600 subdir is for atari2600
				if (strings.Contains(lowerPath, "/atari2600/") ||
					strings.Contains(lowerPath, "/atari 2600/")) &&
					filepath.Ext(lowerPath) == ".bin" {
					return true
				}
				return false
			},
		},
		{
			ID:       "LLAPIAtari2600",
			SystemID: systemdefs.SystemAtari2600,
			Launch:   launchAltCore(systemdefs.SystemAtari2600, "_LLAPI/Atari7800_LLAPI"),
		},
		{
			ID:         systemdefs.SystemAtari5200,
			SystemID:   systemdefs.SystemAtari5200,
			Folders:    []string{"ATARI5200"},
			Extensions: []string{".car", ".a52", ".bin", ".rom"},
			Launch:     launch(pl, systemdefs.SystemAtari5200),
		},
		{
			ID:         systemdefs.SystemAtari7800,
			SystemID:   systemdefs.SystemAtari7800,
			Folders:    []string{"ATARI7800"},
			Extensions: []string{".a78", ".bin"},
			Launch:     launch(pl, systemdefs.SystemAtari7800),
		},
		{
			ID:       "LLAPIAtari7800",
			SystemID: systemdefs.SystemAtari7800,
			Launch:   launchAltCore(systemdefs.SystemAtari7800, "_LLAPI/Atari7800_LLAPI"),
		},
		{
			ID:         systemdefs.SystemAtariLynx,
			SystemID:   systemdefs.SystemAtariLynx,
			Folders:    []string{"AtariLynx"},
			Extensions: []string{".lnx"},
			Launch:     launch(pl, systemdefs.SystemAtariLynx),
		},
		{
			ID:         systemdefs.SystemCasioPV1000,
			SystemID:   systemdefs.SystemCasioPV1000,
			Folders:    []string{"Casio_PV-1000"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemCasioPV1000),
		},
		{
			ID:         systemdefs.SystemCDI,
			SystemID:   systemdefs.SystemCDI,
			Folders:    []string{"CD-i"},
			Extensions: []string{".cue", ".chd"},
			Launch:     launch(pl, systemdefs.SystemCDI),
		},
		{
			ID:         systemdefs.SystemChannelF,
			SystemID:   systemdefs.SystemChannelF,
			Folders:    []string{"ChannelF"},
			Extensions: []string{".rom", ".bin"},
			Launch:     launch(pl, systemdefs.SystemChannelF),
		},
		{
			ID:         systemdefs.SystemColecoVision,
			SystemID:   systemdefs.SystemColecoVision,
			Folders:    []string{"Coleco"},
			Extensions: []string{".col", ".bin", ".rom"},
			Launch:     launch(pl, systemdefs.SystemColecoVision),
		},
		{
			ID:         systemdefs.SystemCreatiVision,
			SystemID:   systemdefs.SystemCreatiVision,
			Folders:    []string{"CreatiVision"},
			Extensions: []string{".rom", ".bin", ".bas"},
			Launch:     launch(pl, systemdefs.SystemCreatiVision),
		},
		{
			ID:         systemdefs.SystemFDS,
			SystemID:   systemdefs.SystemFDS,
			Folders:    []string{"NES", "FDS"},
			Extensions: []string{".fds"},
			Launch:     launch(pl, systemdefs.SystemFDS),
		},
		{
			ID:         systemdefs.SystemGamate,
			SystemID:   systemdefs.SystemGamate,
			Folders:    []string{"Gamate"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemGamate),
		},
		{
			ID:         systemdefs.SystemGameboy,
			SystemID:   systemdefs.SystemGameboy,
			Folders:    []string{"GAMEBOY"},
			Extensions: []string{".gb"},
			Launch:     launch(pl, systemdefs.SystemGameboy),
		},
		{
			ID:       "LLAPIGameboy",
			SystemID: systemdefs.SystemGameboy,
			Launch:   launchAltCore(systemdefs.SystemGameboy, "_LLAPI/Gameboy_LLAPI"),
		},
		{
			ID:         systemdefs.SystemGameboyColor,
			SystemID:   systemdefs.SystemGameboyColor,
			Folders:    []string{"GAMEBOY", "GBC"},
			Extensions: []string{".gbc"},
			Launch:     launch(pl, systemdefs.SystemGameboyColor),
		},
		{
			ID:         systemdefs.SystemGameboy2P,
			SystemID:   systemdefs.SystemGameboy2P,
			Folders:    []string{"GAMEBOY2P"},
			Extensions: []string{".gb", ".gbc"},
			Launch:     launch(pl, systemdefs.SystemGameboy2P),
		},
		{
			ID:         systemdefs.SystemGameGear,
			SystemID:   systemdefs.SystemGameGear,
			Folders:    []string{"SMS", "GameGear"},
			Extensions: []string{".gg"},
			Launch:     launch(pl, systemdefs.SystemGameGear),
		},
		{
			ID:         systemdefs.SystemGameNWatch,
			SystemID:   systemdefs.SystemGameNWatch,
			Folders:    []string{"GameNWatch"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemGameNWatch),
		},
		{
			ID:         "GameAndWatch",
			SystemID:   systemdefs.SystemGameNWatch,
			Folders:    []string{"Game and Watch"},
			Extensions: []string{".gnw"},
			Launch:     launchAggGnw,
		},
		{
			ID:         systemdefs.SystemGBA,
			SystemID:   systemdefs.SystemGBA,
			Folders:    []string{"GBA"},
			Extensions: []string{".gba"},
			Launch:     launch(pl, systemdefs.SystemGBA),
		},
		{
			ID:       "LLAPIGBA",
			SystemID: systemdefs.SystemGBA,
			Launch:   launchAltCore(systemdefs.SystemGBA, "_LLAPI/GBA_LLAPI"),
		},
		{
			ID:         systemdefs.SystemGBA2P,
			SystemID:   systemdefs.SystemGBA2P,
			Folders:    []string{"GBA2P"},
			Extensions: []string{".gba"},
			Launch:     launch(pl, systemdefs.SystemGBA2P),
		},
		{
			ID:         systemdefs.SystemGenesis,
			SystemID:   systemdefs.SystemGenesis,
			Folders:    []string{"MegaDrive", "Genesis"},
			Extensions: []string{".gen", ".bin", ".md"},
			Launch:     launch(pl, systemdefs.SystemGenesis),
		},
		{
			ID:       "SindenGenesis",
			SystemID: systemdefs.SystemGenesis,
			Launch:   launchSinden(systemdefs.SystemGenesis, "Genesis"),
		},
		{
			ID:       "SindenMegaDrive",
			SystemID: systemdefs.SystemGenesis,
			Launch:   launchSinden(systemdefs.SystemGenesis, "MegaDrive"),
		},
		{
			ID:       "LLAPIMegaDrive",
			SystemID: systemdefs.SystemGenesis,
			Launch:   launchAltCore(systemdefs.SystemGenesis, "_LLAPI/MegaDrive_LLAPI"),
		},
		{
			ID:         systemdefs.SystemIntellivision,
			SystemID:   systemdefs.SystemIntellivision,
			Folders:    []string{"Intellivision"},
			Extensions: []string{".int", ".bin"},
			Launch:     launch(pl, systemdefs.SystemIntellivision),
		},
		{
			ID:         systemdefs.SystemJaguar,
			SystemID:   systemdefs.SystemJaguar,
			Folders:    []string{"Jaguar"},
			Extensions: []string{".jag", ".j64", ".rom", ".bin"},
			Launch:     launch(pl, systemdefs.SystemJaguar),
		},
		{
			ID:         systemdefs.SystemMasterSystem,
			SystemID:   systemdefs.SystemMasterSystem,
			Folders:    []string{"SMS"},
			Extensions: []string{".sms"},
			Launch:     launch(pl, systemdefs.SystemMasterSystem),
		},
		{
			ID:       "SindenSMS",
			SystemID: systemdefs.SystemMasterSystem,
			Launch:   launchSinden(systemdefs.SystemMasterSystem, "SMS"),
		},
		{
			ID:       "LLAPISMS",
			SystemID: systemdefs.SystemMasterSystem,
			Launch:   launchAltCore(systemdefs.SystemMasterSystem, "_LLAPI/SMS_LLAPI"),
		},
		{
			ID:         systemdefs.SystemMegaCD,
			SystemID:   systemdefs.SystemMegaCD,
			Folders:    []string{"MegaCD"},
			Extensions: []string{".cue", ".chd"},
			Launch:     launch(pl, systemdefs.SystemMegaCD),
		},
		{
			ID:       "SindenMegaCD",
			SystemID: systemdefs.SystemMegaCD,
			Launch:   launchSinden(systemdefs.SystemMegaCD, "MegaCD"),
		},
		{
			ID:       "LLAPIMegaCD",
			SystemID: systemdefs.SystemMegaCD,
			Launch:   launchAltCore(systemdefs.SystemMegaCD, "_LLAPI/MegaCD_LLAPI"),
		},
		{
			ID:         systemdefs.SystemMegaDuck,
			SystemID:   systemdefs.SystemMegaDuck,
			Folders:    []string{"GAMEBOY", "MegaDuck"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemMegaDuck),
		},
		{
			ID:       "LLAPINeoGeo",
			SystemID: systemdefs.SystemNeoGeo,
			Launch:   launchAltCore(systemdefs.SystemNeoGeo, "_LLAPI/NeoGeo_LLAPI"),
		},
		{
			ID:         systemdefs.SystemNeoGeoCD,
			SystemID:   systemdefs.SystemNeoGeoCD,
			Folders:    []string{"NeoGeo-CD", "NEOGEO"},
			Extensions: []string{".cue", ".chd"},
			Launch:     launch(pl, systemdefs.SystemNeoGeoCD),
		},
		{
			ID:         systemdefs.SystemNES,
			SystemID:   systemdefs.SystemNES,
			Folders:    []string{"NES"},
			Extensions: []string{".nes"},
			Launch:     launch(pl, systemdefs.SystemNES),
		},
		{
			ID:       "SindenNES",
			SystemID: systemdefs.SystemNES,
			Launch:   launchSinden(systemdefs.SystemNES, "NES"),
		},
		{
			ID:         systemdefs.SystemNESMusic,
			SystemID:   systemdefs.SystemNESMusic,
			Folders:    []string{"NES"},
			Extensions: []string{".nsf"},
			Launch:     launch(pl, systemdefs.SystemNESMusic),
		},
		{
			ID:       "LLAPINES",
			SystemID: systemdefs.SystemNES,
			Launch:   launchAltCore(systemdefs.SystemNES, "_LLAPI/NES_LLAPI"),
		},
		{
			ID:         systemdefs.SystemNintendo64,
			SystemID:   systemdefs.SystemNintendo64,
			Folders:    []string{"N64"},
			Extensions: []string{".n64", ".z64"},
			Launch:     launch(pl, systemdefs.SystemNintendo64),
		},
		{
			ID:       "LLAPINintendo64",
			SystemID: systemdefs.SystemNintendo64,
			Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_LLAPI"),
		},
		{
			ID:       "LLAPI80MHzNintendo64",
			SystemID: systemdefs.SystemNintendo64,
			Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_80MHz_LLAPI"),
		},
		{
			ID:       "80MHzNintendo64",
			SystemID: systemdefs.SystemNintendo64,
			Launch:   launchAltCore(systemdefs.SystemNintendo64, "_Console/N64_80MHz"),
		},
		{
			ID:       "PWMNintendo64",
			SystemID: systemdefs.SystemNintendo64,
			Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/N64_PWM"),
		},
		{
			ID:       "PWM80MHzNintendo64",
			SystemID: systemdefs.SystemNintendo64,
			Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/_Turbo/N64_80MHz_PWM"),
		},
		{
			ID:         systemdefs.SystemOdyssey2,
			SystemID:   systemdefs.SystemOdyssey2,
			Folders:    []string{"ODYSSEY2"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemOdyssey2),
		},
		{
			ID:         systemdefs.SystemPocketChallengeV2,
			SystemID:   systemdefs.SystemPocketChallengeV2,
			Folders:    []string{"WonderSwan", "PocketChallengeV2"},
			Extensions: []string{".pc2"},
			Launch:     launch(pl, systemdefs.SystemPocketChallengeV2),
		},
		{
			ID:         systemdefs.SystemPokemonMini,
			SystemID:   systemdefs.SystemPokemonMini,
			Folders:    []string{"PokemonMini"},
			Extensions: []string{".min"},
			Launch:     launch(pl, systemdefs.SystemPokemonMini),
		},
		{
			ID:         systemdefs.SystemPSX,
			SystemID:   systemdefs.SystemPSX,
			Folders:    []string{"PSX"},
			Extensions: []string{".cue", ".chd", ".exe"},
			Launch:     launch(pl, systemdefs.SystemPSX),
		},
		{
			ID:       "LLAPIPSX",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/PSX_LLAPI"),
		},
		{
			ID:       "SindenPSX",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchSinden(systemdefs.SystemPSX, "PSX"),
		},
		{
			ID:       "2XPSX",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_Console/PSX2XCPU"),
		},
		{
			ID:       "PWMPSX",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/PSX_PWM"),
		},
		{
			ID:       "PWM2XPSX",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/_Turbo/PSX2XCPU_PWM"),
		},
		{
			ID:         systemdefs.SystemSega32X,
			SystemID:   systemdefs.SystemSega32X,
			Folders:    []string{"S32X"},
			Extensions: []string{".32x"},
			Launch:     launch(pl, systemdefs.SystemSega32X),
		},
		{
			ID:       "LLAPIS32X",
			SystemID: systemdefs.SystemSega32X,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/S32X_LLAPI"),
		},
		{
			ID:         systemdefs.SystemSG1000,
			SystemID:   systemdefs.SystemSG1000,
			Folders:    []string{"SG1000", "Coleco", "SMS"},
			Extensions: []string{".sg"},
			Launch:     launch(pl, systemdefs.SystemSG1000),
		},
		{
			ID:         systemdefs.SystemSuperGameboy,
			SystemID:   systemdefs.SystemSuperGameboy,
			Folders:    []string{"SGB"},
			Extensions: []string{".sgb", ".gb", ".gbc"},
			Launch:     launch(pl, systemdefs.SystemSuperGameboy),
		},
		{
			ID:       "LLAPISuperGameboy",
			SystemID: systemdefs.SystemSuperGameboy,
			Launch:   launchAltCore(systemdefs.SystemSuperGameboy, "_LLAPI/SGB_LLAPI"),
		},
		{
			ID:         systemdefs.SystemSuperVision,
			SystemID:   systemdefs.SystemSuperVision,
			Folders:    []string{"SuperVision"},
			Extensions: []string{".bin", ".sv"},
			Launch:     launch(pl, systemdefs.SystemSuperVision),
		},
		{
			ID:         systemdefs.SystemSaturn,
			SystemID:   systemdefs.SystemSaturn,
			Folders:    []string{"Saturn"},
			Extensions: []string{".cue", ".chd"},
			Launch:     launch(pl, systemdefs.SystemSaturn),
		},
		{
			ID:       "LLAPISaturn",
			SystemID: systemdefs.SystemSaturn,
			Launch:   launchAltCore(systemdefs.SystemSaturn, "_LLAPI/Saturn_LLAPI"),
		},
		{
			ID:       "PWMSaturn",
			SystemID: systemdefs.SystemPSX,
			Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/Saturn_PWM"),
		},
		{
			ID:         systemdefs.SystemSNES,
			SystemID:   systemdefs.SystemSNES,
			Folders:    []string{"SNES"},
			Extensions: []string{".sfc", ".smc", ".bin", ".bs"},
			Launch:     launch(pl, systemdefs.SystemSNES),
		},
		{
			ID:       "LLAPISNES",
			SystemID: systemdefs.SystemSNES,
			Launch:   launchAltCore(systemdefs.SystemSNES, "_LLAPI/SNES_LLAPI"),
		},
		{
			ID:       "SindenSNES",
			SystemID: systemdefs.SystemSNES,
			Launch:   launchSinden(systemdefs.SystemSNES, "SNES"),
		},
		{
			ID:         systemdefs.SystemSNESMusic,
			SystemID:   systemdefs.SystemSNESMusic,
			Folders:    []string{"SNES"},
			Extensions: []string{".spc"},
			Launch:     launch(pl, systemdefs.SystemSNESMusic),
		},
		{
			ID:         systemdefs.SystemSuperGrafx,
			SystemID:   systemdefs.SystemSuperGrafx,
			Folders:    []string{"TGFX16"},
			Extensions: []string{".sgx"},
			Launch:     launch(pl, systemdefs.SystemSuperGrafx),
		},
		{
			ID:         systemdefs.SystemTurboGrafx16,
			SystemID:   systemdefs.SystemTurboGrafx16,
			Folders:    []string{"TGFX16"},
			Extensions: []string{".pce", ".bin"},
			Launch:     launch(pl, systemdefs.SystemTurboGrafx16),
		},
		{
			ID:       "LLAPITurboGrafx16",
			SystemID: systemdefs.SystemTurboGrafx16,
			Launch:   launchAltCore(systemdefs.SystemTurboGrafx16, "_LLAPI/TurboGrafx16_LLAPI"),
		},
		{
			ID:         systemdefs.SystemTurboGrafx16CD,
			SystemID:   systemdefs.SystemTurboGrafx16CD,
			Folders:    []string{"TGFX16-CD"},
			Extensions: []string{".cue", ".chd"},
			Launch:     launch(pl, systemdefs.SystemTurboGrafx16CD),
		},
		{
			ID:         systemdefs.SystemVC4000,
			SystemID:   systemdefs.SystemVC4000,
			Folders:    []string{"VC4000"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemVC4000),
		},
		{
			ID:         systemdefs.SystemVectrex,
			SystemID:   systemdefs.SystemVectrex,
			Folders:    []string{"VECTREX"},
			Extensions: []string{".vec", ".bin", ".rom"}, // TODO: overlays (.ovr)
			Launch:     launch(pl, systemdefs.SystemVectrex),
		},
		{
			ID:         systemdefs.SystemWonderSwan,
			SystemID:   systemdefs.SystemWonderSwan,
			Folders:    []string{"WonderSwan"},
			Extensions: []string{".ws"},
			Launch:     launch(pl, systemdefs.SystemWonderSwan),
		},
		{
			ID:         systemdefs.SystemWonderSwanColor,
			SystemID:   systemdefs.SystemWonderSwanColor,
			Folders:    []string{"WonderSwan", "WonderSwanColor"},
			Extensions: []string{".wsc"},
			Launch:     launch(pl, systemdefs.SystemWonderSwanColor),
		},
		// Computers
		{
			ID:         systemdefs.SystemAcornAtom,
			SystemID:   systemdefs.SystemAcornAtom,
			Folders:    []string{"AcornAtom"},
			Extensions: []string{".vhd"},
			Launch:     launch(pl, systemdefs.SystemAcornAtom),
		},
		{
			ID:         systemdefs.SystemAcornElectron,
			SystemID:   systemdefs.SystemAcornElectron,
			Folders:    []string{"AcornElectron"},
			Extensions: []string{".vhd"},
			Launch:     launch(pl, systemdefs.SystemAcornElectron),
		},
		{
			ID:         systemdefs.SystemAliceMC10,
			SystemID:   systemdefs.SystemAliceMC10,
			Folders:    []string{"AliceMC10"},
			Extensions: []string{".c10"},
			Launch:     launch(pl, systemdefs.SystemAliceMC10),
		},
		{
			ID:         systemdefs.SystemAmstrad,
			SystemID:   systemdefs.SystemAmstrad,
			Folders:    []string{"Amstrad"},
			Extensions: []string{".dsk", ".cdt"}, // TODO: globbing support? for .e??
			Launch:     launch(pl, systemdefs.SystemAmstrad),
		},
		{
			ID:         systemdefs.SystemAmstradPCW,
			SystemID:   systemdefs.SystemAmstradPCW,
			Folders:    []string{"Amstrad PCW"},
			Extensions: []string{".dsk"},
			Launch:     launch(pl, systemdefs.SystemAmstradPCW),
		},
		{
			ID:         systemdefs.SystemDOS,
			SystemID:   systemdefs.SystemDOS,
			Folders:    []string{"AO486", "/media/fat/_DOS Games"},
			Extensions: []string{".mgl", ".vhd", ".img", ".ima", ".vfd", ".iso", ".cue", ".chd"},
			Launch:     launchDOS(),
		},
		{
			ID:         systemdefs.SystemApogee,
			SystemID:   systemdefs.SystemApogee,
			Folders:    []string{"APOGEE"},
			Extensions: []string{".rka", ".rkr", ".gam"},
			Launch:     launch(pl, systemdefs.SystemApogee),
		},
		{
			ID:         systemdefs.SystemAppleI,
			SystemID:   systemdefs.SystemAppleI,
			Folders:    []string{"Apple-I"},
			Extensions: []string{".txt"},
			Launch:     launch(pl, systemdefs.SystemAppleI),
		},
		{
			ID:         systemdefs.SystemAppleII,
			SystemID:   systemdefs.SystemAppleII,
			Folders:    []string{"Apple-II"},
			Extensions: []string{".dsk", ".do", ".po", ".nib", ".hdv"},
			Launch:     launch(pl, systemdefs.SystemAppleII),
		},
		{
			ID:         systemdefs.SystemAquarius,
			SystemID:   systemdefs.SystemAquarius,
			Folders:    []string{"AQUARIUS"},
			Extensions: []string{".bin", ".caq"},
			Launch:     launch(pl, systemdefs.SystemAquarius),
		},
		{
			ID:         systemdefs.SystemAtari800,
			SystemID:   systemdefs.SystemAtari800,
			Folders:    []string{"ATARI800"},
			Extensions: []string{".atr", ".xex", ".xfd", ".atx", ".car", ".rom", ".bin"},
			Launch:     launch(pl, systemdefs.SystemAtari800),
		},
		{
			ID:         systemdefs.SystemBBCMicro,
			SystemID:   systemdefs.SystemBBCMicro,
			Folders:    []string{"BBCMicro"},
			Extensions: []string{".ssd", ".dsd", ".vhd"},
			Launch:     launch(pl, systemdefs.SystemBBCMicro),
		},
		{
			ID:         systemdefs.SystemBK0011M,
			SystemID:   systemdefs.SystemBK0011M,
			Folders:    []string{"BK0011M"},
			Extensions: []string{".bin", ".dsk", ".vhd"},
			Launch:     launch(pl, systemdefs.SystemBK0011M),
		},
		{
			ID:         systemdefs.SystemC16,
			SystemID:   systemdefs.SystemC16,
			Folders:    []string{"C16"},
			Extensions: []string{".d64", ".g64", ".prg", ".tap", ".bin"},
			Launch:     launch(pl, systemdefs.SystemC16),
		},
		{
			ID:         systemdefs.SystemC64,
			SystemID:   systemdefs.SystemC64,
			Folders:    []string{"C64"},
			Extensions: []string{".d64", ".g64", ".t64", ".d81", ".prg", ".crt", ".reu", ".tap"},
			Launch:     launch(pl, systemdefs.SystemC64),
		},
		{
			ID:         systemdefs.SystemCasioPV2000,
			SystemID:   systemdefs.SystemCasioPV2000,
			Folders:    []string{"Casio_PV-2000"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemCasioPV2000),
		},
		{
			ID:         systemdefs.SystemCoCo2,
			SystemID:   systemdefs.SystemCoCo2,
			Folders:    []string{"CoCo2"},
			Extensions: []string{".dsk", ".cas", ".ccc", ".rom"},
			Launch:     launch(pl, systemdefs.SystemCoCo2),
		},
		{
			ID:         systemdefs.SystemEDSAC,
			SystemID:   systemdefs.SystemEDSAC,
			Folders:    []string{"EDSAC"},
			Extensions: []string{".tap"},
			Launch:     launch(pl, systemdefs.SystemEDSAC),
		},
		{
			ID:         systemdefs.SystemGalaksija,
			SystemID:   systemdefs.SystemGalaksija,
			Folders:    []string{"Galaksija"},
			Extensions: []string{".tap"},
			Launch:     launch(pl, systemdefs.SystemGalaksija),
		},
		{
			ID:         systemdefs.SystemInteract,
			SystemID:   systemdefs.SystemInteract,
			Folders:    []string{"Interact"},
			Extensions: []string{".cin", ".k7"},
			Launch:     launch(pl, systemdefs.SystemInteract),
		},
		{
			ID:         systemdefs.SystemJupiter,
			SystemID:   systemdefs.SystemJupiter,
			Folders:    []string{"Jupiter"},
			Extensions: []string{".ace"},
			Launch:     launch(pl, systemdefs.SystemJupiter),
		},
		{
			ID:         systemdefs.SystemLaser,
			SystemID:   systemdefs.SystemLaser,
			Folders:    []string{"Laser"},
			Extensions: []string{".vz"},
			Launch:     launch(pl, systemdefs.SystemLaser),
		},
		{
			ID:         systemdefs.SystemLynx48,
			SystemID:   systemdefs.SystemLynx48,
			Folders:    []string{"Lynx48"},
			Extensions: []string{".tap"},
			Launch:     launch(pl, systemdefs.SystemLynx48),
		},
		{
			ID:         systemdefs.SystemMacPlus,
			SystemID:   systemdefs.SystemMacPlus,
			Folders:    []string{"MACPLUS"},
			Extensions: []string{".dsk", ".img", ".vhd"},
			Launch:     launch(pl, systemdefs.SystemMacPlus),
		},
		{
			ID:         systemdefs.SystemMSX,
			SystemID:   systemdefs.SystemMSX,
			Folders:    []string{"MSX"},
			Extensions: []string{".vhd"},
			Launch:     launch(pl, systemdefs.SystemMSX),
		},
		{
			ID:         "MSX1",
			SystemID:   systemdefs.SystemMSX1,
			Folders:    []string{"MSX1"},
			Extensions: []string{".dsk", ".rom"},
			Launch:     launch(pl, systemdefs.SystemMSX1),
		},
		{
			ID:         systemdefs.SystemMultiComp,
			SystemID:   systemdefs.SystemMultiComp,
			Folders:    []string{"MultiComp"},
			Extensions: []string{".img"},
			Launch:     launch(pl, systemdefs.SystemMultiComp),
		},
		{
			ID:         systemdefs.SystemOrao,
			SystemID:   systemdefs.SystemOrao,
			Folders:    []string{"ORAO"},
			Extensions: []string{".tap"},
			Launch:     launch(pl, systemdefs.SystemOrao),
		},
		{
			ID:         systemdefs.SystemOric,
			SystemID:   systemdefs.SystemOric,
			Folders:    []string{"Oric"},
			Extensions: []string{".dsk"},
			Launch:     launch(pl, systemdefs.SystemOric),
		},
		{
			ID:         systemdefs.SystemPCXT,
			SystemID:   systemdefs.SystemPCXT,
			Folders:    []string{"PCXT"},
			Extensions: []string{".img", ".vhd", ".ima", ".vfd"},
			Launch:     launch(pl, systemdefs.SystemPCXT),
		},
		{
			ID:         systemdefs.SystemPDP1,
			SystemID:   systemdefs.SystemPDP1,
			Folders:    []string{"PDP1"},
			Extensions: []string{".bin", ".rim", ".pdp"},
			Launch:     launch(pl, systemdefs.SystemPDP1),
		},
		{
			ID:         systemdefs.SystemPET2001,
			SystemID:   systemdefs.SystemPET2001,
			Folders:    []string{"PET2001"},
			Extensions: []string{".prg", ".tap"},
			Launch:     launch(pl, systemdefs.SystemPET2001),
		},
		{
			ID:         systemdefs.SystemPMD85,
			SystemID:   systemdefs.SystemPMD85,
			Folders:    []string{"PMD85"},
			Extensions: []string{".rmm"},
			Launch:     launch(pl, systemdefs.SystemPMD85),
		},
		{
			ID:         systemdefs.SystemQL,
			SystemID:   systemdefs.SystemQL,
			Folders:    []string{"QL"},
			Extensions: []string{".mdv", ".win"},
			Launch:     launch(pl, systemdefs.SystemQL),
		},
		{
			ID:         systemdefs.SystemRX78,
			SystemID:   systemdefs.SystemRX78,
			Folders:    []string{"RX78"},
			Extensions: []string{".bin"},
			Launch:     launch(pl, systemdefs.SystemRX78),
		},
		{
			ID:         systemdefs.SystemSAMCoupe,
			SystemID:   systemdefs.SystemSAMCoupe,
			Folders:    []string{"SAMCOUPE"},
			Extensions: []string{".dsk", ".mgt", ".img"},
			Launch:     launch(pl, systemdefs.SystemSAMCoupe),
		},
		{
			ID:         systemdefs.SystemSordM5,
			SystemID:   systemdefs.SystemSordM5,
			Folders:    []string{"Sord M5"},
			Extensions: []string{".bin", ".rom", ".cas"},
			Launch:     launch(pl, systemdefs.SystemSordM5),
		},
		{
			ID:         systemdefs.SystemSpecialist,
			SystemID:   systemdefs.SystemSpecialist,
			Folders:    []string{"SPMX"},
			Extensions: []string{".rks", ".odi"},
			Launch:     launch(pl, systemdefs.SystemSpecialist),
		},
		{
			ID:         systemdefs.SystemSVI328,
			SystemID:   systemdefs.SystemSVI328,
			Folders:    []string{"SVI328"},
			Extensions: []string{".cas", ".bin", ".rom"},
			Launch:     launch(pl, systemdefs.SystemSVI328),
		},
		{
			ID:         systemdefs.SystemTatungEinstein,
			SystemID:   systemdefs.SystemTatungEinstein,
			Folders:    []string{"TatungEinstein"},
			Extensions: []string{".dsk"},
			Launch:     launch(pl, systemdefs.SystemTatungEinstein),
		},
		{
			ID:         systemdefs.SystemTI994A,
			SystemID:   systemdefs.SystemTI994A,
			Folders:    []string{"TI-99_4A"},
			Extensions: []string{".bin", ".m99"},
			Launch:     launch(pl, systemdefs.SystemTI994A),
		},
		{
			ID:         systemdefs.SystemTomyTutor,
			SystemID:   systemdefs.SystemTomyTutor,
			Folders:    []string{"TomyTutor"},
			Extensions: []string{".bin", ".cas"},
			Launch:     launch(pl, systemdefs.SystemTomyTutor),
		},
		{
			ID:         systemdefs.SystemTRS80,
			SystemID:   systemdefs.SystemTRS80,
			Folders:    []string{"TRS-80"},
			Extensions: []string{".dsk", ".jvi", ".cmd", ".cas"},
			Launch:     launch(pl, systemdefs.SystemTRS80),
		},
		{
			ID:         systemdefs.SystemTSConf,
			SystemID:   systemdefs.SystemTSConf,
			Folders:    []string{"TSConf"},
			Extensions: []string{".vhd"},
			Launch:     launch(pl, systemdefs.SystemTSConf),
		},
		{
			ID:         systemdefs.SystemUK101,
			SystemID:   systemdefs.SystemUK101,
			Folders:    []string{"UK101"},
			Extensions: []string{".txt", ".bas", ".lod"},
			Launch:     launch(pl, systemdefs.SystemUK101),
		},
		{
			ID:         systemdefs.SystemVector06C,
			SystemID:   systemdefs.SystemVector06C,
			Folders:    []string{"VECTOR06"},
			Extensions: []string{".rom", ".com", ".c00", ".edd", ".fdd"},
			Launch:     launch(pl, systemdefs.SystemVector06C),
		},
		{
			ID:         systemdefs.SystemVIC20,
			SystemID:   systemdefs.SystemVIC20,
			Folders:    []string{"VIC20"},
			Extensions: []string{".d64", ".g64", ".prg", ".tap", ".crt"},
			Launch:     launch(pl, systemdefs.SystemVIC20),
		},
		{
			ID:         systemdefs.SystemX68000,
			SystemID:   systemdefs.SystemX68000,
			Folders:    []string{"X68000"},
			Extensions: []string{".d88", ".hdf", ".mgl"},
			Launch:     launch(pl, systemdefs.SystemX68000),
		},
		{
			ID:         systemdefs.SystemZX81,
			SystemID:   systemdefs.SystemZX81,
			Folders:    []string{"ZX81"},
			Extensions: []string{".p", ".0"},
			Launch:     launch(pl, systemdefs.SystemZX81),
		},
		{
			ID:         systemdefs.SystemZXSpectrum,
			SystemID:   systemdefs.SystemZXSpectrum,
			Folders:    []string{"Spectrum"},
			Extensions: []string{".tap", ".csw", ".tzx", ".sna", ".z80", ".trd", ".img", ".dsk", ".mgt", ".vhd"},
			Launch:     launch(pl, systemdefs.SystemZXSpectrum),
		},
		{
			ID:         systemdefs.SystemZXNext,
			SystemID:   systemdefs.SystemZXNext,
			Folders:    []string{"ZXNext"},
			Extensions: []string{".vhd", ".tzx", ".csw"},
			Launch:     launch(pl, systemdefs.SystemZXNext),
		},
		// Other
		{
			ID:         systemdefs.SystemArcade,
			SystemID:   systemdefs.SystemArcade,
			Folders:    []string{"_Arcade"},
			Extensions: []string{".mra"},
			Launch:     launch(pl, systemdefs.SystemArcade),
		},
		{
			ID:         systemdefs.SystemArduboy,
			SystemID:   systemdefs.SystemArduboy,
			Folders:    []string{"Arduboy"},
			Extensions: []string{".hex", ".bin"},
			Launch:     launch(pl, systemdefs.SystemArduboy),
		},
		{
			ID:         systemdefs.SystemChip8,
			SystemID:   systemdefs.SystemChip8,
			Folders:    []string{"Chip8"},
			Extensions: []string{".ch8"},
			Launch:     launch(pl, systemdefs.SystemChip8),
		},
		{
			ID:         systemdefs.SystemGroovy,
			SystemID:   systemdefs.SystemGroovy,
			Folders:    []string{"Groovy"},
			Extensions: []string{".gmc"},
			Launch:     launch(pl, systemdefs.SystemGroovy),
		},
		{
			ID:         "Generic",
			Extensions: []string{".mgl", ".rbf", ".mra"},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				err := mgls.LaunchBasicFile(path)
				if err != nil {
					return nil, fmt.Errorf("failed to launch generic file: %w", err)
				}
				log.Debug().Msgf("setting active game: %s", path)
				err = activegame.SetActiveGame(path)
				if err != nil {
					return nil, fmt.Errorf("failed to set active game: %w", err)
				}
				return nil, nil
			},
		},
	}
}
