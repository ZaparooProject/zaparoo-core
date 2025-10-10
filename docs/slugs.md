# Slug System Reference

This document describes Zaparoo Core's title normalization and matching system, which enables fuzzy matching between user-provided game titles (from NFC tags) and messy ROM filenames.

## Architecture Overview

The system is built around **two primary workflows** supported by shared normalization and parsing libraries:

### Primary Workflows

1. **Indexing Workflow** - Scans filesystem and populates the media database
   - Parses filenames to extract titles and metadata tags
   - Generates normalized slugs for matching
   - Stores structured data for fast lookups

2. **Resolution Workflow** - Matches user queries against the indexed database
   - Normalizes user input using the same rules as indexing
   - Executes progressive fallback strategies for fuzzy matching
   - Selects best result when multiple matches exist

### Shared Libraries

- **Slug Normalizer** (`pkg/database/slugs/`) - Canonical slug generation and normalization
  - `slugify.go` - Core 10-stage normalization pipeline
  - `scripts.go` - Script detection (CJK, Cyrillic, Arabic, etc.)
  - `normalization.go` - Script-specific normalization rules
- **Matcher** (`pkg/database/matcher/`) - Resolution-specific matching algorithms
  - `scoring.go` - Token/prefix scoring, candidate ranking
  - `fuzzy.go` - Jaro-Winkler fuzzy matching
- **Tag Parser** (`pkg/database/tags/filename_parser.go`) - Extract metadata and titles from filenames
- **Indexing Pipeline** (`pkg/database/mediascanner/indexing_pipeline.go`) - Filesystem scanning and database population

## Shared Library: Slug Normalizer

**Purpose:** Convert any game title into a canonical, normalized slug optimized for fuzzy matching. Used by both indexing and resolution workflows.

**Location:** `pkg/database/slugs/slugify.go` → `SlugifyString()`

**Used By:**
- Resolution Workflow: Normalizes user queries
- Indexing Workflow: Normalizes filenames for database storage

**Input:** Any title string (e.g., `"The Legend of Zelda: Ocarina of Time"`)

**Output:** Normalized slug string (e.g., `"legendofzeldaocarinaoftime"`)

### Normalization Pipeline

#### Stage 1: Width Normalization

**Process:**

Converts fullwidth and halfwidth characters to their normalized forms using `width.Fold`:

- **ASCII characters**: Fullwidth → Halfwidth (enables Latin text processing)
- **CJK characters**: Halfwidth → Fullwidth (ensures consistent display and matching)

**Why width normalization first?** This ensures all subsequent stages work on consistent character widths. Fullwidth ASCII becomes regular ASCII for regex matching, while halfwidth katakana becomes fullwidth for proper CJK handling.

**Examples:**

- Fullwidth ASCII: `"ＡＢＣＤＥＦ"` → `"ABCDEF"`
- Fullwidth numbers: `"１２３"` → `"123"`
- Fullwidth delimiters: `"Game：Subtitle"` → `"Game:Subtitle"`
- Halfwidth katakana: `"ｳｴｯｼﾞ"` → `"ウエッジ"`
- Mixed: `"Super Ｍario １２３"` → `"Super Mario 123"`

#### Stage 2: Unicode Normalization (Symbol Removal + NFKC/NFC + Diacritic Removal)

**Process:**

1. **Symbol Removal** - Removes unicode symbols from categories `So` (Other Symbol: ™, ®, ©, ℠) and `Sc` (Currency Symbol: $, €, ¥). Math symbols like `<`, `>`, `+` are preserved for later removal.
2. **Script-Aware Normalization**:
   - **For Latin text** (no CJK detected):
     - NFKC Normalization - Normalizes compatibility characters to their canonical forms
     - NFD + Mark Removal + NFC - Removes diacritical marks while preserving base characters
   - **For CJK text** (contains Chinese, Japanese, or Korean characters):
     - NFC only - Canonical composition to properly combine marks from width.Fold
     - **No NFKC** - Prevents mangling of katakana characters
     - **No diacritic removal** - Preserves essential marks (dakuten, handakuten)

Unicode normalization ensures all subsequent regex patterns and string operations work on predictable, canonical text. CJK-specific handling prevents corruption of Japanese/Korean/Chinese characters.

**Why remove symbols first?** NFKC converts some symbols to ASCII letters (™→TM, ℠→SM), which would incorrectly become part of the slug. By removing symbols first, we ensure they're completely stripped rather than converted. Math symbols are preserved because they'll be handled by the final non-alphanumeric cleanup stage.

**Examples:**

- Symbols: `"Sonic™"` → `"Sonic"`, `"Game®"` → `"Game"` (removed, not converted to letters)
- Diacritics (Latin): `"Pokémon"` → `"Pokemon"`, `"Café"` → `"Cafe"`
- Ligatures: `"ﬁnal"` → `"final"`
- Superscripts: `"Game²"` → `"Game2"`
- Other compatibility chars: `"①"` → `"1"`
- CJK preserved: `"ドラゴンクエスト"` → `"ドラゴンクエスト"`

#### Stage 3: Leading Number Prefix Stripping

**Pattern:** `^\d+[.\s\-]+`

Removes common list numbering prefixes:

- `"1. Game Title"` → `"Game Title"`
- `"01 - Game Title"` → `"Game Title"`
- `"42. Answer"` → `"Answer"`

#### Stage 4: Secondary Title Decomposition and Article Stripping

**Secondary Title Delimiters (Priority Order):**

1. Colon: `:` (highest priority)
2. Dash with spaces: `-` (medium priority)
3. Possessive with space: `'s ` (lowest priority - retains `'s` in main title)

**Delimiter Priority Rules:**

- Only the **first occurrence** of the **highest-priority delimiter** is used for splitting
- If a colon `:` exists anywhere in the title, it takes precedence over `-` and `'s `
- If no colon exists but `-` does, it takes precedence over `'s `
- `'s ` is only used as a delimiter if neither `:` nor `-` are present

**Process:**

1. Split title at first occurrence of highest-priority delimiter found
2. Strip leading articles ("The", "A", "An") from both main and secondary titles independently using `stripLeadingArticle()`
3. Recombine with single space

**Examples:**

- `"Legend of Zelda: The Minish Cap"` → `"Legend of Zelda Minish Cap"` (secondary article stripped)
- `"Disney's The Lion King"` → `"Disney's Lion King"` (`'s ` used as delimiter, secondary article stripped)
- `"Movie - A New Hope"` → `"Movie New Hope"` (`-` used as delimiter, secondary article stripped)
- `"Someone's Something: Time to Die"` → `"Someone's Something Time to Die"` (`:` takes priority over `'s `)
- `"Player's Choice - Final Battle"` → `"Player's Choice Final Battle"` (`-` takes priority over `'s `)

#### Stage 5: Trailing Article Normalization

**Pattern:** `,\s*the\s*($|[\s:\-\(\[])` (case-insensitive)

Removes ", The" from the end:

- `"Legend, The"` → `"Legend"`
- `"Mega Man, The"` → `"Mega Man"`

#### Stage 6: Symbol and Separator Normalization

**Patterns (via `normalizeSymbolsAndSeparators()`):**

Conjunctions:
- `&` → `and`
- `\s+\+\s+` → `and` (plus with spaces)
- `\s+'n'\s+` → `and` (n with both apostrophes)
- `\s+'n\s+` → `and` (n with left apostrophe)
- `\s+n'\s+` → `and` (n with right apostrophe)
- `\s+n\s+` → `and` (standalone n)

Separators:
- `[:_\-]+` → ` ` (space)

Converts conjunctions and separators in one pass:

- `"Sonic & Knuckles"` → `"Sonic and Knuckles"`
- `"Rock + Roll Racing"` → `"Rock and Roll Racing"`
- `"Rock 'n' Roll"` → `"Rock and Roll"`
- `"Zelda:Link"` → `"Zelda Link"`
- `"Super_Mario_Bros"` → `"Super Mario Bros"`

#### Stage 7: Metadata Stripping

**Patterns (via `stripMetadataBrackets()`):**

- Parentheses: `\s*\([^)]*\)`
- Brackets: `\s*\[[^\]]*\]`
- Braces: `\s*\{[^}]*\}`
- Angle brackets: `\s*<[^>]*>`

Removes region codes, tags, and other metadata from all bracket types:

- `"Game (USA)"` → `"Game"`
- `"Game [!]"` → `"Game"`
- `"Title {Europe}"` → `"Title"`
- `"Sonic <Beta>"` → `"Sonic"`
- `"Title (Rev 1) [b] {En} <Proto>"` → `"Title"`

#### Stage 8: Edition/Version Suffix Stripping

**Patterns (via `stripEditionAndVersionSuffixes()`):**

- Edition suffix: `(?i)\s+(Version|Edition|GOTY\s+Edition|Game\s+of\s+the\s+Year\s+Edition|Deluxe\s+Edition|Special\s+Edition|Definitive\s+Edition|Ultimate\s+Edition)$`
- Version suffix: `\s+v[.]?(?:\d{1,3}(?:[.]\d{1,4})*|[IVX]{1,5})$`

Removes common edition and version suffixes:

- `"Game Special Edition"` → `"Game"`
- `"Title Deluxe Edition"` → `"Title"`
- `"Game Version"` → `"Game"`
- `"Title v1.2"` → `"Title"`
- `"Game v1.2.3"` → `"Game"`
- `"Final Fantasy vVII"` → `"Final Fantasy"`

#### Stage 9: Roman Numeral Conversion

**Patterns (via `convertRomanNumerals()`):**

Applied in order from longest to shortest:

- `\bXIX\b` → `"19"`
- `\bXVIII\b` → `"18"`
- `\bXVII\b` → `"17"`
- `\bXVI\b` → `"16"`
- `\bXV\b` → `"15"`
- `\bXIV\b` → `"14"`
- `\bXIII\b` → `"13"`
- `\bXII\b` → `"12"`
- `\bXI\b` → `"11"`
- `\bIX\b` → `"9"`
- `\bVIII\b` → `"8"`
- `\bVII\b` → `"7"`
- `\bVI\b` → `"6"`
- `\bV\b` → `"5"`
- `\bIV\b` → `"4"`
- `\bIII\b` → `"3"`
- `\bII\b` → `"2"`
- `\sI($|[\s:_\-])` → `" 1$1"`

Converts Roman numerals (II-XIX) to Arabic numbers:

- `"Final Fantasy VII"` → `"Final Fantasy 7"`
- `"Street Fighter II"` → `"Street Fighter 2"`
- `"Final Fantasy XI"` → `"Final Fantasy 11"`
- `"Mega Man X"` → `"Mega Man X"` (X intentionally NOT converted to avoid "Mega Man X" → "Mega Man 10")

**Note:** Order matters - longer numerals must be matched first to avoid partial replacements. X is intentionally excluded to preserve game titles like "Mega Man X" and "MegaRace X".

#### Stage 10: Final Slugification (CJK-Aware)

**Patterns:**

- ASCII-only: `[^a-z0-9]+` → removed
- Unicode-preserving: `[^a-z0-9\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}...]+` → removed

**Process:**

1. Generate both ASCII and Unicode versions of the slug
2. Apply intelligent selection:
   - If original contains CJK characters → use Unicode slug (contains both Latin AND CJK)
   - Otherwise → use ASCII slug (pure Latin)
3. Trim whitespace

**Why use Unicode slug for mixed titles?** The Unicode slug naturally concatenates both Latin and CJK portions, making the title searchable by EITHER part without requiring separate database columns. This handles cases where we can't distinguish between dual-language titles (`"Street Fighter ストリート"`) and translation pairs (`"Street Fighter (USA) ストリートファイター (Japan)"`).

**Examples:**

- Pure CJK: `"ドラゴンクエスト"` → `"ドラゴンクエスト"` (preserved)
- CJK with numeral: `"ファイナルファンタジーVII"` → `"ファイナルファンタジー7"` (CJK + converted numeral)
- Mixed Latin+CJK: `"Street Fighter ストリート"` → `"streetfighterストリート"` (both parts preserved!)
- Pure Latin: `"The Legend of Zelda"` → `"legendofzelda"` (standard behavior)

**Result:** 
- Pure Latin titles: Clean ASCII slugs
- Any title with CJK: Concatenated slug containing both Latin and CJK portions
- User can search by either part and matching strategies handle both cases

### Idempotency Guarantee

The slugification function is **idempotent and deterministic**:

```
SlugifyString(SlugifyString(x)) == SlugifyString(x)
```

Running slugification multiple times produces the same result. This holds true for both Latin and CJK text.

### Complete Examples

#### Example 1: Latin Title with Metadata

```
Input:     "The Legend of Zelda: The Minish Cap (USA) [!]"
Stage 1:   "The Legend of Zelda: The Minish Cap (USA) [!]" (no fullwidth chars)
Stage 2:   "The Legend of Zelda: The Minish Cap (USA) [!]" (unicode normalized)
Stage 3:   "The Legend of Zelda: The Minish Cap (USA) [!]" (no leading numbers)
Stage 4:   "Legend of Zelda Minish Cap (USA) [!]" (split on ":", stripped "The" from both parts)
Stage 5:   "Legend of Zelda Minish Cap (USA) [!]" (no trailing article)
Stage 6:   "Legend of Zelda Minish Cap (USA) [!]" (no symbols/separators to normalize)
Stage 7:   "Legend of Zelda Minish Cap" (removed "(USA) [!]")
Stage 8:   "Legend of Zelda Minish Cap" (no edition suffix)
Stage 9:   "Legend of Zelda Minish Cap" (no Roman numerals)
Stage 10:  "legendofzeldaminishcap" (ASCII slug - no CJK detected)
```

#### Example 2: Pure CJK Title

```
Input:     "ドラゴンクエストVII (Japan)"
Stage 1:   "ドラゴンクエストVII (Japan)" (halfwidth katakana normalized to fullwidth)
Stage 2:   "ドラゴンクエストVII (Japan)" (NFC applied, NFKC skipped for CJK)
Stage 3:   "ドラゴンクエストVII (Japan)" (no leading numbers)
Stage 4:   "ドラゴンクエストVII (Japan)" (no secondary title)
Stage 5:   "ドラゴンクエストVII (Japan)" (no trailing article)
Stage 6:   "ドラゴンクエストVII (Japan)" (no symbols/separators to normalize)
Stage 7:   "ドラゴンクエストVII" (removed "(Japan)")
Stage 8:   "ドラゴンクエストVII" (no edition suffix)
Stage 9:   "ドラゴンクエスト7" (VII → 7)
Stage 10:  "ドラゴンクエスト7" (Unicode slug - CJK detected)
```

#### Example 3: Mixed Latin + CJK Title

```
Input:     "Street Fighter ストリートファイター (USA)"
Stage 1:   "Street Fighter ストリートファイター (USA)" (width normalized)
Stage 2:   "Street Fighter ストリートファイター (USA)" (NFKC for Latin, NFC for CJK)
Stage 3:   "Street Fighter ストリートファイター (USA)" (no leading numbers)
Stage 4:   "Street Fighter ストリートファイター (USA)" (no secondary title)
Stage 5:   "Street Fighter ストリートファイター (USA)" (no trailing article)
Stage 6:   "Street Fighter ストリートファイター (USA)" (no symbols/separators to normalize)
Stage 7:   "Street Fighter ストリートファイター" (removed "(USA)")
Stage 8:   "Street Fighter ストリートファイター" (no edition suffix)
Stage 9:   "Street Fighter ストリートファイター" (no Roman numerals)
Stage 10:  "streetfighterストリートファイター" (Unicode slug - contains both Latin + CJK!)
```

---

## Resolution Workflow

**Purpose:** Match user queries against the indexed media database using progressive fallback strategies for robust fuzzy matching.

**Entry Point:** `pkg/zapscript/slugs.go` → `cmdSlug()`

**Input:** System ID + user-provided title (e.g., `"nes/Super Mario Bros"`)

**Output:** Best matching `database.SearchResultWithCursor` (media entry to launch)

**Uses:**

- **Slug Normalizer**: `SlugifyString()`, `NormalizeToWords()`
- **Match Utilities**: `GenerateMatchInfo()`, `ScorePrefixCandidate()`, `ScoreTokenMatch()`, `ScoreTokenSetRatio()`, `GenerateProgressiveTrimCandidates()`
- **Fuzzy Matching**: `findFuzzyMatches()` (Jaro-Winkler similarity)

### Resolution Strategies

Resolution attempts multiple fallback strategies in order:

#### Strategy 1: Exact Match

**Database Function:** `SearchMediaBySlug(systemID, slug, tagFilters)`

Direct lookup of the slugified query:

- Query: `"nes/Super Mario Bros"`
- Slug: `"supermariobros"` (via `SlugifyString()`)
- Matches: Database entries with exact slug `"supermariobros"`

#### Strategy 2: Prefix Match with Edition-Aware Ranking

**Database Function:** `SearchMediaBySlugPrefix(systemID, slug, tagFilters)`

Finds all titles starting with the query slug, then ranks by score:

**Word Sequence Validation:**

- Uses `NormalizeToWords()` to convert both query and candidates to word arrays
- For queries with 2+ words, candidates must start with the same word sequence
- `"super mario"` → `["super", "mario"]` matches `"super mario bros"` → `["super", "mario", "bros"]` ✓
- `"super mario"` → `["super", "mario"]` does NOT match `"super metroid"` → `["super", "metroid"]` ✗

**Scoring Algorithm** (`ScorePrefixCandidate` in `pkg/database/matcher/scoring.go`):

```
base_score = 0

// Bonus: Has edition-like suffix
if has_suffix(["se", "specialedition", "remaster", "remastered", "directorscut",
               "ultimate", "gold", "goty", "deluxe", "definitive", "enhanced",
               "cd32", "cdtv", "aga", "missiondisk", "expansion", "addon"]):
    score += 100

// Penalty: Has sequel-like suffix
// Uses NormalizeToWords() to check last word
if last_word in ["2", "3", "4", "5", "6", "7", "8", "9",
                 "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x"]:
    score -= 10
else:
    score += 20  // Bonus for NOT being a sequel

// Penalty: Length difference
score -= abs(len(candidate) - len(query))

return score
```

#### Strategy 3: Token-Based Similarity Matching (Hybrid Approach)

**Functions:**

- `ScoreTokenMatch(query, candidate)` in `pkg/database/matcher/scoring.go`
- `ScoreTokenSetRatio(query, candidate)` in `pkg/database/matcher/scoring.go`

When word sequence validation filters out all prefix candidates, attempts order-independent word matching using **two complementary methods**. The system uses the **maximum score** from both approaches.

**Method 1: Token Match (Weighted Word Matching)**

Algorithm:

1. Convert both titles to word arrays using `NormalizeToWords()`
2. For each query word, find best matching candidate word
3. Remove matched words from pool (prevents double-counting)
4. Apply word-level weights: longer/unique words score higher
   - Base weight: 10.0
   - Longer words (6+ chars): +10 bonus
   - Common words ("of", "the", "and"): -5 penalty
5. Score: `weighted_matched_words / (weighted_query_words + unmatched_candidate_words * 0.4 * 10.0)`

Strengths: Handles word variations ("link" vs "links"), exact word matching

**Method 2: Token Set Ratio (Set-Based Matching)**

Algorithm:

1. Convert both titles to unique word sets (automatic deduplication)
2. Calculate intersection (common words)
3. Calculate query coverage: `intersection / query_words`
4. Calculate candidate coverage: `intersection / candidate_words`
5. Score: `(query_coverage × 0.8 + candidate_coverage × 0.2) × (1 - extra_penalty)`

Strengths: Handles duplicate words, extra metadata, subset queries

**Selection:**

- Both methods score each candidate
- System uses `max(TokenMatch, TokenSetRatio)` as final score
- Minimum threshold: 0.5

**Examples:**

Token Match better:

- `"link awakening zelda"` vs `"Zelda Link's Awakening"`: TokenMatch: 1.00, SetRatio: 0.62
- Reason: TokenMatch handles word variations ("link" → "links")

Token Set Ratio better:

- `"zelda zelda ocarina"` vs `"Legend of Zelda Ocarina of Time"`: TokenMatch: 0.54, SetRatio: 0.81
- Reason: SetRatio deduplicates automatically

Both work well:

- `"mario super world"` vs `"Super Mario World"`: TokenMatch: 1.00, SetRatio: 0.92
- `"super mario bros 3 usa"` vs `"Super Mario Bros 3"`: TokenMatch: 0.67, SetRatio: 0.72

#### Strategy 4: Secondary Title-Dropping Main Title Search

**Function:** `GenerateMatchInfo(title)` in `pkg/database/matcher/scoring.go`

Detects secondary title delimiters and searches for just the main title:

**Secondary Title Delimiters:**

- Colon: `:` (highest priority)
- Dash with spaces: `-` (medium priority)
- Possessive with space: `'s ` (lowest priority, retains `'s` in main title)

**Process:**

1. Split title at first occurrence of highest-priority delimiter
2. Strip leading articles from secondary title using `stripLeadingArticle()`
3. Slugify main title and secondary title portions separately using `SlugifyString()`
4. Exact match search on main title slug only

**Examples:**

- `"Legend of Zelda: Link's Awakening"` → main: `"legendofzelda"`, secondary: `"linksawakening"`
- `"Sonic - The Hedgehog"` → main: `"sonic"`, secondary: `"hedgehog"` (article "The" stripped from secondary)
- `"Sid Meier's Pirates"` → main: `"sidmeiers"` (includes `'s`), secondary: `"pirates"`

#### Strategy 5: Secondary Title-Only Literal Search

For titles with secondary titles, searches using ONLY the secondary title portion:

**Requirements:**

- Must have a secondary title (detected by `GenerateMatchInfo()`)
- Secondary title slug must be ≥4 characters

**Process:**

1. Try exact match on secondary title slug
2. If no match, try prefix match on secondary title slug

**Example:**

- Query: `"Legend of Zelda: Ocarina of Time"`
- Secondary title slug: `"ocarinaoftime"` (extracted by `GenerateMatchInfo()`)
- Searches for games matching just `"ocarinaoftime"`

#### Strategy 6: Jaro-Winkler Fuzzy Matching

**Function:** `findFuzzyMatches()` in `pkg/zapscript/slugs.go`

Handles typos and spelling variations using Jaro-Winkler similarity:

**Requirements:**

- Query slug must be ≥5 characters
- Fetches all slugs for the system

**Algorithm:**

- Pre-filter: Skip candidates with length difference > 2 characters
- Calculate Jaro-Winkler similarity (0.0-1.0) using `edlib.JaroWinklerSimilarity()`
- Minimum similarity threshold: 0.85
- Sort results by similarity (highest first)

**Why Jaro-Winkler:**

- Optimized for short strings (game titles)
- Heavily weights matching prefixes (users typically get start of title correct)
- Naturally handles British/American spelling variations (0.94-0.98 similarity)

**Examples:**

- `"zelad"` → `"zelda"` (similarity: 0.953)
- `"mraio"` → `"mario"` (similarity: 0.940)
- `"colour"` vs `"color"` (similarity: 0.967)
- `"honour"` vs `"honor"` (similarity: 0.967)

#### Strategy 7: Progressive Trim Candidates

**Function:** `GenerateProgressiveTrimCandidates(title)` in `pkg/database/matcher/scoring.go`

Last resort strategy: progressively removes words from the end of the title for overly-verbose queries:

**Pre-Processing:**

1. Strip metadata brackets using `stripMetadataBrackets()` (all bracket types: `()`, `[]`, `{}`, `<>`)
2. Strip edition/version suffixes using `stripEditionAndVersionSuffixes()`
3. Split into words using `strings.Fields()`

**Candidate Generation:**

- Generates up to 10 trim candidates
- Stops when: down to 1 word, slug length < 6 chars, or 10 iterations reached
- Each candidate tried with both exact match and prefix match
- Deduplication prevents redundant queries

**Example:**

```
Input: "Legend of Zelda: Link's Awakening DX (USA)"

Pre-processed: "Legend of Zelda Link's Awakening DX" (metadata stripped)
Words: ["Legend", "of", "Zelda", "Link's", "Awakening", "DX"]

Candidates (up to 10 trims, min 1 word, min 6 chars):
1. "legendofzeldalinksawakeningdx" (exact + prefix)
2. "legendofzeldalinksawakening" (exact + prefix)
3. "legendofzeldalinks" (exact + prefix)
4. "legendofzelda" (exact + prefix)
5. "legendof" (exact + prefix)
6. "legend" (6 chars - stops here)
```

### Tag Filtering

All strategies support tag filtering via `tagFilters []database.TagFilter`:

```go
type TagFilter struct {
    Type  string  // e.g., "region", "lang", "unfinished"
    Value string  // e.g., "USA", "en", "demo"
}
```

**Filter Application:**

- AND logic: All specified tags must be present
- Applied at database query level
- Reduces result set before ranking

### Multi-Result Selection

When multiple results match, `selectBestResult()` in `pkg/zapscript/slugs.go` applies intelligent prioritization:

**Priority 1: User-Specified Tags**

- If tag filters provided via `[tags=...]` syntax, prefer exact tag matches
- Filter down to tagged results only using `filterByTags()`

**Priority 2: Exclude Variants**

Function: `filterOutVariants()` calls `isVariant()`

Removes:

- Demos: `unfinished:demo*`
- Betas: `unfinished:beta*`
- Prototypes: `unfinished:proto*`
- Alphas: `unfinished:alpha*`
- Samples: `unfinished:sample`
- Previews: `unfinished:preview`
- Prereleases: `unfinished:prerelease`
- Hacks: `unlicensed:hack`
- Translations: `unlicensed:translation*` (includes `translation:old`)
- Bootlegs: `unlicensed:bootleg`
- Clones: `unlicensed:clone`
- Bad dumps: `dump:bad`

**Priority 3: Exclude Re-releases**

Function: `filterOutRereleases()` calls `isRerelease()`

Removes:

- Re-releases: `rerelease:*`
- Reboxed: `reboxed:*`

**Priority 4: Preferred Regions**

Function: `filterByPreferredRegions()`

- Match against `config.DefaultRegions()` from user configuration
- Prefer entries tagged with user's regions
- Fallback to untagged entries
- Last resort: other regions

**Priority 5: Preferred Languages**

Function: `filterByPreferredLanguages()`

- Match against `config.DefaultLangs()` from user configuration
- Prefer entries tagged with user's languages
- Fallback to untagged entries
- Last resort: other languages

**Priority 6: Alphabetical by Filename**

Function: `selectAlphabeticallyByFilename()`

- If still multiple results, pick first alphabetically
- Uses `filepath.Base()` for filename comparison
- Sorts using `sort.Slice()` with string comparison

---

## Indexing Workflow

**Purpose:** Scan filesystem and populate the media database with normalized, searchable entries.

**Entry Point:** `pkg/database/mediascanner/` → Scanner orchestration

**Core Functions:**
- `AddMediaPath()` - Database insertion logic
- `GetPathFragments()` - Path parsing and caching

**Input:** File path (e.g., `"/roms/nes/Super Mario Bros (USA) [!].nes"`)

**Output:** Database entries (System, MediaTitle, Media, MediaTags)

**Uses:**

- **Slug Normalizer**: `SlugifyString()` for matchable slugs
- **Title Extractor**: `getTitleFromFilename()` for display names
- **Tag Parser**: `tags.ParseFilenameToCanonicalTags()` for metadata extraction

### Indexing Pipeline

#### Step 1: Extract Path Fragments (`GetPathFragments()`)

**Process:**

1. **Check cache**: PathFragments are cached with key `(path, filenameTagsEnabled)`
2. **Clean path**: `filepath.Clean(path)` unless URI scheme detected (e.g., `kodi://`)
3. **Extract extension**: `filepath.Ext()` → lowercase, skip if contains spaces or is URI
4. **Extract filename**: Remove extension from base path using `strings.CutSuffix()`
5. **Extract title**: Call `getTitleFromFilename()` (Process 4)
6. **Slugify title**: Call `slugs.SlugifyString(title)` (Process 1)
7. **Handle CJK titles**: If slug is empty (legacy behavior), use lowercase filename as fallback. With CJK support, pure CJK slugs are now preserved.
8. **Extract tags**: Call `getTagsFromFileName()` → `tags.ParseFilenameToCanonicalTags()` (if enabled in config)
9. **Cache result**: Store PathFragments for future lookups

**Example 1: Latin Title**

```
Path:     "/roms/nes/Super Mario Bros (USA) [!].nes"
FileName: "Super Mario Bros (USA) [!]"
Title:    "Super Mario Bros"
Slug:     "supermariobros"
Ext:      ".nes"
Tags:     ["region:usa", "dumpinfo:verified"]
```

**Example 2: CJK Title**

```
Path:     "/roms/sfc/ドラゴンクエストVII (Japan).sfc"
FileName: "ドラゴンクエストVII (Japan)"
Title:    "ドラゴンクエストVII"
Slug:     "ドラゴンクエスト7"
Ext:      ".sfc"
Tags:     ["region:jp"]
```

#### Step 2: Extract Tags from Filename (`getTagsFromFileName()`)

**Tag Extraction System:** `pkg/database/tags/filename_parser.go`

- Main function: `ParseFilenameToCanonicalTags()`
- Tag extraction: `extractTags()` state machine
- Special patterns: `extractSpecialPatterns()`

**Bracket Type Support (via state machine):**

1. **Parentheses `()`**: Region, language, year, development status, revisions, versions
2. **Square brackets `[]`**: Dump info (verified, bad, hacked, translation, fixed, cracked)
3. **Braces `{}`**: Treated like parentheses for tag extraction
4. **Angle brackets `<>`**: Treated like parentheses for tag extraction

**Tag Types Extracted:**

1. **Parentheses/braces/angle tags:**

   - Region codes: `(USA)`, `{Europe}`, `<Japan>` → `region:us`, `region:eu`, `region:jp`
   - Languages: `(En)`, `{Fr,De}`, `<Es>` → `lang:en`, `lang:fr`, `lang:de`, `lang:es`
   - Years: `(1997)` → `year:1997` (years 1970-2029 supported)
   - Development status: `(Beta)`, `{Proto}`, `<Sample>` → `unfinished:beta`, `unfinished:proto`, `unfinished:sample`
   - Revisions: `(Rev A)`, `{Rev 1}` → `rev:a`, `rev:1`
   - Versions: `(v1.0)`, `<v1.2.3>` → `rev:1.0`, `rev:1.2.3`

2. **Bracket tags (dump info):**

   - Verified: `[!]` → `dump:verified`
   - Bad dump: `[b]` → `dump:bad`
   - Hacked: `[h]` → `unlicensed:hack`
   - Translation: `[T+Eng]` → `unlicensed:translation`, `lang:en`
   - Fixed: `[f]` → `dump:fixed`
   - Cracked: `[cr]` → `dump:cracked`

3. **Special patterns (extracted first via `extractSpecialPatterns()`):**

   - Multi-disc: `(Disc 1 of 3)` → `media:disc`, `disc:1`, `disctotal:3`
   - Revisions: `(Rev A)` → `rev:a`
   - Versions: `(v1.2)` → `rev:1.2`

4. **Bracketless patterns:**
   - Translation with status: `T+Eng` → `unlicensed:translation`, `lang:en` (current/newer)
   - Translation outdated: `T-Ger` → `unlicensed:translation:old`, `lang:de`
   - Translation generic: `TFre` → `unlicensed:translation`, `lang:fr` (3-letter code required)
   - Translation with version: `T+Spa v2.1.3` → `unlicensed:translation`, `lang:es`, `rev:2.1.3`
   - Standalone version: `v1.0` → `rev:1.0`

**Tag Format:** Canonical tags use `"type:value"` format (e.g., `"region:usa"`, `"lang:en"`)

**Multi-language handling:** `(En,Fr,De)` or `{En,Fr,De}` → multiple `lang:` tags (`lang:en`, `lang:fr`, `lang:de`)

#### Step 3: Insert into Database (`AddMediaPath()`)

**Database Schema:**

```
System (DBID, SystemID, Name)
    ↓ (FK: SystemDBID)
MediaTitle (DBID, Slug, Name, SystemDBID)
    ↓ (FK: MediaTitleDBID)
Media (DBID, Path, MediaTitleDBID, SystemDBID)
    ↓ (FK: MediaDBID)
MediaTag (TagDBID, MediaDBID)
    ↓ (FK: TagDBID)
Tag (DBID, Tag, TypeDBID)
    ↓ (FK: TypeDBID)
TagType (DBID, Type)
```

**Insertion Order:**

1. Insert/find System by SystemID
2. Insert/find MediaTitle by (Slug, SystemDBID)
3. Insert/find Media by (Path, SystemDBID)
4. Insert/find Tags (if filename tags enabled)
5. Link tags to media via MediaTag table

**Constraint Handling:**

- UNIQUE constraint violations are handled gracefully
- Existing entries are looked up and IDs cached
- In-memory maps prevent duplicate insertion attempts

#### Step 4: Tag Seeding (`SeedCanonicalTags()`)

**Pre-seeded Tag Types:**

- `unknown` (fallback for unmapped tags)
- `extension` (file extensions like `.nes`, `.sfc`)
- All canonical tag types from `tags.CanonicalTagDefinitions`:
  - `region` (60+ region codes: us, eu, jp, etc.)
  - `lang` (50+ language codes: en, fr, de, ja, etc.)
  - `year` (years 1970-2029)
  - `dumpinfo`/`dump` (verified, bad, fixed, cracked, etc.)
  - `unfinished` (demo, beta, proto, alpha, sample, preview, prerelease)
  - `unlicensed` (hack, translation, bootleg, clone, aftermarket, pirate)
  - `media` (disc, cart, tape, digital, etc.)
  - `disc` (disc numbers: 1, 2, 3, etc.)
  - `disctotal` (total discs: 2, 3, 4, etc.)
  - `rev` (revision identifiers: a, b, 1, 1.0, 1.2.3, etc.)
  - `rerelease`, `reboxed`, `players`, `perspective`, `special`, and more

**Seeding happens:** During fresh indexing (after truncate) or when indexes are rebuilt

---

## Shared Library: Title Extractor

**Purpose:** Extract clean, human-readable titles from filenames for display purposes. Simple utility used by the indexing workflow.

**Location:** `pkg/database/tags/filename_parser.go` → `ParseTitleFromFilename()`

**Used By:** Indexing Workflow (via `GetPathFragments()`)

**Input:** Filename without extension (e.g., `"Super Mario Bros (USA) [!]"`)

**Output:** Human-readable title (e.g., `"Super Mario Bros"`)

### Extraction Method

**Implementation:**

```go
func getTitleFromFilename(filename string) string {
    r := helpers.CachedMustCompile(`^([^(\[{<]*)`)
    title := r.FindString(filename)
    return strings.TrimSpace(title)
}
```

**Regex Pattern:** `^([^(\[{<]*)` (updated from `^([^(\[]*)` to include `{}` and `<>`)

**Process:**

1. Match everything from start of string until first `(`, `[`, `{`, or `<`
2. Extract matched portion using `FindString()`
3. Trim whitespace using `strings.TrimSpace()`

**Examples:**

- `"Super Mario Bros (USA) [!]"` → `"Super Mario Bros"`
- `"Legend of Zelda - Link's Awakening (v1.0)"` → `"Legend of Zelda - Link's Awakening"`
- `"Final Fantasy VII (Disc 1 of 3)"` → `"Final Fantasy VII"`
- `"Game Title {Europe}"` → `"Game Title"`
- `"Sonic <Beta>"` → `"Sonic"`
- `"Final Fantasy (USA)[!]{En}<Proto>"` → `"Final Fantasy"`
- `"Sonic & Knuckles"` → `"Sonic & Knuckles"` (no changes - no metadata)

**Storage:**

- Title stored in `MediaTitle.Name` field
- Used for display in UI, API responses
- NOT used for slug matching (slug is normalized separately)

---

## Shared Functions Summary

### Core Shared Functions

| Function                              | Location                               | Process 1 | Process 2 | Process 3 | Process 4 |
| ------------------------------------- | -------------------------------------- | --------- | --------- | --------- | --------- |
| `SlugifyString()`                     | `pkg/database/slugs/slugify.go`        | ✅ Core   | ✅ Used   | ✅ Used   | ❌        |
| `NormalizeToWords()`                  | `pkg/database/slugs/slugify.go`        | ✅ Helper | ✅ Used   | ❌        | ❌        |
| `stripLeadingArticle()`               | `pkg/database/slugs/slugify.go`        | ✅ Helper | ❌        | ❌        | ❌        |
| `stripMetadataBrackets()`             | `pkg/database/slugs/slugify.go`        | ✅ Helper | ✅ Used   | ❌        | ❌        |
| `stripEditionAndVersionSuffixes()`    | `pkg/database/slugs/slugify.go`        | ✅ Helper | ✅ Used   | ❌        | ❌        |
| `normalizeConjunctions()`             | `pkg/database/slugs/slugify.go`        | ✅ Helper | ❌        | ❌        | ❌        |
| `convertRomanNumerals()`              | `pkg/database/slugs/slugify.go`        | ✅ Helper | ❌        | ❌        | ❌        |
| `normalizeSeparators()`               | `pkg/database/slugs/slugify.go`        | ✅ Helper | ❌        | ❌        | ❌        |
| `GenerateMatchInfo()`                 | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `GenerateProgressiveTrimCandidates()` | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `ScorePrefixCandidate()`              | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `ScoreTokenMatch()`                   | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `ScoreTokenSetRatio()`                | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `StartsWithWordSequence()`            | `pkg/database/matcher/scoring.go`              | ❌        | ✅ Used   | ❌        | ❌        |
| `FindFuzzyMatches()`                  | `pkg/database/matcher/fuzzy.go`                | ❌        | ✅ Used   | ❌        | ❌        |
| `ParseTitleFromFilename()`            | `pkg/database/tags/filename_parser.go`         | ❌        | ❌        | ✅ Used   | ✅ Core   |
| `getTagsFromFileName()`               | `pkg/database/mediascanner/indexing_pipeline.go` | ❌        | ❌        | ✅ Used   | ❌        |
| `tags.ParseFilenameToCanonicalTags()` | `pkg/database/tags/filename_parser.go` | ❌        | ❌        | ✅ Used   | ❌        |
| `tags.extractTags()`                  | `pkg/database/tags/filename_parser.go` | ❌        | ❌        | ✅ Used   | ❌        |
| `tags.extractSpecialPatterns()`       | `pkg/database/tags/filename_parser.go` | ❌        | ❌        | ✅ Used   | ❌        |

### Shared Regex Patterns

**From Process 1 (slugify.go):**

- `editionSuffixRegex` - Edition suffix stripping, used by `stripEditionAndVersionSuffixes()`
- `versionSuffixRegex` - Version suffix stripping (`v1.2`, `vIII`), used by `stripEditionAndVersionSuffixes()`
- `leadingNumPrefixRegex` - Leading number prefix stripping (`1.`, `01 -`)
- `parenthesesRegex` - Parentheses removal, used by `stripMetadataBrackets()`
- `bracketsRegex` - Bracket removal, used by `stripMetadataBrackets()`
- `bracesRegex` - Brace removal, used by `stripMetadataBrackets()`
- `angleBracketsRegex` - Angle bracket removal, used by `stripMetadataBrackets()`
- `separatorsRegex` - Separator normalization (`:`, `_`, `-` to space), used by `normalizeSeparators()`
- `nonAlphanumRegex` - Final slugification cleanup
- `trailingArticleRegex` - Trailing article removal (`, The`)
- `plusRegex`, `nWithApostrophesRegex`, etc. - Conjunction normalization patterns, used by `normalizeConjunctions()`
- `romanNumeralI` - Roman numeral I pattern (special case: suffix ` I`)
- `romanNumeralPatterns` - Roman numeral patterns (II-XIX), used by `convertRomanNumerals()`
- `romanNumeralReplacements` - Roman numeral to Arabic mappings

**From Process 2 (matcher/scoring.go):**

- Uses shared regex from Process 1 via utility functions

**From Process 4 (tags/filename_parser.go):**

- Title extraction: `^([^(\[{<]*)` - Matches until first bracket of any type

**From Tag Extraction (filename_parser.go):**

- Disc pattern: `(?i)\(Disc\s+(\d+)\s+of\s+(\d+)\)`
- Revision pattern: `(?i)\(Rev[\s-]([A-Z0-9]+)\)`
- Version pattern: `(?i)\(v(\d+(?:\.\d+)*)\)`
- Translation pattern: `(^|\s)(T)([+-]?)([A-Za-z]{2,3})(?:\s+v(\d+(?:\.\d+)*))?(?:\s|[.]|$)`
- Standalone version pattern: `\bv(\d+(?:\.\d+)*)\b`
- State machine for bracket extraction (supports `()`, `[]`, `{}`, `<>`)

### Data Flow Between Processes

```
User Input (NFC tag, API)
    ↓
[Process 1: Normalize]
    ↓
Normalized Slug
    ↓
[Process 2: Resolve]
    ↓
Match Against DB ← [Process 3: Index] ← Filesystem Scan
                        ↑                        ↓
                    [Process 4: Extract]   Database Entry
                                                ↓
                                          MediaTitle.Name (for display)
                                          MediaTitle.Slug (for matching)
```

---

## Component Responsibilities

### Slug Normalizer Library

**What It Does:**

- Width normalization (fullwidth/halfwidth conversion)
- Unicode normalization (script-aware: NFKC for Latin, NFC for CJK)
- Diacritic removal (Latin only, preserves CJK marks)
- Article stripping (leading, trailing, secondary)
- Symbol and separator normalization
- Metadata removal (parentheses, brackets)
- Edition suffix stripping
- Roman numeral conversion
- Final slug generation (concatenates Latin + CJK when both present)

**Used By:** Both Resolution and Indexing workflows

### Resolution Workflow

**What It Does:**

- Parses `SystemID/GameName` format from user input
- Normalizes user queries via Slug Normalizer
- Executes 6-strategy progressive fallback cascade
- Scores and ranks multiple matches
- Applies tag filters
- Selects best result based on user preferences

**Does NOT:** Modify the database or extract tags from filenames

### Indexing Workflow

**What It Does:**

- Scans filesystem for media files
- Extracts human-readable titles via Title Extractor
- Generates normalized slugs via Slug Normalizer
- Extracts metadata tags via Tag Parser
- Inserts/updates database entries (System, MediaTitle, Media, Tags)
- Handles UNIQUE constraint violations gracefully
- Caches path fragments for performance

**Does NOT:** Match user queries (that's the Resolution Workflow)

### Title Extractor Library

**What It Does:**

- Extracts clean titles from filenames (strips metadata brackets)
- Provides human-readable strings for UI display

**Used By:** Indexing Workflow only

### Tag Parser Library

**What It Does:**

- Parses No-Intro/TOSEC-style filename metadata
- Extracts region, language, version, dump status tags
- Converts to canonical tag format

**Used By:** Indexing Workflow only

---

## CJK Support

**Overview:**

The slug system supports **concatenated slug generation** for CJK (Chinese, Japanese, Korean) titles, preserving both Latin and CJK portions when both are present.

**Key Features:**

1. **Pure CJK titles preserved**: `"ドラゴンクエスト"` → `"ドラゴンクエスト"` (not stripped)
2. **Mixed Latin+CJK concatenated**: `"Street Fighter ストリート"` → `"streetfighterストリート"` (both parts kept!)
3. **Width normalization**: Fullwidth ASCII → halfwidth, halfwidth katakana → fullwidth
4. **Script-aware Unicode normalization**:
   - Latin: NFKC + diacritic removal (traditional behavior)
   - CJK: NFC only (prevents katakana corruption)
5. **Searchable by either part**: Resolution strategies handle both Latin and CJK queries

**Why Concatenation?**

When we can't distinguish between dual-language titles (`"Street Fighter ストリート"`) and translation pairs in filenames, concatenating both parts ensures the title remains searchable by EITHER the Latin OR CJK portion. The Resolution Workflow's strategies (prefix matching, fuzzy matching) handle both cases naturally.

**Implementation Details:**

- Width normalization (`width.Fold`) added as Stage 0
- CJK detection regex: `[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}...]`
- Script detection determines NFKC (Latin) vs NFC (CJK) normalization path
- When CJK detected: use Unicode slug (automatically contains both Latin + CJK)
- When no CJK: use ASCII-only slug

**Examples:**

| Input                              | Output                              | Reasoning                                        |
| ---------------------------------- | ----------------------------------- | ------------------------------------------------ |
| `"ドラゴンクエストVII"`            | `"ドラゴンクエスト7"`               | Pure CJK preserved, Roman numeral converted      |
| `"Street Fighter ストリート"`      | `"streetfighterストリート"`         | Mixed → both parts concatenated, searchable both |
| `"ファイナルファンタジー (Japan)"` | `"ファイナルファンタジー"`          | Pure CJK, metadata stripped                      |
| `"Ａｂｃ123ＤＥＦ"`                | `"abc123def"`                       | Fullwidth ASCII normalized to halfwidth          |
| `"ｳｴｯｼﾞ"`                          | `"ウエッジ"`                        | Halfwidth katakana normalized to fullwidth       |
| `"Super Mario スーパーマリオ"`     | `"supermarioスーパーマリオ"`        | Searchable by "supermario" OR "スーパーマリオ"  |

## Implementation Notes

**For Developers:**

1. **Slug Normalizer** is the single source of truth - both workflows use identical normalization
2. **Resolution Workflow** owns all database querying and matching strategies
3. **Indexing Workflow** is responsible for all database writes
4. **Title Extractor** is a simple utility (5 lines) used only during indexing
5. Shared libraries are in `pkg/database/slugs/` for slug operations and `pkg/database/tags/` for tag operations
6. All normalization uses deterministic, idempotent operations
7. Cache slugified values - slugification is deterministic and expensive (regex-heavy)
8. Database indexes on slugs are critical for Resolution Workflow performance
9. Test edge cases: unicode, possessives, Roman numerals, special characters, **CJK text**, **mixed-language titles**
10. Magic numbers are now named constants - see `pkg/database/matcher/scoring.go` and `pkg/zapscript/slugs.go`
