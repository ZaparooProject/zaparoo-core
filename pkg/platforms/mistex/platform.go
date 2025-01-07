//go:build linux || darwin

package mistex

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/bendahl/uinput"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/input"
	mm "github.com/wizzomafizzo/mrext/pkg/mister"
)

type Platform struct {
	kbd    input.Keyboard
	gpd    uinput.Gamepad
	tr     *mister.Tracker
	stopTr func() error
}

func (p *Platform) Id() string {
	return "mistex"
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

	err = os.MkdirAll(mister.DataDir, 0755)
	if err != nil {
		return err
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

	if _, err := os.Stat(mister.SuccessSoundFile); err != nil {
		// copy success sound to temp
		sf, err := os.Create(mister.SuccessSoundFile)
		if err != nil {
			log.Error().Msgf("error creating success sound file: %s", err)
		}
		_, err = sf.Write(assets.SuccessSound)
		if err != nil {
			log.Error().Msgf("error writing success sound file: %s", err)
		}
		_ = sf.Close()
	}

	if _, err := os.Stat(mister.FailSoundFile); err != nil {
		// copy fail sound to temp
		ff, err := os.Create(mister.FailSoundFile)
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

func (p *Platform) StartPost(cfg *config.Instance, ns chan<- models.Notification) error {
	tr, stopTr, err := mister.StartTracker(*mister.UserConfigToMrext(cfg), ns, cfg, p)
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

		arcadeDbUpdated, err := mister.UpdateArcadeDb()
		if err != nil {
			log.Error().Msgf("failed to download arcade database: %s", err)
		}

		if arcadeDbUpdated {
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		} else {
			log.Info().Msg("arcade database is up to date")
		}

		m, err := mister.ReadArcadeDb()
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

	if p.gpd != nil {
		err := p.gpd.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Platform) AfterScanHook(token tokens.Token) error {
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

func (p *Platform) ReadersUpdateHook(readers map[string]*readers.Reader) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return games.GetGamesFolders(mister.UserConfigToMrext(cfg))
}

func (p *Platform) ZipsAsDirs() bool {
	return true
}

func (p *Platform) DataDir() string {
	return mister.DataDir
}

func (p *Platform) LogDir() string {
	return mister.TempDir
}

func (p *Platform) ConfigDir() string {
	return mister.DataDir
}

func (p *Platform) TempDir() string {
	return mister.TempDir
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

func (p *Platform) KillLauncher() error {
	return LaunchMenu()
}

func (p *Platform) GetActiveLauncher() string {
	core := mister.GetActiveCoreName()

	if core == mrextConfig.MenuCore {
		return ""
	}

	return core
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
	mister.PlayFail(cfg)
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
	mister.PlaySuccess(cfg)
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

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	return mm.LaunchGenericFile(mister.UserConfigToMrext(cfg), path)
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
	code, ok := mister.KeyboardMap[name]
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
	code, ok := mister.GamepadMap[name]
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}

	p.gpd.ButtonDown(code)
	time.Sleep(40 * time.Millisecond)
	p.gpd.ButtonUp(code)

	return nil
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) error {
	if f, ok := commandsMappings[env.Cmd]; ok {
		return f(p, env)
	} else {
		return fmt.Errorf("command not supported on mister: %s", env.Cmd)
	}
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers() []platforms.Launcher {
	return mister.Launchers
}
