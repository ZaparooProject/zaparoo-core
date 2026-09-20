/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package database

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Migrations run before the service is usable, so their cost is startup time a
// user waits through with the device doing nothing visible. That cost is not
// visible in development either: the statement behind issue #1372 measured
// 198ms on a desktop and 2m14s on a MiSTer with a large library, because SD
// storage is roughly two orders of magnitude slower for this work. Wall-clock
// budgets measured on a desktop therefore predict nothing, so this checks the
// shape of the statements instead — which is what actually decides whether
// cost scales with the size of someone's library.
//
// Work that scales belongs where progress is already reported and expected:
// the indexing path, or a background task after startup. repairBrowseSortIndex
// is the in-tree example, and #1371 moved exactly this class of statement out
// of a migration after it shipped.
//
// A migration that genuinely has to do this anyway opts out in the file:
//
//	-- zaparoo:allow-unbounded <reason it has to happen here>
//
// which is deliberately a written decision rather than a silent one.

const unboundedOptOutPrefix = "zaparoo:allow-unbounded"

var (
	commentBlockPattern  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentPattern   = regexp.MustCompile(`--[^\n]*`)
	stringLiteralPattern = regexp.MustCompile(`'(?:[^']|'')*'`)
	createTablePattern   = regexp.MustCompile(`(?i)\bcreate\s+table\s+(?:if\s+not\s+exists\s+)?` + identifier)
	createIndexPattern   = regexp.MustCompile(`(?i)\bcreate\s+(?:unique\s+)?index\s+(?:if\s+not\s+exists\s+)?` +
		identifier + `\s+on\s+` + identifier)
	updatePattern     = regexp.MustCompile(`(?i)^update\s+` + identifier)
	deletePattern     = regexp.MustCompile(`(?i)^delete\s+from\s+` + identifier)
	insertFromPattern = regexp.MustCompile(`(?i)^insert\s+into\s+` + identifier + `[\s\S]*\bselect\b`)
	dropColumnPattern = regexp.MustCompile(`(?i)^alter\s+table\s+` + identifier + `\s+drop\s+(?:column\s+)?`)
	wherePattern      = regexp.MustCompile(`(?i)\bwhere\b`)
)

// identifier matches a bare or quoted SQL name.
const identifier = `["` + "`" + `\[]?([A-Za-z_][A-Za-z0-9_]*)["` + "`" + `\]]?`

// migrationDirs are the two sets of migrations this rule covers.
func migrationDirs() []string {
	return []string{
		filepath.Join("userdb", "migrations"),
		filepath.Join("mediadb", "migrations"),
	}
}

// upSection returns the Up half of a goose migration. Down statements only run
// on an explicit rollback, which is not a startup cost.
func upSection(body string) string {
	lower := strings.ToLower(body)
	start := strings.Index(lower, "-- +goose up")
	if start < 0 {
		return body
	}
	rest := body[start:]
	if end := strings.Index(strings.ToLower(rest), "-- +goose down"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// sqlStatements strips comments and string literals, then splits on the
// statement separator. Stripping first is what keeps a semicolon or a keyword
// inside a literal from being read as SQL.
func sqlStatements(section string) []string {
	stripped := commentBlockPattern.ReplaceAllString(section, " ")
	stripped = lineCommentPattern.ReplaceAllString(stripped, " ")
	stripped = stringLiteralPattern.ReplaceAllString(stripped, "''")

	raw := strings.Split(stripped, ";")
	statements := make([]string, 0, len(raw))
	for _, statement := range raw {
		collapsed := strings.Join(strings.Fields(statement), " ")
		if collapsed != "" {
			statements = append(statements, collapsed)
		}
	}
	return statements
}

// tablesCreatedIn lists the tables a migration creates itself. Work against a
// table created in the same file is bounded by definition: the table is empty,
// however large the library is.
func tablesCreatedIn(statements []string) map[string]bool {
	created := make(map[string]bool)
	for _, statement := range statements {
		for _, match := range createTablePattern.FindAllStringSubmatch(statement, -1) {
			created[strings.ToLower(match[1])] = true
		}
	}
	return created
}

// optOutReason returns the reason given for an explicit opt-out, if any.
func optOutReason(body string) (string, bool) {
	for _, comment := range lineCommentPattern.FindAllString(body, -1) {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "--"))
		if !strings.HasPrefix(text, unboundedOptOutPrefix) {
			continue
		}
		reason := strings.TrimSpace(strings.TrimPrefix(text, unboundedOptOutPrefix))
		reason = strings.TrimSpace(strings.TrimPrefix(reason, ":"))
		return reason, true
	}
	return "", false
}

// unboundedStatements reports statements whose cost grows with the number of
// rows already in the database.
func unboundedStatements(statements []string, created map[string]bool) []string {
	var findings []string
	for _, statement := range statements {
		switch {
		case createIndexPattern.MatchString(statement):
			match := createIndexPattern.FindStringSubmatch(statement)
			if !created[strings.ToLower(match[2])] {
				findings = append(findings, fmt.Sprintf(
					"builds an index over the existing %s table: %s", match[2], statement,
				))
			}
		case dropColumnPattern.MatchString(statement):
			match := dropColumnPattern.FindStringSubmatch(statement)
			if !created[strings.ToLower(match[1])] {
				findings = append(findings, fmt.Sprintf(
					"drops a column, which rewrites the whole %s table in SQLite: %s", match[1], statement,
				))
			}
		case insertFromPattern.MatchString(statement):
			findings = append(findings, "copies existing rows into another table: "+statement)
		case updatePattern.MatchString(statement) && !wherePattern.MatchString(statement):
			match := updatePattern.FindStringSubmatch(statement)
			if !created[strings.ToLower(match[1])] {
				findings = append(findings, fmt.Sprintf(
					"rewrites every row of %s: %s", match[1], statement,
				))
			}
		case deletePattern.MatchString(statement) && !wherePattern.MatchString(statement):
			match := deletePattern.FindStringSubmatch(statement)
			if !created[strings.ToLower(match[1])] {
				findings = append(findings, fmt.Sprintf(
					"deletes every row of %s: %s", match[1], statement,
				))
			}
		}
	}
	return findings
}

// analyzeMigration returns the unbounded statements in one migration file, or
// nothing when the file carries an opt-out.
func analyzeMigration(t *testing.T, path string) []string {
	t.Helper()

	//nolint:gosec // Path comes from this repository's own migration directories.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(raw)

	if reason, opted := optOutReason(body); opted {
		assert.NotEmpty(t, reason,
			"%s opts out of the unbounded-statement rule without saying why; "+
				"write the reason after %q", filepath.Base(path), unboundedOptOutPrefix)
		return nil
	}

	statements := sqlStatements(upSection(body))
	return unboundedStatements(statements, tablesCreatedIn(statements))
}

// grandfatheredMigrations predate this rule. They are already applied on every
// device, and an applied migration is immutable, so they cannot be changed to
// carry an opt-out comment and are listed here instead.
//
// This list must only ever shrink. A new migration does not belong in it: if
// one genuinely has to do unbounded work, it says so in its own file with a
// `-- zaparoo:allow-unbounded <reason>` comment, where the reason sits next to
// the statement it explains.
var grandfatheredMigrations = map[string]bool{
	"mediadb/migrations/20251001185734_optimize_schema.sql":              true,
	"mediadb/migrations/20260421120000_tag_counts.sql":                   true,
	"mediadb/migrations/20260424190000_add_missing_runtime_indexes.sql":  true,
	"mediadb/migrations/20260429142159_system_browse_cache.sql":          true,
	"mediadb/migrations/20260609120000_media_sortname.sql":               true,
	"mediadb/migrations/20260703080000_scan_staging.sql":                 true,
	"mediadb/migrations/20260902160000_purge_stale_slug_resolutions.sql": true,
	"userdb/migrations/20251104120000_media_history.sql":                 true,
	"userdb/migrations/20251122041921_enhance_history_schema.sql":        true,
	"userdb/migrations/20260714000000_create_profiles_table.sql":         true,
	"userdb/migrations/20260714020000_profile_roles.sql":                 true,
	"userdb/migrations/20260919120000_deck_playlist_id.sql":              true,
}

func TestMigrations_DoNotDoUnboundedWorkAtStartup(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool, len(grandfatheredMigrations))

	for _, dir := range migrationDirs() {
		paths, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		require.NoError(t, err)
		require.NotEmpty(t, paths, "no migrations found in %s", dir)

		for _, path := range paths {
			key := filepath.ToSlash(path)
			findings := analyzeMigration(t, path)

			if grandfatheredMigrations[key] {
				seen[key] = true
				assert.NotEmpty(t, findings,
					"%s no longer does unbounded work, so remove it from "+
						"grandfatheredMigrations; that list is only allowed to shrink", key)
				continue
			}

			assert.Empty(t, findings,
				"%s does work whose cost grows with the size of the library, which runs "+
					"before the service is usable.\n%s\n\nMove it to the indexing path or a "+
					"background task after startup, or record why it has to happen here with "+
					"a `-- %s <reason>` comment.",
				key, strings.Join(findings, "\n"), unboundedOptOutPrefix)
		}
	}

	for key := range grandfatheredMigrations {
		assert.True(t, seen[key],
			"grandfatheredMigrations lists %s, which is not on disk; "+
				"a stale entry hides a real finding if the name is ever reused", key)
	}
}

// The rule is only worth having if it actually catches the shape of statement
// that caused #1372, and only tolerable if it leaves the cheap ones alone.
func TestMigrationCostRule_CatchesUnboundedWorkAndLeavesCheapWorkAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		flagged bool
	}{
		{
			name: "index over an existing table",
			body: "-- +goose Up\nCREATE INDEX idx_media_name ON Media (Name);\n",
			// This is #1372 itself: minutes of startup on a large library.
			flagged: true,
		},
		{
			name: "index over a table the migration creates",
			body: "-- +goose Up\nCREATE TABLE Media (Id INTEGER PRIMARY KEY, Name TEXT);\n" +
				"CREATE INDEX idx_media_name ON Media (Name);\n",
			// The table is empty however large the library is.
			flagged: false,
		},
		{
			name:    "adding a column",
			body:    "-- +goose Up\nALTER TABLE Media ADD COLUMN Hidden INTEGER NOT NULL DEFAULT 0;\n",
			flagged: false,
		},
		{
			name:    "dropping a column rewrites the table",
			body:    "-- +goose Up\nALTER TABLE Media DROP COLUMN Hidden;\n",
			flagged: true,
		},
		{
			name:    "backfilling every row",
			body:    "-- +goose Up\nUPDATE Media SET SortName = Name;\n",
			flagged: true,
		},
		{
			name:    "updating a bounded set",
			body:    "-- +goose Up\nUPDATE Media SET SortName = Name WHERE SortName IS NULL;\n",
			flagged: false,
		},
		{
			name:    "emptying a table",
			body:    "-- +goose Up\nDELETE FROM SlugResolutions;\n",
			flagged: true,
		},
		{
			name: "copying rows into a rebuilt table",
			body: "-- +goose Up\nCREATE TABLE MediaNew (Id INTEGER PRIMARY KEY);\n" +
				"INSERT INTO MediaNew (Id) SELECT Id FROM Media;\n",
			flagged: true,
		},
		{
			name: "down statements are not a startup cost",
			body: "-- +goose Up\nCREATE TABLE Media (Id INTEGER PRIMARY KEY);\n" +
				"-- +goose Down\nUPDATE Media SET Id = 0;\n",
			flagged: false,
		},
		{
			name:    "a semicolon inside a string is not a statement break",
			body:    "-- +goose Up\nUPDATE Media SET Name = 'a;b' WHERE Id = 1;\n",
			flagged: false,
		},
		{
			name: "an explicit opt-out with a reason is allowed",
			body: "-- +goose Up\n-- zaparoo:allow-unbounded the column cannot be added without it\n" +
				"UPDATE Media SET SortName = Name;\n",
			flagged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "20260101000000_fixture.sql")
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))

			findings := analyzeMigration(t, path)
			if tt.flagged {
				assert.NotEmpty(t, findings, "this should have been flagged")
				return
			}
			assert.Empty(t, findings, "this should not have been flagged")
		})
	}
}

// An opt-out with no reason is the failure mode the rule exists to prevent:
// the decision gets made silently and nobody can tell later whether it was
// considered.
func TestMigrationCostRule_RequiresAReasonForAnOptOut(t *testing.T) {
	t.Parallel()

	reason, opted := optOutReason("-- zaparoo:allow-unbounded\nUPDATE Media SET SortName = Name;\n")
	assert.True(t, opted, "the marker still opts the file out")
	assert.Empty(t, reason, "an empty reason is what the rule reports on")

	reason, opted = optOutReason("-- zaparoo:allow-unbounded: nothing else can do this\n")
	assert.True(t, opted)
	assert.Equal(t, "nothing else can do this", reason)
}
