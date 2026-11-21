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

package artwork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// InboxDir is the local staging directory shared with Main_MiSTer.
const InboxDir = "artwork-inbox"

const (
	scopeVersion = "version"
	scopeTitle   = "title"

	requestName     = "request.json"
	captureName     = "capture.png"
	maxRequestBytes = 16 << 10
	// Main reserves 6 MiB for the scaler pixels, at four bytes per pixel.
	maxCapturePixels = 6 << 18
)

var (
	errInvalidCapture = errors.New("invalid artwork capture")
	errGameUnindexed  = errors.New("artwork game has not been indexed")
)

type captureRequest struct {
	GamePath  string
	ImageType string
	Scope     string
	Version   int
}

func readCaptureFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: missing %s", errInvalidCapture, name)
	}
	if err != nil {
		return nil, fmt.Errorf("stat capture %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%w: invalid %s file", errInvalidCapture, name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open capture %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read capture %s: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s exceeds import limit", errInvalidCapture, name)
	}
	return data, nil
}

func parseRequest(data []byte) (captureRequest, error) {
	var request captureRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return captureRequest{}, fmt.Errorf("%w: %w", errInvalidCapture, err)
	}
	if request.Version != 1 || request.GamePath == "" || strings.ContainsRune(request.GamePath, 0) ||
		typeTag(request.ImageType) == "" || (request.Scope != scopeVersion && request.Scope != scopeTitle) {
		return captureRequest{}, fmt.Errorf("%w: invalid manifest fields", errInvalidCapture)
	}
	return request, nil
}

func validatePNG(data []byte) error {
	size, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: decode PNG dimensions: %w", errInvalidCapture, err)
	}
	if size.Width <= 0 || size.Height <= 0 || size.Width > maxCapturePixels/size.Height {
		return fmt.Errorf("%w: PNG dimensions exceed scaler limits", errInvalidCapture)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("%w: decode PNG: %w", errInvalidCapture, err)
	}
	return nil
}

func saveOverride(dir, kind string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create artwork override directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".artwork-*")
	if err != nil {
		return fmt.Errorf("create artwork override: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write artwork override: %w", err)
	}
	if err := os.Rename(file.Name(), filepath.Join(dir, kind+".png")); err != nil {
		return fmt.Errorf("publish artwork override: %w", err)
	}
	revision.Add(1)
	return nil
}

func resolveGame(ctx context.Context, db database.MediaDBI, path string) (*database.MediaFullRow, error) {
	matches, err := db.FindMediaIDsByPaths(ctx, []string{path})
	if err != nil {
		return nil, fmt.Errorf("look up captured game: %w", err)
	}
	var game *database.MediaFullRow
	for _, match := range matches {
		row, err := db.GetMediaWithTitleAndSystem(ctx, match.DBID)
		if err != nil {
			return nil, fmt.Errorf("load captured game: %w", err)
		}
		if row == nil || row.IsMissing || row.Path != path {
			continue
		}
		if game != nil {
			return nil, fmt.Errorf("%w: game path matches more than one system", errInvalidCapture)
		}
		game = row
	}
	if game == nil {
		return nil, errGameUnindexed
	}
	return game, nil
}

func importCapture(
	ctx context.Context, db database.MediaDBI, dataDir string, root *os.Root, maxBytes int64,
) (string, error) {
	manifest, err := readCaptureFile(root, requestName, maxRequestBytes)
	if err != nil {
		return "", err
	}
	request, err := parseRequest(manifest)
	if err != nil {
		return "", err
	}
	game, err := resolveGame(ctx, db, request.GamePath)
	if err != nil {
		return "", err
	}
	pixels, err := readCaptureFile(root, captureName, maxBytes)
	if err != nil {
		return "", err
	}
	if err := validatePNG(pixels); err != nil {
		return "", err
	}

	identity := game.Path
	if request.Scope == scopeTitle {
		slugs, err := groupSlugs(ctx, db, game)
		if err != nil {
			return "", err
		}
		// Every member of the same group replaces the same assignment.
		identity = slugs[0]
	}
	dir := assignmentDir(dataDir, game.System.SystemID, request.Scope, identity)
	if err := saveOverride(dir, request.ImageType, pixels); err != nil {
		return "", err
	}
	return game.System.SystemID, nil
}

func pendingName(name string) bool {
	suffix, ok := strings.CutPrefix(name, "capture-")
	if !ok || len(suffix) != 6 {
		return false
	}
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// ImportPending consumes completed bundles published by Main_MiSTer. A bundle
// for a game the user hasn't indexed yet remains available for a later pass.
func ImportPending(
	ctx context.Context, db database.MediaDBI, inbox, dataDir string, maxBytes int64, changed func(string),
) error {
	entries, err := os.ReadDir(inbox)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read artwork inbox: %w", err)
	}
	root, err := os.OpenRoot(inbox)
	if err != nil {
		return fmt.Errorf("open artwork inbox: %w", err)
	}
	defer func() { _ = root.Close() }()

	type queuedCapture struct {
		time time.Time
		name string
	}
	pending := make([]queuedCapture, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !pendingName(entry.Name()) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			pending = append(pending, queuedCapture{name: entry.Name(), time: info.ModTime()})
		}
	}
	slices.SortStableFunc(pending, func(a, b queuedCapture) int { return a.time.Compare(b.time) })
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		importPendingCapture(ctx, db, dataDir, root, item.name, maxBytes, changed)
	}
	return nil
}

func importPendingCapture(
	ctx context.Context, db database.MediaDBI, dataDir string, root *os.Root,
	name string, maxBytes int64, changed func(string),
) {
	capture, err := root.OpenRoot(name)
	if err != nil {
		log.Warn().Err(err).Str("capture", name).Msg("open queued artwork")
		return
	}
	system, err := importCapture(ctx, db, dataDir, capture, maxBytes)
	_ = capture.Close()
	logger := log.With().Str("capture", name).Logger()
	switch {
	case err == nil:
		changed(system)
		done := ".imported-" + name
		if moveErr := root.Rename(name, done); moveErr != nil {
			logger.Warn().Err(moveErr).Msg("mark artwork capture imported")
			return
		}
		for _, file := range []string{requestName, captureName, ""} {
			if removeErr := root.Remove(filepath.Join(done, file)); removeErr != nil {
				logger.Warn().Err(removeErr).Msg("remove imported artwork staging file")
			}
		}
		logger.Info().Str("system", system).Msg("imported screenshot artwork")
	case errors.Is(err, errGameUnindexed), errors.Is(err, context.Canceled):
		// Retry when the game is indexed or the service restarts.
	case errors.Is(err, errInvalidCapture):
		logger.Warn().Err(err).Msg("rejected screenshot artwork")
		if moveErr := root.Rename(name, ".rejected-"+name); moveErr != nil {
			logger.Warn().Err(moveErr).Msg("quarantine artwork capture")
		}
	default:
		logger.Warn().Err(err).Msg("retrying screenshot artwork import")
	}
}
