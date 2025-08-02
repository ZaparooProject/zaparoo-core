//go:build linux

package mistex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput"
	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	mm "github.com/wizzomafizzo/mrext/pkg/mister"
)

type Platform struct {
	kbd            linuxinput.Keyboard
	gpd            linuxinput.Gamepad
	tr             *mister.Tracker
	stopTr         func() error
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
}

func (p *Platform) ID() string {
	return platforms.PlatformIDMistex
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		libnfc.NewReader(cfg),
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ *config.Instance) error {
	err := os.MkdirAll(mister.TempDir, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(utils.DataDir(p), 0755)
	if err != nil {
		return err
	}

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

	tr, stopTr, err := mister.StartTracker(
		*mister.UserConfigToMrext(cfg),
		cfg,
		p,
		activeMedia,
		setActiveMedia,
	)
	if err != nil {
		return err
	}

	p.tr = tr
	p.stopTr = stopTr

	// attempt arcadedb update
	go func() {
		haveInternet := utils.WaitForInternet(30)
		if !haveInternet {
			log.Warn().Msg("no internet connection, skipping network tasks")
			return
		}

		arcadeDbUpdated, err := mister.UpdateArcadeDb(p)
		if err != nil {
			log.Error().Msgf("failed to download arcade database: %s", err)
		}

		if arcadeDbUpdated {
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		} else {
			log.Info().Msg("arcade database is up to date")
		}

		m, err := mister.ReadArcadeDb(p)
		if err != nil {
			log.Error().Msgf("failed to read arcade database: %s", err)
		} else {
			log.Info().Msgf("arcade database has %d entries", len(m))
		}
	}()

	return nil
}

func (p *Platform) Stop() error {
	if p.stopTr != nil {
		return p.stopTr()
	}

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

func (p *Platform) ScanHook(token tokens.Token) error {
	f, err := os.Create(mister.TokenReadFile)
	if err != nil {
		return fmt.Errorf("unable to create scan result file %s: %s", mister.TokenReadFile, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	_, err = f.WriteString(fmt.Sprintf("%s,%s", token.UID, token.Text))
	if err != nil {
		return fmt.Errorf("unable to write scan result file %s: %s", mister.TokenReadFile, err)
	}

	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return append(cfg.IndexRoots(), games.GetGamesFolders(mister.UserConfigToMrext(cfg))...)
}

func (p *Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    mister.DataDir,
		ConfigDir:  mister.DataDir,
		TempDir:    mister.TempDir,
		ZipsAsDirs: true,
	}
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return mister.NormalizePath(cfg, path)
}

func LaunchMenu() error {
	if _, err := os.Stat(mrextConfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	cmd, err := os.OpenFile(mrextConfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer cmd.Close()

	// TODO: hardcoded for xilinx variant, should read pref from mister.ini
	cmd.WriteString(fmt.Sprintf("load_core %s\n", filepath.Join(mrextConfig.SdFolder, "menu.bit")))

	return nil
}

func (p *Platform) StopActiveLauncher() error {
	err := LaunchMenu()
	if err == nil {
		p.setActiveMedia(nil)
	}
	return err
}

func (p *Platform) GetActiveLauncher() string {
	core := mister.GetActiveCoreName()

	if core == mrextConfig.MenuCore {
		return ""
	}

	return core
}

func (p *Platform) PlayAudio(path string) error {
	if !strings.HasSuffix(strings.ToLower(path), ".wav") {
		return fmt.Errorf("unsupported audio format: %s", path)
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(utils.DataDir(p), path)
	}

	return exec.Command("aplay", path).Start()
}

func (p *Platform) ActiveSystem() string {
	return p.tr.ActiveSystem
}

func (p *Platform) ActiveGame() string {
	return p.tr.ActiveGameId
}

func (p *Platform) ActiveGameName() string {
	return p.tr.ActiveGameName
}

func (p *Platform) ActiveGamePath() string {
	return p.tr.ActiveGamePath
}

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	system, err := games.LookupSystem(id)
	if err != nil {
		return err
	}

	return mm.LaunchCore(mister.UserConfigToMrext(cfg), *system)
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	launcher, err := utils.FindLauncher(cfg, p, path)
	if err != nil {
		return fmt.Errorf("launch media: error finding launcher: %w", err)
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
	code, ok := linuxinput.ToGamepadCode(name)
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}
	return p.gpd.Press(code)
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) (platforms.CmdResult, error) {
	if f, ok := commandsMappings[env.Cmd.Name]; ok {
		return f(p, env)
	} else {
		return platforms.CmdResult{}, fmt.Errorf("command not supported on mister: %s", env.Cmd)
	}
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	return append(utils.ParseCustomLaunchers(p, cfg.CustomLaunchers()), mister.Launchers...)
}

func (p *Platform) ShowNotice(
	_ *config.Instance,
	_ widgetModels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, nil
}

func (p *Platform) ShowLoader(
	_ *config.Instance,
	_ widgetModels.NoticeArgs,
) (func() error, error) {
	return nil, nil
}

func (p *Platform) ShowPicker(
	_ *config.Instance,
	_ widgetModels.PickerArgs,
) error {
	return nil
}
