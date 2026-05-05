# Scraper Requirements — gamelistxml

End-result requirements and test expectations for the gamelist.xml scraper.
Implementation-agnostic: valid before and after any refactor.

---

## Requirements

### Inputs

- **R1** Scraper receives: system list (each with ID, DBID, ROM root paths), DB handle, FS handle, request options (`Force bool`).
- **R2** Scraper emits progress updates (processed / matched / skipped counts) on a channel; channel closed when done.

### Gamelist discovery

- **R3** For each system ROM root, look for `gamelist.xml` at the root level. Skip silently if missing or unreadable; continue to next root.
- **R4** Multiple ROM roots per system are each processed independently.

### Media matching

- **R5** Load all indexed media for the system from DB before matching.
- **R6** Match each media record to a gamelist entry using normalized absolute path (case-insensitive, slash-normalized). First match wins.
- **R7** Fallback: if no path match, match by lowercase basename — only when (a) the gamelist entry is at depth-1 under root, and (b) the media path is a descendant of the root.
- **R8** When multiple gamelist entries resolve to the same path or basename key, the first entry wins; duplicates are discarded.
- **R9** Unmatched media records → silently skipped, no DB write.
- **R10** Gamelist entries with no corresponding indexed media record → silently skipped.

### Force / skip logic

- **R11** `Force=false`: skip any media already marked as scraped by this scraper (sentinel tag). No re-write.
- **R12** `Force=true`: process all matched records regardless of prior scrape status.

### DB writes — on successful match

- **R13** Write sentinel tag `scraper:<scraperID> = scraped` to media record after successful write.
- **R14** ROM-level tags (per media variant): `lang` and `region`, each split on comma → one tag per value, lowercased.
- **R15** Title-level tags (shared across all ROMs for a title): developer, publisher, year, rating, genre, players, arcadeBoard, gameFamily.
- **R16** Title-level properties: description, screenscraper ID, image (9 types: image / boxart / screenshot / thumbnail / marquee / wheel / fanart / titleshot / map), video, manual.
- **R17** No ROM-level properties written by this scraper.

### Field normalization (applied before any mapping)

- **R18** HTML entities unescaped (`&amp;` → `&`, etc.).
- **R19** Tab, CR, LF replaced with single space.
- **R20** Leading/trailing whitespace trimmed.
- **R21** Empty string after normalization → field omitted, no tag/prop written.

### Field-specific normalization

- **R22** Year: extract first 4 characters of date string; reject if any character is non-digit. Formats handled: `YYYYMMDDTHHMMSS`, `YYYY-MM-DD`, `YYYY`.
- **R23** Rating: parse as float 0–1, multiply × 100, round to nearest int, store as string (e.g. `"0.75"` → `"75"`).
- **R24** Players: split on comma/hyphen/space, parse all numeric tokens, store the maximum (e.g. `"1-4"` → `"4"`, `"1, 2, 4"` → `"4"`).

### Image / asset path resolution

- **R25** For each image property: use the XML-provided path if non-empty and valid. If XML path absent or invalid, scan `<root>/media/<subdir>/` for `<stem>.png` using the ordered candidate directory list for that property type; first existing file wins.
- **R26** `stem` = ROM filename without extension.
- **R27** Candidate directory not present under `media/` → skip that candidate, continue list.
- **R28** Filesystem scan checks exact `<stem>.png` filename only (no glob, no extension fallback).

### Path normalization (matching)

- **R29** Before comparing any two paths, normalize: convert all separators to forward slash, apply `filepath.Clean` (collapse `..`, duplicate slashes, trailing slash), lowercase the result.
- **R30** Path comparison is always case-insensitive regardless of OS filesystem case sensitivity.
- **R31** A media path and gamelist path that differ only in separator style (`\` vs `/`), casing, or redundant segments (`./`, `../resolved`) must match.
- **R32** Normalization is applied to both sides of every comparison — media DB paths and gamelist-resolved paths alike.

### Path security

- **R33** All resolved asset paths must be strict descendants of the system root path. Any path that would escape the root (via `..` or absolute redirect) is silently dropped — no tag/prop written.

### Progress and lifecycle

- **R34** Emit progress (processed / matched / skipped) at max one update per 250 ms during the record loop; always emit on error, match-skip, or completion.
- **R35** Respect context cancellation at: system boundary, record loop iteration, DB write. Emit `Done` update before closing channel.
- **R36** Pause/resume support: scraper must yield at pause points without data loss.
- **R37** `LoadRecords` failure → fatal; emit `FatalErr`, stop all processing.
- **R38** Context cancel during `LoadRecords` → clean cancellation (no `FatalErr`).
- **R39** Match error → non-fatal; skip record, emit error in update, continue.
- **R40** DB write failure → non-fatal; skip record, emit error in update, continue.

---

## Test Expectations

| # | Input | Expected outcome |
|---|-------|-----------------|
| T1 | Media path matches gamelist entry exact path | Record written with correct tags and props |
| T2 | Gamelist entry at root depth-1, media at nested subpath | Basename fallback matches; record written |
| T3 | Media path not in any gamelist | Record skipped; no DB write |
| T4 | Gamelist entry not in indexed media | Entry ignored; no DB write |
| T5 | Two gamelist entries resolve same normalized path | First entry used; second ignored |
| T6 | Media already scraped, `Force=false` | Skipped; sentinel not re-written |
| T7 | Media already scraped, `Force=true` | Re-processed; DB updated |
| T8 | `lang="en,fr"` | Two tags: `lang:en`, `lang:fr` |
| T9 | `region=" US , EU "` | Two tags: `region:us`, `region:eu` (trimmed, lowercased) |
| T10 | Rating `"0.75"` | Title tag `rating:75` |
| T11 | Rating `"1"` | Title tag `rating:100` |
| T12 | Players `"1-4"` | Title tag `players:4` |
| T13 | Players `"1, 2, 4"` | Title tag `players:4` |
| T14 | ReleaseDate `"19941130T000000"` | Title tag `year:1994` |
| T15 | ReleaseDate `"1994-11-30"` | Title tag `year:1994` |
| T16 | ReleaseDate `"abc4"` | No year tag |
| T17 | ES path `./roms/game.sfc` | Resolved to absolute under root |
| T18 | ES path `~/roms/game.sfc` | Resolved relative to OS home dir |
| T19 | ES path `../../etc/passwd` | Dropped; no prop written |
| T20 | ES path pointing outside root (absolute) | Dropped; no prop written |
| T21 | Image field present in XML, file exists | XML path used as property value |
| T22 | Image field absent, `<stem>.png` in first candidate dir | Filesystem path used as property value |
| T23 | Image field absent, first candidate dir missing, file in second | Second candidate dir path used |
| T24 | Image field absent, no candidate dir has `<stem>.png` | No image prop written |
| T25 | Field contains `&amp;` / `&lt;` | Stored as `&` / `<` |
| T26 | Field contains embedded `\n` | Replaced with space |
| T27 | Context cancelled mid-record-loop | `Done` update emitted; channel closed cleanly |
| T28 | `LoadRecords` returns non-context error | `FatalErr` set in done update |
| T29 | Context cancel inside `LoadRecords` | Clean done update; no `FatalErr` |
| T30 | DB write fails for one record | That record skipped; subsequent records still processed |
| T31 | `gamelist.xml` missing from one root, present in another | Missing root skipped; second root processed normally |
| T32 | Gamelist entry basename collision (two entries same filename at root depth-1) | First entry wins for basename fallback |
| T33 | Media path is not a descendant of root | Basename fallback not attempted |
| T34 | Scraper ID used in sentinel tag | Sentinel tag type = `scraper:<ID>`, value = `scraped` |
| T35 | `ScreenScraperIDAttr` non-empty and non-zero | Attr form used for screenscraper ID prop |
| T36 | `ScreenScraperIDAttr` empty, `ScreenScraperID` non-zero | Int form used |
| T37 | Both screenscraper fields zero/empty | No screenscraper ID prop written |
| T38 | Media path uses `\` separators, gamelist entry uses `/` | Paths match after normalization |
| T39 | Media path uppercase, gamelist entry lowercase | Paths match after case normalization |
| T40 | Gamelist entry path has redundant `./` prefix | Resolved and cleaned before comparison; matches correctly |
| T41 | Media path has `..` segment that resolves within root | Cleaned before comparison; matches correctly |
| T42 | Two paths differ only in trailing slash | Match succeeds |
| T43 | Root path itself uses mixed separators | Normalization applied to root before any relative comparison |
