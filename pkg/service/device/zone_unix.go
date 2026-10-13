//go:build !windows

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

package device

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const (
	zoneinfoDir = "/usr/share/zoneinfo"
	// tzifMagic starts every compiled zone file.
	tzifMagic = "TZif"
)

// zoneTables list the zones worth naming, most selective first: zone1970.tab
// holds one representative for each group of zones that agree since 1970, and
// zone.tab one for each country.
var zoneTables = []string{"zone1970.tab", "zone.tab"}

// zoneinfoNonZones are files in the zone database that copy another zone
// rather than name one.
var zoneinfoNonZones = map[string]struct{}{
	"localtime":  {},
	"posixrules": {},
}

// systemZones remembers which zone the machine's zone file matched, so the
// zone database is only walked again after the file changes.
var systemZones zoneCache

// systemZoneName reads the zone name from where /etc/localtime points, which
// is how most Unix systems record it. Where the file is a copy of a zone file
// instead, as MiSTer keeps it, the name is the zone whose file it matches.
func systemZoneName() string {
	return systemZones.name(filepath.Join(string(filepath.Separator), "etc", "localtime"), zoneinfoDir)
}

type zoneCacheKey struct {
	modTime   time.Time
	localtime string
	zoneinfo  string
	size      int64
}

type zoneCache struct {
	value string
	key   zoneCacheKey
	mu    syncutil.Mutex
	set   bool
}

// name returns the IANA name of the zone that localtime is set to, or "" when
// it cannot be told.
func (c *zoneCache) name(localtime, zoneinfo string) string {
	if target, err := os.Readlink(localtime); err == nil {
		if name := zoneNameFromPath(target); name != "" {
			return name
		}
	}

	info, err := os.Stat(localtime)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	key := zoneCacheKey{localtime: localtime, zoneinfo: zoneinfo, modTime: info.ModTime(), size: info.Size()}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set && c.key == key {
		return c.value
	}
	value, complete := matchZoneFile(localtime, zoneinfo)
	if !complete {
		// A database that could not be read in full may read fine next time.
		return ""
	}
	c.key, c.value, c.set = key, value, true
	return c.value
}

// zoneNameFromPath takes the zone name from the part of a path that follows
// the zone database directory.
func zoneNameFromPath(path string) string {
	_, name, found := strings.Cut(filepath.ToSlash(path), "zoneinfo/")
	if !found {
		return ""
	}
	return trimZoneVariant(name)
}

// trimZoneVariant drops the directory a zone database keeps its two variants
// of each zone in, which is not part of the name.
func trimZoneVariant(name string) string {
	for _, variant := range []string{"posix/", "right/"} {
		if rest, found := strings.CutPrefix(name, variant); found {
			return rest
		}
	}
	return name
}

// matchZoneFile finds the zone whose file has the same contents as the
// machine's. Zones that share a file are the same rules under different names,
// so the one listed in the zone tables wins, and nothing is returned when that
// still leaves more than one. The second result is false when the zone database
// could not be read in full, in which case no name is given: a zone that went
// unread might have been a second match.
func matchZoneFile(localtime, zoneinfo string) (string, bool) {
	want, err := os.ReadFile(localtime) //nolint:gosec // G304: a fixed system path
	if err != nil {
		return "", false
	}
	if !bytes.HasPrefix(want, []byte(tzifMagic)) {
		return "", true
	}

	candidates := map[string]struct{}{}
	complete := true
	walkErr := filepath.WalkDir(zoneinfo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Keep scanning what can be read, but remember the gap.
			complete = false
			return nil //nolint:nilerr // the gap is recorded in complete
		}
		if entry.IsDir() && entry.Name() == "right" && path != zoneinfo {
			return filepath.SkipDir
		}
		name, matched, err := sameZoneFile(zoneinfo, path, entry, want)
		if err != nil {
			complete = false
			return nil //nolint:nilerr // the gap is recorded in complete
		}
		if matched {
			candidates[name] = struct{}{}
		}
		return nil
	})
	if walkErr != nil || !complete {
		return "", false
	}

	for _, table := range zoneTables {
		if len(candidates) <= 1 {
			break
		}
		listed := listedZones(filepath.Join(zoneinfo, table))
		narrowed := map[string]struct{}{}
		for name := range candidates {
			if _, ok := listed[name]; ok {
				narrowed[name] = struct{}{}
			}
		}
		if len(narrowed) == 1 {
			candidates = narrowed
		}
	}
	if len(candidates) != 1 {
		return "", true
	}
	for name := range candidates {
		return name, true
	}
	return "", true
}

// sameZoneFile reports whether a file in the zone database is a zone with
// exactly the given contents, and the name of that zone if so. The error is
// set when the file could not be checked.
func sameZoneFile(zoneinfo, path string, entry fs.DirEntry, want []byte) (name string, matched bool, err error) {
	if !entry.Type().IsRegular() {
		return "", false, nil
	}
	if _, skip := zoneinfoNonZones[entry.Name()]; skip {
		return "", false, nil
	}
	info, err := entry.Info()
	if err != nil {
		return "", false, fmt.Errorf("stat zone file: %w", err)
	}
	if info.Size() != int64(len(want)) {
		return "", false, nil
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: a file found inside the zone database
	if err != nil {
		return "", false, fmt.Errorf("read zone file: %w", err)
	}
	if !bytes.Equal(got, want) {
		return "", false, nil
	}
	rel, err := filepath.Rel(zoneinfo, path)
	if err != nil {
		return "", false, fmt.Errorf("name zone file: %w", err)
	}
	return trimZoneVariant(filepath.ToSlash(rel)), true, nil
}

// listedZones reads the zone names from a zone table, whose third column is
// the name.
func listedZones(path string) map[string]struct{} {
	data, err := os.ReadFile(path) //nolint:gosec // G304: a table inside the zone database
	if err != nil {
		return nil
	}
	zones := map[string]struct{}{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) > 2 {
			zones[fields[2]] = struct{}{}
		}
	}
	return zones
}
