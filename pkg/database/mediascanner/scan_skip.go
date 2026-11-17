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

package mediascanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/arcadenames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scummvmnames"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// scanRulesVersion identifies the code that turns a walked entry into stored
// rows: filename, title, slug and tag parsing, and the reconcile that writes
// them. Bump it with any change to what a file is stored as, so the next
// index reconciles systems whose files did not change.
// TestScanRules_VersionCoversParsedOutput fails when the parsed form of its
// sample files changes without a bump.
//
// It is a variable so a test can stand in for a later release.
var scanRulesVersion = 2

// scanWalkMarker describes everything a system's reconcile is derived from:
// each walked entry as the scanners reported it, and the settings, code and
// name catalogs that turn an entry into rows. A system whose walk produces
// the marker its last reconcile stored has nothing for another reconcile to
// change.
//
// The catalogs carry their own versions and the code carries
// scanRulesVersion, so a Core upgrade that changes neither leaves unchanged
// systems alone.
//
// ok is false for a system the scanner must always reconcile: one with linked
// media, whose rows also follow their link targets' scraped data.
func scanWalkMarker(cfg *config.Instance, files []platforms.ScanResult) (marker string, ok bool) {
	records := make([]string, 0, len(files))
	for i := range files {
		file := &files[i]
		if file.LinkTarget != "" {
			return "", false
		}
		source := ""
		if file.Source != nil {
			source = fmt.Sprintf("%+v", *file.Source)
		}
		records = append(records, strings.Join([]string{
			file.Path, file.Name, strconv.FormatBool(file.NoExt), source,
		}, "\x00"))
	}
	sort.Strings(records)

	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "rules=%d\narcade=%s\nscummvm=%s\nfilenameTags=%t\nentries=%d\n",
		scanRulesVersion, arcadenames.Version(), scummvmnames.Version(),
		cfg != nil && cfg.FilenameTags(), len(records))
	for _, record := range records {
		_, _ = digest.Write([]byte(record))
		_, _ = digest.Write([]byte{'\n'})
	}
	return hex.EncodeToString(digest.Sum(nil)), true
}
