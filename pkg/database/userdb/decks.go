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
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
)

const deckColumns = `DBID, DeckID, Name, Description, Owned, Metadata, SourceURL, FetchedAt, CreatedAt, UpdatedAt`

const deckItemColumns = `DBID, DeckDBID, Position, Kind, Name, ZapScript, CardID, Scripts, Metadata,
	SystemID, Path, MediaName, Tags, CreatedAt, UpdatedAt`

// CreateDeck inserts an owned deck with its items. The deck's DeckID must
// already be minted and normalized. Fails with ErrDeckLimit when the device
// holds DeckMaxLive owned decks, and ErrDeckItemLimit past DeckMaxItems.
func (db *UserDB) CreateDeck(deck *database.Deck) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if len(deck.Items) > database.DeckMaxItems {
		return database.ErrDeckItemLimit
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) error {
		if deck.Owned {
			count, err := sqlCountOwnedDecks(ctx, tx)
			if err != nil {
				return err
			}
			if count >= database.DeckMaxLive {
				return database.ErrDeckLimit
			}
		}
		deck.CreatedAt, deck.UpdatedAt = now, now
		if err := sqlInsertDeck(ctx, tx, deck); err != nil {
			return err
		}
		return sqlInsertDeckItems(ctx, tx, deck.DBID, deck.Items, now)
	})
}

// GetDeck returns a deck with its items in position order.
func (db *UserDB) GetDeck(deckID string) (*database.Deck, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	deck, err := sqlGetDeck(db.ctx, db.sql.Load(), deckID)
	if err != nil {
		return nil, err
	}
	deck.Items, err = sqlListDeckItems(db.ctx, db.sql.Load(), deck.DBID)
	if err != nil {
		return nil, err
	}
	return deck, nil
}

// ListDecks returns every deck without items, most recently updated first,
// with ItemCount filled in.
func (db *UserDB) ListDecks() ([]database.Deck, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	return sqlListDecks(db.ctx, db.sql.Load())
}

// UpdateDeckMeta replaces a deck's name, description and metadata. Metadata
// nil leaves the stored value alone.
func (db *UserDB) UpdateDeckMeta(deckID, name, description string, metadata json.RawMessage) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) error {
		res, err := tx.ExecContext(ctx, `
			update Decks set Name = ?, Description = ?,
				Metadata = case when ? then Metadata else ? end,
				UpdatedAt = ?
			where DeckID = ?;`,
			name, description, metadata == nil, string(metadata), now, deckID)
		if err != nil {
			return fmt.Errorf("failed to update deck: %w", err)
		}
		return requireDeckAffected(res)
	})
}

// ReplaceDeckItems replaces a deck's whole item list, renumbering positions
// from one in the order given.
func (db *UserDB) ReplaceDeckItems(deckID string, items []database.DeckItem) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if len(items) > database.DeckMaxItems {
		return database.ErrDeckItemLimit
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) error {
		deck, err := sqlGetDeck(ctx, tx, deckID)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `delete from DeckItems where DeckDBID = ?;`, deck.DBID); err != nil {
			return fmt.Errorf("failed to clear deck items: %w", err)
		}
		if insertErr := sqlInsertDeckItems(ctx, tx, deck.DBID, items, now); insertErr != nil {
			return insertErr
		}
		if _, err = tx.ExecContext(ctx, `update Decks set UpdatedAt = ? where DBID = ?;`, now, deck.DBID); err != nil {
			return fmt.Errorf("failed to touch deck: %w", err)
		}
		return nil
	})
}

// DeleteDeck removes a deck and its items. The bool reports whether a deck
// existed.
func (db *UserDB) DeleteDeck(deckID string) (bool, error) {
	if db.sql.Load() == nil {
		return false, ErrNullSQL
	}
	existed := false
	err := db.deckTx(func(ctx context.Context, tx *sql.Tx, _ int64) error {
		deck, err := sqlGetDeck(ctx, tx, deckID)
		if errors.Is(err, database.ErrDeckNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `delete from DeckItems where DeckDBID = ?;`, deck.DBID); err != nil {
			return fmt.Errorf("failed to delete deck items: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `delete from Decks where DBID = ?;`, deck.DBID); err != nil {
			return fmt.Errorf("failed to delete deck: %w", err)
		}
		existed = true
		return nil
	})
	return existed, err
}

// UpsertRemoteDeck inserts or fully replaces a deck that arrived from
// elsewhere (a ZapLink fetch or a sync pull), keeping the stored DBID and
// creation time. A cached copy of somebody else's deck never overwrites an
// owned deck of the same ID (ErrDeckOwned), so a tap on your own deck's
// ZapLink opens the editable local copy. FetchedAt is set to now.
func (db *UserDB) UpsertRemoteDeck(deck *database.Deck) error {
	if db.sql.Load() == nil {
		return ErrNullSQL
	}
	if len(deck.Items) > database.DeckMaxItems {
		return database.ErrDeckItemLimit
	}
	return db.deckTx(func(ctx context.Context, tx *sql.Tx, now int64) error {
		existing, err := sqlGetDeck(ctx, tx, deck.DeckID)
		switch {
		case errors.Is(err, database.ErrDeckNotFound):
			deck.CreatedAt, deck.UpdatedAt, deck.FetchedAt = now, now, now
			if insertErr := sqlInsertDeck(ctx, tx, deck); insertErr != nil {
				return insertErr
			}
		case err != nil:
			return err
		case existing.Owned && !deck.Owned:
			return database.ErrDeckOwned
		default:
			deck.DBID, deck.CreatedAt, deck.UpdatedAt, deck.FetchedAt = existing.DBID, existing.CreatedAt, now, now
			if _, err = tx.ExecContext(ctx, `
				update Decks set Name = ?, Description = ?, Owned = ?, Metadata = ?, SourceURL = ?,
					FetchedAt = ?, UpdatedAt = ?
				where DBID = ?;`,
				deck.Name, deck.Description, deck.Owned, string(deck.Metadata), deck.SourceURL,
				now, now, existing.DBID); err != nil {
				return fmt.Errorf("failed to update remote deck: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `delete from DeckItems where DeckDBID = ?;`, existing.DBID); err != nil {
				return fmt.Errorf("failed to clear deck items: %w", err)
			}
		}
		return sqlInsertDeckItems(ctx, tx, deck.DBID, deck.Items, now)
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

// ListDeckItemLinks returns every script item across all decks with its
// anchor, for re-applying deck membership tags after a reindex.
func (db *UserDB) ListDeckItemLinks() ([]database.DeckItemLink, error) {
	if db.sql.Load() == nil {
		return nil, ErrNullSQL
	}
	rows, err := db.sql.Load().QueryContext(db.ctx, `
		select d.DeckID, i.DBID, i.Kind, i.ZapScript, i.SystemID, i.Path, i.MediaName, i.Tags
		from DeckItems i join Decks d on d.DBID = i.DeckDBID
		order by d.DBID, i.Position;`)
	if err != nil {
		return nil, fmt.Errorf("failed to query deck item links: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close sql rows")
		}
	}()
	links := make([]database.DeckItemLink, 0)
	for rows.Next() {
		var link database.DeckItemLink
		var rawTags string
		if scanErr := rows.Scan(
			&link.DeckID, &link.ItemDBID, &link.Kind, &link.ZapScript,
			&link.Anchor.SystemID, &link.Anchor.Path, &link.Anchor.MediaName, &rawTags,
		); scanErr != nil {
			return nil, fmt.Errorf("failed to scan deck item link: %w", scanErr)
		}
		link.Anchor.Path = pathutil.CanonicalMediaPath(link.Anchor.Path)
		link.Anchor.Tags = database.DecodeTagStrings(rawTags)
		links = append(links, link)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate deck item links: %w", err)
	}
	return links, nil
}

// CountOwnedDecks returns how many decks this device owns.
func (db *UserDB) CountOwnedDecks() (int, error) {
	if db.sql.Load() == nil {
		return 0, ErrNullSQL
	}
	return sqlCountOwnedDecks(db.ctx, db.sql.Load())
}

// deckTx runs fn in one transaction and, on success, advances the media
// preferences revision: deck membership is a listing-affecting tag, so
// browse cursors must be invalidated like any other preference change.
func (db *UserDB) deckTx(fn func(ctx context.Context, tx *sql.Tx, now int64) error) (err error) {
	tx, err := db.sql.Load().BeginTx(db.ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin deck transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().Unix()
	if fnErr := fn(db.ctx, tx, now); fnErr != nil {
		err = fnErr
		return err
	}
	if revErr := sqlAdvanceMediaPreferencesRevision(db.ctx, tx, now); revErr != nil {
		err = revErr
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit deck transaction: %w", err)
	}
	return nil
}

func requireDeckAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read affected deck rows: %w", err)
	}
	if affected == 0 {
		return database.ErrDeckNotFound
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

func sqlInsertDeckItems(
	ctx context.Context, q deckQueryable, deckDBID int64, items []database.DeckItem, now int64,
) error {
	for i := range items {
		item := &items[i]
		item.DeckDBID = deckDBID
		item.Position = i + 1
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
