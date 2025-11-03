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
	"github.com/rs/zerolog/log"
)

const (
	// HomeDir is hardcoded because home in env is not set at the point which
	// the service file is called to start.
	HomeDir   = "/userdata/system"
	DataDir   = HomeDir + "/.local/share/" + config.AppName
	ConfigDir = HomeDir + "/.config/" + config.AppName
)

type Platform struct {
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
	_ *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	// Try to check for running game with retries during startup
	maxRetries := 10
	baseDelay := 100 * time.Millisecond
	var game models.ActiveMedia
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
			time.Sleep(delay)
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

			game = models.ActiveMedia{
				SystemID:   systemID,
				SystemName: systemMeta.Name,
				Name:       gameResp.Name,
				Path:       gameResp.Path,
			}
			running = true
		}
		break
	}

	if running {
		p.setActiveMedia(&game)
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
		ZipsAsDirs: false,
	}
}

func (p *Platform) PlayAudio(path string) error {
	if !strings.HasSuffix(strings.ToLower(path), ".wav") {
		return fmt.Errorf("unsupported audio format: %s", path)
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(helpers.DataDir(p), path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "aplay", path).Start()
	if err != nil {
		return fmt.Errorf("failed to start aplay command: %w", err)
	}
	return nil
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	log.Info().Msg("stopping active launcher")
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

func (*Platform) ReturnToMenu() error {
	// No menu concept on this platform
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string, launcher *platforms.Launcher) error {
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
		log.Info().Msg("exiting current media")
		err = p.StopActiveLauncher(platforms.StopForPreemption)
		if err != nil {
			return err
		}
		time.Sleep(2500 * time.Millisecond)
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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
			ID:                 launcherID,
			SystemID:           v.SystemID,
			Extensions:         v.Extensions,
			Folders:            []string{k},
			SkipFilesystemScan: true, // Use gamelist.xml via Scanner, no filesystem scanning needed
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
				_ []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				results := []platforms.ScanResult{}

				batSysNames, err := toBatoceraSystems(systemID)
				if err != nil {
					return nil, err
				}

				for _, batSysName := range batSysNames {
					// Check for cancellation before processing each system
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					default:
					}

					for _, rootDir := range p.RootDirs(cfg) {
						// Check for cancellation before processing each root directory
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						default:
						}

						gameListPath := filepath.Join(rootDir, batSysName, "gamelist.xml")
						gameList, err := esapi.ReadGameListXML(gameListPath)
						if err != nil {
							log.Error().Msgf("error reading gamelist.xml: %s", err)
							continue
						}
						for _, game := range gameList.Games {
							results = append(results, platforms.ScanResult{
								Name: game.Name,
								Path: filepath.Join(rootDir, batSysName, game.Path),
							})
						}
					}
				}

				return results, nil
			},
			Test: func(cfg *config.Instance, path string) bool {
				path = filepath.Clean(path)
				path = strings.ToLower(path)
				for _, rootDir := range p.RootDirs(cfg) {
					sysDir := filepath.Join(rootDir, k)
					sysDir = filepath.Clean(sysDir)
					sysDir = strings.ToLower(sysDir)
					if strings.HasPrefix(path, sysDir) {
						if filepath.Ext(path) == "" {
							return false
						}
						if filepath.Ext(path) == ".txt" {
							return false
						}
						return true
					}
				}
				return false
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
