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

// CmdEnv is the local state of a scanned token, as it processes each ZapScript
// command. Every command run has access to and can modify it.
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

// ScanResult is a result generated from a media database indexing files or
// other media sources.
type ScanResult struct {
	// Path is the absolute path to this media.
	Path string
	// Name is the display name of the media, shown to the users and used for
	// search queries.
	Name string
}

// Launcher defines how a platform launcher can launch media and what media it
// supports launching.
type Launcher struct {
	// Unique ID of the launcher, visible to user.
	ID string
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

// Settings defines all simple settings/configuration values available for a
// platform.
type Settings struct {
	// DataDir returns the root folder where things like databases and
	// downloaded assets are permanently stored. WARNING: This value should be
	// accessed using the DataDir function in the utils package.
	DataDir string
	// ConfigDir returns the directory where the config file is stored.
	// WARNING: This value should be accessed using the ConfigDir function in
	// the utils package.
	ConfigDir string
	// TempDir returns a temporary directory where the logs are stored and any
	// files used for inter-process communication. Expect it to be deleted.
	TempDir string
	// ZipsAsDir returns true if this platform treats .zip files as if they
	// were directories for the purpose of launching media.
	ZipsAsDirs bool
}

// Platform is the central interface that defines how Core interacts with a
// supported platform.
type Platform interface {
	// ID returns the unique ID of this platform.
	ID() string
	// StartPre runs any necessary platform setup BEFORE the main service has
	// started running.
	StartPre(*config.Instance) error
	// StartPost runs any necessary platform setup AFTER the main service has
	// started running.
	StartPost(
		*config.Instance,
		func() *models.ActiveMedia,
		func(*models.ActiveMedia),
	) error
	// Stop runs any necessary cleanup tasks before the rest of the service
	// starts shutting down.
	Stop() error
	// Settings returns all simple platform-specific settings such as paths.
	// NOTE: Some values on the Settings struct should be accessed using helper
	// functions in the utils package instead of directly. Check comments.
	Settings() Settings
	// ScanHook is run immediately AFTER a successful scan, but BEFORE it is
	// processed for launching.
	ScanHook(tokens.Token) error
	// SupportedReaders returns a list of supported reader modules for platform.
	SupportedReaders(*config.Instance) []readers.Reader
	// RootDirs returns a list of root folders to scan for media files.
	RootDirs(*config.Instance) []string
	// NormalizePath convert a path to a normalized form for the platform, the
	// shortest possible path that can interpreted and launched by Core. For
	// writing to tokens.
	NormalizePath(*config.Instance, string) string
	// StopActiveLauncher kills/exits the currently running launcher process.
	StopActiveLauncher() error
	// PlayAudio plays an audio file at the given path. A relative path will be
	// resolved using the data directory assets folder as the base. This
	// function does not block until the audio finishes.
	PlayAudio(string) error
	// LaunchSystem launches a system by ID, if possible for platform.
	LaunchSystem(*config.Instance, string) error
	// LaunchMedia launches a file by path.
	LaunchMedia(*config.Instance, string) error
	KeyboardInput(string) error // DEPRECATED
	KeyboardPress(string) error
	GamepadPress(string) error
	// ForwardCmd processes a platform-specific ZapScript command.
	ForwardCmd(CmdEnv) (CmdResult, error)
	// LookupMapping is a platform-specific method of matching a token to a
	// mapping. It takes last precedence when checking mapping sources.
	LookupMapping(tokens.Token) (string, bool)
	// Launchers is the complete list of all launchers available on this
	// platform.
	Launchers() []Launcher
	// ShowNotice displays a string on-screen of the platform device. Returns
	// a function that may be used to manually hide the notice and a minimum
	// amount of time that should be waited until trying to close the notice,
	// for platforms where initializing a notice takes time.
	ShowNotice(
		*config.Instance,
		widgetModels.NoticeArgs,
	) (func() error, time.Duration, error)
	// ShowLoader displays a string on-screen of the platform device alongside
	// an animation indicating something is in progress. Returns a function
	// that may be used to manually hide the loader and an optional delay to
	// wait before hiding.
	// TODO: does this need a close delay returned as well?
	ShowLoader(*config.Instance, widgetModels.NoticeArgs) (func() error, error)
	// ShowPicker displays a list picker on-screen of the platform device with
	// a list of Zap Link Cmds to choose from. The chosen action will be
	// forwarded to the local API instance to be run. Returns a function that
	// may be used to manually cancel and hide the picker.
	// TODO: it appears to not return said function
	ShowPicker(*config.Instance, widgetModels.PickerArgs) error
}
