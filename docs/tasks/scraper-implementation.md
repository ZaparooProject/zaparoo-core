# Scraper Subsystem — Implementation Tasks

Reference design: `docs/prompts/scraper-routine.md`

Each task below is self-contained: it can be reviewed, merged, and tested independently. Dependencies are explicit.

---

## Task 1: Schema migration — MediaTitleProperties, MediaProperties, TagTypes.IsExclusive ✅

**Description**

Drop `SupportingMedia`. Create `MediaTitleProperties` (title-level content, migrated rows) and `MediaProperties` (ROM-level content). Add `IsExclusive INTEGER NOT NULL DEFAULT 0` to `TagTypes`. Seed `property:*` type tags on DB open.

**Context citations**

- Design: `docs/prompts/scraper-routine.md` §Schema, §Property Type Tag Constants
- Existing migration pattern: `pkg/database/mediadb/migrations/` (goose Up/Down)
- `SupportingMedia` current schema: `pkg/database/mediadb/migrations/20250605011734_init.sql`
- Tag seeding site: `pkg/database/mediascanner/indexing_pipeline.go:483` (`SeedCanonicalTags`)
- `TagType` struct: `pkg/database/database.go:155`
- `sqlGetAllTagTypes`, `sqlFindTagType`: `pkg/database/mediadb/sql_tags.go` (lines 238 and 39)

**Acceptance criteria**

1. New goose migration file under `pkg/database/mediadb/migrations/` with a timestamp after `20260424190000`.
2. Up migration:
   - Adds `IsExclusive INTEGER NOT NULL DEFAULT 0` to `TagTypes`.
   - Sets `IsExclusive = 1` for all exclusive canonical types (see design doc table).
   - Creates `MediaTitleProperties` with `UNIQUE(MediaTitleDBID, TypeTagDBID)` and indexes.
   - Migrates existing `SupportingMedia` rows (`Path` → `Text`); uses `INSERT OR IGNORE` to respect the new unique constraint.
   - Drops `SupportingMedia`.
   - Creates `MediaProperties` with `UNIQUE(MediaDBID, TypeTagDBID)` and indexes.
3. Down migration restores `SupportingMedia` (lossy on `MediaProperties` rows — document this).
4. `TagType` struct in `pkg/database/database.go` gains `IsExclusive bool` field.
5. All SQL functions that scan `TagType` rows (`sqlGetAllTagTypes`, `sqlFindTagType`) updated to include `IsExclusive`.
6. `SeedCanonicalTags` in `pkg/database/mediascanner/indexing_pipeline.go` seeds the `property:*` tags listed in the design doc (using `InsertTagType` / `InsertTag` following the existing pattern). It also calls `UPDATE TagTypes SET IsExclusive = 1 WHERE Type IN (...)` for the exclusive canonical types after seeding (or sets the field at insert time if the batch inserter supports it).
7. `go build ./...` passes.
8. Existing `mediadb` tests pass without modification. Migration tests in `pkg/database/mediadb/` pass against the new schema.

**Test cases**

- Apply migration to a DB that has existing `SupportingMedia` rows; verify rows appear in `MediaTitleProperties` with `Text` matching the original `Path`. Verify `SupportingMedia` no longer exists.
- Apply migration to an empty DB; verify all three tables exist with correct columns and constraints.
- Verify `TagTypes.IsExclusive` is `1` for `developer`, `publisher`, `year`, `rating`, `players`, `extension` after migration.
- Verify `TagTypes.IsExclusive` is `0` for `lang`, `region`, `dump`, `genre`, `compatibility` after migration.
- Verify `property:description`, `property:image.boxart`, `property:video` tags exist in `Tags` after `SeedCanonicalTags` runs on a fresh DB.
- Run `go test ./pkg/database/mediadb/...` green.

---

## Task 2: TagTypes.IsExclusive — SeedCanonicalTags and dynamic tag creation ✅

**Depends on:** Task 1 (schema column must exist)

**Description**

`SeedCanonicalTags` must set `IsExclusive` correctly for each canonical type. Dynamic tag types created at runtime (e.g. `rev:7-2502` creates the `rev` type) must inherit `IsExclusive` from a lookup table, not default to `0`.

**Context citations**

- `SeedCanonicalTags`: `pkg/database/mediascanner/indexing_pipeline.go:483`
- `InsertTagType` call site in pipeline: `pkg/database/mediascanner/indexing_pipeline.go:503`
- Tag type definitions: `pkg/database/tags/tag_mappings.go` (dump stacking), `pkg/database/tags/filename_parser.go` (multi-lang/region)
- IsExclusive assignments: `docs/prompts/scraper-routine.md` §Design Decisions (canonical table)

**Acceptance criteria**

1. A `canonicalIsExclusive` map (or equivalent) in `pkg/database/mediascanner/indexing_pipeline.go` (or a new `pkg/database/tags/tagtypes.go`) declares the exclusive/additive assignment for every canonical type.
2. `SeedCanonicalTags` passes `IsExclusive` when calling `InsertTagType` for each canonical type, or issues a single `UPDATE` after seeding.
3. `db.InsertTagType` (and its batch variant) accepts `IsExclusive bool` on the `database.TagType` struct and persists it.
4. Runtime tag type creation (path in `indexing_pipeline.go` for new types discovered from filenames) looks up `canonicalIsExclusive[typeStr]` and sets the field accordingly. Unknown types default to `false`.
5. `go build ./...` passes.
6. Existing indexing tests pass (no change to tag values, only the `IsExclusive` column is new).

**Test cases**

- After a fresh scan, query `TagTypes WHERE Type = 'year'`; assert `IsExclusive = 1`.
- After a fresh scan, query `TagTypes WHERE Type = 'lang'`; assert `IsExclusive = 0`.
- After a fresh scan, query `TagTypes WHERE Type = 'dump'`; assert `IsExclusive = 0`.
- Create a new tag type at runtime for an unknown type string (e.g. `"customtype"`); assert `IsExclusive = 0`.
- Run `go test ./pkg/database/mediascanner/...` green.

---

## Task 3: Scraper interface package ✅

**Depends on:** Task 1 (property type tags must be seedable; `TagType.IsExclusive` must exist)

**Description**

Create `pkg/database/scraper/` containing the `Scraper` interface, shared types, `sentinelTag` helper, and the generic `RunScraper` loop.

**Context citations**

- Interface definition: `docs/prompts/scraper-routine.md` §Scraper Interface
- Loop pseudocode: `docs/prompts/scraper-routine.md` §Scrape Loop
- `MediaDBI` interface (methods the loop calls): `pkg/database/database.go:382`

**Acceptance criteria**

1. New file `pkg/database/scraper/scraper.go` exports:
   - `Scraper` interface with `ID() string` and `Scrape(ctx, opts) (<-chan ScrapeUpdate, error)`.
   - `ScrapeOptions` with `Systems []string`, `Force bool`.
   - `ScrapeUpdate` struct with `SystemID`, `Processed`, `Total`, `Matched`, `Skipped int`, `Err`, `FatalErr error`, `Done bool`.
   - `MatchResult` struct with `MediaDBID`, `MediaTitleDBID int64`.
2. New file `pkg/database/scraper/run.go` exports `RunScraper(ctx, opts, db, scraper)` following the pseudocode in the design doc exactly (sentinel check, match, write tags, write properties, write sentinel last).
3. `sentinelTag(scraperID string) string` returns `"scraper." + scraperID + ":scraped"` — unexported helper in `run.go`, tested via the loop behavior.
4. `RunScraper` depends only on `database.MediaDBI` methods added in Task 4 (compile-time checked). The package must compile once Task 4 stubs exist.
5. `go build ./pkg/database/scraper/...` passes.
6. Unit tests in `pkg/database/scraper/run_test.go` cover:
   - Sentinel skip path (record already has sentinel, `Force=false` → `Skipped++`, no write).
   - Force re-run path (record has sentinel, `Force=true` → writes proceed).
   - `FatalErr` from `LoadRecords` → channel closes, no panics.
   - `ctx.Done()` mid-loop → loop exits cleanly, `Done` is emitted.
   - Non-fatal per-record `Err` → loop continues, error forwarded in `ScrapeUpdate`.

**Test cases**

- Mock `Scraper` with 3 records: 1 already sentinel-tagged, 2 not. `Force=false`. Expect `Skipped=1`, `Matched=2`, `Processed=2`.
- Mock `Scraper` with 3 records: all sentinel-tagged. `Force=true`. Expect `Skipped=0`, `Matched=3`, `Processed=3`.
- Cancel context after first record: confirm channel closes and `Done=true` is the last update.

---

## Task 4: MediaDB additions ✅

**Depends on:** Task 1 (tables must exist)

**Description**

Add the DB access methods that `RunScraper` and the scrapers call. All methods go on `*MediaDB` in `pkg/database/mediadb/` and must be added to the `database.MediaDBI` interface in `pkg/database/database.go`.

**Context citations**

- `MediaDBI` interface: `pkg/database/database.go:394`
- Existing `sqlFindMedia` pattern: `pkg/database/mediadb/sql_media.go:35`
- Existing tag write patterns: `pkg/database/mediadb/sql_media_tags.go`
- Upsert SQL pattern: `docs/prompts/scraper-routine.md` §Write pattern for properties
- IsExclusive behaviour: `docs/prompts/scraper-routine.md` §Design Decisions

**Methods to add**

| Method | Notes |
|--------|-------|
| `FindMediaBySystemAndPath(ctx, systemDBID int64, path string) (*Media, error)` | Returns `nil, nil` when not found (not an error). |
| `MediaHasTag(ctx, mediaDBID int64, tagValue string) (bool, error)` | Used by `RunScraper` for sentinel check. Joins `MediaTags → Tags` on `Tags.Tag = tagValue`. |
| `UpsertMediaTags(ctx, mediaDBID int64, tags []TagInfo) error` | Reads `TagTypes.IsExclusive` per type. Exclusive: `DELETE FROM MediaTags WHERE MediaDBID=? AND TagDBID IN (SELECT DBID FROM Tags WHERE TypeDBID=?)` then insert. Additive: `INSERT OR IGNORE`. |
| `UpsertMediaTitleTags(ctx, mediaTitleDBID int64, tags []TagInfo) error` | Same as above for `MediaTitleTags`. |
| `UpsertMediaTitleProperties(ctx, mediaTitleDBID int64, props []MediaProperty) error` | Upsert on `UNIQUE(MediaTitleDBID, TypeTagDBID)`. |
| `UpsertMediaProperties(ctx, mediaDBID int64, props []MediaProperty) error` | Upsert on `UNIQUE(MediaDBID, TypeTagDBID)`. |
| `FindMediaTitlesWithoutSentinel(ctx, systemDBID int64, sentinelTag string) ([]MediaTitle, error)` | Used by DB-first scrapers. Returns titles in system that have no `Media` row with the given sentinel tag. |
| `FindMediaTitleByDBID(ctx, dbid int64) (*MediaTitle, error)` | Convenience lookup. Returns `nil, nil` if not found. |

Add `MediaProperty` struct to `pkg/database/database.go`:
```go
type MediaProperty struct {
    TypeTagDBID int64
    Text        string
    ContentType string
    Binary      []byte
}
```

**Acceptance criteria**

1. All methods implemented in `pkg/database/mediadb/sql_media.go` (or new focused files: `sql_media_properties.go`, `sql_scraper.go`).
2. All methods added to `database.MediaDBI`.
3. `UpsertMediaTags` correctly performs delete-then-insert for exclusive types and INSERT OR IGNORE for additive types. Both paths verified by test.
4. `UpsertMediaTitleProperties` and `UpsertMediaProperties` preserve `DBID` on conflict (only update data columns).
5. `go build ./...` passes.

**Test cases**

- `FindMediaBySystemAndPath`: insert a `Media` row; assert found. Query with wrong path; assert `nil`.
- `MediaHasTag`: insert `MediaTag`; assert `true`. Query with absent tag; assert `false`.
- `UpsertMediaTags` exclusive: write `developer:Nintendo` then `developer:Capcom` for same media; assert only `developer:Capcom` survives.
- `UpsertMediaTags` additive: write `lang:en` then `lang:fr` for same media; assert both survive.
- `UpsertMediaTitleProperties`: write `property:description` then overwrite; assert only new value survives. `DBID` unchanged.
- `UpsertMediaProperties`: write twice for same `(MediaDBID, TypeTagDBID)`; assert single row remains.
- `FindMediaTitlesWithoutSentinel`: insert 3 titles, 2 have sentinel-tagged media; assert only 1 returned.
- Run `go test ./pkg/database/mediadb/...` green.

---

## Task 5: gamelist.xml scraper ✅

**Depends on:** Tasks 3 and 4 (interface + DB methods must exist)

**Description**

Implement `GamelistXMLScraper` in `pkg/database/scraper/gamelistxml/`. Covers record loading, path matching, and full field mapping with normalization.

**Context citations**

- Scraper concrete design: `docs/prompts/scraper-routine.md` §Concrete Implementation 1
- `esapi.ReadGameListXML`: `pkg/platforms/shared/esapi/gamelist.go`
- `esapi.Game` struct (all fields): `pkg/platforms/shared/esapi/gamelist.go`
- `normalizePlayers` behaviour: `"1-4" → "4"`, `"1, 2, 4" → "4"`, `"1" → "1"`
- Existing language tag normalization: `pkg/database/mediascanner/indexing_pipeline.go` (filename parser)
- Property type tag constants: `docs/prompts/scraper-routine.md` §Property Type Tag Constants

**Acceptance criteria**

1. `pkg/database/scraper/gamelistxml/scraper.go`:
   - `GamelistXMLScraper` implements `scraper.Scraper`.
   - `ID()` returns `"gamelist.xml"`.
   - `Scrape(ctx, opts)` delegates to `RunScraper`.
2. `LoadRecords(ctx, system)`: iterates `system.ROMPaths`, finds `gamelist.xml` at each root, parses via `esapi.ReadGameListXML`, yields `GamelistRecord{SystemRootPath, Game}`.
3. `Match(ctx, record, system, db)`: calls `resolveESPath`, then `db.FindMediaBySystemAndPath`. Returns `nil` match if path unresolvable or media not found (no record created).
4. `resolveESPath(esPath, systemRootPath string) string`:
   - `./relative` → `filepath.Join(systemRootPath, rest)`
   - `~/...` → `filepath.Join(os.UserHomeDir(), rest)`
   - Already absolute → returned as-is.
   - Returns `""` if result is not absolute.
5. `MapToDB(record)` emits all fields per the design doc mapping table. Specifically:
   - `game.Lang` → split on `,`, trim, each value → `lang:` MediaTag.
   - `game.Region` → split on `,`, trim, each value → `region:` MediaTag.
   - `game.Players` → `normalizePlayers` → single `players:` MediaTitleTag.
   - `game.Developer`, `game.Publisher` → `developer:`, `publisher:` MediaTitleTags.
   - `game.ReleaseDate` → `extractYear` → `year:` MediaTitleTag.
   - `game.Rating` → `normalizeRating` (`"0.75"` → `"75"`) → `rating:` MediaTitleTag.
   - `game.Genre` → `genre:` MediaTitleTag.
   - `game.Desc` → `property:description` MediaTitleProperty (`text/plain`).
   - `game.Image`, `game.Thumbnail`, `game.Video`, `game.Marquee`, `game.Wheel`, `game.FanArt`, `game.TitleShot`, `game.Map`, `game.Manual` → corresponding `property:*` MediaTitleProperties via `pathProp`.
   - `favorite`, `hidden`, `kidgame`, `disc`, `track` — **not written** (user-state or filename-parser-owned).
6. `pathProp` skips (returns nil) if `resolveESPath` returns `""`.
7. `normalizePlayers(s string) string` is pure function, no DB access.
8. `go build ./pkg/database/scraper/gamelistxml/...` passes.

**Test cases**

All tests use fixture XML files under `pkg/database/scraper/gamelistxml/testdata/`.

- `TestResolveESPath`: `./roms/game.zip` + `/media/snes` → `/media/snes/roms/game.zip`. Absolute path unchanged. `~/game` resolved. Empty input → `""`.
- `TestNormalizePlayers`: `"1"→"1"`, `"1-4"→"4"`, `"1, 2, 4"→"4"`, `"2-4"→"4"`, `""→""`.
- `TestNormalizeRating`: `"0.75"→"75"`, `"1.0"→"100"`, `"0.0"→"0"`, `""→""`.
- `TestExtractYear`: `"19950311T000000"→"1995"`, `"1995-03-11"→"1995"`, `"1995"→"1995"`, `""→""`.
- `TestMapToDB_FullGame`: fixture with all fields populated; assert all expected tags and properties emitted.
- `TestMapToDB_MultiLang`: `Lang="en,fr,de"` → three `lang:` MediaTags.
- `TestMapToDB_SkipFavorite`: fixture with `Favorite=true`; assert no `favorite:` tag emitted.
- `TestMapToDB_PathPropSkipsUnresolvable`: `Image="./missing/../../../etc/passwd"` with a valid root → `resolveESPath` returns `""` → no property emitted (no path traversal outside root).
- Integration test `TestGamelistXMLScraper_RoundTrip`: create an in-memory MediaDB, index a fixture ROM path, run `GamelistXMLScraper.Scrape`, assert tags and properties written, assert sentinel tag present.
- Run `go test ./pkg/database/scraper/gamelistxml/...` green.

---

## Task 6: Scraper registration and API wiring ✅

**Depends on:** Tasks 3, 4, 5

**Description**

Register scrapers with the API server. Implement run, status (SSE), cancel, and properties endpoints. Wire `GamelistXMLScraper` as the first concrete scraper.

**Context citations**

- API surface: `docs/prompts/scraper-routine.md` §API Surface
- Existing API method pattern: `pkg/api/methods/` (any method file for structure)
- API server router: `pkg/api/server.go`
- SSE pattern: look for existing SSE or streaming endpoints in `pkg/api/methods/`
- `ScrapeUpdate` type: `pkg/database/scraper/scraper.go` (Task 3)

**Acceptance criteria**

1. New file(s) in `pkg/api/methods/scrapers.go` (and `scrapers_test.go`):
   - `GET /api/v1/scrapers` → list registered scrapers with `{id, name, status}`. Status is one of `"idle"`, `"running"`.
   - `POST /api/v1/scrapers/{id}/run` → body `{systems, force}` → 202 Accepted if started; 409 Conflict if a scraper is already running; 404 if `id` not registered.
   - `GET /api/v1/scrapers/{id}/status` → SSE stream of `ScrapeUpdate` JSON events; 404 if not registered.
   - `POST /api/v1/scrapers/{id}/cancel` → signals running scraper to stop; 204 on success; 404/409 as appropriate.
2. New endpoints:
   - `GET /api/v1/titles/{titleDBID}/properties` → `[]PropertyResponse` from `MediaTitleProperties`.
   - `GET /api/v1/media/{mediaDBID}/properties` → `[]PropertyResponse` from `MediaProperties`.
3. `PropertyResponse` model: `{typeTag string, contentType string, text string}` (binary not sent over API).
4. Only one scraper runs at a time (enforced with a mutex or atomic flag on the scraper registry).
5. `GamelistXMLScraper` is registered at startup.
6. Routes registered in `pkg/api/server.go`.
7. `go build ./...` passes.

**Test cases**

- `GET /api/v1/scrapers` when no scraper running: assert `status: "idle"` for all entries.
- `POST /api/v1/scrapers/gamelist.xml/run` while one is already running: assert 409.
- `POST /api/v1/scrapers/nonexistent/run`: assert 404.
- `GET /api/v1/scrapers/gamelist.xml/status` on idle scraper: assert SSE stream returns one `{done: true}` event immediately (or 404 — pick a behaviour and document it).
- `POST /api/v1/scrapers/gamelist.xml/cancel` while running: assert scraper context is cancelled and stream closes.
- `GET /api/v1/titles/1/properties` with no properties: assert empty array (not 404).
- `GET /api/v1/titles/999/properties` (missing title): assert 404.
- Run `go test ./pkg/api/...` green.

---

## Task 7: API models and properties endpoints (properties read path) ✅

**Depends on:** Task 4 (properties DB methods)

**Note:** This task can be done in parallel with Task 6 since it only adds read endpoints, or merged into Task 6 if preferred. Split out here because the properties read path is independently testable.

**Description**

Implement the `GET /titles/{id}/properties` and `GET /media/{id}/properties` endpoints with DB queries to `MediaTitleProperties` and `MediaProperties`.

**Context citations**

- Table schemas: `docs/prompts/scraper-routine.md` §MediaTitleProperties, §MediaProperties
- Property type tags: `docs/prompts/scraper-routine.md` §Property Type Tag Constants
- `MediaDBI` interface: `pkg/database/database.go:394`

**Acceptance criteria**

1. `GetMediaTitleProperties(ctx, mediaTitleDBID int64) ([]MediaProperty, error)` added to `MediaDB` and `MediaDBI`. Joins `MediaTitleProperties → Tags` to resolve `TypeTagDBID` → tag value string.
2. `GetMediaProperties(ctx, mediaDBID int64) ([]MediaProperty, error)` same for `MediaProperties`.
3. `PropertyResponse` JSON model: `{typeTag, contentType, text}`. Binary excluded from API.
4. Both endpoints return `[]PropertyResponse` (empty array, not null, when no rows).
5. Unknown `mediaTitleDBID` or `mediaDBID` returns 404.

**Test cases**

- Insert 2 properties for a title; `GET /api/v1/titles/{id}/properties` returns both with correct `typeTag` strings resolved from `Tags`.
- Insert 0 properties; endpoint returns `[]` (200, not 404).
- Request for unknown `titleDBID`: 404.
- Binary field in DB row: assert `text` field in response is the `Text` column (binary not leaked in JSON).

---

## Task 8: Post-implementation bug fixes and hardening ✅

**Depends on:** Tasks 1–5 (all existing scraper code must be in place)

**Description**

Address concrete bugs, security gaps, and reliability issues found in the initial implementation before API wiring begins.

---

### Fix 1: `GetMediaTitleProperties` / `GetMediaProperties` don't populate `TypeTag` ✅

**File:** `pkg/database/mediadb/sql_scraper.go`

Both `GetMediaTitleProperties` and `GetMediaProperties` currently scan only `TypeTagDBID, Text, ContentType, Binary` and never populate the `MediaProperty.TypeTag` string field. The `database.go` comment says "For reads: TypeTag is populated from the joined Tags row" but the JOIN is absent. This breaks the API layer — `PropertyResponse.typeTag` will always be `""`.

**Fix:** Rewrite both queries to join `Tags t ON mtp.TypeTagDBID = t.DBID` and `TagTypes tt ON t.TypeDBID = tt.DBID`, then `SELECT tt.Type || ':' || t.Tag, mtp.TypeTagDBID, mtp.Text, mtp.ContentType, mtp.Binary`, and scan into `p.TypeTag, p.TypeTagDBID, p.Text, p.ContentType, p.Binary`. Update `scanProperties` to accept the extra column. Verify `TestGetMediaTitleProperties_RoundTrip` asserts `got[0].TypeTag != ""`.

---

### Fix 2: `upsertTags` silently drops sentinel writes for unregistered scraper tag types ✅

**File:** `pkg/database/mediadb/sql_scraper.go`

`upsertTags` logs a warning and skips any tag whose `TagTypes.Type` row doesn't exist yet. The sentinel tag (`{Type: "scraper.gamelist.xml", Tag: "scraped"}`) is written through this path, but `"scraper.gamelist.xml"` is never inserted into `TagTypes`. This means the sentinel is never persisted, so every run re-processes all records.

**Fix:** In `upsertTags`, when `SELECT DBID, IsExclusive FROM TagTypes WHERE Type = ?` returns `sql.ErrNoRows`, auto-create the type with `IsExclusive = false` before continuing (mirrors how tags are lazily created). This covers all runtime-dynamic types — scraper sentinels, custom types from filenames, etc. Add a test `TestUpsertMediaTags_AutoCreatesTagType` that inserts a tag with an unregistered type and asserts the write succeeds and a `TagTypes` row exists afterward.

---

### Fix 3: `resolveESPath` allows path traversal outside the system root for relative inputs ✅

**File:** `pkg/database/scraper/gamelistxml/scraper.go`

For relative inputs (neither absolute nor `~/`), `resolveESPath` calls `filepath.Join(systemRootPath, rel)`. `filepath.Join` normalises `..` components, so `resolveESPath("../../etc/passwd", "/media/snes")` returns `/etc/passwd` — an absolute path that passes the `filepath.IsAbs` guard and is stored in `MediaTitleProperties`. The current test `TestResolveESPath_PathTraversal` accepts this as valid.

**Fix:** After computing `abs` for the non-absolute branch, add a containment check:
```go
if !strings.HasPrefix(abs+string(filepath.Separator), filepath.Clean(systemRootPath)+string(filepath.Separator)) {
    return ""
}
```
Update `TestResolveESPath_PathTraversal` to assert `got == ""`. Add a test `TestResolveESPath_TraversalToAbsolute` that feeds `"../../../etc/passwd"` and asserts `""`.

Note: absolute and `~/` inputs are user-authored absolute paths and are intentionally left unrestricted — only the relative branch needs the containment guard.

---

### Fix 4: `RunScraper` final `Done` update sends zeroed counters ✅

**File:** `pkg/database/scraper/run.go`

After all systems are processed the goroutine emits `ch <- ScrapeUpdate{Done: true}`. The final update carries none of the accumulated `Processed`, `Matched`, `Skipped`, or `SystemID` state. API clients and tests that inspect the `Done` event cannot determine totals without summing every prior update.

**Fix:** Track running totals in outer-loop variables (`totalProcessed`, `totalMatched`, `totalSkipped int`). Accumulate per-system values into them. Replace the terminal emit with:
```go
ch <- ScrapeUpdate{Done: true, Processed: totalProcessed, Matched: totalMatched, Skipped: totalSkipped}
```
All three early ctx.Done() cancel paths also carry the accumulated totals. Update `TestRunScraper_FullWrite_HappyPath` to assert `last.Processed == 1 && last.Matched == 1` directly on the Done event.

---

### Fix 5: `upsertTags` tag INSERT has no `OR IGNORE` and no transaction ✅

**File:** `pkg/database/mediadb/sql_scraper.go` — `upsertTags`

Two issues in the same function:

1. **Race condition:** The SELECT-then-INSERT for new tag rows is not atomic. A concurrent writer can insert the same `(TypeDBID, Tag)` between the two statements, causing the INSERT to fail with a UNIQUE constraint error. Change to `INSERT OR IGNORE INTO Tags (TypeDBID, Tag) VALUES (?, ?)` then re-query for the DBID.
2. **No transaction:** Each call to `UpsertMediaTags` or `UpsertMediaTitleTags` issues 3–4 individual statements per tag with no surrounding transaction. For a game with 10 tags that means 30+ round-trips and no atomicity guarantee. Wrap the entire `upsertTags` loop in a `BEGIN`/`COMMIT` (using the caller's `*sql.DB` with `db.BeginTx`).

**Acceptance criteria:** `TestUpsertMediaTags_Idempotent` must still pass. Add a stress test `TestUpsertMediaTags_Concurrent` that spawns 5 goroutines writing the same tag simultaneously and asserts no error and exactly one row in `Tags`.

---

### Fix 6: `Thumbnail` is mapped to `property:image-screenshot` — misleading convention ✅

**File:** `pkg/database/scraper/gamelistxml/scraper.go` — `MapToDB`

`game.Thumbnail` is written as `property:image-screenshot`. In most ES forks (RPI, Sky, Batocera, ES-DE) `<thumbnail>` holds **cover art**, not a screenshot; `<image>` holds the primary composite or screenshot image. Mapping thumbnail → screenshot silently discards cover art when box art (`<image>`) and cover (`<thumbnail>`) are used together.

**Fix:** Add `TagPropertyImageThumbnail TagValue = "image-thumbnail"` to `pkg/database/tags/tag_values.go` and seed it under `TagTypeProperty` in `tags.go`. Map `game.Thumbnail` → `property:image-thumbnail` in `MapToDB`. Update `TestMapToDB_FullGame` to assert the thumbnail property uses `image-thumbnail`. Add a comment in `scraper.go` linking to the field-level fork documentation in `esapi/gamelist.go`.

---

### Fix 7: `players` is an exclusive type but is mapped as a MediaTag, not a MediaTitleTag ✅

**File:** `pkg/database/scraper/gamelistxml/scraper.go` — `MapToDB`; `pkg/database/tags/tagtypes.go`

`TagTypePlayers` is in `CanonicalIsExclusive` with `IsExclusive = true`, and `players` represents game metadata (not a per-ROM variant). However `MapToDB` emits it as a `MediaTag` (ROM-level). For games with multiple ROMs (different regional dumps), each ROM would overwrite the title's player count independently, producing divergent state. `players` should be a `MediaTitleTag` for the same reason `developer` and `genre` are.

**Fix:** Move the `players` block in `MapToDB` from the `mediaTags` section to the `titleTags` section. Update `TestMapToDB_FullGame` to assert `players` appears in `titleTags`, not `mediaTags`.
