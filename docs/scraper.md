# Scraper Subsystem

The scraper subsystem enriches existing MediaDB records with metadata from external sources. The filesystem scanner owns record creation; scrapers update records that already exist.

Current scraper implementations:

- `gamelist.xml` imports EmulationStation metadata such as developer, publisher, genre, rating, player count, descriptions, artwork paths, videos, manuals, and ScreenScraper game IDs.
- `media-folder` imports image paths from EmulationStation-style `media/` folders under each system folder. It does not read `gamelist.xml`, download assets, or write non-image metadata. A force run (re-scrape) also deletes stale image properties whose paths match the same local media-folder convention and whose replacement file is no longer found.

## Code Layout

| Path | Purpose |
|---|---|
| `pkg/database/scraper/` | Shared scrape types (`ScrapeOptions`, `ScrapeUpdate`), sentinel helper, and small channel startup helper |
| `pkg/database/scraper/gamelistxml/` | EmulationStation `gamelist.xml` scraper loop, matcher, mapper, and companion-entry handling |
| `pkg/database/scraper/localmedia/` | EmulationStation `media/` folder image-path importer |
| `pkg/platforms/shared/esmedia/` | Shared EmulationStation media-folder path resolver |
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
3. Loads eligible indexed titles for slug matching (`force=true` loads all titles; otherwise titles without the scraper sentinel are loaded).
4. Loads indexed media rows for the system.
5. With `force=false`, removes media rows that already have sentinel tag `scraper.gamelist.xml:scraped` from path fallback candidates.
6. Loads `gamelist.xml` from each ROM root.
7. Resolves each `<game>` path under its ROM root.
8. Computes the same display-name slug used by the original scraper and prefers that title match.
9. Uses the resolved path to select the concrete Media row for the slug-matched title when possible; otherwise falls back to the first Media row for that title.
10. If slug matching fails, falls back to case-insensitive path matching so otherwise missed records can still scrape.
11. Maps XML fields to media-level tags/properties plus title-level shared tags/properties.
12. Writes metadata through `MediaDB.ApplyScrapeResult`.
13. Writes the scraper sentinel tag to the selected Media row last inside the same transaction.
14. Emits progress updates and a final done update.

The sentinel tag format is `scraper.<id>:scraped`, for example `scraper.gamelist.xml:scraped`. Writing it last is intentional: if a normal record write fails, the transaction rolls back and the missing sentinel leaves that media row eligible for retry.

Force scrapes also persist a run ID and write `scraper-run.<id>:<run-id>` to each media row completed in that operation. If Core restarts mid-force-scrape, resume reuses that run ID and skips rows already marked for the same run while still refreshing older rows that only had the normal sentinel. Run markers are removed when the operation reaches a terminal state.

Per-record write failures are non-fatal: they increment `Skipped`, emit `Err`, and continue. Fatal setup/load/database errors end the run with a terminal update unless caused by context cancellation.

## Tags And Properties

The DB supports tags/properties at both media and title scope. Normal `gamelist.xml` scraping writes per-ROM `region`/`lang` tags and shared title metadata.

| Storage | Scope | Current normal `gamelist.xml` use |
|---|---|---|
| `MediaTags` | ROM-level variant metadata | region, lang, scraper sentinel |
| `MediaTitleTags` | Title-level shared metadata | developer, publisher, year, rating, genre, players, arcadeboard, gamefamily |
| `MediaTitleProperties` | Title-level shared static content | description, XML game ID |
| `MediaProperties` | ROM-level static content | artwork paths, video path, manual path for normal `gamelist.xml` entries |

Tag exclusivity is controlled by `TagTypes.IsExclusive`. Exclusive types replace existing values for that type; additive types accumulate distinct values. The scraper write path groups tags by type and applies that behavior in `upsertTags`.

Property rows are keyed by entity and property type tag. Re-scraping the same property type updates the row in place and preserves row DBID.

Path-backed properties persist their text path and optional `BlobDBID`; the property tables do not persist the `ContentType` computed by the mapper for path values. Blob-backed properties expose content type from `MediaBlobs`. API responses infer path-backed content type and extension from the stored path when DB content type is empty.

Normal gamelist artwork is media-level so regional or language variants can carry different image paths while sharing one title. `media.image` checks media-level properties before title-level properties. If an older scrape left title-level artwork behind, run a force scrape to refresh media-level artwork; no migration removes old title properties.

## Media-level Sentinel Invariant

Normal `gamelist.xml` entries prefer slug/title matching, then use path matching to select the concrete Media row when possible. If no slug match exists, path-only fallback can still select a Media row. The sentinel is written to the same Media row that receives ROM-level tags such as `region` and `lang` plus ROM-level file properties such as artwork.

Title metadata remains shared by `MediaTitleDBID`, so multiple ROM variants can write the same title-level tags/properties. Rewrites are idempotent: exclusive title tags replace same-type values, additive tags are inserted-or-ignored, and properties upsert by type. Media-level properties upsert per concrete Media row, preventing regional artwork variants from overwriting each other.

## gamelist.xml Behavior

`GamelistXMLScraper` scans each system ROM root for `gamelist.xml`. Regular `<game>` entries are resolved to absolute paths under the system ROM root. The scraper first matches the entry to an existing title by the original display-name slug behavior, then uses the resolved path to choose the concrete Media row for that title when possible. If slug matching finds a title but the path does not identify a Media row for that title, only title-level metadata is written. If no known title slug exists, it falls back to case-insensitive path matching. Scrapers do not create `Media` or `MediaTitle` rows.

Path handling for `<game><path>` stays strict:

| Input | Behavior |
|---|---|
| `./relative` or `relative` | Resolved under the system ROM root and rejected if it escapes that root |
| `~/...` | Resolved under the current user's home directory, then rejected unless still under the system ROM root |
| Absolute path | Cleaned and rejected unless under the system ROM root |

Asset path handling for artwork/video/manual uses the same root-bound behavior by default. On MiSTer and MiSTeX only, absolute or `~/...` asset paths may also resolve under platform root directories from `RootDirs(cfg)`, covering SD, USB, CIFS, network, and configured index roots. This applies only to file-backed asset fields; game paths remain bound to the ROM root. Path traversal outside the ROM root or approved platform roots is rejected.

Zip-as-directory paths are supported for matching XML entries such as `./Japan/Game.zip` to indexed media stored under that zip path, while nested artwork paths such as `./media/images/Japan/Game.png` remain resolved as asset paths.

Source fields are cleaned before mapping: HTML entities are unescaped, tab/newline/carriage-return characters become spaces, and surrounding whitespace is trimmed.

### Field Mapping

| ES field | Destination | Notes |
|---|---|---|
| `lang` | `MediaTags: lang` | CSV split, trimmed, lowercased, additive |
| `region` | `MediaTags: region` | CSV split, trimmed, lowercased, additive |
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
| `image` | `MediaProperties: property:image-image` | XML path or filesystem fallback |
| `thumbnail` | `MediaProperties: property:image-thumbnail` | Cover/thumbnail path in most ES forks |
| `boxart2d` | `MediaProperties: property:image-boxart` | XML path or filesystem fallback |
| `boxart3d` | `MediaProperties: property:image-boxart3d` | XML path or filesystem fallback |
| `screenshot` | `MediaProperties: property:image-screenshot` | XML path or filesystem fallback |
| `video` | `MediaProperties: property:video` | Filesystem path |
| `marquee` | `MediaProperties: property:image-marquee` | XML path or filesystem fallback |
| `logo` / `wheel` | `MediaProperties: property:image-wheel` | `logo` takes priority over `wheel`; XML path or filesystem fallback |
| `fanart` | `MediaProperties: property:image-fanart` | XML path or filesystem fallback |
| `titlescreen` / `titleshot` | `MediaProperties: property:image-titleshot` | `titlescreen` takes priority over `titleshot`; XML path or filesystem fallback |
| `map` | `MediaProperties: property:image-map` | XML path or filesystem fallback |
| `manual` | `MediaProperties: property:manual` | PDF path |

Filesystem fallback searches known subdirectories under `<systemRootPath>/media/` when an XML path is absent. For games in subfolders, it searches the mirrored ROM-relative path before the flat filename; for example `./Japan/Game.nes` checks `media/images/Japan/Game.png` before `media/images/Game.png`. Side/back box art are filesystem-fallback only.

By default, only `<ROM root>/gamelist.xml` files are loaded. Nested files such as `<ROM root>/Japan/gamelist.xml` are not read.

An additional metadata bundle can be configured independently of ROM storage:

```toml
[scraper.gamelist_xml]
custom_path = "/path/to/gamelists"
```

For each indexed system, the scraper also checks `<custom_path>/<system ID>/gamelist.xml`. Game paths in this file resolve against the system's first ROM root; asset paths resolve against the custom system directory. Custom bundle image references are optional: only files present during scraping are stored, and missing references fall back to `media/` artwork under the custom directory and then the system's ROM roots. Run the scraper again after installing more bundle artwork. Regular ROM-root gamelists are processed first and take precedence over matching custom entries.

Custom gamelists enrich existing indexed records; they do not create systems, titles, or media rows. For systems that index virtual or non-file-backed entries (where the stored media path does not correspond to a real file), `<path>` must match the exact path the indexer stored for that media row.

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
| `media.image` | Returns the best matching image property as base64 data for one media row, including thumbnail art |
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
  "paused": false,
  "state": "running",
  "totalSteps": 2,
  "currentStep": 1,
  "currentStepDisplay": "Super Nintendo Entertainment System",
  "currentSystem": {
    "systemId": "snes",
    "systemName": "Super Nintendo Entertainment System",
    "processed": 42,
    "total": 100,
    "matched": 38,
    "skipped": 4
  }
}
```

`totalScraped` is derived from scraper sentinel tags in the database, not from the current run's `matched` count. Existing flat fields stay for compatibility; new UIs should use `currentSystem` for current-system progress and `totalSteps`/`currentStep`/`currentStepDisplay` for whole-run system-step progress.

Only one scraper can run at a time, and scraping is mutually exclusive with media indexing.

`media.meta` returns the metadata graph for media rows: media-level tags and properties, title-level tags and properties, and stored system identity. Single requests accept `mediaId` or `system`/`path` and keep the single-response shape; batch requests use `items` and return per-item results. Binary property bytes are not included; clients should use `media.image` for image data. On platforms that treat zips as directories, a `system`/`path` request for a folder or zip-as-directory container resolves only when its direct contents collapse to one logical launch target.

`media.image` accepts one media ref plus image type preferences such as `image`, `boxart`, `boxart3d`, `screenshot`, `wheel`, `titleshot`, `map`, `marquee`, and `fanart`. These resolve to canonical image property tags; for example `boxart` becomes `property:image-boxart` and `image` becomes `property:image-image`. Media-level properties are preferred over title-level properties for the same type. On zip-as-directory platforms, logical container aliases are checked as media-level fallbacks, so artwork attached to a direct single-game target or its container can be found from either path. For stale image properties in these canonical tags, such as missing file paths for `property:image-boxart` or `property:image-image`, `media.image` logs the stale property in memory only and does not delete DB rows; lookup falls through to the next available source.

## Useful Focused Tests

```bash
go test ./pkg/database/scraper/...
go test ./pkg/database/mediadb/ -run 'Scrape|Property|Blob|Sentinel|MediaImage'
go test ./pkg/api/methods/ -run 'Scrape|MediaImage|MediaMeta'
```
