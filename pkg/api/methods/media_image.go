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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	misterMediaImageMaxBytes  = int64(2 * 1024 * 1024)
	defaultMediaImageMaxBytes = int64(8 * 1024 * 1024)
	mediaImageNoImageMax      = 4096
)

// mediaImageSem limits concurrent image lookups so high-volume image misses do
// not saturate SQLite and starve browse/status calls.
var mediaImageSem = make(chan struct{}, 1)

// defaultImageTypes is the preference order used when no imageTypes param is provided.
var defaultImageTypes = []string{
	"image", "thumbnail", "boxart", "boxart3d", "screenshot", "wheel", "titleshot", "map",
	"marquee", "fanart",
}

var imageTypeTags = map[string]string{
	"image":      tags.PropertyTypeTag(tags.TagPropertyImageImage),
	"thumbnail":  tags.PropertyTypeTag(tags.TagPropertyImageThumbnail),
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

type mediaImageNotFoundError struct {
	system string
	path   string
}

type mediaImageNoImageCache struct {
	entries map[string]error
	mu      syncutil.Mutex
}

var mediaImageNoImages = &mediaImageNoImageCache{entries: make(map[string]error)}

func (e *mediaBinaryTooLargeError) Error() string {
	return fmt.Sprintf("media binary %q is too large: %d bytes exceeds %d byte limit", e.path, e.size, e.max)
}

func (e *mediaImageNotFoundError) Error() string {
	return fmt.Sprintf("no image found for media: system=%q path=%q", e.system, filepath.ToSlash(e.path))
}

func mediaImageNoImageMediaIDKey(mediaID int64, prefs []string) string {
	return fmt.Sprintf("id:%d\x00%s", mediaID, strings.Join(prefs, "\x00"))
}

func mediaImageNoImagePathKey(system, path string, prefs []string) string {
	return fmt.Sprintf("path:%s\x00%s\x00%s", system, filepath.ToSlash(path), strings.Join(prefs, "\x00"))
}

func mediaImageNoImageRequestKey(ref mediaRefParam, prefs []string) string {
	if ref.MediaID != nil {
		return mediaImageNoImageMediaIDKey(*ref.MediaID, prefs)
	}
	return mediaImageNoImagePathKey(ref.System, ref.Path, prefs)
}

func (c *mediaImageNoImageCache) get(key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	err, ok := c.entries[key]
	return ok, err
}

func (c *mediaImageNoImageCache) add(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= mediaImageNoImageMax {
		for existing := range c.entries {
			delete(c.entries, existing)
			break
		}
	}

	var noImage *mediaImageNotFoundError
	if errors.As(err, &noImage) {
		c.entries[key] = noImage
		return
	}
	c.entries[key] = err
}

func (c *mediaImageNoImageCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]error)
}

func mediaImageNoImageError(row *database.MediaFullRow) error {
	return fmt.Errorf("%w", models.QuietClientErr(&mediaImageNotFoundError{system: row.System.SystemID, path: row.Path}))
}

func cachedMediaImageNoImageError(err error) error {
	return fmt.Errorf("%w", models.QuietClientErr(err))
}

func isMediaImageNoImageError(err error) bool {
	var noImage *mediaImageNotFoundError
	return errors.As(err, &noImage)
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

func imagePropertyTypeTags(props []database.MediaProperty) []string {
	known := make(map[string]struct{}, len(imageTypeTags))
	for _, typeTag := range imageTypeTags {
		known[typeTag] = struct{}{}
	}

	seen := make(map[string]struct{})
	for _, p := range props {
		if _, ok := known[p.TypeTag]; ok {
			seen[p.TypeTag] = struct{}{}
		}
	}

	result := make([]string, 0, len(seen))
	for typeTag := range seen {
		result = append(result, typeTag)
	}
	sort.Strings(result)
	return result
}

// HandleMediaImage returns a single best-match image for a media record as a
// base64-encoded blob.
func HandleMediaImage(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	ref, err := parseMediaImageRequest(env.Params)
	if err != nil {
		return nil, err
	}
	prefs := imagePrefs(nil, ref.ImageTypes)
	noImageKey := mediaImageNoImageRequestKey(ref, prefs)
	if ok, cachedErr := mediaImageNoImages.get(noImageKey); ok {
		return nil, cachedMediaImageNoImageError(cachedErr)
	}

	select {
	case mediaImageSem <- struct{}{}:
		defer func() { <-mediaImageSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}
	if ok, cachedErr := mediaImageNoImages.get(noImageKey); ok {
		return nil, cachedMediaImageNoImageError(cachedErr)
	}

	maxBytes := mediaImageMaxBytes(env.Platform)
	if ref.MediaID == nil {
		return handleMediaImageSinglePath(&env, ref, prefs, noImageKey, maxBytes)
	}

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}

	db := env.Database.MediaDB
	row := resolved[0].Row
	mediaPropSources, err := mediaImagePropSources(&env, row)
	if err != nil {
		return nil, err
	}
	titleProps, err := db.GetMediaTitleProperties(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	image, err := selectMediaImageFromSources(
		env.Context, afero.NewOsFs(), db, row, mediaPropSources, titleProps, prefs, maxBytes,
	)
	if isMediaImageNoImageError(err) {
		mediaImageNoImages.add(noImageKey, err)
	}
	return image, err
}

func parseMediaImageRequest(raw json.RawMessage) (mediaRefParam, error) {
	var ref mediaRefParam
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil {
		return mediaRefParam{}, models.ClientErrf("invalid params: %w", err)
	}
	if err := validateMediaRef(ref); err != nil {
		return mediaRefParam{}, models.ClientErrf("invalid params: %w", err)
	}
	if err := validateImageTypes(ref.ImageTypes); err != nil {
		return mediaRefParam{}, models.ClientErrf("invalid params: %w", err)
	}
	return ref, nil
}

func handleMediaImageSinglePath(
	env *requests.RequestEnv,
	ref mediaRefParam,
	prefs []string,
	noImageKey string,
	maxBytes int64,
) (any, error) {
	db := env.Database.MediaDB
	row, err := resolveMediaBySystemAndPath(env, ref.System, ref.Path)
	if err != nil {
		return nil, err
	}

	mediaPropSources, err := mediaImagePropSources(env, row)
	if err != nil {
		return nil, err
	}
	titleProps, err := db.GetMediaTitleProperties(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("failed to get title properties: %w", err)
	}

	image, err := selectMediaImageFromSources(
		env.Context, afero.NewOsFs(), db, row, mediaPropSources, titleProps, prefs, maxBytes,
	)
	if isMediaImageNoImageError(err) {
		mediaImageNoImages.add(noImageKey, err)
	}
	return image, err
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

func readMediaBinaryFile(fs afero.Fs, path string, maxBytes int64) ([]byte, error) {
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat media binary file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("media binary file %q is not a regular file", path)
	}
	if info.Size() > maxBytes {
		return nil, &mediaBinaryTooLargeError{path: path, size: info.Size(), max: maxBytes}
	}

	data, err := afero.ReadFile(fs, path)
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

func mediaImagePropSources(env *requests.RequestEnv, row *database.MediaFullRow) ([][]database.MediaProperty, error) {
	mediaIDs, err := equivalentMediaIDs(env, row)
	if err != nil {
		return nil, err
	}
	if len(mediaIDs) == 1 {
		props, propErr := env.Database.MediaDB.GetMediaProperties(env.Context, row.DBID)
		if propErr != nil {
			return nil, fmt.Errorf("failed to get media properties: %w", propErr)
		}
		return [][]database.MediaProperty{props}, nil
	}

	propsByID, err := env.Database.MediaDB.GetMediaPropertiesByMediaDBIDs(env.Context, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get media properties: %w", err)
	}
	sources := make([][]database.MediaProperty, 0, len(mediaIDs))
	for _, id := range mediaIDs {
		sources = append(sources, propsByID[id])
	}
	return sources, nil
}

func selectMediaImageFromSources(
	ctx context.Context,
	fs afero.Fs,
	db database.MediaDBI,
	row *database.MediaFullRow,
	mediaPropSources [][]database.MediaProperty,
	titleProps []database.MediaProperty,
	prefs []string,
	maxBytes int64,
) (models.MediaImageResponse, error) {
	sources := make([]mediaImageSource, 0, len(mediaPropSources)+1)
	for _, props := range mediaPropSources {
		sources = append(sources, mediaImageSource{buildPropsMap(props), true})
	}
	sources = append(sources, mediaImageSource{buildPropsMap(titleProps), false})

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

			image, stale, err := loadMediaImageProperty(ctx, fs, db, row, &prop, src, typeTag, maxBytes)
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

	mediaImageProps := make([]string, 0)
	for _, props := range mediaPropSources {
		mediaImageProps = append(mediaImageProps, imagePropertyTypeTags(props)...)
	}
	log.Debug().
		Str("system", row.System.SystemID).
		Str("path", row.Path).
		Int64("mediaDBID", row.DBID).
		Int64("titleDBID", row.Title.DBID).
		Strs("prefs", prefs).
		Strs("mediaImageProps", mediaImageProps).
		Strs("titleImageProps", imagePropertyTypeTags(titleProps)).
		Msg("media.image: no image found")

	return models.MediaImageResponse{}, mediaImageNoImageError(row)
}

func loadMediaImageProperty(
	ctx context.Context,
	fs afero.Fs,
	db database.MediaDBI,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	src mediaImageSource,
	typeTag string,
	maxBytes int64,
) (*models.MediaImageResponse, bool, error) {
	var binary []byte
	contentType := mediaContentType(prop.ContentType, prop.Text)

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
		if errors.Is(err, database.ErrMediaBlobTooLarge) {
			return nil, false, mediaImageBlobTooLargeError(prop, maxBytes)
		}
		if err != nil {
			return nil, false, fmt.Errorf("media.image: read image blob %d: %w", *prop.BlobDBID, err)
		}
		if len(binary) == 0 {
			logStaleMediaImageProperty(row, prop, src.isMedia, typeTag, "empty blob data")
			return nil, true, nil
		}
	case prop.Text != "":
		data, stale, err := loadMediaImageFile(fs, row, prop, src.isMedia, typeTag, maxBytes)
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
	fs afero.Fs,
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	maxBytes int64,
) (data []byte, stale bool, err error) {
	info, err := fs.Stat(prop.Text)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, err.Error())
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
	data, readErr := readMediaBinaryFile(fs, prop.Text, maxBytes)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			logStaleMediaImageProperty(row, prop, isMedia, typeTag, readErr.Error())
			return nil, true, nil
		}
		return nil, false, mediaImageReadError(prop.Text, readErr)
	}
	return data, false, nil
}

func logStaleMediaImageProperty(
	row *database.MediaFullRow,
	prop *database.MediaProperty,
	isMedia bool,
	typeTag string,
	reason string,
) {
	level := "title"
	if isMedia {
		level = "media"
	}
	log.Debug().
		Str("system", row.System.SystemID).
		Str("path", row.Path).
		Int64("mediaDBID", row.DBID).
		Int64("titleDBID", row.Title.DBID).
		Str("typeTag", typeTag).
		Str("text", prop.Text).
		Str("source", level).
		Str("reason", reason).
		Msg("media.image: stale image property ignored")
}

func mediaImageMaxBytes(pl platforms.Platform) int64 {
	if pl != nil && pl.ID() == ids.Mister {
		return misterMediaImageMaxBytes
	}
	return defaultMediaImageMaxBytes
}
