package batocera

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
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
	kbd            linuxinput.Keyboard
	gpd            linuxinput.Gamepad
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
}

func (p *Platform) ID() string {
	return platforms.PlatformIDBatocera
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		libnfc.NewReader(cfg),
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		optical_drive.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ *config.Instance) error {
	kbd, err := linuxinput.NewKeyboard(linuxinput.DefaultTimeout)
	if err != nil {
		return err
	}
	p.kbd = kbd

	gpd, err := linuxinput.NewGamepad(linuxinput.DefaultTimeout)
	if err != nil {
		return err
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

	game, running, err := apiRunningGame()
	if err != nil {
		return err
	}
	if running {
		systemID, err := fromBatoceraSystem(game.SystemName)
		if err != nil {
			return err
		}

		systemMeta, err := assets.GetSystemMetadata(systemID)
		if err != nil {
			return err
		}

		p.setActiveMedia(&models.ActiveMedia{
			SystemID:   systemID,
			SystemName: systemMeta.Name,
			Name:       game.Name,
			Path:       p.NormalizePath(cfg, game.Path),
		})
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

func (p *Platform) ScanHook(_ tokens.Token) error {
	return nil
}

func (p *Platform) RootDirs(_ *config.Instance) []string {
	return []string{"/userdata/roms"}
}

func (p *Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    DataDir,
		ConfigDir:  ConfigDir,
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		ZipsAsDirs: false,
	}
}

func (p *Platform) NormalizePath(_ *config.Instance, path string) string {
	originalPath := path
	newPath := strings.ReplaceAll(path, "\\", "/")
	lowerPath := strings.ToLower(newPath)

	gotRoot := false
	for _, rootDir := range p.RootDirs(nil) {
		rootDir = strings.ReplaceAll(rootDir, "\\", "/")
		rootDir = strings.ToLower(rootDir)
		if strings.HasPrefix(lowerPath, rootDir) {
			gotRoot = true
			newPath = path[len(rootDir):]
			if len(newPath) > 0 && newPath[0] == '/' {
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
		path = filepath.Join(p.Settings().DataDir, path)
	}

	return exec.Command("aplay", path).Start()
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
	} else {
		return fmt.Errorf("stop active launcher: failed to stop launcher")
	}
}

func (p *Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return fmt.Errorf("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	launcher, err := utils.FindLauncher(cfg, p, path)
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
	err = utils.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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
	return p.kbd.Press(code)
}

func (p *Platform) GamepadPress(name string) error {
	code, ok := linuxinput.GamepadMap[name]
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}
	return p.gpd.Press(code)
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, fmt.Errorf("command not supported on batocera: %s", env.Cmd)
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

type ESGameList struct {
	XMLName xml.Name `xml:"gameList"`
	Games   []struct {
		Name string `xml:"name"`
		Path string `xml:"path"`
	} `xml:"game"`
}

func readESGameListXML(path string) (ESGameList, error) {
	xmlFile, err := os.Open(path)
	if err != nil {
		return ESGameList{}, err
	}
	defer func(xmlFile *os.File) {
		err := xmlFile.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing xml file")
		}
	}(xmlFile)

	data, err := io.ReadAll(xmlFile)
	if err != nil {
		return ESGameList{}, err
	}

	var gameList ESGameList
	err = xml.Unmarshal(data, &gameList)
	if err != nil {
		return ESGameList{}, err
	}

	return gameList, nil
}

func (p *Platform) Launchers() []platforms.Launcher {
	launchers := []platforms.Launcher{
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command(path).Start()
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
			ID:       launcherID,
			SystemID: v,
			Folders:  []string{k},
			Launch: func(cfg *config.Instance, path string) error {
				return apiLaunch(path)
			},
			Scanner: func(
				cfg *config.Instance,
				systemID string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				// drop existing results since they're just junk here
				results = []platforms.ScanResult{}

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

	return launchers
}

func (p *Platform) ShowNotice(
	_ *config.Instance,
	args widgetModels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, apiNotify(args.Text)
}

func (p *Platform) ShowLoader(
	_ *config.Instance,
	args widgetModels.NoticeArgs,
) (func() error, error) {
	return nil, apiNotify(args.Text)
}

func (p *Platform) ShowPicker(
	_ *config.Instance,
	_ widgetModels.PickerArgs,
) error {
	return nil
}
