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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

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

	file := path.Base(u.Path)
	decoded, err := url.PathUnescape(file)
	if err != nil {
		decoded = file
	}
	ext := path.Ext(decoded)
	name := defaultName
	if name == "" {
		name = strings.TrimSuffix(decoded, ext)
	}
	return mediaNames{
		display:  name,
		filename: decoded,
		ext:      ext,
	}
}

func showPreNotice(cfg *config.Instance, pl platforms.Platform, text string) error {
	if text != "" {
		hide, delay, err := pl.ShowNotice(cfg, widgetmodels.NoticeArgs{
			Text: text,
		})
		if err != nil {
			return fmt.Errorf("error showing pre-notice: %w", err)
		}
		if hide == nil {
			hide = func() error { return nil }
		}

		if delay > 0 {
			log.Debug().Msgf("delaying pre-notice: %d", delay)
			time.Sleep(delay)
		}

		err = hide()
		if err != nil {
			return fmt.Errorf("error hiding pre-notice: %w", err)
		}
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

	fallbackDir := cfg.DefaultMediaDir()
	if fallbackDir == "" {
		fallbackDir = filepath.Join(helpers.DataDir(pl), config.MediaDir)
	}
	fallbackDir = filepath.Join(fallbackDir, system.ID)

	// TODO: this would be better if it could auto-detect the existing preferred
	//       platform games folder, but there's currently no shared mechanism to
	//       work out the correct root folder for a platform
	// TODO: config override to set explicit fallback/default media folder

	localPath := filepath.Clean(filepath.Join(fallbackDir, names.filename))

	return localPath, nil
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
	fileURL string,
	systemID string,
	preNotice string,
	displayName string,
	downloader func(opts DownloaderArgs) error,
) (string, error) {
	if fileURL == "" {
		return "", errors.New("media download url is empty")
	}
	if systemID == "" {
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

	names := namesFromURL(fileURL, displayName)

	localPath, err := findInstallDir(cfg, pl, systemID, names)
	if err != nil {
		return "", fmt.Errorf("error finding install dir: %w", err)
	}

	tempPath := localPath + ".part"

	log.Debug().Msgf("remote media local path: %s", localPath)

	// check if the file already exists
	if _, statErr := os.Stat(localPath); statErr == nil {
		if err = showPreNotice(cfg, pl, preNotice); err != nil {
			log.Warn().Err(err).Msgf("error showing pre-notice")
		}
		return localPath, nil
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", fmt.Errorf("error checking file: %w", statErr)
	}

	//nolint:gosec // Safe: other processes may see installed media
	if err = os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", fmt.Errorf("cannot create directories: %w", err)
	}

	// download the file
	log.Info().Msgf("downloading remote media: %s", fileURL)

	itemDisplay := names.display
	loadingText := fmt.Sprintf("Downloading %s...", itemDisplay)

	hideLoader, err := pl.ShowLoader(cfg, widgetmodels.NoticeArgs{
		Text: loadingText,
	})
	if err != nil {
		log.Warn().Err(err).Msgf("error showing loading dialog")
	}
	if hideLoader == nil {
		hideLoader = func() error { return nil }
	}

	if _, statErr := os.Stat(tempPath); statErr == nil {
		log.Warn().Msgf("removing leftover temp file: %s", tempPath)
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Warn().Err(removeErr).Msgf("error removing temp file: %s", tempPath)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		_ = hideLoader()
		return "", fmt.Errorf("error checking temp file: %w", statErr)
	}

	err = downloader(DownloaderArgs{
		cfg:       cfg,
		ctx:       ctx,
		url:       fileURL,
		finalPath: localPath,
		tempPath:  tempPath,
	})
	if err != nil {
		_ = hideLoader()
		return "", fmt.Errorf("error downloading file: %w", err)
	}

	err = hideLoader()
	if err != nil {
		log.Warn().Err(err).Msgf("error hiding loading dialog")
	}

	err = showPreNotice(cfg, pl, preNotice)
	if err != nil {
		log.Warn().Err(err).Msgf("error showing pre-notice")
	}

	return localPath, nil
}
