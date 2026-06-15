//go:build linux

package mister

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
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
	launchShortCore     func(string) error
	closeConsole        func() error
	lastLauncher        platforms.Launcher
	arcadeCardLaunch    arcadeCardLaunchCache
	stopIntent          platforms.StopIntent
	processMu           syncutil.RWMutex
	platformMu          syncutil.Mutex
}

func NewPlatform() *Platform {
	p := &Platform{
		platformMu:      syncutil.Mutex{},
		launchShortCore: mgls.LaunchShortCore,
	}
	p.consoleManager = newConsoleManager(p)
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

	go p.deferredStartPre()

	log.Info().Int64("duration_ms", time.Since(startPreStart).Milliseconds()).
		Msg("StartPre finished")
	return nil
}

// deferredStartPre runs the StartPre work that does not need to complete
// before the JSON-RPC API binds: only the picker directory bootstrap.
// Runs once per process; failures are logged and tolerated. The initial
// CSV mappings load and the mappings watcher both start synchronously
// in StartPre so LookupMapping never sees nil maps and Stop() can never
// race the watcher's stopper assignment.
func (*Platform) deferredStartPre() {
	if misterconfig.MainHasFeature(misterconfig.MainFeaturePicker) {
		if err := os.MkdirAll(misterconfig.MainPickerDir, 0o750); err != nil {
			log.Error().Err(err).Msg("failed to create picker directory")
		} else if err := os.WriteFile(misterconfig.MainPickerSelected, []byte(""), 0o600); err != nil {
			log.Error().Err(err).Msg("failed to write picker selected file")
		}
	}
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
		if err != nil {
			// Non-fatal: an embedded arcade database is used as a fallback. Download
			// failures are usually network/rate-limit issues, not code faults.
			log.Warn().Msgf("failed to download arcade database: %s", err)
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

	// If the RBF cache loaded from disk but its directory mtimes drifted,
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
	p.trackedProcess = proc
}

// setTrackedProcessWithCleanup sets the tracked process and its cleanup completion channel
func (p *Platform) setTrackedProcessWithCleanup(proc *os.Process, done chan struct{}) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
	p.processDone = done
}

// clearTrackedProcess clears both the tracked process and its cleanup channel
func (p *Platform) clearTrackedProcess() {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = nil
	p.processDone = nil
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
		LogDir:     misterconfig.TempDir,
		ZipsAsDirs: true,
	}
}

func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	// Store intent before cancelling context so cleanup goroutine can read it
	p.processMu.Lock()
	p.stopIntent = intent
	p.processMu.Unlock()

	// Check if we have a tracked process before attempting to stop it
	p.processMu.Lock()
	hadTrackedProcess := p.trackedProcess != nil
	p.processMu.Unlock()

	// Invalidate old launcher context ONLY for preemption (new launcher starting)
	// EXCEPT for console launchers which need cleanup goroutine to run
	// For StopForMenu and StopForConsoleReset, we need cleanup to run to unlock VT
	cancelContextNow := intent == platforms.StopForPreemption && !hadTrackedProcess

	// Console launchers (video/ScummVM): delay context cancellation until after cleanup

	if cancelContextNow {
		if p.launcherManager != nil {
			p.launcherManager.NewContext()
		}
	}

	// Check if launcher has custom Kill function
	p.platformMu.Lock()
	customKill := p.lastLauncher.Kill
	p.platformMu.Unlock()

	// Use custom Kill if defined (e.g., keyboard input for ScummVM)
	if customKill != nil {
		log.Debug().Msg("using custom Kill function for launcher")
		if err := customKill(&config.Instance{}); err != nil {
			log.Warn().Err(err).Msg("custom Kill function failed")
		}
		// Custom Kill function used - skip signal-based termination entirely
		// The process will exit on its own via the custom method
	} else {
		// Stop tracked process if it exists using signal-based termination
		p.processMu.Lock()
		if p.trackedProcess != nil {
			proc := p.trackedProcess

			// Staged termination approach:
			// 1. Try SIGTERM first (allows SDL cleanup to run)
			// 2. Wait 5 seconds
			// 3. If still running, force kill with SIGKILL
			// 4. After process dies, deallocate the VT to reset all state
			log.Debug().Msg("sending SIGTERM to tracked process for graceful shutdown")
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				log.Warn().Err(err).Msg("failed to send SIGTERM to tracked process")
				p.trackedProcess = nil
				p.processMu.Unlock()
			} else {
				p.trackedProcess = nil
				p.processMu.Unlock()

				// Wait for graceful exit with timeout
				done := make(chan error, 1)
				go func() {
					_, err := proc.Wait()
					done <- err
				}()

				select {
				case err := <-done:
					if err != nil {
						log.Debug().Err(err).Msg("process exited after SIGTERM")
					} else {
						log.Debug().Msg("process exited gracefully after SIGTERM")
					}
				case <-time.After(5 * time.Second):
					// SIGTERM didn't work within 5 seconds - force kill
					log.Debug().Msg("SIGTERM timeout - sending SIGKILL")
					if err := proc.Kill(); err != nil {
						log.Warn().Err(err).Msg("failed to SIGKILL process")
					} else {
						// Wait for SIGKILL to complete (should be fast)
						select {
						case <-done:
							log.Debug().Msg("process killed with SIGKILL")
						case <-time.After(500 * time.Millisecond):
							log.Warn().Msg("SIGKILL took too long")
						}
					}
				}
			}
		} else {
			p.processMu.Unlock()
		}
	}

	// Clear active media
	p.setActiveMedia(nil)

	// Return to menu if needed - but ONLY for launchers without tracked processes
	// Console launchers (video/ScummVM) have cleanup goroutines that call ReturnToMenu
	// FPGA/MGL launchers have no cleanup goroutine, so we must call it here
	if intent == platforms.StopForMenu || intent == platforms.StopForConsoleReset {
		if !hadTrackedProcess {
			// No cleanup goroutine will run - we must call ReturnToMenu ourselves
			log.Debug().Msg("no tracked process - calling ReturnToMenu directly")
			if err := p.ReturnToMenu(); err != nil {
				log.Warn().Err(err).Msg("failed to return to menu after stopping launcher")
			}
		} else {
			log.Debug().Msg("tracked process existed - cleanup goroutine will call ReturnToMenu")
		}
	}

	// For console launchers during preemption, wait for cleanup to complete
	// before cancelling context. This ensures console state (VT, cursor, video mode)
	// is properly cleaned up before the new launcher starts.
	if intent == platforms.StopForPreemption && hadTrackedProcess {
		// Get the cleanup completion channel
		p.processMu.Lock()
		done := p.processDone
		p.processMu.Unlock()

		if done != nil {
			log.Debug().Msg("waiting for console launcher cleanup to complete")
			select {
			case <-done:
				log.Debug().Msg("console launcher cleanup completed")
			case <-time.After(2 * time.Second):
				// Safety valve: don't hang if process becomes a zombie
				log.Warn().Msg("timeout waiting for console cleanup (2s)")
			}
		}

		// Now invalidate the launcher context to prevent any further operations
		if p.launcherManager != nil {
			p.launcherManager.NewContext()
		}
	}

	return nil
}

func (p *Platform) ReturnToMenu() error {
	// Restore console cursor state on both TTYs
	if err := p.consoleManager.Restore(f9ConsoleVT); err != nil {
		log.Warn().Err(err).Msg("failed to restore tty1 cursor")
	}
	if launcherConsoleVT != f9ConsoleVT {
		if err := p.consoleManager.Restore(launcherConsoleVT); err != nil {
			log.Warn().Err(err).Msgf("failed to restore tty%s cursor", launcherConsoleVT)
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
	if strings.EqualFold(id, "menu") {
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
		if info.IsDir() {
			if base == "__MACOSX" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}

			markerPath := filepath.Join(path, ".zaparooignore")
			if _, statErr := fs.Stat(markerPath); statErr == nil {
				log.Info().Str("path", path).Msg("skipping directory with .zaparooignore marker")
				return filepath.SkipDir
			}
		}

		lowerBase := strings.ToLower(base)
		candidateID := lowerBase
		isZip := filepath.Ext(lowerBase) == ".zip"
		if isZip {
			candidateID = strings.TrimSuffix(lowerBase, filepath.Ext(lowerBase))
		} else if !info.IsDir() {
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
			Name:  name,
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
	cores.GlobalRBFCache.SetPersistPath(filepath.Join(helpers.DataDir(p), config.CacheDir, cores.RBFCacheFileName))
	cores.GlobalRBFCache.Refresh()

	amiga := platforms.Launcher{
		ID:         systemdefs.SystemAmiga,
		SystemID:   systemdefs.SystemAmiga,
		Folders:    []string{"Amiga"},
		Extensions: []string{".adf"},
		Test: func(_ *config.Instance, path string) bool {
			if isAmigaVisionListingFile(path) {
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
			romsetsFilename := "romsets.xml"
			names := make(map[string]string)

			s, err := systemdefs.GetSystem(systemdefs.SystemNeoGeo)
			if err != nil {
				return results, fmt.Errorf("failed to get NeoGeo system: %w", err)
			}

			sfs := mediascanner.GetSystemPaths(ctx, cfg, p, p.RootDirs(cfg), []systemdefs.System{*s})
			log.Debug().Int("paths", len(sfs)).Msg("neogeo scan paths found")

			// Collect NEOGEO paths for filtering
			neogeoPaths := make([]string, len(sfs))
			for i, sf := range sfs {
				neogeoPaths[i] = sf.Path
			}

			// First pass: load all romsets from all directories
			for _, sf := range sfs {
				select {
				case <-ctx.Done():
					return results, ctx.Err()
				default:
				}

				rsf, err := mediascanner.FindPath(ctx, filepath.Join(sf.Path, romsetsFilename))
				if err == nil {
					romsets, readErr := readRomsets(rsf)
					if readErr != nil {
						log.Warn().Err(readErr).Msg("unable to read romsets")
						continue
					}

					for _, romset := range romsets {
						// Handle comma-separated romset name aliases
						for _, name := range strings.Split(romset.Name, ",") {
							names[strings.ToLower(strings.TrimSpace(name))] = romset.AltName
						}
					}
				}
			}

			if len(names) == 0 {
				log.Warn().Msg("no valid romsets.xml found, applying fallback filter for zip contents")
				results = filterNeoGeoZipToNeoOnly(results)
			} else {
				results = filterNeoGeoGameContents(results, names, neogeoPaths)
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
				}
			}

			log.Debug().Int("results", len(results)).Msg("neogeo scan completed")

			return results, nil
		},
	}

	ls := CreateLaunchers(p)
	ls = append(ls, amiga, neogeo, createVideoLauncher(p), createScummVMLauncher(p), createAudioScannerLauncher())

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), ls...)
}

func (p *Platform) ShowNotice(
	_ *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	p.platformMu.Lock()
	needsDelay := time.Since(p.lastUIHidden) < 2*time.Second &&
		!misterconfig.MainHasFeature(misterconfig.MainFeatureNotice)
	p.platformMu.Unlock()

	if needsDelay {
		log.Debug().Msg("waiting for previous notice to finish")
		time.Sleep(3 * time.Second)
	}

	completePath, err := showNotice(p, args.Text, false)
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
	_ *config.Instance,
	args widgetmodels.NoticeArgs,
) (func() error, error) {
	p.platformMu.Lock()
	needsDelay := time.Since(p.lastUIHidden) < 2*time.Second &&
		!misterconfig.MainHasFeature(misterconfig.MainFeatureNotice)
	p.platformMu.Unlock()

	if needsDelay {
		log.Debug().Msg("waiting for previous notice to finish")
		time.Sleep(3 * time.Second)
	}

	completePath, err := showNotice(p, args.Text, true)
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
	_ *config.Instance,
	args widgetmodels.PickerArgs,
) error {
	return showPicker(p, args)
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
