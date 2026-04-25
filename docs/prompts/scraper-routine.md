# Scraper Subsystem Design

## Goal

Enable users to enrich their Zaparoo DB with external metadata — developer, publisher, genre, artwork, descriptions, ratings — sourced from local files (gamelist.xml), hosted REST APIs (TheGamesDB, ScreenScraper), or future sources, and surface that data via API and GUI (web, MiSTer).

---

## Design Decisions

### Scrapers enrich, the filesystem scanner creates

The scraper only writes to records that already exist in the DB. The filesystem scanner owns creation of `Media` and `MediaTitle` rows. A gamelist.xml entry with no corresponding indexed `Media` record is skipped, not inserted. This prevents ghost records with no scanner-verified path.

### Scrapers are on-demand, one at a time — no ordering

Scrapers are triggered individually via API command. Only one scraper runs at a time. There is no concept of scraper priority or chained execution. Each scraper manages its own state independently between calls.

The `TagTypes` table carries an `IsExclusive` boolean column. When `IsExclusive = true`, the type is single-value per entity; when `false`, multiple values are allowed. This is **intent metadata, not a schema constraint** — the DB will not enforce it. It is the scraper's responsibility to read the flag and behave accordingly:

- **Exclusive type** (`IsExclusive = 1`): delete all existing tags of that type for the entity, then insert the new value. This ensures a re-run replaces the prior value rather than stacking duplicates.
- **Additive type** (`IsExclusive = 0`): `INSERT OR IGNORE` — the composite PK deduplicates identical values; distinct values accumulate.

If two different scrapers both write the same exclusive type, the one that ran most recently wins — this is a natural consequence of delete-then-insert and requires no coordination.

For **properties** (`MediaTitleProperties`, `MediaProperties`): upsert on the unique constraint. Last writer wins per type per entity regardless of IsExclusive (properties are always one-per-type by schema).

**Canonical IsExclusive assignments** set during `SeedCanonicalTags`:

| Tag type | IsExclusive | Rationale |
|---|---|---|
| `developer`, `publisher` | true | One authoritative value per title |
| `year`, `rating` | true | Single value per title |
| `rev`, `disc`, `disctotal` | true | Describes one specific file |
| `players`, `extension` | true | Single value per file |
| `media`, `arcadeboard` | true | Single value per file |
| `season`, `episode`, `track`, `volume`, `issue` | true | Single value per file |
| `unfinished`, `copyright` | true | Single status value |
| `lang` | false | World/multilingual releases have multiple languages |
| `region` | false | World releases span multiple regions |
| `dump` | false | Hack modifier tags stack (e.g. `hacked` + `hacked-ffe`) |
| `genre`, `compatibility` | false | Games legitimately have multiple genres/hardware targets |

### Sentinel tag tracks scrape completion per Media record

Each scraper writes a namespaced sentinel tag (`scraper.gamelist.xml:scraped`) to the `Media` record after successfully processing it. On re-run, records with a sentinel are skipped unless `Force = true`. The sentinel is written *last* so a crashed mid-write run leaves no sentinel and the record is retried.

The sentinel lives on `Media` (ROM-level) even though most data lands on `MediaTitle` (title-level), because the same title may have multiple ROMs that each need independent path-based matching and per-ROM tags.

### Tags are categorical; Properties are static attributes

**Tags** (`MediaTags`, `MediaTitleTags`): typed key-value pairs, indexed, filterable via the existing tag filter API. Examples: `developer:Capcom`, `lang:en`, `year:1994`. Tags drive queries and ZapScript expressions.

**Properties** (`MediaTitleProperties`, `MediaProperties`): static content for retrieval or display — text descriptions, image paths, video paths, binary blobs. Properties are not queryable by value; they are fetched for a known entity and rendered. A property's *type* (via `TypeTagDBID`) is the canonical classifier; its *content type* (MIME) tells the client how to render it.

The distinction: a genre tag is something you filter by. A description is something you display. One is a predicate; the other is content.

### Properties: one per type per entity

`MediaTitleProperties` and `MediaProperties` both carry `UNIQUE(EntityID, TypeTagDBID)`. There is exactly one property of each type per entity. Upsert replaces the previous value. Because scrapers run on-demand one at a time, the last scraper the user ran for a given type wins.

---

## Schema

### MediaTitleProperties (replaces SupportingMedia)

Properties attached to a `MediaTitle` — shared across all ROMs of the same title. Used for descriptions, shared artwork, videos.

```sql
CREATE TABLE MediaTitleProperties (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Text           text    not null DEFAULT '',
    ContentType    text    not null,
    Binary         blob,
    UNIQUE(MediaTitleDBID, TypeTagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)    REFERENCES Tags(DBID)        ON DELETE RESTRICT
);

CREATE INDEX mediatitleproperties_title_idx   ON MediaTitleProperties(MediaTitleDBID);
CREATE INDEX mediatitleproperties_typetag_idx ON MediaTitleProperties(TypeTagDBID);
```

`Text` carries filesystem paths, plain text, URLs, or any string value. `Binary` carries binary blobs. `ContentType` is a MIME type (`text/plain`, `image/png`, `video/mp4`, `application/pdf`) and tells the client how to interpret the content.

### MediaProperties (new)

Properties attached to a specific `Media` record (ROM-level). Used for region-specific artwork, per-ROM video clips.

```sql
CREATE TABLE MediaProperties (
    DBID        INTEGER PRIMARY KEY,
    MediaDBID   integer not null,
    TypeTagDBID integer not null,
    Text        text    not null DEFAULT '',
    ContentType text    not null,
    Binary      blob,
    UNIQUE(MediaDBID, TypeTagDBID),
    FOREIGN KEY (MediaDBID)     REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)   REFERENCES Tags(DBID)  ON DELETE RESTRICT
);

CREATE INDEX mediaproperties_media_idx   ON MediaProperties(MediaDBID);
CREATE INDEX mediaproperties_typetag_idx ON MediaProperties(TypeTagDBID);
```

### Write pattern for properties

```sql
INSERT INTO MediaTitleProperties (MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(MediaTitleDBID, TypeTagDBID) DO UPDATE SET
    Text        = excluded.Text,
    ContentType = excluded.ContentType,
    Binary      = excluded.Binary;
```

The `DBID` is preserved on conflict — only data columns are updated. This avoids FK breakage if anything ever references a property DBID directly.

### TagTypes

The `TagTypes` table gains an `IsExclusive` column. No schema constraint enforces it — the scraper reads the flag and decides whether to delete-then-insert (exclusive) or INSERT OR IGNORE (additive). `SeedCanonicalTags` sets `IsExclusive` for every canonical type per the table above. Dynamic tag types created at runtime (e.g. `rev:7-2502`) inherit `IsExclusive = 1` from their parent type at creation time.

### Migration

```sql
-- +goose Up
ALTER TABLE TagTypes ADD COLUMN IsExclusive INTEGER NOT NULL DEFAULT 0;

-- Update canonical exclusive types
UPDATE TagTypes SET IsExclusive = 1 WHERE Type IN (
    'developer', 'publisher', 'year', 'rating',
    'rev', 'disc', 'disctotal',
    'players', 'extension',
    'media', 'arcadeboard',
    'season', 'episode', 'track', 'volume', 'issue',
    'unfinished', 'copyright'
);
-- lang, region, dump, genre, compatibility remain IsExclusive = 0

-- +goose Down (IsExclusive)
-- SQLite does not support DROP COLUMN before 3.35; use a table rebuild if targeting older SQLite.
-- For SQLite >= 3.35:
ALTER TABLE TagTypes DROP COLUMN IsExclusive;

-- Rename SupportingMedia → MediaTitleProperties, add Text column (replaces Path),
-- enforce UNIQUE(MediaTitleDBID, TypeTagDBID).

CREATE TABLE MediaTitleProperties (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Text           text    not null DEFAULT '',
    ContentType    text    not null,
    Binary         blob,
    UNIQUE(MediaTitleDBID, TypeTagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)    REFERENCES Tags(DBID)        ON DELETE RESTRICT
);

-- Migrate existing SupportingMedia rows; Path becomes Text.
INSERT OR IGNORE INTO MediaTitleProperties
    (DBID, MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary)
SELECT DBID, MediaTitleDBID, TypeTagDBID, Path, ContentType, Binary
FROM SupportingMedia;

DROP TABLE SupportingMedia;

CREATE INDEX mediatitleproperties_title_idx   ON MediaTitleProperties(MediaTitleDBID);
CREATE INDEX mediatitleproperties_typetag_idx ON MediaTitleProperties(TypeTagDBID);

-- New ROM-level properties table.
CREATE TABLE MediaProperties (
    DBID        INTEGER PRIMARY KEY,
    MediaDBID   integer not null,
    TypeTagDBID integer not null,
    Text        text    not null DEFAULT '',
    ContentType text    not null,
    Binary      blob,
    UNIQUE(MediaDBID, TypeTagDBID),
    FOREIGN KEY (MediaDBID)   REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID) REFERENCES Tags(DBID)  ON DELETE RESTRICT
);

CREATE INDEX mediaproperties_media_idx   ON MediaProperties(MediaDBID);
CREATE INDEX mediaproperties_typetag_idx ON MediaProperties(TypeTagDBID);

-- +goose Down
CREATE TABLE SupportingMedia (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)    REFERENCES Tags(DBID)        ON DELETE RESTRICT
);

INSERT INTO SupportingMedia
    (DBID, MediaTitleDBID, TypeTagDBID, Path, ContentType, Binary)
SELECT DBID, MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary
FROM MediaTitleProperties;

DROP TABLE MediaProperties;
DROP TABLE MediaTitleProperties;
```

---

## Property Type Tag Constants

`TypeTagDBID` references a `Tags` row. The following canonical type tag values must be seeded in the `Tags` table on DB open, alongside other system tag types. Scrapers reference these constants; they do not create property type tags at runtime.

| Tag value | Table | ContentType | Text holds |
|---|---|---|---|
| `property:description` | MediaTitleProperties | `text/plain` | Description text |
| `property:image.boxart` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.screenshot` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.marquee` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.wheel` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.fanart` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.titleshot` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:image.map` | MediaTitleProperties | `image/*` | Absolute filesystem path |
| `property:video` | MediaTitleProperties | `video/*` | Absolute filesystem path |
| `property:manual` | MediaTitleProperties | `application/pdf` | Absolute filesystem path |
| `property:image.boxart` | MediaProperties | `image/*` | Absolute filesystem path (region-specific) |
| `property:video` | MediaProperties | `video/*` | Absolute filesystem path (ROM-specific) |

Property type tags use the `property` tag type, separate from the `supporting` namespace used in the old SupportingMedia design.

---

## The Match Problem

A scraper receives raw data from a source and must bind it to records in the Zaparoo DB. Three strategies, used depending on source type:

1. **Path match** (gamelist.xml): The source provides a filesystem path. Resolve it to absolute via `resolveESPath`, then find `Media.Path = resolvedPath AND Media.SystemDBID = system.DBID`. Most reliable; no ambiguity.

2. **Title slug match** (REST APIs, MAME DBs): The source provides a title string. Slugify with `GenerateSlugWithMetadata`, fuzzy-match against `MediaTitles` for the system using the existing prefilter index. Needs a confidence threshold and a skip path.

3. **External ID match** (ScreenScraper, TheGamesDB): A prior run stored an opaque external ID as a sentinel-namespaced tag (e.g. `scraper.screenscraper:id` with value `"12345"`). Look up that tag, find the `MediaTitle`, update. Fastest on re-runs.

---

## Scraper Interface

```go
// pkg/database/scraper/scraper.go

// Scraper is the interface all metadata scrapers implement.
// Each scraper owns one source: a local file format, a REST API, etc.
type Scraper interface {
    // ID returns the stable scraper identifier used in sentinel tag names.
    // Must be globally unique. Examples: "gamelist.xml", "screenscraper".
    ID() string

    // Scrape starts the goroutine and returns a channel of progress updates.
    // The channel is closed when the goroutine exits (done or cancelled).
    Scrape(ctx context.Context, opts ScrapeOptions) (<-chan ScrapeUpdate, error)
}

// ScrapeOptions configures a scrape run.
type ScrapeOptions struct {
    // Systems limits scraping to these system IDs. Nil means all systems.
    Systems []string

    // Force re-processes records that already have a sentinel tag.
    Force bool
}

// ScrapeUpdate is one progress event from a running scraper.
type ScrapeUpdate struct {
    SystemID  string
    Processed int    // records handled so far in this system
    Total     int    // total records in this system (0 if unknown at start)
    Matched   int    // records that found a DB entry
    Skipped   int    // records with no match or already sentinel-tagged
    Err       error  // non-fatal per-record error; scraper continues
    FatalErr  error  // fatal error; scraper has stopped
    Done      bool   // true on the final update
}
```

---

## Scrape Loop

All scrapers run through the same outer loop. Source-specific steps are marked `[SOURCE]`.

```
RunScraper(ctx, opts, db, scraper):
  systems = resolveSystemsFromOpts(opts.Systems, db)

  for each system:
    emit ScrapeUpdate{SystemID: system.ID, Total: 0}

    // Step 1 [SOURCE]: load records from the source for this system
    records, err = scraper.LoadRecords(ctx, system)
    if err: emit FatalErr, return
    emit ScrapeUpdate{Total: len(records)}

    for each record in records:
      if ctx.Done(): return

      // Step 2: skip if already scraped and not forcing
      if !opts.Force:
        if db.MediaHasTag(record.MediaDBID, sentinelTag(scraper.ID())):
          emit Skipped++; continue

      // Step 3 [SOURCE]: match source record to a Zaparoo Media/MediaTitle
      match, confidence = scraper.Match(ctx, record, system, db)
      if match == nil or confidence < MinConfidence:
        emit Skipped++; continue

      // Step 4 [SOURCE]: map source record to tag/property writes
      mediaTags, titleTags, titleProps, mediaProps = scraper.MapToDB(record)

      // Step 5: write tags
      //   TagTypes.IsExclusive = 1: delete existing tags of that type for
      //     this entity, then insert (enforced by the caller, not the DB)
      //   TagTypes.IsExclusive = 0: INSERT OR IGNORE (composite PK deduplicates)
      db.UpsertMediaTags(match.MediaDBID, mediaTags)
      db.UpsertMediaTitleTags(match.MediaTitleDBID, titleTags)

      // Step 6: write properties (upsert — UNIQUE constraint, last writer wins)
      db.UpsertMediaTitleProperties(match.MediaTitleDBID, titleProps)
      db.UpsertMediaProperties(match.MediaDBID, mediaProps)

      // Step 7: write sentinel last (absent sentinel = safe to retry)
      db.InsertMediaTag(match.MediaDBID, sentinelTag(scraper.ID()))

      emit Processed++, Matched++

  emit Done
```

---

## Concrete Implementation 1: gamelist.xml Scraper

**Source type:** Local filesystem file per system ROM path.
**Match strategy:** Path match.
**Loop strategy:** Source-first (file drives iteration, DB confirms match).
**Package:** `pkg/database/scraper/gamelistxml/`

### Record Loading

```
GamelistXMLScraper.LoadRecords(ctx, system):
  for each rootPath in system.ROMPaths:
    gamelistPath = filepath.Join(rootPath, "gamelist.xml")
    if not exists: continue
    gamelist, err = esapi.ReadGameListXML(gamelistPath)
    if err: warn, continue
    for each game in gamelist.Games:
      yield GamelistRecord{SystemRootPath: rootPath, Game: game}
```

### Matching

```
GamelistXMLScraper.Match(ctx, record, system, db):
  absPath = resolveESPath(record.Game.Path, record.SystemRootPath)
  // resolveESPath handles: ./relative, ~/home-relative, absolute.
  // Returns "" if the path cannot be resolved to an absolute form.
  if absPath == "": log.Warn, return nil, 0

  media = db.FindMediaBySystemAndPath(system.DBID, absPath)
  if media == nil:
    // Not indexed. IsMissing or never scanned. Do not create phantom records.
    log.Debug, return nil, 0

  title = db.FindMediaTitleByDBID(media.MediaTitleDBID)
  return MatchResult{MediaDBID: media.DBID, MediaTitleDBID: title.DBID}, 1.0
```

### Tag and Property Mapping

```
GamelistXMLScraper.MapToDB(record) → mediaTags, titleTags, titleProps, mediaProps:
  game = record.Game

  // --- MediaTags: ROM-level, variant-specific ---
  if game.Lang    != "": mediaTags += tag("lang",    normalizeLanguage(game.Lang))
  if game.Region  != "": mediaTags += tag("region",  normalizeRegion(game.Region))
  // normalizePlayers extracts the upper bound of any range or set:
  //   "1" → "1",  "1-4" → "4",  "1, 2, 4" → "4"
  // players is IsExclusive=true; emits a single players:max tag.
  if game.Players != "": mediaTags += tag("players", normalizePlayers(game.Players))
  // favorite/hidden/kidgame: user-state, not scraped.
  // disc/track: owned by filename parser, not overwritten here.

  // --- MediaTitleTags: title-level, shared across all ROMs ---
  // Caller reads TagTypes.IsExclusive to determine write behaviour.
  // developer, publisher, year, rating → IsExclusive=true (delete-then-insert)
  // genre, arcadesystem, gamefamily   → IsExclusive=false (INSERT OR IGNORE)
  if game.Developer   != "": titleTags += tag("developer", game.Developer)
  if game.Publisher   != "": titleTags += tag("publisher", game.Publisher)
  if game.ReleaseDate != "":
    year = extractYear(game.ReleaseDate)  // "19950311T000000" → "1995"
    if year != "": titleTags += tag("year", year)
  if game.Rating != "":
    titleTags += tag("rating", normalizeRating(game.Rating))  // "0.75" → "75"
  if game.Genre            != "": titleTags += tag("genre", game.Genre)
  if game.ArcadeSystemName != "": titleTags += tag("arcadesystem", game.ArcadeSystemName)
  if game.Family           != "": titleTags += tag("gamefamily", game.Family)

  // --- MediaTitleProperties: title-level static content ---
  if game.Desc != "":
    titleProps += prop("property:description", game.Desc, "text/plain", nil)
  // For each media field: resolve path, emit property if resolvable.
  // Image semantic varies by ES fork (see esapi/gamelist.go); treat as boxart.
  if game.Image    != "": titleProps += pathProp("property:image.boxart",    game.Image,    record.SystemRootPath)
  if game.Thumbnail!= "": titleProps += pathProp("property:image.screenshot",game.Thumbnail,record.SystemRootPath)
  if game.Video    != "": titleProps += pathProp("property:video",            game.Video,    record.SystemRootPath)
  if game.Marquee  != "": titleProps += pathProp("property:image.marquee",   game.Marquee,  record.SystemRootPath)
  if game.Wheel    != "": titleProps += pathProp("property:image.wheel",     game.Wheel,    record.SystemRootPath)
  if game.FanArt   != "": titleProps += pathProp("property:image.fanart",    game.FanArt,   record.SystemRootPath)
  if game.TitleShot!= "": titleProps += pathProp("property:image.titleshot", game.TitleShot,record.SystemRootPath)
  if game.Map      != "": titleProps += pathProp("property:image.map",       game.Map,      record.SystemRootPath)
  if game.Manual   != "": titleProps += pathProp("property:manual",          game.Manual,   record.SystemRootPath)
  // mediaProps: none written by gamelist.xml scraper (all artwork is title-level here)

// pathProp resolves the ES path to absolute; returns nil if unresolvable.
pathProp(typeTag, esPath, systemRootPath):
  abs = resolveESPath(esPath, systemRootPath)
  if abs == "": return nil
  return prop(typeTag, abs, mimeFromExt(abs), nil)
```

**Normalization requirements before implementation:**
- Language codes: gamelist.xml may give `"en"`, `"en,fr"`, or `"English"`. Must map to the same canonical values the filename parser emits (`lang:en`, `lang:fr`). Multi-value strings need splitting and each value emitted separately.
- Ratings: stored as `"0.75"` in gamelist.xml. Internal representation is an open question — see §Open Questions.
- Player counts: `"1-4"` is a range. `normalizePlayers` returns the upper bound as a string (`"4"`), matching the canonical `players:4` tag already seeded by `SeedCanonicalTags`.

---

## Concrete Implementation 2: REST API Scraper (sketch)

**Source type:** External HTTPS API (TheGamesDB, ScreenScraper).
**Match strategy:** Title slug match on first run; external ID tag on subsequent runs.
**Loop strategy:** DB-first (Zaparoo MediaTitles drive the work queue).
**Additional requirements:** rate limiting, HTTP response caching, auth keys in config, resumability via DBConfig checkpoint.

```
RESTAPIScraper.LoadRecords(ctx, system):
  // DB-first: our records drive the queue.
  titles = db.FindMediaTitlesWithoutSentinel(system.DBID, sentinelTag(self.ID()))
  for each title: yield RESTRecord{MediaTitle: title, System: system}

RESTAPIScraper.Match(ctx, record, system, db):
  // Re-run fast path: use stored external ID if available.
  idTag = db.FindMediaTitleTagByType(record.MediaTitle.DBID, "scraper."+self.ID()+":id")
  if idTag != nil:
    data = api.FetchByID(ctx, idTag.Value)
    return MatchResult{MediaTitleDBID: record.MediaTitle.DBID}, 1.0, data

  // First run: search by title, rank results by slug similarity.
  results = api.Search(ctx, record.MediaTitle.Name, system.ExternalSystemID)
  if len(results) == 0: return nil, 0
  best = fuzzyRankAPIResults(results, record.MediaTitle.Slug)
  if best.Score < MinConfidence: return nil, best.Score

  // Store external ID so future runs use the fast path.
  db.InsertMediaTitleTag(record.MediaTitle.DBID, "scraper."+self.ID()+":id", best.ExternalID)

  return MatchResult{MediaTitleDBID: record.MediaTitle.DBID}, best.Score, best.Data
```

---

## Looping Strategies

### Source-first (gamelist.xml)
Source file drives iteration. For each source record, find the matching Zaparoo `Media` by path. Only processes records that have a match. Correct for local-file sources structured around game files.

### DB-first (REST APIs)
Zaparoo `MediaTitles` drive iteration. For each unscraped title, query the external source. Correct when the external source covers all titles as searchable entries.

### Hybrid (ScreenScraper)
First run: DB-first — query our Media, search API by title, store external ID.
Subsequent runs: external-ID-first — query stored IDs, bulk-fetch from API, update.

---

## API Surface

```
// List registered scrapers and their current status
GET /api/v1/scrapers
→ [{id: "gamelist.xml", name: "EmulationStation Gamelist", status: "idle"}, ...]

// Trigger a scrape run (runs in background)
POST /api/v1/scrapers/{id}/run
body: {systems: ["snes", "nes"], force: false}
→ 202 Accepted

// Stream progress events (SSE)
GET /api/v1/scrapers/{id}/status
→ SSE stream of ScrapeUpdate events

// Cancel a running scrape
POST /api/v1/scrapers/{id}/cancel

// Fetch properties for a title (for GUI display)
GET /api/v1/titles/{titleDBID}/properties
→ [{typeTag: "property:description", contentType: "text/plain", text: "..."},
   {typeTag: "property:image.boxart", contentType: "image/png",  text: "/abs/path/to/boxart.png"},
   ...]

// Fetch properties for a specific ROM
GET /api/v1/media/{mediaDBID}/properties
→ [{typeTag: "property:image.boxart", contentType: "image/png", text: "/abs/path/to/jp-boxart.png"}, ...]
```

---

## Data Visibility

- Tags (`developer`, `publisher`, `genre`, `year`, `lang`, `region`) surface in the existing tag filter API and are usable in ZapScript: `media:filter[developer:Capcom]`
- Properties surface via the `/properties` endpoints above, grouped by type for GUI rendering
- No new query indexes are required — tag filtering already works; property fetches are FK lookups on indexed columns

---

## Agent Task Breakdown

1. **Schema migration** — Drop `SupportingMedia`, create `MediaTitleProperties` (with `Text`, `UNIQUE` constraint, migrated rows) and `MediaProperties`. Seed `property:*` type tags on DB open.

2. **TagTypes.IsExclusive** — Add `IsExclusive INTEGER NOT NULL DEFAULT 0` to `TagTypes`. Update `SeedCanonicalTags` to set the flag for all canonical types per the table in §Design Decisions. Dynamic tag types (e.g. `rev:7-2502`) inherit the `IsExclusive` value of their parent type at creation time.

3. **Scraper interface package** — Create `pkg/database/scraper/` with `Scraper` interface, `ScrapeOptions`, `ScrapeUpdate`, `sentinelTag()` helper, and the generic `RunScraper` loop.

4. **MediaDB additions** — Add: `FindMediaBySystemAndPath`, `UpsertMediaTags` (exclusive/additive split), `UpsertMediaTitleTags`, `UpsertMediaTitleProperties`, `UpsertMediaProperties`, `FindMediaTitlesWithoutSentinel`, `MediaHasTag`.

5. **gamelist.xml scraper** — Implement `GamelistXMLScraper` in `pkg/database/scraper/gamelistxml/`. Includes: `resolveESPath`, `LoadRecords`, `Match`, `MapToDB` with full field mapping and normalization. Round-trip tests against fixture XML files.

6. **Scraper registration and API wiring** — Register scrapers with the API server. Wire the gamelist.xml scraper as the first concrete implementation. Connect `RunScraper` to the run/cancel endpoints and SSE status stream.

7. **API endpoints** — Add scraper list, run, status (SSE), cancel, and properties endpoints. Connect to `RunScraper`.
