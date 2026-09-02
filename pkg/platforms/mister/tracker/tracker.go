//go:build linux

package tracker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
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

const (
	ArcadeSystem           = "Arcade"
	mediaLookupTimeout     = 2 * time.Second
	trackerFileSettleDelay = 50 * time.Millisecond
	misterUSBRootCount     = 4
)

// platformWithArcadeCache is an optional interface for platforms that support arcade card launch caching.
type platformWithArcadeCache interface {
	platforms.Platform
	CheckAndClearArcadeCardLaunch(setname string) bool
}

type NameMapping struct {
	CoreName   string
	System     string
	Name       string
	ArcadeName string
}

// arcadeResolution caches one set name's resolution outcome so a core that
// stays active (or is re-entered) doesn't re-run the MediaDB search and MRA
// parses on every LoadCore call.
type arcadeResolution struct {
	path string
	ok   bool
}

type Tracker struct {
	pl               platforms.Platform
	setActiveMedia   func(*models.ActiveMedia)
	cfg              *config.Instance
	serviceCtx       context.Context
	activeMedia      func() *models.ActiveMedia
	readActiveGame   func() (string, error)
	db               *database.Database
	arcadeResolved   map[string]arcadeResolution
	setActiveGame    func(string) error
	coreNameFile     string
	selection        selectionFiles
	ActiveSystemName string
	ActiveSystem     string
	ActiveGameID     string
	ActiveGameName   string
	ActiveGamePath   string
	ActiveCore       string
	NameMap          []NameMapping
	mu               syncutil.Mutex
}

func generateNameMap(pl platforms.Platform, cfg *config.Instance) []NameMapping {
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

	// Installed alternate cores can report a different CORENAME from their
	// system ID (for example RA_SNES for SNES). Include launcher runtime data
	// so tracker validation does not discard authoritative media from them.
	if runtimeProvider, ok := pl.(platforms.LauncherRuntimeProvider); ok {
		launchers := pl.Launchers(cfg)
		for i := range launchers {
			launcher := &launchers[i]
			if launcher.SystemID == "" {
				continue
			}
			runtime := runtimeProvider.LauncherRuntime(cfg, launcher)
			if runtime.MisterCore == nil || runtime.MisterCore.Name == "" {
				continue
			}
			nameMap = append(nameMap, NameMapping{
				CoreName: runtime.MisterCore.Name,
				System:   launcher.SystemID,
				Name:     launcher.SystemID,
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
	ctx context.Context,
	pl platforms.Platform,
	cfg *config.Instance,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
) (*Tracker, error) {
	log.Info().Msg("starting tracker")

	nameMap := generateNameMap(pl, cfg)

	log.Info().Int("count", len(nameMap)).Msg("loaded name mappings")

	return &Tracker{
		pl:               pl,
		cfg:              cfg,
		serviceCtx:       ctx,
		db:               db,
		ActiveCore:       "",
		ActiveSystem:     "",
		ActiveSystemName: "",
		ActiveGameID:     "",
		ActiveGameName:   "",
		ActiveGamePath:   "",
		NameMap:          nameMap,
		activeMedia:      activeMedia,
		setActiveMedia:   setActiveMedia,
		readActiveGame:   activegame.GetActiveGame,
		setActiveGame:    activegame.SetActiveGame,
		coreNameFile:     misterconfig.CoreNameFile,
		selection:        misterSelectionFiles(),
	}, nil
}

// coreNamePath is the CORENAME file this tracker reads. Tests override it;
// zero-value trackers fall back to MiSTer's real path.
func (tr *Tracker) coreNamePath() string {
	if tr.coreNameFile != "" {
		return tr.coreNameFile
	}
	return misterconfig.CoreNameFile
}

func (tr *Tracker) activeGame() (string, error) {
	if tr.readActiveGame != nil {
		return tr.readActiveGame()
	}
	activeGame, err := activegame.GetActiveGame()
	if err != nil {
		return "", fmt.Errorf("read active game: %w", err)
	}
	return activeGame, nil
}

func (tr *Tracker) setActiveGamePath(path string) error {
	if tr.setActiveGame != nil {
		return tr.setActiveGame(path)
	}
	if err := activegame.SetActiveGame(path); err != nil {
		return fmt.Errorf("write active game: %w", err)
	}
	return nil
}

func (tr *Tracker) mediaLookupContext() (context.Context, context.CancelFunc) {
	parent := tr.serviceCtx
	if parent == nil {
		parent = context.Background()
	}
	//nolint:gosec // Caller owns and invokes returned cancel function.
	return context.WithTimeout(parent, mediaLookupTimeout)
}

func (tr *Tracker) ReloadNameMap() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	nameMap := generateNameMap(tr.pl, tr.cfg)
	log.Info().Int("count", len(nameMap)).Msg("reloaded name mappings")
	tr.NameMap = nameMap
	// Set-name to MRA-path resolutions may change along with the name map
	// (an ArcadeDatabase.csv update can add or rename entries).
	tr.arcadeResolved = nil
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

// ResolveExternalMediaPath maps a legacy MediaHistory row's set-name-only
// MediaPath back to its canonical indexed .mra path, for the arcade history
// backfill task (arcade_history_backfill.go).
func (tr *Tracker) ResolveExternalMediaPath(ctx context.Context, systemID, value string) (string, bool) {
	if systemID != ArcadeSystem || value == "" {
		return "", false
	}
	if tr.db == nil || tr.db.MediaDB == nil {
		return "", false
	}

	tr.mu.Lock()
	mapping := tr.LookupCoreName(value)
	tr.mu.Unlock()
	if mapping == nil || mapping.ArcadeName == "" {
		return "", false
	}

	return ResolveArcadeSetName(ctx, tr.db.MediaDB, value, mapping.ArcadeName)
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

	data, err := os.ReadFile(tr.coreNamePath())
	if err != nil {
		// CORENAME is absent until MiSTer launches a core (e.g. right after boot).
		// That's expected; other read errors (permissions, I/O) stay at Error.
		if os.IsNotExist(err) {
			log.Debug().Msgf("core name file not present yet: %s", err)
		} else {
			log.Error().Msgf("error reading core name: %s", err)
		}
		return
	}

	coreName := strings.TrimSpace(string(data))

	// MiSTer truncates CORENAME before rewriting it, so inotify delivers a write
	// event while the file is still empty. An empty read is never a real core
	// change: acting on it retires the active media milliseconds before the new
	// core name arrives, splitting one play session in two.
	if coreName == "" {
		log.Debug().Msg("core name file is empty, ignoring transient write")
		return
	}

	if coreName == misterconfig.MenuCore {
		err := tr.setActiveGamePath("")
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

		mraPath, name := tr.resolveArcadeGame(result.CoreName, result.ArcadeName)
		activeGamePath := result.CoreName
		if mraPath != "" {
			activeGamePath = mraPath
		}
		err := tr.setActiveGamePath(activeGamePath)
		if err != nil {
			log.Warn().Err(err).Msg("error setting active game")
		}

		tr.ActiveGameName = name
		tr.ActiveSystem = ArcadeSystem
		tr.ActiveSystemName = ArcadeSystem
		if mraPath != "" {
			tr.ActiveGameID = fmt.Sprintf("%s/%s", ArcadeSystem, filepath.Base(mraPath))
			tr.ActiveGamePath = mraPath
		} else {
			tr.ActiveGameID = coreName
			tr.ActiveGamePath = "" // no way to find mra path from CORENAME
		}

		// Check if this arcade game was recently launched via card scan. Consume
		// the cache entry either way, but only suppress the notification while
		// that launch's media is still published. If the media has already been
		// retired there is nothing left to duplicate, and this is the only
		// chance to restore it.
		if arcadePl, ok := tr.pl.(platformWithArcadeCache); ok {
			cardLaunch := arcadePl.CheckAndClearArcadeCardLaunch(result.CoreName)
			if cardLaunch && tr.arcadeMediaPublished(activeGamePath) {
				log.Debug().
					Str("setname", result.CoreName).
					Msg("skipping duplicate arcade notification (launched via card)")
				return
			}
		}

		// Don't overwrite a more authoritative observation of the same game:
		// a Zaparoo launch or a resolved FILESELECT event may already have
		// published this canonical .mra path before CORENAME caught up.
		if tr.arcadeMediaPublished(activeGamePath) {
			return
		}

		tr.setActiveMedia(models.NewActiveMedia(
			tr.ActiveSystem,
			tr.ActiveSystemName,
			activeGamePath,
			tr.ActiveGameName,
			"", // LauncherID unknown when tracking MiSTer core changes
		))
		return
	}

	// A bare core launch has no game of its own. Retire media from the old
	// core immediately, then only restore ACTIVEGAME if it belongs to this
	// new core. This prevents a stale MiSTer selection signal from reviving
	// the previous game's media and history after launch.system or stop.
	if active := tr.activeMedia(); active != nil && !coreMatchesSystem(coreName, active.SystemID, tr.NameMap) {
		tr.stopGame()
	}
	// ACTIVEGAME may arrive before CORENAME during a manual launch. Retry a
	// non-empty value now that the core is authoritative, but never let an
	// empty tracker file clear same-system media DoLaunch already published.
	if activeGame, err := tr.activeGame(); err == nil && activeGame != "" {
		tr.loadGameLocked()
	}
}

func coreMatchesSystem(coreName, systemID string, mappings []NameMapping) bool {
	if coreName == "" || strings.EqualFold(coreName, misterconfig.MenuCore) || systemID == "" {
		return false
	}
	for i := range mappings {
		if strings.EqualFold(mappings[i].CoreName, coreName) &&
			strings.EqualFold(mappings[i].System, systemID) {
			return true
		}
	}
	return false
}

// resolveArcadeGame resolves an externally detected arcade core's set name to
// its canonical indexed .mra path and display name. On resolution or lookup
// failure it falls back to the raw set name and the ArcadeDatabase.csv name,
// matching prior behaviour.
func (tr *Tracker) resolveArcadeGame(setName, arcadeName string) (mraPath, name string) {
	name = arcadeName
	path, ok := tr.lookupArcadeSetPath(setName, arcadeName)
	if !ok {
		return "", name
	}

	if tr.db != nil && tr.db.MediaDB != nil {
		systems := []systemdefs.System{{ID: ArcadeSystem}}
		ctx, cancel := tr.mediaLookupContext()
		results, searchErr := tr.db.MediaDB.SearchMediaPathExact(ctx, systems, path)
		cancel()
		if searchErr == nil && len(results) > 0 && results[0].Name != "" {
			name = results[0].Name
		}
	}
	return path, name
}

// lookupArcadeSetPath resolves and caches setName's canonical .mra path for
// the lifetime of the Tracker, or until ReloadNameMap clears the cache.
// Failed resolutions are retried because MediaDB may still be indexing.
func (tr *Tracker) lookupArcadeSetPath(setName, arcadeName string) (string, bool) {
	if cached, found := tr.arcadeResolved[setName]; found {
		return cached.path, cached.ok
	}

	var path string
	var ok bool
	if tr.db != nil && tr.db.MediaDB != nil {
		ctx, cancel := tr.mediaLookupContext()
		path, ok = ResolveArcadeSetName(ctx, tr.db.MediaDB, setName, arcadeName)
		cancel()
	}
	if ok {
		if tr.arcadeResolved == nil {
			tr.arcadeResolved = make(map[string]arcadeResolution)
		}
		tr.arcadeResolved[setName] = arcadeResolution{path: path, ok: true}
	}
	return path, ok
}

// ClearActiveGame synchronously retires tracker state and clears the shared
// ACTIVEGAME signal so a bare core or menu transition cannot inherit it.
func (tr *Tracker) ClearActiveGame() error {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	err := tr.setActiveGamePath("")
	tr.stopGame()
	return err
}

// arcadeMediaPublished reports whether the active media already covers this
// arcade launch, so a redundant publish can be skipped safely.
func (tr *Tracker) arcadeMediaPublished(path string) bool {
	if path == "" {
		return false
	}
	active := tr.activeMedia()
	return active != nil && active.SystemID == ArcadeSystem && active.Path == path
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
	tr.loadGameLocked()
}

func (tr *Tracker) loadGameLocked() {
	activeGame, err := tr.activeGame()
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

	if filepath.Ext(strings.ToLower(filename)) == ".mgl" {
		mgl, mglErr := mgls.ReadMgl(path)
		if mglErr != nil {
			// A missing MGL (e.g. AmigaVision virtual paths, or a cleaned-up temp
			// MGL) just means we can't track that game; not a fault worth Sentry.
			if errors.Is(mglErr, os.ErrNotExist) {
				log.Warn().Err(mglErr).Str("path", path).Msg("active game mgl file not found")
			} else {
				log.Error().Err(mglErr).Str("path", path).Msg("error reading mgl")
			}
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
	var launcher platforms.Launcher
	switch {
	case len(launchers) > 0:
		launcher = launchers[0]
	default:
		var guessed bool
		launcher, guessed = helpers.GuessLauncherForPath(path)
		if !guessed {
			log.Warn().Msgf("no launchers found for %s", path)
			return
		}
	}
	log.Debug().Msgf("tracker detected launcher: %v", launcher)

	if launcher.SystemID == "" {
		log.Warn().Str("path", path).Msg("launcher has empty system ID")
		return
	}
	if !coreMatchesSystem(tr.ActiveCore, launcher.SystemID, tr.NameMap) {
		log.Debug().Str("path", path).Str("core", tr.ActiveCore).
			Str("system", launcher.SystemID).Msg("ignoring active game from a different core")
		return
	}

	system, err := systemdefs.GetSystem(launcher.SystemID)
	if err != nil {
		log.Error().Err(err).Str("systemID", launcher.SystemID).Msg("error getting system")
		return
	}
	log.Debug().Msgf("tracker detected system: %v", system)

	meta, err := assets.GetSystemMetadata(system.ID)
	if err != nil {
		log.Error().Err(err).Str("systemID", system.ID).Msg("error getting system metadata")
		return
	}

	// Try to get clean display name from database first, fallback to filename parsing
	pathInfo := helpers.GetPathInfo(path)
	name := tags.ParseTitleFromFilename(pathInfo.Name, false)
	if tr.db != nil && tr.db.MediaDB != nil {
		systems := []systemdefs.System{{ID: system.ID}}
		ctx, cancel := tr.mediaLookupContext()
		results, searchErr := tr.db.MediaDB.SearchMediaPathExact(ctx, systems, path)
		cancel()
		if searchErr == nil && len(results) > 0 && results[0].Name != "" {
			name = results[0].Name
			log.Debug().Str("path", path).Msg("tracker using indexed display name")
		} else {
			log.Debug().Str("path", path).Msg("tracker media not indexed, using filename")
		}
	}

	id := fmt.Sprintf("%s/%s", system.ID, filename)

	if id != tr.ActiveGameID {
		tr.ActiveGameID = id
		tr.ActiveGameName = name
		tr.ActiveGamePath = path

		tr.ActiveSystem = system.ID
		tr.ActiveSystemName = meta.Name

		tr.setActiveMedia(models.NewActiveMedia(
			system.ID,
			meta.Name,
			path,
			name,
			"", // LauncherID unknown when tracking MiSTer core changes
		))
	}
}

func (tr *Tracker) StopAll() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.stopCore()
	tr.stopGame()
}

// resolveStorageRelativePath mirrors MiSTer's relative-path root selection.
// Main stores 0 for SD and nonzero for USB in config/device.bin; USB uses the
// first available /media/usb0-3 root containing the path. Used both for
// recent-file entries and for the file selector's FULLPATH, which is
// relative to the active storage root while browsing.
func resolveStorageRelativePath(path string, storageSelection []byte, exists func(string) bool) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if len(storageSelection) >= 4 && binary.LittleEndian.Uint32(storageSelection[:4]) != 0 {
		mediaRoot := filepath.Dir(misterconfig.SDRootDir)
		for i := range misterUSBRootCount {
			root := filepath.Join(mediaRoot, fmt.Sprintf("usb%d", i))
			candidate := filepath.Join(root, path)
			if exists(candidate) {
				return candidate
			}
		}
	}
	return filepath.Join(misterconfig.SDRootDir, path)
}

// recentGamePath reads the newest launchable path from a MiSTer recent file.
func recentGamePath(filename string, storageSelection []byte) (string, error) {
	if !strings.Contains(filename, "_recent") {
		return "", nil
	}

	recents, err := mistermain.ReadRecent(filename)
	if err != nil {
		return "", fmt.Errorf("error reading recent file: %w", err)
	}
	if len(recents) == 0 {
		return "", nil
	}

	newest := recents[0]
	if !strings.HasSuffix(filename, "cores_recent.cfg") {
		path := filepath.Join(newest.Directory, newest.Name)
		return resolveStorageRelativePath(path, storageSelection, func(candidate string) bool {
			_, statErr := os.Stat(candidate)
			return statErr == nil
		}), nil
	}

	// Main menu recents contain cores and MGLs; only MGLs identify a game.
	if !strings.HasSuffix(strings.ToLower(newest.Name), ".mgl") {
		return "", nil
	}
	mglPath := ResolvePath(filepath.Join(newest.Directory, newest.Name))
	mgl, err := mgls.ReadMgl(mglPath)
	if err != nil {
		return "", fmt.Errorf("error reading mgl file: %w", err)
	}
	return mgl.File.Path, nil
}

// loadRecent writes a recent file's newest launchable path to ACTIVEGAME.
func (tr *Tracker) loadRecent(filename string) error {
	storageSelection, _ := os.ReadFile(filepath.Join(misterconfig.CoreConfigFolder, "device.bin"))
	path, err := recentGamePath(filename, storageSelection)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err = tr.setActiveGamePath(path); err != nil {
		return fmt.Errorf("error setting active game: %w", err)
	}
	return nil
}

// fileSelection is one settled MiSTer native file-selector event: the
// FILESELECT/FULLPATH/CURRENTPATH trio read together after the selection has
// stopped changing.
type fileSelection struct {
	Status      string
	FullPath    string
	CurrentPath string
}

func trimTrackerFileContent(data []byte) string {
	return strings.TrimSpace(strings.Trim(string(data), "\x00"))
}

// readFileSelectionFrom reads MiSTer's file-selector status trio. FULLPATH
// and CURRENTPATH are only read once FILESELECT says "selected" - while
// browsing they hold in-progress state, and MakeFile truncates before
// writing, so reading them unconditionally risks catching a torn write.
func readFileSelectionFrom(statusFile, fullPathFile, currentPathFile string) (fileSelection, error) {
	statusData, err := os.ReadFile(statusFile) // #nosec G304 -- caller passes trusted MiSTer status file paths
	if err != nil {
		return fileSelection{}, fmt.Errorf("failed to read file selection status: %w", err)
	}
	sel := fileSelection{Status: trimTrackerFileContent(statusData)}
	if sel.Status != "selected" {
		return sel, nil
	}

	fullPathData, err := os.ReadFile(fullPathFile) // #nosec G304 -- caller passes trusted MiSTer status file paths
	if err != nil {
		return fileSelection{}, fmt.Errorf("failed to read selected full path: %w", err)
	}
	// #nosec G304 -- caller passes trusted MiSTer status file paths
	currentPathData, err := os.ReadFile(currentPathFile)
	if err != nil {
		return fileSelection{}, fmt.Errorf("failed to read selected current path: %w", err)
	}
	sel.FullPath = trimTrackerFileContent(fullPathData)
	sel.CurrentPath = trimTrackerFileContent(currentPathData)
	return sel, nil
}

// selectionStaleWindow bounds how far apart MiSTer's FILESELECT status and the
// CURRENTPATH it describes may be written and still belong to the same
// selection. Both MiSTer and writeCurrentPathTo write the trio back to back,
// so a real selection lands well inside this.
const selectionStaleWindow = 2 * time.Second

// selectionIsStale reports whether FILESELECT was written meaningfully later
// than the path it claims to describe.
//
// MiSTer never clears FILESELECT after a launch and rewrites it again when a
// core exits, leaving the status at "selected" while FULLPATH and CURRENTPATH
// still name the game that just ended. Without this the exit re-notification
// reads as a fresh launch and resurrects the game that was just closed, which
// then accrues playtime forever because no further core change is coming.
func selectionIsStale(statusFile, currentPathFile string, window time.Duration) (bool, error) {
	statusInfo, err := os.Stat(statusFile)
	if err != nil {
		return false, fmt.Errorf("failed to stat file selection status: %w", err)
	}
	pathInfo, err := os.Stat(currentPathFile)
	if err != nil {
		return false, fmt.Errorf("failed to stat selected current path: %w", err)
	}
	return statusInfo.ModTime().Sub(pathInfo.ModTime()) > window, nil
}

// stripExt mirrors the extension stripping MiSTer's get_display_name
// (file_io.cpp) applies to CURRENTPATH for .mra/.mgl/.rbf files and cores
// with a single declared extension.
func stripExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}

// matchesSelectionName reports whether entryName is the file MiSTer recorded
// as CURRENTPATH, either verbatim (MGL-driven launches write the full
// basename) or with its extension stripped (the interactive file selector's
// altname).
func matchesSelectionName(entryName, current string) bool {
	return strings.EqualFold(entryName, current) || strings.EqualFold(stripExt(entryName), current)
}

// resolveDirEntry finds the single directory entry matching current. An
// extension-stripped match is only used when it is unambiguous - MiSTer's
// altname can't otherwise be told apart from a same-named entry with a
// different extension.
func resolveDirEntry(entries []os.DirEntry, current string) (string, bool) {
	for _, e := range entries {
		if strings.EqualFold(e.Name(), current) {
			return e.Name(), true
		}
	}
	match, count := "", 0
	for _, e := range entries {
		if strings.EqualFold(stripExt(e.Name()), current) {
			match = e.Name()
			count++
		}
	}
	if count == 1 {
		return match, true
	}
	return "", false
}

// composeSelectedPath resolves one settled file-selection event to the
// concrete path MiSTer launched. sel.FullPath is either an absolute file
// path (MGL launches, Zaparoo's own writeCurrentPath) or, for the
// interactive file selector, the browsed directory relative to the active
// storage root; sel.CurrentPath is the selected entry's display name. A
// selection this can't confidently resolve (ambiguous match, a name rewritten
// by names.txt/NeoGeo translation tables, a stale mid-write read) reports
// ok=false rather than guessing.
func composeSelectedPath(
	sel fileSelection,
	storageSelection []byte,
	stat func(string) (os.FileInfo, error),
	readDir func(string) ([]os.DirEntry, error),
) (path string, ok bool) {
	if sel.Status != "selected" {
		return "", false
	}
	current := sel.CurrentPath
	if current == "" || current == ".." {
		return "", false
	}
	if sel.FullPath == "" {
		return "", false
	}

	base := resolveStorageRelativePath(sel.FullPath, storageSelection, func(candidate string) bool {
		_, statErr := stat(candidate)
		return statErr == nil
	})

	info, err := stat(base)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		if !matchesSelectionName(filepath.Base(base), current) {
			return "", false
		}
		return base, true
	}

	entries, err := readDir(base)
	if err != nil {
		return "", false
	}
	name, found := resolveDirEntry(entries, current)
	if !found {
		return "", false
	}
	selected := filepath.Join(base, name)

	selectedInfo, err := stat(selected)
	if err != nil {
		return "", false
	}
	if !selectedInfo.IsDir() {
		return selected, true
	}

	// A disc-style folder holding exactly one file (MiSTer's
	// MENU_GENERIC_FILE_SELECTED) launches that file directly; a folder with
	// several files (e.g. a NeoGeo folder set) launches as the directory.
	innerEntries, err := readDir(selected)
	if err != nil {
		return selected, true
	}
	onlyFile, fileCount := "", 0
	for _, e := range innerEntries {
		if e.IsDir() {
			continue
		}
		fileCount++
		onlyFile = e.Name()
	}
	if fileCount == 1 {
		return filepath.Join(selected, onlyFile), true
	}
	return selected, true
}

// isSystemOrMenuPath rejects MiSTer's own configuration, script, and core
// selections so they are never recorded as a launched game: Scripts/, the
// config folder, linux/, and the file types MiSTer's menu itself opens
// (cores, filters, presets, scripts, MiSTer.ini variants).
func isSystemOrMenuPath(path string) bool {
	if helpers.PathHasPrefix(path, misterconfig.ScriptsDir) ||
		helpers.PathHasPrefix(path, misterconfig.CoreConfigFolder) ||
		helpers.PathHasPrefix(path, misterconfig.LinuxDir) {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".rbf", ".ini", ".cfg", ".txt", ".sh":
		return true
	default:
		return false
	}
}

// hasSystemLauncher reports whether any launcher in the list identifies a
// known system, i.e. is a real media launcher rather than MiSTer's generic
// core/RBF catch-all (which has no SystemID).
func hasSystemLauncher(launchers []platforms.Launcher) bool {
	for i := range launchers {
		if launchers[i].SystemID != "" {
			return true
		}
	}
	return false
}

// selectionFiles names the MiSTer file-selector trio plus the storage
// selection, so the resolution below can be driven from a temp directory.
type selectionFiles struct {
	status      string
	fullPath    string
	currentPath string
	deviceBin   string
}

func misterSelectionFiles() selectionFiles {
	return selectionFiles{
		status:      misterconfig.FileSelectFile,
		fullPath:    misterconfig.FullPathFile,
		currentPath: misterconfig.CurrentPathFile,
		deviceBin:   filepath.Join(misterconfig.CoreConfigFolder, "device.bin"),
	}
}

// resolveSelectedLaunchPath reads the settled selection and reports the path
// MiSTer launched, or ok=false when there is nothing to act on: an unreadable
// trio, a selection that cannot be resolved confidently, or a status
// re-notified long after the paths it describes.
func resolveSelectedLaunchPath(files selectionFiles, window time.Duration) (string, bool) {
	sel, err := readFileSelectionFrom(files.status, files.fullPath, files.currentPath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read MiSTer file selection")
		return "", false
	}

	if sel.Status == "selected" {
		// Fail open: an unreadable timestamp must not drop a real launch.
		stale, staleErr := selectionIsStale(files.status, files.currentPath, window)
		switch {
		case staleErr != nil:
			log.Warn().Err(staleErr).Msg("failed to age MiSTer file selection, treating it as current")
		case stale:
			log.Debug().Str("path", sel.FullPath).
				Msg("ignoring MiSTer file selection re-notified after its paths were written")
			return "", false
		}
	}

	storageSelection, _ := os.ReadFile(files.deviceBin) // #nosec G304 -- fixed MiSTer config path
	return composeSelectedPath(sel, storageSelection, os.Stat, os.ReadDir)
}

// loadFileSelection resolves the tracker's file-selector trio and records the
// result as the active game. Both the trio and the recorder come from the
// tracker so a test can drive the whole decision from a temp directory.
func (tr *Tracker) loadFileSelection() {
	path, ok := resolveSelectedLaunchPath(tr.selection, selectionStaleWindow)
	if !ok {
		return
	}

	if isSystemOrMenuPath(path) {
		log.Debug().Str("path", path).Msg("ignoring MiSTer system/menu file selection")
		return
	}
	if launchers := helpers.PathToLaunchers(tr.cfg, tr.pl, path); !hasSystemLauncher(launchers) {
		log.Debug().Str("path", path).Msg("ignoring MiSTer file selection with no matching launcher")
		return
	}

	log.Info().Str("path", path).Msg("manual MiSTer file launch detected")
	if err := tr.setActiveGamePath(path); err != nil {
		log.Error().Err(err).Str("path", path).Msg("failed to set selected active game")
	}
}

func trackerFileChanged(op fsnotify.Op) bool {
	return op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0
}

func trackerRecentFileChanged(filename string) bool {
	return filepath.Dir(filename) == filepath.Clean(misterconfig.CoreConfigFolder) &&
		strings.Contains(filepath.Base(filename), "_recent")
}

func dispatchTrackerFileLoad(settled <-chan time.Time, load func()) {
	go func() {
		<-settled
		load()
	}()
}

// trackerFileLoad picks the load function for a watched tracker file, and
// reports whether its write has to settle before the file is read.
//
// Settling matters for every file that is rewritten by truncating first: the
// inotify event arrives while the file is still empty, and an empty read is
// not a real state change. ACTIVEGAME is written that way by SetActiveGame
// itself, and reading the truncated value retires the media the launch just
// published, splitting one play session into a zero-second entry and the
// tracker's later restore of it. CORENAME needs no delay because LoadCore
// filters the empty read itself.
func (tr *Tracker) trackerFileLoad(name string) (load func(), settle bool) {
	switch {
	case name == misterconfig.CoreNameFile:
		return tr.LoadCore, false
	case name == misterconfig.ActiveGameFile:
		return tr.loadGame, true
	case name == misterconfig.FileSelectFile:
		// MakeFile truncates before writing the new status. Wait for
		// FILESELECT, FULLPATH, and CURRENTPATH to settle as one event.
		return tr.loadFileSelection, true
	case trackerRecentFileChanged(name):
		// MiSTer truncates and rewrites binary recent files. Let the write
		// settle before reading the first complete record.
		return func() {
			recentErr := tr.loadRecent(name)
			if recentErr != nil {
				if errors.Is(recentErr, os.ErrNotExist) {
					log.Debug().Err(recentErr).Msg("recent file was replaced before it could be read")
				} else {
					log.Error().Msgf("error loading recent file: %s", recentErr)
				}
			}
		}, true
	}
	return nil, false
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
				if !trackerFileChanged(event.Op) {
					continue
				}
				load, settle := tr.trackerFileLoad(event.Name)
				switch {
				case load == nil:
					continue
				case settle:
					dispatchTrackerFileLoad(time.After(trackerFileSettleDelay), load)
				default:
					load()
				}
			case watchErr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn().Msgf("error in watcher: %s", watchErr)
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

	if _, statSelectErr := os.Stat(misterconfig.FileSelectFile); os.IsNotExist(statSelectErr) {
		//nolint:gosec // MiSTer system file, needs to be readable by other apps
		writeSelectErr := os.WriteFile(misterconfig.FileSelectFile, []byte(""), 0o644)
		if writeSelectErr != nil {
			return nil, fmt.Errorf("failed to write file selection status: %w", writeSelectErr)
		}
		log.Info().Msgf("created file selection status: %s", misterconfig.FileSelectFile)
	}

	// Watch the status file, not FULLPATH. MiSTer rewrites FULLPATH while the
	// user browses and only FILESELECT=selected confirms a launch.
	log.Debug().Msgf("adding watcher for file selection status: %s", misterconfig.FileSelectFile)
	err = watcher.Add(misterconfig.FileSelectFile)
	if err != nil {
		return nil, fmt.Errorf("failed to watch file selection status (%s): %w", misterconfig.FileSelectFile, err)
	}

	elapsed := time.Since(startTime)
	log.Info().Msgf("file watcher setup completed in %v", elapsed)
	return watcher, nil
}

func StartTracker(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
) (*Tracker, func() error, error) {
	tr, err := NewTracker(ctx, pl, cfg, activeMedia, setActiveMedia, db)
	if err != nil {
		log.Error().Msgf("error creating tracker: %s", err)
		return nil, nil, err
	}

	log.Debug().Msg("loading initial core state")
	tr.LoadCore()
	if activegame.ActiveGameEnabled() {
		tr.loadGame()
	} else {
		setErr := tr.setActiveGamePath("")
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
