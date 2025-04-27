package mac

import (
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/pn532_uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
)

type Platform struct{}

func (p *Platform) ID() string {
	return platforms.PlatformIDMac
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		pn532_uart.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ *config.Instance,
	_ func() *models.ActiveMedia,
	_ func(*models.ActiveMedia),
) error {
	return nil
}

func (p *Platform) Stop() error {
	return nil
}

func (p *Platform) AfterScanHook(token tokens.Token) error {
	return nil
}

func (p *Platform) ReadersUpdateHook(readers map[string]*readers.Reader) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return []string{}
}

func (p *Platform) ZipsAsDirs() bool {
	return false
}

func (p *Platform) DataDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) LogDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) ConfigDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.ConfigHome, config.AppName)
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return path
}

func (p *Platform) KillLauncher() error {
	return nil
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
}

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	log.Info().Msgf("launching system: %s", id)
	return nil
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
	return nil
}

func (p *Platform) KeyboardPress(name string) error {
	return nil
}

func (p *Platform) GamepadPress(name string) error {
	return nil
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		{
			Id:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command(path).Start()
			},
		},
	}
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
