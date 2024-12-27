//go:build linux || darwin

package mister

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/bendahl/uinput"
	"github.com/rs/zerolog/log"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/input"
	"github.com/wizzomafizzo/mrext/pkg/mister"
)

type Platform struct {
	kbd                 input.Keyboard
	gpd                 uinput.Gamepad
	tr                  *Tracker
	stopTr              func() error
	dbLoadTime          time.Time
	uidMap              map[string]string
	textMap             map[string]string
	stopMappingsWatcher func() error
	cmdMappings         map[string]func(platforms.Platform, platforms.CmdEnv) error
	readers             map[string]*readers.Reader
	lastScan            *tokens.Token
	stopSocket          func()
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

func (p *Platform) Id() string {
	return "mister"
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		libnfc.NewReader(cfg),
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		optical_drive.NewReader(cfg),
	}
}

func (p *Platform) StartPre(cfg *config.Instance) error {
	err := os.MkdirAll(TempDir, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(DataDir, 0755)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Join(p.DataDir(), platforms.AssetsDir), 0755)
	if err != nil {
		return err
	}

	// migrate old config folder db
	oldTaptoDbPath := "/media/fat/Scripts/.config/tapto/tapto.db"
	newTaptoDbPath := filepath.Join(p.DataDir(), config.TapToDbFile)
	if _, err := os.Stat(oldTaptoDbPath); err == nil {
		if _, err := os.Stat(newTaptoDbPath); errors.Is(err, os.ErrNotExist) {
			err := utils.CopyFile(oldTaptoDbPath, newTaptoDbPath)
			if err != nil {
				return err
			}
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

	err = Setup(p.tr)
	if err != nil {
		return err
	}

	stopSocket, err := StartSocketServer(
		p,
		func() *tokens.Token {
			return p.lastScan
		},
		func() map[string]*readers.Reader {
			return p.readers
		},
	)
	if err != nil {
		log.Error().Msgf("error starting socket server: %s", err)
	}
	p.stopSocket = stopSocket

	p.cmdMappings = map[string]func(platforms.Platform, platforms.CmdEnv) error{
		"mister.ini":    CmdIni,
		"mister.core":   CmdLaunchCore,
		"mister.script": cmdMisterScript(*p),
		"mister.mgl":    CmdMisterMgl,

		"ini": CmdIni, // DEPRECATED
	}

	return nil
}

func (p *Platform) StartPost(cfg *config.Instance, ns chan<- models.Notification) error {
	tr, stopTr, err := StartTracker(*UserConfigToMrext(cfg), ns)
	if err != nil {
		return err
	}

	p.tr = tr
	p.stopTr = stopTr

	return nil
}

func (p *Platform) Stop() error {
	if p.stopTr != nil {
		err := p.stopTr()
		if err != nil {
			return err
		}
	}

	if p.gpd != nil {
		err := p.gpd.Close()
		if err != nil {
			return err
		}
	}

	if p.stopMappingsWatcher != nil {
		err := p.stopMappingsWatcher()
		if err != nil {
			return err
		}
	}

	if p.stopSocket != nil {
		p.stopSocket()
	}

	return nil
}

func (p *Platform) AfterScanHook(token tokens.Token) error {
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

	return nil
}

func (p *Platform) ReadersUpdateHook(readers map[string]*readers.Reader) error {
	p.readers = readers
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return games.GetGamesFolders(UserConfigToMrext(cfg))
}

func (p *Platform) ZipsAsDirs() bool {
	return true
}

func (p *Platform) DataDir() string {
	return DataDir
}

func (p *Platform) LogDir() string {
	return TempDir
}

func (p *Platform) ConfigDir() string {
	return DataDir
}

func (p *Platform) TempDir() string {
	return TempDir
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return NormalizePath(cfg, path)
}

func (p *Platform) KillLauncher() error {
	ExitGame()
	return nil
}

func (p *Platform) GetActiveLauncher() string {
	core := GetActiveCoreName()

	if core == mrextConfig.MenuCore {
		return ""
	}

	return core
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
	PlayFail(cfg)
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
	PlaySuccess(cfg)
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

	return mister.LaunchCore(UserConfigToMrext(cfg), *system)
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	launchers := utils.PathToLaunchers(cfg, p, path)

	if len(launchers) == 0 {
		return errors.New("no launcher found")
	}

	// just pick the first one for now
	return launchers[0].Launch(cfg, path)
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
	if f, ok := p.cmdMappings[env.Cmd]; ok {
		return f(p, env)
	} else {
		return fmt.Errorf("command not supported on mister: %s", env.Cmd)
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
	defer f.Close()

	var romsets Romsets
	if err := xml.NewDecoder(f).Decode(&romsets); err != nil {
		return nil, fmt.Errorf("failed to decode XML: %w", err)
	}

	return romsets.Romsets, nil
}

func (p *Platform) Launchers() []platforms.Launcher {
	amiga := platforms.Launcher{
		Id:         gamesdb.SystemAmiga,
		SystemId:   gamesdb.SystemAmiga,
		Folders:    []string{"Amiga"},
		Extensions: []string{".adf"},
		Launch:     launch,
		Scanner: func(
			cfg *config.Instance,
			systemId string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting amigavision scan")
			aGamesPath := "listings/games.txt"
			aDemosPath := "listings/demos.txt"
			var fullPaths []string

			s, err := gamesdb.GetSystem(gamesdb.SystemAmiga)
			if err != nil {
				return results, err
			}

			sfs := gamesdb.GetSystemPaths(p, p.RootDirs(cfg), []gamesdb.System{*s})
			for _, sf := range sfs {
				for _, txt := range []string{aGamesPath, aDemosPath} {
					tp, err := gamesdb.FindPath(filepath.Join(sf.Path, txt))
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
		Id:         gamesdb.SystemNeoGeo,
		SystemId:   gamesdb.SystemNeoGeo,
		Folders:    []string{"NEOGEO"},
		Extensions: []string{".neo"},
		Launch:     launch,
		Scanner: func(
			cfg *config.Instance,
			systemId string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			log.Info().Msg("starting neogeo scan")
			romsetsFilename := "romsets.xml"
			names := make(map[string]string)

			s, err := gamesdb.GetSystem(gamesdb.SystemNeoGeo)
			if err != nil {
				return results, err
			}

			sfs := gamesdb.GetSystemPaths(p, p.RootDirs(cfg), []gamesdb.System{*s})
			for _, sf := range sfs {
				rsf, err := gamesdb.FindPath(filepath.Join(sf.Path, romsetsFilename))
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
		Id:         "MPlayerVideo",
		SystemId:   gamesdb.SystemVideo,
		Folders:    []string{"Video", "Movies", "TV"},
		Extensions: []string{".mp4", ".mkv", ".avi"},
		Launch:     launchMPlayer(*p),
		Kill:       killMPlayer,
	}

	ls := Launchers
	ls = append(ls, amiga)
	ls = append(ls, neogeo)
	ls = append(ls, mplayerVideo)
	return ls
}
