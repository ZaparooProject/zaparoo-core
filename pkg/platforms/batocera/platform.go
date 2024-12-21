package batocera

import (
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
)

type Platform struct {
}

func (p *Platform) Id() string {
	return "batocera"
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		libnfc.NewReader(cfg),
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
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
	return []string{
		"/userdata/roms",
	}
}

func (p *Platform) ZipsAsDirs() bool {
	return false
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

	relPath := path
	for _, rf := range p.RootDirs(cfg) {
		if strings.HasPrefix(relPath, rf+"/") {
			relPath = strings.TrimPrefix(relPath, rf+"/")
			break
		}
	}
	log.Info().Msgf("relative path: %s", relPath)

	root := strings.Split(relPath, "/")[0]
	log.Info().Msgf("root: %s", root)

	systemId := ""
	for _, launcher := range p.Launchers() {
		for _, folder := range launcher.Folders {
			if folder == root {
				systemId = launcher.SystemId
				break
			}
		}
	}

	if systemId == "" {
		log.Error().Msgf("system not found for path: %s", path)
	}

	for _, launcher := range p.Launchers() {
		if launcher.SystemId == systemId {
			return launcher.Launch(cfg, path)
		}
	}

	return errors.New("launcher not found")
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
	return []platforms.Launcher{
		{
			SystemId:   gamesdb.SystemGenesis,
			Folders:    []string{"megadrive"},
			Extensions: []string{".bin", ".gen", ".md", ".sg", ".smd", ".zip", ".7z"},
			Launch: func(cfg *config.Instance, path string) error {
				cmd := exec.Command("emulatorlauncher", "-system", "megadrive", "-rom", path)
				cmd.Env = os.Environ()
				cmd.Env = append(cmd.Env, "DISPLAY=:0.0")
				return cmd.Start()
			},
		},
	}
}
