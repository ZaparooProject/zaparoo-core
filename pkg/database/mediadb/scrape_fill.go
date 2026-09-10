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

package mediadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// These helpers run inside the ordinary scrape transaction. Missing means no
// stored field, not an empty value or artwork whose file is temporarily absent.
func fillMissingScrapeTarget(ctx context.Context, c *scrapeWriteTxContext, target database.ScrapeWriteTarget) error {
	write := target.Write
	if err := fillMissingScrapeTags(ctx, c, target.MediaDBID, false, write.MediaTags); err != nil {
		return err
	}
	if err := fillMissingScrapeTags(ctx, c, target.MediaTitleDBID, true, write.TitleTags); err != nil {
		return err
	}
	if err := fillMissingScrapeProperties(ctx, c, target.MediaDBID, false, write.MediaProps); err != nil {
		return err
	}
	if err := fillMissingScrapeProperties(ctx, c, target.MediaTitleDBID, true, write.TitleProps); err != nil {
		return err
	}
	return upsertMediaTagsWithContext(ctx, c, target.MediaDBID, []database.TagInfo{write.Sentinel})
}

func fillMissingScrapeTags(
	ctx context.Context, c *scrapeWriteTxContext, id int64, title bool, values []database.TagInfo,
) error {
	table, column := "MediaTags", "MediaDBID"
	if title {
		table, column = "MediaTitleTags", "MediaTitleDBID"
	}
	for _, tag := range values {
		typeID, exclusive, err := c.resolveTagType(ctx, tag.Type)
		if err != nil {
			return err
		}
		if exclusive {
			var exists bool
			err = c.tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+
				" m JOIN Tags t ON t.DBID=m.TagDBID WHERE m."+column+"=? AND t.TypeDBID=?)", id, typeID).Scan(&exists)
			if err != nil {
				return fmt.Errorf("check missing scrape tag: %w", err)
			}
			if exists {
				continue
			}
		}
		// Reusing a global tag must not replace its display label for other media.
		var tagID int64
		err = c.tx.QueryRowContext(ctx,
			"SELECT DBID FROM Tags WHERE TypeDBID=? AND Tag=?", typeID, tag.Tag).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID, err = c.resolveTag(ctx, typeID, tag.Type, tag.Tag, tag.Label)
		}
		if err != nil {
			return fmt.Errorf("resolve missing scrape tag: %w", err)
		}
		if _, err := c.tx.ExecContext(ctx, "INSERT OR IGNORE INTO "+table+" ("+column+", TagDBID) VALUES (?, ?)",
			id, tagID); err != nil {
			return fmt.Errorf("insert missing scrape tag: %w", err)
		}
	}
	return nil
}

func fillMissingScrapeProperties(
	ctx context.Context, c *scrapeWriteTxContext, id int64, title bool, values []database.MediaProperty,
) error {
	table, column := "MediaProperties", "MediaDBID"
	if title {
		table, column = "MediaTitleProperties", "MediaTitleDBID"
	}
	for _, property := range values {
		typeID, err := c.resolvePropertyTypeTag(ctx, property.TypeTag)
		if err != nil {
			return err
		}
		result, err := c.tx.ExecContext(ctx, "INSERT INTO "+table+" ("+column+", TypeTagDBID, Text, BlobDBID)"+
			" VALUES (?, ?, ?, ?) ON CONFLICT("+column+", TypeTagDBID) DO NOTHING",
			id, typeID, property.Text, property.BlobDBID)
		if err != nil {
			return fmt.Errorf("insert missing scrape property: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read inserted scrape properties: %w", err)
		}
		if changed > 0 && isImageProperty(property.TypeTag) {
			if title {
				c.changedImageMediaTitleIDs[id] = struct{}{}
			} else {
				c.changedImageMediaIDs[id] = struct{}{}
			}
		}
	}
	return nil
}
