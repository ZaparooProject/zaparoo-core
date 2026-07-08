# User-configurable `_Other` launchables

## Problem

MiSTer's `_Other` folder holds standalone, non-ROM "launchable" cores (Chess,
Donut, Flappy Bird, etc.). Zaparoo Core only recognizes a fixed set of these,
hardcoded as Go literals in `pkg/platforms/mister/launchables.go` plus UUID
constants in `pkg/launchables/ids.go`. Adding a newly-released `_Other` core
(e.g. Arduboy, Chip8, OpenBOR, PICO-8, Solarus, Sonic Mania, Tamagotchi)
requires a Core code change, a new UUID constant, and an update to a
whitelist-style regression test (`TestLaunchablesOtherCoreDefinitions`,
which asserts an exact count and enumerates every name/category). There is
no user-facing way to register a new one, and no documentation of the
mechanism at all.

This was discovered from a live device: of ~16 distinct apps physically
present in `/media/fat/_Other`, only 9 (Chess, Donut, Epoch Galaxy II,
Flappy Bird, GBMidi, Game of Life, GenMidi, Slug Cross, Tomy Scramble)
appeared in Core's `systems` API response under category `"Other"`.

## Goal

Let a user register additional `_Other` launchables via `config.toml`,
without a Core code change or recompile. Scope is deliberately narrow:
`_Other` only (not `_Console`/`_Computer`), no hot-reload, no attempt to fix
unrelated pre-existing ambiguities. See "Out of scope" below.

## Config shape

New file `pkg/config/configotherlaunchables.go`, following the existing
`configsystems.go` (`Systems.Default` / `[[systems.default]]`) pattern:

```go
type OtherLaunchable struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Category string `toml:"category,omitempty"`
	CorePath string `toml:"core_path"`
}
```

Wired onto `Values` in `pkg/config/config.go` as a new top-level field:

```go
OtherLaunchables []OtherLaunchable `toml:"other_launchables,omitempty"`
```

Example:

```toml
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
category = "Other"
core_path = "Arduboy"

[[other_launchables]]
id = "solarus"
name = "Solarus"
core_path = "Solarus"
```

`core_path` is a **bare filename prefix within `_Other`** (e.g. `"Arduboy"`,
not `"_Other/Arduboy"`) — Core prepends the `_Other/` directory itself. This
keeps the field free of path separators, which is also what the validation
step (below) enforces, removing any directory-traversal surface.

No `config_schema` bump is needed. Confirmed via git history that
`SchemaVersion` has stayed `1` since introduction despite many additive
optional sections being added over time; the check exists to guard breaking
format changes, not purely-additive ones.

## Validation (at config load time)

Following the existing "log a warning, skip the bad entry, keep loading"
idiom used elsewhere in `applyTOML` (regex compile failures) and
`LoadCustomLaunchers` (bad file skip + info summary):

- **Required fields**: `id`, `name`, `core_path` must be non-empty. Missing
  any → `log.Warn()`, skip entry.
- **`core_path` sanity**: must not contain `/`, `\`, or `..`. Violation →
  warn + skip.
- **`category`**: empty → defaults to `"Other"`. Non-empty → validated
  against the known set (`Other`, `Console`, `Computer`, `Arcade` — the
  categories already used across launchables/system metadata today).
  Unknown value → warn + skip the entry entirely (not coerced to
  `"Other"` — an explicit typo shouldn't silently take on a different
  meaning than written).
- **Duplicate `id` within the user's own list**: warn + skip the second
  occurrence, keep the first.

None of these failures abort `Load()` — same fault-tolerance posture as the
rest of `pkg/config`.

## ID stability & conflicts with built-ins

- Every user entry's system UUID is `uuid.NewSHA1(zaparooLaunchableNamespace,
  []byte(strings.ToLower(id)))` (UUID v5) — deterministic; the same `id`
  string always produces the same UUID across restarts and devices.
- Built-ins are given a small canonical lowercase string key (e.g.
  `"chess"`, `"donut"`) purely for matching against user config — a small
  addition to `launchables.go`, not a behavior change for existing installs.
- **Collision with a built-in** (user defines `id = "chess"`): the user's
  `Name`/`Category`/`CorePath` replace the built-in's fields, but the UUID
  **stays the built-in's original fixed constant** (e.g.
  `launchables.MisterOtherChess`), never recomputed. This is deliberate:
  that UUID is the frontend's system-id key for hidden/renamed/cover-art
  overrides. Recomputing it on override would silently strand any existing
  per-system customization the user already has for that built-in.
- Brand-new `id`s with no built-in collision get a fresh v5 UUID.

## Runtime wiring

In `(*Platform).Launchables()` (`pkg/platforms/mister/launchables.go`):

1. Build the existing hardcoded `_Other` entries exactly as today.
2. Load `cfg.OtherLaunchables()` (already validated per above).
3. For each valid user entry, look up its `id` (lowercased) against the
   built-ins' canonical string keys:
   - Match → mutate that `VirtualSystem`'s `Name`/`Category`, rebuild its
     `Launch`/`Test` closures against the user's `CorePath` (UUID
     untouched).
   - No match → construct a new `launchables.VirtualSystem{ID: v5UUID,
     Name, Category, Launch: p.launchOtherCore(path.Join("_Other",
     corePath)), Test: p.otherCoreExists(path.Join("_Other", corePath))}`
     and append it.
4. Existing `validateLaunchables` (duplicate-ID panic guard, required-field
   checks) still runs over the merged list as today.

This reuses `launchOtherCore`/`otherCoreExists` unchanged — only which
entries feed into them changes.

## Testing

- `pkg/config/configotherlaunchables_test.go`: table-driven tests over the
  validation function — valid entry passes; missing `id`/`name`/`core_path`
  skipped with warning; `core_path` with `/`, `\`, `..` rejected; unknown
  `category` rejected; empty `category` defaults to `"Other"`; duplicate
  `id` keeps the first, drops the second.
- `pkg/platforms/mister/launchables_test.go`: new cases alongside the
  existing `TestLaunchablesOtherCoreDefinitions` —
  - a new user entry (no collision) appears in the merged list with a
    stable v5 UUID and launch/test functions wired to its `core_path`.
  - a colliding `id` (e.g. `"chess"`) overrides `Name`/`Category`/
    `CorePath` but the UUID equals `launchables.MisterOtherChess`
    unchanged.
  - the existing `require.Len(t, items, 32)` whitelist assertion is left
    untouched — it only changes if new built-in entries are added, which
    is out of scope here.
- No changes needed to `mgls_test.go` or the `RBFCache` tests.

## Docs

`zaparoo-core` has no local user-facing config docs — they live on the
external zaparoo.org docs site, not in this repo. This design adds a Go
doc-comment on the new `OtherLaunchables()` accessor (matching the
`AudioPauseOnLaunch()`-style convention already used in
`configsystems.go`), but the public docs site needs a matching update from
whoever owns that site — call this out explicitly in the PR description.

## Out of scope

- **`_Console`/`_Computer` folders.** This design covers `_Other` only.
  Extending the same mechanism to the other two folders would be a
  follow-up if needed.
- **Multi-match file resolution.** When multiple files share a prefix
  (e.g. `Solarus_20260627.rbf`, `Solarus_20260708.rbf`,
  `Solarus_TILESTATIC.rbf`), Core's launch path never resolves this itself
  — it writes an `.mgl` with the bare unresolved prefix and lets MiSTer's
  own firmware (`Main_MiSTer`, via `/dev/MiSTer_cmd`) decide which concrete
  file loads. This is true for every `_Other` launchable today, hardcoded
  or user-defined, and is **not** addressed by this design. (A separate
  mechanism, `RBFCache.getByMglGlobLocked`, does deterministic
  newest-wins resolution, but only for a different alt-core registry
  unrelated to `_Other`.)
- **Hot-reload.** Changes to `other_launchables` take effect on Core
  restart only, consistent with how a new `.rbf` file itself only becomes
  usable after Core re-evaluates its launchable list at startup.
- **Actually adding the 7 missing cores as data.** Not needed as part of
  this change — once it ships, the user can add Arduboy, Chip8, OpenBOR,
  PICO-8, Solarus, Sonic Mania, and Tamagotchi themselves via
  `config.toml`.
