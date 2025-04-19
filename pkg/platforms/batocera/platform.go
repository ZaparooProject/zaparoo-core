package batocera

import (
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"github.com/bendahl/uinput"
	"github.com/wizzomafizzo/mrext/pkg/input"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	AssetsDir            = "assets"
	SuccessSoundFilename = "success.wav"
	FailSoundFilename    = "fail.wav"
)

type Platform struct {
	kbd input.Keyboard
	gpd uinput.Gamepad
}

func (p *Platform) Id() string {
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
	for _, dir := range []string{
		p.DataDir(),
		p.LogDir(),
		p.ConfigDir(),
		p.TempDir(),
		filepath.Join(p.DataDir(), AssetsDir),
	} {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return fmt.Errorf("failed to create dir: %w", err)
		}
	}

	kbd, err := input.NewKeyboard()
	if err != nil {
		return err
	}

	p.kbd = kbd

	gpd, err := uinput.CreateGamepad(
		"/dev/uinput",
		[]byte("zaparoo"),
		0x1234,
		0x5678,
	)
	if err != nil {
		return err
	}
	p.gpd = gpd

	successPath := filepath.Join(p.DataDir(), AssetsDir, SuccessSoundFilename)
	if _, err := os.Stat(successPath); err != nil {
		sf, err := os.Create(successPath)
		if err != nil {
			log.Error().Msgf("error creating success sound file: %s", err)
		}
		_, err = sf.Write(assets.SuccessSound)
		if err != nil {
			log.Error().Msgf("error writing success sound file: %s", err)
		}
		_ = sf.Close()
	}

	failPath := filepath.Join(p.DataDir(), AssetsDir, FailSoundFilename)
	if _, err := os.Stat(failPath); err != nil {
		// copy fail sound to temp
		ff, err := os.Create(failPath)
		if err != nil {
			log.Error().Msgf("error creating fail sound file: %s", err)
		}
		_, err = ff.Write(assets.FailSound)
		if err != nil {
			log.Error().Msgf("error writing fail sound file: %s", err)
		}
		_ = ff.Close()
	}

	return nil
}

func (p *Platform) StartPost(_ *config.Instance, _ chan<- models.Notification) error {
	return nil
}

func (p *Platform) Stop() error {
	if p.gpd != nil {
		err := p.gpd.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Platform) AfterScanHook(_ tokens.Token) error {
	return nil
}

func (p *Platform) ReadersUpdateHook(_ map[string]*readers.Reader) error {
	return nil
}

func (p *Platform) RootDirs(_ *config.Instance) []string {
	return []string{"/userdata/roms"}
}

func (p *Platform) ZipsAsDirs() bool {
	return false
}

func (p *Platform) DataDir() string {
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) LogDir() string {
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) ConfigDir() string {
	return filepath.Join(xdg.ConfigHome, config.AppName)
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(_ *config.Instance, path string) string {
	return path
}

func (p *Platform) KillLauncher() error {
	tries := 0
	maxTries := 10
	for tries < maxTries {
		log.Debug().Msgf("trying to kill launcher: %d", tries+1)
		err := apiEmuKill()
		if err != nil {
			log.Error().Msgf("error killing launcher: %s", err)
		}
		tries++
	}
	return nil
}

func (p *Platform) GetActiveLauncher() string {
	game, running, err := apiRunningGame()
	if err != nil {
		log.Error().Msgf("error getting running game: %s", err)
		return ""
	} else if !running {
		return ""
	} else {
		system, err := fromBatoceraSystem(game.SystemName)
		if err != nil {
			log.Error().Msgf("error converting system name: %s", err)
			return ""
		}
		return system
	}
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
	if !cfg.AudioFeedback() {
		return
	}
	failPath := filepath.Join(p.DataDir(), AssetsDir, FailSoundFilename)
	err := exec.Command("aplay", failPath).Start()
	if err != nil {
		log.Error().Msgf("error playing fail sound: %s", err)
	}
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
	if !cfg.AudioFeedback() {
		return
	}
	successPath := filepath.Join(p.DataDir(), AssetsDir, SuccessSoundFilename)
	err := exec.Command("aplay", successPath).Start()
	if err != nil {
		log.Error().Msgf("error playing success sound: %s", err)
	}
}

func (p *Platform) ActiveSystem() string {
	game, running, err := apiRunningGame()
	if err != nil {
		log.Error().Msgf("error getting running game: %s", err)
		return ""
	} else if !running {
		return ""
	} else {
		system, err := fromBatoceraSystem(game.SystemName)
		if err != nil {
			log.Error().Msgf("error converting system name: %s", err)
			return ""
		}
		return system
	}
}

func (p *Platform) ActiveGame() string {
	game, running, err := apiRunningGame()
	if err != nil {
		log.Error().Msgf("error getting running game: %s", err)
		return ""
	} else if !running {
		return ""
	} else {
		system, err := fromBatoceraSystem(game.SystemName)
		if err != nil {
			log.Error().Msgf("error converting system name: %s", err)
			return ""
		}
		return system + "/" + game.Name
	}
}

func (p *Platform) ActiveGameName() string {
	game, running, err := apiRunningGame()
	if err != nil {
		log.Error().Msgf("error getting running game: %s", err)
		return ""
	} else if !running {
		return ""
	} else {
		return game.Name
	}
}

func (p *Platform) ActiveGamePath() string {
	game, running, err := apiRunningGame()
	if err != nil {
		log.Error().Msgf("error getting running game: %s", err)
		return ""
	} else if !running {
		return ""
	} else {
		return game.Path
	}
}

func (p *Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return fmt.Errorf("launching system not supported on batocera")
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	launchers := utils.PathToLaunchers(cfg, p, path)
	if len(launchers) == 0 {
		return errors.New("no launcher found")
	}
	launcher := launchers[0]

	if launcher.AllowListOnly && !cfg.IsLauncherFileAllowed(path) {
		return errors.New("file not allowed: " + path)
	}

	log.Info().Msgf("launching file with %s: %s", launcher.Id, path)
	return launcher.Launch(cfg, path)
}

func (p *Platform) KeyboardInput(input string) error {
	code, err := strconv.Atoi(input)
	if err != nil {
		return err
	}

	p.kbd.Press(code)

	return nil
}

func (p *Platform) KeyboardPress(name string) error {
	code, ok := KeyboardMap[name]
	if !ok {
		return fmt.Errorf("unknown key: %s", name)
	}

	if code < 0 {
		p.kbd.Combo(42, -code)
	} else {
		p.kbd.Press(code)
	}

	return nil
}

func (p *Platform) GamepadPress(name string) error {
	code, ok := GamepadMap[name]
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}

	err := p.gpd.ButtonDown(code)
	if err != nil {
		return err
	}

	time.Sleep(40 * time.Millisecond)

	err = p.gpd.ButtonUp(code)
	if err != nil {
		return err
	}

	return nil
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) error {
	return fmt.Errorf("command not supported on batocera: %s", env.Cmd)
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
			Id:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command(path).Start()
			},
		},
	}

	for k, v := range SystemMap {
		launchers = append(launchers, platforms.Launcher{
			Id:      v,
			Folders: []string{k},
			Launch: func(cfg *config.Instance, path string) error {
				return apiLaunch(path)
			},
			Scanner: func(
				cfg *config.Instance,
				systemID string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				batSysName, err := toBatoceraSystem(systemID)
				if err != nil {
					return nil, err
				}

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
