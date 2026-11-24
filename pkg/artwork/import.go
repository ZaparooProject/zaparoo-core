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

// MaxCaptureAge is how long a capture waits for its game to be indexed, and
// how long a rejected capture is kept for diagnosis, before it is deleted.
const MaxCaptureAge = 7 * 24 * time.Hour

const (
	importedPrefix = ".imported-"
	rejectedPrefix = ".rejected-"
)

// Importer consumes completed bundles published by Main_MiSTer. A bundle for
// a game that isn't indexed yet waits for the next completed index instead of
// being looked up again on every pass.
type Importer struct {
	DB      database.MediaDBI
	Changed func(system string)
	Now     func() time.Time
	// waiting maps an unmatched capture to the index generation it was last
	// looked up in.
	waiting  map[string]uint64
	Inbox    string
	DataDir  string
	MaxBytes int64
}

// ImportPending runs one import pass with no memory of earlier passes.
func ImportPending(
	ctx context.Context, db database.MediaDBI, inbox, dataDir string, maxBytes int64, changed func(string),
) error {
	importer := &Importer{DB: db, Inbox: inbox, DataDir: dataDir, MaxBytes: maxBytes, Changed: changed}
	return importer.Run(ctx, 0)
}

// Run imports every pending capture. indexGeneration changes whenever an
// index completes, which is the only time an unmatched capture can resolve.
func (im *Importer) Run(ctx context.Context, indexGeneration uint64) error {
	// Stat first: Windows reports listing a file as a directory as "not
	// found", which would make a broken inbox look like no inbox at all.
	info, err := os.Stat(im.Inbox)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat artwork inbox: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("artwork inbox %s is not a directory", im.Inbox)
	}
	entries, err := os.ReadDir(im.Inbox)
	if err != nil {
		return fmt.Errorf("read artwork inbox: %w", err)
	}
	root, err := os.OpenRoot(im.Inbox)
	if err != nil {
		return fmt.Errorf("open artwork inbox: %w", err)
	}
	defer func() { _ = root.Close() }()
	now := time.Now()
	if im.Now != nil {
		now = im.Now()
	}
	if im.waiting == nil {
		im.waiting = make(map[string]uint64)
	}

	type queuedCapture struct {
		time time.Time
		name string
	}
	pending := make([]queuedCapture, 0, len(entries))
	present := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		switch {
		case pendingName(name):
			present[name] = struct{}{}
			pending = append(pending, queuedCapture{name: name, time: info.ModTime()})
		case strings.HasPrefix(name, importedPrefix):
			// A staging folder whose cleanup failed on an earlier pass.
			removeBundle(root, name, "remove imported artwork staging folder")
		case strings.HasPrefix(name, rejectedPrefix) && now.Sub(info.ModTime()) > MaxCaptureAge:
			removeBundle(root, name, "remove rejected artwork capture")
		}
	}
	for name := range im.waiting {
		if _, ok := present[name]; !ok {
			delete(im.waiting, name)
		}
	}
	slices.SortStableFunc(pending, func(a, b queuedCapture) int { return a.time.Compare(b.time) })
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		expired := now.Sub(item.time) > MaxCaptureAge
		if generation, ok := im.waiting[item.name]; ok && generation == indexGeneration && !expired {
			continue
		}
		if im.importPendingCapture(ctx, root, item.name) {
			delete(im.waiting, item.name)
			continue
		}
		if expired {
			log.Info().Str("capture", item.name).Msg("dropping screenshot artwork for a game that was never indexed")
			removeBundle(root, item.name, "remove expired artwork capture")
			delete(im.waiting, item.name)
			continue
		}
		im.waiting[item.name] = indexGeneration
	}
	return nil
}

func removeBundle(root *os.Root, name, action string) {
	if err := root.RemoveAll(name); err != nil {
		log.Warn().Err(err).Str("capture", name).Msg(action)
	}
}

// importPendingCapture reports false only when the capture is still waiting
// for its game to be indexed.
func (im *Importer) importPendingCapture(ctx context.Context, root *os.Root, name string) bool {
	logger := log.With().Str("capture", name).Logger()
	capture, err := root.OpenRoot(name)
	if err != nil {
		logger.Warn().Err(err).Msg("open queued artwork")
		return true
	}
	system, err := importCapture(ctx, im.DB, im.DataDir, capture, im.MaxBytes)
	_ = capture.Close()
	switch {
	case err == nil:
		im.Changed(system)
		done := importedPrefix + name
		if moveErr := root.Rename(name, done); moveErr != nil {
			logger.Warn().Err(moveErr).Msg("mark artwork capture imported")
			// Never import the same capture twice.
			removeBundle(root, name, "remove imported artwork capture")
			return true
		}
		removeBundle(root, done, "remove imported artwork staging folder")
		logger.Info().Str("system", system).Msg("imported screenshot artwork")
	case errors.Is(err, errGameUnindexed):
		return false
	case errors.Is(err, context.Canceled):
		// Retry when the service restarts.
	case errors.Is(err, errInvalidCapture):
		logger.Warn().Err(err).Msg("rejected screenshot artwork")
		if moveErr := root.Rename(name, rejectedPrefix+name); moveErr != nil {
			logger.Warn().Err(moveErr).Msg("quarantine artwork capture")
		}
	default:
		logger.Warn().Err(err).Msg("retrying screenshot artwork import")
	}
	return true
}
