//go:build linux

package batocera

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
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
	kbd            linuxinput.Keyboard
	gpd            linuxinput.Gamepad
}

func (*Platform) ID() string {
	return platforms.PlatformIDBatocera
}

func (*Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		libnfc.NewReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		opticaldrive.NewReader(cfg),
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
	cfg *config.Instance,
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
		gameResp, isRunning, err := apiRunningGame()
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
				Path:       p.NormalizePath(cfg, gameResp.Path),
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

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	originalPath := path
	newPath := strings.ReplaceAll(path, "\\", "/")
	lowerPath := strings.ToLower(newPath)

	gotRoot := false
	for _, rootDir := range p.RootDirs(cfg) {
		rootDir = strings.ReplaceAll(rootDir, "\\", "/")
		rootDir = strings.ToLower(rootDir)
		if strings.HasPrefix(lowerPath, rootDir) {
			gotRoot = true
			newPath = path[len(rootDir):]
			if newPath != "" && newPath[0] == '/' {
				newPath = newPath[1:]
			}
			break
		}
	}
	if !gotRoot {
		return originalPath
	}

	parts := strings.Split(newPath, "/")
	if len(parts) < 2 {
		return originalPath
	}

	system, err := fromBatoceraSystem(parts[0])
	if err != nil || system == "" {
		return originalPath
	}

	return system + "/" + strings.Join(parts[1:], "/")
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

func (p *Platform) StopActiveLauncher() error {
	log.Info().Msg("stopping active launcher")
	tries := 0
	maxTries := 10

	killed := false
	for tries < maxTries {
		log.Debug().Msgf("trying to kill launcher: try #%d", tries+1)
		err := apiEmuKill()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		_, running, err := apiRunningGame()
		if err != nil {
			return err
		} else if !running {
			killed = true
			break
		}

		tries++
	}

	if killed {
		log.Info().Msg("stopped active launcher")
		p.setActiveMedia(nil)
		return nil
	}
	return errors.New("stop active launcher: failed to stop launcher")
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	launcher, err := helpers.FindLauncher(cfg, p, path)
	if err != nil {
		return fmt.Errorf("launch media: error finding launcher: %w", err)
	}

	// exit current media if one is running
	_, running, err := apiRunningGame()
	if err != nil {
		return err
	} else if running {
		log.Info().Msg("exiting current media")
		err = p.StopActiveLauncher()
		if err != nil {
			return err
		}
		time.Sleep(2500 * time.Millisecond)
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(cfg, p, p.setActiveMedia, &launcher, path)
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

type ESGame struct {
	Name string `xml:"name"`
	Path string `xml:"path"`
}

type ESGameList struct {
	XMLName xml.Name `xml:"gameList"`
	Games   []ESGame `xml:"game"`
}

func readESGameListXML(path string) (ESGameList, error) {
	xmlFile, err := os.Open(path) //nolint:gosec // Internal EmulationStation gamelist XML path
	if err != nil {
		return ESGameList{}, fmt.Errorf("failed to open ES game list XML file %s: %w", path, err)
	}
	defer func(xmlFile *os.File) {
		closeErr := xmlFile.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing xml file")
		}
	}(xmlFile)

	data, err := io.ReadAll(xmlFile)
	if err != nil {
		return ESGameList{}, fmt.Errorf("failed to read ES game list XML file %s: %w", path, err)
	}

	var gameList ESGameList
	err = xml.Unmarshal(data, &gameList)
	if err != nil {
		return ESGameList{}, fmt.Errorf("failed to unmarshal ES game list XML: %w", err)
	}

	return gameList, nil
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
			Launch: func(_ *config.Instance, path string) error {
				return exec.CommandContext(context.Background(), path).Start()
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
			Launch: func(_ *config.Instance, path string) error {
				return apiLaunch(path)
			},
			Scanner: func(
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
					for _, rootDir := range p.RootDirs(cfg) {
						gameListPath := filepath.Join(rootDir, batSysName, "gamelist.xml")
						gameList, err := readESGameListXML(gameListPath)
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
	return nil, 0, apiNotify(args.Text)
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, apiNotify(args.Text)
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}
