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
	"encoding/base64"
	"testing"
)

func FuzzBrowseVisibilityCursor(f *testing.F) {
	for _, raw := range []string{
		`{}`, `{"lastId":1,"totalFiles":2}`, `{"includeHidden":true,"preferencesRevision":"1"}`, `null`,
	} {
		f.Add(base64.StdEncoding.EncodeToString([]byte(raw)), false)
	}
	f.Add("invalid", true)
	f.Fuzz(func(t *testing.T, cursor string, includeHidden bool) {
		before, err := readBrowseCursorData(cursor)
		if err != nil {
			return
		}
		stamped, err := stampVisibilityCursor(cursor, "42", includeHidden)
		if err != nil {
			t.Fatal(err)
		}
		after, err := readBrowseCursorData(stamped)
		if err != nil {
			t.Fatal(err)
		}
		if after.IncludeHidden == nil || *after.IncludeHidden != includeHidden || after.PreferencesRevision != "42" {
			t.Fatal("visibility scope lost during round trip")
		}
		before.IncludeHidden, after.IncludeHidden = nil, nil
		before.PreferencesRevision, after.PreferencesRevision = "", ""
		if before != after {
			t.Fatal("cursor pagination fields changed")
		}
	})
}
