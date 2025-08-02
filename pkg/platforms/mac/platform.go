//go:build darwin

package mac

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/pn532_uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
}

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
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	return nil
}

func (p *Platform) Stop() error {
	return nil
}

func (p *Platform) ScanHook(token tokens.Token) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return cfg.IndexRoots()
}

func (p *Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    filepath.Join(xdg.DataHome, config.AppName),
		ConfigDir:  filepath.Join(xdg.ConfigHome, config.AppName),
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		ZipsAsDirs: false,
	}
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return path
}

func (p *Platform) StopActiveLauncher() error {
	p.setActiveMedia(nil)
	return nil
}

func (p *Platform) PlayAudio(path string) error {
	return nil
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

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = utils.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

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

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := []platforms.Launcher{
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				// TODO: consider storing this context to enable programmatic game termination/quit functionality
				ctx := context.Background()
				return exec.CommandContext(ctx, path).Start()
			},
		},
	}

	return append(utils.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
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
