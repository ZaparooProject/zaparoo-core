//go:build linux

package batocera

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// HomeDir is hardcoded because home in env is not set at the point which
	// the service file is called to start.
	HomeDir   = "/userdata/system"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
	LogDir    = DataDir + "/" + config.LogsDir
)

type Platform struct {
	clock          clockwork.Clock
	cfg            *config.Instance
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	trackedProcess *os.Process
	kbd            linuxinput.Keyboard
	gpd            linuxinput.Gamepad
	processMu      sync.RWMutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDBatocera
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		libnfc.NewLegacyUARTReader(cfg),
		libnfc.NewLegacyI2CReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		opticaldrive.NewReader(cfg),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
		tty2oled.NewReader(cfg, p),
	}

	var enabled []readers.Reader
	for _, r := range allReaders {
		metadata := r.Metadata()
		if cfg.IsDriverEnabled(metadata.ID, metadata.DefaultEnabled) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (p *Platform) StartPre(_ *config.Instance) error {
	// Initialize clock if not set (for production use)
	if p.clock == nil {
		p.clock = clockwork.NewRealClock()
	}

	kbd, err := linuxinput.NewKeyboard(linuxinput.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to create keyboard input device: %w", err)
	}
	p.kbd = kbd

	gpd, err := linuxinput.NewGamepad(linuxinput.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to create gamepad input device: %w", err)
	}
	p.gpd = gpd

	return nil
}

func (p *Platform) StartPost(
	cfg *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
) error {
	p.cfg = cfg
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	// Try to check for running game with retries during startup
	maxRetries := 10
	baseDelay := 100 * time.Millisecond
	var game *models.ActiveMedia
	running := false

	for attempt := 0; attempt <= maxRetries; attempt++ {
		gameResp, isRunning, err := esapi.APIRunningGame()
		if err != nil {
			if attempt == maxRetries {
				log.Warn().Err(err).Msg("ES API unavailable after retries, continuing without active media detection")
				p.setActiveMedia(nil)
				return nil
			}

			delay := time.Duration(1<<attempt) * baseDelay
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}

			log.Debug().Msgf("ES API check failed during startup (attempt %d/%d), retrying in %v: %v",
				attempt+1, maxRetries+1, delay, err)
			p.clock.Sleep(delay)
			continue
		}

		// Success - process the result
		if isRunning {
			systemID, err := fromBatoceraSystem(gameResp.SystemName)
			if err != nil {
				log.Warn().Err(err).Msgf("failed to convert system %s, setting no active media", gameResp.SystemName)
				p.setActiveMedia(nil)
				return nil
			}

			systemMeta, err := assets.GetSystemMetadata(systemID)
			if err != nil {
				log.Warn().Err(err).Msgf("failed to get system metadata for %s, setting no active media", systemID)
				p.setActiveMedia(nil)
				return nil
			}

			game = models.NewActiveMedia(
				systemID,
				systemMeta.Name,
				gameResp.Path,
				gameResp.Name,
				"", // LauncherID unknown when detecting already-running game
			)
			running = true
		}
		break
	}

	if running {
		p.setActiveMedia(game)
	} else {
		p.setActiveMedia(nil)
	}

	return nil
}

func (p *Platform) Stop() error {
	err := p.kbd.Close()
	if err != nil {
		log.Warn().Err(err).Msg("error closing keyboard")
	}

	err = p.gpd.Close()
	if err != nil {
		log.Warn().Err(err).Msg("error closing gamepad")
	}

	return nil
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	return append(cfg.IndexRoots(), "/userdata/roms")
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    DataDir,
		ConfigDir:  ConfigDir,
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		LogDir:     LogDir,
		ZipsAsDirs: false,
	}
}

func (p *Platform) StopActiveLauncher(reason platforms.StopIntent) error {
	log.Info().Msg("stopping active launcher")

	// Check if Kodi is the active launcher
	activeMedia := p.activeMedia()
	if activeMedia != nil && isKodiLauncher(activeMedia.LauncherID) {
		// Use Kodi-specific stopping mechanism with reason-based behavior
		return p.stopKodi(p.cfg, reason)
	}

	// Use EmulationStation API for games/emulators
	tries := 0
	maxTries := 10

	killed := false
	for tries < maxTries {
		log.Debug().Msgf("trying to kill launcher: try #%d", tries+1)
		err := esapi.APIEmuKill()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to kill emulator: %w", err)
		}

		_, running, err := esapi.APIRunningGame()
		if err != nil {
			return fmt.Errorf("failed to check running game status: %w", err)
		} else if !running {
			killed = true
			break
		}

		tries++
	}

	if killed {
		log.Info().Msg("stopped active launcher")

		// Also kill tracked process if it exists
		p.processMu.Lock()
		if p.trackedProcess != nil {
			if err := p.trackedProcess.Kill(); err != nil {
				log.Warn().Err(err).Msg("failed to kill tracked process")
			} else {
				log.Debug().Msg("killed tracked process")
			}
			p.trackedProcess = nil
		}
		p.processMu.Unlock()

		p.setActiveMedia(nil)
		return nil
	}
	return errors.New("stop active launcher: failed to stop launcher")
}

func (p *Platform) ReturnToMenu() error {
	// Stop the active launcher (Kodi, game, or emulator) to return to EmulationStation menu
	return p.StopActiveLauncher(platforms.StopForMenu)
}

func (p *Platform) LaunchSystem(_ *config.Instance, systemID string) error {
	if strings.EqualFold(systemID, "menu") {
		return p.ReturnToMenu()
	}

	return errors.New("launching systems is not supported")
}

// shouldKeepRunningInstance checks if we should preserve the currently running application
// when launching new media. Returns true if both current and new launchers use the same
// running instance identifier (e.g., both use "kodi"), meaning they communicate with the
// same persistent application.
func (p *Platform) shouldKeepRunningInstance(cfg *config.Instance, newLauncher *platforms.Launcher) bool {
	// If new launcher doesn't use a running instance, always kill current app
	if newLauncher.UsesRunningInstance == "" {
		return false
	}

	// Get currently active media
	activeMedia := p.activeMedia()
	if activeMedia == nil {
		return false
	}

	// Find the current launcher to check if it shares the same running instance
	// TODO: This performs an O(N) linear scan over all launchers on every media launch.
	launchers := p.Launchers(cfg)
	for i := range launchers {
		if launchers[i].ID == activeMedia.LauncherID {
			// Keep running if both launchers use the same instance identifier
			if launchers[i].UsesRunningInstance == newLauncher.UsesRunningInstance {
				log.Info().Msgf("keeping running %s instance for launcher transition: %s -> %s",
					newLauncher.UsesRunningInstance, launchers[i].ID, newLauncher.ID)
				return true
			}
			// Found the launcher but it uses a different instance, don't keep running
			break
		}
	}

	return false
}

// isKodiLauncher checks if the given launcher ID is a Kodi launcher
// TODO: shouldn't be hardcoding this list
func isKodiLauncher(launcherID string) bool {
	kodiLaunchers := []string{
		"KodiLocalVideo", "KodiMovie", "KodiTVEpisode", "KodiLocalAudio",
		"KodiSong", "KodiAlbum", "KodiArtist", "KodiTVShow",
	}
	for _, id := range kodiLaunchers {
		if launcherID == id {
			return true
		}
	}
	return false
}

// stopKodi stops Kodi based on the stop intent:
// - StopForMenu: Stop playback only, keep Kodi running (return to Kodi main menu)
// - StopForPreemption: Quit Kodi entirely (launching a game or different app)
func (p *Platform) stopKodi(cfg *config.Instance, reason platforms.StopIntent) error {
	client := kodi.NewClient(cfg)

	// If stopping to return to menu (e.g., stop command or launch.system:menu),
	// just stop playback but keep Kodi running ("Kodi mode" stays active)
	if reason == platforms.StopForMenu {
		log.Info().Msg("stopping Kodi playback (Kodi mode stays active)")
		if err := client.Stop(); err != nil {
			return fmt.Errorf("failed to stop Kodi playback: %w", err)
		}
		// Don't clear activeMedia - Kodi is still running and we're still in "Kodi mode"
		log.Info().Msg("Kodi playback stopped, returned to Kodi main menu")
		return nil
	}

	// StopForPreemption: Launching a game or different app, quit Kodi entirely
	log.Info().Msg("quitting Kodi (exiting Kodi mode)")

	// Try graceful quit via JSON-RPC with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Quit(ctx); err == nil {
		log.Info().Msg("Kodi stopped gracefully via JSON-RPC")
		p.setActiveMedia(nil)

		// Also clear the tracked process since Kodi exited gracefully
		p.processMu.Lock()
		p.trackedProcess = nil
		p.processMu.Unlock()

		return nil
	}

	log.Debug().Msg("JSON-RPC quit failed, attempting process kill")

	// Fallback: kill the tracked process
	p.processMu.Lock()
	defer p.processMu.Unlock()

	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill Kodi process")
			return fmt.Errorf("failed to stop Kodi: %w", err)
		}
		log.Info().Msg("Kodi process killed")
		p.trackedProcess = nil
	}

	p.setActiveMedia(nil)
	return nil
}

func (p *Platform) LaunchMedia(
	cfg *config.Instance, path string, launcher *platforms.Launcher, db *database.Database,
) error {
	log.Info().Msgf("launch media: %s", path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	// exit current media if one is running
	_, running, err := esapi.APIRunningGame()
	if err != nil {
		return fmt.Errorf("failed to check running game status: %w", err)
	} else if running {
		// Check if we should preserve the running app (e.g., both launchers use same Kodi instance)
		if !p.shouldKeepRunningInstance(cfg, launcher) {
			log.Info().Msg("exiting current media")
			err = p.StopActiveLauncher(platforms.StopForPreemption)
			if err != nil {
				return err
			}
			time.Sleep(2500 * time.Millisecond)
		} else {
			// We're keeping the running instance (e.g., switching between Kodi movies)
			// Clear the old activeMedia first to trigger media.stopped, then DoLaunch will set the new one
			log.Info().Msg("keeping running instance, clearing old active media before switching")
			p.setActiveMedia(nil)
		}
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(&helpers.LaunchParams{
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: p.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
	})
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	return nil
}

func (p *Platform) KeyboardPress(name string) error {
	code, ok := linuxinput.ToKeyboardCode(name)
	if !ok {
		return fmt.Errorf("unknown keyboard key: %s", name)
	}
	err := p.kbd.Press(code)
	if err != nil {
		return fmt.Errorf("failed to press keyboard key %s: %w", name, err)
	}
	return nil
}

func (p *Platform) GamepadPress(name string) error {
	code, ok := linuxinput.ToGamepadCode(name)
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}
	err := p.gpd.Press(code)
	if err != nil {
		return fmt.Errorf("failed to press gamepad button %s: %w", name, err)
	}
	return nil
}

func (*Platform) ForwardCmd(env *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, fmt.Errorf("command not supported on batocera: %s", env.Cmd)
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := []platforms.Launcher{
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				err := exec.CommandContext(context.Background(), path).Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start command: %w", err)
				}
				return nil, nil //nolint:nilnil // Command launches don't return a process handle
			},
		},
	}

	for k, v := range SystemMap {
		launcherID, ok := LauncherMap[k]
		if !ok {
			log.Error().Msgf("unknown batocera launcher: %s", k)
			continue
		}

		launchers = append(launchers, platforms.Launcher{
			ID:         launcherID,
			SystemID:   v.SystemID,
			Extensions: v.Extensions,
			Folders:    []string{k},
			// SkipFilesystemScan defaults to false - built-in scanner will find all ROM files
			// Scanner function adds metadata from gamelist.xml
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				err := esapi.APILaunch(path)
				if err != nil {
					return nil, fmt.Errorf("failed to launch via API: %w", err)
				}
				return nil, nil //nolint:nilnil // API launches don't return a process handle
			},
			Scanner: func(
				ctx context.Context,
				cfg *config.Instance,
				systemID string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				// Filter out metadata files as a defensive guard
				metadataDirs := []string{"images", "videos", "manuals", "marquees", "thumbnails", "wheels", "fanart"}
				filteredResults := make([]platforms.ScanResult, 0, len(results))
				for _, result := range results {
					// Skip gamelist.xml files
					if strings.HasSuffix(strings.ToLower(result.Path), "gamelist.xml") {
						continue
					}

					// Skip files in metadata directories
					pathLower := strings.ToLower(result.Path)
					inMetadataDir := false
					for _, metaDir := range metadataDirs {
						// Check if path contains /metadatadir/ (with separator on both sides)
						if strings.Contains(pathLower, string(filepath.Separator)+metaDir+string(filepath.Separator)) {
							inMetadataDir = true
							break
						}
					}
					if inMetadataDir {
						continue
					}

					filteredResults = append(filteredResults, result)
				}
				results = filteredResults

				batSysNames, err := toBatoceraSystems(systemID)
				if err != nil {
					return results, err
				}

				// Create map of existing results for quick lookup
				resultMap := make(map[string]*platforms.ScanResult)
				for i := range results {
					resultMap[results[i].Path] = &results[i]
				}

				// Process each Batocera system name for this Zaparoo system
				for _, batSysName := range batSysNames {
					// Check for cancellation
					select {
					case <-ctx.Done():
						return results, ctx.Err()
					default:
					}

					// Try each root directory
					for _, rootDir := range p.RootDirs(cfg) {
						systemDir := filepath.Join(rootDir, batSysName)
						gameListPath := filepath.Join(systemDir, "gamelist.xml")

						// Read gamelist.xml for metadata
						gameList, err := esapi.ReadGameListXML(gameListPath)
						if err != nil {
							log.Debug().Err(err).Msgf("no gamelist.xml for %s", batSysName)
							continue // No gamelist.xml is fine - use filesystem names
						}

						// Update names from gamelist.xml metadata
						for _, game := range gameList.Games {
							// Clean the game path (remove leading ./)
							gamePath := filepath.Clean(game.Path)
							fullPath := filepath.Join(systemDir, gamePath)

							// If this game was found by filesystem scanner, update its name
							if result, exists := resultMap[fullPath]; exists {
								result.Name = game.Name
							}
						}
					}
				}

				return results, nil
			},
		})
	}

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
}

func (*Platform) ShowNotice(
	_ *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	if err := esapi.APINotify(args.Text); err != nil {
		return nil, 0, fmt.Errorf("failed to show notice: %w", err)
	}
	return nil, 0, nil
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, error) {
	if err := esapi.APINotify(args.Text); err != nil {
		return nil, fmt.Errorf("failed to show loader: %w", err)
	}
	return func() error { return nil }, nil
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}
