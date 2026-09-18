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

package userdb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
)

const deckColumns = `DBID, DeckID, Name, Description, Owned, Metadata, SourceURL, FetchedAt, CreatedAt, UpdatedAt`

const deckItemColumns = `DBID, DeckDBID, Position, Kind, Name, ZapScript, CardID, Scripts, Metadata,
	SystemID, Path, MediaName, Tags, CreatedAt, UpdatedAt`

// CreateDeck inserts a deck with its items. The deck's DeckID must already be
// minted and normalized. A deck fails with ErrDeckItemLimit past
// DeckMaxItems. How many decks a device holds is not capped: a deck costs
// well under ten kilobytes, and the work a deck creates is bounded per deck
// by DeckMaxItems rather than by their number.
func (db *UserDB) CreateDeck(deck *database.Deck) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if len(deck.Items) > database.DeckMaxItems {
		return database.ErrDeckItemLimit
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) (bool, error) {
		deck.CreatedAt, deck.UpdatedAt = now, now
		if err := sqlInsertDeck(ctx, tx, deck); err != nil {
			return false, err
		}
		if err := sqlSaveDeckItems(ctx, tx, deck.DBID, nil, deck.Items, now); err != nil {
			return false, err
		}
		return true, nil
	})
}

// GetDeck returns a deck with its items in position order, both read from
// the same snapshot.
func (db *UserDB) GetDeck(deckID string) (*database.Deck, error) {
	conn := db.sql.Load()
	if conn == nil {
		return nil, ErrNullSQL
	}
	tx, err := conn.BeginTx(db.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin deck read: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			log.Warn().Err(rollbackErr).Msg("failed to end deck read")
		}
	}()
	return sqlGetDeckWithItems(db.ctx, tx, deckID)
}

// ListDecks returns every deck without items, most recently updated first,
// with ItemCount filled in.
func (db *UserDB) ListDecks() ([]database.Deck, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlListDecks(db.ctx, db.sql.Load())
}

// UpdateDeck edits a deck in one transaction. It loads the deck with its
// items, lets edit change the deck's Name, Description, Metadata and Items,
// and stores those fields; edit's other changes are ignored. An item that
// still carries the DBID of one of the deck's items keeps that row, so its ID
// and creation time survive the edit. Any other item is inserted, and rows no
// item kept are deleted. Positions follow list order from one. When edit
// fails, or leaves more than DeckMaxItems items, nothing is written. Returns
// the deck as stored.
func (db *UserDB) UpdateDeck(deckID string, edit func(deck *database.Deck) error) (*database.Deck, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	var updated *database.Deck
	err := db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) (bool, error) {
		deck, err := sqlGetDeckWithItems(ctx, tx, deckID)
		if err != nil {
			return false, err
		}
		if lockErr := refuseLockedDeck(ctx, tx, deckID); lockErr != nil {
			return false, lockErr
		}
		stored := deck.Items
		deck.Items = slices.Clone(stored)
		if editErr := edit(deck); editErr != nil {
			return false, editErr
		}
		if len(deck.Items) > database.DeckMaxItems {
			return false, database.ErrDeckItemLimit
		}
		deck.UpdatedAt = now
		if _, execErr := tx.ExecContext(ctx, `
			update Decks set Name = ?, Description = ?, Metadata = ?, UpdatedAt = ?
			where DBID = ?;`,
			deck.Name, deck.Description, string(deck.Metadata), now, deck.DBID); execErr != nil {
			return false, fmt.Errorf("failed to update deck: %w", execErr)
		}
		if saveErr := sqlSaveDeckItems(ctx, tx, deck.DBID, stored, deck.Items, now); saveErr != nil {
			return false, saveErr
		}
		updated = deck
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteDeck removes a deck and its items. The bool reports whether a deck
// existed.
func (db *UserDB) DeleteDeck(deckID string) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	existed := false
	err := db.deckTx(func(ctx context.Context, tx *sql.Tx, _ int64) (bool, error) {
		deck, err := sqlGetDeck(ctx, tx, deckID)
		if errors.Is(err, database.ErrDeckNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if lockErr := refuseLockedDeck(ctx, tx, deckID); lockErr != nil {
			return false, lockErr
		}
		if _, err = tx.ExecContext(ctx, `delete from DeckItems where DeckDBID = ?;`, deck.DBID); err != nil {
			return false, fmt.Errorf("failed to delete deck items: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `delete from Decks where DBID = ?;`, deck.DBID); err != nil {
			return false, fmt.Errorf("failed to delete deck: %w", err)
		}
		existed = true
		return true, nil
	})
	return existed, err
}

// refuseLockedDeck fails with ErrDeckReadOnly when the linked account locked
// the deck. It reads the sync row inside the caller's transaction, so a lock
// arriving from a sync pass cannot land between the check and the write, and
// a row that cannot be read refuses the edit rather than allowing it. Sync
// writes its own copy of a locked deck through UpsertRemoteDeck, which does
// not come this way.
func refuseLockedDeck(ctx context.Context, tx *sql.Tx, deckID string) error {
	var locked bool
	err := tx.QueryRowContext(ctx, `select Locked from DeckSync where DeckID = ?;`, deckID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read deck sync state: %w", err)
	}
	if locked {
		return database.ErrDeckReadOnly
	}
	return nil
}

// UpsertRemoteDeck inserts or fully replaces a deck that arrived from
// elsewhere (a ZapLink fetch or a sync pull), keeping the stored DBID and
// creation time. The deck's ID is normalized first. A cached copy of somebody
// else's deck never overwrites an owned deck of the same ID (ErrDeckOwned),
// so a tap on your own deck's ZapLink opens the editable local copy. A deck
// fails with ErrDeckItemLimit past DeckMaxItems.
//
// An arriving item carries no row of its own, so one that matches a stored
// item exactly adopts that row and the local file it is linked to. Only what
// really changed is written, and a fetch that brings nothing new records that
// the deck was checked and nothing else. It reports whether the stored deck
// changed, so the caller can queue the deck's tags only when it did.
func (db *UserDB) UpsertRemoteDeck(deck *database.Deck) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	if len(deck.Items) > database.DeckMaxItems {
		return false, database.ErrDeckItemLimit
	}
	deckID, err := database.NormalizeDeckID(deck.DeckID)
	if err != nil {
		return false, fmt.Errorf("remote deck: %w", err)
	}
	deck.DeckID = deckID

	unchanged, err := db.touchUnchangedRemoteDeck(deck)
	if err != nil || unchanged {
		return false, err
	}

	changed := false
	err = db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) (bool, error) {
		existing, getErr := sqlGetDeckWithItems(ctx, tx, deck.DeckID)
		var stored []database.DeckItem
		switch {
		case errors.Is(getErr, database.ErrDeckNotFound):
			deck.CreatedAt, deck.UpdatedAt, deck.FetchedAt = now, now, now
			if insertErr := sqlInsertDeck(ctx, tx, deck); insertErr != nil {
				return false, insertErr
			}
		case getErr != nil:
			return false, getErr
		case existing.Owned && !deck.Owned:
			return false, database.ErrDeckOwned
		default:
			deck.DBID, deck.CreatedAt, deck.UpdatedAt, deck.FetchedAt = existing.DBID, existing.CreatedAt, now, now
			stored = existing.Items
			adoptRemoteDeckItemRows(stored, deck.Items)
			if _, execErr := tx.ExecContext(ctx, `
				update Decks set Name = ?, Description = ?, Owned = ?, Metadata = ?, SourceURL = ?,
					FetchedAt = ?, UpdatedAt = ?
				where DBID = ?;`,
				deck.Name, deck.Description, deck.Owned, string(deck.Metadata), deck.SourceURL,
				now, now, existing.DBID); execErr != nil {
				return false, fmt.Errorf("failed to update remote deck: %w", execErr)
			}
		}
		if saveErr := sqlSaveDeckItems(ctx, tx, deck.DBID, stored, deck.Items, now); saveErr != nil {
			return false, saveErr
		}
		changed = true
		return true, nil
	})
	return changed, err
}

// touchUnchangedRemoteDeck records that a deck was fetched when the arriving
// copy holds nothing the stored one does not. Rewriting the unchanged rows
// would move every item to a new row, drop the local file each item is linked
// to, and advance the media preferences revision for a listing that did not
// change; taking the fetch time alone keeps the next open from fetching
// again. It reports whether the deck was left alone.
func (db *UserDB) touchUnchangedRemoteDeck(deck *database.Deck) (bool, error) {
	existing, err := db.GetDeck(deck.DeckID)
	if errors.Is(err, database.ErrDeckNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing.Owned && !deck.Owned {
		return true, database.ErrDeckOwned
	}
	if !sameRemoteDeck(existing, deck) {
		return false, nil
	}
	if _, err = db.sql.Load().ExecContext(db.ctx,
		`update Decks set FetchedAt = ? where DBID = ?;`, time.Now().Unix(), existing.DBID,
	); err != nil {
		return false, fmt.Errorf("failed to record deck fetch: %w", err)
	}
	return true, nil
}

// sameRemoteDeck reports whether an arriving deck holds nothing the stored
// one does not. The arriving items carry no rows or local file links, so they
// are compared on what the source actually serves.
func sameRemoteDeck(stored, arriving *database.Deck) bool {
	if stored.Name != arriving.Name || stored.Description != arriving.Description ||
		stored.SourceURL != arriving.SourceURL || stored.Owned != arriving.Owned ||
		!bytes.Equal(stored.Metadata, arriving.Metadata) ||
		len(stored.Items) != len(arriving.Items) {
		return false
	}
	for i := range stored.Items {
		if !sameServedDeckItem(&stored.Items[i], &arriving.Items[i]) {
			return false
		}
	}
	return true
}

// sameServedDeckItem compares the part of an item its source serves, leaving
// out the row and the local file the item is linked to, which are this
// device's own.
func sameServedDeckItem(a, b *database.DeckItem) bool {
	return a.Kind == b.Kind && a.Name == b.Name && a.ZapScript == b.ZapScript && a.CardID == b.CardID &&
		database.EncodeDeckCardScripts(a.Scripts) == database.EncodeDeckCardScripts(b.Scripts) &&
		bytes.Equal(a.Metadata, b.Metadata)
}

// adoptRemoteDeckItemRows gives each arriving item the row of the stored item
// it matches, along with the local file that row is linked to, so an item
// that survived the fetch keeps the file this device picked for it. Each
// stored row is claimed once, in order, so a deck that only reorders its
// items carries every link across.
func adoptRemoteDeckItemRows(stored, arriving []database.DeckItem) {
	claimed := make([]bool, len(stored))
	for i := range arriving {
		item := &arriving[i]
		if item.DBID != 0 {
			continue
		}
		for j := range stored {
			if claimed[j] || !sameServedDeckItem(&stored[j], item) {
				continue
			}
			claimed[j] = true
			item.DBID = stored[j].DBID
			item.Anchor = stored[j].Anchor
			break
		}
	}
}

// RenameDeck moves a deck and its sync row to a new ID. Fails with
// ErrDeckNotFound when no deck holds oldID.
func (db *UserDB) RenameDeck(oldID, newID string) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) (bool, error) {
		res, err := tx.ExecContext(ctx, `update Decks set DeckID = ?, UpdatedAt = ? where DeckID = ?;`,
			newID, now, oldID)
		if err != nil {
			return false, fmt.Errorf("failed to rename deck: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("failed to count renamed decks: %w", err)
		}
		if affected == 0 {
			return false, fmt.Errorf("%w: %s", database.ErrDeckNotFound, oldID)
		}
		if _, err = tx.ExecContext(ctx, `update DeckSync set DeckID = ? where DeckID = ?;`, newID, oldID); err != nil {
			return false, fmt.Errorf("failed to rename deck sync row: %w", err)
		}
		return true, nil
	})
}

// SetDeckItemAnchor records the local file a game item resolved to. It never
// inserts.
func (db *UserDB) SetDeckItemAnchor(itemDBID int64, anchor *database.DeckItemAnchor) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	_, err := db.sql.Load().ExecContext(db.ctx, `
		update DeckItems set SystemID = ?, Path = ?, MediaName = ?, Tags = ?, UpdatedAt = ?
		where DBID = ?;`,
		anchor.SystemID, pathutil.CanonicalMediaPath(anchor.Path), anchor.MediaName,
		database.EncodeTagStrings(anchor.Tags), time.Now().Unix(), itemDBID)
	if err != nil {
		return fmt.Errorf("failed to set deck item anchor: %w", err)
	}
	return nil
}

// CountOwnedDecks returns how many decks this device owns.
func (db *UserDB) CountOwnedDecks() (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlCountOwnedDecks(db.ctx, db.sql.Load())
}

// deckTx runs fn in one transaction. The transaction opens with the write
// that advances the media preferences revision, because deck membership is a
// listing-affecting tag and browse cursors must be invalidated like any other
// preference change. Writing first also takes SQLite's write lock before fn
// reads anything, so concurrent deck writes wait on the busy timeout rather
// than one failing on a snapshot the other made stale. fn reports whether it
// changed anything; the transaction commits only when it did and no error
// was returned.
func (db *UserDB) deckTx(fn func(ctx context.Context, tx *sql.Tx, now int64) (bool, error)) error {
	tx, err := db.sql.Load().BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin deck transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			log.Warn().Err(rollbackErr).Msg("failed to roll back deck transaction")
		}
	}()
	now := time.Now().Unix()
	if revErr := sqlAdvanceMediaPreferencesRevision(db.ctx, tx, now); revErr != nil {
		return revErr
	}
	changed, err := fn(db.ctx, tx, now)
	if err != nil || !changed {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit deck transaction: %w", err)
	}
	return nil
}

type deckQueryable interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func sqlCountOwnedDecks(ctx context.Context, q deckQueryable) (int, error) {
	var count int
	if err := q.QueryRowContext(ctx, `select count(*) from Decks where Owned = 1;`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count owned decks: %w", err)
	}
	return count, nil
}

func sqlInsertDeck(ctx context.Context, q deckQueryable, deck *database.Deck) error {
	err := q.QueryRowContext(ctx, `
		insert into Decks(DeckID, Name, Description, Owned, Metadata, SourceURL, FetchedAt, CreatedAt, UpdatedAt)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		returning DBID;`,
		deck.DeckID, deck.Name, deck.Description, deck.Owned, string(deck.Metadata), deck.SourceURL,
		deck.FetchedAt, deck.CreatedAt, deck.UpdatedAt,
	).Scan(&deck.DBID)
	if err != nil {
		return fmt.Errorf("failed to insert deck: %w", err)
	}
	return nil
}

// sqlSaveDeckItems makes items the item list of the deck whose rows are
// currently stored. An item carrying the DBID of a stored row keeps that row,
// once per row; every other item is inserted, and stored rows no item kept are
// deleted. Items are updated in place with their DBID, deck, position and
// timestamps. A kept row is only written when its position or content
// changed, and its UpdatedAt only moves for a content change.
func sqlSaveDeckItems(
	ctx context.Context, q deckQueryable, deckDBID int64, stored, items []database.DeckItem, now int64,
) error {
	byID := make(map[int64]*database.DeckItem, len(stored))
	for i := range stored {
		byID[stored[i].DBID] = &stored[i]
	}
	kept := make(map[int64]*database.DeckItem, len(items))
	for i := range items {
		item := &items[i]
		row, ok := byID[item.DBID]
		if !ok || kept[item.DBID] != nil {
			item.DBID = 0
			continue
		}
		kept[item.DBID] = row
	}
	for i := range stored {
		if kept[stored[i].DBID] != nil {
			continue
		}
		if _, err := q.ExecContext(ctx, `delete from DeckItems where DBID = ?;`, stored[i].DBID); err != nil {
			return fmt.Errorf("failed to delete deck item: %w", err)
		}
	}
	// Rows that move are parked at negative positions first, so no write
	// below collides with the unique (DeckDBID, Position) index.
	moved := make([]any, 0, len(kept))
	for i := range items {
		if row := kept[items[i].DBID]; row != nil && row.Position != i+1 {
			moved = append(moved, row.DBID)
		}
	}
	if len(moved) > 0 {
		if _, err := q.ExecContext(ctx, `update DeckItems set Position = -Position where DBID in (`+
			strings.TrimSuffix(strings.Repeat("?,", len(moved)), ",")+`);`, moved...); err != nil {
			return fmt.Errorf("failed to move deck items: %w", err)
		}
	}
	for i := range items {
		item := &items[i]
		item.DeckDBID, item.Position = deckDBID, i+1
		if row := kept[item.DBID]; row != nil {
			item.CreatedAt, item.UpdatedAt = row.CreatedAt, row.UpdatedAt
			sameContent := sameDeckItemContent(row, item)
			if sameContent && row.Position == item.Position {
				continue
			}
			if !sameContent {
				item.UpdatedAt = now
			}
			if _, err := q.ExecContext(ctx, `
				update DeckItems set Position = ?, Kind = ?, Name = ?, ZapScript = ?, CardID = ?,
					Scripts = ?, Metadata = ?, SystemID = ?, Path = ?, MediaName = ?, Tags = ?, UpdatedAt = ?
				where DBID = ?;`,
				item.Position, item.Kind, item.Name, item.ZapScript, item.CardID,
				database.EncodeDeckCardScripts(item.Scripts), string(item.Metadata),
				item.Anchor.SystemID, pathutil.CanonicalMediaPath(item.Anchor.Path), item.Anchor.MediaName,
				database.EncodeTagStrings(item.Anchor.Tags), item.UpdatedAt, item.DBID,
			); err != nil {
				return fmt.Errorf("failed to update deck item %d: %w", item.Position, err)
			}
			continue
		}
		item.CreatedAt, item.UpdatedAt = now, now
		err := q.QueryRowContext(ctx, `
			insert into DeckItems(DeckDBID, Position, Kind, Name, ZapScript, CardID, Scripts, Metadata,
				SystemID, Path, MediaName, Tags, CreatedAt, UpdatedAt)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			returning DBID;`,
			deckDBID, item.Position, item.Kind, item.Name, item.ZapScript, item.CardID,
			database.EncodeDeckCardScripts(item.Scripts), string(item.Metadata),
			item.Anchor.SystemID, pathutil.CanonicalMediaPath(item.Anchor.Path), item.Anchor.MediaName,
			database.EncodeTagStrings(item.Anchor.Tags), now, now,
		).Scan(&item.DBID)
		if err != nil {
			return fmt.Errorf("failed to insert deck item %d: %w", item.Position, err)
		}
	}
	return nil
}

// sameDeckItemContent reports whether two items store the same columns,
// ignoring identity, position and timestamps.
func sameDeckItemContent(a, b *database.DeckItem) bool {
	return a.Kind == b.Kind && a.Name == b.Name && a.ZapScript == b.ZapScript && a.CardID == b.CardID &&
		database.EncodeDeckCardScripts(a.Scripts) == database.EncodeDeckCardScripts(b.Scripts) &&
		bytes.Equal(a.Metadata, b.Metadata) &&
		a.Anchor.SystemID == b.Anchor.SystemID &&
		pathutil.CanonicalMediaPath(a.Anchor.Path) == pathutil.CanonicalMediaPath(b.Anchor.Path) &&
		a.Anchor.MediaName == b.Anchor.MediaName &&
		database.EncodeTagStrings(a.Anchor.Tags) == database.EncodeTagStrings(b.Anchor.Tags)
}

func scanDeck(scan func(dest ...any) error) (*database.Deck, error) {
	var deck database.Deck
	var metadata string
	if err := scan(
		&deck.DBID, &deck.DeckID, &deck.Name, &deck.Description, &deck.Owned, &metadata,
		&deck.SourceURL, &deck.FetchedAt, &deck.CreatedAt, &deck.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if metadata != "" {
		deck.Metadata = json.RawMessage(metadata)
	}
	return &deck, nil
}

func sqlGetDeck(ctx context.Context, q deckQueryable, deckID string) (*database.Deck, error) {
	deck, err := scanDeck(q.QueryRowContext(ctx, `select `+deckColumns+` from Decks where DeckID = ?;`, deckID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", database.ErrDeckNotFound, deckID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan deck: %w", err)
	}
	return deck, nil
}

// sqlGetDeckWithItems returns a deck with its items in position order.
func sqlGetDeckWithItems(ctx context.Context, q deckQueryable, deckID string) (*database.Deck, error) {
	deck, err := sqlGetDeck(ctx, q, deckID)
	if err != nil {
		return nil, err
	}
	deck.Items, err = sqlListDeckItems(ctx, q, deck.DBID)
	if err != nil {
		return nil, err
	}
	return deck, nil
}

func sqlListDecks(ctx context.Context, q deckQueryable) ([]database.Deck, error) {
	rows, err := q.QueryContext(ctx, `
		select `+deckColumns+`, (select count(*) from DeckItems i where i.DeckDBID = Decks.DBID)
		from Decks order by UpdatedAt desc, DBID desc;`)
	if err != nil {
		return nil, fmt.Errorf("failed to query decks: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	decks := make([]database.Deck, 0)
	for rows.Next() {
		var deck database.Deck
		var metadata string
		if scanErr := rows.Scan(
			&deck.DBID, &deck.DeckID, &deck.Name, &deck.Description, &deck.Owned, &metadata,
			&deck.SourceURL, &deck.FetchedAt, &deck.CreatedAt, &deck.UpdatedAt, &deck.ItemCount,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan deck: %w", scanErr)
		}
		if metadata != "" {
			deck.Metadata = json.RawMessage(metadata)
		}
		decks = append(decks, deck)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate decks: %w", err)
	}
	return decks, nil
}

func sqlListDeckItems(ctx context.Context, q deckQueryable, deckDBID int64) ([]database.DeckItem, error) {
	rows, err := q.QueryContext(ctx,
		`select `+deckItemColumns+` from DeckItems where DeckDBID = ? order by Position;`, deckDBID)
	if err != nil {
		return nil, fmt.Errorf("failed to query deck items: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	items := make([]database.DeckItem, 0)
	for rows.Next() {
		var item database.DeckItem
		var scripts, metadata, rawTags string
		if scanErr := rows.Scan(
			&item.DBID, &item.DeckDBID, &item.Position, &item.Kind, &item.Name, &item.ZapScript, &item.CardID,
			&scripts, &metadata, &item.Anchor.SystemID, &item.Anchor.Path, &item.Anchor.MediaName, &rawTags,
			&item.CreatedAt, &item.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan deck item: %w", scanErr)
		}
		item.Scripts = database.DecodeDeckCardScripts(scripts)
		if metadata != "" {
			item.Metadata = json.RawMessage(metadata)
		}
		item.Anchor.Path = pathutil.CanonicalMediaPath(item.Anchor.Path)
		item.Anchor.Tags = database.DecodeTagStrings(rawTags)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate deck items: %w", err)
	}
	return items, nil
}
