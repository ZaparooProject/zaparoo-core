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

func HasSystem(dataDir, system string) bool {
	info, err := os.Stat(systemDir(dataDir, system))
	return err == nil && info.IsDir()
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
			if member.DBID != row.Title.DBID && member.SystemID == row.System.SystemID {
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
	if !HasSystem(dataDir, system) {
		return nil, nil, nil
	}

	exact := make(map[string]assignment)
	dir := assignmentDir(dataDir, system, scopeVersion, row.Path)
	if readErr := readAssignments(dir, exact); readErr != nil {
		return nil, nil, readErr
	}
	titleDir := filepath.Join(systemDir(dataDir, system), scopeTitle)
	if _, statErr := os.Stat(titleDir); errors.Is(statErr, os.ErrNotExist) {
		return assignmentProperties(exact), nil, nil
	}
	slugs, err := groupSlugs(ctx, db, row)
	if err != nil {
		return assignmentProperties(exact), nil, err
	}
	shared := make(map[string]assignment)
	for _, slug := range slugs {
		if readErr := readAssignments(assignmentDir(dataDir, system, scopeTitle, slug), shared); readErr != nil {
			return assignmentProperties(exact), nil, readErr
		}
	}
	return assignmentProperties(exact), assignmentProperties(shared), nil
}
