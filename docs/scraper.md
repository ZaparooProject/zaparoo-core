# Scraper Subsystem

The scraper subsystem enriches existing MediaDB records with metadata from external sources. The filesystem scanner owns record creation; scrapers only update records that already exist.

The first scraper implementation is `gamelist.xml`, which imports EmulationStation metadata such as developer, publisher, genre, rating, player count, descriptions, artwork paths, videos, manuals, and ScreenScraper game IDs.

## Code Layout

| Path | Purpose |
|---|---|
| `pkg/database/scraper/` | Public scraper interface, shared types, and generic run loop |
| `pkg/database/scraper/gamelistxml/` | EmulationStation `gamelist.xml` implementation |
| `pkg/database/mediadb/sql_scraper.go` | MediaDB tag/property reads and writes used by scrapers |
| `pkg/api/methods/media_scrape.go` | JSON-RPC scrape start/cancel handlers and scraper listing |
| `pkg/api/methods/media_image.go` | JSON-RPC image lookup from scraped properties |
| `pkg/api/methods/media_meta.go` | JSON-RPC metadata graph lookup for media rows |

## Interfaces

All metadata scrapers implement `scraper.Scraper`:

```go
type Scraper interface {
	ID() string
	Name() string
	SupportedSystems() []string
	Scrape(ctx context.Context, opts ScrapeOptions) (<-chan ScrapeUpdate, error)
}
```

`ID` is stable API and is also used in sentinel tag names. `SupportedSystems` returns an empty slice when the scraper supports all systems.

Concrete scrapers plug into the shared loop by implementing `ScraperLoop[T]`:

```go
type ScraperLoop[T any] interface {
	ID() string
	LoadRecords(ctx context.Context, system ScrapeSystem) ([]T, error)
	Match(ctx context.Context, record T, system ScrapeSystem, db database.MediaDBI) (*MatchResult, error)
	MapToDB(record T) MapResult
}
```

The API layer registers scraper instances in `RequestEnv.Scrapers`. `media.scrape` looks up the scraper by `scraperId` and runs it as a background operation.

## Run Loop

`RunScraper` in `pkg/database/scraper/run.go` is generic over the source record type. The caller must pass resolved systems with `ID`, `DBID`, and `ROMPaths` populated.

For each system, the loop:

1. Emits an initial update with an unknown total.
2. Loads source records with `LoadRecords`.
3. Emits the total record count.
4. Matches each source record to an existing `Media` and `MediaTitle` row.
5. Skips already-scraped records unless `force` is set.
6. Maps matched records to tag and property writes.
7. Writes media tags, title tags, title properties, and media properties.
8. Writes the scraper sentinel tag last.

The sentinel tag format is `scraper.<id>:scraped`, for example `scraper.gamelist.xml:scraped`. Writing it last is intentional: if a run fails halfway through a record, the missing sentinel leaves that record eligible for retry on the next run.

`Match` failures and write failures are non-fatal per-record errors. They increment `Skipped`, emit `Err`, and the loop continues. `LoadRecords` failures are fatal unless they are caused by context cancellation. The channel always closes after a terminal `Done` or `FatalErr` update.

## Tags And Properties

Scrapers write normal MediaDB tags and properties:

| Storage | Scope | Examples |
|---|---|---|
| `MediaTags` | ROM-level variant metadata | language, region, scraper sentinel |
| `MediaTitleTags` | Title-level shared metadata | developer, publisher, year, rating, genre, players |
| `MediaTitleProperties` | Title-level static content | description, artwork paths, video path, manual path, XML game ID |
| `MediaProperties` | ROM-level static content | Supported by the loop, but not currently written by `gamelist.xml` |

Tag exclusivity is controlled by the canonical `TagTypes.IsExclusive` flag. Exclusive tag types replace existing values for that type; additive tag types accumulate distinct values. The database does not enforce this directly. The scraper write path groups tags by type and applies the correct behavior in `upsertTags`.

Property rows are keyed by entity and property type tag. Re-scraping the same property type updates the row in place and preserves the row DBID.

## gamelist.xml Behavior

`GamelistXMLScraper` scans each system ROM root for `gamelist.xml`. Each `<game>` entry is resolved to an absolute path and matched against existing media with `FindMediaBySystemAndPathFold`.

Path handling:

| Input | Behavior |
|---|---|
| `./relative` or `relative` | Resolved under the system ROM root and rejected if it escapes that root |
| `~/...` | Resolved under the current user's home directory |
| Absolute path | Cleaned and used as-is |

Source fields are cleaned before mapping: HTML entities are unescaped, control whitespace is collapsed, and surrounding whitespace is trimmed.

### Field Mapping

| ES field | Destination | Notes |
|---|---|---|
| `lang` | `MediaTags: lang` | CSV split, lowercased, additive |
| `region` | `MediaTags: region` | CSV split, lowercased, additive |
| `developer` | `MediaTitleTags: developer` | Exclusive |
| `publisher` | `MediaTitleTags: publisher` | Exclusive |
| `releasedate` | `MediaTitleTags: year` | First four characters when present |
| `rating` | `MediaTitleTags: rating` | Normalized from `0..1` style ratings to `0..100` text |
| `genre` | `MediaTitleTags: genre` | Additive |
| `players` | `MediaTitleTags: players` | Highest player count from ranges/lists |
| `arcadesystemname` | `MediaTitleTags: arcadeboard` | Exclusive |
| `family` | `MediaTitleTags: gamefamily` | Additive |
| `desc` | `MediaTitleProperties: property:description` | Plain text |
| ScreenScraper game ID | `MediaTitleProperties: property:xml-game-id` | From XML attribute or element value |
| `image` | `MediaTitleProperties: property:image-image` | XML path or filesystem fallback |
| `thumbnail` | `MediaTitleProperties: property:image-thumbnail` | Cover/thumbnail path in most ES forks |
| `video` | `MediaTitleProperties: property:video` | Filesystem path |
| `marquee` | `MediaTitleProperties: property:image-marquee` | XML path or filesystem fallback |
| `wheel` | `MediaTitleProperties: property:image-wheel` | XML path or filesystem fallback |
| `fanart` | `MediaTitleProperties: property:image-fanart` | XML path or filesystem fallback |
| `titleshot` | `MediaTitleProperties: property:image-titleshot` | XML path or filesystem fallback |
| `map` | `MediaTitleProperties: property:image-map` | XML path or filesystem fallback |
| `manual` | `MediaTitleProperties: property:manual` | PDF path |

`gamelist.xml` deliberately does not scrape user-state fields such as favorite, hidden, or kidgame. It also does not overwrite filename-parser-owned fields such as disc and track.

For image properties, the scraper first uses the XML path if present. If the XML field is empty, it searches known media subdirectories under `<systemRootPath>/media/` for a file matching the ROM filename stem. `image-boxart` and `image-screenshot` are filesystem-fallback only because they do not have dedicated EmulationStation XML fields.

## API Surface

JSON-RPC methods:

| Method | Purpose |
|---|---|
| `scrapers` | Lists registered scrapers with ID, name, and supported systems |
| `media.scrape` | Starts a scraper run as a background operation |
| `media.scrape.cancel` | Cancels the active scraper run |
| `media.meta` | Returns tags and properties for one media row and its title |
| `media.image` | Returns the best matching image property as base64 data |
| `media.clean.orphans` | Removes missing media rows and orphaned related data |

`media.scrape` params:

```json
{
  "scraperId": "gamelist.xml",
  "systems": ["snes", "nes"],
  "force": false
}
```

Progress is broadcast as `media.scraping` notifications:

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

Only one scraper can run at a time, and scraping is mutually exclusive with media indexing.

`media.meta` returns the full metadata graph for a single media row: media-level tags and properties, title-level tags and properties, and the stored system identity.

`media.image` accepts image type preferences such as `image`, `boxart`, `screenshot`, `wheel`, `titleshot`, `map`, `marquee`, and `fanart`. These resolve to canonical image property tags; for example `boxart` becomes `property:image-boxart` and `image` becomes `property:image-image`. Media-level properties are preferred over title-level properties for the same type. Stale file paths are removed automatically and lookup falls through to the next available source.
