// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package platforms

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
)

var ErrNotSupported = errors.New("operation not supported on this platform")

// LauncherLifecycle determines how a launcher process is managed
type LauncherLifecycle int

const (
	// LifecycleFireAndForget (zero value) launches without tracking
	LifecycleFireAndForget LauncherLifecycle = iota
	// LifecycleTracked launches and keeps process handle for stopping
	LifecycleTracked
	// LifecycleBlocking waits for process to exit naturally
	LifecycleBlocking
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
	Playlist      playlists.PlaylistController
	Cfg           *config.Instance
	Database      *database.Database
	Cmd           parser.Command
	TotalCommands int
	CurrentIndex  int
	Unsafe        bool
}

// CmdResult returns a summary of what global side effects may or may not have
// happened as a result of a single ZapScript command running.
type CmdResult struct {
	// Playlist is the result of the playlist change.
	Playlist *playlists.Playlist
	// NewCommands instructs the script runner to prepend these additional
	// commands to the current script's remaining command list.
	NewCommands []parser.Command
	// MediaChanged is true if a command may have started or stopped running
	// media, and could affect handling of the hold mode feature. This doesn't
	// include playlist changes, which manage running media separately.
	MediaChanged bool
	// PlaylistChanged is true if a command started/changed/stopped a playlist.
	PlaylistChanged bool
	// Unsafe flags that a token has been generate by a remote/untrusted source
	// and can no longer be considered safe. This flag will flow on to any
	// remaining commands.
	Unsafe bool
}

// ScanResult is a result generated from a media database indexing files or
// other media sources.
type ScanResult struct {
	// Path is the absolute path to this media.
	Path string
	// Name is the display name of the media, shown to the users and used for
	// search queries.
	Name string
	// NoExt indicates this is a virtual path with no file extension.
	// When true, filepath.Ext() extraction is skipped to avoid extracting
	// garbage from paths like "/games/file.txt/Game (v1.0)" or "kodi://123/Dr. Strange".
	NoExt bool
}

// Launcher defines how a platform launcher can launch media and what media it
// supports launching.
type Launcher struct {
	// Test function returns true if file looks supported by this launcher.
	// It's checked after all standard extension and folder checks.
	Test func(*config.Instance, string) bool
	// Launch function, takes a direct as possible path/ID media file.
	// Returns process handle for tracked processes, nil for fire-and-forget.
	Launch func(*config.Instance, string) (*os.Process, error)
	// Kill function kills the current active launcher, if possible.
	Kill func(*config.Instance) error
	// Optional function to perform custom media scanning. Takes the list of
	// results from the standard scan, if any, and returns the final list.
	Scanner func(context.Context, *config.Instance, string, []ScanResult) ([]ScanResult, error)
	// Unique ID of the launcher, visible to user.
	ID string
	// System associated with this launcher.
	SystemID string
	// Folders to scan for files, relative to the root folders of the platform.
	// TODO: Support absolute paths?
	// TODO: rename RootDirs
	Folders []string
	// Extensions to match for files during a standard scan.
	Extensions []string
	// Accepted schemes for URI-style launches.
	Schemes []string
	// If true, all resolved paths must be in the allow list before they
	// can be launched.
	AllowListOnly bool
	// SkipFilesystemScan prevents the mediascanner from walking this launcher's
	// folders during indexing. The launcher's Scanner (if any) still runs.
	// Use for launchers that rely entirely on custom scanners (e.g., Batocera
	// gamelist.xml, Kodi API queries) and don't need filesystem scanning.
	SkipFilesystemScan bool
	// Lifecycle determines how the launcher process is managed.
	Lifecycle LauncherLifecycle
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
	ScanHook(*tokens.Token) error
	// SupportedReaders returns a list of supported reader modules for platform.
	SupportedReaders(*config.Instance) []readers.Reader
	// RootDirs returns a list of root folders to scan for media files.
	RootDirs(*config.Instance) []string
	// NormalizePath convert a path to a normalized form for the platform, the
	// shortest possible path that can interpreted and launched by Core. For
	// writing to tokens.
	NormalizePath(*config.Instance, string) string
	// StopActiveLauncher kills/exits the currently running launcher process
	// and clears the active media if it was successful.
	StopActiveLauncher() error
	// SetTrackedProcess stores a process handle for lifecycle management.
	// Used by DoLaunch to track processes that can be killed later.
	SetTrackedProcess(*os.Process)
	// PlayAudio plays an audio file at the given path. A relative path will be
	// resolved using the data directory assets folder as the base. This
	// function does not block until the audio finishes.
	PlayAudio(string) error
	// LaunchSystem launches a system by ID. This generally means, if a
	// platform even has the capability, attempt to launch the default or most
	// appropriate launcher for a given system, without any media loaded.
	LaunchSystem(*config.Instance, string) error
	// LaunchMedia launches some media by path and sets the active media if it
	// was successful. Pass nil for launcher to auto-detect, or a specific Launcher.
	LaunchMedia(*config.Instance, string, *Launcher) error
	// KeyboardPress presses and then releases a single keyboard button on a
	// virtual keyboard, using a key name from the ZapScript format.
	KeyboardPress(string) error
	// GamepadPress presses and then releases a single gamepad button on a
	// virtual gamepad, using a button name from the ZapScript format.
	GamepadPress(string) error
	// ForwardCmd processes a platform-specific ZapScript command.
	ForwardCmd(*CmdEnv) (CmdResult, error)
	// LookupMapping is a platform-specific method of matching a token to a
	// mapping. It takes last precedence when checking mapping sources.
	LookupMapping(*tokens.Token) (string, bool) // DEPRECATED
	// Launchers is the complete list of all launchers available on this
	// platform.
	Launchers(*config.Instance) []Launcher
	// ShowNotice displays a string on-screen of the platform device. Returns
	// a function that may be used to manually hide the notice and a minimum
	// amount of time that should be waited until trying to close the notice,
	// for platforms where initializing a notice takes time.
	// TODO: can this just block instead of returning a delay?
	ShowNotice(
		*config.Instance,
		widgetmodels.NoticeArgs,
	) (func() error, time.Duration, error)
	// ShowLoader displays a string on-screen of the platform device alongside
	// an animation indicating something is in progress. Returns a function
	// that may be used to manually hide the loader and an optional delay to
	// wait before hiding.
	// TODO: does this need a close delay returned as well?
	ShowLoader(*config.Instance, widgetmodels.NoticeArgs) (func() error, error)
	// ShowPicker displays a list picker on-screen of the platform device with
	// a list of Zap Link Cmds to choose from. The chosen action will be
	// forwarded to the local API instance to be run. Returns a function that
	// may be used to manually cancel and hide the picker.
	// TODO: it appears to not return said function
	ShowPicker(*config.Instance, widgetmodels.PickerArgs) error
}
