# User-configurable `_Other` launchables Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user register additional MiSTer `_Other` launchable cores via a new `[[other_launchables]]` section in `config.toml`, without a Core code change or recompile.

**Architecture:** A new `pkg/config` section (`OtherLaunchable` struct, validated at config-load time) supplies user entries. `pkg/platforms/mister/launchables.go` merges those entries with the existing hardcoded `_Other` definitions at `Launchables()` call time — a matching `id` overrides an existing entry's display fields while freezing its UUID; a new `id` is appended with a UUID deterministically derived from the id string.

**Tech Stack:** Go 1.26.4, `github.com/pelletier/go-toml/v2`, `github.com/google/uuid`, `github.com/rs/zerolog/log`, `testify` (`assert`/`require`).

## Global Constraints

- Scope is `_Other` only — `_Console`/`_Computer` are not touched by this plan.
- No `config_schema` bump — this is a purely additive optional section.
- `core_path` is a bare filename prefix within `_Other` (e.g. `"Arduboy"`), never a path — reject any value containing `/`, `\`, or `..`.
- `category` defaults to `"Other"` when omitted; a non-empty value must be one of `Other`, `Console`, `Computer`, `Handheld`, `Arcade` or the entire entry is dropped.
- Invalid entries are dropped with `log.Warn()`; they never abort config load.
- A user entry whose `id` (case-insensitive) matches a built-in's canonical config id overrides that built-in's `Name`/`Category`/`CorePath` but **must keep the built-in's original fixed UUID** — never recompute it.
- A brand-new user `id` gets UUID `uuid.NewSHA1(launchables.ZaparooLaunchableNamespace, []byte(strings.ToLower(id)))` — deterministic, stable across restarts.
- Config reload is startup-only — no hot-reload requirement.
- Reuse `launchOtherCore`/`testOtherCore` unchanged — only which entries feed into them changes.

---

## File Structure

- Create: `pkg/config/configotherlaunchables.go` — `OtherLaunchable` struct, category allow-list, `validateOtherLaunchables`, `cloneOtherLaunchables`, `(*Instance).OtherLaunchables()` accessor.
- Create: `pkg/config/configotherlaunchables_test.go` — unit tests for validation and the accessor.
- Modify: `pkg/config/config.go` — add `OtherLaunchables`/`otherLaunchablesValid` fields to `Values`; call `validateOtherLaunchables` from `applyTOML`.
- Modify: `pkg/launchables/ids.go` — add `ZaparooLaunchableNamespace`, the UUID v5 namespace for user-defined launchables.
- Modify: `pkg/platforms/mister/launchables.go` — turn the 9 hardcoded `_Other` `VirtualSystem` literals into a `misterOtherLaunchableDefinition` slice (matching the existing `misterCoreLaunchableDefinition` pattern already used for `_Console`/`_Computer`), add `mergeOtherLaunchableDefinitions`, wire it into `Launchables()`.
- Modify: `pkg/platforms/mister/launchables_test.go` — add merge-behavior tests; existing `TestLaunchablesOtherCoreDefinitions` (asserts exactly 32 items) is unaffected since it passes a config with no `other_launchables`.

---

### Task 1: `pkg/config` — `other_launchables` section

**Files:**
- Create: `pkg/config/configotherlaunchables.go`
- Create: `pkg/config/configotherlaunchables_test.go`
- Modify: `pkg/config/config.go:57-74` (the `Values` struct), `pkg/config/config.go:301-306` (`applyTOML`)

**Interfaces:**
- Produces: `type OtherLaunchable struct { ID, Name, Category, CorePath string }` (all `string`, `toml` tags `id`/`name`/`category,omitempty`/`core_path`); `func (c *Instance) OtherLaunchables() []OtherLaunchable`. Task 2 consumes both.

- [ ] **Step 1: Write the failing tests**

Create `pkg/config/configotherlaunchables_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOtherLaunchables_ValidEntryPasses(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", Category: "Other", CorePath: "Arduboy"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "arduboy", valid[0].ID)
	assert.Equal(t, "Arduboy", valid[0].Name)
	assert.Equal(t, "Other", valid[0].Category)
	assert.Equal(t, "Arduboy", valid[0].CorePath)
}

func TestValidateOtherLaunchables_CategoryDefaultsToOther(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "solarus", Name: "Solarus", CorePath: "Solarus"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "Other", valid[0].Category)
}

func TestValidateOtherLaunchables_MissingRequiredFieldsRejected(t *testing.T) {
	tests := []struct {
		name  string
		entry OtherLaunchable
	}{
		{name: "missing id", entry: OtherLaunchable{Name: "Arduboy", CorePath: "Arduboy"}},
		{name: "missing name", entry: OtherLaunchable{ID: "arduboy", CorePath: "Arduboy"}},
		{name: "missing core_path", entry: OtherLaunchable{ID: "arduboy", Name: "Arduboy"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := validateOtherLaunchables([]OtherLaunchable{tt.entry})
			assert.Empty(t, valid)
		})
	}
}

func TestValidateOtherLaunchables_CorePathRejectsPathSeparatorsAndTraversal(t *testing.T) {
	tests := []string{"_Other/Arduboy", "..\\Arduboy", "../Arduboy", "sub/dir"}

	for _, corePath := range tests {
		t.Run(corePath, func(t *testing.T) {
			raw := []OtherLaunchable{{ID: "arduboy", Name: "Arduboy", CorePath: corePath}}
			assert.Empty(t, validateOtherLaunchables(raw))
		})
	}
}

func TestValidateOtherLaunchables_UnknownCategoryRejected(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", Category: "Homebrew", CorePath: "Arduboy"},
	}

	assert.Empty(t, validateOtherLaunchables(raw))
}

func TestValidateOtherLaunchables_DuplicateIDKeepsFirst(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", CorePath: "Arduboy"},
		{ID: "Arduboy", Name: "Arduboy Two", CorePath: "ArduboyTwo"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "Arduboy", valid[0].Name)
}

func TestOtherLaunchables_LoadTOMLRoundTrip(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
core_path = "Arduboy"

[[other_launchables]]
id = "bad"
name = "Bad Entry"
category = "NotARealCategory"
core_path = "Bad"
`))

	entries := cfg.OtherLaunchables()

	require.Len(t, entries, 1)
	assert.Equal(t, "arduboy", entries[0].ID)
	assert.Equal(t, "Other", entries[0].Category)
}

func TestOtherLaunchables_ReturnsIndependentCopy(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
core_path = "Arduboy"
`))

	entries := cfg.OtherLaunchables()
	entries[0].Name = "Mutated"

	entriesAgain := cfg.OtherLaunchables()
	assert.Equal(t, "Arduboy", entriesAgain[0].Name)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/config/... -run TestValidateOtherLaunchables -v` and `go test ./pkg/config/... -run TestOtherLaunchables -v`
Expected: FAIL to compile — `undefined: OtherLaunchable`, `undefined: validateOtherLaunchables`, `entries.OtherLaunchables undefined`.

- [ ] **Step 3: Create `pkg/config/configotherlaunchables.go`**

```go
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

package config

import (
	"strings"

	"github.com/rs/zerolog/log"
)

// OtherLaunchable is a user-configured MiSTer _Other launchable entry. The
// mister platform merges these with its built-in list at runtime (see
// pkg/platforms/mister/launchables.go).
type OtherLaunchable struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Category string `toml:"category,omitempty"`
	CorePath string `toml:"core_path"`
}

const defaultOtherLaunchableCategory = "Other"

var validOtherLaunchableCategories = map[string]struct{}{
	"Other":    {},
	"Console":  {},
	"Computer": {},
	"Handheld": {},
	"Arcade":   {},
}

// validateOtherLaunchables filters raw other_launchables entries parsed from
// config.toml, dropping invalid ones with a warning and defaulting an empty
// category to "Other". The first occurrence of a duplicate id (case
// insensitive) wins; later duplicates are dropped.
func validateOtherLaunchables(raw []OtherLaunchable) []OtherLaunchable {
	valid := make([]OtherLaunchable, 0, len(raw))
	seenIDs := make(map[string]struct{}, len(raw))

	for _, entry := range raw {
		if entry.ID == "" || entry.Name == "" || entry.CorePath == "" {
			log.Warn().Msgf("other_launchables entry missing required id/name/core_path, ignoring: %+v", entry)
			continue
		}

		if strings.ContainsAny(entry.CorePath, `/\`) || strings.Contains(entry.CorePath, "..") {
			log.Warn().Msgf(
				"other_launchables entry %q has invalid core_path %q (must be a bare filename prefix), ignoring",
				entry.ID, entry.CorePath,
			)
			continue
		}

		id := strings.ToLower(entry.ID)
		if _, ok := seenIDs[id]; ok {
			log.Warn().Msgf("other_launchables entry %q is a duplicate id, ignoring", entry.ID)
			continue
		}

		if entry.Category == "" {
			entry.Category = defaultOtherLaunchableCategory
		} else if _, ok := validOtherLaunchableCategories[entry.Category]; !ok {
			log.Warn().Msgf(
				"other_launchables entry %q has unknown category %q, ignoring",
				entry.ID, entry.Category,
			)
			continue
		}

		seenIDs[id] = struct{}{}
		valid = append(valid, entry)
	}

	return valid
}

func cloneOtherLaunchables(entries []OtherLaunchable) []OtherLaunchable {
	owned := make([]OtherLaunchable, len(entries))
	copy(owned, entries)
	return owned
}

// OtherLaunchables returns validated user-configured MiSTer _Other launchable
// entries. Invalid entries are already filtered out at config load time.
func (c *Instance) OtherLaunchables() []OtherLaunchable {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneOtherLaunchables(c.vals.otherLaunchablesValid)
}
```

- [ ] **Step 4: Wire the new section into `Values` and `applyTOML`**

In `pkg/config/config.go`, replace the `Values` struct (currently lines 57-74):

```go
type Values struct {
	Groovy         Groovy    `toml:"groovy,omitempty"`
	Input          Input     `toml:"input,omitempty"`
	AutoUpdate     *bool     `toml:"auto_update,omitempty"`
	UpdateChannel  *string   `toml:"update_channel,omitempty"`
	Audio          Audio     `toml:"audio"`
	Service        Service   `toml:"service,omitempty"`
	Launchers      Launchers `toml:"launchers,omitempty"`
	Playtime       Playtime  `toml:"playtime,omitempty"`
	Media          Media     `toml:"media,omitempty"`
	ZapScript      ZapScript `toml:"zapscript,omitempty"`
	Mappings       Mappings  `toml:"mappings,omitempty"`
	Systems        Systems   `toml:"systems,omitempty"`
	Readers        Readers   `toml:"readers,omitempty"`
	ConfigSchema   int       `toml:"config_schema"`
	DebugLogging   bool      `toml:"debug_logging"`
	ErrorReporting bool      `toml:"error_reporting"`
}
```

with:

```go
type Values struct {
	Groovy                Groovy            `toml:"groovy,omitempty"`
	Input                 Input             `toml:"input,omitempty"`
	AutoUpdate            *bool             `toml:"auto_update,omitempty"`
	UpdateChannel         *string           `toml:"update_channel,omitempty"`
	Audio                 Audio             `toml:"audio"`
	Service               Service           `toml:"service,omitempty"`
	Launchers             Launchers         `toml:"launchers,omitempty"`
	Playtime              Playtime          `toml:"playtime,omitempty"`
	Media                 Media             `toml:"media,omitempty"`
	ZapScript             ZapScript         `toml:"zapscript,omitempty"`
	Mappings              Mappings          `toml:"mappings,omitempty"`
	Systems               Systems           `toml:"systems,omitempty"`
	Readers               Readers           `toml:"readers,omitempty"`
	OtherLaunchables      []OtherLaunchable `toml:"other_launchables,omitempty"`
	otherLaunchablesValid []OtherLaunchable
	ConfigSchema          int  `toml:"config_schema"`
	DebugLogging          bool `toml:"debug_logging"`
	ErrorReporting        bool `toml:"error_reporting"`
}
```

Then in `applyTOML` (currently `pkg/config/config.go:301-306`):

```go
func (c *Instance) applyTOML(data string) error {
	if err := toml.Unmarshal([]byte(data), &c.vals); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// prepare allow files regexes
```

insert the validation call right after the unmarshal, before the "prepare allow files regexes" comment:

```go
func (c *Instance) applyTOML(data string) error {
	if err := toml.Unmarshal([]byte(data), &c.vals); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	c.vals.otherLaunchablesValid = validateOtherLaunchables(c.vals.OtherLaunchables)

	// prepare allow files regexes
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./pkg/config/... -run 'TestValidateOtherLaunchables|TestOtherLaunchables' -v`
Expected: PASS (9 test functions, all green).

- [ ] **Step 6: Run the full config package test suite to check for regressions**

Run: `go test -race ./pkg/config/...`
Expected: PASS, no regressions in existing config tests.

- [ ] **Step 7: Commit**

```bash
git add pkg/config/configotherlaunchables.go pkg/config/configotherlaunchables_test.go pkg/config/config.go
git commit -m "feat(config): add other_launchables config section

Lets users declare additional MiSTer _Other launchable cores in
config.toml. Entries are validated at load time (required fields,
core_path must be a bare filename with no path separators, category
must be a known value or omitted). Invalid entries are dropped with a
warning; nothing here is wired into the mister platform yet."
```

---

### Task 2: Wire user entries into the MiSTer platform's launchable list

**Files:**
- Modify: `pkg/launchables/ids.go` (add namespace UUID)
- Modify: `pkg/platforms/mister/launchables.go`
- Modify: `pkg/platforms/mister/launchables_test.go`

**Interfaces:**
- Consumes: `config.OtherLaunchable{ID, Name, Category, CorePath string}` and `(*config.Instance).OtherLaunchables() []config.OtherLaunchable` from Task 1.
- Produces: `var launchables.ZaparooLaunchableNamespace uuid.UUID`; `type misterOtherLaunchableDefinition struct { ConfigID, Name, Category, CoreName string; ID uuid.UUID }`; `func mergeOtherLaunchableDefinitions(builtins []misterOtherLaunchableDefinition, userEntries []config.OtherLaunchable) []misterOtherLaunchableDefinition`. Nothing outside this package consumes these directly — `Platform.Launchables` is the only caller, and its own signature (`func (p *Platform) Launchables(*config.Instance) []launchables.Launchable`) is unchanged from the outside.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/platforms/mister/launchables_test.go`. First add `"github.com/google/uuid"` to the existing import block (currently `errors`, `os`, `path/filepath`, `testing`, `config`, `systemdefs`, `launchables`, `assert`, `require`):

```go
import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

Then add these test functions at the end of the file:

```go
func TestMergeOtherLaunchableDefinitions_AppendsNewEntry(t *testing.T) {
	merged := mergeOtherLaunchableDefinitions(
		misterOtherLaunchableDefinitions,
		[]config.OtherLaunchable{
			{ID: "arduboy", Name: "Arduboy", Category: "Other", CorePath: "Arduboy"},
		},
	)

	require.Len(t, merged, len(misterOtherLaunchableDefinitions)+1)
	added := merged[len(merged)-1]
	assert.Equal(t, "Arduboy", added.Name)
	assert.Equal(t, "Other", added.Category)
	assert.Equal(t, "Arduboy", added.CoreName)
	assert.Equal(t, uuid.NewSHA1(launchables.ZaparooLaunchableNamespace, []byte("arduboy")), added.ID)
}

func TestMergeOtherLaunchableDefinitions_OverridesBuiltinKeepsUUID(t *testing.T) {
	merged := mergeOtherLaunchableDefinitions(
		misterOtherLaunchableDefinitions,
		[]config.OtherLaunchable{
			{ID: "chess", Name: "Chess Renamed", Category: "Console", CorePath: "ChessAlt"},
		},
	)

	require.Len(t, merged, len(misterOtherLaunchableDefinitions))

	var chess *misterOtherLaunchableDefinition
	for i := range merged {
		if merged[i].ConfigID == "chess" {
			chess = &merged[i]
			break
		}
	}
	require.NotNil(t, chess, "chess entry missing from merged list")
	assert.Equal(t, "Chess Renamed", chess.Name)
	assert.Equal(t, "Console", chess.Category)
	assert.Equal(t, "ChessAlt", chess.CoreName)
	assert.Equal(t, launchables.MisterOtherChess, chess.ID)
}

func TestLaunchables_IncludesUserConfiguredOtherLaunchable(t *testing.T) {
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
core_path = "Arduboy"
`))

	items := (&Platform{}).Launchables(cfg)

	require.Len(t, items, 33)

	var found *launchables.VirtualSystem
	for i := range items {
		if system, ok := items[i].(launchables.VirtualSystem); ok && system.Name == "Arduboy" {
			found = &system
			break
		}
	}
	require.NotNil(t, found, "Arduboy launchable missing")
	assert.Equal(t, "Other", found.Category)
	assert.Equal(t, uuid.NewSHA1(launchables.ZaparooLaunchableNamespace, []byte("arduboy")), found.ID)
	assert.NotNil(t, found.Launch)
	assert.NotNil(t, found.Test)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/platforms/mister/... -run 'TestMergeOtherLaunchableDefinitions|TestLaunchables_IncludesUserConfiguredOtherLaunchable' -v`

Note: this package has `//go:build linux` — on a non-Linux dev machine `go test` will report "build constraints exclude all Go files in this directory", which is expected. Run this and the later verification steps on a Linux machine or CI (the repo's `native-pr-tests` CI job runs on `ubuntu-latest`), or via `GOOS=linux go vet ./pkg/platforms/mister/...` for a local compile-only check.

Expected (on Linux): FAIL to compile — `undefined: mergeOtherLaunchableDefinitions`, `undefined: launchables.ZaparooLaunchableNamespace`, `undefined: misterOtherLaunchableDefinition`.

- [ ] **Step 3: Add the namespace UUID to `pkg/launchables/ids.go`**

Append after the existing `var (...)` block (after line 61):

```go
// ZaparooLaunchableNamespace derives stable UUIDs (via uuid.NewSHA1) for
// user-configured launchables from their config.toml "id" string, so the
// same id always produces the same UUID across restarts and devices.
var ZaparooLaunchableNamespace = uuid.MustParse("f7a49fd1-2910-4fa8-8b41-db0f3510e1fc")
```

- [ ] **Step 4: Refactor `pkg/platforms/mister/launchables.go`**

Replace lines 77-168 (the `Launchables` method and its inline `_Other` literals) with a data-driven definition slice, a merge function, and a slimmer `Launchables`:

```go
type misterOtherLaunchableDefinition struct {
	// ConfigID is the canonical lowercase id a user's other_launchables
	// entry must use in config.toml to override this built-in entry.
	ConfigID string
	Name     string
	Category string
	// CoreName is the bare filename prefix within _Other (no directory).
	CoreName string
	ID       uuid.UUID
}

var misterOtherLaunchableDefinitions = []misterOtherLaunchableDefinition{
	{
		ConfigID: "chess", ID: launchables.MisterOtherChess,
		Name: "Chess", Category: misterLaunchableCategoryOther, CoreName: "Chess",
	},
	{
		ConfigID: "donut", ID: launchables.MisterOtherDonut,
		Name: "Donut", Category: misterLaunchableCategoryOther, CoreName: "Donut",
	},
	{
		ConfigID: "epochgalaxyii", ID: launchables.MisterOtherEpochGalaxyII,
		Name: "Epoch Galaxy II", Category: misterLaunchableCategoryOther, CoreName: "EpochGalaxyII",
	},
	{
		ConfigID: "flappybird", ID: launchables.MisterOtherFlappyBird,
		Name: "Flappy Bird", Category: misterLaunchableCategoryOther, CoreName: "FlappyBird",
	},
	{
		ConfigID: "gameoflife", ID: launchables.MisterOtherGameOfLife,
		Name: "Game of Life", Category: misterLaunchableCategoryOther, CoreName: "GameOfLife",
	},
	{
		ConfigID: "gbmidi", ID: launchables.MisterOtherGBMidi,
		Name: "GBMidi", Category: misterLaunchableCategoryOther, CoreName: "GBMidi",
	},
	{
		ConfigID: "genmidi", ID: launchables.MisterOtherGenMidi,
		Name: "GenMidi", Category: misterLaunchableCategoryOther, CoreName: "GenMidi",
	},
	{
		ConfigID: "slugcross", ID: launchables.MisterOtherSlugCross,
		Name: "Slug Cross", Category: misterLaunchableCategoryOther, CoreName: "SlugCross",
	},
	{
		ConfigID: "tomyscramble", ID: launchables.MisterOtherTomyScramble,
		Name: "Tomy Scramble", Category: misterLaunchableCategoryOther, CoreName: "TomyScramble",
	},
}

// mergeOtherLaunchableDefinitions overlays user-configured other_launchables
// entries onto the built-in _Other definitions. A user entry whose id
// matches a built-in's ConfigID (case-insensitive) replaces that entry's
// Name/Category/CoreName but keeps its original fixed UUID, so any existing
// frontend hidden/renamed/cover-art state tied to that system id survives.
// A user entry with no matching ConfigID is appended as a new definition
// with a UUID derived deterministically from its id.
func mergeOtherLaunchableDefinitions(
	builtins []misterOtherLaunchableDefinition,
	userEntries []config.OtherLaunchable,
) []misterOtherLaunchableDefinition {
	merged := make([]misterOtherLaunchableDefinition, len(builtins))
	copy(merged, builtins)

	index := make(map[string]int, len(merged))
	for i, def := range merged {
		index[def.ConfigID] = i
	}

	for _, entry := range userEntries {
		id := strings.ToLower(entry.ID)
		if i, ok := index[id]; ok {
			merged[i].Name = entry.Name
			merged[i].Category = entry.Category
			merged[i].CoreName = entry.CorePath
			continue
		}
		merged = append(merged, misterOtherLaunchableDefinition{
			ConfigID: id,
			Name:     entry.Name,
			Category: entry.Category,
			CoreName: entry.CorePath,
			ID:       uuid.NewSHA1(launchables.ZaparooLaunchableNamespace, []byte(id)),
		})
		index[id] = len(merged) - 1
	}

	return merged
}

// Launchables exposes launch-only MiSTer core entries that do not already have
// media launchers.
func (p *Platform) Launchables(cfg *config.Instance) []launchables.Launchable {
	otherDefs := mergeOtherLaunchableDefinitions(misterOtherLaunchableDefinitions, cfg.OtherLaunchables())

	items := make([]launchables.Launchable, 0, len(otherDefs)+1+len(misterCoreLaunchableDefinitions))
	for _, def := range otherDefs {
		items = append(items, launchables.VirtualSystem{
			ID:       def.ID,
			Name:     def.Name,
			Category: def.Category,
			Launch:   p.launchOtherCore(filepath.Join("_Other", def.CoreName)),
			Test:     testOtherCore(def.CoreName),
		})
	}

	// 3S-ARM is a native ARM port of Street Fighter III: 3rd Strike that
	// ships as an _Other core but is a real arcade game, so it is exposed
	// as virtual media under the Arcade system rather than an Other entry.
	items = append(items, launchables.VirtualMedia{
		ID:       launchables.MisterArcadeThirdStrike,
		Name:     "Street Fighter III: 3rd Strike (3S-ARM)",
		SystemID: systemdefs.SystemArcade,
		Launch:   p.launchOtherCore(filepath.Join("_Other", "3S-ARM")),
		Test:     testOtherCore("3S-ARM"),
	})

	for _, def := range misterCoreLaunchableDefinitions {
		items = append(items, launchables.VirtualSystem{
			ID:       def.ID,
			Name:     def.Name,
			Category: def.Category,
			Launch:   p.launchCore(def.CorePath),
			Test:     testCore(def.CorePath),
		})
	}

	return items
}
```

Everything below the old `Launchables` method (`testOtherCore`, `testCore`, `otherCoreExists`, `coreExists`, `closeLaunchConsole`, `launchShortCoreFile`, `launchOtherCore`, `launchCore` — currently lines 170-241) is unchanged and stays as-is.

- [ ] **Step 5: Run the tests to verify they pass**

Run (on Linux, or CI): `go test ./pkg/platforms/mister/... -run 'TestMergeOtherLaunchableDefinitions|TestLaunchables_IncludesUserConfiguredOtherLaunchable|TestLaunchablesOtherCoreDefinitions' -v`
Expected: PASS, including the pre-existing `TestLaunchablesOtherCoreDefinitions` (still asserts `require.Len(t, items, 32)` since it passes `&config.Instance{}` with no `other_launchables` configured — unaffected by this change).

- [ ] **Step 6: Run the full mister and launchables package test suites to check for regressions**

Run: `go test -race ./pkg/platforms/mister/... ./pkg/launchables/...`
Expected: PASS, no regressions.

- [ ] **Step 7: Run golangci-lint**

Run: `golangci-lint run ./pkg/config/... ./pkg/launchables/... ./pkg/platforms/mister/...`
Expected: no new lint findings.

- [ ] **Step 8: Commit**

```bash
git add pkg/launchables/ids.go pkg/platforms/mister/launchables.go pkg/platforms/mister/launchables_test.go
git commit -m "feat(mister): merge user-configured other_launchables at runtime

Turns the hardcoded _Other launchable list into a data-driven slice
and overlays config.toml's [[other_launchables]] entries onto it. A
user id matching a built-in overrides its display fields while
keeping the built-in's fixed UUID; a new id gets a UUID deterministically
derived from the id string (uuid.NewSHA1), so users can now register
cores like Arduboy, Chip8, OpenBOR, PICO-8, Solarus, Sonic Mania, and
Tamagotchi themselves without a Core code change."
```

---

## Self-Review Notes

**Spec coverage:** Config shape (Task 1 Steps 3-4) — done. Validation rules (Task 1 Step 3) — done. ID stability & conflict handling (Task 2 Steps 3-4) — done. Runtime wiring (Task 2 Step 4) — done. Testing (both tasks) — done. Docs — intentionally out of scope per spec (no in-repo user docs exist to update); the Go doc-comments on `OtherLaunchables()`, `ZaparooLaunchableNamespace`, and `mergeOtherLaunchableDefinitions` are the in-repo documentation this plan produces. Out-of-scope items (`_Console`/`_Computer`, multi-match resolution, hot-reload, actually adding the 7 cores as data) are correctly not present in any task.

**Placeholder scan:** No TBD/TODO markers; every step has complete code.

**Type consistency:** `OtherLaunchable{ID, Name, Category, CorePath}` (Task 1) matches its use in Task 2's `mergeOtherLaunchableDefinitions(builtins []misterOtherLaunchableDefinition, userEntries []config.OtherLaunchable)` and in the `cfg.LoadTOML`-based tests. `misterOtherLaunchableDefinition{ConfigID, Name, Category, CoreName, ID}` field names are used consistently across the definition slice, the merge function, and both new tests. `launchables.ZaparooLaunchableNamespace` name matches between `ids.go` and both test files.
