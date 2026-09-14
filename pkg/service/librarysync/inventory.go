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

package librarysync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

const (
	inventoryPageSize      = 500
	inventorySchemaVersion = 1
	// maxInventoryItems and maxInventoryBytes are the largest inventory the
	// account accepts.
	maxInventoryItems = 1_000_000
	maxInventoryBytes = 8 << 20
	// defaultResolvePace spaces resolve requests so a first sync of a large
	// library stays inside the per-address request budget the devices of
	// one household share.
	defaultResolvePace = time.Second
	// rejectedRetryAfter is how long a rejected fingerprint is trusted
	// before it is offered to the account again.
	rejectedRetryAfter = 7 * 24 * time.Hour
	// inventoryConfirmInterval is how often an unchanged index is checked
	// against what the account holds.
	inventoryConfirmInterval = time.Hour
)

// syncedMediaTypes are the media types whose files go into the inventory.
var syncedMediaTypes = map[slugs.MediaType]bool{
	slugs.MediaTypeGame: true,
}

// Inventory pass outcomes.
const (
	InventorySkipped   = "skipped"
	InventoryConfirmed = "confirmed"
	InventoryUploaded  = "uploaded"
	InventoryTooLarge  = "too_large"
)

var errUnknownOrdinal = errors.New("inventory names an ordinal the account never issued")

// InventoryResult summarizes one inventory pass.
type InventoryResult struct {
	Outcome    string
	Generation int64
	ItemCount  int
	Resolved   int
	Rejected   int
}

// SyncInventory brings the account's copy of this device's inventory up to
// date with the current index. An index generation already committed is only
// checked against the account once an hour; force checks it now.
func (s *Service) SyncInventory(ctx context.Context, force bool) (InventoryResult, error) {
	s.inventoryMu.Lock()
	defer s.inventoryMu.Unlock()

	if !s.cfg.LibrarySyncEnabled() {
		return InventoryResult{}, ErrDisabled
	}
	client, err := s.client()
	if err != nil {
		return InventoryResult{}, err
	}
	mediaDB := s.db.MediaDB
	if !mediaDBSettled(mediaDB) {
		return InventoryResult{}, ErrNotSettled
	}
	generation, err := mediaDB.IndexGeneration()
	if err != nil {
		return InventoryResult{}, fmt.Errorf("read index generation: %w", err)
	}
	if generation == 0 {
		// Nothing has been indexed, so there is no library to describe yet.
		return InventoryResult{Outcome: InventorySkipped}, nil
	}

	state, err := mediaDB.GetLibraryInventoryState(ctx)
	if err != nil {
		return InventoryResult{}, fmt.Errorf("read library inventory state: %w", err)
	}
	if state.Endpoint != client.BaseURL() {
		// Ordinals belong to the server that issued them.
		if clearErr := mediaDB.ClearLibraryOrdinalCache(ctx); clearErr != nil {
			return InventoryResult{}, fmt.Errorf("clear library ordinal cache: %w", clearErr)
		}
		state = database.LibraryInventoryState{Endpoint: client.BaseURL()}
	}
	if state.Credential != client.CredentialTag() {
		// Linked again since the last commit: the account side starts with
		// no inventory for this device, so the record proves nothing.
		state.Credential = client.CredentialTag()
		state.ConfirmedAt = time.Time{}
	}

	result := InventoryResult{Generation: generation, ItemCount: state.ItemCount}
	if state.Generation == generation {
		if state.TooLarge {
			result.Outcome = InventoryTooLarge
			return result, nil
		}
		if state.SHA256 != "" {
			if !force && !state.ConfirmedAt.IsZero() && s.now().Sub(state.ConfirmedAt) < inventoryConfirmInterval {
				result.Outcome = InventorySkipped
				return result, nil
			}
			held, holds, getErr := accountInventory(ctx, client)
			if getErr != nil {
				return InventoryResult{}, getErr
			}
			if holds && held.ContentSHA256 == state.SHA256 {
				state.ConfirmedAt = s.now()
				if setErr := mediaDB.SetLibraryInventoryState(ctx, &state); setErr != nil {
					return InventoryResult{}, fmt.Errorf("store library inventory state: %w", setErr)
				}
				result.Outcome = InventoryConfirmed
				return result, nil
			}
		}
	}

	result, err = s.buildAndCommit(ctx, client, &state, generation)
	if errors.Is(err, errUnknownOrdinal) {
		log.Warn().Msg("library inventory held an ordinal the account does not know; resolving the library again")
		if clearErr := mediaDB.ClearLibraryOrdinalCache(ctx); clearErr != nil {
			return InventoryResult{}, fmt.Errorf("clear library ordinal cache: %w", clearErr)
		}
		result, err = s.buildAndCommit(ctx, client, &state, generation)
	}
	return result, err
}

func (s *Service) buildAndCommit(
	ctx context.Context,
	client *backup.OnlineClient,
	state *database.LibraryInventoryState,
	generation int64,
) (InventoryResult, error) {
	mediaDB := s.db.MediaDB
	bitmap, result, err := s.buildInventory(ctx, client, generation)
	if err != nil {
		return InventoryResult{}, err
	}
	result.Generation = generation
	// The index must not have moved under the walk, or the bitmap describes
	// no generation at all.
	after, err := mediaDB.IndexGeneration()
	if err != nil {
		return InventoryResult{}, fmt.Errorf("read index generation: %w", err)
	}
	if after != generation || !mediaDBSettled(mediaDB) {
		return InventoryResult{}, ErrNotSettled
	}

	bitmap.RunOptimize()
	body, err := bitmap.MarshalBinary()
	if err != nil {
		return InventoryResult{}, fmt.Errorf("encode library inventory: %w", err)
	}
	cardinality := bitmap.GetCardinality()
	if cardinality > maxInventoryItems || len(body) > maxInventoryBytes {
		result.ItemCount = int(min(cardinality, math.MaxInt32))
		return s.recordTooLarge(ctx, state, &result)
	}
	result.ItemCount = int(cardinality)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	// The walk can take minutes on a first sync; never upload for a user who
	// turned sync off meanwhile.
	if !s.cfg.LibrarySyncEnabled() {
		return InventoryResult{}, ErrDisabled
	}
	if err = putInventory(ctx, client, digest, body, generation, result.ItemCount); err != nil {
		if apiErr, ok := backup.AsAPIError(err); ok {
			switch {
			case apiErr.Code == codeUnknownOrdinal:
				return InventoryResult{}, errUnknownOrdinal
			case apiErr.Code == codeInventoryTooLarge, apiErr.Code == codePayloadTooLarge,
				apiErr.Status == http.StatusRequestEntityTooLarge:
				return s.recordTooLarge(ctx, state, &result)
			}
		}
		return InventoryResult{}, err
	}

	now := s.now()
	*state = database.LibraryInventoryState{
		Endpoint:    state.Endpoint,
		Credential:  state.Credential,
		SHA256:      digest,
		Generation:  generation,
		ItemCount:   result.ItemCount,
		CommittedAt: now,
		ConfirmedAt: now,
	}
	if err := mediaDB.SetLibraryInventoryState(ctx, state); err != nil {
		return InventoryResult{}, fmt.Errorf("store library inventory state: %w", err)
	}
	if removed, pruneErr := mediaDB.PruneLibraryOrdinals(ctx, generation); pruneErr != nil {
		log.Debug().Err(pruneErr).Msg("failed to prune library ordinal cache")
	} else if removed > 0 {
		log.Debug().Int64("removed", removed).Msg("pruned library ordinal cache")
	}
	result.Outcome = InventoryUploaded
	log.Info().Int("items", result.ItemCount).Int("bytes", len(body)).Int64("generation", generation).
		Int("resolved", result.Resolved).Int("rejected", result.Rejected).
		Msg("library inventory committed")
	return result, nil
}

// buildInventory walks every present file of a synced media type, resolving
// fingerprints the cache has no usable answer for, and returns the bitmap of
// their ordinals.
func (s *Service) buildInventory(
	ctx context.Context,
	client *backup.OnlineClient,
	generation int64,
) (*roaring.Bitmap, InventoryResult, error) {
	mediaDB := s.db.MediaDB
	bitmap := roaring.New()
	var result InventoryResult
	var lastResolve time.Time
	after := int64(0)
	for {
		if err := s.pauser.Wait(ctx); err != nil {
			return nil, result, fmt.Errorf("library inventory walk: %w", err)
		}
		page, err := mediaDB.LibraryMediaPage(ctx, after, inventoryPageSize)
		if err != nil {
			return nil, result, fmt.Errorf("read library media page: %w", err)
		}
		if len(page) == 0 {
			return bitmap, result, nil
		}
		after = page[len(page)-1].MediaDBID

		identities, err := pageIdentities(ctx, mediaDB, page)
		if err != nil {
			return nil, result, err
		}
		if len(identities) == 0 {
			continue
		}
		pending, err := s.addCachedOrdinals(ctx, bitmap, identities, generation)
		if err != nil {
			return nil, result, err
		}
		if len(pending) == 0 {
			continue
		}
		if waitErr := s.waitResolvePace(ctx, lastResolve); waitErr != nil {
			return nil, result, waitErr
		}
		lastResolve = s.now()
		answers, err := resolveIdentities(ctx, client, pending, s.now(), generation)
		if err != nil {
			return nil, result, err
		}
		if err := mediaDB.PutLibraryOrdinals(ctx, answers); err != nil {
			return nil, result, fmt.Errorf("store library ordinals: %w", err)
		}
		for i := range answers {
			if answers[i].Ordinal > 0 {
				bitmap.Add(answers[i].Ordinal)
				result.Resolved++
			} else {
				result.Rejected++
			}
		}
	}
}

// pageIdentities builds the identity observation of each file in a page
// whose system holds a synced media type, keyed by fingerprint.
func pageIdentities(
	ctx context.Context, mediaDB database.MediaDBI, page []database.LibraryMediaRow,
) (map[string]*database.MediaIdentity, error) {
	kept := make([]database.LibraryMediaRow, 0, len(page))
	ids := make([]int64, 0, len(page))
	mediaTypes := make(map[string]slugs.MediaType)
	for i := range page {
		mediaType, ok := mediaTypes[page[i].SystemID]
		if !ok {
			system, err := systemdefs.GetSystem(page[i].SystemID)
			if err != nil {
				continue
			}
			mediaType = system.GetMediaType()
			mediaTypes[page[i].SystemID] = mediaType
		}
		if !syncedMediaTypes[mediaType] {
			continue
		}
		kept = append(kept, page[i])
		ids = append(ids, page[i].MediaDBID)
	}
	identities := make(map[string]*database.MediaIdentity, len(kept))
	if len(kept) == 0 {
		return identities, nil
	}
	tags, err := mediaDB.GetMediaTagsByMediaDBIDs(context.WithoutCancel(ctx), ids)
	if err != nil {
		return nil, fmt.Errorf("read library media tags: %w", err)
	}
	for i := range kept {
		row := &kept[i]
		identity, err := database.BuildMediaIdentity(
			mediaTypes[row.SystemID], row.SystemID, row.Name, row.Slug, tags[row.MediaDBID],
		)
		if err != nil {
			// A title whose name slugifies to nothing cannot name a game.
			continue
		}
		if _, seen := identities[identity.ObservationFingerprint]; !seen {
			identities[identity.ObservationFingerprint] = &identity
		}
	}
	return identities, nil
}

// addCachedOrdinals adds every usable cached answer to the bitmap, records
// the cached fingerprints as met by this generation, and returns the
// identities still to resolve.
func (s *Service) addCachedOrdinals(
	ctx context.Context,
	bitmap *roaring.Bitmap,
	identities map[string]*database.MediaIdentity,
	generation int64,
) ([]*database.MediaIdentity, error) {
	mediaDB := s.db.MediaDB
	fingerprints := make([]string, 0, len(identities))
	for fingerprint := range identities {
		fingerprints = append(fingerprints, fingerprint)
	}
	cached, err := mediaDB.GetLibraryOrdinals(ctx, fingerprints)
	if err != nil {
		return nil, fmt.Errorf("read library ordinal cache: %w", err)
	}
	now := s.now()
	seen := make([]string, 0, len(cached))
	pending := make([]*database.MediaIdentity, 0, len(identities)-len(cached))
	for fingerprint, identity := range identities {
		entry, ok := cached[fingerprint]
		switch {
		case !ok:
			pending = append(pending, identity)
		case entry.Ordinal > 0:
			bitmap.Add(entry.Ordinal)
			seen = append(seen, fingerprint)
		case now.Sub(entry.ResolvedAt) >= rejectedRetryAfter:
			pending = append(pending, identity)
		default:
			seen = append(seen, fingerprint)
		}
	}
	if err := mediaDB.MarkLibraryOrdinalsSeen(ctx, seen, generation); err != nil {
		return nil, fmt.Errorf("mark library ordinals seen: %w", err)
	}
	return pending, nil
}

func (s *Service) waitResolvePace(ctx context.Context, last time.Time) error {
	if last.IsZero() {
		return nil
	}
	wait := s.resolvePace - s.now().Sub(last)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("library inventory walk: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// resolveIdentities asks the account for the ordinal of each identity and
// returns the answers to cache. An item the response does not answer, or
// answers with an ordinal out of range, is left out so a later walk asks
// again.
func resolveIdentities(
	ctx context.Context,
	client *backup.OnlineClient,
	identities []*database.MediaIdentity,
	now time.Time,
	generation int64,
) ([]database.LibraryOrdinal, error) {
	request := resolveRequest{Items: make([]resolveItem, len(identities))}
	for i := range identities {
		request.Items[i] = resolveItem{MediaIdentity: identities[i]}
	}
	var response resolveResponse
	err := client.RetryRateLimited(ctx, func() error {
		response = resolveResponse{}
		return client.DoJSON(ctx, http.MethodPost, pathResolve, &request, &response)
	})
	if err != nil {
		return nil, fmt.Errorf("resolve library media: %w", err)
	}
	return resolveAnswers(identities, &response, now, generation), nil
}

// resolveAnswers turns a resolve response into cache entries, keeping the
// first well-formed answer for each requested item.
func resolveAnswers(
	identities []*database.MediaIdentity,
	response *resolveResponse,
	now time.Time,
	generation int64,
) []database.LibraryOrdinal {
	answers := make([]database.LibraryOrdinal, 0, len(response.Items))
	answered := make(map[int]bool, len(response.Items))
	for _, item := range response.Items {
		if item.Index < 0 || item.Index >= len(identities) || answered[item.Index] {
			continue
		}
		answer := database.LibraryOrdinal{
			Fingerprint:    identities[item.Index].ObservationFingerprint,
			ResolvedAt:     now,
			SeenGeneration: generation,
		}
		switch item.Status {
		case resolveStatusResolved:
			if item.Ordinal == nil || *item.Ordinal < 1 || *item.Ordinal > math.MaxInt32 {
				continue
			}
			answer.Ordinal = uint32(*item.Ordinal)
		case resolveStatusRejected:
			answer.Code = item.Code
		default:
			continue
		}
		answered[item.Index] = true
		answers = append(answers, answer)
	}
	return answers
}

// accountInventory returns what the account holds for this device, and
// whether it holds anything.
func accountInventory(ctx context.Context, client *backup.OnlineClient) (inventoryResponse, bool, error) {
	var held inventoryResponse
	err := client.RetryRateLimited(ctx, func() error {
		held = inventoryResponse{}
		return client.DoJSON(ctx, http.MethodGet, pathInventory, nil, &held)
	})
	if apiErr, ok := backup.AsAPIError(err); ok && apiErr.Status == http.StatusNotFound {
		return inventoryResponse{}, false, nil
	}
	if err != nil {
		return inventoryResponse{}, false, fmt.Errorf("read library inventory: %w", err)
	}
	return held, true, nil
}

// putInventory commits the bitmap. An upload the account reports as
// superseded carries an older generation than the one it holds, which only
// happens when this device's generation counter restarted, so the held
// inventory is dropped and the upload repeated once.
func putInventory(
	ctx context.Context,
	client *backup.OnlineClient,
	digest string,
	body []byte,
	generation int64,
	itemCount int,
) error {
	headers := http.Header{}
	headers.Set(headerGeneration, strconv.FormatInt(generation, 10))
	headers.Set(headerSchemaVersion, strconv.Itoa(inventorySchemaVersion))
	headers.Set(headerItemCount, strconv.Itoa(itemCount))
	path := pathInventory + "/" + digest

	put := func() (*inventoryResponse, error) {
		var committed inventoryResponse
		err := client.RetryRateLimited(ctx, func() error {
			committed = inventoryResponse{}
			return client.DoBytes(ctx, http.MethodPut, path, body, headers, &committed)
		})
		if err != nil {
			return nil, fmt.Errorf("commit library inventory: %w", err)
		}
		return &committed, nil
	}

	committed, err := put()
	if err != nil {
		return err
	}
	if !committed.Superseded {
		return nil
	}
	log.Warn().Int64("generation", generation).Int64("held_generation", committed.IndexGeneration).
		Msg("library inventory superseded by an older index counter; replacing it")
	if dropErr := dropInventory(ctx, client); dropErr != nil {
		return dropErr
	}
	again, err := put()
	if err != nil {
		return err
	}
	if again.Superseded {
		return errors.New("library inventory still superseded after replacing it")
	}
	return nil
}

func (s *Service) recordTooLarge(
	ctx context.Context, state *database.LibraryInventoryState, result *InventoryResult,
) (InventoryResult, error) {
	*state = database.LibraryInventoryState{
		Endpoint:   state.Endpoint,
		Credential: state.Credential,
		Generation: result.Generation,
		ItemCount:  result.ItemCount,
		TooLarge:   true,
	}
	if err := s.db.MediaDB.SetLibraryInventoryState(ctx, state); err != nil {
		return InventoryResult{}, fmt.Errorf("store library inventory state: %w", err)
	}
	log.Warn().Int("items", result.ItemCount).Msg("library inventory is larger than the account accepts")
	if s.inbox != nil {
		if err := s.inbox.Add(
			"Library too large to sync",
			inbox.WithBody(fmt.Sprintf(
				"This device holds %d games, more than Zaparoo Online can list for one device. "+
					"Favorites and decks still sync.", result.ItemCount,
			)),
			inbox.WithSeverity(inbox.SeverityWarning),
			inbox.WithCategory(inbox.CategoryLibraryInventoryTooLarge),
		); err != nil {
			log.Warn().Err(err).Msg("failed to add library inventory inbox message")
		}
	}
	result.Outcome = InventoryTooLarge
	return *result, nil
}

// DeleteInventory drops this device's inventory from the account if Library
// sync was turned off since it was last committed. A device that is no
// longer linked has nothing left to drop.
func (s *Service) DeleteInventory(ctx context.Context) (bool, error) {
	s.inventoryMu.Lock()
	defer s.inventoryMu.Unlock()

	raw, found, err := s.db.UserDB.GetDeviceState(DeviceStateKeyInventoryDeletePending)
	if err != nil {
		return false, fmt.Errorf("read library inventory delete marker: %w", err)
	}
	if !found || raw != "1" || s.cfg.LibrarySyncEnabled() {
		return false, nil
	}
	client, err := s.client()
	if err == nil {
		err = dropInventory(ctx, client)
	}
	if err != nil && !backup.IsRemoteUnlinkedError(err) {
		return false, err
	}
	if setErr := s.db.UserDB.SetDeviceState(DeviceStateKeyInventoryDeletePending, "0"); setErr != nil {
		return false, fmt.Errorf("clear library inventory delete marker: %w", setErr)
	}
	state, err := s.db.MediaDB.GetLibraryInventoryState(ctx)
	if err != nil {
		return true, fmt.Errorf("read library inventory state: %w", err)
	}
	// The ordinal cache stays: it is still valid for the same server.
	state = database.LibraryInventoryState{Endpoint: state.Endpoint, Credential: state.Credential}
	if err := s.db.MediaDB.SetLibraryInventoryState(ctx, &state); err != nil {
		return true, fmt.Errorf("store library inventory state: %w", err)
	}
	log.Info().Msg("library inventory removed from the account")
	return true, nil
}

func dropInventory(ctx context.Context, client *backup.OnlineClient) error {
	err := client.RetryRateLimited(ctx, func() error {
		return client.DoJSON(ctx, http.MethodDelete, pathInventory, nil, nil)
	})
	if err != nil {
		return fmt.Errorf("delete library inventory: %w", err)
	}
	return nil
}
