// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
	"github.com/rs/zerolog/log"
)

// ErrUnsafeMediaName is returned for a download URL whose last path segment is
// not a plain filename. The name comes from a remote server, so it decides
// where on this device the download lands.
var ErrUnsafeMediaName = errors.New("remote media name is not a plain filename")

type mediaNames struct {
	display  string
	filename string
	ext      string
}

func namesFromURL(rawURL, defaultName string) mediaNames {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		file := filepath.Base(rawURL)
		ext := filepath.Ext(file)
		name := defaultName
		if name == "" {
			name = strings.TrimSuffix(file, ext)
		}
		return mediaNames{
			display:  name,
			filename: file,
			ext:      ext,
		}
	}

	// url.Parse already returns Path decoded, so the segment is unescaped
	// here and must not be unescaped again: a second pass turns %252e%252e%252f
	// into "../" and hands the server a say in which directory it writes to.
	file := path.Base(u.Path)
	ext := path.Ext(file)
	name := defaultName
	if name == "" {
		name = strings.TrimSuffix(file, ext)
	}
	return mediaNames{
		display:  name,
		filename: file,
		ext:      ext,
	}
}

// decodeDisplayText undoes percent-encoding in text that is only shown to the
// user. A remote URL launch carries pre_notice and name in its query string,
// where spaces arrive as %20. Text that is not valid percent-encoding, such as
// "100% done", is shown as written. A "+" is left alone because it is a
// literal plus in hand-written ZapScript.
func decodeDisplayText(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

// safeMediaFilename reports whether name may be joined onto an install
// directory. The name is chosen by whoever serves the URL, so it has to be one
// path segment that stays put: anything carrying a separator, or naming a
// directory rather than a file, would move the download somewhere the caller
// did not pick.
func safeMediaFilename(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return false
	}
	// filepath.Base is what the join would collapse the name to. A name that
	// survives it unchanged cannot traverse on this OS.
	return filepath.Base(name) == name
}

func showPreNotice(
	ctx context.Context,
	ui *uievents.Service,
	text string,
) error {
	if text == "" {
		return nil
	}
	if ui == nil {
		return nil
	}
	handle, err := ui.Open(ctx, &uievents.Request{
		Kind:        models.UIEventKindNotice,
		Message:     text,
		Timeout:     30 * time.Second,
		Dismissible: true,
	})
	if err != nil {
		return fmt.Errorf("error opening pre-notice: %w", err)
	}
	if handle.MinimumDisplay > 0 {
		log.Debug().Dur("delay", handle.MinimumDisplay).Msg("delaying pre-notice")
		timer := time.NewTimer(handle.MinimumDisplay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	if err = handle.Complete(models.UIOutcomeCompleted); err != nil {
		return fmt.Errorf("error completing pre-notice: %w", err)
	}
	return nil
}

func findInstallDir(
	cfg *config.Instance,
	pl platforms.Platform,
	systemID string,
	names mediaNames,
) (string, error) {
	system, err := systemdefs.LookupSystem(systemID)
	if err != nil {
		return "", fmt.Errorf("error getting system: %w", err)
	}

	if !safeMediaFilename(names.filename) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeMediaName, names.filename)
	}

	// Downloads used to land in the data directory's media folder. One that
	// is already there is used where it is rather than fetched again.
	legacyDir := legacyInstallDir(cfg, pl, system.ID)
	legacyPath, err := joinWithin(legacyDir, names.filename)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(legacyPath); statErr == nil {
		return legacyPath, nil
	}

	return joinWithin(preferredInstallDir(cfg, pl, system.ID, legacyDir), names.filename)
}

// joinWithin joins a checked file name onto dir. The name check should make
// an escape unreachable; this stays because the cost of being wrong is a
// write outside the media directory, and this is the last point where that is
// still cheap to stop.
func joinWithin(dir, name string) (string, error) {
	localPath := filepath.Clean(filepath.Join(dir, name))
	if !isWithinDir(dir, localPath) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeMediaName, name)
	}
	return localPath, nil
}

// legacyInstallDir is <media_dir>/<system>, or <data>/media/<system> when no
// media_dir is set: where downloads went before they were placed beside the
// system's other games, and still the fallback when there is no such folder.
func legacyInstallDir(cfg *config.Instance, pl platforms.Platform, systemID string) string {
	dir := cfg.DefaultMediaDir()
	if dir == "" {
		dir = filepath.Join(helpers.DataDir(pl), config.MediaDir)
	}
	return filepath.Join(dir, systemID)
}

// systemFolders returns the folders the system's launchers read games from:
// each root directory joined with each relative launcher folder, highest
// priority root first, then the launchers' absolute folders. The first
// relative folder is also returned, as the one to create when none exist.
func systemFolders(cfg *config.Instance, pl platforms.Platform, systemID string) (dirs []string, create string) {
	var rel, abs []string
	seen := make(map[string]struct{})
	launchers := pl.Launchers(cfg)
	for i := range launchers {
		l := &launchers[i]
		if l.SystemID != systemID || l.SkipFilesystemScan {
			continue
		}
		for _, folder := range l.Folders {
			if folder == "" {
				continue
			}
			if _, dup := seen[folder]; dup {
				continue
			}
			seen[folder] = struct{}{}
			if filepath.IsAbs(folder) {
				abs = append(abs, folder)
			} else {
				rel = append(rel, folder)
			}
		}
	}
	roots := pl.RootDirs(cfg)
	for _, root := range roots {
		for _, folder := range rel {
			dirs = append(dirs, filepath.Join(root, folder))
		}
	}
	dirs = append(dirs, abs...)
	if len(rel) > 0 {
		for _, root := range roots {
			if isDir(root) {
				create = filepath.Join(root, rel[0])
				break
			}
		}
	}
	return dirs, create
}

// preferredInstallDir picks where a download for the system goes: the first
// of the system's game folders that exists, so the file sits beside the
// user's other games and is indexed with them; else the main game folder,
// created under the highest priority root that exists; else the legacy
// media folder.
func preferredInstallDir(cfg *config.Instance, pl platforms.Platform, systemID, legacyDir string) string {
	dirs, create := systemFolders(cfg, pl, systemID)
	for _, dir := range dirs {
		if isDir(dir) {
			return dir
		}
	}
	if create != "" {
		return create
	}
	return legacyDir
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// isWithinDir reports whether target sits under dir. Both are expected clean
// and absolute; a relative traversal out of dir, or a sibling with dir as a
// string prefix, is not within it.
func isWithinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type DownloaderArgs struct {
	cfg       *config.Instance
	ctx       context.Context
	url       string
	finalPath string
	tempPath  string
}

// maxDownloadTimeout is the emergency timeout for downloads. This is a safety
// backstop for stalled connections, not the primary cancellation mechanism.
const maxDownloadTimeout = 1 * time.Hour

func InstallRemoteFile(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	ui *uievents.Service,
	fileURL string,
	systemID string,
	preNotice string,
	displayName string,
	downloader func(opts DownloaderArgs) error,
) (string, error) {
	return InstallRemoteMedia(ctx, cfg, pl, ui, &RemoteMedia{
		URL:         fileURL,
		SystemID:    systemID,
		PreNotice:   preNotice,
		DisplayName: displayName,
	}, downloader)
}

// RemoteMedia describes a game or media file to fetch onto the device.
type RemoteMedia struct {
	// URL is fetched with the downloader.
	URL string
	// SystemID decides which folder the download lands in.
	SystemID string
	// FileName is the name saved on disk, or for an archive the folder it
	// is extracted into. Empty takes the last segment of URL.
	FileName string
	// DisplayName is shown while downloading. Empty uses the file name.
	DisplayName string
	// PreNotice is shown once the media is ready, before it launches.
	PreNotice string
	// LaunchExts are the extensions the system's launchers open, used to
	// pick the file to start from an extracted archive.
	LaunchExts []string
	// Size is the download size in bytes when known, so a download that
	// cannot fit is refused before it starts. Zero skips the check.
	Size int64
	// Archive is a zip of a game made of several files. It is extracted
	// into a folder named FileName and the zip deleted.
	Archive bool
}

// downloadHeadroom is the space a download must leave free beyond its own
// size, so fetching a game does not fill the disk Core and the OS run from.
const downloadHeadroom = 64 * 1024 * 1024

// ErrInsufficientSpace is a download that would not fit on the disk.
var ErrInsufficientSpace = errors.New("not enough free space for download")

// InstallRemoteMedia downloads media into the system's game folder, unless it
// is already there, and returns the local path to launch.
func InstallRemoteMedia(
	ctx context.Context,
	cfg *config.Instance,
	pl platforms.Platform,
	ui *uievents.Service,
	media *RemoteMedia,
	downloader func(opts DownloaderArgs) error,
) (string, error) {
	if media == nil || media.URL == "" {
		return "", errors.New("media download url is empty")
	}
	if media.SystemID == "" {
		return "", errors.New("media system id is empty")
	}
	if downloader == nil {
		return "", errors.New("downloader function is nil")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, maxDownloadTimeout)
	defer cancel()

	preNotice := decodeDisplayText(media.PreNotice)
	names := namesFromURL(media.URL, decodeDisplayText(media.DisplayName))
	if media.FileName != "" {
		names.filename = media.FileName
		names.ext = filepath.Ext(media.FileName)
		if names.display == "" {
			names.display = strings.TrimSuffix(media.FileName, names.ext)
		}
	}

	localPath, err := findInstallDir(cfg, pl, media.SystemID, names)
	if err != nil {
		return "", fmt.Errorf("error finding install dir: %w", err)
	}
	log.Debug().Msgf("remote media local path: %s", localPath)

	launchPath, err := existingMedia(localPath, media)
	if err != nil {
		return "", err
	}
	if launchPath != "" {
		if err = showPreNotice(ctx, ui, preNotice); err != nil {
			log.Warn().Err(err).Msgf("error showing pre-notice")
		}
		return launchPath, nil
	}

	dir := filepath.Dir(localPath)
	//nolint:gosec // Safe: other processes may see installed media
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create directories: %w", err)
	}
	if spaceErr := checkFreeSpace(dir, media); spaceErr != nil {
		return "", spaceErr
	}

	log.Info().Msgf("downloading remote media: %s", media.URL)
	hideLoader := showLoader(ctx, ui, fmt.Sprintf("Downloading %s...", names.display))

	if media.Archive {
		launchPath, err = downloadArchive(ctx, cfg, media, localPath, downloader)
	} else {
		launchPath = localPath
		err = downloadFile(ctx, cfg, media.URL, localPath, downloader)
	}
	if hideErr := hideLoader(); hideErr != nil {
		log.Warn().Err(hideErr).Msgf("error hiding loading dialog")
	}
	if err != nil {
		return "", err
	}

	if err = showPreNotice(ctx, ui, preNotice); err != nil {
		log.Warn().Err(err).Msgf("error showing pre-notice")
	}
	return launchPath, nil
}

// existingMedia returns the launch path of media already on disk at
// localPath, or "" when it still has to be downloaded.
func existingMedia(localPath string, media *RemoteMedia) (string, error) {
	info, err := os.Stat(localPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("error checking file: %w", err)
	case !media.Archive:
		return localPath, nil
	case !info.IsDir():
		return "", fmt.Errorf("%s exists and is not a folder", localPath)
	}
	launchPath, err := pickLaunchFile(localPath, media.LaunchExts)
	if err != nil {
		return "", fmt.Errorf("existing game folder: %w", err)
	}
	return launchPath, nil
}

// checkFreeSpace refuses a download of known size that would leave less than
// downloadHeadroom free. An archive is counted twice, since the zip is still
// on disk while it is extracted.
func checkFreeSpace(dir string, media *RemoteMedia) error {
	if media.Size <= 0 {
		return nil
	}
	free, err := helpers.FreeDiskSpace(dir)
	if err != nil {
		log.Warn().Err(err).Msgf("could not read free space for %s", dir)
		return nil
	}
	need := uint64(media.Size)
	if media.Archive {
		need *= 2
	}
	need += downloadHeadroom
	if free < need {
		return fmt.Errorf("%w: %d MB free in %s, need %d MB",
			ErrInsufficientSpace, free/(1024*1024), dir, need/(1024*1024))
	}
	return nil
}

func showLoader(ctx context.Context, ui *uievents.Service, text string) func() error {
	if ui == nil {
		return func() error { return nil }
	}
	handle, err := ui.Open(ctx, &uievents.Request{
		Kind:    models.UIEventKindLoader,
		Message: text,
	})
	if err != nil {
		log.Warn().Err(err).Msg("error opening loading event")
		return func() error { return nil }
	}
	return func() error {
		return handle.Complete(models.UIOutcomeCompleted)
	}
}

// downloadFile fetches rawURL to finalPath through a .part file, so an
// interrupted download is never mistaken for a finished one.
func downloadFile(
	ctx context.Context,
	cfg *config.Instance,
	rawURL, finalPath string,
	downloader func(opts DownloaderArgs) error,
) error {
	tempPath := finalPath + ".part"
	if err := removeLeftover(tempPath); err != nil {
		return err
	}
	err := downloader(DownloaderArgs{
		cfg:       cfg,
		ctx:       ctx,
		url:       rawURL,
		finalPath: finalPath,
		tempPath:  tempPath,
	})
	if err != nil {
		return fmt.Errorf("error downloading file: %w", err)
	}
	return nil
}

func removeLeftover(p string) error {
	_, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error checking leftover %s: %w", p, err)
	}
	log.Warn().Msgf("removing leftover download: %s", p)
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("error removing leftover %s: %w", p, err)
	}
	return nil
}

// downloadArchive fetches a zipped game and extracts it into gameDir. The zip
// and the half-extracted folder carry hidden names until the game is
// complete, so neither an indexer nor a later launch takes a partial game for
// a finished one.
func downloadArchive(
	ctx context.Context,
	cfg *config.Instance,
	media *RemoteMedia,
	gameDir string,
	downloader func(opts DownloaderArgs) error,
) (string, error) {
	parent, name := filepath.Split(gameDir)
	zipPath := filepath.Join(parent, "."+name+".zip")
	staging := filepath.Join(parent, "."+name+".extracting")

	if err := removeLeftover(zipPath); err != nil {
		return "", err
	}
	if err := removeLeftover(staging); err != nil {
		return "", err
	}
	if err := downloadFile(ctx, cfg, media.URL, zipPath, downloader); err != nil {
		return "", err
	}
	defer func() {
		if err := os.Remove(zipPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warn().Err(err).Msgf("error removing downloaded archive: %s", zipPath)
		}
	}()

	budget, err := extractionBudget(parent)
	if err != nil {
		return "", err
	}
	if extractErr := extractZip(ctx, zipPath, staging, budget); extractErr != nil {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			log.Warn().Err(rmErr).Msgf("error removing partial extraction: %s", staging)
		}
		return "", fmt.Errorf("error extracting %s: %w", name, extractErr)
	}

	dropSingleDiscPlaylists(staging, media.LaunchExts)
	launchFile, err := pickLaunchFile(staging, media.LaunchExts)
	if err != nil {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			log.Warn().Err(rmErr).Msgf("error removing extraction: %s", staging)
		}
		return "", fmt.Errorf("downloaded %s: %w", name, err)
	}
	rel, err := filepath.Rel(staging, launchFile)
	if err != nil {
		return "", fmt.Errorf("error locating launch file: %w", err)
	}
	if err := os.Rename(staging, gameDir); err != nil {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			log.Warn().Err(rmErr).Msgf("error removing extraction: %s", staging)
		}
		return "", fmt.Errorf("error moving extracted game into place: %w", err)
	}
	return filepath.Join(gameDir, rel), nil
}

// extractionBudget is how much an archive may write into dir: the free space
// less downloadHeadroom. Without a reading, extraction is not bounded by
// space, only by the disk filling.
func extractionBudget(dir string) (int64, error) {
	free, err := helpers.FreeDiskSpace(dir)
	if err != nil {
		log.Warn().Err(err).Msgf("could not read free space for %s", dir)
		return math.MaxInt64 - 1, nil
	}
	if free <= downloadHeadroom {
		return 0, fmt.Errorf("%w: %d MB free in %s", ErrInsufficientSpace, free/(1024*1024), dir)
	}
	return int64(min(free-downloadHeadroom, math.MaxInt64-1)), nil //nolint:gosec // capped
}
