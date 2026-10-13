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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeZoneFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

// zoneFixture is a zone database in a temp directory with the machine's zone
// file beside it.
type zoneFixture struct {
	zoneinfo  string
	localtime string
}

func newZoneFixture(t *testing.T) zoneFixture {
	t.Helper()
	dir := t.TempDir()
	return zoneFixture{
		zoneinfo:  filepath.Join(dir, "usr", "share", "zoneinfo"),
		localtime: filepath.Join(dir, "localtime"),
	}
}

func (f zoneFixture) zone(t *testing.T, name, contents string) {
	t.Helper()
	writeZoneFile(t, filepath.Join(f.zoneinfo, filepath.FromSlash(name)), "TZif"+contents)
}

func (f zoneFixture) table(t *testing.T, name string, zones ...string) {
	t.Helper()
	var text strings.Builder
	text.WriteString("# header\n")
	for _, zone := range zones {
		text.WriteString("XX\t+0000+00000\t" + zone + "\tcomment\n")
	}
	writeZoneFile(t, filepath.Join(f.zoneinfo, name), text.String())
}

func (f zoneFixture) copyOf(t *testing.T, contents string) {
	t.Helper()
	writeZoneFile(t, f.localtime, "TZif"+contents)
}

func TestZoneCache_LinkIntoZoneDatabase(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	require.NoError(t, os.Symlink(filepath.Join(f.zoneinfo, "Australia", "Perth"), f.localtime))

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_LinkIntoVariantDirectory(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "posix/Australia/Perth", "perth")
	require.NoError(t, os.Symlink(filepath.Join(f.zoneinfo, "posix", "Australia", "Perth"), f.localtime))

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_CopiedFile(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	f.zone(t, "Australia/Sydney", "sydney")
	f.zone(t, "posix/Australia/Perth", "perth")
	f.zone(t, "right/Australia/Perth", "perth")
	f.copyOf(t, "perth")

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_LinkOutsideZoneDatabase(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	copied := filepath.Join(filepath.Dir(f.localtime), "linux", "timezone")
	writeZoneFile(t, copied, "TZifperth")
	require.NoError(t, os.Symlink(copied, f.localtime))

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_AliasesResolveToListedZone(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	f.zone(t, "Australia/West", "perth")
	f.zone(t, "posixrules", "perth")
	f.table(t, "zone1970.tab", "Australia/Perth")
	f.copyOf(t, "perth")

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_FallsBackToCountryTable(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Europe/Berlin", "cet")
	f.zone(t, "Europe/Oslo", "cet")
	f.table(t, "zone1970.tab", "America/New_York")
	f.table(t, "zone.tab", "Europe/Oslo")
	f.copyOf(t, "cet")

	var cache zoneCache
	assert.Equal(t, "Europe/Oslo", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_AmbiguousIsUnknown(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Europe/Berlin", "cet")
	f.zone(t, "Europe/Oslo", "cet")
	f.table(t, "zone.tab", "Europe/Berlin", "Europe/Oslo")
	f.copyOf(t, "cet")

	var cache zoneCache
	assert.Empty(t, cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_NoMatchIsUnknown(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	f.copyOf(t, "somewhere else")

	var cache zoneCache
	assert.Empty(t, cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_NotAZoneFileIsUnknown(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	writeZoneFile(t, filepath.Join(f.zoneinfo, "Australia", "Perth"), "not a zone file")
	writeZoneFile(t, f.localtime, "not a zone file")

	var cache zoneCache
	assert.Empty(t, cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_MissingFilesAreUnknown(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)

	var cache zoneCache
	assert.Empty(t, cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_WalksOnlyWhenTheFileChanges(t *testing.T) {
	t.Parallel()

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	f.zone(t, "Australia/Sydney", "sydney")
	f.copyOf(t, "perth")

	var cache zoneCache
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))

	// The database losing the zone cannot change the answer while the zone file
	// is the same, because nothing is read again.
	require.NoError(t, os.Remove(filepath.Join(f.zoneinfo, "Australia", "Perth")))
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))

	f.copyOf(t, "sydney")
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(f.localtime, later, later))
	assert.Equal(t, "Australia/Sydney", cache.name(f.localtime, f.zoneinfo))
}

func TestZoneCache_IncompleteWalkIsUnknownAndNotCached(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}

	f := newZoneFixture(t)
	f.zone(t, "Australia/Perth", "perth")
	f.zone(t, "Australia/West", "perth")
	unreadable := filepath.Join(f.zoneinfo, "Australia", "West")
	require.NoError(t, os.Chmod(unreadable, 0o000))
	f.table(t, "zone1970.tab", "Australia/Perth")
	f.copyOf(t, "perth")

	var cache zoneCache
	assert.Empty(t, cache.name(f.localtime, f.zoneinfo))

	require.NoError(t, os.Chmod(unreadable, 0o600))
	assert.Equal(t, "Australia/Perth", cache.name(f.localtime, f.zoneinfo))
}
