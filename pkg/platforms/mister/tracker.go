//go:build linux || darwin

package mister

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"

	"github.com/wizzomafizzo/mrext/pkg/metadata"
	mrextUtils "github.com/wizzomafizzo/mrext/pkg/utils"

	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/mister"
)

const ArcadeSystem = "Arcade"

type NameMapping struct {
	CoreName   string
	System     string
	Name       string // TODO: use names.txt
	ArcadeName string
}

type Tracker struct {
	Config           *mrextConfig.UserConfig
	mu               sync.Mutex
	pl               platforms.Platform
	cfg              *config.Instance
	ActiveCore       string
	ActiveSystem     string
	ActiveSystemName string
	ActiveGameId     string
	ActiveGameName   string
	ActiveGamePath   string
	NameMap          []NameMapping
	activeMedia      func() *models.ActiveMedia
	setActiveMedia   func(*models.ActiveMedia)
}

func generateNameMap() []NameMapping {
	nameMap := make([]NameMapping, 0)

	for _, system := range games.Systems {
		if system.SetName != "" {
			nameMap = append(nameMap, NameMapping{
				CoreName: system.SetName,
				System:   system.Id,
				Name:     system.Name,
			})
		} else if len(system.Folder) > 0 {
			nameMap = append(nameMap, NameMapping{
				CoreName: system.Folder[0],
				System:   system.Id,
				Name:     system.Name,
			})
		} else {
			log.Warn().Msgf("system %s has no setname or folder", system.Id)
		}
	}

	arcadeDbEntries, err := metadata.ReadArcadeDb()
	if err != nil {
		log.Error().Msgf("error reading arcade db: %s", err)
	} else {
		for _, entry := range arcadeDbEntries {
			nameMap = append(nameMap, NameMapping{
				CoreName:   entry.Setname,
				System:     ArcadeSystem,
				Name:       ArcadeSystem,
				ArcadeName: entry.Name,
			})
		}
	}

	return nameMap
}

func NewTracker(
	mrextCfg *mrextConfig.UserConfig,
	pl platforms.Platform,
	cfg *config.Instance,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) (*Tracker, error) {
	log.Info().Msg("starting tracker")

	nameMap := generateNameMap()

	log.Info().Msgf("loaded %d name mappings", len(nameMap))

	return &Tracker{
		Config:           mrextCfg,
		pl:               pl,
		cfg:              cfg,
		ActiveCore:       "",
		ActiveSystem:     "",
		ActiveSystemName: "",
		ActiveGameId:     "",
		ActiveGameName:   "",
		ActiveGamePath:   "",
		NameMap:          nameMap,
		activeMedia:      activeMedia,
		setActiveMedia:   setActiveMedia,
	}, nil
}

func (tr *Tracker) ReloadNameMap() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	nameMap := generateNameMap()
	log.Info().Msgf("reloaded %d name mappings", len(nameMap))
	tr.NameMap = nameMap
}

func (tr *Tracker) LookupCoreName(name string) *NameMapping {
	if name == "" {
		return nil
	}

	log.Debug().Msgf("looking up core name: %s", name)

	for i, mapping := range tr.NameMap {
		if !strings.EqualFold(mapping.CoreName, name) {
			continue
		} else {
			log.Debug().Msgf("found mapping: %s -> %s", name, mapping.Name)
		}

		if mapping.ArcadeName != "" {
			log.Debug().Msgf("arcade name: %s", mapping.ArcadeName)
			return &tr.NameMap[i]
		}

		_, err := systemdefs.LookupSystem(name)
		if err != nil {
			log.Error().Msgf("error getting system: %s", err)
			continue
		}

		log.Info().Msgf("found mapping: %s -> %s", name, mapping.Name)
		return &tr.NameMap[i]
	}

	return nil
}

func (tr *Tracker) stopCore() bool {
	if tr.ActiveCore != "" {
		if tr.ActiveCore == ArcadeSystem {
			tr.ActiveGameId = ""
			tr.ActiveGamePath = ""
			tr.ActiveGameName = ""
			tr.ActiveSystem = ""
			tr.ActiveSystemName = ""
		}

		tr.ActiveCore = ""

		return true
	} else {
		return false
	}
}

// LoadCore loads the current running core and set it as active.
func (tr *Tracker) LoadCore() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	data, err := os.ReadFile(mrextConfig.CoreNameFile)
	coreName := string(data)

	if err != nil {
		log.Error().Msgf("error reading core name: %s", err)
		return
	}

	if coreName == mrextConfig.MenuCore {
		err := mister.SetActiveGame("")
		if err != nil {
			log.Error().Msgf("error setting active game: %s", err)
		}
	}

	if coreName == tr.ActiveCore {
		return
	}

	tr.stopCore()
	tr.ActiveCore = coreName

	if coreName == mrextConfig.MenuCore {
		log.Debug().Msg("in menu, stopping game")
		tr.stopGame()
		return
	}

	// set arcade core details
	if result := tr.LookupCoreName(coreName); result != nil && result.ArcadeName != "" {
		err := mister.SetActiveGame(result.CoreName)
		if err != nil {
			log.Warn().Err(err).Msg("error setting active game")
		}

		tr.ActiveGameId = coreName
		tr.ActiveGameName = result.ArcadeName
		tr.ActiveGamePath = "" // no way to find mra path from CORENAME
		tr.ActiveSystem = ArcadeSystem
		tr.ActiveSystemName = ArcadeSystem

		tr.setActiveMedia(&models.ActiveMedia{
			SystemID:   tr.ActiveSystem,
			SystemName: tr.ActiveSystemName,
			Name:       tr.ActiveGameName,
			Path:       coreName,
			Started:    time.Now(),
		})
	}
}

func (tr *Tracker) stopGame() {
	tr.ActiveGameId = ""
	tr.ActiveGamePath = ""
	tr.ActiveGameName = ""
	tr.ActiveSystem = ""
	tr.ActiveSystemName = ""

	tr.setActiveMedia(nil)
}

// Load the current running game and set it as active.
func (tr *Tracker) loadGame() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	activeGame, err := mister.GetActiveGame()
	if err != nil {
		log.Error().Msgf("error getting active game: %s", err)
		tr.stopGame()
		return
	} else if activeGame == "" {
		log.Debug().Msg("active game is empty, stopping game")
		tr.stopGame()
		return
	} else if !filepath.IsAbs(activeGame) {
		log.Debug().Msgf("active game is not absolute, assuming arcade: %s", activeGame)
		return
	}

	path := mister.ResolvePath(activeGame)
	filename := filepath.Base(path)
	name := mrextUtils.RemoveFileExt(filename)

	if filepath.Ext(strings.ToLower(filename)) == ".mgl" {
		mgl, err := mister.ReadMgl(path)
		if err != nil {
			log.Error().Msgf("error reading mgl: %s", err)
		} else {
			path = mister.ResolvePath(mgl.File.Path)
			log.Info().Msgf("mgl path: %s", path)
		}
	}

	if strings.HasSuffix(strings.ToLower(filename), ".ini") {
		log.Debug().Msgf("ignoring ini file: %s", path)
		return
	}

	launchers := utils.PathToLaunchers(tr.cfg, tr.pl, path)
	if len(launchers) == 0 {
		log.Warn().Msgf("no launchers found for %s", path)
		return
	}

	system, err := systemdefs.GetSystem(launchers[0].SystemID)
	if err != nil {
		log.Error().Msgf("error getting system %s", err)
		return
	}

	meta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		log.Error().Msgf("error getting system metadata %s", err)
		return
	}

	id := fmt.Sprintf("%s/%s", system.ID, filename)

	if id != tr.ActiveGameId {
		tr.ActiveGameId = id
		tr.ActiveGameName = name
		tr.ActiveGamePath = path

		tr.ActiveSystem = system.ID
		tr.ActiveSystemName = meta.Name

		tr.setActiveMedia(&models.ActiveMedia{
			SystemID:   system.ID,
			SystemName: meta.Name,
			Name:       name,
			Path:       path,
			Started:    time.Now(),
		})
	}
}

func (tr *Tracker) StopAll() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.stopCore()
	tr.stopGame()
}

// Read a core's recent file and attempt to write the newest entry's
// launch-able path to ACTIVEGAME.
func loadRecent(filename string) error {
	if !strings.Contains(filename, "_recent") {
		return nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("error opening game file: %w", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Error().Msgf("error closing file: %s", err)
		}
	}(file)

	recents, err := mister.ReadRecent(filename)
	if err != nil {
		return fmt.Errorf("error reading recent file: %w", err)
	} else if len(recents) == 0 {
		return nil
	}

	newest := recents[0]

	if strings.HasSuffix(filename, "cores_recent.cfg") {
		// main menu's recent file, written when launching mgls
		if strings.HasSuffix(strings.ToLower(newest.Name), ".mgl") {
			mglPath := mister.ResolvePath(filepath.Join(newest.Directory, newest.Name))
			mgl, err := mister.ReadMgl(mglPath)
			if err != nil {
				return fmt.Errorf("error reading mgl file: %w", err)
			}

			err = mister.SetActiveGame(mgl.File.Path)
			if err != nil {
				return fmt.Errorf("error setting active game: %w", err)
			}
		}
	} else {
		// individual core's recent file
		err = mister.SetActiveGame(filepath.Join(newest.Directory, newest.Name))
		if err != nil {
			return fmt.Errorf("error setting active game: %w", err)
		}
	}

	return nil
}

func (tr *Tracker) runPickerSelection(name string) {
	contents, err := os.ReadFile(name)
	if err != nil {
		log.Error().Msgf("error reading main picker selected: %s", err)
	} else if len(contents) == 0 {
		log.Error().Msgf("main picker selected is empty")
	} else {
		path := strings.TrimSpace(string(contents))
		path = mrextConfig.SdFolder + "/" + path
		log.Info().Msgf("main picker selected path: %s", path)

		pickerContents, err := os.ReadFile(path)
		if err != nil {
			log.Error().Msgf("error reading main picker selected path: %s", err)
		} else {
			_, err = client.LocalClient(tr.cfg, models.MethodRunScript, string(pickerContents))
			if err != nil {
				log.Error().Err(err).Msg("error running local client")
			}
		}

		files, err := os.ReadDir(MainPickerDir)
		if err != nil {
			log.Error().Msgf("error reading picker items dir: %s", err)
		} else {
			for _, file := range files {
				err := os.Remove(filepath.Join(MainPickerDir, file.Name()))
				if err != nil {
					log.Error().Msgf("error deleting file %s: %s", file.Name(), err)
				}
			}
		}
	}
}

// StartFileWatch Start thread for monitoring changes to all files relating to core/game launches.
func StartFileWatch(tr *Tracker) (*fsnotify.Watcher, error) {
	log.Info().Msg("starting file watcher")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					if event.Name == mrextConfig.CoreNameFile {
						tr.LoadCore()
					} else if event.Name == mrextConfig.ActiveGameFile {
						tr.loadGame()
					} else if strings.HasPrefix(event.Name, mrextConfig.CoreConfigFolder) {
						err = loadRecent(event.Name)
						if err != nil {
							log.Error().Msgf("error loading recent file: %s", err)
						}
					} else if event.Name == MainPickerSelected {
						log.Info().Msgf("main picker selected: %s", event.Name)
						tr.runPickerSelection(event.Name)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error().Msgf("error in watcher: %s", err)
			}
		}
	}()

	if _, err := os.Stat(mrextConfig.CoreNameFile); os.IsNotExist(err) {
		err := os.WriteFile(mrextConfig.CoreNameFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created core name file: %s", mrextConfig.CoreNameFile)
	}

	err = watcher.Add(mrextConfig.CoreNameFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(mrextConfig.CoreConfigFolder); os.IsNotExist(err) {
		err := os.MkdirAll(mrextConfig.CoreConfigFolder, 0755)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created core config folder: %s", mrextConfig.CoreConfigFolder)
	}

	err = watcher.Add(mrextConfig.CoreConfigFolder)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(mrextConfig.ActiveGameFile); os.IsNotExist(err) {
		err := os.WriteFile(mrextConfig.ActiveGameFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created active game file: %s", mrextConfig.ActiveGameFile)
	}

	err = watcher.Add(mrextConfig.ActiveGameFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(mrextConfig.CurrentPathFile); os.IsNotExist(err) {
		err := os.WriteFile(mrextConfig.CurrentPathFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created current path file: %s", mrextConfig.CurrentPathFile)
	}

	err = watcher.Add(mrextConfig.CurrentPathFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(MainPickerSelected); err == nil && MainHasFeature(MainFeaturePicker) {
		err = watcher.Add(MainPickerSelected)
		if err != nil {
			return nil, err
		}
	}

	return watcher, nil
}

func StartTracker(
	mrextCfg mrextConfig.UserConfig,
	cfg *config.Instance,
	pl platforms.Platform,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) (*Tracker, func() error, error) {
	tr, err := NewTracker(&mrextCfg, pl, cfg, activeMedia, setActiveMedia)
	if err != nil {
		log.Error().Msgf("error creating tracker: %s", err)
		return nil, nil, err
	}

	tr.LoadCore()
	if !mister.ActiveGameEnabled() {
		err := mister.SetActiveGame("")
		if err != nil {
			log.Error().Msgf("error setting active game: %s", err)
		}
	}

	watcher, err := StartFileWatch(tr)
	if err != nil {
		log.Error().Msgf("error starting file watch: %s", err)
		return nil, nil, err
	}

	return tr, func() error {
		err := watcher.Close()
		if err != nil {
			log.Error().Msgf("error closing file watcher: %s", err)
		}
		tr.StopAll()
		return nil
	}, nil
}
