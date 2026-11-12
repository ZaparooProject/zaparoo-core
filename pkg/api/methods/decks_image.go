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
	"errors"
	"fmt"
	"image/gif"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/decks"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	deckImageTypeCover = "deck:cover"
	deckImageTypeCard  = "deck:card"
	errDeckImageMsg    = "image not found"
)

// deckImageClient fetches deck and card images from their https URLs. It
// refuses a redirect to anything but https, and connects only to public
// addresses: the URLs come from deck and card metadata, which another
// account can write. Each dial is checked, so redirects and DNS answers are
// too. No proxy, so the address checked is the one connected to.
var deckImageClient = &http.Client{
	Timeout:   20 * time.Second,
	Transport: useragent.Transport(publicOnlyTransport()),
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("deck image redirect is not https")
		}
		if len(via) >= 5 {
			return errors.New("deck image redirected too many times")
		}
		return nil
	},
}

// publicOnlyTransport is the default transport with no proxy and a dialer
// that refuses any address that is not public.
func publicOnlyTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := base.Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("deck image address %q: %w", address, err)
			}
			addr, err := netip.ParseAddr(host)
			if err != nil || !publicAddr(addr) {
				return fmt.Errorf("deck image address %s is not public", host)
			}
			return nil
		},
	}
	transport.DialContext = dialer.DialContext
	return transport
}

// sharedAddressSpace is the carrier-grade NAT range (RFC 6598), which
// netip does not count as private.
var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr reports whether an address is a public unicast one: not
// loopback, private, link-local, shared, multicast or unspecified.
func publicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() && !sharedAddressSpace.Contains(addr)
}

// usableImageURL reports whether Core fetches an image URL: https only.
func usableImageURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

// deckCovers is the cover store for the request's platform.
func deckCovers(env *requests.RequestEnv) (*decks.Covers, error) {
	if env.Platform == nil {
		return nil, errors.New("deck covers are not available")
	}
	return decks.NewCovers(env.Platform), nil
}

// deckHasImage reports a cover decks.image can serve for a deck: a stored
// cover file that is still there, or a usable remote image.
func deckHasImage(env *requests.RequestEnv, deck *database.Deck) bool {
	if deck.CoverFile != "" {
		if covers, err := deckCovers(env); err == nil {
			if _, err := covers.Path(deck.CoverFile); err == nil {
				return true
			}
		}
	}
	return usableImageURL(deck.RemoteImageURL())
}

// deckImageSource is the image decks.image serves: a cover file stored here
// or an image fetched from an https URL.
type deckImageSource struct {
	localPath string
	remoteURL string
}

// HandleDecksImage returns a deck's cover, or a card item's art, in the
// media.image result shape.
//
//nolint:gocritic // single-use parameter in API handler
func HandleDecksImage(env requests.RequestEnv) (any, error) {
	var params models.DecksImageParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	localPath := params.Delivery == mediaImageDeliveryPath
	if localPath && (params.MaxSize == nil || *params.MaxSize <= 0) {
		return nil, models.ClientErrf("decks.image: localPath delivery requires a positive maxSize")
	}
	deck, err := loadDeck(&env, params.DeckID)
	if err != nil {
		return nil, err
	}

	source, typeTag, err := deckImageSourceFor(&env, deck, params.ItemID)
	if err != nil {
		return nil, err
	}
	maxSize := 0
	if params.MaxSize != nil {
		maxSize = int(snapThumbMaxSize(*params.MaxSize))
	}
	return serveDeckImage(env.Context, source, typeTag, maxSize, localPath)
}

func deckImageSourceFor(
	env *requests.RequestEnv, deck *database.Deck, itemID *int64,
) (source deckImageSource, typeTag string, err error) {
	if itemID != nil {
		for i := range deck.Items {
			if deck.Items[i].DBID != *itemID {
				continue
			}
			if imageURL := deck.Items[i].CardImageURL(); usableImageURL(imageURL) {
				return deckImageSource{remoteURL: imageURL}, deckImageTypeCard, nil
			}
			return deckImageSource{}, "", models.ClientErrf(errDeckImageMsg)
		}
		return deckImageSource{}, "", models.ClientErr(database.ErrDeckItemNotFound)
	}
	if deck.CoverFile != "" {
		covers, coversErr := deckCovers(env)
		if coversErr != nil {
			return deckImageSource{}, "", coversErr
		}
		path, pathErr := covers.Path(deck.CoverFile)
		if pathErr == nil {
			return deckImageSource{localPath: path}, deckImageTypeCover, nil
		}
		log.Warn().Str("deck", deck.DeckID).Str("file", deck.CoverFile).Msg("deck cover file is missing")
	}
	if imageURL := deck.RemoteImageURL(); usableImageURL(imageURL) {
		return deckImageSource{remoteURL: imageURL}, deckImageTypeCover, nil
	}
	return deckImageSource{}, "", models.ClientErrf(errDeckImageMsg)
}

// serveDeckImage answers with the source at full size, or resized to
// maxSize through the thumbnail cache, the way media.image does.
func serveDeckImage(
	ctx context.Context, source deckImageSource, typeTag string, maxSize int, localPath bool,
) (models.MediaImageResponse, error) {
	cache := mediaThumbCachePointer.Load()
	var sourceKey string
	var load func() ([]byte, string, error)
	if source.localPath != "" {
		info, err := os.Stat(source.localPath)
		if err != nil {
			return models.MediaImageResponse{}, models.ClientErrf(errDeckImageMsg)
		}
		sourceKey = fileThumbSourceKey(source.localPath, info)
		load = func() ([]byte, string, error) {
			data, readErr := readMediaBinaryFile(afero.NewOsFs(), source.localPath, decks.CoverMaxBytes)
			if readErr != nil {
				return nil, "", fmt.Errorf("read deck cover: %w", readErr)
			}
			contentType, typeErr := decks.CoverContentType(data)
			if typeErr != nil {
				return nil, "", models.ClientErrf(errDeckImageMsg)
			}
			return data, contentType, nil
		}
	} else {
		sourceKey = remoteImageSourceKey(source.remoteURL)
		load = func() ([]byte, string, error) {
			return fetchRemoteImage(ctx, cache, source.remoteURL)
		}
	}

	if maxSize > 0 && cache != nil {
		if response, _, found := cachedMediaImageResponse(
			cache, sourceKey, typeTag, maxSize, source.localPath, localPath,
		); found {
			return response, nil
		}
	}
	data, contentType, err := load()
	if err != nil {
		return models.MediaImageResponse{}, err
	}
	if maxSize <= 0 {
		return inlineMediaImageResponse(data, contentType, source.localPath, typeTag), nil
	}
	resizeSem := cache.resizeSemaphore()
	select {
	case resizeSem <- struct{}{}:
	case <-ctx.Done():
		return models.MediaImageResponse{}, ctx.Err()
	}
	data, contentType, _ = resizeImageWithColor(data, contentType, maxSize)
	<-resizeSem
	if cache != nil {
		path, cacheErr := cache.set(sourceKey, maxSize, data, contentType)
		if cacheErr != nil {
			log.Debug().Err(cacheErr).Msg("decks.image: failed to write thumbnail cache file")
		} else if localPath {
			if response, ok := localMediaImageResponse(cache, path, contentType, typeTag); ok {
				return response, nil
			}
		}
	}
	return inlineMediaImageResponse(data, contentType, source.localPath, typeTag), nil
}

// remoteImageSourceKey names a fetched image in the thumbnail cache by URL.
func remoteImageSourceKey(rawURL string) string {
	return "u:" + rawURL
}

// fetchRemoteImage returns an https image, from the thumbnail cache when it
// was fetched before. Clients never fetch these themselves: Core does, once,
// and keeps the image beside its other thumbnails.
func fetchRemoteImage(
	ctx context.Context, cache *mediaThumbCache, rawURL string,
) (data []byte, contentType string, err error) {
	if !usableImageURL(rawURL) {
		return nil, "", models.ClientErrf(errDeckImageMsg)
	}
	sourceKey := remoteImageSourceKey(rawURL)
	if cache != nil {
		if cached, cachedType, found := cache.read(sourceKey, 0); found {
			return cached, cachedType, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, "", models.ClientErrf(errDeckImageMsg)
	}
	resp, err := deckImageClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch deck image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		log.Debug().Int("status", resp.StatusCode).Str("url", rawURL).Msg("deck image fetch failed")
		return nil, "", models.ClientErrf(errDeckImageMsg)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, decks.CoverMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read deck image: %w", err)
	}
	if len(data) > decks.CoverMaxBytes {
		return nil, "", models.ClientErrf("image is larger than 10 MiB")
	}
	contentType, err = decks.CoverContentType(data)
	if err != nil {
		return nil, "", models.ClientErrf(errDeckImageMsg)
	}
	if cache != nil {
		if _, cacheErr := cache.set(sourceKey, 0, data, contentType); cacheErr != nil {
			log.Debug().Err(cacheErr).Msg("decks.image: failed to cache fetched image")
		}
	}
	return data, contentType, nil
}

// deckCoverBytes reads the image a decks.update cover names.
func deckCoverBytes(env *requests.RequestEnv, cover *models.DeckCoverInput) ([]byte, error) {
	switch cover.Kind {
	case models.DeckCoverKindMedia:
		if cover.System == "" || cover.Path == "" {
			return nil, models.ClientErrf("invalid params: a media cover needs system and path")
		}
		if env.Database == nil || env.Database.MediaDB == nil {
			return nil, errors.New("media database is not available")
		}
		ref := mediaRefParam{System: cover.System, Path: cover.Path}
		prefs := imagePrefs(nil, nil)
		raw, err := loadRawMediaImageLimited(env, ref, prefs, mediaImageNoImageRequestKey(ref, prefs), nil)
		if err != nil {
			return nil, err
		}
		return validCoverImage(raw.binary)
	case models.DeckCoverKindFile:
		// A file cover reads any image on the Core host.
		if !isLocalOrAdmin(env) {
			return nil, models.ClientErrf("%w", ErrForbidden)
		}
		if cover.System != "" || cover.Path == "" || !filepath.IsAbs(cover.Path) {
			return nil, models.ClientErrf("invalid params: a file cover needs an absolute path and no system")
		}
		info, err := os.Stat(cover.Path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, models.ClientErrf("cover file not found: %s", cover.Path)
		}
		if info.Size() > decks.CoverMaxBytes {
			return nil, models.ClientErr(decks.ErrCoverTooLarge)
		}
		data, err := readMediaBinaryFile(afero.NewOsFs(), cover.Path, decks.CoverMaxBytes)
		if err != nil {
			return nil, models.ClientErrf("cover file cannot be read: %w", err)
		}
		return validCoverImage(data)
	default:
		return nil, models.ClientErrf("invalid params: unknown cover kind %q", cover.Kind)
	}
}

// validCoverImage accepts bytes that decode as a JPEG, PNG, GIF or WebP
// image of at most 10 MiB.
func validCoverImage(data []byte) ([]byte, error) {
	if len(data) > decks.CoverMaxBytes {
		return nil, models.ClientErr(decks.ErrCoverTooLarge)
	}
	contentType, err := decks.CoverContentType(data)
	if err != nil {
		return nil, models.ClientErr(err)
	}
	if contentType == "image/gif" {
		_, err = gif.Decode(bytes.NewReader(data))
	} else {
		_, err = decodeResizableImage(data, contentType)
	}
	if err != nil {
		return nil, models.ClientErrf("cover does not decode as an image: %w", err)
	}
	return data, nil
}

// pruneDeckCovers removes cover files no deck names any more.
func pruneDeckCovers(env *requests.RequestEnv) {
	covers, err := deckCovers(env)
	if err != nil || env.Database == nil || env.Database.UserDB == nil {
		return
	}
	list, err := env.Database.UserDB.ListDecks()
	if err != nil {
		log.Debug().Err(err).Msg("failed to list decks for cover pruning")
		return
	}
	keep := make(map[string]struct{}, len(list))
	for i := range list {
		if list[i].CoverFile != "" {
			keep[list[i].CoverFile] = struct{}{}
		}
	}
	covers.Prune(keep, time.Now())
}

// trimmedCoverInput normalizes a cover input's text fields.
func trimmedCoverInput(cover *models.DeckCoverInput) *models.DeckCoverInput {
	if cover == nil {
		return nil
	}
	out := *cover
	out.Kind = strings.TrimSpace(out.Kind)
	out.System = strings.TrimSpace(out.System)
	out.Path = strings.TrimSpace(out.Path)
	return &out
}
