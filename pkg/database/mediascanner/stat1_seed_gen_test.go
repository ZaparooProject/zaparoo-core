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
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/require"
)

// TestGenerateStat1Seed regenerates pkg/database/mediadb/stat1_seed_data.go
// from a synthetic library built through the production staging pipeline.
// It is a maintenance tool, not a test: it only runs when STAT1_SEED_GEN=1
// is set. Rerun it after schema or index changes so the seeded statistics
// keep matching the live schema:
//
//	STAT1_SEED_GEN=1 go test -run TestGenerateStat1Seed ./pkg/database/mediascanner/
func TestGenerateStat1Seed(t *testing.T) {
	if os.Getenv("STAT1_SEED_GEN") != "1" {
		t.Skip("set STAT1_SEED_GEN=1 to regenerate the planner statistics seed")
	}

	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()
	ctx := context.Background()

	// A skewed spread of system sizes mimicking a large mixed library:
	// a few huge arcade-style sets, many mid-size consoles, a long tail.
	systems := seedGenSystems()
	require.NoError(t, SeedCanonicalTags(ctx, db.MediaDB))

	totalFiles := 0
	for i, sys := range systems {
		count := seedGenFileCount(i)
		totalFiles += count

		require.NoError(t, db.MediaDB.BeginTransaction(true))
		require.NoError(t, db.MediaDB.ClearScanStage())
		for n := range count {
			require.NoError(t, StageMediaPath(&StageMediaPathParams{
				DB:       db.MediaDB,
				SystemID: sys,
				Path:     seedGenPath(sys, n),
			}))
		}
		_, err := db.MediaDB.ReconcileStagedSystem(ctx, sys, database.ScanReconcileOpts{})
		require.NoError(t, err)
		require.NoError(t, db.MediaDB.CommitTransaction())
	}
	t.Logf("generated synthetic library: %d systems, %d files", len(systems), totalFiles)

	// Full-precision ANALYZE: the seed is captured once, so spend the time
	// for exact statistics rather than the approximate runtime variant. Use a
	// direct connection to the same database file to read sqlite_stat1.
	sqlDB, err := sql.Open("sqlite3", db.MediaDB.GetDBPath())
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()
	_, err = sqlDB.ExecContext(ctx, "ANALYZE")
	require.NoError(t, err)

	rows, err := sqlDB.QueryContext(ctx,
		"SELECT tbl, COALESCE(idx, ''), stat FROM sqlite_stat1 ORDER BY tbl, idx")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var b strings.Builder
	for rows.Next() {
		var tbl, idx, stat string
		require.NoError(t, rows.Scan(&tbl, &idx, &stat))
		// Cache tables are rebuilt from scratch on every device and carry no
		// planner-critical queries at seed time.
		if strings.HasPrefix(tbl, "Browse") || strings.HasSuffix(tbl, "Cache") || tbl == "ScanStage" {
			continue
		}
		fmt.Fprintf(&b, "\t{tbl: %q, idx: %q, stat: %q},\n", tbl, idx, stat)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, b.String(), "ANALYZE produced no statistics rows")

	out := seedGenHeader + b.String() + "}\n"
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	target := filepath.Join(filepath.Dir(thisFile), "..", "mediadb", "stat1_seed_data.go")
	require.NoError(t, os.WriteFile(target, []byte(out), 0o600))
	t.Logf("wrote %s", target)
}

func seedGenSystems() []string {
	all := systemdefs.AllSystems()
	ids := make([]string, 0, len(all))
	for _, sys := range all {
		ids = append(ids, sys.ID)
	}
	sort.Strings(ids)
	if len(ids) > 30 {
		ids = ids[:30]
	}
	return ids
}

// seedGenFileCount skews sizes: index 0-2 are huge sets, 3-14 mid-size,
// the rest a small long tail.
func seedGenFileCount(i int) int {
	switch {
	case i < 3:
		return 15000
	case i < 15:
		return 4000
	default:
		return 400
	}
}

var (
	seedGenWords1 = []string{
		"Super", "Mega", "Ultra", "Final", "Dragon", "Star", "Shadow", "Crystal",
		"Golden", "Iron", "Neon", "Turbo", "Cosmic", "Mystic", "Royal", "Silent",
	}
	seedGenWords2 = []string{
		"Quest", "Fighter", "Racer", "Warrior", "Legend", "Saga", "Force",
		"Strike", "Rally", "Kombat", "Hunter", "Runner", "Odyssey", "Chronicle",
	}
	seedGenRegions = []string{"(USA)", "(Europe)", "(Japan)", "(World)", "(USA, Europe)"}
	seedGenExtras  = []string{"", "", "", " (Rev 1)", " (Rev 2)", " (Beta)", " (Proto)", " (Demo)"}
)

// seedGenPath builds a deterministic, realistically-tagged filename so the
// staging pipeline produces representative title, slug, and tag rows.
func seedGenPath(systemID string, n int) string {
	name := fmt.Sprintf("%s %s %d %s%s",
		seedGenWords1[n%len(seedGenWords1)],
		seedGenWords2[(n/len(seedGenWords1))%len(seedGenWords2)],
		n,
		seedGenRegions[n%len(seedGenRegions)],
		seedGenExtras[n%len(seedGenExtras)],
	)
	return filepath.Join(string(filepath.Separator), "roms", systemID, name+".bin")
}

const seedGenHeader = `// Zaparoo Core
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

// Code generated by TestGenerateStat1Seed (pkg/database/mediascanner); DO NOT EDIT.
// Regenerate with: STAT1_SEED_GEN=1 go test -run TestGenerateStat1Seed ./pkg/database/mediascanner/

package mediadb

// stat1SeedRows are sqlite_stat1 rows captured from a synthetic ~100k-file
// library built through the production staging pipeline. See stat1_seed.go
// for how and when they are applied.
var stat1SeedRows = []struct{ tbl, idx, stat string }{
`
