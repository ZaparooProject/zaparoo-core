package platforms

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"time"

	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
)

const (
	AssetsDir   = "assets"
	MappingsDir = "mappings"
)

const (
	PlatformIDBatocera  = "batocera"
	PlatformIDBazzite   = "bazzite"
	PlatformIDChimeraOS = "chimeraos"
	PlatformIDLibreELEC = "libreelec"
	PlatformIDLinux     = "linux"
	PlatformIDMac       = "mac"
	PlatformIDMister    = "mister"
	PlatformIDMistex    = "mistex"
	PlatformIDRecalbox  = "recalbox"
	PlatformIDRetroPie  = "retropie"
	PlatformIDSteamOS   = "steamos"
	PlatformIDWindows   = "windows"
)

type CmdEnv struct {
	Cmd           string
	Args          string
	NamedArgs     map[string]string
	Cfg           *config.Instance
	Playlist      playlists.PlaylistController
	Text          string
	TotalCommands int
	CurrentIndex  int
	Unsafe        bool
}

// CmdResult returns a summary of what global side effects may or may not have
// happened as a result of a single ZapScript command running.
type CmdResult struct {
	// MediaChanged is true if a command may have started or stopped running
	// media, and could affect handling of the hold mode feature. This doesn't
	// include playlist changes, which manage running media separately.
	MediaChanged bool
	// PlaylistChanged is true if a command started/changed/stopped a playlist.
	PlaylistChanged bool
	// Playlist is the result of the playlist change.
	Playlist *playlists.Playlist
}

type ScanResult struct {
	Path string
	Name string
}

type Launcher struct {
	// Unique ID of the launcher, visible to user.
	Id string
	// Systems associated with this launcher.
	SystemID string
	// Folders to scan for files, relative to the root folders of the platform.
	// TODO: Support absolute paths?
	// TODO: rename RootDirs
	Folders []string
	// Extensions to match for files during a standard scan.
	Extensions []string
	// Accepted schemes for URI-style launches.
	Schemes []string
	// Test function returns true if file looks supported by this launcher.
	// It's checked after all standard extension and folder checks.
	Test func(*config.Instance, string) bool
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
	// ID returns the unique ID of this platform.
	ID() string
	// StartPre runs any necessary platform setup BEFORE the main
	// service has started running.
	StartPre(*config.Instance) error
	// StartPost runs any necessary platform setup AFTER the main
	// service has started running.
	StartPost(
		*config.Instance,
		func() *models.ActiveMedia,
		func(*models.ActiveMedia),
	) error
	// Stop runs any necessary cleanup tasks before the rest of the service
	// starts shutting down.
	Stop() error
	// AfterScanHook is run immediately after a successful scan, but before
	// it is processed for launching.
	AfterScanHook(tokens.Token) error
	// ReadersUpdateHook runs after a change has occurred with the state of
	// the connected readers (i.e. when a reader is connected or disconnected),
	// and is given the current new state of readers connected.
	// TODO: this hook isn't very useful without knowing what changed. it may be
	// better to split it into 2 separate hooks for added/removed
	ReadersUpdateHook(map[string]*readers.Reader) error
	// SupportedReaders returns a list of supported reader modules for platform.
	SupportedReaders(*config.Instance) []readers.Reader
	// RootDirs returns a list of root folders to scan for media files.
	RootDirs(*config.Instance) []string
	// ZipsAsDirs returns true if the platform treats .zip files as folders.
	// TODO: this is just a mister thing. i wonder if it would be better to have
	// some sort of single "config" value to look up things like this
	// instead of implementing a method on every platform
	ZipsAsDirs() bool
	// DataDir returns the path to the configuration/database data for Core.
	DataDir() string
	// LogDir returns the path to the log folder for Zaparoo Core.
	LogDir() string
	// ConfigDir returns the path of the parent directory of the config file.
	ConfigDir() string
	// TempDir returns the path for storing temporary files. It may be called
	// multiple times and must return the same path for the service lifetime.
	TempDir() string
	// NormalizePath convert a path to a normalized form for the platform, the
	// shortest possible path that can interpreted and launched by Core. For
	// writing to tokens.
	NormalizePath(*config.Instance, string) string
	// KillLauncher kills the currently running launcher process, if possible.
	KillLauncher() error
	// PlayFailSound plays a sound effect for error feedback.
	// TODO: merge with PlaySuccessSound into single PlayAudio function?
	PlayFailSound(*config.Instance)
	// PlaySuccessSound plays a sound effect for success feedback.
	PlaySuccessSound(*config.Instance)
	// LaunchSystem launches a system by ID, if possible for platform.
	LaunchSystem(*config.Instance, string) error
	// LaunchMedia launches a file by path.
	LaunchMedia(*config.Instance, string) error
	KeyboardInput(string) error // DEPRECATED
	KeyboardPress(string) error
	GamepadPress(string) error
	// ForwardCmd processes a platform-specific ZapScript command.
	ForwardCmd(CmdEnv) (CmdResult, error)
	LookupMapping(tokens.Token) (string, bool)
	Launchers() []Launcher
	// ShowNotice displays a string on-screen of the platform device. Returns
	// a function that may be used to manually hide the notice.
	ShowNotice(*config.Instance, widgetModels.NoticeArgs) (func() error, time.Duration, error)
	// ShowLoader displays a string on-screen of the platform device alongside
	// an animation indicating something is in progress. Returns a function
	// that may be used to manually hide the loader and an optional delay to
	// wait before hiding.
	ShowLoader(*config.Instance, widgetModels.NoticeArgs) (func() error, error)
	// ShowPicker displays a list picker on-screen of the platform device with
	// a list of Zap Link Cmds to choose from. The chosen action will be
	// forwarded to the local API instance to be run. Returns a function that
	// may be used to manually cancel and hide the picker.
	ShowPicker(*config.Instance, widgetModels.PickerArgs) error
}

type LaunchToken struct {
	Token    tokens.Token
	Launcher Launcher
}
