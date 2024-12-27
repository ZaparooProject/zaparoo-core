package platforms

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
)

const (
	AssetsDir   = "assets"
	MappingsDir = "mappings"
)

type CmdEnv struct {
	Cmd           string
	Args          string
	NamedArgs     map[string]string
	Cfg           *config.Instance
	Playlist      playlists.PlaylistController
	Manual        bool
	Text          string
	TotalCommands int
	CurrentIndex  int
}

type ScanResult struct {
	Path string
	Name string
}

type Launcher struct {
	// Unique ID of the launcher, visible to user.
	Id string
	// Systems associated with this launcher.
	SystemId string
	// Folders to scan for files, relative to the root folders of the platform.
	// TODO: Support absolute paths?
	// TODO: rename RootDirs
	Folders []string
	// Extensions to match for files during a standard scan.
	Extensions []string
	// Accepted schemes for URI-style launches.
	Schemes []string
	// Launch function, takes a direct as possible path/ID media file.
	Launch func(*config.Instance, string) error
	// Kill function kills the current active launcher, if possible.
	Kill func(*config.Instance) error
	// Optional function to perform custom media scanning. Takes the list of
	// results from the standard scan, if any, and returns the final list.
	Scanner func(*config.Instance, string, []ScanResult) ([]ScanResult, error)
	// If true, all resolved paths must be in the allow list before they
	// can be launched.
	AllowListOnly bool
}

type Platform interface {
	// Id returns the unique ID of this platform.
	Id() string
	// StartPre runs any necessary platform setup functions before the main
	// service has started running.
	StartPre(*config.Instance) error
	// StartPost runs any necessary platform setup function after the main
	// service has started running.
	StartPost(*config.Instance, chan<- models.Notification) error
	// Stop runs any necessary cleanup function before the rest of the service
	// starts shutting down.
	Stop() error
	// Run immediately after a successful scan, before it is processed for launching.
	AfterScanHook(tokens.Token) error
	// Run after the active readers have been updated.
	ReadersUpdateHook(map[string]*readers.Reader) error
	// List of supported readers for this platform.
	SupportedReaders(*config.Instance) []readers.Reader
	// List of root folders to scan for media files.
	RootDirs(*config.Instance) []string
	// Whether to treat zip files as folders during media scanning.
	ZipsAsDirs() bool
	// Path to the configuration/database data for Zaparoo Core.
	DataDir() string
	// Path to the log folder for Zaparoo Core.
	LogDir() string
	// ConfigDir returns the path of the parent directory of the config file.
	ConfigDir() string
	// TempDir return the path for storing temporary files. It may be called
	// multiple times and must return the same path for the service lifetime.
	TempDir() string
	// Convert a path to a normalized form for the platform, the shortest
	// possible path that can interpreted and lanched by Zaparoo Core. For writing
	// to tokens.
	NormalizePath(*config.Instance, string) string
	// Kill the currently running launcher process if possible.
	KillLauncher() error
	// Return the ID of the currently active launcher. Empty string if none.
	GetActiveLauncher() string
	// Play a sound effect for error feedback.
	PlayFailSound(*config.Instance) // TODO: change to like PlaySound?
	// Play a sound effect for success feedback.
	PlaySuccessSound(*config.Instance)
	// Returns the currently active system ID.
	ActiveSystem() string
	// Returns the currently active game ID.
	ActiveGame() string // TODO: check where this is used
	// Returns the currently active game name.
	ActiveGameName() string
	// Returns the currently active game path.
	ActiveGamePath() string
	// Launch a system by ID.
	LaunchSystem(*config.Instance, string) error
	// Launch a file by path.
	LaunchFile(*config.Instance, string) error
	// Launch a shell command.
	Shell(string) error
	KeyboardInput(string) error // DEPRECATED
	KeyboardPress(string) error
	GamepadPress(string) error
	// Process a token command that has been resolved to a platform command.
	ForwardCmd(CmdEnv) error
	LookupMapping(tokens.Token) (string, bool)
	Launchers() []Launcher
}

type LaunchToken struct {
	Token    tokens.Token
	Launcher Launcher
}
