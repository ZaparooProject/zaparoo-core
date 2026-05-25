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

package methods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/rs/zerolog/log"
)

const (
	misterMediaImageMaxBytes  = int64(2 * 1024 * 1024)
	defaultMediaImageMaxBytes = int64(8 * 1024 * 1024)
)

// defaultImageTypes is the preference order used when no imageTypes param is provided.
var defaultImageTypes = []string{
	"image", "boxart", "boxart3d", "screenshot", "wheel", "titleshot", "map",
	"marquee", "fanart",
}

var imageTypeTags = map[string]string{
	"image":      tags.PropertyTypeTag(tags.TagPropertyImageImage),
	"boxart":     tags.PropertyTypeTag(tags.TagPropertyImageBoxart),
	"boxart3d":   tags.PropertyTypeTag(tags.TagPropertyImageBoxart3D),
	"boxartside": tags.PropertyTypeTag(tags.TagPropertyImageBoxartSide),
	"boxartback": tags.PropertyTypeTag(tags.TagPropertyImageBoxartBack),
	"screenshot": tags.PropertyTypeTag(tags.TagPropertyImageScreenshot),
	"wheel":      tags.PropertyTypeTag(tags.TagPropertyImageWheel),
	"titleshot":  tags.PropertyTypeTag(tags.TagPropertyImageTitleshot),
	"map":        tags.PropertyTypeTag(tags.TagPropertyImageMap),
	"marquee":    tags.PropertyTypeTag(tags.TagPropertyImageMarquee),
	"fanart":     tags.PropertyTypeTag(tags.TagPropertyImageFanart),
}

type mediaBinaryTooLargeError struct {
	path string
	size int64
	max  int64
}

type mediaImageSource struct {
	propMap map[string]database.MediaProperty
	isMedia bool
}

func (e *mediaBinaryTooLargeError) Error() string {
	return fmt.Sprintf("media binary %q is too large: %d bytes exceeds %d byte limit", e.path, e.size, e.max)
}

// resolveImageTypeTag converts a short image type name to the full property TypeTag.
func resolveImageTypeTag(t string) (string, bool) {
	typeTag, ok := imageTypeTags[t]
	return typeTag, ok
}

// buildPropsMap converts a []database.MediaProperty slice into a map keyed by TypeTag.
func buildPropsMap(props []database.MediaProperty) map[string]database.MediaProperty {
	m := make(map[string]database.MediaProperty, len(props))
	for _, p := range props {
		m[p.TypeTag] = p
	}
	return m
}

// HandleMediaImage returns a single best-match image for a media record as a
// base64-encoded blob. Batch image requests are intentionally unsupported; image
// clients must request one bounded image per JSON-RPC call.
func HandleMediaImage(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	if mediaImageHasItems(env.Params) {
		return nil, models.ClientErrf("media.image batch requests are no longer supported")
	}

	params, err := parseMediaRequest(env.Params, 1)
	if err != nil {
		return nil, err
	}
	if params.Batch {
		return nil, models.ClientErrf("media.image batch requests are no longer supported")
	}

	maxBytes := mediaImageMaxBytes(env.Platform)
	if params.Items[0].MediaID == nil {
		return handleMediaImageSinglePath(&env, params.Items[0], maxBytes)
	}

	resolved, err := resolveMediaRefs(&env, params.Items)
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}

	db := env.Database.MediaDB
	row := resolved[0].Row
	mediaProps, err := db.GetMediaPropertyMetadata(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media property metadata: %w", err)
	}
	titleProps, err := db.GetMediaTitlePropertyMetadata(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title property metadata: %w", err)
	}

	prefs := imagePrefs(params.ImageTypes, params.Items[0].ImageTypes)
	return selectMediaImage(env.Context, db, row, mediaProps, titleProps, prefs, maxBytes)
}

func mediaImageHasItems(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, ok := fields["items"]
	return ok
}

func handleMediaImageSinglePath(env *requests.RequestEnv, ref mediaRefParam, maxBytes int64) (any, error) {
	db := env.Database.MediaDB
	row, err := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if err != nil {
		return nil, err
	}

	mediaProps, err := db.GetMediaPropertyMetadata(env.Context, row.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get media property metadata: %w", err)
	}
	titleProps, err := db.GetMediaTitlePropertyMetadata(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title property metadata: %w", err)
	}

	return selectMediaImage(env.Context, db, row, mediaProps, titleProps, imagePrefs(nil, ref.ImageTypes), maxBytes)
}

func imagePrefs(topLevel, itemLevel []string) []string {
	if len(itemLevel) > 0 {
		return itemLevel
	}
	if len(topLevel) > 0 {
		return topLevel
	}
	return defaultImageTypes
}

func readMediaBinaryFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat media binary file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media binary file %q is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return nil, &mediaBinaryTooLargeError{path: path, size: info.Size(), max: maxBytes}
	}

	f, err := os.Open(path) // #nosec G304 -- path comes from indexed media metadata and is size-checked before read.
	if err != nil {
		return nil, fmt.Errorf("open media binary file %q: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Str("path", path).Msg("media.image: failed to close image file")
		}
	}()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read media binary file %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, &mediaBinaryTooLargeError{path: path, size: int64(len(data)), max: maxBytes}
	}
	return data, nil
}

func mediaImageReadError(path string, err error) error {
	var tooLarge *mediaBinaryTooLargeError
	if errors.As(err, &tooLarge) {
		return models.ClientErrf("media.image: image file too large: %s", tooLarge.Error())
	}
	return fmt.Errorf("media.image: read image file %q: %w", path, err)
}

func mediaImageBlobTooLargeError(prop *database.MediaProperty, maxBytes int64) error {
	return models.ClientErrf(
		"media.image: image blob too large for %s: %d bytes exceeds %d byte limit",
		prop.TypeTag, prop.BlobSize, maxBytes,
	)
}

func selectMediaImage(
	ctx context.Context,
	db database.MediaDBI,
	row *database.MediaFullRow,
	mediaProps []database.MediaProperty,
	titleProps []database.MediaProperty,
	prefs []string,
	maxBytes int64,
) (models.MediaImageResponse, error) {
	mediaMap := buildPropsMap(mediaProps)
	titleMap := buildPropsMap(titleProps)
	sources := []mediaImageSource{
		{mediaMap, true},
		{titleMap, false},
	}

	seen := make(map[string]bool, len(prefs))
	for _, t := range prefs {
		typeTag, ok := resolveImageTypeTag(t)
		if !ok || seen[typeTag] {
			continue
		}
		seen[typeTag] = true

		for _, src := range sources {
			prop, ok := src.propMap[typeTag]
			if !ok {
				continue
			}

			image, stale, err := loadMediaImageProperty(ctx, db, row, &prop, src, typeTag, maxBytes)
			if stale {
				delete(src.propMap, typeTag)
				continue
			}
			if err != nil {
				return models.MediaImageResponse{}, err
			}
			if image != nil {
				return *image, nil
			}
		}
	}

	return models.MediaImageResponse{}, models.ClientErrf(
		"no image found for media: %s/%s", row.System.SystemID, row.Path,
	)
}

func loadMediaImageProperty(
	ctx context.Context,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	src mediaImageSource,
	typeTag string,
	maxBytes int64,
) (*models.MediaImageResponse, bool, error) {
	var binary []byte
	contentType := prop.ContentType

	switch {
	case len(prop.Binary) > 0:
		if int64(len(prop.Binary)) > maxBytes {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		binary = prop.Binary
	case prop.BlobDBID != nil:
		if prop.BlobSize <= 0 {
			return nil, false, models.ClientErrf("media.image: image blob has unknown size for %s", prop.TypeTag)
		}
		if prop.BlobSize > maxBytes {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		var err error
		binary, contentType, err = db.GetMediaBlobDataCapped(ctx, *prop.BlobDBID, maxBytes)
		if err != nil {
			return nil, false, fmt.Errorf("media.image: read image blob %d: %w", *prop.BlobDBID, err)
		}
		if len(binary) == 0 {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
	case prop.Text != "":
		data, stale, err := loadMediaImageFile(ctx, db, row, prop, src.isMedia, typeTag, maxBytes)
		if stale || err != nil {
			return nil, stale, err
		}
		binary = data
	default:
		return nil, false, nil
	}

	return &models.MediaImageResponse{
		Extension:   mediaContentExtension(contentType, prop.Text),
		ContentType: contentType,
		Data:        base64.StdEncoding.EncodeToString(binary),
		TypeTag:     typeTag,
	}, false, nil
}

func loadMediaImageFile(
	ctx context.Context,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	maxBytes int64,
) (data []byte, stale bool, err error) {
	info, err := os.Stat(prop.Text)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			deleteStaleMediaImageProperty(ctx, db, row, prop, isMedia, typeTag)
			return nil, true, nil
		}
		return nil, false, mediaImageReadError(prop.Text, err)
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("media binary file %q is not a regular file", prop.Text)
		return nil, false, mediaImageReadError(prop.Text, err)
	}
	if info.Size() > maxBytes {
		return nil, false, mediaImageReadError(prop.Text, &mediaBinaryTooLargeError{
			path: prop.Text,
			size: info.Size(),
			max:  maxBytes,
		})
	}
	data, readErr := readMediaBinaryFile(prop.Text, maxBytes)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			deleteStaleMediaImageProperty(ctx, db, row, prop, isMedia, typeTag)
			return nil, true, nil
		}
		return nil, false, mediaImageReadError(prop.Text, readErr)
	}
	return data, false, nil
}

func deleteStaleMediaImageProperty(
	ctx context.Context,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
) {
	if !isMedia {
		if delErr := db.DeleteMediaTitleProperty(ctx, row.Title.DBID, prop.TypeTagDBID); delErr != nil {
			log.Warn().Err(delErr).Int64("titleDBID", row.Title.DBID).Str("typeTag", typeTag).
				Msg("media.image: failed to delete stale title property")
		}
		return
	}
	if delErr := db.DeleteMediaProperty(ctx, row.DBID, prop.TypeTagDBID); delErr != nil {
		log.Warn().Err(delErr).Int64("mediaDBID", row.DBID).Str("typeTag", typeTag).
			Msg("media.image: failed to delete stale media property")
	}
}

func mediaImageMaxBytes(pl platforms.Platform) int64 {
	if pl != nil && pl.ID() == ids.Mister {
		return misterMediaImageMaxBytes
	}
	return defaultMediaImageMaxBytes
}
