# Scraper Subsystem

The scraper subsystem enriches existing MediaDB records with metadata from external sources. The filesystem scanner owns record creation; scrapers update records that already exist.

The only current scraper implementation is `gamelist.xml`, which imports EmulationStation metadata such as developer, publisher, genre, rating, player count, descriptions, artwork paths, videos, manuals, and ScreenScraper game IDs.

## Code Layout

| Path | Purpose |
|---|---|
| `pkg/database/scraper/` | Shared scrape types (`ScrapeOptions`, `ScrapeUpdate`), sentinel helper, and small channel startup helper |
| `pkg/database/scraper/gamelistxml/` | EmulationStation `gamelist.xml` scraper loop, matcher, mapper, and companion-entry handling |
| `pkg/platforms/*` | Platform scraper registration through `Platform.Scrapers` |
| `pkg/database/mediadb/sql_scraper.go` | MediaDB scraper read/write helpers, property/blob helpers, and metadata graph queries |
| `pkg/api/methods/media_scrape.go` | JSON-RPC scrape start/status/cancel/resume handlers and scraper listing |
| `pkg/api/methods/media_meta.go` | JSON-RPC metadata graph lookup for media rows |
| `pkg/api/methods/media_image.go` | JSON-RPC image lookup from scraped properties |

## Registration And API Lifecycle

Platforms expose available scrapers with:

```go
Scrapers(*config.Instance) map[string]platforms.Scraper
```

`platforms.Scraper` carries `ID`, `Name`, `SupportedSystemIDs`, optional `CustomOpts`, and a `Scrape` callback. The callback receives context, config, platform, filesystem, database, shared scrape options, custom options, and an update channel.

`media.scrape` looks up the requested `scraperId` from `env.Platform.Scrapers(env.Config)`, rejects the request if media indexing or another scrape is active, creates an app-scoped cancelable context, starts the scraper in the background, tracks it as a MediaDB background operation, and publishes `media.scraping` notifications.

`media.scrape.status` returns the latest in-memory status snapshot plus a fresh scraped-count query. `media.scrape.cancel` cancels the active scrape context. `media.scrape.resume` resumes the shared scrape pauser. Scraping and indexing are mutually exclusive.

## Run Loop

There is no generic source-record scrape loop. `pkg/database/scraper/run.go` only provides a small helper for wrapping callback/channel startup. The `gamelist.xml` implementation owns its loop in `GamelistXMLScraper.scrapeLoop`.

For each system, the normal loop:

1. Resolves target systems from indexed MediaDB systems and platform launcher paths.
2. Runs ZaparooCompanion processing first. This is a special path; see [ZaparooCompanion Entries](#zaparoocompanion-entries).
3. Builds candidate titles:
   - `force=true`: all titles for the system.
   - `force=false`: titles without sentinel tag `scraper.gamelist.xml:scraped`.
4. Loads `gamelist.xml` from each ROM root.
5. Resolves each `<game>` path under its ROM root and derives a scanner-compatible title slug, using `<name>` as provided name when present.
6. Matches the slug to an existing `MediaTitle` row.
7. Chooses the first Media DBID for the matched title as the sentinel write target.
8. Maps XML fields to title-level tags and properties.
9. Writes metadata through `MediaDB.ApplyScrapeResult`.
10. Writes the scraper sentinel tag last inside the same transaction.
11. Emits progress updates and a final done update.

The sentinel tag format is `scraper.<id>:scraped`, for example `scraper.gamelist.xml:scraped`. Writing it last is intentional: if a normal record write fails, the transaction rolls back and the missing sentinel leaves that title eligible for retry.

Per-record write failures are non-fatal: they increment `Skipped`, emit `Err`, and continue. Fatal setup/load/database errors end the run with a terminal update unless caused by context cancellation.

## Tags And Properties

The DB supports tags/properties at both media and title scope, but the normal `gamelist.xml` scraper currently writes title metadata only.

| Storage | Scope | Current normal `gamelist.xml` use |
|---|---|---|
| `MediaTags` | ROM-level variant metadata | Sentinel tag only for normal entries; companion child `region`/`lang` only |
| `MediaTitleTags` | Title-level shared metadata | developer, publisher, year, rating, genre, players, arcadeboard, gamefamily |
| `MediaTitleProperties` | Title-level static content | description, artwork paths, video path, manual path, XML game ID |
| `MediaProperties` | ROM-level static content | Supported by DB helpers, not currently written by normal `gamelist.xml` entries |

Tag exclusivity is controlled by `TagTypes.IsExclusive`. Exclusive types replace existing values for that type; additive types accumulate distinct values. The scraper write path groups tags by type and applies that behavior in `upsertTags`.

Property rows are keyed by entity and property type tag. Re-scraping the same property type updates the row in place and preserves row DBID.

Path-backed properties persist their text path and optional `BlobDBID`; the property tables do not persist the `ContentType` computed by the mapper for path values. Blob-backed properties expose content type from `MediaBlobs`. API responses can infer extensions from content type or text path, but path-backed `contentType` may be empty.

## Title-level Metadata Invariant

Normal `gamelist.xml` metadata is shared by title. The sentinel is written to one Media row associated with the matched title, and `FindMediaTitlesWithoutSentinel` skips a title if any associated Media row has that sentinel.

This is safe while the normal scraper writes only `MediaTitleTags` and `MediaTitleProperties`. Future per-ROM metadata imports need a different sentinel strategy, such as tagging every media row or splitting title-level and media-level sentinel tags.

## gamelist.xml Behavior

`GamelistXMLScraper` scans each system ROM root for `gamelist.xml`. Regular `<game>` entries are resolved to absolute paths under the system ROM root, converted to scanner-compatible slugs, and matched to existing `MediaTitle` rows. Scrapers do not create `Media` or `MediaTitle` rows.

Path handling:

| Input | Behavior |
|---|---|
| `./relative` or `relative` | Resolved under the system ROM root and rejected if it escapes that root |
| `~/...` | Resolved under the current user's home directory, then rejected unless still under the system ROM root |
| Absolute path | Cleaned and rejected unless under the system ROM root |

Source fields are cleaned before mapping: HTML entities are unescaped, tab/newline/carriage-return characters become spaces, and surrounding whitespace is trimmed.

### Field Mapping

Regular ES `lang` and `region` fields are not currently imported for normal game entries.

| ES field | Destination | Notes |
|---|---|---|
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
| `boxart2d` | `MediaTitleProperties: property:image-boxart` | XML path or filesystem fallback |
| `boxart3d` | `MediaTitleProperties: property:image-boxart3d` | XML path or filesystem fallback |
| `screenshot` | `MediaTitleProperties: property:image-screenshot` | XML path or filesystem fallback |
| `video` | `MediaTitleProperties: property:video` | Filesystem path |
| `marquee` | `MediaTitleProperties: property:image-marquee` | XML path or filesystem fallback |
| `logo` / `wheel` | `MediaTitleProperties: property:image-wheel` | `logo` takes priority over `wheel`; XML path or filesystem fallback |
| `fanart` | `MediaTitleProperties: property:image-fanart` | XML path or filesystem fallback |
| `titlescreen` / `titleshot` | `MediaTitleProperties: property:image-titleshot` | `titlescreen` takes priority over `titleshot`; XML path or filesystem fallback |
| `map` | `MediaTitleProperties: property:image-map` | XML path or filesystem fallback |
| `manual` | `MediaTitleProperties: property:manual` | PDF path |

Filesystem fallback searches known subdirectories under `<systemRootPath>/media/` for a `<rom filename stem>.png` file when an XML path is absent. Side/back box art are filesystem-fallback only.

`gamelist.xml` deliberately does not scrape user-state fields such as favorite, hidden, or kidgame. It also does not overwrite filename-parser-owned fields such as disc and track.

## ZaparooCompanion Entries

`gamelist.xml` has a special path for entries marked with `source="ZaparooCompanion"` as either a `source` attribute or `<source>` element.

Companion records are split into:

- Parent entries: have an ID attribute and no path. They carry shared title metadata.
- Child entries: have `parentid` and path. They reference parent metadata.

Child matching:

- Paths ending in `.slug` match an existing title by slug, then use the first Media row for that title as the write target.
- Other child paths first try an exact case-insensitive media path lookup.
- If exact lookup fails, the scraper falls back to filename suffix matching with `FindMediaBySystemAndPathSuffix`.
- Ambiguous suffix matches are skipped instead of updating multiple same-basename media rows.

For matched children, parent metadata is written onto the child title, child `region` and `lang` are written to the child Media row as media-level tags, and the scraper sentinel is written to that child Media row. These writes use `ApplyScrapeResult`, so title metadata, child tags, and the sentinel are committed together.

Current caveats:

- Companion processing still runs before normal title filtering.
- With `force=false`, child media rows that already have the `scraper.gamelist.xml:scraped` sentinel are skipped.
- Companion processed/matched/skipped counts contribute to run counters, but companion entries do not have a separate total in status updates.

These caveats document current behavior, not necessarily desired long-term behavior.

## API Surface

JSON-RPC methods:

| Method | Purpose |
|---|---|
| `scrapers` | Lists registered scrapers with ID, name, and supported systems |
| `media.scrape` | Starts a scraper run as a background operation |
| `media.scrape.status` | Returns latest in-memory scraper status plus current DB scraped count |
| `media.scrape.cancel` | Cancels the active scraper run |
| `media.scrape.resume` | Resumes a paused scraper run |
| `media.meta` | Returns tags and metadata-only properties for one or more media rows and their titles |
| `media.image` | Returns the best matching image property as base64 data for one media row |
| `media.clean.orphans` | Removes missing media rows and orphaned related data |

`media.scrape` params:

```json
{
  "scraperId": "gamelist.xml",
  "systems": ["snes", "nes"],
  "force": false
}
```

Progress is queryable with `media.scrape.status` and broadcast as `media.scraping` notifications:

```json
{
  "scraperId": "gamelist.xml",
  "systemId": "snes",
  "processed": 42,
  "total": 100,
  "matched": 38,
  "skipped": 4,
  "totalScraped": 1000,
  "scraping": true,
  "done": false,
  "paused": false
}
```

`totalScraped` is derived from scraper sentinel tags in the database, not from the current run's `matched` count.

Only one scraper can run at a time, and scraping is mutually exclusive with media indexing.

`media.meta` returns the metadata graph for media rows: media-level tags and properties, title-level tags and properties, and stored system identity. Single requests accept `mediaId` or `system`/`path` and keep the single-response shape; batch requests use `items` and return per-item results. Binary property bytes are not included; clients should use `media.image` for image data.

`media.image` accepts one media ref plus image type preferences such as `image`, `boxart`, `boxart3d`, `screenshot`, `wheel`, `titleshot`, `map`, `marquee`, and `fanart`. These resolve to canonical image property tags; for example `boxart` becomes `property:image-boxart` and `image` becomes `property:image-image`. Media-level properties are preferred over title-level properties for the same type. Stale file path properties are removed automatically and lookup falls through to the next available source.

## Useful Focused Tests

```bash
go test ./pkg/database/scraper/...
go test ./pkg/database/mediadb/ -run 'Scrape|Property|Blob|Sentinel|MediaImage'
go test ./pkg/api/methods/ -run 'Scrape|MediaImage|MediaMeta'
```
