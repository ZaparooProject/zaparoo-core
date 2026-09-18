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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

const (
	// DeviceStateKeyDecksSince is the pull cursor for decks.
	DeviceStateKeyDecksSince = "library_decks_since"
	// DeviceStateKeyDecksLink records the endpoint and device link the deck
	// bookkeeping belongs to.
	DeviceStateKeyDecksLink = "library_decks_link"

	pathDecks         = "/v1/device/decks"
	deckPushBatch     = 200
	deckPullLimit     = 500
	deckPullMaxPages  = 200
	deckPushMaxRounds = 3
	deckMaxConflicts  = 5
	// deckAccessFreshness is how long a pull is trusted when a client looks
	// at decks or opens one. It matches how often the link service learns
	// about deck changes, so a tap is as fresh as a link.
	deckAccessFreshness = 30 * time.Second

	codeDeckLocked  = "deck_locked"
	codeDeckIDTaken = "deck_id_taken"
	// codeDeckIDUncreatable is this device's own note that a deck was never
	// offered to the account, because only a minted ID can be created there.
	codeDeckIDUncreatable = "deck_id_uncreatable"
)

// DecksResult summarizes one deck pass.
type DecksResult struct {
	Pulled    int
	Pushed    int
	Conflicts int
	Rejected  int
}

// deckContent is what the account and this device agree on for a deck.
// Card items are named by card ID alone: their scripts and name belong to
// the card.
type deckContent struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Items       []deckContentItem `json:"items"`
}

//nolint:tagliatelle // Stored in the shape the deck is pushed in.
type deckContentItem struct {
	Kind      string `json:"kind"`
	CardID    string `json:"card_id,omitempty"`
	Name      string `json:"name,omitempty"`
	ZapScript string `json:"zapscript,omitempty"`
}

func (item *deckContentItem) key() string {
	if item.Kind == database.DeckItemKindCard {
		return "card:" + item.CardID
	}
	return "script:" + item.ZapScript
}

func (c deckContent) encode() string {
	encoded, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func (c deckContent) hash() string {
	sum := sha256.Sum256([]byte(c.encode()))
	return hex.EncodeToString(sum[:8])
}

func decodeDeckContent(raw string) (deckContent, bool) {
	var content deckContent
	if raw == "" || json.Unmarshal([]byte(raw), &content) != nil {
		return deckContent{}, false
	}
	return content, true
}

func localDeckContent(deck *database.Deck) deckContent {
	content := deckContent{
		Name: deck.Name, Description: deck.Description, Items: make([]deckContentItem, 0, len(deck.Items)),
	}
	for i := range deck.Items {
		item := &deck.Items[i]
		if item.Kind == database.DeckItemKindCard {
			content.Items = append(content.Items, deckContentItem{Kind: item.Kind, CardID: item.CardID})
			continue
		}
		content.Items = append(content.Items, deckContentItem{
			Kind: item.Kind, Name: item.Name, ZapScript: item.ZapScript,
		})
	}
	return content
}

func serverDeckContent(deck *deckSync) deckContent {
	content := deckContent{
		Name: deck.Name, Description: deck.Description, Items: make([]deckContentItem, 0, len(deck.Items)),
	}
	for i := range deck.Items {
		item := &deck.Items[i]
		if item.Kind == database.DeckItemKindCard {
			content.Items = append(content.Items, deckContentItem{Kind: item.Kind, CardID: item.CardID})
			continue
		}
		content.Items = append(content.Items, deckContentItem{
			Kind: item.Kind, Name: item.Name, ZapScript: item.ZapScript,
		})
	}
	return content
}

// occurrenceKeys names each item by its key and how many times the key came
// before it, so a script added twice is two items.
func occurrenceKeys(items []deckContentItem) []string {
	seen := make(map[string]int, len(items))
	keys := make([]string, len(items))
	for i := range items {
		key := items[i].key()
		keys[i] = key + "#" + strconv.Itoa(seen[key])
		seen[key]++
	}
	return keys
}

// mergeDeck merges a deck both sides changed since base. The result follows
// the account's order without the items removed here; items added here go
// after the nearest item before them that is still in the list, or first.
// A name or description changed here wins over the account's.
func mergeDeck(base, local, server *deckContent) deckContent {
	merged := deckContent{Name: server.Name, Description: server.Description}
	if local.Name != base.Name {
		merged.Name = local.Name
	}
	if local.Description != base.Description {
		merged.Description = local.Description
	}

	baseKeys := occurrenceKeys(base.Items)
	localKeys := occurrenceKeys(local.Items)
	serverKeys := occurrenceKeys(server.Items)
	inBase := make(map[string]deckContentItem, len(baseKeys))
	for i, key := range baseKeys {
		inBase[key] = base.Items[i]
	}
	inLocal := make(map[string]deckContentItem, len(localKeys))
	for i, key := range localKeys {
		inLocal[key] = local.Items[i]
	}

	resultKeys := make([]string, 0, len(server.Items)+len(local.Items))
	resultItems := make(map[string]deckContentItem, len(server.Items)+len(local.Items))
	for i, key := range serverKeys {
		if _, wasBase := inBase[key]; wasBase {
			if _, kept := inLocal[key]; !kept {
				continue
			}
		}
		item := server.Items[i]
		if localItem, ok := inLocal[key]; ok && item.Kind == database.DeckItemKindScript {
			if baseItem, ok := inBase[key]; !ok || localItem.Name != baseItem.Name {
				item.Name = localItem.Name
			}
		}
		resultKeys = append(resultKeys, key)
		resultItems[key] = item
	}

	for i, key := range localKeys {
		if _, wasBase := inBase[key]; wasBase {
			continue
		}
		if _, present := resultItems[key]; present {
			continue
		}
		at := 0
		for j := i - 1; j >= 0; j-- {
			if pos := indexOf(resultKeys, localKeys[j]); pos >= 0 {
				at = pos + 1
				break
			}
		}
		resultKeys = append(resultKeys[:at], append([]string{key}, resultKeys[at:]...)...)
		resultItems[key] = local.Items[i]
	}

	merged.Items = make([]deckContentItem, 0, len(resultKeys))
	for _, key := range resultKeys {
		merged.Items = append(merged.Items, resultItems[key])
	}
	return merged
}

func indexOf(keys []string, key string) int {
	for i := range keys {
		if keys[i] == key {
			return i
		}
	}
	return -1
}

// SyncDecks converges owned decks with the account: pull first so the bases
// are current, then push what changed here.
func (s *Service) SyncDecks(ctx context.Context) (DecksResult, error) {
	if err := s.lockDecks(ctx); err != nil {
		return DecksResult{}, err
	}
	defer s.unlockDecks()
	pass, err := s.newDeckPass()
	if err != nil {
		return DecksResult{}, err
	}
	if err := pass.pull(ctx); err != nil {
		return pass.result, err
	}
	if err := pass.push(ctx); err != nil {
		return pass.result, err
	}
	if pass.result.Pulled > 0 || pass.result.Pushed > 0 || pass.result.Conflicts > 0 {
		log.Info().Int("pulled", pass.result.Pulled).Int("pushed", pass.result.Pushed).
			Int("conflicts", pass.result.Conflicts).Int("rejected", pass.result.Rejected).
			Msg("decks synced")
	}
	return pass.result, nil
}

// PullDecksIfStale pulls decks when the last pull is older than the access
// window, for a client listing them or a deck that just opened.
func (s *Service) PullDecksIfStale(ctx context.Context) error {
	last := s.lastDeckPull.Load()
	if last != 0 && s.now().Sub(time.Unix(0, last)) < deckAccessFreshness {
		return nil
	}
	return s.PullDecks(ctx)
}

// PullDecks applies deck changes from the account without pushing.
func (s *Service) PullDecks(ctx context.Context) error {
	if err := s.lockDecks(ctx); err != nil {
		return err
	}
	defer s.unlockDecks()
	pass, err := s.newDeckPass()
	if err != nil {
		return err
	}
	return pass.pull(ctx)
}

func (s *Service) lockDecks(ctx context.Context) error {
	select {
	case s.deckSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for deck sync: %w", ctx.Err())
	}
}

func (s *Service) unlockDecks() {
	<-s.deckSem
}

type deckPass struct {
	svc    *Service
	client *backup.OnlineClient
	rows   map[string]*database.DeckSyncRow
	result DecksResult
}

func (s *Service) newDeckPass() (*deckPass, error) {
	if !s.cfg.LibrarySyncEnabled() {
		return nil, ErrDisabled
	}
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	pass := &deckPass{svc: s, client: client}
	if linkErr := pass.checkLink(); linkErr != nil {
		return nil, linkErr
	}
	rows, err := s.db.UserDB.ListDeckSync()
	if err != nil {
		return nil, fmt.Errorf("read deck sync rows: %w", err)
	}
	pass.rows = make(map[string]*database.DeckSyncRow, len(rows))
	for i := range rows {
		pass.rows[rows[i].DeckID] = &rows[i]
	}
	return pass, nil
}

func (p *deckPass) userDB() database.UserDBI {
	return p.svc.db.UserDB
}

func (p *deckPass) checkLink() error {
	link := p.client.BaseURL() + "#" + p.client.CredentialTag()
	stored, found, err := p.userDB().GetDeviceState(DeviceStateKeyDecksLink)
	if err != nil {
		return fmt.Errorf("read deck sync link: %w", err)
	}
	if found && stored == link {
		return nil
	}
	if err := p.userDB().ClearDeckSync(); err != nil {
		return fmt.Errorf("clear deck sync rows: %w", err)
	}
	if err := p.userDB().SetDeviceState(DeviceStateKeyDecksSince, "0"); err != nil {
		return fmt.Errorf("reset deck cursor: %w", err)
	}
	if err := p.userDB().SetDeviceState(DeviceStateKeyDecksLink, link); err != nil {
		return fmt.Errorf("store deck sync link: %w", err)
	}
	return nil
}

func (p *deckPass) saveRow(row *database.DeckSyncRow) error {
	p.rows[row.DeckID] = row
	if err := p.userDB().UpsertDeckSync([]database.DeckSyncRow{*row}); err != nil {
		return fmt.Errorf("store deck sync row: %w", err)
	}
	return nil
}

func (p *deckPass) dropRow(deckID string) error {
	delete(p.rows, deckID)
	if err := p.userDB().DeleteDeckSync([]string{deckID}); err != nil {
		return fmt.Errorf("remove deck sync row: %w", err)
	}
	return nil
}

// pull applies every deck that changed on the account since the cursor.
func (p *deckPass) pull(ctx context.Context) error {
	raw, _, err := p.userDB().GetDeviceState(DeviceStateKeyDecksSince)
	if err != nil {
		return fmt.Errorf("read deck cursor: %w", err)
	}
	since, _ := strconv.ParseInt(raw, 10, 64)
	for range deckPullMaxPages {
		var page deckPullResponse
		query := url.Values{}
		query.Set("since", strconv.FormatInt(since, 10))
		query.Set("limit", strconv.Itoa(deckPullLimit))
		err := p.client.RetryRateLimited(ctx, func() error {
			page = deckPullResponse{}
			return p.client.DoJSON(ctx, http.MethodGet, pathDecks+"?"+query.Encode(), nil, &page)
		})
		if err != nil {
			return fmt.Errorf("pull decks: %w", err)
		}
		if page.Reset {
			// The account erased its decks. Local decks are the user's data
			// and stay; only the bookkeeping starts over, so every owned
			// deck pushes again as new.
			log.Info().Msg("decks were erased on the account; forgetting sync bookkeeping, local decks are kept")
			if err := p.userDB().ClearDeckSync(); err != nil {
				return fmt.Errorf("clear deck sync rows: %w", err)
			}
			p.rows = make(map[string]*database.DeckSyncRow)
			// Every deck is about to be pushed again as new, and decks the
			// account no longer holds may lose their rows, so reproject the
			// lot rather than only the decks this pass touches.
			p.svc.db.QueueAllDeckTags()
			since = 0
		}
		for i := range page.Items {
			if applyErr := p.applyPulled(&page.Items[i]); applyErr != nil {
				return applyErr
			}
		}
		if page.NextSince > since {
			since = page.NextSince
			if err := p.userDB().SetDeviceState(DeviceStateKeyDecksSince, strconv.FormatInt(since, 10)); err != nil {
				return fmt.Errorf("store deck cursor: %w", err)
			}
		}
		if !page.HasMore {
			break
		}
	}
	p.svc.lastDeckPull.Store(p.svc.now().UnixNano())
	return nil
}

// applyPulled adopts one deck from the account. A deck unchanged here takes
// the account's copy; a deck changed on both sides is merged and stays to be
// pushed; a deck deleted on the account is deleted here when it is clean,
// and kept to be brought back when it holds edits made here.
func (p *deckPass) applyPulled(remote *deckSync) error {
	deckID, err := database.NormalizeDeckID(remote.DeckID)
	if err != nil {
		log.Debug().Err(err).Str("deck", remote.DeckID).Msg("skipping deck with an ID this Core cannot hold")
		return nil
	}
	row := p.rows[deckID]
	if row != nil && row.Revision >= remote.Revision {
		return nil
	}
	p.result.Pulled++
	local, err := p.userDB().GetDeck(deckID)
	if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
		return fmt.Errorf("read deck %s: %w", deckID, err)
	}

	if remote.Deleted {
		return p.applyTombstone(deckID, local, row, remote.Revision)
	}

	server := serverDeckContent(remote)
	target := server
	if local != nil && local.Owned {
		content := localDeckContent(local)
		base, agreed := decodeDeckContent(snapshotOf(row))
		if !agreed {
			// Never agreed, as after a relink: keep both sides' items.
			base = deckContent{Name: server.Name, Description: server.Description}
		}
		if content.encode() != base.encode() {
			target = mergeDeck(&base, &content, &server)
		}
	}
	if err := p.writeLocal(deckID, remote, &target, local); err != nil {
		return err
	}
	next := &database.DeckSyncRow{
		DeckID: deckID, Snapshot: server.encode(), Revision: remote.Revision, Locked: remote.IsLocked,
	}
	return p.saveRow(next)
}

// writeLocal stores content as the owned deck, taking each card's scripts,
// name and display data from the account's copy and keeping each game item's
// link to a local file.
func (p *deckPass) writeLocal(
	deckID string, remote *deckSync, content *deckContent, local *database.Deck,
) error {
	if len(content.Items) > database.DeckMaxItems {
		// A merge of two full decks can outgrow the limit both sides keep.
		log.Warn().Str("deck", deckID).Int("items", len(content.Items)).
			Msg("merged deck is over the item limit; taking the account's copy")
		server := serverDeckContent(remote)
		p.svc.notifyInbox("Deck too long to merge", fmt.Sprintf(
			"%q holds %d items on Zaparoo Online and cannot take the ones added here as well, "+
				"so the account's copy was kept. The limit is %d items.",
			server.Name, len(server.Items), database.DeckMaxItems), inbox.CategoryNone)
		content = &server
	}
	serverCards := make(map[string]*deckSyncItem)
	for i := range remote.Items {
		if remote.Items[i].Kind == database.DeckItemKindCard {
			serverCards[remote.Items[i].CardID] = &remote.Items[i]
		}
	}
	localByKey := make(map[string][]database.DeckItem)
	if local != nil {
		for i := range local.Items {
			item := &local.Items[i]
			key := (&deckContentItem{Kind: item.Kind, CardID: item.CardID, ZapScript: item.ZapScript}).key()
			localByKey[key] = append(localByKey[key], *item)
		}
	}
	items := make([]database.DeckItem, 0, len(content.Items))
	for i := range content.Items {
		wanted := content.Items[i]
		key := wanted.key()
		var item database.DeckItem
		if candidates := localByKey[key]; len(candidates) > 0 {
			item = candidates[0]
			localByKey[key] = candidates[1:]
		}
		item.DBID, item.DeckDBID, item.Position = 0, 0, 0
		item.Kind = wanted.Kind
		if wanted.Kind == database.DeckItemKindCard {
			item.CardID = wanted.CardID
			if card, ok := serverCards[wanted.CardID]; ok {
				item.Name = card.Name
				item.Scripts = card.Scripts
				item.Metadata = card.Metadata
			}
		} else {
			item.Name = wanted.Name
			item.ZapScript = wanted.ZapScript
		}
		items = append(items, item)
	}
	deck := &database.Deck{
		DeckID:      deckID,
		Name:        content.Name,
		Description: content.Description,
		Owned:       true,
		Metadata:    remote.Metadata,
		Items:       items,
	}
	changed, err := p.userDB().UpsertRemoteDeck(deck)
	if err != nil {
		return fmt.Errorf("store deck %s: %w", deckID, err)
	}
	if !changed {
		return nil
	}
	p.svc.db.QueueDeckTags(deckID)
	action := models.DecksChangedRefreshed
	if local == nil {
		action = models.DecksChangedCreated
	}
	p.svc.notifyDecks(deckID, action)
	return nil
}

// applyTombstone acts on a deck the account deleted. A clean local copy,
// one that still matches the last agreed content, is deleted here too. A
// copy with edits made here since is the user's work: it stays, and is
// pushed against the tombstone's revision so the account brings it back
// under its own ID and the cards written for it keep working.
func (p *deckPass) applyTombstone(
	deckID string, local *database.Deck, row *database.DeckSyncRow, revision int64,
) error {
	if local == nil || !local.Owned {
		return p.dropRow(deckID)
	}
	clean := row != nil && localDeckContent(local).encode() == row.Snapshot
	if clean {
		if err := p.deleteLocal(deckID); err != nil {
			return err
		}
		return p.dropRow(deckID)
	}
	log.Info().Str("deck", deckID).Msg("deck deleted on the account holds edits made here; keeping it")
	return p.saveRow(&database.DeckSyncRow{DeckID: deckID, Revision: revision})
}

func (p *deckPass) deleteLocal(deckID string) error {
	if _, err := p.userDB().DeleteDeck(deckID); err != nil {
		return fmt.Errorf("delete deck %s: %w", deckID, err)
	}
	// Projecting a deck that is gone clears the tags it left behind.
	p.svc.db.QueueDeckTags(deckID)
	p.svc.notifyDecks(deckID, models.DecksChangedDeleted)
	return nil
}

// push sends every owned deck that differs from its agreed copy, and every
// deck deleted here that the account still holds.
func (p *deckPass) push(ctx context.Context) error {
	for range deckPushMaxRounds {
		records, err := p.dirtyRecords()
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		retry := false
		for start := 0; start < len(records); start += deckPushBatch {
			end := min(start+deckPushBatch, len(records))
			batch := records[start:end]
			request := deckPushRequest{Items: make([]deckPushRecord, len(batch))}
			for i := range batch {
				request.Items[i] = batch[i].record
			}
			var response deckPushResponse
			err := p.client.RetryRateLimited(ctx, func() error {
				response = deckPushResponse{}
				return p.client.DoJSON(ctx, http.MethodPost, pathDecks, &request, &response)
			})
			if err != nil {
				return fmt.Errorf("push decks: %w", err)
			}
			again, handleErr := p.handlePushResults(batch, &response)
			if handleErr != nil {
				return handleErr
			}
			retry = retry || again
		}
		if !retry {
			return nil
		}
	}
	return nil
}

type pendingDeck struct {
	content *deckContent
	deckID  string
	name    string
	record  deckPushRecord
}

func (p *deckPass) dirtyRecords() ([]pendingDeck, error) {
	list, err := p.userDB().ListDecks()
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	owned := make(map[string]bool, len(list))
	pending := make([]pendingDeck, 0)
	for i := range list {
		if !list[i].Owned {
			continue
		}
		deckID := list[i].DeckID
		owned[deckID] = true
		row := p.rows[deckID]
		if row != nil && row.Locked {
			continue
		}
		deck, getErr := p.userDB().GetDeck(deckID)
		if getErr != nil {
			return nil, fmt.Errorf("read deck %s: %w", deckID, getErr)
		}
		content := localDeckContent(deck)
		if row != nil && content.encode() == row.Snapshot {
			continue
		}
		if row != nil && row.RejectedHash != "" && row.RejectedHash == content.hash() {
			continue
		}
		if pushIsCreate(row) && !database.IsMintedDeckID(deckID) {
			// The account only takes a minted ID on a create, and refuses
			// the whole batch over one record, so holding this deck back
			// keeps every other deck syncing. Nothing about the deck is
			// stored as rejected, so a later Core that can create it will.
			if err := p.reportUncreatableDeck(deckID, deck.Name); err != nil {
				return nil, err
			}
			continue
		}
		record := deckPushRecord{DeckID: deckID, Name: &content.Name, Description: &content.Description}
		items := make([]deckPushItemInput, 0, len(content.Items))
		for _, item := range content.Items {
			items = append(items, deckPushItemInput(item))
		}
		record.Items = &items
		if row != nil {
			record.BaseRevision = row.Revision
		}
		pending = append(pending, pendingDeck{deckID: deckID, name: deck.Name, content: &content, record: record})
	}
	for deckID, row := range p.rows {
		if owned[deckID] {
			continue
		}
		if row.Revision == 0 {
			if err := p.dropRow(deckID); err != nil {
				return nil, err
			}
			continue
		}
		pending = append(pending, pendingDeck{
			deckID: deckID,
			record: deckPushRecord{DeckID: deckID, BaseRevision: row.Revision, Deleted: true},
		})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].deckID < pending[j].deckID })
	return pending, nil
}

// pushIsCreate reports whether pushing this deck would be a create, the only
// push where the account reads the deck's ID as one a device minted.
func pushIsCreate(row *database.DeckSyncRow) bool { return row == nil || row.Revision == 0 }

// reportUncreatableDeck tells the user, once, that a deck stays on the
// device. It is held back rather than marked rejected: the deck is fine, it
// is its older ID the account will not take on a create.
func (p *deckPass) reportUncreatableDeck(deckID, name string) error {
	row := p.rows[deckID]
	if row != nil && row.RejectedCode == codeDeckIDUncreatable {
		return nil
	}
	log.Warn().Str("deck", deckID).Msg("deck cannot be created on the account under its ID; keeping it here")
	p.svc.notifyInbox("Deck not synced", fmt.Sprintf(
		"%q stays on this device: its ID is older than the ones Zaparoo Online issues, "+
			"and the account cannot take it as a new deck.", name), inbox.CategoryNone)
	next := &database.DeckSyncRow{DeckID: deckID, RejectedCode: codeDeckIDUncreatable}
	if row != nil {
		copied := *row
		copied.RejectedCode = codeDeckIDUncreatable
		next = &copied
	}
	return p.saveRow(next)
}

func (p *deckPass) handlePushResults(batch []pendingDeck, response *deckPushResponse) (bool, error) {
	retry := false
	for _, result := range response.Items {
		if result.Index < 0 || result.Index >= len(batch) {
			continue
		}
		pending := &batch[result.Index]
		var err error
		switch result.Status {
		case statusApplied:
			p.result.Pushed++
			err = p.handleApplied(pending, result.Deck)
		case statusConflict:
			p.result.Conflicts++
			retry = true
			err = p.handleConflict(pending, result.Deck)
		case statusRejected:
			var again bool
			again, err = p.handleRejected(pending, &result)
			retry = retry || again
		}
		if err != nil {
			return retry, err
		}
	}
	return retry, nil
}

func (p *deckPass) handleApplied(pending *pendingDeck, remote *deckSync) error {
	if pending.record.Deleted || remote == nil {
		return p.dropRow(pending.deckID)
	}
	if remote.Deleted {
		// A retried create for a deck deleted on the account since.
		local, err := p.userDB().GetDeck(pending.deckID)
		if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
			return fmt.Errorf("read deck %s: %w", pending.deckID, err)
		}
		return p.applyTombstone(pending.deckID, local, p.rows[pending.deckID], remote.Revision)
	}
	server := serverDeckContent(remote)
	row := &database.DeckSyncRow{
		DeckID: pending.deckID, Snapshot: server.encode(), Revision: remote.Revision, Locked: remote.IsLocked,
	}
	local, err := p.userDB().GetDeck(pending.deckID)
	if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
		return fmt.Errorf("read deck %s: %w", pending.deckID, err)
	}
	// Store the account's card scripts and display data, or its whole copy
	// when the deck is locked. When the account kept a different deck under
	// this ID (a retried create), merge with nothing agreed so both sides'
	// items survive.
	target := server
	if !remote.IsLocked && server.encode() != pending.content.encode() {
		empty := deckContent{Name: server.Name, Description: server.Description}
		target = mergeDeck(&empty, pending.content, &server)
	}
	if err := p.writeLocal(pending.deckID, remote, &target, local); err != nil {
		return err
	}
	return p.saveRow(row)
}

func (p *deckPass) handleConflict(pending *pendingDeck, remote *deckSync) error {
	row := p.rows[pending.deckID]
	if remote == nil {
		// The account holds no such deck: create it again next round.
		if pending.record.Deleted {
			return p.dropRow(pending.deckID)
		}
		next := &database.DeckSyncRow{DeckID: pending.deckID}
		return p.saveRow(next)
	}
	if remote.Deleted {
		if pending.record.Deleted {
			return p.dropRow(pending.deckID)
		}
		local, err := p.userDB().GetDeck(pending.deckID)
		if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
			return fmt.Errorf("read deck %s: %w", pending.deckID, err)
		}
		return p.applyTombstone(pending.deckID, local, row, remote.Revision)
	}
	if pending.record.Deleted {
		// Deleted here, changed there: the delete stands, against the new
		// revision.
		next := &database.DeckSyncRow{
			DeckID: pending.deckID, Snapshot: serverDeckContent(remote).encode(),
			Revision: remote.Revision, Locked: remote.IsLocked,
		}
		return p.saveRow(next)
	}
	local, err := p.userDB().GetDeck(pending.deckID)
	if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
		return fmt.Errorf("read deck %s: %w", pending.deckID, err)
	}
	server := serverDeckContent(remote)
	target := server
	conflicts := 1
	if row != nil {
		conflicts = row.Conflicts + 1
	}
	if base, ok := decodeDeckContent(snapshotOf(row)); ok && conflicts <= deckMaxConflicts && !remote.IsLocked {
		target = mergeDeck(&base, pending.content, &server)
	} else if conflicts > deckMaxConflicts {
		log.Warn().Str("deck", pending.deckID).Msg("deck keeps conflicting; taking the account's copy")
		conflicts = 0
	}
	if err := p.writeLocal(pending.deckID, remote, &target, local); err != nil {
		return err
	}
	next := &database.DeckSyncRow{
		DeckID: pending.deckID, Snapshot: server.encode(), Revision: remote.Revision,
		Locked: remote.IsLocked, Conflicts: conflicts,
	}
	return p.saveRow(next)
}

func snapshotOf(row *database.DeckSyncRow) string {
	if row == nil {
		return ""
	}
	return row.Snapshot
}

func (p *deckPass) handleRejected(pending *pendingDeck, result *deckPushResult) (bool, error) {
	switch result.Code {
	case codeDeckIDTaken:
		return true, p.remint(pending)
	case codeDeckLocked:
		if result.Deck == nil {
			break
		}
		local, err := p.userDB().GetDeck(pending.deckID)
		if err != nil && !errors.Is(err, database.ErrDeckNotFound) {
			return false, fmt.Errorf("read deck %s: %w", pending.deckID, err)
		}
		server := serverDeckContent(result.Deck)
		if err := p.writeLocal(pending.deckID, result.Deck, &server, local); err != nil {
			return false, err
		}
		p.svc.notifyInbox("Deck locked on Zaparoo Online", fmt.Sprintf(
			"%q was locked on Zaparoo Online, so changes made on this device were replaced.", server.Name),
			inbox.CategoryNone)
		row := &database.DeckSyncRow{
			DeckID: pending.deckID, Snapshot: server.encode(), Revision: result.Deck.Revision, Locked: true,
		}
		return false, p.saveRow(row)
	}
	p.result.Rejected++
	row := p.rows[pending.deckID]
	next := &database.DeckSyncRow{DeckID: pending.deckID}
	if row != nil {
		copied := *row
		next = &copied
	}
	if pending.content != nil {
		if next.RejectedHash != pending.content.hash() {
			log.Warn().Str("deck", pending.deckID).Str("code", result.Code).Msg("deck was rejected by the account")
			p.svc.notifyInbox("Deck not synced", fmt.Sprintf(
				"%q could not be synced with Zaparoo Online (%s). It is kept on this device.",
				pending.name, strings.ReplaceAll(result.Code, "_", " ")), inbox.CategoryNone)
		}
		next.RejectedHash = pending.content.hash()
	}
	next.RejectedCode = result.Code
	return false, p.saveRow(next)
}

// remint gives a deck whose ID another account holds a new one. Cards
// already written with the old ID stop opening it, so the user is told.
func (p *deckPass) remint(pending *pendingDeck) error {
	newID, err := database.NewDeckID()
	if err != nil {
		return fmt.Errorf("mint deck id: %w", err)
	}
	if err := p.userDB().RenameDeck(pending.deckID, newID); err != nil {
		return fmt.Errorf("rename deck %s: %w", pending.deckID, err)
	}
	if row, ok := p.rows[pending.deckID]; ok {
		delete(p.rows, pending.deckID)
		row.DeckID = newID
		p.rows[newID] = row
	}
	// The old ID's tags are cleared and the new ID's written in the
	// background: a deck holding titles this device does not have costs a
	// title lookup per item.
	p.svc.db.QueueDeckTags(pending.deckID, newID)
	log.Warn().Str("old", pending.deckID).Str("new", newID).Msg("deck ID was already taken; minted a new one")
	p.svc.notifyInbox("Deck given a new ID", fmt.Sprintf(
		"%q needed a new ID to sync. Cards written for it before need to be written again; "+
			"they still open the copy the other account keeps.", pending.name),
		inbox.CategoryNone)
	p.svc.notifyDecks(pending.deckID, models.DecksChangedDeleted)
	p.svc.notifyDecks(newID, models.DecksChangedCreated)
	return nil
}

func (s *Service) notifyDecks(deckID, action string) {
	if s.notifications == nil {
		return
	}
	notifications.DecksChanged(s.notifications, models.DecksChangedNotification{DeckID: deckID, Action: action})
}

func (s *Service) notifyInbox(title, body, category string) {
	if s.inbox == nil {
		return
	}
	if err := s.inbox.Add(title, inbox.WithBody(body), inbox.WithSeverity(inbox.SeverityWarning),
		inbox.WithCategory(category)); err != nil {
		log.Warn().Err(err).Msg("failed to add deck sync inbox message")
	}
}

type deckPushRequest struct {
	Items []deckPushRecord `json:"items"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckPushRecord struct {
	Name         *string              `json:"name,omitempty"`
	Description  *string              `json:"description,omitempty"`
	Items        *[]deckPushItemInput `json:"items,omitempty"`
	DeckID       string               `json:"deck_id"`
	BaseRevision int64                `json:"base_revision"`
	Deleted      bool                 `json:"deleted,omitempty"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckPushItemInput struct {
	Kind      string `json:"kind"`
	CardID    string `json:"card_id,omitempty"`
	Name      string `json:"name,omitempty"`
	ZapScript string `json:"zapscript,omitempty"`
}

type deckPushResponse struct {
	Items []deckPushResult `json:"items"`
}

type deckPushResult struct {
	Deck   *deckSync `json:"deck"`
	Status string    `json:"status"`
	Code   string    `json:"code"`
	Index  int       `json:"index"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckPullResponse struct {
	Items     []deckSync `json:"items"`
	NextSince int64      `json:"next_since"`
	HasMore   bool       `json:"has_more"`
	Reset     bool       `json:"reset"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckSync struct {
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	DeckID      string          `json:"deck_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Items       []deckSyncItem  `json:"items"`
	Revision    int64           `json:"revision"`
	IsLocked    bool            `json:"is_locked"`
	Deleted     bool            `json:"deleted"`
}

//nolint:tagliatelle // Wire shape follows the Zaparoo Online API contract.
type deckSyncItem struct {
	Metadata  json.RawMessage           `json:"metadata,omitempty"`
	Kind      string                    `json:"kind"`
	CardID    string                    `json:"card_id"`
	Name      string                    `json:"name"`
	ZapScript string                    `json:"zapscript"`
	Scripts   []database.DeckCardScript `json:"scripts"`
	Position  int                       `json:"position"`
}
