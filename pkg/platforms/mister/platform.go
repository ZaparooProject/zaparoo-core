//go:build linux

package mister

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/mister"
)

type Platform struct {
	kbd                 linuxinput.Keyboard
	gpd                 linuxinput.Gamepad
	tracker             *Tracker
	stopTracker         func() error
	dbLoadTime          time.Time
	uidMap              map[string]string
	textMap             map[string]string
	stopMappingsWatcher func() error
	cmdMappings         map[string]func(platforms.Platform, platforms.CmdEnv) (platforms.CmdResult, error)
	lastScan            *tokens.Token
	platformMu          sync.Mutex
	lastLauncher        platforms.Launcher
	lastUIHidden        time.Time
	activeMedia         func() *models.ActiveMedia
	setActiveMedia      func(*models.ActiveMedia)
}

func NewPlatform() *Platform {
	return &Platform{
		platformMu: sync.Mutex{},
	}
}

func (p *Platform) setLastLauncher(l platforms.Launcher) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	p.lastLauncher = l
}

func (p *Platform) getLastLauncher() platforms.Launcher {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	return p.lastLauncher
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

func (p *Platform) SetDB(uidMap map[string]string, textMap map[string]string) {
	p.dbLoadTime = time.Now()
	p.uidMap = uidMap
	p.textMap = textMap
}

func (p *Platform) ID() string {
	return platforms.PlatformIDMister
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
	if MainHasFeature(MainFeaturePicker) {
		err := os.MkdirAll(MainPickerDir, 0755)
		if err != nil {
			return err
		}
		err = os.WriteFile(MainPickerSelected, []byte(""), 0644)
		if err != nil {
			return err
		}
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

	uids, texts, err := LoadCsvMappings()
	if err != nil {
		log.Error().Msgf("error loading mappings: %s", err)
	} else {
		p.SetDB(uids, texts)
	}

	closeMappingsWatcher, err := StartCsvMappingsWatcher(
		p.GetDBLoadTime,
		p.SetDB,
	)
	if err != nil {
		log.Error().Msgf("error starting mappings watcher: %s", err)
	}
	p.stopMappingsWatcher = closeMappingsWatcher

	p.cmdMappings = map[string]func(platforms.Platform, platforms.CmdEnv) (platforms.CmdResult, error){
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
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	tr, stopTr, err := StartTracker(
		*UserConfigToMrext(cfg),
		cfg,
		p,
		activeMedia,
		setActiveMedia,
	)
	if err != nil {
		return err
	}

	p.tracker = tr
	p.stopTracker = stopTr

	// attempt arcadedb update
	go func() {
		haveInternet := utils.WaitForInternet(30)
		if !haveInternet {
			log.Warn().Msg("no internet connection, skipping network tasks")
			return
		}

		arcadeDbUpdated, err := UpdateArcadeDb(p)
		if err != nil {
			log.Error().Msgf("failed to download arcade database: %s", err)
		}

		if arcadeDbUpdated {
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		} else {
			log.Info().Msg("arcade database is up to date")
		}

		m, err := ReadArcadeDb(p)
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

func (p *Platform) ScanHook(token tokens.Token) error {
	f, err := os.Create(TokenReadFile)
	if err != nil {
		return fmt.Errorf("unable to create scan result file %s: %s", TokenReadFile, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	_, err = f.WriteString(fmt.Sprintf("%s,%s", token.UID, token.Text))
	if err != nil {
		return fmt.Errorf("unable to write scan result file %s: %s", TokenReadFile, err)
	}

	p.lastScan = &token

	// stop SAM from playing anything else
	if _, err := os.Stat("/tmp/.SAM_tmp/SAM_Joy_Activity"); err == nil {
		err = os.WriteFile("/tmp/.SAM_tmp/SAM_Joy_Activity", []byte("zaparoo"), 0644)
		if err != nil {
			log.Error().Msgf("error writing to SAM_Joy_Activity: %s", err)
		}
	}

	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return append(cfg.IndexRoots(), games.GetGamesFolders(UserConfigToMrext(cfg))...)
}

func (p *Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    DataDir,
		ConfigDir:  DataDir,
		TempDir:    TempDir,
		ZipsAsDirs: true,
	}
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return NormalizePath(cfg, path)
}

func (p *Platform) StopActiveLauncher() error {
	ExitGame()
	p.setActiveMedia(nil)
	return nil
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

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	system, err := games.LookupSystem(id)
	if err != nil {
		return err
	}

	return mister.LaunchCore(UserConfigToMrext(cfg), *system)
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	path = checkInZip(path)
	launcher, err := utils.FindLauncher(cfg, p, path)
	if err != nil {
		return fmt.Errorf("launch media: error finding launcher: %w", err)
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = utils.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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

	var codes []int
	for _, name := range names {
		code, ok := linuxinput.ToKeyboardCode(name)
		if !ok {
			return fmt.Errorf("unknown keyboard key: %s", name)
		}
		codes = append(codes, code)
	}

	if len(codes) == 1 {
		return p.kbd.Press(codes[0])
	} else {
		return p.kbd.Combo(codes...)
	}
}

func (p *Platform) GamepadPress(name string) error {
	code, ok := linuxinput.ToGamepadCode(name)
	if !ok {
		return fmt.Errorf("unknown button: %s", name)
	}
	return p.gpd.Press(code)
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) (platforms.CmdResult, error) {
	if f, ok := p.cmdMappings[env.Cmd.Name]; ok {
		return f(p, env)
	} else {
		return platforms.CmdResult{}, fmt.Errorf("command not supported on mister: %s", env.Cmd)
	}
}

func (p *Platform) LookupMapping(t tokens.Token) (string, bool) {
	oldDb := p.getDB()

	// check nfc.csv uids
	if v, ok := oldDb.Uids[t.UID]; ok {
		log.Info().Msg("launching with csv uid match override")
		return v, true
	}

	// check nfc.csv texts
	for pattern, cmd := range oldDb.Texts {
		// check if pattern is a regex
		re, err := regexp.Compile(pattern)

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

func readRomsets(filepath string) ([]Romset, error) {
	f, err := os.Open(filepath)
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
		Test: func(cfg *config.Instance, path string) bool {
			if strings.Contains(path, aGamesPath) || strings.Contains(path, aDemosPath) {
				return true
			} else {
				return false
			}
		},
		Launch: launch(systemdefs.SystemAmiga),
		Scanner: func(
			cfg *config.Instance,
			systemId string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting amigavision scan")

			var fullPaths []string

			s, err := systemdefs.GetSystem(systemdefs.SystemAmiga)
			if err != nil {
				return results, err
			}

			sfs := mediascanner.GetSystemPaths(cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			for _, sf := range sfs {
				for _, txt := range []string{aGamesPath, aDemosPath} {
					tp, err := mediascanner.FindPath(filepath.Join(sf.Path, txt))
					if err == nil {
						f, err := os.Open(tp)
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
					Path: p,
					Name: filepath.Base(p),
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
		Test: func(cfg *config.Instance, path string) bool {
			if filepath.Ext(path) == ".zip" {
				return true
			} else if filepath.Ext(path) == "" {
				return true
			} else {
				return false
			}
		},
		Launch: launch(systemdefs.SystemNeoGeo),
		Scanner: func(
			cfg *config.Instance,
			systemId string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting neogeo scan")
			romsetsFilename := "romsets.xml"
			names := make(map[string]string)

			s, err := systemdefs.GetSystem(systemdefs.SystemNeoGeo)
			if err != nil {
				return results, err
			}

			sfs := mediascanner.GetSystemPaths(cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			for _, sf := range sfs {
				rsf, err := mediascanner.FindPath(filepath.Join(sf.Path, romsetsFilename))
				if err == nil {
					romsets, err := readRomsets(rsf)
					if err != nil {
						log.Warn().Err(err).Msg("unable to read romsets")
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
							Path: filepath.Join(sf.Path, f),
							Name: altName,
						})
					}
				}
			}

			return results, nil
		},
	}

	mplayerVideo := platforms.Launcher{
		ID:         "MPlayerVideo",
		SystemID:   systemdefs.SystemVideo,
		Folders:    []string{"Video", "Movies", "TV"},
		Extensions: []string{".mp4", ".mkv", ".avi"},
		Launch:     launchMPlayer(p),
		Kill:       killMPlayer,
	}

	ls := Launchers
	ls = append(ls, amiga)
	ls = append(ls, neogeo)
	ls = append(ls, mplayerVideo)

	return append(utils.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}

func (p *Platform) ShowNotice(
	cfg *config.Instance,
	args widgetModels.NoticeArgs,
) (func() error, time.Duration, error) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	if time.Since(p.lastUIHidden) < 2*time.Second && !MainHasFeature(MainFeatureNotice) {
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
	args widgetModels.NoticeArgs,
) (func() error, error) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	if time.Since(p.lastUIHidden) < 2*time.Second && !MainHasFeature(MainFeatureNotice) {
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
	args widgetModels.PickerArgs,
) error {
	return showPicker(cfg, p, args)
}
