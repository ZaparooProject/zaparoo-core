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
	"cmp"
	"strconv"
	"strings"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
)

const (
	// searchSortAddedDesc orders a flag's list by when each game got the
	// flag, newest first.
	searchSortAddedDesc = "added-desc"
	// addedSearchPage is how many rows one read of the flagged set asks for,
	// and addedSearchMaxPages bounds the reads, so a set is read whole.
	addedSearchPage     = 1000
	addedSearchMaxPages = 100
)

// addedSortFlags are the flags that record when they were set, in the order
// they are looked for.
var addedSortFlags = map[string]database.MediaUserFlag{
	string(tags.TagUserFavorite):  database.MediaUserFlagFavorite,
	string(tags.TagUserLiked):     database.MediaUserFlagLiked,
	string(tags.TagUserDisliked):  database.MediaUserFlagDisliked,
	string(tags.TagUserPlayLater): database.MediaUserFlagPlayLater,
}

// addedSortFlag returns the flag an added-desc search orders by: the first
// positive filter on a flag that records when it was set.
func addedSortFlag(filters []zapscript.TagFilter) (database.MediaUserFlag, bool) {
	for _, filter := range filters {
		if filter.Type != string(tags.TagTypeUser) || filter.Operator != zapscript.TagOperatorAND {
			continue
		}
		if flag, ok := addedSortFlags[filter.Value]; ok {
			return flag, true
		}
	}
	return "", false
}

// addedSortValue is the cursor sort value of an added-desc row: when the flag
// was set (0 for unknown) and the row's name sort value.
func addedSortValue(setAt int64, name string) string {
	return strconv.FormatInt(setAt, 10) + ":" + name
}

func parseAddedSortValue(value string) (setAt int64, name string, err error) {
	rawAt, name, found := strings.Cut(value, ":")
	if !found {
		return 0, "", models.ClientErrf("invalid cursor: malformed added-desc position")
	}
	setAt, err = strconv.ParseInt(rawAt, 10, 64)
	if err != nil || setAt < 0 {
		return 0, "", models.ClientErrf("invalid cursor: malformed added-desc position")
	}
	return setAt, name, nil
}

// compareAdded orders rows by set time, newest first, then rows with no time
// after every dated row, each run by name ascending and then by media ID.
func compareAdded(aAt int64, aName string, aID, bAt int64, bName string, bID int64) int {
	switch {
	case aAt > 0 && bAt == 0:
		return -1
	case aAt == 0 && bAt > 0:
		return 1
	case aAt != bAt:
		return cmp.Compare(bAt, aAt)
	}
	if order := caseFoldCompare(aName, bName); order != 0 {
		return order
	}
	return cmp.Compare(aID, bID)
}

// caseFoldCompare compares ASCII case-insensitively, as the name sort does.
func caseFoldCompare(a, b string) int {
	limit := min(len(a), len(b))
	for i := range limit {
		left, right := a[i], b[i]
		if left >= 'A' && left <= 'Z' {
			left += 'a' - 'A'
		}
		if right >= 'A' && right <= 'Z' {
			right += 'a' - 'A'
		}
		if order := cmp.Compare(left, right); order != 0 {
			return order
		}
	}
	return cmp.Compare(len(a), len(b))
}
