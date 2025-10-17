# Title Normalization and Matching System

Zaparoo Core's title normalization and matching system enables users to launch games by providing natural language titles (e.g., "The Legend of Zelda: Ocarina of Time") that are fuzzy-matched against potentially messy ROM filenames. This document describes the complete architecture of this system.

## Overview

The system enables game lookups using **natural language titles** rather than exact filenames or unique identifiers. Users can write titles in various forms (with or without articles, with Roman numerals or digits, with typos, etc.) and the system will find matching games through progressive normalization and intelligent fallback strategies.

**Key Concept:** Slugs are **not IDs**. They are an intermediary normalization step that enables fuzzy matching between user queries and indexed filenames. The system normalizes both sides of the equation:

- **User input** → normalize → slug → match against database
- **Filenames** → normalize → slug → store in database

The system is coupled with a tag extraction and storage system. Metadata extracted from titles/filenames during this process is stored as tags in the database, and tags can be used as additional filters for media title lookups where they might be ambiguous otherwise.

## Pros and Cons

The system is a pragmatic best effort approach to work around some major constraints:

- Hashing files is too slow on low resource devices like MiSTer FPGA and older Raspberry Pis.
- We don't want to tie the project to any specific online services for game identification or canonical IDs.
- Hashing files is too specific without also involving an online service or large local database.
- We can't assume that users will have internet access when indexing or launching games.

The approach has several major advantages:

- By using a natural language system name + game title written to a Zaparoo token, we can now have a high confidence that the token will match the intended game, even if the user input is not an exact filename. This means Zaparoo tokens can now work seamlessly between devices, no matter the filename scheme or folder structure.
- Third party apps and services don't need to be aware of this process. For example, an app could query a title from an online service like IGDB and write the name, as is, to an NFC card or QR code, and it would work without any special integration. The value could even be written manually by an end user.
- Because the process is internal to Zaparoo Core, we can improve it over time without breaking compatibility. New normalization rules can be added, and the system can be tuned based on real world usage.
- The system makes searching for media locally much more useful.
- It opens up a path for scraping metadata from existing online services without needing to hash files.

The approach also has some disadvantages:

- By design, the system in isolation can never support matching games between languages and is limited in matching regional differences. It can potentially be improved in the future with optional additional metadata sources.
- Many of the methods used to normalize titles prioritise performance over accuracy. For example, the system does not use a full natural language processing library or dictionary to understand the meaning of words. Instead, it uses a series of heuristic rules and regex patterns to approximate this behavior.
- In its current state, the system heavily prioritizes English titles for edge case heuristics. This could be improved in the future with contributions from native speakers of other languages.
- Conflicts of normalized titles can and do exist. This is mostly mitigated by using the system name as a namespace, but there are still cases where multiple games in the same system can normalize to the same slug. Tools are provided (tags) to help disambiguate these cases.

Overall, the system is not perfect, but it's a huge improvement over the current state without requiring any external dependencies and only marginally increasing the time taken to index and match games.

## Architecture

The system consists of two primary workflows supported by shared normalization libraries:

### 1. Indexing Workflow

Scans the filesystem and populates the media database with normalized, searchable entries.

**Input:** File paths from platform-specific media directories
**Output:** Database entries (Systems, MediaTitles, Media, Tags)
**Entry Point:** `pkg/database/mediascanner/indexing_pipeline.go`

### 2. Resolution Workflow

Matches user-provided game titles against the indexed database using progressive fallback strategies.

**Input:** `SystemID/GameName` format from user (e.g., `nes/Super Mario Bros`)
**Output:** Best matching media entry to launch
**Entry Point:** `pkg/zapscript/titles.go` → `cmdTitle()`

### 3. Shared Libraries

Both workflows use common normalization code:

- **Slug Normalizer** (`pkg/database/slugs/slugify.go`)

  - Core 14-stage normalization pipeline (Stages 1-13 + final slugification)
  - Script detection and script-specific processing
  - Multi-script support (Latin, CJK, Cyrillic, Greek, Arabic, Hebrew, Indic, Thai, Burmese, Khmer, Lao, Amharic)

- **Matcher Utilities** (`pkg/database/matcher/`)

  - Token-based similarity scoring
  - Fuzzy matching algorithms (Jaro-Winkler)
  - Prefix matching with edition-aware ranking
  - Progressive trim candidate generation

- **Tag Parser** (`pkg/database/tags/filename_parser.go`)
  - Extracts metadata from No-Intro/TOSEC-style filenames
  - Converts to canonical tag format

---

## Normalization Pipeline

The normalization pipeline is the heart of the system. It converts any game title into a canonical, normalized form optimized for fuzzy matching. Both indexing and resolution workflows use identical normalization.

**Location:** `pkg/database/slugs/slugify.go` → `SlugifyString()`

**Function Signature:**

```go
func SlugifyString(input string) string
```

**Guarantees:**

- **Deterministic:** Same input always produces same output
- **Idempotent:** `SlugifyString(SlugifyString(x)) == SlugifyString(x)`
- **Multi-script aware:** Preserves non-Latin scripts while normalizing Latin text

### 14-Stage Processing Pipeline

The pipeline executes stages in a specific order to ensure correctness. Stages 1-13 are performed by `normalizeInternal()`, followed by Stage 14 (final slugification) in `SlugifyString()`. Stages are numbered by execution order, not logical grouping.

#### Stage 1: Width Normalization

**Purpose:** Convert fullwidth and halfwidth characters to normalized forms.

**Process:**

- **Fullwidth ASCII** → **Halfwidth** (enables Latin text processing)
- **Halfwidth CJK** → **Fullwidth** (ensures consistent display and matching)

**Examples:**

- `"ＡＢＣＤＥＦ"` → `"ABCDEF"`
- `"１２３"` → `"123"`
- `"ｳｴｯｼﾞ"` → `"ウエッジ"` (halfwidth katakana → fullwidth)
- `"Super Ｍario １２３"` → `"Super Mario 123"`

**Why first?** Ensures all subsequent stages work on consistent character widths. Fullwidth ASCII becomes regular ASCII for regex matching, while halfwidth katakana becomes fullwidth for proper CJK handling.

#### Stage 2: Punctuation Normalization

**Purpose:** Normalize Unicode punctuation variants to ASCII equivalents for consistent processing.

**Normalized characters:**

- **Curly quotes:** `' ' " "` → `' "`
- **Prime marks:** `′ ″` → `' "`
- **Grave/acute:** `` ` ´`` → `'`
- **Dashes:** `– — ― −` → `-`
- **Ellipsis:** `…` → `...`

**Examples:**

- `"Link's Awakening"` → `"Link's Awakening"` (curly apostrophe → straight)
- `"Super–Bros."` → `"Super-Bros."` (en dash → hyphen)
- `"Rock 'n' Roll"` → `"Rock 'n' Roll"` (enables later conjunction normalization)

**Why before Stage 3?** These specific cases are not handled by Unicode normalization forms, so they must be explicitly converted first.

#### Stage 3: Unicode Normalization

**Purpose:** Remove symbols and diacritics, apply script-specific Unicode normalization.

**Process:**

1. **Symbol Removal** - Removes Unicode symbols: `™ © ® ℠ $ € ¥`
2. **Script Detection** - Determines if text contains CJK, Cyrillic, Greek, etc.
3. **Script-Specific Normalization:**
   - **Latin text:** NFKC + NFD + Mark Removal + NFC (removes diacritics)
   - **CJK text:** NFC only (preserves essential marks like dakuten/handakuten)
   - **Other scripts:** NFC only (preserves script-specific marks)

**Examples:**

- Symbols: `"Sonic™"` → `"Sonic"`, `"Game®"` → `"Game"`
- Diacritics (Latin): `"Pokémon"` → `"Pokemon"`, `"Café"` → `"Cafe"`
- Ligatures: `"ﬁnal"` → `"final"`
- CJK preserved: `"ドラゴンクエスト"` → `"ドラゴンクエスト"`

**Why remove symbols first?** NFKC would convert `™→TM`, `℠→SM`, which would incorrectly become part of the slug. By removing symbols first, they're completely stripped.

#### Stage 4: Metadata Stripping

**Purpose:** Remove all bracket types containing metadata (region codes, dump info, tags).

**Bracket Types Supported:**

- Parentheses: `(...)`
- Square brackets: `[...]`
- Braces: `{...}`
- Angle brackets: `<...>`

**Examples:**

- `"Game (USA) [!]"` → `"Game"`
- `"Title {Europe} <Beta>"` → `"Title"`
- `"Sonic ((nested)) [test]"` → `"Sonic"`

**Process:** State machine tracks nesting depth for each bracket type, only writes characters when outside all brackets.

#### Stage 5: Secondary Title Decomposition and Article Stripping

**Purpose:** Split titles on delimiters and strip leading articles from both main and secondary parts.

**Secondary Title Delimiters (Priority Order):**

1. **Colon** `:` (highest priority)
2. **Dash with spaces** `-` (medium priority)
3. **Possessive with space** `'s ` (lowest priority, retains `'s` in main title)

**Process:**

1. Split title at first occurrence of highest-priority delimiter found
2. Strip leading articles ("The", "A", "An") from **both** main and secondary titles
3. Recombine with single space

**Examples:**

- `"Legend of Zelda: The Minish Cap"` → `"Legend of Zelda Minish Cap"` (secondary article stripped)
- `"Disney's The Lion King"` → `"Disney's Lion King"` (`'s ` delimiter, secondary article stripped)
- `"Movie - A New Hope"` → `"Movie New Hope"` (dash delimiter, secondary article stripped)
- `"Someone's Something: Time to Die"` → `"Someone's Something Time to Die"` (colon takes priority over `'s `)

#### Stage 6: Trailing Article Normalization

**Purpose:** Remove trailing articles like ", The" from the end of titles.

**Pattern:** `, The` followed by end of string or separator characters (space, colon, dash, bracket)

**Examples:**

- `"Legend, The"` → `"Legend"`
- `"Mega Man, The"` → `"Mega Man"`
- `"Story, the:"` → `"Story:"` (case insensitive)

#### Stage 7: Symbol and Separator Normalization

**Purpose:** Convert conjunctions and separators to normalized forms.

**Conjunctions:**

- `&` → `and`
- `+` → `and` (plus with spaces)
- `'n'` → `and`
- `'n` → `and`
- `n'` → `and`
- `n` → `and`
- `+` (standalone) → `plus`

**Separators:** `: _ - / \ , ;` → space
**Note:** Period `.` is NOT converted here; handled after abbreviation expansion (Stage 10)

**Examples:**

- `"Sonic & Knuckles"` → `"Sonic and Knuckles"`
- `"Rock + Roll Racing"` → `"Rock and Roll Racing"`
- `"Game+"` → `"Game plus"`
- `"Zelda:Link"` → `"Zelda Link"`
- `"Super_Mario_Bros"` → `"Super Mario Bros"`

#### Stage 8: Edition/Version Suffix Stripping

**Purpose:** Remove standalone edition/version words and version numbers from titles.

**Stripped Patterns:**

- **Edition words:** `version`, `edition`, `ausgabe`, `versione`, `edizione`, `versao`, `edicao`, `バージョン`, `エディション`, `ヴァージョン`
- **Version numbers:** `v1.0`, `v2.3.1`, `vVII`, etc.

**NOT Stripped:** Semantic edition markers like "Special", "Ultimate", "Remastered", "Deluxe", "Definitive" - these represent different products users may want to target specifically.

**Examples:**

- `"Pokemon Red Version"` → `"Pokemon Red"`
- `"Game Edition"` → `"Game"`
- `"Title v1.2"` → `"Title"`
- `"Game Special Edition"` → `"Game Special"` (Special kept, Edition stripped)
- `"Street Fighter II Champion Edition"` → `"Street Fighter II Champion"` (Edition stripped, Roman numeral preserved until Stage 13)

#### Stage 9: Abbreviation Expansion

**Purpose:** Expand common abbreviations found in game titles.

**Two Types:**

1. **Period-required:** Only expand when period present

   - `feat.` → `featuring` (but `feat` alone is a real word: achievement)
   - `no.` → `number` (but `no` alone is a word)
   - `st.` → `saint` (but `st` usually means "street")

2. **Flexible:** Expand with or without period
   - `vs` / `vs.` → `versus`
   - `bros` / `bros.` → `brothers`
   - `dr` / `dr.` → `doctor`
   - `mr` / `mr.` → `mister`
   - `vol` / `vol.` → `volume`
   - `pt` / `pt.` → `part`
   - `ft` / `ft.` → `featuring`
   - `jr` / `jr.` → `junior`
   - `sr` / `sr.` → `senior`

**Examples:**

- `"Mario vs Donkey Kong"` → `"Mario versus Donkey Kong"`
- `"Super Mario Bros."` → `"Super Mario Brothers"`
- `"Dr. Mario"` → `"Doctor Mario"`
- `"A great feat"` → `"A great feat"` (not expanded - no period)

#### Stage 10: Period Conversion

**Purpose:** Convert all periods to spaces (safe after abbreviation expansion).

**Examples:**

- `"Dr. Mario"` → `"Doctor  Mario"` (abbreviation already expanded, periods become spaces)
- `"Game.Title.Example"` → `"Game Title Example"`

**Why after Stage 9?** Ensures abbreviations are expanded first (using period as a signal), then periods can be safely converted to spaces.

#### Stage 11: Number Word Expansion

**Purpose:** Expand number words (one, two, three, etc.) to numeric forms.

**Supported:** 1-20 in both forms (`one` or `one.` → `1`)

**Examples:**

- `"Game One"` → `"Game 1"`
- `"Part Two"` → `"Part 2"`
- `"Street Fighter Two"` → `"Street Fighter 2"`

**Process:** Applied twice - once to existing words, then again after period removal creates new words (e.g., `"one.two.three"` → `"one two three"` → `"1 2 3"`).

#### Stage 12: Ordinal Number Normalization

**Purpose:** Remove ordinal suffixes from numbers.

**Pattern:** `\b(\d+)(?:st|nd|rd|th)\b` → `$1`

**Examples:**

- `"Street Fighter 2nd Impact"` → `"Street Fighter 2 Impact"`
- `"21st Century"` → `"21 Century"`
- `"3rd Strike"` → `"3 Strike"`

**Why before Stage 13?** Allows `"2nd"` and `"II"` to both normalize to `"2"` for consistent matching.

#### Stage 13: Roman Numeral Conversion

**Purpose:** Convert Roman numerals (II-XIX) to Arabic numbers. Also lowercases the entire string.

**Supported:** II, III, IV, V, VI, VII, VIII, IX, XI, XII, XIII, XIV, XV, XVI, XVII, XVIII, XIX
**Intentionally Excluded:** X (to avoid "Mega Man X" → "Mega Man 10")

**Examples:**

- `"Final Fantasy VII"` → `"final fantasy 7"` (converted and lowercased)
- `"Street Fighter II"` → `"street fighter 2"`
- `"Mega Man X"` → `"mega man x"` (X preserved)

**Optimization:** Performs case-insensitive matching without full-string case conversions by converting to lowercase character-by-character during output. This is the final text processing stage.

#### Stage 14: Final Slugification (Multi-Script Aware)

**Purpose:** Remove non-alphanumeric characters, with script-aware filtering.

**Process:**

1. Create two versions:
   - **ASCII slug:** Remove everything except `a-z0-9`
   - **Unicode slug:** Remove everything except `a-z0-9` + all script characters
2. **Script Detection:** Determine if title contains non-Latin characters
3. **Intelligent Selection:**
   - If contains CJK/Cyrillic/Greek/etc. → use Unicode slug
   - Otherwise → use ASCII slug

**Why Unicode slug for mixed titles?** The Unicode slug naturally concatenates both Latin and non-Latin portions, making titles searchable by EITHER part without requiring separate database columns.

**Examples:**

- Pure CJK: `"ドラゴンクエスト"` → `"ドラゴンクエスト"` (preserved)
- CJK with numeral: `"ファイナルファンタジーVII"` → `"ファイナルファンタジー7"`
- Mixed Latin+CJK: `"Street Fighter ストリート"` → `"streetfighterストリート"` (both parts preserved!)
- Pure Latin: `"The Legend of Zelda"` → `"legendofzelda"` (standard ASCII)
- Cyrillic: `"Тетрис"` → `"тетрис"` (preserved)

### Complete Normalization Examples

#### Example 1: Latin Title with Metadata

```
Input:     "The Legend of Zelda: The Minish Cap (USA) [!]"
Stage 1:   "The Legend of Zelda: The Minish Cap (USA) [!]" (no fullwidth)
Stage 2:   "The Legend of Zelda: The Minish Cap (USA) [!]" (no special punctuation)
Stage 3:   "The Legend of Zelda: The Minish Cap (USA) [!]" (unicode normalized)
Stage 4:   "The Legend of Zelda: The Minish Cap" (brackets removed)
Stage 5:   "Legend of Zelda Minish Cap" (split on ":", stripped "The" from both)
Stage 6:   "Legend of Zelda Minish Cap" (no trailing article)
Stage 7:   "Legend of Zelda Minish Cap" (no symbols to normalize)
Stage 8:   "Legend of Zelda Minish Cap" (no edition suffix)
Stage 9:   "Legend of Zelda Minish Cap" (no abbreviations)
Stage 10:  "Legend of Zelda Minish Cap" (no periods)
Stage 11:  "Legend of Zelda Minish Cap" (no number words)
Stage 12:  "Legend of Zelda Minish Cap" (no ordinals)
Stage 13:  "legend of zelda minish cap" (lowercased)
Stage 14:  "legendofzeldaminishcap" (ASCII slug - no non-Latin detected)
```

#### Example 2: Pure CJK Title

```
Input:     "ドラゴンクエストVII (Japan)"
Stage 1:   "ドラゴンクエストVII (Japan)" (halfwidth katakana → fullwidth if any)
Stage 2:   "ドラゴンクエストVII (Japan)" (no special punctuation)
Stage 3:   "ドラゴンクエストVII (Japan)" (NFC applied, diacritics preserved)
Stage 4:   "ドラゴンクエストVII" (brackets removed)
Stage 5:   "ドラゴンクエストVII" (no secondary title)
Stage 6:   "ドラゴンクエストVII" (no trailing article)
Stage 7:   "ドラゴンクエストVII" (no symbols to normalize)
Stage 8:   "ドラゴンクエストVII" (no edition suffix)
Stage 9:   "ドラゴンクエストVII" (no abbreviations)
Stage 10:  "ドラゴンクエストVII" (no periods)
Stage 11:  "ドラゴンクエストVII" (no number words)
Stage 12:  "ドラゴンクエストVII" (no ordinals)
Stage 13:  "ドラゴンクエスト7" (VII → 7, lowercased)
Stage 14:  "ドラゴンクエスト7" (Unicode slug - CJK detected)
```

#### Example 3: Complex Latin Title with Abbreviations

```
Input:     "Super Mario Bros. 2nd Edition (USA) (Rev A)"
Stage 1:   "Super Mario Bros. 2nd Edition (USA) (Rev A)" (no fullwidth)
Stage 2:   "Super Mario Bros. 2nd Edition (USA) (Rev A)" (no special punctuation)
Stage 3:   "Super Mario Bros. 2nd Edition (USA) (Rev A)" (unicode normalized)
Stage 4:   "Super Mario Bros. 2nd Edition" (brackets removed)
Stage 5:   "Super Mario Bros. 2nd Edition" (no secondary title)
Stage 6:   "Super Mario Bros. 2nd Edition" (no trailing article)
Stage 7:   "Super Mario Bros  2nd Edition" (period after Bros becomes space)
Stage 8:   "Super Mario Bros  2nd" (Edition stripped)
Stage 9:   "Super Mario Brothers  2nd" (Bros → Brothers)
Stage 10:  "Super Mario Brothers  2nd" (periods already converted)
Stage 11:  "Super Mario Brothers  2nd" (no number words)
Stage 12:  "Super Mario Brothers  2" (2nd → 2)
Stage 13:  "super mario brothers  2" (lowercased)
Stage 14:  "supermariobrothers2" (ASCII slug)
```

---

## Indexing Workflow

The indexing workflow scans the filesystem and populates the media database with normalized entries that can be efficiently searched during resolution.

**Entry Point:** `pkg/database/mediascanner/indexing_pipeline.go`

**Process Flow:**

```
Filesystem Scan → Path Fragments Extraction → Title Normalization →
Slug Generation → Tag Extraction → Database Insertion
```

### Pre-Processing: Contextual Leading Number Detection

**Purpose:** Distinguish between intentional list numbering (e.g., `"1. Game.zip"`, `"2. Game.zip"`) and legitimate title numbers (e.g., `"1942.zip"`, `"007.zip"`) by analyzing the entire directory context.

**Location:** `pkg/database/mediascanner/mediascanner.go` → `detectNumberingPattern()`

**Algorithm:**

```go
func detectNumberingPattern(files []platforms.ScanResult, threshold float64, minFiles int) bool
```

**Parameters:**

- `threshold`: Minimum ratio of matching files (default: 0.5 = 50%)
- `minFiles`: Minimum file count to apply heuristic (default: 5)

**Pattern:** `^\d{1,3}[.\s\-]+` (matches `"1. "`, `"01 - "`, `"42. "`, etc.)

**Logic:**

1. Count files matching the list numbering pattern in a directory
2. Calculate ratio: `matching_files / total_files`
3. Apply stripping if: `ratio > threshold AND total_files >= minFiles`

**Examples:**

Directory with list numbering (stripping enabled):

```
1. Super Mario Bros.nes
2. Legend of Zelda.nes
3. Metroid.nes
4. Mega Man.nes
5. Castlevania.nes
→ 5/5 files match (100% > 50%) → stripLeadingNumbers = true
```

Directory with legitimate numbers (stripping disabled):

```
1942.nes
1943.nes
Galaga.nes
Pac-Man.nes
Donkey Kong.nes
→ 2/5 files match (40% < 50%) → stripLeadingNumbers = false
```

**Application:** The `stripLeadingNumbers` boolean is passed to `ParseTitleFromFilename()` which affects both title extraction for display AND slug generation.

### Step 1: Extract Path Fragments

**Function:** `GetPathFragments(cfg, path, noExt, stripLeadingNumbers)`

**Process:**

1. **Clean path:** `filepath.Clean(path)` unless URI scheme detected
2. **Extract extension:** `filepath.Ext()` → lowercase
3. **Extract filename:** Remove extension from base path
4. **Extract title:** Call `ParseTitleFromFilename(filename, stripLeadingNumbers)`
   - If `stripLeadingNumbers = true`: Remove `^\d+[.\s\-]+` pattern
   - Strip everything after first bracket: `^([^(\[{<]*)`
   - Normalize separators: `_` → space, `&` → `and`
5. **Slugify title:** Call `slugs.SlugifyString(title)` (14-stage pipeline)
6. **Handle empty slugs:** If slug is empty, use lowercase filename as fallback
7. **Extract tags:** Call `tags.ParseFilenameToCanonicalTags()` (if enabled)
8. **Cache result:** Store `PathFragments` for future lookups

**Example 1: Latin Title (no list numbering)**

```
Path:              "/roms/nes/Super Mario Bros (USA) [!].nes"
FileName:          "Super Mario Bros (USA) [!]"
StripLeadingNums:  false
Title:             "Super Mario Bros"
Slug:              "supermariobros"
Ext:               ".nes"
Tags:              ["region:us", "dump:verified"]
```

**Example 2: With List Numbering**

```
Path:              "/roms/nes/1. Super Mario Bros (USA).nes"
FileName:          "1. Super Mario Bros (USA)"
StripLeadingNums:  true (detected at directory level)
Title:             "Super Mario Bros" (leading "1. " stripped)
Slug:              "supermariobros"
Ext:               ".nes"
Tags:              ["region:us"]
```

**Example 3: CJK Title**

```
Path:              "/roms/sfc/ドラゴンクエストVII (Japan).sfc"
FileName:          "ドラゴンクエストVII (Japan)"
StripLeadingNums:  false
Title:             "ドラゴンクエストVII"
Slug:              "ドラゴンクエスト7"
Ext:               ".sfc"
Tags:              ["region:jp"]
```

### Step 2: Tag Extraction

**Function:** `tags.ParseFilenameToCanonicalTags(filename)`

**Tag Extraction System:** `pkg/database/tags/filename_parser.go`

**Bracket Type Support:**

- **Parentheses `()`:** Region, language, year, development status, revisions, versions
- **Square brackets `[]`:** Dump info (verified, bad, hacked, translation, fixed, cracked)
- **Braces `{}`:** Treated like parentheses
- **Angle brackets `<>`:** Treated like parentheses

**Tag Types Extracted:**

1. **Region codes:** `(USA)`, `{Europe}`, `<Japan>` → `region:us`, `region:eu`, `region:jp`
2. **Languages:** `(En)`, `{Fr,De}`, `<Es>` → `lang:en`, `lang:fr`, `lang:de`, `lang:es`
3. **Years:** `(1997)` → `year:1997` (1970-2029 supported)
4. **Development status:** `(Beta)`, `{Proto}` → `unfinished:beta`, `unfinished:proto`
5. **Revisions:** `(Rev A)`, `{Rev 1}` → `rev:a`, `rev:1`
6. **Versions:** `(v1.0)`, `<v1.2.3>` → `rev:1.0`, `rev:1.2.3`
7. **Dump info:** `[!]` → `dump:verified`, `[b]` → `dump:bad`
8. **Unlicensed:** `[h]` → `unlicensed:hack`, `[T+Eng]` → `unlicensed:translation`, `lang:en`

**Multi-language handling:** `(En,Fr,De)` → multiple tags: `lang:en`, `lang:fr`, `lang:de`

**Tag Format:** Canonical tags use `"type:value"` format for database storage.

### Step 3: Database Insertion

**Function:** `AddMediaPath(db, ss, systemID, path, noExt, stripLeadingNumbers, cfg)`

**Database Schema:**

```
Systems (DBID, SystemID, Name)
    ↓ FK: SystemDBID
MediaTitles (DBID, Slug, Name, SystemDBID)
    ↓ FK: MediaTitleDBID
Media (DBID, Path, MediaTitleDBID, SystemDBID)
    ↓ FK: MediaDBID
MediaTags (MediaDBID, TagDBID)
    ↓ FK: TagDBID
Tags (DBID, Tag, TypeDBID)
    ↓ FK: TypeDBID
TagTypes (DBID, Type)
```

**Insertion Order:**

1. Insert/find **System** by `SystemID`
2. Insert/find **MediaTitle** by `(Slug, SystemDBID)` composite key
   - Stores both **Slug** (for matching) and **Name** (for display)
3. Insert/find **Media** by `(Path, SystemDBID)` composite key
4. Insert/find **Tags** (if filename tags enabled)
5. Link tags to media via **MediaTags** join table

**Constraint Handling:**

- UNIQUE constraint violations handled gracefully
- Existing entries looked up and IDs cached in `ScanState` maps
- In-memory maps prevent duplicate insertion attempts

**Caching:** `ScanState` maintains maps:

- `SystemIDs`: `systemID → DBID`
- `TitleIDs`: `systemID:slug → DBID`
- `MediaIDs`: `systemID:path → DBID`
- `TagTypeIDs`: `type → DBID`
- `TagIDs`: `type:value → DBID`

### Step 4: Tag Seeding

**Function:** `SeedCanonicalTags(db, ss)`

**Pre-seeded Tag Types:**

- `unknown` (fallback for unmapped tags)
- `extension` (file extensions: `.nes`, `.sfc`, etc.)
- All canonical types from `tags.CanonicalTagDefinitions`:
  - `region` (60+ region codes)
  - `lang` (50+ language codes)
  - `year` (1970-2029)
  - `dump` / `dumpinfo` (verified, bad, fixed, cracked)
  - `unfinished` (demo, beta, proto, alpha, sample, preview, prerelease)
  - `unlicensed` (hack, translation, bootleg, clone, aftermarket, pirate)
  - `media` (disc, cart, tape, digital)
  - `disc`, `disctotal` (disc numbers and totals)
  - `rev` (revision identifiers)
  - `rerelease`, `reboxed`, `players`, `perspective`, `special`, etc.

**When:** During fresh indexing or when indexes are rebuilt

---

## Resolution Workflow

The resolution workflow takes a user-provided game title and finds the best matching media entry using progressive fallback strategies.

**Entry Point:** `pkg/zapscript/titles.go` → `cmdTitle()`

**Input Format:** `SystemID/GameName` (e.g., `nes/Super Mario Bros`, `sfc/ドラゴンクエスト`)

**Output:** Best matching media entry to launch

### Input Processing

1. **Validate format:** Must contain `/` separator, both parts non-empty
2. **Parse system ID:** Extract and validate using `systemdefs.LookupSystem()`
3. **Parse game name:** Extract title portion
4. **Parse tag filters:** Extract from advanced args `[tags=...]` syntax
5. **Slugify query:** Normalize using `slugs.SlugifyString()`

### Resolution Cache

**Purpose:** Avoid expensive fuzzy matching by caching successful resolutions.

**Table:** `SlugResolutionCache`

**Cache Key:** `SystemID + Slug + TagFilters` (JSON-serialized and sorted for consistency)

**Cached Data:**

- `MediaDBID`: Which media was matched
- `Strategy`: Which strategy found the match (for analytics/debugging)
- `LastUpdated`: Timestamp

**Process:**

1. Check cache before attempting resolution
2. On cache hit, retrieve full media entry by DBID
3. On cache miss, execute full resolution workflow
4. On successful match, cache the resolution with strategy name

### Resolution Strategies

The system attempts strategies in order, using the first that produces results. Each strategy is progressively more lenient.

#### Strategy 1: Exact Match

**Function:** `SearchMediaBySlug(systemID, slug, tagFilters)`

Direct lookup of the slugified query.

**Example:**

- Query: `nes/Super Mario Bros`
- Slug: `supermariobros`
- Matches: Database entries with exact slug `supermariobros` in system `nes`

**Database Query:** `WHERE SystemID = ? AND Slug = ?` with tag filters applied

#### Strategy 2: Prefix Match with Edition-Aware Ranking

**Function:** `SearchMediaBySlugPrefix(systemID, slug, tagFilters)`

Finds all titles starting with the query slug, validates word sequences, then ranks by score.

**Word Sequence Validation:**

- Uses `NormalizeToWords()` to convert titles to word arrays
- For queries with 2+ words, candidates must start with the same word sequence
- `"super mario"` → `["super", "mario"]` matches `"super mario bros"` → `["super", "mario", "bros"]` ✓
- `"super mario"` → `["super", "mario"]` does NOT match `"super metroid"` → `["super", "metroid"]` ✗

**Scoring Algorithm:** `ScorePrefixCandidate(querySlug, candidateSlug)`

```
score = 0

// Bonus: Has edition-like suffix
if hasSuffix(["se", "specialedition", "remaster", "remastered",
              "directorscut", "ultimate", "gold", "goty", "deluxe",
              "definitive", "enhanced", "cd32", "cdtv", "aga",
              "missiondisk", "expansion", "addon"]):
    score += 100

// Penalty: Has sequel-like suffix (uses last word)
if lastWord in ["2", "3", "4", "5", "6", "7", "8", "9",
                "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x"]:
    score -= 10
else:
    score += 20  // Bonus for NOT being a sequel

// Penalty: Length difference
score -= abs(len(candidate) - len(query))

return score
```

**Selection:** Candidate with highest score is selected.

#### Strategy 3: Token-Based Similarity Matching

**Functions:** `ScoreTokenMatch()` and `ScoreTokenSetRatio()`

When word sequence validation filters out all prefix candidates, attempts order-independent word matching using **two complementary methods**. The system uses the **maximum score** from both approaches.

**Method 1: Token Match (Weighted Word Matching)**

Algorithm:

1. Break titles into normalized words using `NormalizeToWords()`
2. For each query word, find best matching candidate word
3. Remove matched words from pool (prevents double-counting)
4. Apply word-level weights:
   - **Base weight:** 10.0
   - **Longer words (6+ chars):** +10 bonus
   - **Common words** ("of", "the", "and"): -5 penalty
5. Calculate score: `weighted_matched / (weighted_query + unmatched_candidate * 0.4 * 10.0)`

Strengths: Handles word variations ("link" vs "links"), exact word matching

**Method 2: Token Set Ratio (Set-Based Matching)**

Algorithm:

1. Convert titles to unique word sets (automatic deduplication)
2. Calculate intersection (common words)
3. Calculate coverage:
   - `query_coverage = intersection / query_words`
   - `candidate_coverage = intersection / candidate_words`
4. Score: `(query_coverage × 0.8 + candidate_coverage × 0.2) × (1 - extra_penalty)`

Strengths: Handles duplicate words, extra metadata, subset queries

**Selection:** System uses `max(TokenMatch, TokenSetRatio)` as final score. Minimum threshold: 0.5

**Examples:**

Token Match better:

- `"link awakening zelda"` vs `"Zelda Link's Awakening"`: TokenMatch 1.00, SetRatio 0.62

Token Set Ratio better:

- `"zelda zelda ocarina"` vs `"Legend of Zelda Ocarina of Time"`: TokenMatch 0.54, SetRatio 0.81

#### Strategy 4: Main Title-Only Search

**Function:** `GenerateMatchInfo(title)` → detects secondary titles

When a query contains a secondary title (delimited by `:`, `-`, or `'s `), search using just the main title.

**Process:**

1. Split title at first occurrence of highest-priority delimiter
2. Strip leading articles from secondary title
3. Slugify main title separately
4. Search for exact match on main title slug only

**Examples:**

- `"Legend of Zelda: Link's Awakening"` → main: `"legendofzelda"`, secondary: `"linksawakening"`
- Search for: `"legendofzelda"` (exact match)

#### Strategy 5: Secondary Title Exact Match

**Requirements:**

- Must have a secondary title (detected by `GenerateMatchInfo()`)
- Secondary title slug must be ≥4 characters

**Process:**

1. Extract secondary title slug using `GenerateMatchInfo()`
2. Try exact match on secondary title slug

**Examples:**

- Query: `"Legend of Zelda: Ocarina of Time"`
- Secondary slug: `"ocarinaoftime"`
- Searches for games matching exactly `"ocarinaoftime"`

#### Strategy 6: Secondary Title Prefix Match

**Requirements:**

- Must have a secondary title (detected by `GenerateMatchInfo()`)
- Secondary title slug must be ≥4 characters
- Strategy 5 (exact match) found no results

**Process:**

1. Extract secondary title slug using `GenerateMatchInfo()`
2. Try prefix match on secondary title slug

**Examples:**

- Query: `"Legend of Zelda: Ocarina"`
- Secondary slug: `"ocarina"`
- Searches for games starting with `"ocarina"`

#### Strategy 7: Jaro-Winkler Fuzzy Matching

**Function:** `FindFuzzyMatches()` in `pkg/database/matcher/fuzzy.go`

Handles typos and spelling variations using Jaro-Winkler similarity.

**Requirements:**

- Query slug must be ≥5 characters
- Fetches all slugs for the system using `GetAllSlugsForSystem()`

**Algorithm:**

- **Pre-filter:** Skip candidates with length difference > 2 characters
- **Calculate similarity:** Use `edlib.JaroWinklerSimilarity()` (0.0-1.0)
- **Minimum similarity:** 0.85 threshold
- **Sort:** By similarity (highest first)

**Why Jaro-Winkler:**

- Optimized for short strings (game titles)
- Heavily weights matching prefixes (users typically get start correct)
- Naturally handles British/American spelling variations (0.94-0.98 similarity)

**Examples:**

- `"zelad"` → `"zelda"` (similarity: 0.953)
- `"mraio"` → `"mario"` (similarity: 0.940)
- `"colour"` vs `"color"` (similarity: 0.967)

#### Strategy 8: Progressive Trim Candidates (Last Resort)

**Function:** `GenerateProgressiveTrimCandidates(title)`

Progressively removes words from the end of overly-verbose queries.

**Pre-Processing:**

1. Strip metadata brackets using `StripMetadataBrackets()`
2. Strip edition/version suffixes using `StripEditionAndVersionSuffixes()`
3. Split into words using `strings.Fields()`

**Candidate Generation:**

- Generates up to 10 trim candidates
- Stops when: down to 2 words, slug length < 6 chars, or 10 iterations reached
- Each candidate tried with both exact match and prefix match
- Deduplication prevents redundant queries

**Example:**

```
Input: "Legend of Zelda: Link's Awakening DX (USA)"

Pre-processed: "Legend of Zelda Link's Awakening DX" (metadata stripped)
Words: ["Legend", "of", "Zelda", "Link's", "Awakening", "DX"]

Candidates (up to 10 trims, min 2 words, min 6 chars):
1. "legendofzeldalinksawakeningdx" (exact + prefix)
2. "legendofzeldalinksawakening" (exact + prefix)
3. "legendofzeldalinks" (exact + prefix)
4. "legendofzelda" (exact + prefix)
5. "legendof" (exact + prefix)
6. "legend" (6 chars - stops here)
```

### Multi-Result Selection

When multiple results match, `selectBestResult()` applies intelligent prioritization:

**Priority 1: User-Specified Tags**

- If tag filters provided via `[tags=...]` syntax, filter to exact tag matches
- Uses `filterByTags()` with operator support (AND, NOT, OR)

**Priority 2: Exclude Variants**

- Function: `filterOutVariants()` calls `isVariant()`
- Removes:
  - Demos: `unfinished:demo*`
  - Betas: `unfinished:beta*`
  - Prototypes: `unfinished:proto*`
  - Alphas: `unfinished:alpha*`
  - Samples: `unfinished:sample`
  - Previews: `unfinished:preview`
  - Prereleases: `unfinished:prerelease`
  - Hacks: `unlicensed:hack`
  - Translations: `unlicensed:translation*`
  - Bootlegs: `unlicensed:bootleg`
  - Clones: `unlicensed:clone`
  - Bad dumps: `dump:bad`

**Priority 3: Exclude Re-releases**

- Function: `filterOutRereleases()` calls `isRerelease()`
- Removes:
  - Re-releases: `rerelease:*`
  - Reboxed: `reboxed:*`

**Priority 4: Preferred Regions**

- Function: `filterByPreferredRegions()`
- Match against `config.DefaultRegions()` from user configuration
- Prefer entries tagged with user's regions
- Fallback to untagged entries
- Last resort: other regions

**Priority 5: Preferred Languages**

- Function: `filterByPreferredLanguages()`
- Match against `config.DefaultLangs()` from user configuration
- Prefer entries tagged with user's languages
- Fallback to untagged entries
- Last resort: other languages

**Priority 6: Alphabetical by Filename**

- Function: `selectAlphabeticallyByFilename()`
- If still multiple results, pick first alphabetically
- Uses `filepath.Base()` for filename comparison

---

## Multi-Script Support

The slug system provides comprehensive support for non-Latin scripts, preserving them while normalizing Latin text.

### Supported Scripts

- **Latin:** English, French, Spanish, etc. (default/fallback)
- **CJK:** Chinese (Hanzi), Japanese (Hiragana, Katakana, Kanji), Korean (Hangul)
- **Cyrillic:** Russian, Ukrainian, Bulgarian, Serbian, etc.
- **Greek:** Ancient and Modern Greek
- **Indic:** Devanagari, Bengali, Tamil, Telugu, Kannada, Malayalam, Gurmukhi, Gujarati, Oriya, Sinhala
- **Arabic:** Arabic, Persian, Urdu
- **Hebrew:** Hebrew
- **Thai:** Thai (requires n-gram matching for no word boundaries)
- **Burmese:** Burmese/Myanmar (requires n-gram matching)
- **Khmer:** Khmer/Cambodian (requires n-gram matching)
- **Lao:** Lao (requires n-gram matching)
- **Amharic:** Amharic/Ethiopic (Ge'ez)

### Script Detection

**Location:** `pkg/database/slugs/scripts.go` → `detectScript()`

**Process:**

1. Scan string for characters from each script
2. Return first detected non-Latin script
3. If no non-Latin script found, return `ScriptLatin`

**Script Types:**

```go
type ScriptType int

const (
    ScriptLatin    ScriptType = iota // Latin alphabet (English, French, Spanish, etc.)
    ScriptCJK                        // Chinese, Japanese, Korean
    ScriptCyrillic                   // Russian, Ukrainian, Bulgarian, Serbian, etc.
    ScriptGreek                      // Greek
    ScriptIndic                      // Devanagari, Bengali, Tamil, Telugu, etc.
    ScriptArabic                     // Arabic, Urdu, Persian/Farsi
    ScriptHebrew                     // Hebrew
    ScriptThai                       // Thai (requires n-gram matching)
    ScriptBurmese                    // Burmese/Myanmar (requires n-gram matching)
    ScriptKhmer                      // Khmer/Cambodian (requires n-gram matching)
    ScriptLao                        // Lao (requires n-gram matching)
    ScriptAmharic                    // Amharic/Ethiopic
)
```

### Script-Specific Normalization

**Location:** `pkg/database/slugs/normalization.go` → `normalizeByScript()`

**Latin Text:**

- **NFKC:** Compatibility normalization (ligatures, superscripts, etc.)
- **NFD:** Decompose characters into base + combining marks
- **Mark Removal:** Strip combining marks (diacritics)
- **NFC:** Recompose into canonical form

**CJK Text:**

- **NFC only:** Canonical composition to properly combine marks from width.Fold
- **No NFKC:** Prevents mangling of katakana characters
- **No diacritic removal:** Preserves essential marks (dakuten, handakuten)

**Other Scripts:**

- **NFC only:** Preserves script-specific marks and features
- **No aggressive normalization:** Maintains script integrity

### Mixed-Language Titles

For titles containing both Latin and non-Latin text, the Unicode slug concatenates both portions, making the title searchable by EITHER part.

**Examples:**

Pure CJK:

```
"ドラゴンクエストVII" → "ドラゴンクエスト7"
```

Mixed Latin + CJK:

```
"Street Fighter ストリートファイター" → "streetfighterストリートファイター"
```

Searchable by either:

- Query: `"street fighter"` → matches via Latin portion
- Query: `"ストリートファイター"` → matches via CJK portion

Pure Cyrillic:

```
"Тетрис" → "тетрис"
```

### Width Normalization

**Purpose:** Ensure consistent character widths for CJK and Latin text.

**Transformations:**

- **Fullwidth ASCII** → **Halfwidth:** `"ＡＢＣＤＥＦ"` → `"ABCDEF"`
- **Halfwidth Katakana** → **Fullwidth:** `"ｳｴｯｼﾞ"` → `"ウエッジ"`

**Why:** Enables consistent regex matching and proper CJK display.

---

## Utility Functions

### NormalizeToWords()

**Purpose:** Convert a title to normalized word arrays for token-based matching and scoring.

**Location:** `pkg/database/slugs/slugify.go` → `NormalizeToWords()`

**Process:**

- Runs Stages 1-13 of normalization pipeline (same as `normalizeInternal()`)
- **Stops before** Stage 14 (final character filtering)
- Preserves spaces between words
- Returns `[]string` of lowercase words

**Example:**

```go
NormalizeToWords("The Legend of Zelda: Ocarina of Time (USA)")
→ "legend of zelda ocarina of time"
→ []string{"legend", "of", "zelda", "ocarina", "of", "time"}
```

**Used By:**

- Token-based similarity matching
- Word sequence validation
- Sequel suffix detection
- Weighted word scoring

### StripLeadingArticle()

**Purpose:** Remove leading articles ("The", "A", "An") from a string.

**Examples:**

- `"The Legend of Zelda"` → `"Legend of Zelda"`
- `"A New Hope"` → `"New Hope"`
- `"An American Tail"` → `"American Tail"`

### SplitTitle()

**Purpose:** Split a title into main and secondary parts based on delimiter priority.

**Delimiters (priority order):**

1. `:` (highest)
2. `-` (medium)
3. `'s ` (lowest, retains `'s` in main title)

**Returns:** `(mainTitle, secondaryTitle, hasSecondary)`

**Examples:**

- `"Zelda: Link's Awakening"` → `("Zelda", "Link's Awakening", true)`
- `"Game - Subtitle"` → `("Game", "Subtitle", true)`
- `"Mario's Adventure"` → `("Mario's", "Adventure", true)`

---

## Performance Optimizations

### Pipeline Context Caching

**Type:** `pipelineContext`

**Cached Values:**

- `isASCII`: Result of ASCII check (avoids redundant checks)
- `script`: Detected script type
- `scriptCached`: Whether script detection has been performed

**Benefits:**

- Avoids multiple ASCII checks across stages
- Reuses script detection for final slug selection
- Reduces redundant Unicode processing

### Slug Resolution Caching

**Table:** `SlugResolutionCache`

**Purpose:** Cache successful slug resolutions to avoid expensive fuzzy matching.

**Cache Invalidation:** Entries cascade-deleted when media is removed.

### Database Indexes

**Critical Indexes:**

- `(SystemDBID, Slug)`: Supports prefix searches
- `(Slug)`: Supports exact match lookups
- `(TagDBID, MediaDBID)`: Supports tag filtering

### ScanState Maps

**Purpose:** In-memory caching during indexing to reduce database lookups.

**Maps:**

- `SystemIDs`: `systemID → DBID`
- `TitleIDs`: `systemID:slug → DBID`
- `MediaIDs`: `systemID:path → DBID`
- `TagTypeIDs`: `type → DBID`
- `TagIDs`: `type:value → DBID`

**Benefit:** Prevents duplicate insertion attempts and reduces database round-trips.

---

## Implementation Notes

### For Developers

1. **Slug Normalizer** is the single source of truth - both indexing and resolution use identical normalization
2. **Resolution Workflow** owns all database querying and matching strategies
3. **Indexing Workflow** is responsible for all database writes
4. Database indexes on `(SystemDBID, Slug)` are critical for resolution performance
5. Cache slugified values - slugification is deterministic but regex-heavy
6. Test edge cases: Unicode, possessives, Roman numerals, special characters, CJK text, mixed-language titles
7. Magic numbers are named constants - see `pkg/database/matcher/scoring.go` and `pkg/zapscript/titles.go`
8. All normalization uses deterministic, idempotent operations

### Idempotency Guarantee

The slugification function is **idempotent and deterministic**:

```
SlugifyString(SlugifyString(x)) == SlugifyString(x)
```

Running slugification multiple times produces the same result. This holds for both Latin and non-Latin text.

### Function Reference

| Function                              | Location                               | Purpose                                                 |
| ------------------------------------- | -------------------------------------- | ------------------------------------------------------- |
| `SlugifyString()`                     | `pkg/database/slugs/slugify.go`        | Core 14-stage normalization (Stages 1-13 + slugification) |
| `NormalizeToWords()`                  | `pkg/database/slugs/slugify.go`        | Normalize to word arrays (stops before final filtering) |
| `StripLeadingArticle()`               | `pkg/database/slugs/slugify.go`        | Remove leading articles ("The", "A", "An")              |
| `SplitTitle()`                        | `pkg/database/slugs/slugify.go`        | Split on delimiters (`:`, `-`, `'s `)                   |
| `GenerateMatchInfo()`                 | `pkg/database/matcher/scoring.go`      | Extract main/secondary title slugs                      |
| `GenerateProgressiveTrimCandidates()` | `pkg/database/matcher/scoring.go`      | Generate progressive trim candidates                    |
| `ScorePrefixCandidate()`              | `pkg/database/matcher/scoring.go`      | Score prefix matches with edition awareness             |
| `ScoreTokenMatch()`                   | `pkg/database/matcher/scoring.go`      | Token-based similarity (word-order independent)         |
| `ScoreTokenSetRatio()`                | `pkg/database/matcher/scoring.go`      | Set-based similarity (handles duplicates)               |
| `StartsWithWordSequence()`            | `pkg/database/matcher/scoring.go`      | Validate word sequence matching                         |
| `FindFuzzyMatches()`                  | `pkg/database/matcher/fuzzy.go`        | Jaro-Winkler fuzzy matching                             |
| `ParseTitleFromFilename()`            | `pkg/database/tags/filename_parser.go` | Extract clean titles from filenames                     |
| `ParseFilenameToCanonicalTags()`      | `pkg/database/tags/filename_parser.go` | Extract tags from filenames                             |
