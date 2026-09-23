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
	"fmt"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// Every path that writes a tag checks it against the tag vocabulary
// (tags.ValidateTagValue) first. A refused tag is dropped and logged and the
// rest of the write goes ahead: one bad value must not cost a title all of its
// metadata. Scrapers and the filename parser are expected to have mapped their
// values already, so a refusal here means a source was not mapped properly.

// maxRefusedTagWarnings bounds how many distinct refused tags are logged at
// warn per process, so a scraper that maps nothing cannot flood the log.
const maxRefusedTagWarnings = 256

var refusedTagWarnings = struct {
	seen map[string]struct{}
	mu   syncutil.Mutex
}{seen: make(map[string]struct{})}

func warnRefusedTag(source, tagType, value string, err error) {
	key := source + "\x00" + tagType + "\x00" + value
	refusedTagWarnings.mu.Lock()
	_, seen := refusedTagWarnings.seen[key]
	full := len(refusedTagWarnings.seen) >= maxRefusedTagWarnings
	if !seen && !full {
		refusedTagWarnings.seen[key] = struct{}{}
	}
	refusedTagWarnings.mu.Unlock()
	if seen {
		return
	}
	event := log.Warn()
	if full {
		event = log.Debug()
	}
	event.Err(err).Str("source", source).Str("type", tagType).Str("value", value).
		Msg("tag refused: not in the tag vocabulary")
}

// acceptTagInfos returns the tags the vocabulary accepts, logging the rest.
// Labels are kept only on free-text types: a closed or format value is shared
// by every title that carries it, so one source's wording must not name it.
// The input slice is not modified.
func acceptTagInfos(source string, in []database.TagInfo) []database.TagInfo {
	var out []database.TagInfo
	for i, ti := range in {
		err := tags.ValidateTagValue(tags.TagType(ti.Type), ti.Tag)
		keepLabel := ti.Label == "" || isFreeTextType(ti.Type)
		if err == nil && keepLabel {
			if out != nil {
				out = append(out, ti)
			}
			continue
		}
		if out == nil {
			out = make([]database.TagInfo, i, len(in))
			copy(out, in[:i])
		}
		if err != nil {
			warnRefusedTag(source, ti.Type, ti.Tag, err)
			continue
		}
		ti.Label = ""
		out = append(out, ti)
	}
	if out == nil {
		return in
	}
	return out
}

func isFreeTextType(tagType string) bool {
	rule, ok := tags.RuleFor(tags.TagType(tagType))
	return ok && rule.Kind == tags.KindFreeText
}

// acceptScrapeTargets applies acceptTagInfos to every target's tags, copying a
// write only when something in it changes. A sentinel the vocabulary refuses
// is an error: it is Core's own bookkeeping, never source data.
func acceptScrapeTargets(
	method string, targets []database.ScrapeWriteTarget,
) ([]database.ScrapeWriteTarget, error) {
	out := targets
	copied := false
	for i := range targets {
		write := targets[i].Write
		if err := tags.ValidateTagValue(tags.TagType(write.Sentinel.Type), write.Sentinel.Tag); err != nil {
			return nil, fmt.Errorf("%s: sentinel: %w", method, err)
		}
		mediaTags := acceptTagInfos(write.Sentinel.Type, write.MediaTags)
		titleTags := acceptTagInfos(write.Sentinel.Type, write.TitleTags)
		if sameTagSlice(mediaTags, write.MediaTags) && sameTagSlice(titleTags, write.TitleTags) {
			continue
		}
		if !copied {
			out = make([]database.ScrapeWriteTarget, len(targets))
			copy(out, targets)
			copied = true
		}
		accepted := *write
		accepted.MediaTags = mediaTags
		accepted.TitleTags = titleTags
		out[i].Write = &accepted
	}
	return out, nil
}

// sameTagSlice reports whether acceptTagInfos returned its input unchanged.
func sameTagSlice(a, b []database.TagInfo) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// scanCreatableTagTypes are the types whose values the scanner may create as
// new Tags rows during reconcile: every type that is not a closed list. Closed
// values are seeded, so a staged closed value that has no row cannot be valid.
// The scanner only stages values the vocabulary accepts (StageScannedMedia),
// so what it creates here always passes the type's rule.
var scanCreatableTagTypes = func() []string {
	var out []string
	for tagType, rule := range tags.TagRules {
		if rule.Kind == tags.KindClosed || tagType == tags.TagTypeUser {
			continue
		}
		out = append(out, string(tagType))
	}
	sort.Strings(out)
	return out
}()

// sqlPruneOffVocabularyTags reports whether it changed anything. It deletes
// every tag type Core no longer defines and
// every tag value its type no longer accepts, with the links to them. It runs
// when the vocabulary stamp changes (sqlSeedCanonicalTags), so a release that
// narrows the vocabulary cleans existing databases rather than leaving old
// values reachable through filters and tag lists. Property tags are excluded:
// they are Core's own schema keys, checked when they are resolved.
func sqlPruneOffVocabularyTags(ctx context.Context, db sqlQueryable) (bool, error) {
	refused, err := readRefusedTagIDs(ctx, db)
	if err != nil {
		return false, err
	}

	const chunkSize = 500
	for start := 0; start < len(refused); start += chunkSize {
		chunk := refused[start:min(start+chunkSize, len(refused))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		holders := prepareVariadic("?", ",", len(chunk))
		for _, table := range []string{"MediaTags", "MediaTitleTags", "Tags"} {
			column := "TagDBID"
			if table == "Tags" {
				column = "DBID"
			}
			//nolint:gosec // table and column are constants; holders are "?" placeholders.
			query := fmt.Sprintf("DELETE FROM %s WHERE %s IN (%s)", table, column, holders)
			if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
				return false, fmt.Errorf("failed to prune %s: %w", table, execErr)
			}
		}
	}

	unknownTypes, err := readUnknownTagTypeIDs(ctx, db)
	if err != nil {
		return false, err
	}
	for _, id := range unknownTypes {
		// Their tags were all refused above, so no link still points at them.
		if _, execErr := db.ExecContext(ctx, "DELETE FROM TagTypes WHERE DBID = ?", id); execErr != nil {
			return false, fmt.Errorf("failed to prune tag type: %w", execErr)
		}
	}

	// A label names one source's wording. Only free-text values keep one; a
	// closed or format value is shared by every title that carries it.
	res, err := db.ExecContext(ctx, `
		UPDATE Tags SET DisplayName = ''
		WHERE DisplayName != '' AND TypeDBID IN (
			SELECT DBID FROM TagTypes WHERE Type NOT IN (?, ?, ?, ?))`,
		string(tags.TagTypeDeveloper), string(tags.TagTypePublisher), string(tags.TagTypeCredit),
		string(tags.TagTypeProperty))
	if err != nil {
		return false, fmt.Errorf("failed to clear labels on shared tags: %w", err)
	}
	cleared, err := res.RowsAffected()
	if err != nil {
		cleared = 0
	}

	if len(refused) > 0 || len(unknownTypes) > 0 || cleared > 0 {
		log.Info().Int("tags", len(refused)).Int("types", len(unknownTypes)).Int64("labels", cleared).
			Msg("removed tags that are no longer in the tag vocabulary")
	}
	return len(refused) > 0 || len(unknownTypes) > 0 || cleared > 0, nil
}

// readRefusedTagIDs returns every non-property tag whose value its type's rule
// refuses, including every tag of a type Core does not define.
func readRefusedTagIDs(ctx context.Context, db sqlQueryable) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT t.DBID, tt.Type, t.Tag
		FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type != ?`, string(tags.TagTypeProperty))
	if err != nil {
		return nil, fmt.Errorf("failed to read tags for vocabulary pruning: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tag rows")
		}
	}()
	var refused []int64
	for rows.Next() {
		var id int64
		var tagType, value string
		if scanErr := rows.Scan(&id, &tagType, &value); scanErr != nil {
			return nil, fmt.Errorf("failed to scan tag for vocabulary pruning: %w", scanErr)
		}
		if !tags.IsValidTagValue(tags.TagType(tagType), value) {
			refused = append(refused, id)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate tags for vocabulary pruning: %w", rowsErr)
	}
	return refused, nil
}

// readUnknownTagTypeIDs returns every tag type Core does not define.
func readUnknownTagTypeIDs(ctx context.Context, db sqlQueryable) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT DBID, Type FROM TagTypes`)
	if err != nil {
		return nil, fmt.Errorf("failed to read tag types for vocabulary pruning: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close tag type rows")
		}
	}()
	var unknown []int64
	for rows.Next() {
		var id int64
		var tagType string
		if scanErr := rows.Scan(&id, &tagType); scanErr != nil {
			return nil, fmt.Errorf("failed to scan tag type for vocabulary pruning: %w", scanErr)
		}
		if !tags.IsKnownType(tags.TagType(tagType)) {
			unknown = append(unknown, id)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failed to iterate tag types for vocabulary pruning: %w", rowsErr)
	}
	return unknown, nil
}
