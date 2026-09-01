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

// Package artwork resolves cover art for display readers through the same
// pipeline that serves the media.image API method, so a connected display gets
// the artwork Core already scraped, resized and cached for its other clients.
package artwork

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// imageTypes is the preference order a display asks for. Box art first because
// a display is showing one small portrait image and boxes suit that shape
// better than a screenshot does.
var imageTypes = []string{"boxart", "thumbnail", "image", "titleshot", "screenshot"}

// maxRequestedSize caps the longest edge a caller can ask for. It matches the
// top of Core's thumbnail tier ladder, above which the pipeline would only be
// upscaling, and keeps the int32 conversion below provably in range.
const maxRequestedSize = 768

// Source resolves artwork by delegating to the media.image handler. It holds
// only what that handler reads, and deliberately not the service context, so
// pkg/service can depend on this package without a cycle.
type Source struct {
	platform platforms.Platform
	cfg      *config.Instance
	db       *database.Database
}

// New returns a Source. It is safe to call before any media exists; artwork is
// resolved per request.
func New(pl platforms.Platform, cfg *config.Instance, db *database.Database) *Source {
	return &Source{platform: pl, cfg: cfg, db: db}
}

// Artwork implements readers.ArtworkSource.
//
// It returns readers.ErrNoArtwork when the media is unknown or simply has no
// image, which is an ordinary outcome rather than a failure: plenty of media is
// never scraped.
func (s *Source) Artwork(
	ctx context.Context, systemID, path string, maxSize int,
) (*readers.MediaArtwork, error) {
	if s.db == nil {
		return nil, readers.ErrNoArtwork
	}
	// Guard here rather than letting the handler reject it, so a caller bug
	// cannot be mistaken for media that simply has no artwork.
	if systemID == "" || path == "" {
		return nil, fmt.Errorf("artwork lookup needs a system and a path, got %q and %q", systemID, path)
	}

	if maxSize <= 0 || maxSize > maxRequestedSize {
		maxSize = maxRequestedSize
	}
	requested := int32(maxSize)

	params := models.MediaImageParams{
		System:     systemID,
		Path:       path,
		MaxSize:    &requested,
		ImageTypes: imageTypes,
		// A local path avoids base64-encoding the image only to decode it
		// again in this process. The handler falls back to inline delivery
		// when the thumbnail cache is unavailable, so both are handled below.
		Delivery: methods.MediaImageDeliveryLocalPath,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal media image params: %w", err)
	}

	// The image handler shares a single-slot semaphore with API traffic, so
	// bound the wait the same way an API request would be bounded.
	ctx, cancel := context.WithTimeout(ctx, config.APIRequestTimeout)
	defer cancel()

	result, err := methods.HandleMediaImage(requests.RequestEnv{
		Context:       ctx,
		Platform:      s.platform,
		Config:        s.cfg,
		Database:      s.db,
		LauncherCache: helpers.GlobalLauncherCache,
		Params:        raw,
	})
	if err != nil {
		return nil, s.classify(systemID, path, err)
	}

	response, ok := result.(models.MediaImageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected media image result type %T", result)
	}
	return decode(&response)
}

// classify turns a media.image failure into the error a display driver should
// see.
//
// The handler reports "this media has no image" as a quiet client error, which
// is the ordinary case for unscraped media and stays silent. Everything the
// handler classifies as a plain client error is also treated as no artwork -
// unknown media and an oversized image both land there, and neither is worth
// failing a scene over - but it is logged, so a malformed request from this
// package shows up instead of silently rendering every title coverless.
func (*Source) classify(systemID, path string, err error) error {
	var quietErr *models.QuietClientError
	if errors.As(err, &quietErr) {
		return readers.ErrNoArtwork
	}

	var clientErr *models.ClientError
	if errors.As(err, &clientErr) {
		log.Debug().Err(err).
			Str("system", systemID).
			Str("path", path).
			Msg("artwork: media image request rejected, rendering without a cover")
		return readers.ErrNoArtwork
	}

	return fmt.Errorf("resolve media image: %w", err)
}

func decode(response *models.MediaImageResponse) (*readers.MediaArtwork, error) {
	var data []byte
	switch {
	case response.LocalPath != "":
		read, err := os.ReadFile(response.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("read cached artwork: %w", err)
		}
		data = read
	case response.Data != "":
		decoded, err := base64.StdEncoding.DecodeString(response.Data)
		if err != nil {
			return nil, fmt.Errorf("decode artwork payload: %w", err)
		}
		data = decoded
	default:
		return nil, readers.ErrNoArtwork
	}

	if len(data) == 0 {
		return nil, readers.ErrNoArtwork
	}
	return &readers.MediaArtwork{
		ContentType: response.ContentType,
		TypeTag:     response.TypeTag,
		Data:        data,
	}, nil
}
