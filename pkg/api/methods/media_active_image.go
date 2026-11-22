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
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/rs/zerolog/log"
)

// Type tags media.active.image reports for where the art came from.
const (
	activeImageTypeEmbedded = "audio:" + trackmeta.SourceEmbedded
	activeImageTypeFolder   = "audio:" + trackmeta.SourceFolder
	// activeImageTypeCover is a cover declared by the token that launched
	// the media, such as an external drive's zaparoo.json.
	activeImageTypeCover = "token:cover"
)

var (
	errNoActiveAudioTrack = errors.New("no active audio track")
	errNoCoverArt         = errors.New("no cover art")
	errCoverArtTooLarge   = errors.New("cover art is too large")
	// errUnsupportedCoverArt is a declared cover that is not a PNG or JPEG.
	errUnsupportedCoverArt = errors.New("cover art is not a PNG or JPEG image")
)

// readTrackPicture is trackmeta.ReadPicture, replaced by tests.
//
//nolint:gochecknoglobals // swapped only by tests
var readTrackPicture = trackmeta.ReadPicture

// HandleMediaActiveImage returns the cover art of the audio track playing in
// a slot: the picture embedded in its tags, else a cover image beside it. The
// track is always the one the slot is playing; a client never names a path.
// maxSize and delivery work as they do for media.image.
//
//nolint:gocritic // RequestEnv is copied once at the API handler boundary.
func HandleMediaActiveImage(env requests.RequestEnv) (any, error) {
	var params models.MediaActiveImageParams
	if len(bytes.TrimSpace(env.Params)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(env.Params))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
		if err := validation.DefaultValidator.Validate(&params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}
	slot, err := mediaslot.Normalize(params.Slot)
	if err != nil {
		return nil, models.ClientErrf("invalid slot: %w", err)
	}
	deliveryRef := mediaRefParam{MaxSize: params.MaxSize}
	if deliveryErr := validateMediaImageDelivery(params.Delivery, deliveryRef); deliveryErr != nil {
		return nil, deliveryErr
	}
	localPath := params.Delivery == mediaImageDeliveryPath

	maxSize := 0
	var cache *mediaThumbCache
	if params.MaxSize != nil {
		maxSize = int(snapThumbMaxSize(*params.MaxSize))
		cache = mediaThumbCachePointer.Load()
	}

	if coverPath := activeMediaCoverPath(&env, slot); coverPath != "" {
		return serveActiveMediaCover(&env, coverPath, maxSize, cache, localPath)
	}

	trackPath, ok := activeAudioTrackPath(&env, slot)
	if !ok {
		return nil, models.ClientErrf("%w", errNoActiveAudioTrack)
	}

	trackInfo, err := os.Stat(trackPath)
	if err != nil {
		return nil, models.ClientErrf("%w", errNoActiveAudioTrack)
	}
	trackKey := fileThumbSourceKey(trackPath, trackInfo)

	// A resized copy made before is served without reading the tags again.
	if cache != nil {
		for _, candidate := range activeImageCacheCandidates(trackPath, trackKey) {
			if response, _, found := cachedMediaImageResponse(
				cache, candidate.sourceKey, candidate.typeTag, maxSize, candidate.sourcePath, localPath,
			); found {
				return response, nil
			}
		}
	}

	pic, err := readTrackPicture(trackPath, MediaImageMaxBytes(env.Platform))
	switch {
	case errors.Is(err, trackmeta.ErrNoArt):
		return nil, models.ClientErrf("%w", errNoCoverArt)
	case errors.Is(err, trackmeta.ErrTagsTooLarge):
		return nil, models.ClientErrf("%w", errCoverArtTooLarge)
	case err != nil:
		return nil, fmt.Errorf("read cover art: %w", err)
	}

	typeTag := activeImageTypeEmbedded
	sourceKey := activeImageSourceKey(trackmeta.SourceEmbedded, trackKey, "")
	sourcePath := ""
	if pic.Source == trackmeta.SourceFolder {
		typeTag = activeImageTypeFolder
		sourcePath = pic.Path
		artInfo, statErr := os.Stat(pic.Path)
		if statErr != nil {
			return nil, models.ClientErrf("%w", errNoCoverArt)
		}
		sourceKey = activeImageSourceKey(trackmeta.SourceFolder, trackKey, fileThumbSourceKey(pic.Path, artInfo))
	}

	return deliverActiveImage(&env, &activeImage{
		data:        pic.Data,
		contentType: pic.MIMEType,
		sourceKey:   sourceKey,
		sourcePath:  sourcePath,
		typeTag:     typeTag,
	}, maxSize, cache, localPath)
}

// activeImage is art found for the active media, ready to deliver.
type activeImage struct {
	contentType string
	sourceKey   string
	sourcePath  string
	typeTag     string
	data        []byte
}

// deliverActiveImage resizes img when maxSize asks for it, caches the result
// and returns it inline or as a cached local path.
func deliverActiveImage(
	env *requests.RequestEnv, img *activeImage, maxSize int, cache *mediaThumbCache, localPath bool,
) (any, error) {
	binary, contentType := img.data, img.contentType
	if maxSize > 0 {
		resizeSem := cache.resizeSemaphore()
		select {
		case resizeSem <- struct{}{}:
		case <-env.Context.Done():
			return nil, fmt.Errorf("wait for image resize: %w", env.Context.Err())
		}
		binary, contentType, _ = resizeImageWithColor(binary, contentType, maxSize)
		<-resizeSem
		if cache != nil {
			path, cacheErr := cache.set(img.sourceKey, maxSize, binary, contentType)
			if cacheErr != nil {
				log.Debug().Err(cacheErr).Msg("media.active.image: failed to cache thumbnail")
			} else if localPath {
				if response, served := localMediaImageResponse(cache, path, contentType, img.typeTag); served {
					return response, nil
				}
			}
		}
	}
	return inlineMediaImageResponse(binary, contentType, img.sourcePath, img.typeTag), nil
}

// activeMediaCoverPath returns the cover the launching token declared for
// the primary media, or "" when it declared none.
func activeMediaCoverPath(env *requests.RequestEnv, slot string) string {
	if slot != mediaslot.Primary || env.State == nil {
		return ""
	}
	media := env.State.ActiveMedia()
	if media == nil {
		return ""
	}
	return media.ImagePath
}

// activeCoverSourceKey names a declared cover in the thumbnail cache. It
// changes whenever the file does.
func activeCoverSourceKey(coverKey string) string {
	return "c:" + activeImageTypeCover + ":" + coverKey
}

// serveActiveMediaCover delivers the cover a token declared for the active
// media. The file was checked when the token was read; it is checked again
// here because the drive holding it can change, and only a PNG or JPEG
// within the size and dimension limits is served.
func serveActiveMediaCover(
	env *requests.RequestEnv, coverPath string, maxSize int, cache *mediaThumbCache, localPath bool,
) (any, error) {
	info, err := os.Lstat(coverPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, models.ClientErrf("%w", errNoCoverArt)
	}
	sourceKey := activeCoverSourceKey(fileThumbSourceKey(coverPath, info))
	if cache != nil {
		if response, _, found := cachedMediaImageResponse(
			cache, sourceKey, activeImageTypeCover, maxSize, coverPath, localPath,
		); found {
			return response, nil
		}
	}

	data, err := readActiveCover(coverPath, info, MediaImageMaxBytes(env.Platform))
	if err != nil {
		return nil, err
	}
	contentType, err := activeCoverContentType(data)
	if err != nil {
		return nil, err
	}
	return deliverActiveImage(env, &activeImage{
		data:        data,
		contentType: contentType,
		sourceKey:   sourceKey,
		sourcePath:  coverPath,
		typeTag:     activeImageTypeCover,
	}, maxSize, cache, localPath)
}

// readActiveCover reads coverPath, refusing it when the file opened is not
// the one info describes, as when it was swapped for a link after the check.
func readActiveCover(coverPath string, info os.FileInfo, maxBytes int64) ([]byte, error) {
	//nolint:gosec // the path was validated against the token's root when read
	file, err := os.Open(coverPath)
	if err != nil {
		return nil, models.ClientErrf("%w", errNoCoverArt)
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, models.ClientErrf("%w", errNoCoverArt)
	}
	data, err := readOpenedMediaFile(file, opened, maxBytes)
	var tooLarge *mediaBinaryTooLargeError
	if errors.As(err, &tooLarge) {
		return nil, models.ClientErrf("%w", errCoverArtTooLarge)
	}
	if err != nil {
		return nil, fmt.Errorf("read cover art: %w", err)
	}
	return data, nil
}

// activeCoverContentType identifies a declared cover as PNG or JPEG from its
// bytes and checks the frame it declares before anything decodes it.
func activeCoverContentType(data []byte) (string, error) {
	var contentType string
	var decodeConfig func(io.Reader) (image.Config, error)
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		contentType, decodeConfig = "image/png", png.DecodeConfig
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		contentType, decodeConfig = "image/jpeg", jpeg.DecodeConfig
	default:
		return "", models.ClientErrf("%w", errUnsupportedCoverArt)
	}
	cfg, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", models.ClientErrf("%w", errUnsupportedCoverArt)
	}
	if int64(cfg.Width)*int64(cfg.Height) > mediaImageMaxPixels {
		return "", models.ClientErrf("%w", errCoverArtTooLarge)
	}
	return contentType, nil
}

// activeAudioTrackPath returns the path of the track native audio is
// playing in slot.
func activeAudioTrackPath(env *requests.RequestEnv, slot string) (string, bool) {
	if env.State == nil || env.PlaybackManager == nil {
		return "", false
	}
	media := env.State.ActiveMedia()
	if slot == mediaslot.Background {
		media = env.State.BackgroundMedia()
	}
	if media == nil || media.LauncherID != platforms.NativeAudioLauncherID {
		return "", false
	}
	// Published media and playback state are updated separately, so while a
	// track is being replaced they can name different tracks. Only a path
	// that matches the published media is that track's.
	path := env.PlaybackManager.State(slot).Path
	if path == "" || path != media.Path {
		return "", false
	}
	return path, true
}

// activeImageSourceKey names a track's art in the thumbnail cache. Embedded
// art changes with the track file; folder art changes with either file.
func activeImageSourceKey(source, trackKey, artKey string) string {
	return "a:" + systemdefs.SystemAudio + ":" + source + ":" + trackKey + "|" + artKey
}

type activeImageCandidate struct {
	sourceKey  string
	typeTag    string
	sourcePath string
}

// activeImageCacheCandidates lists the cache entries a track's art could be
// under, best first: embedded art, then the image beside the track.
func activeImageCacheCandidates(trackPath, trackKey string) []activeImageCandidate {
	out := []activeImageCandidate{{
		sourceKey: activeImageSourceKey(trackmeta.SourceEmbedded, trackKey, ""),
		typeTag:   activeImageTypeEmbedded,
	}}
	if artPath, ok := trackmeta.FolderArtPath(trackPath); ok {
		if info, err := os.Stat(artPath); err == nil {
			out = append(out, activeImageCandidate{
				sourceKey:  activeImageSourceKey(trackmeta.SourceFolder, trackKey, fileThumbSourceKey(artPath, info)),
				typeTag:    activeImageTypeFolder,
				sourcePath: artPath,
			})
		}
	}
	return out
}
