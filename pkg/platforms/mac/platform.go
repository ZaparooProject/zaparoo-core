package mac

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/pn532_uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
)

type Platform struct {
}

func (p *Platform) Id() string {
	return "mac"
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		pn532_uart.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ chan<- models.Notification) error {
	return nil
}

func (p *Platform) StartPost(_ *config.Instance, _ chan<- models.Notification) error {
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

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	return filepath.Dir(exe)
}

func (p *Platform) DataDir() string {
	return utils.ExeDir()
}

func (p *Platform) LogDir() string {
	return utils.ExeDir()
}

func (p *Platform) ConfigDir() string {
	return utils.ExeDir()
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return path
}

func LaunchMenu() error {
	return nil
}

func (p *Platform) KillLauncher() error {
	return nil
}

func (p *Platform) LaunchingEnabled() bool {
	return true
}

func (p *Platform) SetLaunching(disabled bool) error {
	return nil
}

func (p *Platform) GetActiveLauncher() string {
	return ""
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
}

func (p *Platform) ActiveSystem() string {
	return ""
}

func (p *Platform) ActiveGame() string {
	return ""
}

func (p *Platform) ActiveGameName() string {
	return ""
}

func (p *Platform) ActiveGamePath() string {
	return ""
}

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	log.Info().Msgf("launching system: %s", id)
	return nil
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	log.Info().Msgf("launching file: %s", path)

	if filepath.Ext(path) == ".txt" {
		// get filename minus ext

		fn := filepath.Base(path)
		fn = fn[:len(fn)-4]

		return exec.Command("cmd", "/c", "C:\\Program Files (x86)\\Steam\\steam.exe", "steam://rungameid/"+fn).Start()
	}

	return nil
}

func (p *Platform) Shell(cmd string) error {
	return nil
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

func (p *Platform) ForwardCmd(env platforms.CmdEnv) error {
	return nil
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers() []platforms.Launcher {
	return nil
}
