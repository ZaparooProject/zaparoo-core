//go:build linux && !android

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// PhysicalDiscLauncherPrefix starts the ID of each launcher that plays the
	// disc in a drive through a physical CD provider, as in PhysicalDiscPSX.
	PhysicalDiscLauncherPrefix = "PhysicalDisc"
	// PSXCDPlayerLauncherID plays audio CDs and .cue rips with the PSX core's
	// CD player.
	PSXCDPlayerLauncherID = "PSXCDPlayer"
	// MiSTerHiFiLauncherID plays audio with the MiSTer Hi-Fi script.
	MiSTerHiFiLauncherID = "MiSTerHiFi"

	hiFiScriptName    = "misterhifi.sh"
	hiFiPhysicalCDArg = "--physical-cd"
	dvdPlayerRBF      = "DVD_Player.rbf"
)

var (
	errNoDiscProvider = errors.New("no physical CD provider is installed")
	errNoDiscLauncher = errors.New("the installed physical CD provider cannot play this system")
	errNoDVDCore      = errors.New("no DVD core is installed")
	errNotDiscPath    = errors.New("not a physical disc path")

	// physicalDiscPath is physicaldisc://<drive>/<system>.
	physicalDiscPath = regexp.MustCompile(`^(?i:physicaldisc)://(sr\d{1,3})/([A-Za-z0-9]+)$`)
	// cdAudioPath is cdaudio://<drive>, or cdaudio://<drive>/<track>.
	cdAudioPath = regexp.MustCompile(`^(?i:cdaudio)://(sr\d{1,3})(?:/(\d{1,2}))?$`)

	hiFiExtensions = []string{".mp3", ".wav", ".flac", ".ogg", ".oga", ".m4a", ".m3u", ".m3u8"}
)

// discProvider is a third-party Main build that streams a disc in a drive to
// a core. Each ships an MGL per system it supports, in its own folder.
type discProvider struct {
	mgls   map[string]string
	binary string
	mglDir string
}

// discProviders are tried in order; the first installed one with an MGL for
// the system plays the disc.
var discProviders = []discProvider{
	{
		binary: "MiSTer_Physical-CD",
		mglDir: "_Physical Disc Cores",
		mgls: map[string]string{
			systemdefs.SystemMegaCD:         "MegaCD.mgl",
			systemdefs.SystemSaturn:         "Saturn.mgl",
			systemdefs.SystemPSX:            "PSX.mgl",
			systemdefs.SystemTurboGrafx16CD: "TurboGrafx16-CD.mgl",
			systemdefs.SystemNeoGeoCD:       "NeoGeoCD.mgl",
			systemdefs.System3DO:            "3DO.mgl",
			systemdefs.SystemCDI:            "CDi.mgl",
			systemdefs.SystemGenesisPlus:    "MDPlus.mgl",
			systemdefs.SystemSNESMSU1:       "SNES-MSU1.mgl",
		},
	},
	{
		binary: "MiSTer-disc",
		mglDir: "_Disc_Cores",
		mgls: map[string]string{
			systemdefs.SystemMegaCD:         "Mega CD.mgl",
			systemdefs.SystemSaturn:         "Saturn.mgl",
			systemdefs.SystemPSX:            "PlayStation.mgl",
			systemdefs.SystemTurboGrafx16CD: "TurboGrafx-CD.mgl",
			systemdefs.SystemNeoGeoCD:       "Neo Geo CD.mgl",
			systemdefs.System3DO:            "3DO.mgl",
			systemdefs.SystemCDI:            "Philips CD-i.mgl",
		},
	},
}

// physicalDiscSystems is every system a disc in a drive can be launched as,
// in a fixed order.
var physicalDiscSystems = []string{
	systemdefs.SystemMegaCD,
	systemdefs.SystemSaturn,
	systemdefs.SystemPSX,
	systemdefs.SystemTurboGrafx16CD,
	systemdefs.SystemNeoGeoCD,
	systemdefs.System3DO,
	systemdefs.SystemCDI,
	systemdefs.SystemGenesisPlus,
	systemdefs.SystemSNESMSU1,
	systemdefs.SystemDVDPlayer,
}

// parsePhysicalDiscPath splits physicaldisc://<drive>/<system>.
func parsePhysicalDiscPath(path string) (drive, system string, ok bool) {
	m := physicalDiscPath.FindStringSubmatch(path)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// parseCDAudioPath splits cdaudio://<drive>[/<track>]. Track is 0 for the
// whole disc.
func parseCDAudioPath(path string) (drive string, track int, ok bool) {
	m := cdAudioPath.FindStringSubmatch(path)
	if m == nil {
		return "", 0, false
	}
	if m[2] != "" {
		n, err := strconv.Atoi(m[2])
		if err != nil || n < 1 || n > 99 {
			return "", 0, false
		}
		track = n
	}
	return m[1], track, true
}

func driveExists(fs afero.Fs, drive string) bool {
	_, err := fs.Stat("/dev/" + drive)
	return err == nil
}

func fileExists(fs afero.Fs, path string) bool {
	info, err := fs.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// resolveDiscProviderMGL returns the MGL the first installed provider plays
// system with. A provider counts as installed when its Main build is in the
// SD root.
func resolveDiscProviderMGL(fs afero.Fs, root, system string) (string, error) {
	installed := false
	for _, provider := range discProviders {
		if !fileExists(fs, filepath.Join(root, provider.binary)) {
			continue
		}
		installed = true
		name, ok := provider.mgls[system]
		if !ok {
			continue
		}
		mgl := filepath.Join(root, provider.mglDir, name)
		if fileExists(fs, mgl) {
			return mgl, nil
		}
	}
	if !installed {
		return "", errNoDiscProvider
	}
	return "", fmt.Errorf("%w: %s", errNoDiscLauncher, system)
}

// datedDVDCore returns the date of a DVD_YYYYMMDD.rbf core, or 0 when name
// is not one.
func datedDVDCore(name string) int {
	if len(name) != len("DVD_YYYYMMDD.rbf") || !strings.HasPrefix(name, "DVD_") ||
		!strings.EqualFold(name[12:], ".rbf") {
		return 0
	}
	date := 0
	for _, c := range name[4:12] {
		if c < '0' || c > '9' {
			return 0
		}
		date = date*10 + int(c-'0')
	}
	return date
}

type dvdCores struct {
	dated     string
	hybrid    string
	datedDate int
}

// findDVDCores walks dir and its _-prefixed subfolders for DVD cores. It
// lists names only and stats just the _-prefixed entries and core names: the
// walk covers _Arcade, which holds thousands of files on a typical SD card.
func findDVDCores(fs afero.Fs, dir string, found *dvdCores) {
	d, err := fs.Open(dir)
	if err != nil {
		return
	}
	names, err := d.Readdirnames(-1)
	if closeErr := d.Close(); closeErr != nil {
		log.Debug().Err(closeErr).Str("dir", dir).Msg("closing directory")
	}
	if err != nil {
		return
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		hybrid := strings.EqualFold(name, dvdPlayerRBF)
		date := datedDVDCore(name)
		if !strings.HasPrefix(name, "_") && !hybrid && date == 0 {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := fs.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if strings.HasPrefix(name, "_") {
				findDVDCores(fs, path, found)
			}
			continue
		}
		if hybrid {
			if found.hybrid == "" {
				found.hybrid = path
			}
			continue
		}
		if date > found.datedDate {
			found.datedDate = date
			found.dated = path
		}
	}
}

// resolveDVDCore returns the newest DVD_YYYYMMDD.rbf core, else the hybrid
// DVD_Player.rbf.
func resolveDVDCore(fs afero.Fs, root string) (string, error) {
	var found dvdCores
	findDVDCores(fs, root, &found)
	if found.dated != "" {
		return found.dated, nil
	}
	if found.hybrid != "" {
		return found.hybrid, nil
	}
	return "", errNoDVDCore
}

// resolvePhysicalDiscTarget returns the file MiSTer loads to play a disc of
// system from a drive.
func resolvePhysicalDiscTarget(fs afero.Fs, root, system string) (string, error) {
	if system == systemdefs.SystemDVDPlayer {
		return resolveDVDCore(fs, root)
	}
	return resolveDiscProviderMGL(fs, root, system)
}

// physicalDiscTarget checks path names a disc of system in a drive and
// returns the file that plays it.
func physicalDiscTarget(fs afero.Fs, root, system, path string) (string, error) {
	_, pathSystem, ok := parsePhysicalDiscPath(path)
	if !ok || !strings.EqualFold(pathSystem, system) {
		return "", fmt.Errorf("%w: %s", errNotDiscPath, path)
	}
	return resolvePhysicalDiscTarget(fs, root, system)
}

// createPhysicalDiscLaunchers returns a launcher per system a disc in a drive
// can be, so the media it starts is reported under that system. Playing the
// disc itself needs a physical CD provider, or a DVD core for DVDs; Core only
// loads it.
func createPhysicalDiscLaunchers(pl *Platform) []platforms.Launcher {
	ls := make([]platforms.Launcher, 0, len(physicalDiscSystems))
	for _, system := range physicalDiscSystems {
		ls = append(ls, platforms.Launcher{
			ID:                 PhysicalDiscLauncherPrefix + system,
			SystemID:           system,
			Schemes:            []string{readers.PhysicalDiscScheme},
			SkipFilesystemScan: true,
			Test: func(_ *config.Instance, path string) bool {
				_, pathSystem, ok := parsePhysicalDiscPath(path)
				return ok && strings.EqualFold(pathSystem, system)
			},
			VirtualPathExists: func(_ *config.Instance, path string) bool {
				drive, pathSystem, ok := parsePhysicalDiscPath(path)
				return ok && strings.EqualFold(pathSystem, system) && driveExists(pl.filesystem(), drive)
			},
			// Launch resolution checks every launcher's availability, so a
			// DVD core is looked for only when a DVD is played: finding one
			// walks the SD card.
			Availability: func(_ *config.Instance) error {
				if system == systemdefs.SystemDVDPlayer {
					return nil
				}
				_, err := resolvePhysicalDiscTarget(pl.filesystem(), misterconfig.SDRootDir, system)
				return err
			},
			// The path is checked before running media is stopped.
			Preflight: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
				_, err := physicalDiscTarget(pl.filesystem(), misterconfig.SDRootDir, system, path)
				return err
			},
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				target, err := physicalDiscTarget(pl.filesystem(), misterconfig.SDRootDir, system, path)
				if err != nil {
					return nil, err
				}
				if err := pl.closeLaunchConsole(); err != nil {
					log.Warn().Err(err).Msg("failed to close console before FPGA launch")
				}
				log.Info().Str("system", system).Str("target", target).Msg("playing the disc in the drive")
				if err := pl.launchBasicFilePath(target); err != nil {
					logMGLLaunchError(err)
					return nil, err
				}
				return nil, nil //nolint:nilnil // FPGA launches have no process handle.
			},
		})
	}
	return ls
}

// cueRip returns the cue sheet a .cue path or a <file>.cue/<n> track path
// names.
func cueRip(path string) (string, bool) {
	if file, _, ok := audio.SplitTrackPath(path); ok {
		path = file
	}
	return path, strings.EqualFold(filepath.Ext(path), ".cue")
}

// createPSXCDPlayerLauncher plays audio CDs with the PSX core's CD player: a
// .cue rip is loaded into the core's CD drive, and a disc in a drive through
// the physical CD provider's PSX MGL.
func createPSXCDPlayerLauncher(pl *Platform) platforms.Launcher {
	launchRip := launch(pl, systemdefs.SystemPSX)
	return platforms.Launcher{
		ID:                 PSXCDPlayerLauncherID,
		SystemID:           systemdefs.SystemAudio,
		ExplicitOnly:       true,
		Extensions:         []string{".cue"},
		Schemes:            []string{readers.CDAudioScheme},
		SkipFilesystemScan: true,
		Test: func(_ *config.Instance, path string) bool {
			if _, _, ok := parseCDAudioPath(path); ok {
				return true
			}
			_, ok := cueRip(path)
			return ok
		},
		Preflight: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
			if _, _, ok := parseCDAudioPath(path); ok {
				_, err := resolveDiscProviderMGL(pl.filesystem(), misterconfig.SDRootDir, systemdefs.SystemPSX)
				return err
			}
			if _, ok := cueRip(path); !ok {
				return fmt.Errorf("PSX CD player plays .cue rips and audio CDs, not %s", path)
			}
			return nil
		},
		Launch: func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
			if _, _, ok := parseCDAudioPath(path); ok {
				mgl, err := resolveDiscProviderMGL(pl.filesystem(), misterconfig.SDRootDir, systemdefs.SystemPSX)
				if err != nil {
					return nil, err
				}
				if err := pl.closeLaunchConsole(); err != nil {
					log.Warn().Err(err).Msg("failed to close console before FPGA launch")
				}
				if err := pl.launchBasicFilePath(mgl); err != nil {
					logMGLLaunchError(err)
					return nil, err
				}
				return nil, nil //nolint:nilnil // FPGA launches have no process handle.
			}
			cue, ok := cueRip(path)
			if !ok {
				return nil, fmt.Errorf("PSX CD player plays .cue rips and audio CDs, not %s", path)
			}
			// The core's CD player starts at the first track; it cannot be
			// told to start at another.
			return launchRip(cfg, cue, opts)
		},
	}
}

func hiFiScriptPath() string {
	return filepath.Join(misterconfig.ScriptsDir, hiFiScriptName)
}

// hiFiArgs is the argument MiSTer Hi-Fi plays path with: --physical-cd for
// the disc in a drive, else the file.
func hiFiArgs(path string) (string, error) {
	if _, track, ok := parseCDAudioPath(path); ok {
		if track != 0 {
			return "", errors.New("MiSTer Hi-Fi plays a whole audio CD, not one track")
		}
		return hiFiPhysicalCDArg, nil
	}
	if strings.Contains(path, "://") {
		return "", fmt.Errorf("MiSTer Hi-Fi cannot play %s", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range hiFiExtensions {
		if ext == e {
			return command.ShellQuote(path), nil
		}
	}
	return "", fmt.Errorf("MiSTer Hi-Fi cannot play %s files", ext)
}

// createMiSTerHiFiLauncher plays audio with the MiSTer Hi-Fi script, when it
// is installed.
func createMiSTerHiFiLauncher(pl *Platform) platforms.Launcher {
	return platforms.Launcher{
		ID:                 MiSTerHiFiLauncherID,
		SystemID:           systemdefs.SystemAudio,
		ExplicitOnly:       true,
		Extensions:         hiFiExtensions,
		Schemes:            []string{readers.CDAudioScheme},
		SkipFilesystemScan: true,
		NoActiveMedia:      true,
		Test: func(_ *config.Instance, path string) bool {
			_, track, ok := parseCDAudioPath(path)
			return ok && track == 0
		},
		Availability: func(_ *config.Instance) error {
			if !fileExists(pl.filesystem(), hiFiScriptPath()) {
				return errors.New("MiSTer Hi-Fi is not installed")
			}
			return nil
		},
		Preflight: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
			_, err := hiFiArgs(path)
			return err
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			args, err := hiFiArgs(path)
			if err != nil {
				return nil, err
			}
			ctx := context.Background()
			if pl.launcherManager != nil {
				ctx = pl.launcherManager.GetContext()
			}
			// Hi-Fi runs on the console, which a running core holds.
			if scriptFPGAActive(pl) {
				if err := scriptReturnToMenu(pl); err != nil {
					return nil, fmt.Errorf("failed to return to menu: %w", err)
				}
				if err := waitForMenuCore(ctx); err != nil {
					return nil, err
				}
			}
			if err := runScriptContext(ctx, pl, hiFiScriptPath(), args, false, ""); err != nil {
				return nil, err
			}
			return nil, nil //nolint:nilnil // No process handle; the runner owns it.
		},
	}
}

// createDiscLaunchers returns the launchers for discs in a drive and audio
// CDs.
func createDiscLaunchers(pl *Platform) []platforms.Launcher {
	return append(createPhysicalDiscLaunchers(pl), createPSXCDPlayerLauncher(pl), createMiSTerHiFiLauncher(pl))
}
