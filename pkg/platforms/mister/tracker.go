//go:build linux || darwin

package mister

// TODO: i don't think it's actually useful to track if a system is running,
// it should probably be completely removed and just report game playing state

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/notifications"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	config2 "github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	utils2 "github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"

	"github.com/wizzomafizzo/mrext/pkg/metadata"
	"github.com/wizzomafizzo/mrext/pkg/utils"

	"github.com/wizzomafizzo/mrext/pkg/config"
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
	Config           *config.UserConfig
	mu               sync.Mutex
	ns               chan<- models.Notification
	pl               platforms.Platform
	cfg              *config2.Instance
	ActiveCore       string
	ActiveSystem     string
	ActiveSystemName string
	ActiveGameId     string
	ActiveGameName   string
	ActiveGamePath   string
	NameMap          []NameMapping
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

func NewTracker(cfg *config.UserConfig, ns chan<- models.Notification, pl platforms.Platform, cfg2 *config2.Instance) (*Tracker, error) {
	log.Info().Msg("starting tracker")

	nameMap := generateNameMap()

	log.Info().Msgf("loaded %d name mappings", len(nameMap))

	return &Tracker{
		Config:           cfg,
		ns:               ns,
		pl:               pl,
		cfg:              cfg2,
		ActiveCore:       "",
		ActiveSystem:     "",
		ActiveSystemName: "",
		ActiveGameId:     "",
		ActiveGameName:   "",
		ActiveGamePath:   "",
		NameMap:          nameMap,
	}, nil
}

func (tr *Tracker) ReloadNameMap() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	nameMap := generateNameMap()
	log.Info().Msgf("reloaded %d name mappings", len(nameMap))
	tr.NameMap = nameMap
}

func (tr *Tracker) LookupCoreName(name string) (NameMapping, bool) {
	log.Debug().Msgf("looking up core name: %s", name)

	for _, mapping := range tr.NameMap {
		if len(mapping.CoreName) != len(name) {
			continue
		}

		if !strings.EqualFold(mapping.CoreName, name) {
			continue
		} else if mapping.ArcadeName != "" {
			log.Debug().Msgf("arcade name: %s", mapping.ArcadeName)
			return mapping, true
		}

		_, err := systemdefs.LookupSystem(name)
		if err != nil {
			log.Error().Msgf("error getting system %s", err)
			continue
		}

		log.Info().Msgf("found mapping: %s -> %s", name, mapping.Name)
		return mapping, true
	}

	return NameMapping{}, false
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

	data, err := os.ReadFile(config.CoreNameFile)
	coreName := string(data)

	if err != nil {
		log.Error().Msgf("error reading core name: %s", err)
		return
	}

	if coreName == config.MenuCore {
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

	if coreName == config.MenuCore {
		log.Debug().Msg("in menu, stopping game")
		tr.stopGame()
		return
	}

	// set arcade core details
	if result, ok := tr.LookupCoreName(coreName); ok && result.ArcadeName != "" {
		err := mister.SetActiveGame(result.CoreName)
		if err != nil {
			log.Warn().Err(err).Msg("error setting active game")
		}

		tr.ActiveGameId = coreName
		tr.ActiveGameName = result.ArcadeName
		tr.ActiveGamePath = "" // no way to find mra path from CORENAME
		tr.ActiveSystem = ArcadeSystem
		tr.ActiveSystemName = ArcadeSystem

		notifications.MediaStarted(tr.ns, models.MediaStartedParams{
			SystemId:   tr.ActiveSystem,
			SystemName: tr.ActiveSystemName,
			MediaName:  tr.ActiveGameName,
			MediaPath:  coreName,
		})
	}
}

func (tr *Tracker) stopGame() {
	tr.ActiveGameId = ""
	tr.ActiveGamePath = ""
	tr.ActiveGameName = ""
	tr.ActiveSystem = ""
	tr.ActiveSystemName = ""
	notifications.MediaStopped(tr.ns)
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
	name := utils.RemoveFileExt(filename)

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

	launchers := utils2.PathToLaunchers(tr.cfg, tr.pl, path)
	if len(launchers) == 0 {
		log.Warn().Msgf("no launchers found for %s", path)
		return
	}

	system, err := systemdefs.GetSystem(launchers[0].SystemId)
	if err != nil {
		log.Error().Msgf("error getting system %s", err)
		return
	}

	meta, err := assets.GetSystemMetadata(system.Id)
	if err != nil {
		log.Error().Msgf("error getting system metadata %s", err)
		return
	}

	id := fmt.Sprintf("%s/%s", system.Id, filename)

	if id != tr.ActiveGameId {
		tr.ActiveGameId = id
		tr.ActiveGameName = name
		tr.ActiveGamePath = path

		tr.ActiveSystem = system.Id
		tr.ActiveSystemName = meta.Name

		notifications.MediaStarted(tr.ns, models.MediaStartedParams{
			SystemId:   system.Id,
			SystemName: meta.Name,
			MediaName:  name,
			MediaPath:  path,
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
		path = config.SdFolder + "/" + path
		log.Info().Msgf("main picker selected path: %s", path)

		pickerContents, err := os.ReadFile(path)
		if err != nil {
			log.Error().Msgf("error reading main picker selected path: %s", err)
		} else {
			_, err = client.LocalClient(tr.cfg, models.MethodRunCommand, string(pickerContents))
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
					if event.Name == config.CoreNameFile {
						tr.LoadCore()
					} else if event.Name == config.ActiveGameFile {
						tr.loadGame()
					} else if strings.HasPrefix(event.Name, config.CoreConfigFolder) {
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

	if _, err := os.Stat(config.CoreNameFile); os.IsNotExist(err) {
		err := os.WriteFile(config.CoreNameFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created core name file: %s", config.CoreNameFile)
	}

	err = watcher.Add(config.CoreNameFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(config.CoreConfigFolder); os.IsNotExist(err) {
		err := os.MkdirAll(config.CoreConfigFolder, 0755)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created core config folder: %s", config.CoreConfigFolder)
	}

	err = watcher.Add(config.CoreConfigFolder)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(config.ActiveGameFile); os.IsNotExist(err) {
		err := os.WriteFile(config.ActiveGameFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created active game file: %s", config.ActiveGameFile)
	}

	err = watcher.Add(config.ActiveGameFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(config.CurrentPathFile); os.IsNotExist(err) {
		err := os.WriteFile(config.CurrentPathFile, []byte(""), 0644)
		if err != nil {
			return nil, err
		}
		log.Info().Msgf("created current path file: %s", config.CurrentPathFile)
	}

	err = watcher.Add(config.CurrentPathFile)
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

func StartTracker(cfg config.UserConfig, ns chan<- models.Notification, cfg2 *config2.Instance, pl platforms.Platform) (*Tracker, func() error, error) {
	tr, err := NewTracker(&cfg, ns, pl, cfg2)
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
