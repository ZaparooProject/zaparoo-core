//go:build linux

package mister

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamelistxml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/localmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/tracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/installer"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/rs232barcode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	amigaVisionGamesListing   = "listings/games.txt"
	amigaVisionDemosListing   = "listings/demos.txt"
	amigaVisionGamesBrowseDir = "Games"
	amigaVisionDemosBrowseDir = "Demos"
)

type amigaVisionListing struct {
	Path      string
	BrowseDir string
}

type amigaVisionVirtualPath struct {
	InstallPath string
	ListingName string
	GameName    string
}

var (
	amigaVisionListings = []amigaVisionListing{
		{Path: amigaVisionGamesListing, BrowseDir: amigaVisionGamesBrowseDir},
		{Path: amigaVisionDemosListing, BrowseDir: amigaVisionDemosBrowseDir},
	}
	amigaVisionMGLPaths = []string{
		filepath.Join(misterconfig.SDRootDir, "_Computer", "Amiga.mgl"),
		filepath.Join(misterconfig.SDRootDir, "_Computer", "Amiga 500.mgl"),
	}
)

// arcadeCardLaunchCache stores the last arcade game launched via card to prevent duplicate tracker notifications.
type arcadeCardLaunchCache struct {
	timestamp time.Time
	setname   string
	mu        syncutil.RWMutex
}

type Platform struct {
	shared.LinuxInput
	ctx                 context.Context
	fs                  afero.Fs
	dbLoadTime          time.Time
	lastUIHidden        time.Time
	launcherManager     platforms.LauncherContextManager
	trackedProcess      *os.Process
	processDone         chan struct{} // Signals when tracked process cleanup completes
	tracker             *tracker.Tracker
	uidMap              map[string]string
	stopMappingsWatcher func() error
	cmdMappings         map[string]func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error)
	lastScan            *tokens.Token
	stopTracker         func() error
	setActiveMedia      func(*models.ActiveMedia)
	activeMedia         func() *models.ActiveMedia
	textMap             map[string]string
	consoleManager      *MiSTerConsoleManager
	profileData         *profileDataManager
	launchShortCore     func(string) error
	launchBasicFile     func(string) error
	closeConsole        func() error
	lastLauncher        platforms.Launcher
	arcadeCardLaunch    arcadeCardLaunchCache
	stopIntent          platforms.StopIntent
	trackedProcessGroup bool
	processMu           syncutil.RWMutex
	platformMu          syncutil.Mutex
}

func NewPlatform() *Platform {
	p := &Platform{
		platformMu:      syncutil.Mutex{},
		fs:              afero.NewOsFs(),
		launchShortCore: mgls.LaunchShortCore,
		launchBasicFile: mgls.LaunchBasicFile,
	}
	p.consoleManager = newConsoleManager(p)
	p.profileData = newProfileDataManager(p.fs)
	return p
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
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	return oldDb{
		Uids:  p.uidMap,
		Texts: p.textMap,
	}
}

func (p *Platform) GetDBLoadTime() time.Time {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	return p.dbLoadTime
}

func (p *Platform) SetDB(uidMap, textMap map[string]string) {
	p.platformMu.Lock()
	defer p.platformMu.Unlock()
	p.dbLoadTime = time.Now()
	p.uidMap = uidMap
	p.textMap = textMap
}

func (*Platform) ID() string {
	return platformids.Mister
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		tty2oled.NewReader(cfg, p),
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		libnfc.NewLegacyUARTReader(cfg),
		libnfc.NewLegacyI2CReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		rs232barcode.NewReader(cfg),
		opticaldrive.NewReader(cfg),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
	}

	var enabled []readers.Reader
	for _, r := range allReaders {
		metadata := r.Metadata()
		driver := config.DriverInfo{
			ID:                metadata.ID,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
		}
		if cfg.IsReaderEnabled(driver, config.ReaderEnableContextCandidate) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (p *Platform) StartPre(cfg *config.Instance) error {
	startPreStart := time.Now()
	configureTLSRootFallback()

	if err := p.InitDevices(cfg, true); err != nil {
		return fmt.Errorf("failed to initialize input devices: %w", err)
	}

	p.cmdMappings = map[string]func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error){
		"mister.ini":       CmdIni,
		"mister.core":      CmdLaunchCore,
		"mister.script":    cmdMisterScript(p),
		"mister.mgl":       CmdMisterMgl,
		"mister.wallpaper": CmdWallpaper,

		"ini": CmdIni, // DEPRECATED
	}

	// Load CSV mappings synchronously: the API begins accepting scans before
	// deferredStartPre runs, and a token scanned in that window would miss
	// any user-configured nfc.csv override. The load is cheap (no-op fast
	// path when nfc.csv is absent; <10ms parse otherwise).
	uids, texts, err := LoadCsvMappings()
	if err != nil {
		// A malformed nfc.csv is user-supplied data, not a code fault; log at
		// Warn so it stays out of Sentry while remaining visible locally.
		log.Warn().Msgf("error loading mappings: %s", err)
	} else {
		p.SetDB(uids, texts)
		log.Info().Int("uid_count", len(uids)).Int("text_count", len(texts)).Msg("CSV mappings loaded")
	}

	// Start the watcher synchronously so its stopper is committed to the
	// platform before StartPre returns. If we deferred this into a goroutine,
	// a fast Stop() could read p.stopMappingsWatcher == nil and miss the
	// close, leaking the watcher's listener goroutine.
	closeMappingsWatcher, err := StartCsvMappingsWatcher(
		p.GetDBLoadTime,
		p.SetDB,
	)
	if err != nil {
		log.Error().Msgf("error starting mappings watcher: %s", err)
	}
	p.platformMu.Lock()
	p.stopMappingsWatcher = closeMappingsWatcher
	p.platformMu.Unlock()

	log.Info().Int64("duration_ms", time.Since(startPreStart).Milliseconds()).
		Msg("StartPre finished")
	return nil
}

var configureTLSDefaults = tlsroots.ConfigureDefaults

var configureZapLinkHTTPTransport = zapscript.ConfigureHTTPTransport

var configureInstallerHTTPTransport = installer.ConfigureHTTPTransport

func configureTLSRootFallback() {
	fallbackPaths := []string{
		misterconfig.UpdateAllDownloaderCACert,
		misterconfig.SystemCACert,
	}

	usedPath, err := configureTLSDefaults(fallbackPaths)
	if err != nil {
		log.Warn().Err(err).Msg("failed to configure MiSTer TLS CA fallback")
		return
	}

	configureZapLinkHTTPTransport()
	configureInstallerHTTPTransport()

	if usedPath == "" {
		log.Debug().Msg("no MiSTer TLS CA fallback bundle found")
		return
	}

	log.Info().Str("path", usedPath).Msg("configured MiSTer TLS CA fallback bundle")
}

func (p *Platform) StartPost(
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	p.ctx = ctx
	p.launcherManager = launcherManager
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	tr, stopTr, err := tracker.StartTracker(
		ctx,
		cfg,
		p,
		activeMedia,
		setActiveMedia,
		db,
	)
	if err != nil {
		return fmt.Errorf("failed to start tracker: %w", err)
	}

	p.tracker = tr
	p.stopTracker = stopTr

	// Defer the arcade DB update to the idle scheduler so it doesn't
	// compete with the launcher's first request for the network or the
	// single ARM core. Production always supplies a scheduler; the nil
	// branch is reached only in tests where the network task is unwanted,
	// and we skip rather than spawn a goroutine that could outlive Stop().
	arcadeDBTask := func(ctx context.Context) {
		haveInternet := helpers.WaitForInternetContext(ctx, 30)
		if !haveInternet {
			log.Warn().Msg("no internet connection, skipping network tasks")
			return
		}

		arcadeDbUpdated, err := arcadedb.UpdateArcadeDb(p)
		switch {
		case err != nil:
			// Non-fatal: an embedded arcade database is used as a fallback. Download
			// failures are usually network/rate-limit issues, not code faults.
			log.Warn().Msgf("failed to download arcade database: %s", err)
		case arcadeDbUpdated:
			log.Info().Msg("arcade database updated")
			tr.ReloadNameMap()
		default:
			log.Info().Msg("arcade database is up to date")
		}

		m, err := arcadedb.ReadArcadeDb(p)
		if err != nil {
			log.Error().Msgf("failed to read arcade database: %s", err)
		} else {
			log.Info().Msgf("arcade database has %d entries", len(m))
		}
	}
	if scheduler != nil {
		scheduler.Schedule(
			ctx, "arcade-db-update",
			5*time.Second, 300*time.Second,
			arcadeDBTask,
		)
	} else {
		// No scheduler — only happens in tests where the network task is
		// unwanted. Skip rather than spawning a goroutine that could
		// outlive Stop() and race shutdown.
		log.Debug().Msg("no idle scheduler; skipping arcade DB update")
	}

	// If the RBF cache loaded from disk but its shallow manifest drifted,
	// the persisted entries are still serving requests but a rescan is
	// needed to pick up any added/removed cores. Defer the rescan to the
	// idle scheduler so it doesn't compete with the launcher's first
	// requests for the single ARM core or the SQLite file lock.
	switch {
	case scheduler == nil && cores.GlobalRBFCache.NeedsRescan():
		log.Debug().Msg("no idle scheduler; skipping RBF rescan")
	case scheduler != nil && cores.GlobalRBFCache.NeedsRescan():
		scheduler.Schedule(
			ctx, "rbf-rescan",
			5*time.Second, 60*time.Second,
			func(_ context.Context) {
				cores.GlobalRBFCache.Refresh()
			},
		)
	}

	return nil
}

func (p *Platform) Stop() error {
	if p.stopTracker != nil {
		err := p.stopTracker()
		if err != nil {
			return err
		}
	}

	p.CloseDevices()

	p.platformMu.Lock()
	stopWatcher := p.stopMappingsWatcher
	p.platformMu.Unlock()
	if stopWatcher != nil {
		if err := stopWatcher(); err != nil {
			return err
		}
	}

	return nil
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.trackedProcess != proc {
		p.processDone = nil
		p.trackedProcessGroup = false
	}
	p.trackedProcess = proc
}

// setTrackedProcessWithCleanup sets tracked process lifecycle state.
func (p *Platform) setTrackedProcessWithCleanup(proc *os.Process, done chan struct{}, processGroup bool) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
	p.processDone = done
	p.trackedProcessGroup = processGroup
}

// clearTrackedProcess clears lifecycle state when proc is still current.
func (p *Platform) clearTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.trackedProcess != proc {
		return
	}
	p.trackedProcess = nil
	p.processDone = nil
	p.trackedProcessGroup = false
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

func (p *Platform) filesystem() afero.Fs {
	if p.fs != nil {
		return p.fs
	}
	return afero.NewOsFs()
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    misterconfig.DataDir,
		ConfigDir:  misterconfig.DataDir,
		TempDir:    misterconfig.TempDir,
		LogDir:     misterconfig.TempDir,
		ZipsAsDirs: true,
	}
}

func signalTrackedProcess(proc *os.Process, processGroup bool, signal syscall.Signal) error {
	if processGroup {
		if err := syscall.Kill(-proc.Pid, signal); err != nil {
			return fmt.Errorf("signal process group: %w", err)
		}
		return nil
	}
	if err := proc.Signal(signal); err != nil {
		return fmt.Errorf("signal process: %w", err)
	}
	return nil
}

func trackedProcessGroupAlive(proc *os.Process, processGroup bool) bool {
	if !processGroup {
		return false
	}
	return !errors.Is(syscall.Kill(-proc.Pid, 0), syscall.ESRCH)
}

func killRemainingProcessGroup(proc *os.Process, processGroup bool) {
	if !trackedProcessGroupAlive(proc, processGroup) {
		return
	}

	log.Warn().Msg("tracked process group still alive after leader exit, sending SIGKILL")
	if err := signalTrackedProcess(proc, true, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Warn().Err(err).Msg("failed to SIGKILL remaining process group")
		return
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for trackedProcessGroupAlive(proc, true) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForTrackedProcess(proc *os.Process, done chan struct{}) chan struct{} {
	if done != nil {
		return done
	}

	waitDone := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(waitDone)
	}()
	return waitDone
}

func stopTrackedProcess(proc *os.Process, done chan struct{}, processGroup bool, gracefulStop func() error) {
	const (
		gracefulTimeout = 5 * time.Second
		termTimeout     = 2 * time.Second
		killTimeout     = 500 * time.Millisecond
	)

	waitDone := waitForTrackedProcess(proc, done)
	gracefulSent := false
	if gracefulStop != nil {
		log.Debug().Msg("using custom Kill function for launcher")
		if err := gracefulStop(); err != nil {
			log.Warn().Err(err).Msg("custom Kill function failed, falling back to SIGTERM")
		} else {
			gracefulSent = true
		}
	}
	if !gracefulSent {
		log.Debug().Msg("sending SIGTERM to tracked process")
		if err := signalTrackedProcess(proc, processGroup, syscall.SIGTERM); err != nil &&
			!errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			log.Warn().Err(err).Msg("failed to SIGTERM tracked process")
		}
	}

	stopped := false
	select {
	case <-waitDone:
		stopped = true
		log.Debug().Msg("tracked process cleanup completed")
	case <-time.After(gracefulTimeout):
	}

	if !stopped && gracefulSent {
		log.Warn().Msg("custom graceful stop timed out, sending SIGTERM")
		if err := signalTrackedProcess(proc, processGroup, syscall.SIGTERM); err != nil &&
			!errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			log.Warn().Err(err).Msg("failed to SIGTERM tracked process")
		}
		select {
		case <-waitDone:
			stopped = true
			log.Debug().Msg("tracked process cleanup completed after SIGTERM")
		case <-time.After(termTimeout):
		}
	}

	if !stopped {
		log.Warn().Msg("tracked process stop timed out, sending SIGKILL")
		if err := signalTrackedProcess(proc, processGroup, syscall.SIGKILL); err != nil &&
			!errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			log.Warn().Err(err).Msg("failed to SIGKILL tracked process")
		}
		select {
		case <-waitDone:
			log.Debug().Msg("tracked process cleanup completed after SIGKILL")
		case <-time.After(killTimeout):
			log.Warn().Msg("tracked process cleanup timed out after SIGKILL")
		}
	}

	killRemainingProcessGroup(proc, processGroup)
}

func (p *Platform) BackupDefinitions() []platforms.BackupDefinition {
	return BackupDefinitions(p.Settings())
}

func (p *Platform) BackupPlan() platforms.BackupPlan {
	definitions := BackupDefinitions(p.Settings())
	if p.profileData == nil {
		return platforms.BackupPlan{Definitions: definitions}
	}
	return p.profileData.backupPlan(p.Settings(), definitions)
}

func (p *Platform) PrepareBackupRestore() (func(bool) error, error) {
	if p.profileData == nil {
		return func(bool) error { return nil }, nil
	}
	return p.profileData.prepareBackupRestore()
}

func (p *Platform) BackupRestoreRoot() string {
	return BackupRestoreRoot(p.Settings())
}

func BackupRestoreRoot(settings platforms.Settings) string {
	return filepath.Dir(settings.DataDir)
}

func BackupDefinitions(settings platforms.Settings) []platforms.BackupDefinition {
	root := BackupRestoreRoot(settings)
	return []platforms.BackupDefinition{
		{
			Category:     "settings",
			SourceRoot:   root,
			RestoreRoot:  "",
			NonRecursive: true,
			Include: []platforms.BackupPattern{
				{Glob: "MiSTer.ini"},
				{Glob: "MiSTer_alt_*.ini"},
				{Glob: "MiSTer_*.ini"},
				{Glob: "MiSTer.ini.*"},
				{Glob: "downloader.ini"},
			},
			Exclude: []platforms.BackupPattern{{Glob: "MiSTer_example.ini"}},
		},
		{
			Category:    "settings",
			SourceRoot:  filepath.Join(root, "config"),
			RestoreRoot: "config",
			Include: []platforms.BackupPattern{
				{Glob: "*.cfg"},
				{Glob: "*.dat"},
				{Glob: "*.f2"},
			},
			Exclude: []platforms.BackupPattern{{Contains: "_recent"}},
		},
		{
			Category:     "inputs",
			SourceRoot:   root,
			RestoreRoot:  "",
			NonRecursive: true,
			Include: []platforms.BackupPattern{
				{Glob: "gamecontrollerdb_user.txt"},
			},
		},
		{
			Category:     "inputs",
			SourceRoot:   filepath.Join(root, "linux"),
			RestoreRoot:  "linux",
			NonRecursive: true,
			Include: []platforms.BackupPattern{
				{Glob: "gamecontrollerdb_user.txt"},
			},
		},
		{
			Category:    "inputs",
			SourceRoot:  filepath.Join(root, "config", "inputs"),
			RestoreRoot: filepath.Join("config", "inputs"),
			Include: []platforms.BackupPattern{
				{Glob: "*.map"},
				{Glob: "*.zip"},
			},
			Exclude: []platforms.BackupPattern{{Glob: filepath.Join("renamed", "*")}},
		},
		{
			Category:    "saves",
			SourceRoot:  filepath.Join(root, "zaparoo", "profiles"),
			RestoreRoot: filepath.Join("zaparoo", "profiles"),
			Include:     []platforms.BackupPattern{{Contains: "/saves/"}},
		},
		{
			Category:    "savestates",
			SourceRoot:  filepath.Join(root, "zaparoo", "profiles"),
			RestoreRoot: filepath.Join("zaparoo", "profiles"),
			Include:     []platforms.BackupPattern{{Contains: "/savestates/"}},
		},
		{
			Category:    "saves",
			SourceRoot:  filepath.Join(root, "saves"),
			RestoreRoot: "saves",
			Include:     []platforms.BackupPattern{{All: true}},
		},
		{
			Category:    "savestates",
			SourceRoot:  filepath.Join(root, "savestates"),
			RestoreRoot: "savestates",
			Include:     []platforms.BackupPattern{{All: true}},
		},
	}
}

func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	p.processMu.Lock()
	p.stopIntent = intent
	proc := p.trackedProcess
	done := p.processDone
	processGroup := p.trackedProcessGroup
	p.processMu.Unlock()

	if proc == nil && intent == platforms.StopForPreemption && p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	// Capture the current launcher cleanup before clearing it. Script-tracked
	// processes do not set lastLauncher and must not inherit a stale Kill hook.
	p.platformMu.Lock()
	customKill := p.lastLauncher.Kill
	if proc != nil {
		p.lastLauncher = platforms.Launcher{}
	}
	p.platformMu.Unlock()

	if proc != nil {
		var gracefulStop func() error
		if customKill != nil {
			gracefulStop = func() error {
				return customKill(&config.Instance{})
			}
		}
		stopTrackedProcess(proc, done, processGroup, gracefulStop)
		if done == nil {
			p.clearTrackedProcess(proc)
		}
	}

	p.setActiveMedia(nil)

	if proc == nil && (intent == platforms.StopForMenu || intent == platforms.StopForConsoleReset) {
		log.Debug().Msg("no tracked process - calling ReturnToMenu directly")
		if err := p.ReturnToMenu(); err != nil {
			log.Warn().Err(err).Msg("failed to return to menu after stopping launcher")
		}
	}

	if intent == platforms.StopForPreemption && proc != nil && p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	return nil
}

func (p *Platform) ReturnToMenu() error {
	p.processMu.Lock()
	hasTrackedProcess := p.trackedProcess != nil
	p.processMu.Unlock()
	if hasTrackedProcess {
		return p.StopActiveLauncher(platforms.StopForMenu)
	}

	// Restore console cursor state on both TTYs
	if err := p.consoleManager.Restore(f9ConsoleVT); err != nil {
		log.Warn().Err(err).Msg("failed to restore tty1 cursor")
	}
	if armLauncherVT != f9ConsoleVT {
		if err := p.consoleManager.Restore(armLauncherVT); err != nil {
			log.Warn().Err(err).Msgf("failed to restore tty%s cursor", armLauncherVT)
		}
	}

	err := mistermain.LaunchMenu()
	if err != nil {
		log.Error().Err(err).Msg("failed to launch menu")
		return fmt.Errorf("failed to launch menu: %w", err)
	}

	// Wait for menu transition to settle
	time.Sleep(300 * time.Millisecond)

	// Clear console active flag - we're back in FPGA mode
	p.consoleManager.mu.Lock()
	p.consoleManager.active = false
	p.consoleManager.mu.Unlock()

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

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	// Handle menu specially - launch menu core directly
	if strings.EqualFold(id, platforms.SystemMenu) {
		if err := mistermain.LaunchMenu(); err != nil {
			return fmt.Errorf("failed to launch menu: %w", err)
		}
		return nil
	}

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

func (p *Platform) LaunchMedia(
	cfg *config.Instance, path string, launcher *platforms.Launcher, db *database.Database,
	opts *platforms.LaunchOptions,
) error {
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
	err := platforms.DoLaunch(&platforms.LaunchParams{
		Context:        p.ctx,
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: p.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
		Options:        opts,
	}, helpers.GetPathName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	// Allow core to settle before accepting another launch
	time.Sleep(2 * time.Second)

	p.setLastLauncher(launcher)
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

// isInsideGameFolder checks if a path is a file inside an extracted game folder.
// A game folder is any directory under a NEOGEO path that matches romsets.xml.
//
// Example: /media/fat/games/NEOGEO/ct2k3sa/crom0
//   - NEOGEO path: /media/fat/games/NEOGEO
//   - Directory segment: ct2k3sa (matches romsets -> game folder)
//   - File inside: crom0
//   - Returns true
//
// Example: /media/fat/games/NEOGEO/favorites/ct2k3sa/crom0
//   - NEOGEO path: /media/fat/games/NEOGEO
//   - Directory segment: ct2k3sa (matches romsets -> game folder)
//   - File inside: crom0
//   - Returns true
//
// Example: /media/fat/games/NEOGEO/collection/game.neo
//   - NEOGEO path: /media/fat/games/NEOGEO
//   - Directory segment: collection (not in romsets -> not a game folder)
//   - Returns false
func isInsideGameFolder(
	lowerPath string,
	romsetNames map[string]string,
	normalizedNeogeoPaths []string,
) bool {
	for _, neoPath := range normalizedNeogeoPaths {
		// Ensure neoPath ends with separator for correct prefix matching
		neoPathWithSep := neoPath
		if !strings.HasSuffix(neoPathWithSep, string(filepath.Separator)) {
			neoPathWithSep += string(filepath.Separator)
		}

		if !strings.HasPrefix(lowerPath, neoPathWithSep) {
			continue
		}

		// Get the relative path after NEOGEO folder
		relPath := lowerPath[len(neoPathWithSep):]
		if relPath == "" {
			continue // Path is the NEOGEO folder itself
		}

		// Split into components
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) < 2 {
			continue // Not deep enough to be inside a folder
		}

		// Any directory segment before the file can be the game folder.
		for _, dir := range parts[:len(parts)-1] {
			if _, isGame := romsetNames[dir]; isGame {
				return true
			}
		}
	}
	return false
}

func collectNeoGeoRomsetEntries(
	ctx context.Context,
	fs afero.Fs,
	root string,
	romsetNames map[string]string,
	seen map[string]struct{},
) ([]platforms.ScanResult, error) {
	results := make([]platforms.ScanResult, 0)
	cleanRoot := filepath.Clean(root)

	err := afero.Walk(fs, cleanRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			log.Warn().Err(walkErr).Str("path", path).Msg("unable to read neogeo entry")
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if path == cleanRoot {
			return nil
		}

		base := info.Name()
		isDirectory := info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 {
			targetInfo, statErr := fs.Stat(path)
			isDirectory = statErr == nil && targetInfo.IsDir()
			log.Debug().Str("path", path).Bool("directory", isDirectory).
				Msg("neogeo symlink candidate found")
		}
		if isDirectory {
			if base == "__MACOSX" || strings.HasPrefix(base, ".") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			markerPath := filepath.Join(path, ".zaparooignore")
			if _, statErr := fs.Stat(markerPath); statErr == nil {
				log.Info().Str("path", path).Msg("skipping directory with .zaparooignore marker")
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		lowerBase := strings.ToLower(base)
		candidateID := lowerBase
		isZip := filepath.Ext(lowerBase) == ".zip"
		if isZip {
			candidateID = strings.TrimSuffix(lowerBase, filepath.Ext(lowerBase))
		} else if !isDirectory {
			return nil
		}

		altName, ok := romsetNames[candidateID]
		if !ok {
			return nil
		}

		cleanPath := filepath.Clean(path)
		if _, ok := seen[cleanPath]; ok {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		seen[cleanPath] = struct{}{}

		results = append(results, platforms.ScanResult{
			Path:  cleanPath,
			Name:  altName,
			NoExt: true,
		})

		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return results, fmt.Errorf("failed to walk neogeo romsets: %w", err)
	}

	return results, nil
}

// filterNeoGeoGameContents filters out scan results that are inside games
// (matching entries in the romsets map), whether those games are:
// - Zip files: paths containing ".zip/" where zip name matches romsets
// - Extracted folders: paths where immediate child of NEOGEO folder matches romsets
//
// Examples of filtered paths:
//   - /NEOGEO/mslug.zip/internal -> filtered (zip game)
//   - /NEOGEO/mslug/crom0 -> filtered (folder game)
//
// Examples of kept paths:
//   - /NEOGEO/mslug.zip -> kept (zip file itself)
//   - /NEOGEO/mslug/ -> kept (folder itself, if indexed)
//   - /NEOGEO/collection.zip/game.neo -> kept (zip is not a game)
//   - /NEOGEO/collection/game.neo -> kept (folder is not a game)
func filterNeoGeoGameContents(
	results []platforms.ScanResult,
	romsetNames map[string]string,
	neogeoPaths []string,
) []platforms.ScanResult {
	filtered := make([]platforms.ScanResult, 0, len(results))

	// Normalize NEOGEO paths for case-insensitive comparison
	normalizedNeogeoPaths := make([]string, len(neogeoPaths))
	for i, p := range neogeoPaths {
		normalizedNeogeoPaths[i] = strings.ToLower(filepath.Clean(p))
	}

	for _, r := range results {
		lowerPath := strings.ToLower(r.Path)

		// Filter paths inside game zips
		if idx := strings.Index(lowerPath, ".zip/"); idx != -1 {
			zipPath := r.Path[:idx+4]
			zipName := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
			if _, isGame := romsetNames[strings.ToLower(zipName)]; isGame {
				continue
			}
		}

		// Filter paths inside game folders
		if isInsideGameFolder(lowerPath, romsetNames, normalizedNeogeoPaths) {
			continue
		}

		filtered = append(filtered, r)
	}
	return filtered
}

// filterNeoGeoZipToNeoOnly filters results when no romsets.xml is available.
// Only keeps:
// - Files NOT inside zips (top-level .neo files, folders)
// - Files inside zips that end with .neo extension
// - Zip files themselves if they contain no .neo files (likely game ROM sets)
//
// This provides graceful degradation when romsets.xml is missing or invalid.
func filterNeoGeoZipToNeoOnly(results []platforms.ScanResult) []platforms.ScanResult {
	// First pass: identify which zips contain .neo files
	zipsWithNeo := make(map[string]bool)
	allZips := make(map[string]bool)

	for _, r := range results {
		lowerPath := strings.ToLower(r.Path)
		if idx := strings.Index(lowerPath, ".zip/"); idx != -1 {
			zipPath := r.Path[:idx+4]
			allZips[zipPath] = true
			if strings.HasSuffix(lowerPath, ".neo") {
				zipsWithNeo[zipPath] = true
			}
		}
	}

	// Second pass: filter and collect results
	filtered := make([]platforms.ScanResult, 0, len(results))
	for _, r := range results {
		lowerPath := strings.ToLower(r.Path)

		if idx := strings.Index(lowerPath, ".zip/"); idx != -1 {
			zipPath := r.Path[:idx+4]
			if zipsWithNeo[zipPath] && strings.HasSuffix(lowerPath, ".neo") {
				filtered = append(filtered, r)
			}
			continue
		}

		filtered = append(filtered, r)
	}

	// Add zips that don't contain .neo files as launchable games
	for zipPath := range allZips {
		if !zipsWithNeo[zipPath] {
			filtered = append(filtered, platforms.ScanResult{Path: zipPath})
		}
	}

	return filtered
}

func splitAmigaVisionInstallPaths(paths []mediascanner.PathResult) (
	preferred []mediascanner.PathResult,
	other []mediascanner.PathResult,
) {
	preferred = make([]mediascanner.PathResult, 0, len(paths))
	other = make([]mediascanner.PathResult, 0, len(paths))

	for _, path := range paths {
		if !hasAmigaVisionImage(path.Path) {
			log.Debug().Str("path", path.Path).Msg("skipping AmigaVision path without boot image")
			continue
		}
		if isPreferredAmigaVisionPath(path.Path) {
			preferred = append(preferred, path)
			continue
		}
		other = append(other, path)
	}

	return preferred, other
}

func hasAmigaVisionImage(path string) bool {
	for _, image := range []string{"AmigaVision.hdf", "MegaAGS.hdf"} {
		if _, err := os.Stat(filepath.Join(path, image)); err == nil {
			return true
		}
	}
	return false
}

func isPreferredAmigaVisionPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.Clean(path)), filepath.Join("games", "amiga"))
}

func isAmigaVisionListingFile(path string) bool {
	cleanPath := filepath.ToSlash(strings.ToLower(filepath.Clean(path)))
	for _, listing := range amigaVisionListings {
		if strings.HasSuffix(cleanPath, "/"+filepath.ToSlash(listing.Path)) {
			return true
		}
	}
	return false
}

func filterAmigaVisionListingFiles(results []platforms.ScanResult) []platforms.ScanResult {
	filtered := make([]platforms.ScanResult, 0, len(results))
	for _, result := range results {
		if isAmigaVisionListingFile(result.Path) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

func amigaVisionVirtualPathParts(path string) (amigaVisionVirtualPath, bool) {
	dir := filepath.Dir(path)
	switch strings.ToLower(filepath.Base(dir)) {
	case strings.ToLower(amigaVisionGamesBrowseDir):
		return amigaVisionVirtualPath{
			InstallPath: filepath.Clean(filepath.Join(dir, "..")),
			ListingName: "games.txt",
			GameName:    filepath.Base(path),
		}, true
	case strings.ToLower(amigaVisionDemosBrowseDir):
		return amigaVisionVirtualPath{
			InstallPath: filepath.Clean(filepath.Join(dir, "..")),
			ListingName: "demos.txt",
			GameName:    filepath.Base(path),
		}, true
	default:
		return amigaVisionVirtualPath{}, false
	}
}

func amigaVisionListingContainsGame(installPath, listingName, gameName string) bool {
	f, err := os.Open(filepath.Join(installPath, "listings", listingName)) //nolint:gosec // Internal amiga listing path
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("unable to close amiga txt")
		}
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() == gameName {
			return true
		}
	}
	return false
}

func isAmigaVisionVirtualPath(path string) bool {
	virtualPath, ok := amigaVisionVirtualPathParts(path)
	if !ok {
		return false
	}
	return hasAmigaVisionImage(virtualPath.InstallPath) ||
		amigaVisionListingContainsGame(virtualPath.InstallPath, virtualPath.ListingName, virtualPath.GameName)
}

func scanAmigaVisionListingFile(path, installPath string, listing amigaVisionListing) []platforms.ScanResult {
	f, err := os.Open(path) //nolint:gosec // Internal amiga games/demos path
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("unable to open amiga txt")
		return nil
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("path", path).Msg("unable to close amiga txt")
		}
	}()

	var results []platforms.ScanResult
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		results = append(results, platforms.ScanResult{
			Path:  filepath.Join(installPath, listing.BrowseDir, name),
			NoExt: true,
		})
	}
	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("unable to scan amiga txt")
	}
	return results
}

func amigaVisionMGLScanResults(installPath string, mglPaths []string) []platforms.ScanResult {
	results := make([]platforms.ScanResult, 0, len(mglPaths))
	for _, mglPath := range mglPaths {
		if _, err := os.Stat(mglPath); err != nil {
			continue
		}
		name := filepath.Base(mglPath)
		results = append(results, platforms.ScanResult{
			Path: filepath.Join(installPath, name),
			Name: strings.TrimSuffix(name, filepath.Ext(name)),
		})
	}
	return results
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	// Launchers is invoked from many hot paths (token scans, RPC handlers,
	// indexing). The Refresh fast path stats only the snapshot directories
	// and returns early when nothing has changed, so the syscall cost per
	// call is bounded to ~one readdir plus one stat per top-level _* dir.
	cores.GlobalRBFCache.SetFilesystem(p.filesystem())
	cores.GlobalRBFCache.SetPersistPath(filepath.Join(helpers.DataDir(p), config.CacheDir, cores.RBFCacheFileName))
	cores.GlobalRBFCache.Refresh()

	amiga := platforms.Launcher{
		ID:         systemdefs.SystemAmiga,
		SystemID:   systemdefs.SystemAmiga,
		Folders:    []string{"Amiga"},
		Extensions: []string{".adf"},
		Test: func(_ *config.Instance, path string) bool {
			if isAmigaVisionListingFile(path) || isAmigaVisionVirtualMGLPath(path) {
				return true
			}

			return isAmigaVisionVirtualPath(path)
		},
		Launch: launchAmiga(p),
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			default:
			}

			log.Info().Msg("starting amigavision scan")

			results = filterAmigaVisionListingFiles(results)

			s, err := systemdefs.GetSystem(systemdefs.SystemAmiga)
			if err != nil {
				return results, fmt.Errorf("failed to get Amiga system: %w", err)
			}

			sfs := mediascanner.GetSystemPaths(ctx, cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			log.Debug().Int("paths", len(sfs)).Msg("amigavision scan paths found")

			preferredPaths, otherPaths := splitAmigaVisionInstallPaths(sfs)
			validPaths := make([]mediascanner.PathResult, 0, len(preferredPaths)+len(otherPaths))
			validPaths = append(validPaths, preferredPaths...)
			validPaths = append(validPaths, otherPaths...)

			for _, sf := range validPaths {
				select {
				case <-ctx.Done():
					return results, ctx.Err()
				default:
				}

				for _, listing := range amigaVisionListings {
					tp, err := mediascanner.FindPath(ctx, filepath.Join(sf.Path, listing.Path))
					if err != nil {
						continue
					}
					results = append(results, scanAmigaVisionListingFile(tp, sf.Path, listing)...)
				}
				results = append(results, amigaVisionMGLScanResults(sf.Path, amigaVisionMGLPaths)...)
			}

			log.Debug().Int("results", len(results)).Msg("amigavision scan completed")

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
			ctx context.Context,
			cfg *config.Instance,
			_ string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			select {
			case <-ctx.Done():
				return results, ctx.Err()
			default:
			}

			log.Info().Msg("starting neogeo scan")
			inputResultCount := len(results)
			filteredResultCount := 0
			addedResultCount := 0
			romsetDefinitionCount := 0
			romsetsFilename := "romsets.xml"
			names := make(map[string]string)

			s, err := systemdefs.GetSystem(systemdefs.SystemNeoGeo)
			if err != nil {
				return results, fmt.Errorf("failed to get NeoGeo system: %w", err)
			}

			sfs := mediascanner.GetSystemPaths(ctx, cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})

			// Collect NEOGEO paths for filtering
			neogeoPaths := make([]string, len(sfs))
			for i, sf := range sfs {
				neogeoPaths[i] = sf.Path
			}
			log.Debug().Int("paths", len(sfs)).Strs("roots", neogeoPaths).Msg("neogeo scan paths found")

			// First pass: load all romsets from all directories
			for _, sf := range sfs {
				select {
				case <-ctx.Done():
					return results, ctx.Err()
				default:
				}

				expectedRomsetsPath := filepath.Join(sf.Path, romsetsFilename)
				rsf, findErr := mediascanner.FindPath(ctx, expectedRomsetsPath)
				if findErr != nil {
					log.Debug().Err(findErr).Str("path", expectedRomsetsPath).Msg("neogeo romsets not found")
					continue
				}

				romsets, readErr := readRomsets(rsf)
				if readErr != nil {
					log.Warn().Err(readErr).Str("path", rsf).Msg("unable to read neogeo romsets")
					continue
				}

				romsetDefinitionCount += len(romsets)
				for _, romset := range romsets {
					// Handle comma-separated romset name aliases
					for _, name := range strings.Split(romset.Name, ",") {
						names[strings.ToLower(strings.TrimSpace(name))] = romset.AltName
					}
				}
				log.Debug().Str("path", rsf).Int("romsets", len(romsets)).Int("totalAliases", len(names)).
					Msg("neogeo romsets loaded")
			}

			resultsBeforeFilter := len(results)
			if len(names) == 0 {
				log.Warn().Strs("roots", neogeoPaths).
					Msg("no valid romsets.xml found, applying fallback filter for zip contents")
				results = filterNeoGeoZipToNeoOnly(results)
			} else {
				results = filterNeoGeoGameContents(results, names, neogeoPaths)
			}
			if removed := resultsBeforeFilter - len(results); removed > 0 {
				filteredResultCount = removed
			}

			// Second pass: read directories recursively and add launchable romset entries.
			if len(names) == 0 {
				log.Debug().Msg("skipping neogeo recursive scan without romsets")
			} else {
				osFs := afero.NewOsFs()
				seenNeoGeoEntries := make(map[string]struct{})
				for _, sf := range sfs {
					select {
					case <-ctx.Done():
						return results, ctx.Err()
					default:
					}

					entries, scanErr := collectNeoGeoRomsetEntries(ctx, osFs, sf.Path, names, seenNeoGeoEntries)
					if scanErr != nil {
						if ctx.Err() != nil {
							return results, ctx.Err()
						}
						log.Warn().Err(scanErr).Str("path", sf.Path).Msg("unable to scan neogeo directory")
						continue
					}
					results = append(results, entries...)
					addedResultCount += len(entries)
					log.Debug().Str("path", sf.Path).Int("matches", len(entries)).
						Msg("neogeo romset root scanned")
				}
			}

			log.Debug().Int("roots", len(sfs)).Int("romsets", romsetDefinitionCount).
				Int("aliases", len(names)).Int("input", inputResultCount).Int("filtered", filteredResultCount).
				Int("added", addedResultCount).Int("results", len(results)).
				Msg("neogeo scan completed")

			return results, nil
		},
	}

	neogeo, neogeoMVS := addNeoGeoMVSLauncher(p, &neogeo)
	ls := addArcadeSystemLaunchers(p, CreateLaunchers(p))
	ls = append(
		ls, amiga, neogeo, neogeoMVS,
		createVideoLauncher(p), createScummVMLauncher(p), createAudioScannerLauncher(),
	)

	custom := helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers())
	return append(custom, ls...)
}

func (*Platform) MinimumUIDisplay(kind models.UIEventKind) time.Duration {
	if kind == models.UIEventKindNotice {
		return preNoticeTime()
	}
	return 0
}

func (p *Platform) PresentUI(
	ctx context.Context,
	event *models.UIEvent,
) (func() error, error) {
	const orphanTimeoutSeconds = int(time.Hour / time.Second)

	p.platformMu.Lock()
	needsDelay := time.Since(p.lastUIHidden) < 2*time.Second
	p.platformMu.Unlock()
	if needsDelay {
		log.Debug().Msg("waiting for previous UI event to finish")
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	closeEvent := func(argsPath string) func() error {
		return func() error {
			p.platformMu.Lock()
			defer p.platformMu.Unlock()
			p.lastUIHidden = time.Now()
			return hideNotice(p.filesystem(), argsPath)
		}
	}

	switch event.Kind {
	case models.UIEventKindNotice, models.UIEventKindLoader:
		text := event.Message
		if text == "" {
			text = event.Title
		}
		argsPath, err := showNotice(p, widgetmodels.NoticeArgs{
			Text:        text,
			EventID:     event.ID,
			Timeout:     orphanTimeoutSeconds,
			Dismissible: event.Dismissible,
		}, event.Kind == models.UIEventKindLoader)
		if err != nil {
			return nil, err
		}
		return closeEvent(argsPath), nil
	case models.UIEventKindPicker, models.UIEventKindConfirm:
		items := make([]widgetmodels.PickerItem, 0, len(event.Choices))
		selected := -1
		if event.Kind == models.UIEventKindConfirm {
			items = append(items, widgetmodels.PickerItem{
				Name:   "Confirm",
				Action: models.UIResponseActionConfirm,
			})
			selected = 0
		} else {
			for i, choice := range event.Choices {
				items = append(items, widgetmodels.PickerItem{
					ID:     choice.ID,
					Name:   choice.Label,
					Action: models.UIResponseActionSelect,
				})
				if choice.ID == event.SelectedChoiceID {
					selected = i
				}
			}
		}
		argsPath, err := showPicker(p, &widgetmodels.PickerArgs{
			Title:       event.Title,
			Message:     event.Message,
			EventID:     event.ID,
			Items:       items,
			Selected:    selected,
			Timeout:     orphanTimeoutSeconds,
			Dismissible: event.Dismissible,
		})
		if err != nil {
			return nil, err
		}
		return closeEvent(argsPath), nil
	default:
		return nil, platforms.ErrNotSupported
	}
}

func (p *Platform) ConsoleManager() platforms.ConsoleManager {
	return p.consoleManager
}

// ManagedByPackageManager checks if this install is managed by MiSTer
// Downloader or update_all by looking for known database entries in
// the downloader configuration file.
func (*Platform) ManagedByPackageManager() bool {
	path := filepath.Join(
		misterconfig.ScriptsDir,
		".config", "downloader", "downloader.json",
	)

	data, err := os.ReadFile(path) //nolint:gosec // Internal config path
	if err != nil {
		return false
	}

	var cfg struct {
		Dbs map[string]json.RawMessage `json:"dbs"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}

	// MiSTer Downloader database IDs for Zaparoo (mrext/tapto) and
	// the full mrext suite (mrext/all) which includes Zaparoo.
	_, hasTapto := cfg.Dbs["mrext/tapto"]
	_, hasAll := cfg.Dbs["mrext/all"]
	return hasTapto || hasAll
}

func (*Platform) Scrapers(_ *config.Instance) map[string]platforms.Scraper {
	gamelist := gamelistxml.NewPlatformScraper()
	media := localmedia.NewPlatformScraper()
	return map[string]platforms.Scraper{gamelist.ID: gamelist, media.ID: media}
}

// SetArcadeCardLaunch caches the arcade setname when launching via card.
func (p *Platform) SetArcadeCardLaunch(setname string) {
	p.arcadeCardLaunch.mu.Lock()
	defer p.arcadeCardLaunch.mu.Unlock()
	p.arcadeCardLaunch.setname = setname
	p.arcadeCardLaunch.timestamp = time.Now()
	log.Debug().
		Str("setname", setname).
		Msg("cached arcade card launch")
}

// CheckAndClearArcadeCardLaunch checks if the setname was recently launched via card.
// Returns true if there's a match within the last 15 seconds, false otherwise.
// Clears the cache after checking to prevent stale suppressions.
func (p *Platform) CheckAndClearArcadeCardLaunch(setname string) bool {
	p.arcadeCardLaunch.mu.Lock()
	defer p.arcadeCardLaunch.mu.Unlock()

	// Check if cache is empty
	if p.arcadeCardLaunch.setname == "" {
		return false
	}

	// Check if setnames match
	if p.arcadeCardLaunch.setname != setname {
		return false
	}

	// Check if within time window (15 seconds)
	elapsed := time.Since(p.arcadeCardLaunch.timestamp)
	if elapsed > 15*time.Second {
		// Cache is stale, clear it
		p.arcadeCardLaunch.setname = ""
		return false
	}

	// Match found - clear cache and return true
	log.Debug().
		Str("setname", setname).
		Dur("elapsed", elapsed).
		Msg("suppressing duplicate arcade notification")
	p.arcadeCardLaunch.setname = ""
	return true
}
