//go:build linux

package tracker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker/activegame"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

const ArcadeSystem = "Arcade"

type NameMapping struct {
	CoreName   string
	System     string
	Name       string // TODO: use names.txt
	ArcadeName string
}

type Tracker struct {
	pl               platforms.Platform
	setActiveMedia   func(*models.ActiveMedia)
	cfg              *config.Instance
	activeMedia      func() *models.ActiveMedia
	ActiveSystemName string
	ActiveSystem     string
	ActiveGameID     string
	ActiveGameName   string
	ActiveGamePath   string
	ActiveCore       string
	NameMap          []NameMapping
	mu               sync.Mutex
}

func generateNameMap(pl platforms.Platform) []NameMapping {
	nameMap := make([]NameMapping, 0)

	for key := range cores.Systems {
		system := cores.Systems[key]
		switch {
		case system.SetName != "":
			nameMap = append(nameMap, NameMapping{
				CoreName: system.SetName,
				System:   system.ID,
				Name:     system.ID,
			})
		default:
			nameMap = append(nameMap, NameMapping{
				CoreName: system.ID,
				System:   system.ID,
				Name:     system.ID,
			})
		}
	}

	arcadeDbEntries, err := arcadedb.ReadArcadeDb(pl)
	if err != nil {
		log.Error().Msgf("error reading arcade db: %s", err)
	} else {
		for i := range arcadeDbEntries {
			entry := &arcadeDbEntries[i]
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
	pl platforms.Platform,
	cfg *config.Instance,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) (*Tracker, error) {
	log.Info().Msg("starting tracker")

	nameMap := generateNameMap(pl)

	log.Info().Msgf("loaded %d name mappings", len(nameMap))

	return &Tracker{
		pl:               pl,
		cfg:              cfg,
		ActiveCore:       "",
		ActiveSystem:     "",
		ActiveSystemName: "",
		ActiveGameID:     "",
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

	nameMap := generateNameMap(tr.pl)
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
		}
		log.Debug().Msgf("found mapping: %s -> %s", name, mapping.Name)

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
			tr.ActiveGameID = ""
			tr.ActiveGamePath = ""
			tr.ActiveGameName = ""
			tr.ActiveSystem = ""
			tr.ActiveSystemName = ""
		}

		tr.ActiveCore = ""

		return true
	}
	return false
}

// LoadCore loads the current running core and set it as active.
func (tr *Tracker) LoadCore() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	data, err := os.ReadFile(misterconfig.CoreNameFile)
	coreName := string(data)

	if err != nil {
		log.Error().Msgf("error reading core name: %s", err)
		return
	}

	if coreName == misterconfig.MenuCore {
		err := activegame.SetActiveGame("")
		if err != nil {
			log.Error().Msgf("error setting active game: %s", err)
		}
	}

	if coreName == tr.ActiveCore {
		return
	}

	oldCore := tr.ActiveCore
	tr.stopCore()
	tr.ActiveCore = coreName
	log.Info().Str("old_core", oldCore).Str("new_core", coreName).Msg("core changed")

	if coreName == misterconfig.MenuCore {
		log.Debug().Msg("in menu, stopping game")
		tr.stopGame()
		return
	}

	// set arcade core details
	if result := tr.LookupCoreName(coreName); result != nil && result.ArcadeName != "" {
		log.Info().Str("arcade_game", result.ArcadeName).Str("setname", result.CoreName).Msg("arcade game detected")
		err := activegame.SetActiveGame(result.CoreName)
		if err != nil {
			log.Warn().Err(err).Msg("error setting active game")
		}

		tr.ActiveGameID = coreName
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
	tr.ActiveGameID = ""
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

	activeGame, err := activegame.GetActiveGame()
	switch {
	case err != nil:
		log.Error().Msgf("error getting active game: %s", err)
		tr.stopGame()
		return
	case activeGame == "":
		log.Debug().Msg("active game is empty, stopping game")
		tr.stopGame()
		return
	case !filepath.IsAbs(activeGame):
		log.Debug().Str("active_game", activeGame).Msg("processing arcade game (non-absolute path)")
		return
	}

	log.Debug().Str("active_game", activeGame).Msg("processing active game")

	path := ResolvePath(activeGame)
	filename := filepath.Base(path)
	name := helpers.FilenameFromPath(filename)

	if filepath.Ext(strings.ToLower(filename)) == ".mgl" {
		mgl, mglErr := mgls.ReadMgl(path)
		if mglErr != nil {
			log.Error().Msgf("error reading mgl: %s", mglErr)
		} else {
			path = ResolvePath(mgl.File.Path)
			log.Info().Msgf("mgl path: %s", path)
		}
	}

	if strings.HasSuffix(strings.ToLower(filename), ".ini") {
		log.Debug().Msgf("ignoring ini file: %s", path)
		return
	}

	launchers := helpers.PathToLaunchers(tr.cfg, tr.pl, path)
	if len(launchers) == 0 {
		log.Warn().Msgf("no launchers found for %s", path)
		return
	}
	log.Debug().Msgf("tracker detected launchers: %v", launchers)

	system, err := systemdefs.GetSystem(launchers[0].SystemID)
	if err != nil {
		log.Error().Msgf("error getting system %s", err)
		return
	}
	log.Debug().Msgf("tracker detected system: %v", system)

	meta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		log.Error().Msgf("error getting system metadata %s", err)
		return
	}

	id := fmt.Sprintf("%s/%s", system.ID, filename)

	if id != tr.ActiveGameID {
		tr.ActiveGameID = id
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

	file, err := os.Open(filename) //nolint:gosec // Internal game file path
	if err != nil {
		return fmt.Errorf("error opening game file: %w", err)
	}
	defer func(file *os.File) {
		closeErr := file.Close()
		if closeErr != nil {
			log.Error().Msgf("error closing file: %s", closeErr)
		}
	}(file)

	recents, err := mistermain.ReadRecent(filename)
	if err != nil {
		return fmt.Errorf("error reading recent file: %w", err)
	} else if len(recents) == 0 {
		return nil
	}

	newest := recents[0]

	if strings.HasSuffix(filename, "cores_recent.cfg") {
		// main menu's recent file, written when launching mgls
		if strings.HasSuffix(strings.ToLower(newest.Name), ".mgl") {
			mglPath := ResolvePath(filepath.Join(newest.Directory, newest.Name))
			mgl, mglErr := mgls.ReadMgl(mglPath)
			if mglErr != nil {
				return fmt.Errorf("error reading mgl file: %w", mglErr)
			}

			err = activegame.SetActiveGame(mgl.File.Path)
			if err != nil {
				return fmt.Errorf("error setting active game: %w", err)
			}
		}
	} else {
		// individual core's recent file
		err = activegame.SetActiveGame(filepath.Join(newest.Directory, newest.Name))
		if err != nil {
			return fmt.Errorf("error setting active game: %w", err)
		}
	}

	return nil
}

func (tr *Tracker) runPickerSelection(name string) {
	contents, err := os.ReadFile(name) //nolint:gosec // Internal picker selection file
	switch {
	case err != nil:
		log.Error().Msgf("error reading main picker selected: %s", err)
	case len(contents) == 0:
		log.Error().Msgf("main picker selected is empty")
	default:
		path := strings.TrimSpace(string(contents))
		path = misterconfig.SDRootDir + "/" + path
		log.Info().Msgf("main picker selected path: %s", path)

		pickerContents, err := os.ReadFile(path) //nolint:gosec // Internal picker content path
		if err != nil {
			log.Error().Msgf("error reading main picker selected path: %s", err)
		} else {
			_, err = client.LocalClient(context.Background(), tr.cfg, models.MethodRun, string(pickerContents))
			if err != nil {
				log.Error().Err(err).Msg("error running local client")
			}
		}

		files, err := os.ReadDir(misterconfig.MainPickerDir)
		if err != nil {
			log.Error().Msgf("error reading picker items dir: %s", err)
		} else {
			for _, file := range files {
				err := os.Remove(filepath.Join(misterconfig.MainPickerDir, file.Name()))
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
	startTime := time.Now()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					switch {
					case event.Name == misterconfig.CoreNameFile:
						tr.LoadCore()
					case event.Name == misterconfig.ActiveGameFile:
						tr.loadGame()
					case strings.HasPrefix(event.Name, misterconfig.CoreConfigFolder):
						err = loadRecent(event.Name)
						if err != nil {
							log.Error().Msgf("error loading recent file: %s", err)
						}
					case event.Name == misterconfig.MainPickerSelected:
						log.Info().Msgf("main picker selected: %s", event.Name)
						tr.runPickerSelection(event.Name)
					}
				}
			case watchErr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error().Msgf("error in watcher: %s", watchErr)
			}
		}
	}()

	if _, statErr := os.Stat(misterconfig.CoreNameFile); os.IsNotExist(statErr) {
		//nolint:gosec // MiSTer system file, needs to be readable by other apps
		writeErr := os.WriteFile(misterconfig.CoreNameFile, []byte(""), 0o644)
		if writeErr != nil {
			return nil, fmt.Errorf("failed to write core name file: %w", writeErr)
		}
		log.Info().Msgf("created core name file: %s", misterconfig.CoreNameFile)
	}

	log.Debug().Msgf("adding watcher for core name file: %s", misterconfig.CoreNameFile)
	err = watcher.Add(misterconfig.CoreNameFile)
	if err != nil {
		return nil, fmt.Errorf("failed to watch core name file (%s): %w", misterconfig.CoreNameFile, err)
	}

	if _, statErr := os.Stat(misterconfig.CoreConfigFolder); os.IsNotExist(statErr) {
		//nolint:gosec // MiSTer system directory, needs to be accessible by other apps
		mkdirErr := os.MkdirAll(misterconfig.CoreConfigFolder, 0o755)
		if mkdirErr != nil {
			return nil, fmt.Errorf("failed to create core config folder: %w", mkdirErr)
		}
		log.Info().Msgf("created core config folder: %s", misterconfig.CoreConfigFolder)
	}

	log.Debug().Msgf("adding watcher for core config folder: %s", misterconfig.CoreConfigFolder)
	err = watcher.Add(misterconfig.CoreConfigFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to watch core config folder (%s): %w", misterconfig.CoreConfigFolder, err)
	}

	if _, statActiveErr := os.Stat(misterconfig.ActiveGameFile); os.IsNotExist(statActiveErr) {
		//nolint:gosec // MiSTer system file, needs to be readable by other apps
		writeActiveErr := os.WriteFile(misterconfig.ActiveGameFile, []byte(""), 0o644)
		if writeActiveErr != nil {
			return nil, fmt.Errorf("failed to write active game file: %w", writeActiveErr)
		}
		log.Info().Msgf("created active game file: %s", misterconfig.ActiveGameFile)
	}

	log.Debug().Msgf("adding watcher for active game file: %s", misterconfig.ActiveGameFile)
	err = watcher.Add(misterconfig.ActiveGameFile)
	if err != nil {
		return nil, fmt.Errorf("failed to watch active game file (%s): %w", misterconfig.ActiveGameFile, err)
	}

	if _, statPathErr := os.Stat(misterconfig.CurrentPathFile); os.IsNotExist(statPathErr) {
		//nolint:gosec // MiSTer system file, needs to be readable by other apps
		writePathErr := os.WriteFile(misterconfig.CurrentPathFile, []byte(""), 0o644)
		if writePathErr != nil {
			return nil, fmt.Errorf("failed to write current path file: %w", writePathErr)
		}
		log.Info().Msgf("created current path file: %s", misterconfig.CurrentPathFile)
	}

	log.Debug().Msgf("adding watcher for current path file: %s", misterconfig.CurrentPathFile)
	err = watcher.Add(misterconfig.CurrentPathFile)
	if err != nil {
		return nil, fmt.Errorf("failed to watch current path file (%s): %w", misterconfig.CurrentPathFile, err)
	}

	_, pickerExists := os.Stat(misterconfig.MainPickerSelected)
	if pickerExists == nil && misterconfig.MainHasFeature(misterconfig.MainFeaturePicker) {
		log.Debug().Msgf("adding watcher for picker selected file: %s", misterconfig.MainPickerSelected)
		err = watcher.Add(misterconfig.MainPickerSelected)
		if err != nil {
			return nil, fmt.Errorf("failed to watch picker selected file (%s): %w",
				misterconfig.MainPickerSelected, err)
		}
	}

	elapsed := time.Since(startTime)
	log.Info().Msgf("file watcher setup completed in %v", elapsed)
	return watcher, nil
}

func StartTracker(
	cfg *config.Instance,
	pl platforms.Platform,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) (*Tracker, func() error, error) {
	tr, err := NewTracker(pl, cfg, activeMedia, setActiveMedia)
	if err != nil {
		log.Error().Msgf("error creating tracker: %s", err)
		return nil, nil, err
	}

	log.Debug().Msg("loading initial core state")
	tr.LoadCore()
	if !activegame.ActiveGameEnabled() {
		setErr := activegame.SetActiveGame("")
		if setErr != nil {
			log.Error().Msgf("error setting active game: %s", setErr)
		}
	}

	log.Info().Msg("initializing file watcher for tracker")
	watcher, err := StartFileWatch(tr)
	if err != nil {
		log.Error().Msgf("error starting file watch: %s", err)
		return nil, nil, err
	}
	log.Info().Msg("tracker initialization completed successfully")

	return tr, func() error {
		err := watcher.Close()
		if err != nil {
			log.Error().Msgf("error closing file watcher: %s", err)
		}
		tr.StopAll()
		return nil
	}, nil
}

// Convert a launchable path to an absolute path.
func ResolvePath(path string) string {
	if path == "" {
		return path
	}

	cwd, _ := os.Getwd()
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			log.Error().Err(err).Str("path", cwd).Msg("failed to restore working directory")
		}
	}()
	if err := os.Chdir(misterconfig.SDRootDir); err != nil {
		return path
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}
