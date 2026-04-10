# Title normalization and matching

Zaparoo matches games by title, not filename. Write a game name like "The Legend of Zelda: Ocarina of Time" and it gets fuzzy-matched against indexed ROM filenames. Both the query and the filenames go through the same slug pipeline, so naming differences wash out.

Slugs are not IDs. They're a normalized form used for matching. The original titles and filenames stick around for display and fallback matching.

Offline-first, no hashing (too slow on MiSTer FPGA / older Pis). English heuristics only for now.

## How it works

### Indexing

When scanning media, Zaparoo:

1. Cleans path and extracts filename (strips extension and path)
2. Parses filename to extract a clean display title (see [Filename parser](#filename-parser))
3. Extracts tags from brackets/parentheses (see [Tag extraction](#tag-extraction))
4. Determines media type from the system (Game, TVShow, Movie, Music, Image, Audio, Video, Application)
5. Slugifies the title using media-type-aware normalization (see [Slug normalization](#slug-normalization))
6. Stores path, title, slug, tags, and metadata in the database

### Resolution

When launching by title (e.g. `launch.title` with arg `NES/Super Mario Bros`):

1. Parses `SystemID/GameName` format
2. Extracts and merges tags from the query (three sources — see [Tags in queries](#tags-in-queries))
3. Slugifies the game name using the same normalization as indexing
4. Tries matching strategies in order until finding a result (see [Matching strategies](#matching-strategies))
5. Scores and filters results to pick the best match
6. Caches the resolution for future queries

---

## Slug normalization

Two-phase pipeline that converts titles into a canonical form. Indexing and resolution run the same normalization.

`pkg/database/slugs/slugify.go` → `Slugify(mediaType, input)`

### Phase 1: Media-specific parsing

Format-specific normalization runs before the universal pass.

**Games** (`pkg/database/slugs/media_parsing_game.go`):

Width normalization first (fullwidth → ASCII), then:

1. Split on `:` and strip leading articles: "The Zelda: Link's Awakening" → "Zelda Link's Awakening"
2. Strip trailing articles: "Legend, The" → "Legend"
3. Strip metadata brackets: `(USA)`, `[!]`, `{Europe}` → removed
4. Strip edition/version suffixes: "Edition", "Version", "v1.0" → removed
5. Normalize symbols/separators (preserve commas for trailing article detection)
6. Expand abbreviations: "Bros" → "brothers", "vs" → "versus", "Dr" → "doctor"
7. Expand number words: "one" → "1", "two" → "2" (1-20)
8. Normalize ordinals: "2nd" → "2", "3rd" → "3"
9. Convert roman numerals: "VII" → "7", "II" → "2" (preserves "X" in "Mega Man X")

**TV Shows** (`pkg/database/slugs/media_parsing_tv.go`):

Width normalization first, then:

1. Strip scene tags: quality, codec, source (1080p, x264, BluRay, etc.)
2. Dots → spaces (scene release convention)
3. Strip metadata brackets
4. Normalize date episodes: various date formats → `YYYY-MM-DD`
5. Normalize season-based: `S01E02`, `1x02`, `S01.E02` → `s01e02`
6. Normalize absolute numbering: `Episode 001`, `Ep 42`, `#001` → `e001`
7. Reorder components: episode marker placed after show name
8. Split titles and strip articles
9. Strip trailing articles

**Movies** (`pkg/database/slugs/media_parsing_movie.go`):

1. Width normalization
2. Strip scene tags (preserves edition qualifiers like "Extended", "Director's Cut")
3. Dots → spaces
4. Strip edition suffixes: "Edition", "Version", "Cut", "Release"
5. Strip brackets (years extracted as tags)
6. Split titles and strip articles
7. Strip trailing articles

**Music** (`pkg/database/slugs/media_parsing_music.go`):

1. Width normalization
2. Strip scene tags: format (FLAC, MP3), quality (V0, 320), source (CD, Vinyl, WEB)
3. Separators → spaces (dots, underscores, dashes)
4. Strip brackets (years extracted as tags)
5. Strip disc numbers: CD1, Disc 1 → removed
6. Strip leading article: "The Beatles" → "Beatles"
7. Strip trailing articles
8. Collapse whitespace

**Image/Audio/Video/Application**: Pass through to Phase 2 only.

### Phase 2: Universal normalization

`pkg/database/slugs/slugify.go` → `normalizeInternal()`

Applied after media-specific parsing:

1. Width normalization — fullwidth → halfwidth (ASCII), halfwidth → fullwidth (CJK)
2. Punctuation normalization — curly quotes, fancy dashes → ASCII equivalents
3. Unicode normalization — remove symbols (™©®), strip diacritics (Pokémon → Pokemon)
4. Symbols and separators — `&` → `and`, separators → spaces
5. Periods → spaces (safe after abbreviation expansion)
6. Lowercase

Final: strip all non-alphanumeric characters (script-aware for non-Latin text).

### Example

```
Input:  "The Legend of Zelda: Ocarina of Time (USA) [!]"

Phase 1 (Game):
  Split & strip articles → "Legend of Zelda Ocarina of Time (USA) [!]"
  Strip brackets         → "Legend of Zelda Ocarina of Time"

Phase 2:
  Lowercase              → "legend of zelda ocarina of time"

Final:
  Strip non-alphanumeric → "legendofzeldaocarinaoftime"
```

### Multi-script support

`pkg/database/slugs/scripts.go`

Non-Latin scripts (CJK, Cyrillic, Arabic, etc.) are kept intact while Latin text gets full normalization. Mixed titles just concatenate both parts:

```
"Street Fighter ストリートファイター" → "streetfighterストリートファイター"
```

You can search by either portion. Script-specific rules handle diacritics differently per writing system (Arabic vowel marks get stripped, Indic vowel marks are preserved, etc.).

---

## Filename parser

Filenames are parsed during indexing to pull out clean titles and metadata tags.

`pkg/database/mediascanner/indexing_pipeline.go` → `GetPathFragments()`
`pkg/database/tags/filename_parser.go`

### Title extraction

`tags.ParseTitleFromFilename(filename, stripLeadingNumbers)` — 8-step pipeline:

1. **Remove extension** — strips `.zip`, `.nes`, `.mkv` etc. (2-4 char extensions only)
2. **Strip release group** — `-YIFY`, `-SPARKS` at end (uppercase, 3+ chars)
3. **Normalize separators** — if filename has no spaces and 2+ dots/underscores/dashes, convert all to spaces. Detects scene releases and ROM naming conventions.
4. **Strip scene artifacts** — only after a year if found. Removes resolution, source, codec, audio, HDR, status tags. Protects titles like "Cam (2018)" where "Cam" is the actual title.
5. **Strip episode markers** — `S01E02`, `s1e2` removed from display title (kept for tag extraction)
6. **Strip leading numbers** — optional, only when directory context shows list-style numbering (`01 - Game Name` → `Game Name`)
7. **Remove bracket content** — all `()`, `[]`, `{}`, `<>` and their contents
8. **Normalize whitespace** — collapse multiple spaces, trim

Examples:

```
"Super Mario Bros. III (USA) (Rev A) [!].nes"
  → remove ext → strip brackets → "Super Mario Bros. III"

"The.Dark.Knight.2008.1080p.BluRay.x264-YIFY.mkv"
  → remove ext → strip group → normalize seps → strip scene → "The Dark Knight 2008"

"Breaking.Bad.S01E02.Gray.Matter.720p.mkv"
  → normalize seps → strip scene → strip episode → "Breaking Bad Gray Matter"
```

### Tag extraction

`tags.ParseFilenameToCanonicalTags(filename)` — 4-step pipeline:

**Step 1: Special patterns** — extracts patterns outside brackets: disc numbers (`Disc 1 of 2` → `disc:1`), revisions (`Rev A` → `rev:a`), versions (`v1.2` → `rev:1-2`), years (`1997` → `year:1997`), episodes (`S01E02` → `season:1`, `episode:2`), issues, tracks, translations.

**Step 2: Bracket content** — state machine parser extracts `()`, `{}`, `<>` tags separately from `[]` tags.

**Step 3: Parentheses tags** — context-aware disambiguation:
- First paren tag → region if it matches the known region list (USA, Europe, Japan, etc.)
- Subsequent tags → language, version, dev status
- Multi-value: `(En,Fr,De)` → `lang:en`, `lang:fr`, `lang:de`

**Step 4: Square bracket tags** — always dump info or modifications:
- `[!]` → `dump:verified`, `[b]` → `dump:bad`, `[h]` → `hack:yes`, `[T+En]` → `translation:en`

Example:

```
"Super Mario Bros. 3 (USA) (Rev A) [!].nes"
  → [rev:a, region:us, dump:verified]

"Zelda (Europe) (En,Fr,De,Es,It).gba"
  → [region:eu, lang:en, lang:fr, lang:de, lang:es, lang:it]
```

---

## Matching strategies

Resolution tries strategies in order, each more lenient than the last, until something matches.

`pkg/zapscript/titles/resolve.go`

```
1. Cache lookup
   ↓ miss
2. Exact slug match (with tag filters)
   ↓ no results or low confidence
3. Exact slug match (without tags)
   ↓ no results
4. Secondary title match
   ↓ no results
5. Fuzzy matching
   ↓ no results
6. Main title only
   ↓ no results
7. Progressive trim
```

**Cache** — keyed by SystemID + Slug + Tags. Returns immediately on hit.

**Exact match with tags** — direct slug lookup with tags as filters. Early exit if confidence >= 0.95.

**Exact match without tags** — same lookup, tags ignored. Tags become soft preferences during result selection.

**Secondary title** — handles mismatched subtitles. Bidirectional: "Zelda: Ocarina of Time" matches "Ocarina of Time" and vice versa.

**Fuzzy matching** — pre-filter (±2 chars length diff), then: token signature (word-order-independent), Jaro-Winkler (typo tolerance, 0.85+ similarity), Damerau-Levenshtein for tie-breaking top candidates.

**Main title only** — uses just the part before the first delimiter. "Zelda: Ocarina" matches "Zelda" and "Zelda: Ocarina of Time".

**Progressive trim** — removes words from the end of the query, max 3 iterations. "Legend of Zelda Link's Awakening DX" tries progressively shorter slugs.

### Tags in queries

Tags come from three sources (highest priority wins):

1. **Advanced args**: `NES/Zelda?tags=region:us,-unfinished:beta` — explicit, overrides everything
2. **Inline canonical**: `NES/Zelda (+region:us) (-unfinished:beta)` — `+` for AND, `-` for NOT
3. **Filename-style**: `NES/Zelda (USA) (1986)` — auto-extracted, lowest priority

### Result selection

When multiple results match, they get filtered and scored:

**Confidence** is a base score from the strategy (0.85–1.0), adjusted by tag matching:
- >= 0.95: launch immediately
- >= 0.70: launch with info
- >= 0.60: launch with warning
- < 0.60: error

**Filtering** (in order):
1. User-specified tag filters
2. Exclude variants: unfinished (demo, beta, proto), unlicensed (hack, translation, bootleg), bad dumps
3. Exclude re-releases
4. Preferred regions from user config
5. Preferred languages from user config
6. File type priority based on launcher extension order
7. Quality tie-breaking: numeric suffix penalty, path depth, character density, filename length
