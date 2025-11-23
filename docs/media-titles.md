# Title Normalization and Matching System

Zaparoo Core's title normalization and matching system enables users to launch games using natural language titles (e.g., "The Legend of Zelda: Ocarina of Time") that are fuzzy-matched against indexed ROM filenames. This document provides a high-level overview of how the system works.

## Overview

The system enables game lookups using **natural language titles** rather than exact filenames or unique identifiers. Users can write titles in various forms (with or without articles, with Roman numerals or digits, with typos, etc.) and the system will find matching games through progressive normalization and intelligent fallback strategies.

**Key Concept:** Slugs are **not IDs**. They are an intermediary normalization step that enables fuzzy matching between user queries and indexed filenames. The system normalizes both sides:

- **User input** → normalize → slug → match against database
- **Filenames** → normalize → slug → store in database

The original input title and filename are preserved for additional context during resolution.

## Why This Approach?

The system works around several constraints:

- **No hashing**: Too slow on low-resource devices (MiSTer FPGA, older Raspberry Pis)
- **Offline-first**: No dependency on online services or internet access
- **Cross-device portability**: Tokens work across different devices despite different file naming schemes

**Advantages:**

- Natural language names on NFC/QR tokens work universally
- Third-party apps don't need special integration - just write the game name
- System can be improved over time without breaking compatibility
- Makes local media search much more useful

**Limitations:**

- No cross-language support (and currently prioritizes English heuristics)
- Conflicts can occur (mitigated by system namespacing and tags)
- Best-effort normalization rather than perfect accuracy

## How It Works

### 1. Indexing

When scanning media, Zaparoo:

1. Cleans path and extracts filename (strips file extension and path)
2. Parses filename to extract clean display title (10-step pipeline - see Filename Parser section)
3. Extracts tags from filename using bracket disambiguation (4-step pipeline - see Filename Parser section)
4. Determines media type from system (Game, TVShow, Movie, Music, etc.)
5. Runs media-type-aware slugification on title (two-phase normalization - see Slug Normalization section)
6. Stores path, title, slug, tags, and metadata in database for fast searching

### 2. Resolution (Query-Based Launching)

When launching by title query (e.g., `launch.title("NES/Super Mario Bros")`), Zaparoo:

1. Parses `SystemID/GameName` format
2. Validates format and looks up system in database
3. Extracts tags from three sources in user query (advanced args, canonical tags, filename-style metadata)
4. Merges tags with priority hierarchy
5. Slugifies game name using media-type-aware normalization (same as indexing)
6. Checks cache for previous resolutions
7. Tries multiple matching strategies in order until finding a match (see Matching Strategies section)
8. Applies confidence scoring and filtering to select best result
9. Caches successful resolution for future queries

**Note:** The user's query is processed separately from filenames. Users can include tags in their queries using filename-style `(USA)`, canonical `(+region:us)`, or advanced args `?tags=region:us` formats.

---

## Slug Normalization

Slug normalization uses a **two-phase architecture** that converts titles into a canonical form. Both indexing and resolution use identical normalization.

**Implementation:** `pkg/database/slugs/slugify.go` → `Slugify(mediaType MediaType, input string)`

### Two-Phase Architecture

#### Phase 1: Media-Specific Parsing

Applies format-specific normalization based on media type **before** universal normalization.

**For Games** (`ParseGame`):
1. Width normalization (fullwidth separators → ASCII for detection)
2. Split titles and strip articles: "The Zelda: Link's Awakening" → "Zelda Link's Awakening"
3. Strip trailing articles: "Legend, The" → "Legend"
4. Strip metadata brackets: `(USA)`, `[!]`, `{Europe}` → removed
5. Strip edition/version suffixes: "Edition", "Version", "v1.0" → removed
6. Normalize symbols/separators (preserve commas for trailing articles)
7. Expand abbreviations: "Bros" → "brothers", "vs" → "versus", "Dr" → "doctor"
8. Expand number words: "one" → "1", "two" → "2" (1-20)
9. Normalize ordinals: "2nd" → "2", "3rd" → "3"
10. Convert roman numerals: "VII" → "7", "II" → "2" (preserves "X" in "Mega Man X")

**For TV Shows** (`ParseTVShow`):
- Normalizes episode formats: `S01E02` / `1x02` / `1-02` → `s01e02`
- Preserves episode information for matching

**For Movies/Music** (`ParseMovie`, `ParseMusic`):
- Currently pass-through (future enhancement)

#### Phase 2: Universal Normalization (`normalizeInternal`)

Applied after media-specific parsing:

1. **Width Normalization** - Fullwidth → Halfwidth (ASCII), Halfwidth → Fullwidth (CJK)
2. **Punctuation Normalization** - Curly quotes, fancy dashes → standard ASCII
3. **Unicode Normalization** - Remove symbols (™©®), remove diacritics (Pokémon → Pokemon), script-aware processing
4. **Symbols & Separators** - `&` → `and`, separators → spaces: "Sonic & Knuckles" → "Sonic and Knuckles"
5. **Period Conversion** - All periods → spaces (safe after abbreviations expanded)
6. **Lowercasing** - Convert to lowercase

**Final Stage** (in `Slugify`/`SlugifyWithTokens`):

7. **Character Filtering** - Remove non-alphanumeric, multi-script aware

### Complete Example (Games)

```
Input:  "The Legend of Zelda: Ocarina of Time (USA) [!]"

Phase 1 (ParseGame):
  Step 1-2: Split & strip articles → "Zelda Ocarina of Time (USA) [!]"
  Step 4:   Strip brackets → "Zelda Ocarina of Time"
  Step 10:  Roman numerals (none) → "Zelda Ocarina of Time"

Phase 2 (normalizeInternal):
  Step 6:   Lowercase → "zelda ocarina of time"

Final:
  Step 7:   Filter → "legendofzeldaocarinaoftime"

Output: "legendofzeldaocarinaoftime"
```

### Multi-Script Support

The system preserves non-Latin scripts (CJK, Cyrillic, Arabic, etc.) while aggressively normalizing Latin text:

- **Latin titles**: Full normalization, ASCII output
- **CJK titles**: Preserved characters, essential marks kept
- **Mixed titles**: Both portions concatenated, searchable by either part

**Example:**

```
Input:  "Street Fighter ストリートファイター"
Output: "streetfighterストリートファイター"
```

This makes the title searchable by either the Latin or CJK portion.

---

## Filename Parser (Indexing)

During media indexing, Zaparoo parses filenames to extract clean titles and metadata tags. This happens when scanning ROM directories, media libraries, or individual files.

**Implementation:** `pkg/database/mediascanner/indexing_pipeline.go` → `GetPathFragments()`, `pkg/database/tags/filename_parser.go`

### Indexing Pipeline

When a file is indexed, the system:

1. **Cleans path** - Normalizes to forward slashes, handles URIs
2. **Extracts filename** - Strips file extension and path
3. **Parses title** - Extracts clean display title from filename
4. **Extracts tags** - Parses metadata from brackets/parentheses
5. **Slugifies title** - Creates normalized slug for matching
6. **Stores in database** - Saves path, title, slug, and tags

### Title Extraction from Filename

**Function:** `tags.ParseTitleFromFilename(filename, stripLeadingNumbers)`

Extracts a clean, human-readable display title from a filename by removing metadata and normalizing artifacts.

#### The 10-Step Pipeline

1. **Remove File Extension**
   - Strips `.zip`, `.nes`, `.mkv`, etc.
   - Only removes if 2-4 characters after last dot
   - Example: `"game.nes"` → `"game"`

2. **Strip Release Group**
   - Removes scene release group suffix: `-GROUP` at end
   - Must be uppercase, 3+ characters
   - Example: `"Movie-YIFY"` → `"Movie"`
   - **Done early** before hyphen → space conversion

3. **Normalize Filename Separators** (contextual)
   - **Trigger:** Filename has no spaces AND 2+ separators (dots, underscores, or dashes)
   - **Action:** Convert all `.`, `_`, `-` → spaces
   - Examples:
     - `"The.Dark.Knight.2008.mkv"` → `"The Dark Knight 2008"`
     - `"super_mario_bros.sfc"` → `"super mario bros"`
     - `"mega-man-x.nes"` → `"mega man x"`
   - **Heuristic:** Detects scene releases and ROM naming conventions

4. **Strip Scene Release Artifacts** (contextual)
   - **Trigger:** Only strips from text AFTER a year (if found)
   - **Protects titles:** `"Cam (2018)"` keeps "Cam" (it's the title, not a scene tag)
   - **Removed patterns:**
     - Resolution: `720p`, `1080p`, `2160p`, `4K`, `UHD`
     - Source: `BluRay`, `WEB-DL`, `WEBRip`, `HDTV`, `DVDRip`, `CAM`, `TS`
     - Video codec: `x264`, `x265`, `h264`, `HEVC`, `AVC`
     - Audio codec: `AAC`, `AC3`, `DTS`, `DD5.1`, `TrueHD`, `Atmos`
     - HDR: `HDR`, `HDR10`, `Dolby Vision`
     - Status: `PROPER`, `REPACK`, `INTERNAL`, `LIMITED`
   - Example: `"The Dark Knight 2008 1080p BluRay x264"` → `"The Dark Knight 2008"`

5. **Strip Episode Markers**
   - Removes TV show episode patterns: `S01E02`, `s1e2`
   - Keeps them for tag extraction but removes from display title
   - Example: `"Breaking Bad S01E02 Title"` → `"Breaking Bad Title"`

6. **Strip Leading Numbers** (optional)
   - Only when `stripLeadingNumbers=true` (detected from directory context)
   - Removes list prefixes: `"1. "`, `"01 - "`, `"05-"`
   - Example: `"01 - Game Name"` → `"Game Name"`
   - **Contextual:** Only enabled when directory shows list-style numbering

7. **Extract Year from Brackets**
   - Finds year in format: `(1997)`, `(2008)`
   - Preserves for re-appending after bracket removal
   - Range: 1970-2099

8. **Remove All Bracket Content**
   - **Function:** `slugs.StripMetadataBrackets()`
   - Removes: `()`, `[]`, `{}`, `<>`
   - Handles nested brackets
   - Example: `"Game (USA) [!] {Europe}"` → `"Game"`

9. **Re-append Year**
   - If year was extracted and not still present, append it
   - Example: `"Movie (2008) (Blu-ray)"` → `"Movie 2008"`
   - Preserves useful year information in display title

10. **Normalize Whitespace**
    - Collapses multiple spaces to single space
    - Trims leading/trailing spaces
    - Final cleanup after all transformations

### Title Extraction Examples

**ROM filename:**
```
Input:  "Super Mario Bros. III (USA) (Rev A) [!].nes"
Step 1: Remove extension → "Super Mario Bros. III (USA) (Rev A) [!]"
Step 7: Extract year → (none)
Step 8: Remove brackets → "Super Mario Bros. III"
Step 10: Normalize → "Super Mario Bros. III"
Output: "Super Mario Bros. III"
```

**Scene release:**
```
Input:  "The.Dark.Knight.2008.1080p.BluRay.x264-YIFY.mkv"
Step 1: Remove extension → "The.Dark.Knight.2008.1080p.BluRay.x264-YIFY"
Step 2: Strip release group → "The.Dark.Knight.2008.1080p.BluRay.x264"
Step 3: Normalize separators → "The Dark Knight 2008 1080p BluRay x264"
Step 4: Strip scene artifacts (after year) → "The Dark Knight 2008"
Output: "The Dark Knight 2008"
```

**TV show:**
```
Input:  "Breaking.Bad.S01E02.Gray.Matter.720p.mkv"
Step 3: Normalize separators → "Breaking Bad S01E02 Gray Matter 720p"
Step 4: Strip scene artifacts → "Breaking Bad S01E02 Gray Matter"
Step 5: Strip episode marker → "Breaking Bad Gray Matter"
Output: "Breaking Bad Gray Matter"
```

### Tag Extraction from Filename

**Function:** `tags.ParseFilenameToCanonicalTags(filename)`

Extracts metadata tags from filenames following No-Intro and TOSEC conventions.

#### The 4-Step Pipeline

**Step 1: Extract Special Patterns**
- **Function:** `extractSpecialPatterns()`
- Finds patterns that appear outside brackets:
  - **Translations**: `T+En`, `T-Fr v1.0` → `translation:en`, `translation-:fr`
  - **Disc numbers**: `(Disc 1 of 2)` → `disc:1`, `discof:2`
  - **Revisions**: `(Rev A)`, `(Rev 1)` → `rev:a`, `rev:1`
  - **Versions**: `(v1.2)`, `v3.0` → `version:1.2`, `version:3.0`
  - **Years**: `(1997)` → `year:1997`
  - **Episodes**: `S01E02`, `1x05` → `season:01`, `episode:02`
  - **Issues**: `#12`, `Issue 5` → `issue:12`
  - **Tracks**: `01 -`, `Track 03` → `track:01`
  - **Volumes**: `(Vol. 2)` → `volume:2`
- Removes matched patterns from string for cleaner bracket extraction

**Step 2: Extract Bracket Content**
- **Function:** `extractTags()`
- State machine parser for brackets:
  - `()`, `{}`, `<>` → parentheses tags (region, language, dev status)
  - `[]` → square bracket tags (dump info, hacks)
- Returns two separate lists for disambiguation

**Step 3: Process Parentheses Tags**
- **Function:** `disambiguateTag()` with `BracketTypeParen`
- Context-aware parsing with positional rules:
  - **First paren tag** → usually region (if matches known region)
  - **Subsequent tags** → language, version, dev status, etc.
- Handles multi-value tags: `(En,Fr,De)` → `lang:en`, `lang:fr`, `lang:de`
- **Tag types recognized:**
  - Regions: `USA`, `Europe`, `Japan`, `World`, etc.
  - Languages: `En`, `Fr`, `De`, `Ja`, etc. (2-3 letter codes)
  - Dev status: `Beta`, `Proto`, `Alpha`, `Demo`
  - Versions: `v1.0`, `Rev A`, `Alt`
  - Years: `1997`, `2008`

**Step 4: Process Square Bracket Tags**
- **Function:** `disambiguateTag()` with `BracketTypeSquare`
- Always dump-related or modification info:
  - **Dump status**: `[!]` → `dump:verified`, `[b]` → `dump:bad`
  - **Hacks**: `[h]`, `[h1]` → `hack:yes`, `hack:1`
  - **Translations**: `[T+En]` → `translation:en`
  - **Trainer**: `[t]`, `[t1]` → `trainer:yes`, `trainer:1`
  - **Fixes**: `[f]` → `fix:yes`
  - **Overdumps**: `[o]` → `overdump:yes`

### Tag Extraction Examples

**ROM filename:**
```
Input:  "Super Mario Bros. 3 (USA) (Rev A) [!].nes"

Step 1: Extract special patterns
  → Rev: rev:a (from "(Rev A)")
  → Remaining: "Super Mario Bros. 3 (USA) [!].nes"

Step 2: Extract brackets
  → Paren tags: ["USA"]
  → Square tags: ["!"]

Step 3: Process paren tags
  → "USA" (position 0, first tag) → region:us

Step 4: Process square tags
  → "!" → dump:verified

Output: [rev:a, region:us, dump:verified]
```

**Multi-language ROM:**
```
Input:  "Zelda (Europe) (En,Fr,De,Es,It).gba"

Step 2: Extract brackets
  → Paren tags: ["Europe", "En,Fr,De,Es,It"]

Step 3: Process paren tags (position 0)
  → "Europe" → region:eu

Step 3: Process paren tags (position 1)
  → "En,Fr,De,Es,It" (multi-value) → lang:en, lang:fr, lang:de, lang:es, lang:it

Output: [region:eu, lang:en, lang:fr, lang:de, lang:es, lang:it]
```

**Unfinished ROM:**
```
Input:  "Star Fox 2 (Beta) (1995).sfc"

Step 1: Extract special patterns
  → Year: year:1995
  → Remaining: "Star Fox 2 (Beta).sfc"

Step 3: Process paren tags
  → "Beta" → unfinished:beta

Output: [year:1995, unfinished:beta]
```

**Scene release:**
```
Input:  "The.Dark.Knight.2008.1080p.BluRay.x264-YIFY.mkv"

Step 1: Extract special patterns
  → Year: year:2008
  → Remaining: (no brackets to extract)

Output: [year:2008]
```

### Disambiguation Rules

The system uses **positional** and **bracket-type** rules for disambiguation:

**Positional Rules** (parentheses):
1. **First tag** → region (if matches known region list)
2. **Subsequent tags** → language, version, dev status
3. **Context-aware** → checks previously processed tags

**Bracket Type Rules:**
- **Parentheses/Braces/Angles** → metadata (region, language, version, dev status)
- **Square brackets** → always dump info or modifications (hacks, trainers, fixes)

**Special Handling:**
- **Multi-value tags**: `(En,Fr,De)` → creates multiple lang tags
- **Composite tags**: `(En,Fr)` in Europe ROM → both languages extracted
- **Inferred tags**: "Edition" in plain text → marked as `TagSourceInferred`, skipped for filtering

---

## Matching Strategies

Resolution tries strategies **in order** until finding results. Each strategy becomes progressively more lenient.

**Implementation:** `pkg/zapscript/titles.go` → `cmdTitle()`

### Strategy Flow

```
1. Check cache
   ↓ (miss)
2. Exact match (with tags)
   ↓ (no results OR low confidence)
3. Exact match (without tags)
   ↓ (no results)
4. Secondary title match
   ↓ (no results)
5. Advanced fuzzy matching
   ├─ Token signature (word-order independent)
   ├─ Jaro-Winkler (typo tolerance)
   └─ Damerau-Levenshtein tie-breaking
   ↓ (no results)
6. Main title only
   ↓ (no results)
7. Progressive trim (last resort)
```

### Strategy Details

#### 1. Cache Lookup

- Fast path: checks previous resolutions
- Keyed by: SystemID + Slug + Tags

#### 2. Exact Match (with tags)

- Direct slug lookup
- Tags applied as filters
- **Early exit** if confidence ≥ 0.95 (high confidence)

#### 3. Exact Match (without tags)

- Same slug lookup, tags ignored
- Tags become soft preferences during result selection

#### 4. Secondary Title Match

Handles mismatched secondary titles (bidirectional):

- Query has secondary, DB doesn't: "Zelda: Ocarina" → matches "Ocarina of Time"
- Query simple, DB has secondary: "Ocarina" → matches "Zelda: Ocarina of Time"

#### 5. Advanced Fuzzy Matching

Uses a pre-filter (±3 chars, ±1 word) then tries three algorithms:

- **Token signature**: Order-independent word matching
- **Jaro-Winkler**: Typo tolerance, prefix-weighted (0.85+ similarity)
- **Damerau-Levenshtein**: Tie-breaking for top 5 candidates

#### 6. Main Title Only

Searches using just the main title portion (bidirectional):

- Query has secondary, DB doesn't: "Zelda: Ocarina" → matches "Zelda"
- Query simple, DB has secondary: "Zelda" → matches "Zelda: Ocarina of Time"

#### 7. Progressive Trim

Progressively removes words from the end of the original query (max 3 iterations):

- "Legend of Zelda Link's Awakening DX" → tries "...Awakening", "...Link's", "...Zelda" (then slugifies each)

---

## Result Selection

When multiple results match, the system applies filtering and scoring:

### Confidence Scoring

Base confidence from strategy (0.85-1.0) is adjusted by tag matching:

- **High confidence (≥0.95)**: Launch immediately
- **Acceptable (≥0.70)**: Launch with info message
- **Minimum (≥0.60)**: Launch with warning
- **Below 0.60**: Error out

### Filtering Priority

1. **User-specified tags** - Filter to exact matches (if provided)
2. **Exclude variants** - Remove demos, betas, hacks, translations, bad dumps
3. **Exclude re-releases** - Remove reboxed editions, re-releases
4. **Preferred regions** - Match user's region config
5. **Preferred languages** - Match user's language config
6. **File type priority** - Prefer file types based on launcher extension order (earlier = better)
7. **Quality-based tie-breaking** - Select best file using:
   - Numeric suffix penalty (avoids duplicates like "game (1).zip")
   - Path depth (prefers files in organized folders over deep backups)
   - Character density (cleaner filenames preferred)
   - Filename length (shorter is simpler)

---

## Tag System

Tags provide additional filtering and disambiguation during both indexing and resolution.

### Tag Extraction (During Indexing)

Tags are automatically extracted from filenames during media scanning using the **Filename Parser** (see section above for complete details).

**Common tag types:**
- **Regions**: `(USA)`, `(Europe)`, `(Japan)` → `region:us`, `region:eu`, `region:jp`
- **Languages**: `(En)`, `(Fr,De)` → `lang:en`, `lang:fr`, `lang:de`
- **Years**: `(1997)` → `year:1997`
- **Dump info**: `[!]` → `dump:verified`, `[b]` → `dump:bad`
- **Development**: `(Beta)`, `(Proto)` → `unfinished:beta`, `unfinished:proto`
- **Revisions**: `(Rev A)` → `rev:a`
- **Episodes**: `S01E02`, `1x05` → `season:01`, `episode:02`
- **Discs**: `(Disc 1 of 2)` → `disc:1`, `discof:2`

**See "Filename Parser" section** for the complete 4-step tag extraction pipeline with disambiguation rules.

### Tag Usage in Queries

Tags can be specified in three ways (with priority hierarchy):

1. **Advanced args** (highest priority): `NES/Zelda?tags=region:us,-unfinished:beta`
   - Explicit user requirements via `?tags=` parameter
   - Format: `tag:value` or `-tag:value` (NOT operator)
   - Overrides all other tag sources

2. **Inline canonical tags** (medium priority): `NES/Zelda (+region:us) (-unfinished:beta)`
   - Explicit tag filters with operators in parentheses
   - Supports: `(+tag:value)` AND, `(-tag:value)` NOT, `(tag:value)` AND (default)
   - Overrides filename-style tags

3. **Filename-style** (lowest priority): `NES/Zelda (USA) (1986)` (auto-extracted)
   - Automatically parsed from filename metadata in parentheses
   - Always treated as AND filters
   - Used only when no conflicting higher-priority tags exist

---

## Implementation Notes

### Key Files

**Indexing Pipeline:**
- **Main indexing orchestrator**: `pkg/database/mediascanner/indexing_pipeline.go`
- **Filename title extraction**: `pkg/database/tags/filename_parser.go` → `ParseTitleFromFilename()`
- **Filename tag extraction**: `pkg/database/tags/filename_parser.go` → `ParseFilenameToCanonicalTags()`

**Slug Normalization:**
- **Core slugification**: `pkg/database/slugs/slugify.go`
- **Media parsing (dispatcher)**: `pkg/database/slugs/media_parsing.go`
- **Game parsing**: `pkg/database/slugs/media_parsing_game.go`
- **TV show parsing**: `pkg/database/slugs/media_parsing_tv.go`
- **Movie parsing**: `pkg/database/slugs/media_parsing_movie.go`
- **Music parsing**: `pkg/database/slugs/media_parsing_music.go`
- **Normalization helpers**: `pkg/database/slugs/normalization.go`
- **Script detection**: `pkg/database/slugs/scripts.go`

**Query Resolution (Title-based launching):**
- **Query parser & resolution**: `pkg/zapscript/titles.go` → `cmdTitle()`
- **Matching strategies**: `pkg/zapscript/titles/strategies.go`
- **Result selection**: `pkg/zapscript/titles/selection.go`
- **Fuzzy matching**: `pkg/database/matcher/fuzzy.go`
- **Query tag extraction & merging**: `pkg/zapscript/titles/tags.go`
