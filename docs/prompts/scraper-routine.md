# Scraper Subsystem

Scrapers enrich existing `Media` and `MediaTitle` records in the MediaDB with metadata from external sources — developer, publisher, genre, ratings, artwork paths, descriptions. The filesystem scanner owns record *creation*; scrapers only write to records that already exist.

---

## Package Structure

```
pkg/database/scraper/
    scraper.go          — Scraper, ScraperLoop[T] interfaces, shared types
    run.go              — Generic RunScraper[T] loop + sentinel helpers
    gamelistxml/
        scraper.go      — GamelistXMLScraper implementation

pkg/database/mediadb/
    sql_scraper.go      — DB methods: UpsertMediaTags, UpsertMediaProperties, etc.

pkg/api/methods/
    media_scrape.go     — HandleMediaScrape, HandleMediaScrapeCancel (JSON-RPC)
    media_image.go      — HandleMediaImage (JSON-RPC)
    scrapers.go         — REST handlers: GET /properties endpoints
```

---

## Interfaces

### `Scraper` (public)

```go
type Scraper interface {
    ID() string
    Scrape(ctx context.Context, opts ScrapeOptions) (<-chan ScrapeUpdate, error)
}
```

Registered in `RequestEnv.Scrapers` (immutable map, populated at server startup). The `media.scrape` handler looks up the scraper by `ScraperID` from params.

### `ScraperLoop[T]` (internal)

Concrete scrapers implement this and pass `self` to `RunScraper`:

```go
type ScraperLoop[T any] interface {
    ID() string
    LoadRecords(ctx context.Context, system ScrapeSystem) ([]T, error)
    Match(ctx context.Context, record T, system ScrapeSystem, db database.MediaDBI) (*MatchResult, error)
    MapToDB(record T) MapResult
}
```

### Supporting types

```go
type ScrapeOptions struct {
    Systems []string  // nil/empty = all systems
    Force   bool      // ignore sentinel tags; re-process everything
}

type ScrapeUpdate struct {
    SystemID  string
    Processed int
    Total     int
    Matched   int
    Skipped   int
    Err       error  // non-fatal; loop continues
    FatalErr  error  // fatal; loop stops
    Done      bool   // true on final update (including cancelled)
}

type ScrapeSystem struct {
    ID       string
    DBID     int64
    ROMPaths []string
}

type MatchResult struct {
    MediaDBID      int64
    MediaTitleDBID int64
}

type MapResult struct {
    MediaTags  []database.TagInfo
    TitleTags  []database.TagInfo
    TitleProps []database.MediaProperty
    MediaProps []database.MediaProperty
}
```

---

## Run Loop (`RunScraper[T]`)

`pkg/database/scraper/run.go`. Generic over the record type T. Systems must be pre-resolved by the caller (DBID, ID, ROMPaths all set).

```
RunScraper(ctx, opts, systems, db, scraper):
  sentinel = "scraper." + scraper.ID() + ":scraped"

  for each system:
    emit ScrapeUpdate{SystemID, Total: 0}

    records, err = scraper.LoadRecords(ctx, system)
    if err: emit FatalErr, return

    emit ScrapeUpdate{SystemID, Total: len(records)}

    for each record:
      if ctx.Done(): emit Done with totals, return

      // Match first — sentinel check needs MediaDBID
      match = scraper.Match(ctx, record, system, db)
      if match == nil: skipped++; continue

      // Skip already-scraped unless Force
      if !opts.Force && db.MediaHasTag(match.MediaDBID, sentinel):
        skipped++; continue

      mapped = scraper.MapToDB(record)

      db.UpsertMediaTags(match.MediaDBID, mapped.MediaTags)
      db.UpsertMediaTitleTags(match.MediaTitleDBID, mapped.TitleTags)
      db.UpsertMediaTitleProperties(match.MediaTitleDBID, mapped.TitleProps)
      db.UpsertMediaProperties(match.MediaDBID, mapped.MediaProps)

      // Sentinel written last: absent sentinel = safe to retry after crash
      db.UpsertMediaTags(match.MediaDBID, []TagInfo{sentinelTagInfo(scraper.ID())})

      processed++; matched++
      emit ScrapeUpdate{processed, matched, skipped}

  emit ScrapeUpdate{Done: true, Processed: total, Matched: total, Skipped: total}
```

Per-record non-fatal errors (match errors, write errors) increment `skipped` and carry `Err` in the update; the loop continues. Fatal errors from `LoadRecords` close the channel immediately.

---

## Tag Semantics

### `TagTypes.IsExclusive`

`TagTypes` has an `IsExclusive INTEGER NOT NULL DEFAULT 0` column. This is intent metadata — the DB does not enforce it. `upsertTags` reads it and applies the correct write strategy:

- **Exclusive (`IsExclusive = 1`)**: delete all existing tags of that type for the entity, then insert. Ensures a re-run replaces the previous value instead of stacking.
- **Additive (`IsExclusive = 0`)**: `INSERT OR IGNORE` — the composite PK deduplicates identical rows; distinct values accumulate.

Runtime scraper sentinel types (e.g. `scraper.gamelist.xml`) are auto-created as additive when first encountered.

Canonical assignments (set by `SeedCanonicalTags`):

| Type | IsExclusive |
|---|---|
| `developer`, `publisher`, `year`, `rating` | true |
| `rev`, `disc`, `disctotal` | true |
| `players`, `extension` | true |
| `media`, `arcadeboard` | true |
| `season`, `episode`, `track`, `volume`, `issue` | true |
| `unfinished`, `copyright` | true |
| `lang`, `region`, `dump` | false |
| `genre`, `compatibility`, `gamefamily` | false |

### `upsertTags` implementation (`sql_scraper.go`)

All operations run in a single transaction. Tags are grouped by type before processing so `deleteFn` is called at most once per exclusive type (prevents multiple tags of the same exclusive type clobbering each other within one call). Tag rows that don't exist yet are created with `INSERT OR IGNORE` followed by a re-query.

---

## Property Storage

**`MediaTitleProperties`** — title-level content, shared across all ROMs of the same title (descriptions, shared artwork).

**`MediaProperties`** — ROM-level content (region-specific artwork, per-ROM clips).

Both tables have `UNIQUE(EntityID, TypeTagDBID)`. Upsert preserves `DBID`; only data columns (`Text`, `ContentType`, `Binary`) are updated on conflict. Last writer wins per type per entity.

`TypeTagDBID` references the `Tags` table (which references `TagTypes`). Property type tags are seeded by `SeedCanonicalTags` under the `property` tag type.

### Property type tag constants (`pkg/database/tags/tag_values.go`)

| Constant | Tag value | ContentType | Text holds |
|---|---|---|---|
| `TagPropertyDescription` | `description` | `text/plain` | Plain text |
| `TagPropertyImageBoxart` | `image-boxart` | `image/*` | Absolute filesystem path |
| `TagPropertyImageScreenshot` | `image-screenshot` | `image/*` | Absolute filesystem path |
| `TagPropertyImageThumbnail` | `image-thumbnail` | `image/*` | Absolute filesystem path (ES `<thumbnail>`) |
| `TagPropertyImageMarquee` | `image-marquee` | `image/*` | Absolute filesystem path |
| `TagPropertyImageWheel` | `image-wheel` | `image/*` | Absolute filesystem path |
| `TagPropertyImageFanart` | `image-fanart` | `image/*` | Absolute filesystem path |
| `TagPropertyImageTitleshot` | `image-titleshot` | `image/*` | Absolute filesystem path |
| `TagPropertyImageMap` | `image-map` | `image/*` | Absolute filesystem path |
| `TagPropertyVideo` | `video` | `video/*` | Absolute filesystem path |
| `TagPropertyManual` | `manual` | `application/pdf` | Absolute filesystem path |

Full tag value: `property:<value>` (e.g. `property:image-boxart`).

---

## gamelist.xml Scraper

**Package:** `pkg/database/scraper/gamelistxml/`  
**ID:** `"gamelist.xml"`  
**Match strategy:** Path match — ES path resolved to absolute, then looked up via `FindMediaBySystemAndPathFold`.  
**Loop strategy:** Source-first — gamelist.xml entries drive iteration, DB confirms each match.

### `resolveESPath(esPath, systemRootPath string) string`

- `./relative` or `relative` → `filepath.Join(systemRootPath, rel)`; containment-checked against `systemRootPath` to prevent `../` traversal outside the root.
- `~/...` → `filepath.Join(os.UserHomeDir(), rest)`
- Already absolute → returned as-is.
- Returns `""` if result is not absolute or (for relative inputs) escapes the root.

Note: absolute and `~/` inputs are user-authored paths and are intentionally left unrestricted. Only the relative branch is containment-checked.

### `MapToDB` field mapping

All string fields are cleaned first: HTML entities unescaped, control whitespace collapsed to spaces, trimmed.

**MediaTags (ROM-level):**
| ES field | Tag type | Notes |
|---|---|---|
| `Lang` | `lang` | Split on `,`; each value lowercased; additive |
| `Region` | `region` | Split on `,`; each value lowercased; additive |

**MediaTitleTags (title-level):**
| ES field | Tag type | Notes |
|---|---|---|
| `Developer` | `developer` | Exclusive |
| `Publisher` | `publisher` | Exclusive |
| `ReleaseDate` | `year` | `extractYear` → first 4 chars; exclusive |
| `Rating` | `rating` | `normalizeRating` → `"0.75"` → `"75"`; exclusive |
| `Genre` | `genre` | Additive |
| `Players` | `players` | `normalizePlayers` → upper bound of range/set; title-level exclusive |
| `ArcadeSystemName` | `arcadeboard` | Exclusive |
| `Family` | `gamefamily` | Additive |

**MediaTitleProperties:**
| ES field | Property type tag | Notes |
|---|---|---|
| `Desc` | `property:description` | `text/plain` |
| `Image` | `property:image-boxart` | Path via `pathProp` |
| `Thumbnail` | `property:image-thumbnail` | Cover art in most ES forks |
| `Video` | `property:video` | Path via `pathProp` |
| `Marquee` | `property:image-marquee` | Path via `pathProp` |
| `Wheel` | `property:image-wheel` | Path via `pathProp` |
| `FanArt` | `property:image-fanart` | Path via `pathProp` |
| `TitleShot` | `property:image-titleshot` | Path via `pathProp` |
| `Map` | `property:image-map` | Path via `pathProp` |
| `Manual` | `property:manual` | Path via `pathProp` |

**Not written:** `Favorite`, `Hidden`, `KidGame` (user-state), `Disc`, `Track` (filename-parser-owned).

No ROM-level properties (`MediaProps`) are written by the gamelist.xml scraper.

---

## API Surface

### JSON-RPC (WebSocket)

**`media.scrape`** — Start a scraper run as a background operation. Returns immediately. Progress broadcast as `media.scraping` notifications.

Params (`MediaScrapeParams`):
```json
{
  "scraperId": "gamelist.xml",
  "systems": ["snes", "nes"],
  "force": false
}
```

**`media.scrape.cancel`** — Cancel the running scrape. Returns `{"message": "..."}`.

**`media.scraping` notification** — Broadcast on each `ScrapeUpdate` and on completion.

Payload (`ScrapingStatusResponse`):
```json
{
  "scraperId": "gamelist.xml",
  "systemId": "snes",
  "processed": 42,
  "total": 100,
  "matched": 38,
  "skipped": 4,
  "scraping": true,
  "done": false
}
```

Only one scraper runs at a time. Indexing and scraping are mutually exclusive. `media.scrape` returns a client error if either is already running.

**`media.image`** — Fetch a single best-match image for a media record as a base64-encoded blob.

Params (`MediaImageParams`):
```json
{
  "mediaId": 123,
  "imageTypes": ["image", "boxart", "screenshot"]
}
```

`"image"` is an alias for `"boxart"`. If `imageTypes` is omitted, the default preference order is used: `image, boxart, screenshot, wheel, titleshot, map, marquee, fanart`. Media-level properties take priority over title-level for the same type. Stale file paths (file deleted from disk) are cleaned from the DB automatically and the next preference is tried.

Response (`MediaImageResponse`):
```json
{
  "contentType": "image/png",
  "data": "<base64>",
  "typeTag": "property:image-boxart"
}
```

### REST

**`GET /api/v1/titles/{titleDBID}/properties`** — Returns all `MediaTitleProperties` for a title as `[]PropertyResponse`. Empty array when no properties. 404 when title not found.

**`GET /api/v1/media/{mediaDBID}/properties`** — Returns all `MediaProperties` for a ROM as `[]PropertyResponse`. Empty array when no properties. 404 when media not found.

`PropertyResponse`:
```json
{
  "typeTag": "property:image-boxart",
  "contentType": "image/png",
  "text": "/absolute/path/to/boxart.png"
}
```

Binary blobs are not included in these responses. Use `media.image` (JSON-RPC) to retrieve binary image data.

---

## DB Methods (`database.MediaDBI`)

Methods added for scraper support in `pkg/database/mediadb/sql_scraper.go`:

| Method | Notes |
|---|---|
| `FindMediaBySystemAndPath(ctx, systemDBID, path)` | Exact path match. `nil, nil` when not found. |
| `FindMediaBySystemAndPathFold(ctx, systemDBID, path)` | Case-insensitive path match via `LOWER()`. Used by gamelist.xml scraper. |
| `MediaHasTag(ctx, mediaDBID, tagValue)` | Checks `"type:value"` string against MediaTags. Used for sentinel check. |
| `UpsertMediaTags(ctx, mediaDBID, tags)` | Wraps `upsertTags` for `MediaTags`. |
| `UpsertMediaTitleTags(ctx, mediaTitleDBID, tags)` | Wraps `upsertTags` for `MediaTitleTags`. |
| `UpsertMediaTitleProperties(ctx, mediaTitleDBID, props)` | Upsert on `UNIQUE(MediaTitleDBID, TypeTagDBID)`. |
| `UpsertMediaProperties(ctx, mediaDBID, props)` | Upsert on `UNIQUE(MediaDBID, TypeTagDBID)`. |
| `DeleteMediaTitleProperty(ctx, mediaTitleDBID, typeTagDBID)` | Used by `media.image` to remove stale file refs. |
| `DeleteMediaProperty(ctx, mediaDBID, typeTagDBID)` | Used by `media.image` to remove stale file refs. |
| `GetMediaTitleProperties(ctx, mediaTitleDBID)` | Returns `[]MediaProperty` with `TypeTag` populated via JOIN. |
| `GetMediaProperties(ctx, mediaDBID)` | Returns `[]MediaProperty` with `TypeTag` populated via JOIN. |
| `GetMediaWithTitleAndSystem(ctx, mediaDBID)` | Single JOIN returning `*MediaFullRow`. Used by `media.image`. |
| `FindMediaTitlesWithoutSentinel(ctx, systemDBID, sentinelTag)` | For DB-first scrapers. |
| `FindMediaTitleByDBID(ctx, dbid)` | Convenience lookup. `nil, nil` if not found. |
