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

package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const overridesDir = "artwork-overrides"

var revision atomic.Uint64

// Revision changes when an import publishes a new override. Image requests
// use it to keep in-flight lookups from restoring stale thumbnail memo entries.
func Revision() uint64 { return revision.Load() }

func stableKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func systemDir(dataDir, system string) string {
	return filepath.Join(dataDir, overridesDir, stableKey(strings.ToLower(system)))
}

func assignmentDir(dataDir, system, scope, identity string) string {
	return filepath.Join(systemDir(dataDir, system), scope, stableKey(identity))
}

// assignmentIndex lists the hashed identities that have override folders in
// one system, so requests for every other game skip the SD card entirely.
type assignmentIndex struct {
	versions map[string]struct{}
	titles   map[string]struct{}
	revision uint64
}

func (idx *assignmentIndex) empty() bool {
	return len(idx.versions) == 0 && len(idx.titles) == 0
}

var (
	indexMu syncutil.Mutex
	indexes = make(map[string]*assignmentIndex)
)

func listAssignments(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug().Err(err).Str("dir", dir).Msg("list artwork assignments")
		}
		return nil
	}
	keys := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			keys[entry.Name()] = struct{}{}
		}
	}
	return keys
}

// systemIndex returns the system's assignment index, rebuilding it after an
// import publishes a new override.
func systemIndex(dataDir, system string) *assignmentIndex {
	dir := systemDir(dataDir, system)
	current := revision.Load()
	indexMu.Lock()
	defer indexMu.Unlock()
	if idx, ok := indexes[dir]; ok && idx.revision == current {
		return idx
	}
	// The revision is read before listing, so an import that lands during the
	// listing leaves a stale revision and the next request lists again.
	idx := &assignmentIndex{
		revision: current,
		versions: listAssignments(filepath.Join(dir, scopeVersion)),
		titles:   listAssignments(filepath.Join(dir, scopeTitle)),
	}
	indexes[dir] = idx
	return idx
}

func (idx *assignmentIndex) hasTitle(slugs []string) bool {
	for _, slug := range slugs {
		if _, ok := idx.titles[stableKey(slug)]; ok {
			return true
		}
	}
	return false
}

// HasOverride reports whether a media row has any imported artwork, loading
// the row's title group only when the system has title-wide artwork.
func HasOverride(
	ctx context.Context, db database.MediaDBI, dataDir, system, path string, mediaDBID int64,
) (bool, error) {
	idx := systemIndex(dataDir, system)
	if idx.empty() {
		return false, nil
	}
	if _, ok := idx.versions[stableKey(path)]; ok {
		return true, nil
	}
	if len(idx.titles) == 0 {
		return false, nil
	}
	row, err := db.GetMediaWithTitleAndSystem(ctx, mediaDBID)
	if err != nil {
		return false, fmt.Errorf("load media for artwork lookup: %w", err)
	}
	if row == nil {
		return false, nil
	}
	slugs, err := groupSlugs(ctx, db, row)
	if err != nil {
		return false, err
	}
	return idx.hasTitle(slugs), nil
}

func IsOverridePath(dataDir, path string) bool {
	rel, err := filepath.Rel(filepath.Join(dataDir, overridesDir), path)
	return err == nil && filepath.IsLocal(rel) && rel != "."
}

func typeTag(kind string) string {
	switch kind {
	case "image", "thumbnail", "boxart", "boxart3d", "boxartside", "boxartback",
		"screenshot", "wheel", "titleshot", "map", "marquee", "fanart":
		return "property:image-" + kind
	default:
		return ""
	}
}

type assignment struct {
	modified time.Time
	property database.MediaProperty
}

func readAssignments(dir string, assignments map[string]assignment) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read artwork assignments: %w", err)
	}
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), ".png")
		tag := typeTag(name)
		if !found || tag == "" || !entry.Type().IsRegular() {
			continue
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, os.ErrNotExist) {
			continue
		}
		if infoErr != nil {
			return fmt.Errorf("stat artwork assignment: %w", infoErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		current, exists := assignments[tag]
		if !exists || info.ModTime().After(current.modified) {
			assignments[tag] = assignment{
				property: database.MediaProperty{TypeTag: tag, Text: path, ContentType: "image/png"},
				modified: info.ModTime(),
			}
		}
	}
	return nil
}

func assignmentProperties(assignments map[string]assignment) []database.MediaProperty {
	result := make([]database.MediaProperty, 0, len(assignments))
	for _, item := range assignments {
		result = append(result, item.property)
	}
	return result
}

func groupSlugs(ctx context.Context, db database.MediaDBI, row *database.MediaFullRow) ([]string, error) {
	slugs := []string{row.Title.Slug}
	groups, err := db.GetVersionGroupMembers(ctx, []int64{row.Title.DBID})
	if err != nil {
		return nil, fmt.Errorf("resolve artwork version group: %w", err)
	}
	if members := groups[row.Title.DBID]; len(members) > 1 {
		titles, titleErr := db.GetTitlesByDBIDs(ctx, members)
		if titleErr != nil {
			return nil, fmt.Errorf("load artwork version group titles: %w", titleErr)
		}
		for _, member := range titles {
			if member.DBID != row.Title.DBID && member.SystemDBID == row.System.DBID {
				slugs = append(slugs, member.Slug)
			}
		}
	}
	slices.Sort(slugs)
	return slugs, nil
}

// Properties returns exact-version and shared-title overrides for a row.
// A title assignment follows the current version group, even after MediaDB is
// rebuilt. The files are keyed by system and title slug, not transient DB IDs.
func Properties(
	ctx context.Context, db database.MediaDBI, dataDir string, row *database.MediaFullRow,
) (version, title []database.MediaProperty, err error) {
	system := row.System.SystemID
	idx := systemIndex(dataDir, system)
	if idx.empty() {
		return nil, nil, nil
	}

	exact := make(map[string]assignment)
	if _, ok := idx.versions[stableKey(row.Path)]; ok {
		dir := assignmentDir(dataDir, system, scopeVersion, row.Path)
		if readErr := readAssignments(dir, exact); readErr != nil {
			return nil, nil, readErr
		}
	}
	if len(idx.titles) == 0 {
		return assignmentProperties(exact), nil, nil
	}
	slugs, err := groupSlugs(ctx, db, row)
	if err != nil {
		return assignmentProperties(exact), nil, err
	}
	shared := make(map[string]assignment)
	for _, slug := range slugs {
		if _, ok := idx.titles[stableKey(slug)]; !ok {
			continue
		}
		if readErr := readAssignments(assignmentDir(dataDir, system, scopeTitle, slug), shared); readErr != nil {
			return assignmentProperties(exact), nil, readErr
		}
	}
	return assignmentProperties(exact), assignmentProperties(shared), nil
}
