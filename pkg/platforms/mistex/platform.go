//go:build linux

package mistex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	tr             *tracker.Tracker
	stopTr         func() error
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	trackedProcess *os.Process
	kbd            linuxinput.Keyboard
	gpd            linuxinput.Gamepad
	processMu      sync.RWMutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDMistex
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		tty2oled.NewReader(cfg, p),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
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
	err := os.MkdirAll(misterconfig.TempDir, 0o750)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	err = os.MkdirAll(helpers.DataDir(p), 0o750)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	kbd, err := linuxinput.NewKeyboard(linuxinput.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to create keyboard: %w", err)
	}
	p.kbd = kbd

	gpd, err := linuxinput.NewGamepad(linuxinput.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to create gamepad: %w", err)
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

	tr, stopTr, err := tracker.StartTracker(
		cfg,
		p,
		activeMedia,
		setActiveMedia,
	)
	if err != nil {
		return fmt.Errorf("failed to start tracker: %w", err)
	}

	p.tr = tr
	p.stopTr = stopTr

	// attempt arcadedb update
	go func() {
		haveInternet := helpers.WaitForInternet(30)
		if !haveInternet {
			log.Warn().Msg("no internet connection, skipping network tasks")
			return
		}

		arcadeDbUpdated, err := arcadedb.UpdateArcadeDb(p)
		if err != nil {
			log.Error().Msgf("failed to download arcade database: %s", err)
		}

		if arcadeDbUpdated {
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		} else {
			log.Info().Msg("arcade database is up to date")
		}

		m, err := arcadedb.ReadArcadeDb(p)
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

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
}

func (*Platform) ScanHook(token *tokens.Token) error {
	f, err := os.Create(misterconfig.TokenReadFile)
	if err != nil {
		return fmt.Errorf("unable to create scan result file %s: %w", misterconfig.TokenReadFile, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	_, err = fmt.Fprintf(f, "%s,%s", token.UID, token.Text)
	if err != nil {
		return fmt.Errorf("unable to write scan result file %s: %w", misterconfig.TokenReadFile, err)
	}

	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	return misterconfig.RootDirs(cfg)
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    misterconfig.DataDir,
		ConfigDir:  misterconfig.DataDir,
		TempDir:    misterconfig.TempDir,
		ZipsAsDirs: true,
	}
}

func LaunchMenu() error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %w", err)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func() {
		if err := cmd.Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close command")
		}
	}()

	// TODO: hardcoded for xilinx variant, should read pref from mister.ini
	if _, err := fmt.Fprintf(cmd, "load_core %s\n", filepath.Join(misterconfig.SDRootDir, "menu.bit")); err != nil {
		log.Warn().Err(err).Msg("failed to write to command")
	}

	return nil
}

func (p *Platform) StopActiveLauncher() error {
	// Kill tracked process if it exists
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

	err := LaunchMenu()
	if err == nil {
		p.setActiveMedia(nil)
	}
	return err
}

func (*Platform) GetActiveLauncher() string {
	core := mistermain.GetActiveCoreName()

	if core == misterconfig.MenuCore {
		return ""
	}

	return core
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
		return fmt.Errorf("failed to start audio playback: %w", err)
	}
	return nil
}

func (p *Platform) ActiveSystem() string {
	return p.tr.ActiveSystem
}

func (p *Platform) ActiveGame() string {
	return p.tr.ActiveGameID
}

func (p *Platform) ActiveGameName() string {
	return p.tr.ActiveGameName
}

func (p *Platform) ActiveGamePath() string {
	return p.tr.ActiveGamePath
}

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	system, err := cores.LookupCore(id)
	if err != nil {
		return fmt.Errorf("failed to lookup system %s: %w", id, err)
	}

	err = mgls.LaunchCore(cfg, p, system)
	if err != nil {
		return fmt.Errorf("failed to launch core: %w", err)
	}
	return nil
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

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err := helpers.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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
		return fmt.Errorf("failed to press keyboard key: %w", err)
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
		return fmt.Errorf("failed to press gamepad button: %w", err)
	}
	return nil
}

func (p *Platform) ForwardCmd(env *platforms.CmdEnv) (platforms.CmdResult, error) {
	if f, ok := commandsMappings[env.Cmd.Name]; ok {
		return f(p, env)
	}
	return platforms.CmdResult{}, fmt.Errorf("command not supported on mister: %s", env.Cmd)
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), mister.Launchers...)
}

func (*Platform) ShowNotice(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, platforms.ErrNotSupported
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, platforms.ErrNotSupported
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}
