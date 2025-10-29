//go:build linux

package mister

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/linuxinput"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	dbLoadTime          time.Time
	lastUIHidden        time.Time
	textMap             map[string]string
	trackedProcess      *os.Process
	tracker             *tracker.Tracker
	uidMap              map[string]string
	stopMappingsWatcher func() error
	cmdMappings         map[string]func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error)
	lastScan            *tokens.Token
	stopTracker         func() error
	setActiveMedia      func(*models.ActiveMedia)
	activeMedia         func() *models.ActiveMedia
	launcherManager     platforms.LauncherContextManager
	kbd                 linuxinput.Keyboard
	gpd                 linuxinput.Gamepad
	lastLauncher        platforms.Launcher
	stopIntent          platforms.StopIntent
	consoleActive       bool
	processMu           sync.RWMutex
	platformMu          sync.Mutex
	consoleMu           sync.RWMutex
}

func NewPlatform() *Platform {
	return &Platform{
		platformMu: sync.Mutex{},
	}
}

func (p *Platform) setLastLauncher(l *platforms.Launcher) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	p.lastLauncher = *l
}

type oldDb struct {
	Uids  map[string]string
	Texts map[string]string
}

func (p *Platform) getDB() oldDb {
	return oldDb{
		Uids:  p.uidMap,
		Texts: p.textMap,
	}
}

func (p *Platform) GetDBLoadTime() time.Time {
	return p.dbLoadTime
}

func (p *Platform) SetDB(uidMap, textMap map[string]string) {
	p.dbLoadTime = time.Now()
	p.uidMap = uidMap
	p.textMap = textMap
}

func (*Platform) ID() string {
	return platforms.PlatformIDMister
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		tty2oled.NewReader(cfg, p),
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		opticaldrive.NewReader(cfg),
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
	if misterconfig.MainHasFeature(misterconfig.MainFeaturePicker) {
		err := os.MkdirAll(misterconfig.MainPickerDir, 0o750)
		if err != nil {
			return fmt.Errorf("failed to create picker directory: %w", err)
		}
		err = os.WriteFile(misterconfig.MainPickerSelected, []byte(""), 0o600)
		if err != nil {
			return fmt.Errorf("failed to write picker selected file: %w", err)
		}
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

	log.Debug().Msg("input devices initialized successfully")

	uids, texts, err := LoadCsvMappings()
	if err != nil {
		log.Error().Msgf("error loading mappings: %s", err)
	} else {
		p.SetDB(uids, texts)
		log.Info().Int("uid_count", len(uids)).Int("text_count", len(texts)).Msg("CSV mappings loaded")
	}

	closeMappingsWatcher, err := StartCsvMappingsWatcher(
		p.GetDBLoadTime,
		p.SetDB,
	)
	if err != nil {
		log.Error().Msgf("error starting mappings watcher: %s", err)
	}
	p.stopMappingsWatcher = closeMappingsWatcher

	p.cmdMappings = map[string]func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error){
		"mister.ini":    CmdIni,
		"mister.core":   CmdLaunchCore,
		"mister.script": cmdMisterScript(p),
		"mister.mgl":    CmdMisterMgl,

		"ini": CmdIni, // DEPRECATED
	}

	return nil
}

func (p *Platform) StartPost(
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.launcherManager = launcherManager
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

	p.tracker = tr
	p.stopTracker = stopTr

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
	if p.stopTracker != nil {
		err := p.stopTracker()
		if err != nil {
			return err
		}
	}

	err := p.kbd.Close()
	if err != nil {
		log.Warn().Err(err).Msg("error closing keyboard")
	}

	err = p.gpd.Close()
	if err != nil {
		log.Warn().Err(err).Msg("error closing gamepad")
	}

	if p.stopMappingsWatcher != nil {
		err := p.stopMappingsWatcher()
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
}

func (p *Platform) ScanHook(token *tokens.Token) error {
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

	p.lastScan = token

	// stop SAM from playing anything else
	if _, err := os.Stat("/tmp/.SAM_tmp/SAM_Joy_Activity"); err == nil {
		//nolint:gosec // SAM integration temp file
		err = os.WriteFile("/tmp/.SAM_tmp/SAM_Joy_Activity", []byte("zaparoo"), 0o644)
		if err != nil {
			log.Error().Msgf("error writing to SAM_Joy_Activity: %s", err)
		}
	}

	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	// don't change this, only update misterconfig.RootDirs
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

func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	// Store intent before cancelling context so cleanup goroutine can read it
	p.processMu.Lock()
	p.stopIntent = intent
	p.processMu.Unlock()

	// Invalidate old launcher context - signals cleanup goroutines they're stale
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	// Kill tracked process if it exists
	p.processMu.Lock()
	if p.trackedProcess != nil {
		proc := p.trackedProcess
		if err := proc.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill tracked process")
			p.trackedProcess = nil
			p.processMu.Unlock()
		} else {
			log.Debug().Msg("killed tracked process")
			p.trackedProcess = nil
			p.processMu.Unlock()

			// Wait for process to fully exit (with timeout)
			// This prevents race conditions when launching consecutive videos
			done := make(chan error, 1)
			go func() {
				_, err := proc.Wait()
				done <- err
			}()

			select {
			case err := <-done:
				if err != nil {
					log.Debug().Err(err).Msg("process wait completed with error")
				} else {
					log.Debug().Msg("tracked process fully exited")
				}
			case <-time.After(500 * time.Millisecond):
				log.Warn().Msg("timeout waiting for process exit, continuing anyway")
			}
		}
	} else {
		p.processMu.Unlock()
	}

	// Clear active media
	p.setActiveMedia(nil)

	// If stopping for menu, return to menu (clears console state internally)
	if intent == platforms.StopForMenu {
		if err := p.ReturnToMenu(); err != nil {
			log.Warn().Err(err).Msg("failed to return to menu after stopping launcher")
		}
	}

	return nil
}

func (p *Platform) ReturnToMenu() error {
	// Restore console cursor state on both TTYs
	if err := restoreConsole(f9ConsoleVT); err != nil {
		log.Debug().Err(err).Msg("failed to restore tty1 state")
	}
	if launcherConsoleVT != f9ConsoleVT {
		if err := restoreConsole(launcherConsoleVT); err != nil {
			log.Debug().Err(err).Msgf("failed to restore tty%s state", launcherConsoleVT)
		}
	}

	err := mistermain.LaunchMenu()
	if err != nil {
		return fmt.Errorf("failed to launch menu: %w", err)
	}
	// Wait for menu transition to settle
	time.Sleep(300 * time.Millisecond)

	// Clear console active flag - we're back in FPGA mode
	p.consoleMu.Lock()
	p.consoleActive = false
	p.consoleMu.Unlock()

	return nil
}

// isFPGAActive checks if an FPGA core is currently running (not menu).
// This reads the CORENAME file which MiSTer updates whenever cores change.
// Returns true when a game/system core is active, false when in menu or on error.
// This detects ALL active cores, even those launched outside Zaparoo.
func (*Platform) isFPGAActive() bool {
	coreName := mistermain.GetActiveCoreName()
	return coreName != "" && coreName != misterconfig.MenuCore
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
		return fmt.Errorf("failed to start aplay: %w", err)
	}
	return nil
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
	path = checkInZip(path)
	launchers := helpers.PathToLaunchers(cfg, p, path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().
		Str("launcher", launcher.ID).
		Str("path", path).
		Int("available_launchers", len(launchers)).
		Msg("launching media")
	err := helpers.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	p.setLastLauncher(launcher)
	return nil
}

func (p *Platform) KeyboardPress(arg string) error {
	var names []string
	if len(arg) > 1 {
		arg = strings.TrimLeft(arg, "{")
		arg = strings.TrimRight(arg, "}")
		names = strings.Split(arg, "+")
		for i, name := range names {
			if len(name) > 1 {
				names[i] = "{" + name + "}"
			}
		}
	} else {
		names = []string{arg}
	}

	codes := make([]int, 0, len(names))
	for _, name := range names {
		code, ok := linuxinput.ToKeyboardCode(name)
		if !ok {
			return fmt.Errorf("unknown keyboard key: %s", name)
		}
		codes = append(codes, code)
	}

	if len(codes) == 1 {
		err := p.kbd.Press(codes[0])
		if err != nil {
			return fmt.Errorf("failed to press keyboard key: %w", err)
		}
		return nil
	}
	err := p.kbd.Combo(codes...)
	if err != nil {
		return fmt.Errorf("failed to press keyboard combo: %w", err)
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
	if f, ok := p.cmdMappings[env.Cmd.Name]; ok {
		return f(p, env)
	}
	return platforms.CmdResult{}, fmt.Errorf("command not supported on mister: %s", env.Cmd)
}

func (p *Platform) LookupMapping(t *tokens.Token) (string, bool) {
	oldDb := p.getDB()

	// check nfc.csv uids
	if v, ok := oldDb.Uids[t.UID]; ok {
		log.Info().Msg("launching with csv uid match override")
		return v, true
	}

	// check nfc.csv texts
	for pattern, cmd := range oldDb.Texts {
		// check if pattern is a regex
		re, err := helpers.CachedCompile(pattern)
		// not a regex
		if err != nil {
			if pattern, ok := oldDb.Texts[t.Text]; ok {
				log.Info().Msg("launching with csv text match override")
				return pattern, true
			}

			return "", false
		}

		// regex
		if re.MatchString(t.Text) {
			log.Info().Msg("launching with csv regex text match override")
			return cmd, true
		}
	}

	return "", false
}

type Romset struct {
	Name    string `xml:"name,attr"`
	AltName string `xml:"altname,attr"`
}

type Romsets struct {
	XMLName xml.Name `xml:"romsets"`
	Romsets []Romset `xml:"romset"`
}

func readRomsets(filePath string) ([]Romset, error) {
	f, err := os.Open(filePath) //nolint:gosec // Internal romset file path
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close file")
		}
	}(f)

	var romsets Romsets
	if err := xml.NewDecoder(f).Decode(&romsets); err != nil {
		return nil, fmt.Errorf("failed to decode XML: %w", err)
	}

	return romsets.Romsets, nil
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	aGamesPath := "listings/games.txt"
	aDemosPath := "listings/demos.txt"
	amiga := platforms.Launcher{
		ID:         systemdefs.SystemAmiga,
		SystemID:   systemdefs.SystemAmiga,
		Folders:    []string{"Amiga"},
		Extensions: []string{".adf"},
		Test: func(_ *config.Instance, path string) bool {
			if strings.Contains(path, aGamesPath) || strings.Contains(path, aDemosPath) {
				return true
			}
			return false
		},
		Launch: launch(p, systemdefs.SystemAmiga),
		Scanner: func(
			_ context.Context,
			cfg *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting amigavision scan")

			var fullPaths []string

			s, err := systemdefs.GetSystem(systemdefs.SystemAmiga)
			if err != nil {
				return results, fmt.Errorf("failed to get Amiga system: %w", err)
			}

			sfs := mediascanner.GetSystemPaths(cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			for _, sf := range sfs {
				for _, txt := range []string{aGamesPath, aDemosPath} {
					tp, err := mediascanner.FindPath(filepath.Join(sf.Path, txt))
					if err == nil {
						f, err := os.Open(tp) //nolint:gosec // Internal amiga games/demos path
						if err != nil {
							log.Warn().Err(err).Msg("unable to open amiga txt")
							continue
						}

						scanner := bufio.NewScanner(f)
						for scanner.Scan() {
							fp := filepath.Join(sf.Path, txt, scanner.Text())
							fullPaths = append(fullPaths, fp)
						}

						err = f.Close()
						if err != nil {
							log.Warn().Err(err).Msg("unable to close amiga txt")
						}
					}
				}
			}

			for _, p := range fullPaths {
				results = append(results, platforms.ScanResult{
					Path:  p,
					Name:  filepath.Base(p),
					NoExt: true,
				})
			}

			return results, nil
		},
	}

	neogeo := platforms.Launcher{
		ID:         systemdefs.SystemNeoGeo,
		SystemID:   systemdefs.SystemNeoGeo,
		Folders:    []string{"NEOGEO"},
		Extensions: []string{".neo"},
		Test: func(_ *config.Instance, path string) bool {
			if filepath.Ext(path) == ".zip" {
				return true
			}
			if filepath.Ext(path) == "" {
				return true
			}
			return false
		},
		Launch: launch(p, systemdefs.SystemNeoGeo),
		Scanner: func(
			_ context.Context,
			cfg *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting neogeo scan")
			romsetsFilename := "romsets.xml"
			names := make(map[string]string)

			s, err := systemdefs.GetSystem(systemdefs.SystemNeoGeo)
			if err != nil {
				return results, fmt.Errorf("failed to get NeoGeo system: %w", err)
			}

			sfs := mediascanner.GetSystemPaths(cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			for _, sf := range sfs {
				rsf, err := mediascanner.FindPath(filepath.Join(sf.Path, romsetsFilename))
				if err == nil {
					romsets, readErr := readRomsets(rsf)
					if readErr != nil {
						log.Warn().Err(readErr).Msg("unable to read romsets")
						continue
					}

					for _, romset := range romsets {
						names[romset.Name] = romset.AltName
					}
				}

				// read directory
				dir, err := os.Open(sf.Path)
				if err != nil {
					log.Warn().Err(err).Msg("unable to open neogeo directory")
					continue
				}

				files, err := dir.Readdirnames(-1)
				if err != nil {
					log.Warn().Err(err).Msg("unable to read neogeo directory")
					_ = dir.Close()
					continue
				}

				for _, f := range files {
					id := f
					if filepath.Ext(strings.ToLower(f)) == ".zip" {
						id = strings.TrimSuffix(f, filepath.Ext(f))
					}

					if altName, ok := names[id]; ok {
						results = append(results, platforms.ScanResult{
							Path:  filepath.Join(sf.Path, f),
							Name:  altName,
							NoExt: true,
						})
					}
				}
			}

			return results, nil
		},
	}

	ls := createLaunchers(p)
	ls = append(ls, amiga, neogeo, createVideoLauncher(p))

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}

func (p *Platform) ShowNotice(
	cfg *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	if time.Since(p.lastUIHidden) < 2*time.Second && !misterconfig.MainHasFeature(misterconfig.MainFeatureNotice) {
		log.Debug().Msg("waiting for previous notice to finish")
		time.Sleep(3 * time.Second)
	}

	completePath, err := showNotice(cfg, p, args.Text, false)
	if err != nil {
		return nil, 0, err
	}
	return func() error {
		p.platformMu.Lock()
		defer p.platformMu.Unlock()
		p.lastUIHidden = time.Now()
		return hideNotice(completePath)
	}, preNoticeTime(), nil
}

func (p *Platform) ShowLoader(
	cfg *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, error) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	if time.Since(p.lastUIHidden) < 2*time.Second && !misterconfig.MainHasFeature(misterconfig.MainFeatureNotice) {
		log.Debug().Msg("waiting for previous notice to finish")
		time.Sleep(3 * time.Second)
	}

	completePath, err := showNotice(cfg, p, args.Text, true)
	if err != nil {
		return nil, err
	}
	return func() error {
		p.platformMu.Lock()
		defer p.platformMu.Unlock()
		p.lastUIHidden = time.Now()
		return hideNotice(completePath)
	}, nil
}

func (p *Platform) ShowPicker(
	cfg *config.Instance,
	args widgetmodels.PickerArgs,
) error {
	return showPicker(cfg, p, args)
}
