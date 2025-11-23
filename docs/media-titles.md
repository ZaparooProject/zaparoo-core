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

1. Extracts clean title from filename (strips metadata like region codes)
2. Runs 14-stage normalization to create a slug
3. Extracts tags from filename (region, language, version, etc.)
4. Stores slug + tags in database for fast searching

### 2. Resolution

When launching by title, Zaparoo:

1. Parses `SystemID/GameName` format (e.g., `NES/Super Mario Bros`)
2. Extracts tags from query (if present)
3. Runs same 14-stage normalization to create query slug
4. Tries multiple matching strategies in order until finding a match
5. Applies confidence scoring and filtering to select best result

---

## Slug Normalization

Slug normalization is a **14-stage pipeline** that converts titles into a canonical form. Both indexing and resolution use identical normalization.

**Implementation:** `pkg/database/slugs/slugify.go` → `Slugify(mediaType MediaType, input string)`

### The 14 Stages (Summary)

Each stage processes the string and passes it to the next:

1. **Width Normalization** - Fullwidth → Halfwidth (ASCII), Halfwidth → Fullwidth (CJK)
2. **Punctuation Normalization** - Curly quotes, fancy dashes → standard ASCII
3. **Unicode Normalization** - Remove symbols (™©®), remove diacritics (Pokémon → Pokemon), script-aware processing
4. **Metadata Stripping** - Remove brackets: `(USA) [!]` etc.
5. **Secondary Title Split** - Split on `:`, ` - `, or `'s `, strip articles from both parts: "Zelda: The Minish Cap" → "Zelda Minish Cap"
6. **Trailing Article** - Remove ", The" suffix: "Legend, The" → "Legend"
7. **Symbols & Separators** - `&` → `and`, separators → spaces: "Sonic & Knuckles" → "Sonic and Knuckles"
8. **Edition Stripping** - Remove "Edition", "Version" words
9. **Abbreviation Expansion** - `Bros` → `Brothers`, `vs` → `versus`, `Dr` → `Doctor`
10. **Period Conversion** - All periods → spaces (safe after abbreviations expanded)
11. **Number Words** - `one` → `1`, `two` → `2`, etc. (1-20)
12. **Ordinals** - `2nd` → `2`, `3rd` → `3`
13. **Roman Numerals** - `VII` → `7`, `II` → `2` (also lowercases everything)
14. **Final Slugification** - Remove non-alphanumeric, multi-script aware

**Example:**

```
Input:  "The Legend of Zelda: Ocarina of Time (USA) [!]"
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

Tags provide additional filtering and disambiguation:

### Tag Extraction

Tags are extracted from filenames during indexing:

- **Regions**: `(USA)`, `(Europe)`, `(Japan)` → `region:us`, `region:eu`, `region:jp`
- **Languages**: `(En)`, `(Fr,De)` → `lang:en`, `lang:fr`, `lang:de`
- **Years**: `(1997)` → `year:1997`
- **Dump info**: `[!]` → `dump:verified`, `[b]` → `dump:bad`
- **Development**: `(Beta)`, `(Proto)` → `unfinished:beta`, `unfinished:proto`
- **Revisions**: `(Rev A)` → `rev:a`

### Tag Usage in Queries

Tags can be specified in three ways:

1. **Inline canonical tags**: `NES/Zelda (+region:us) (-unfinished:beta)`
2. **Advanced args**: `NES/Zelda?tags=region:us,-unfinished:beta`
3. **Filename-style**: `NES/Zelda (USA) (1986)` (auto-extracted)

---

## Implementation Notes

### Key Files

- **Normalization**: `pkg/database/slugs/slugify.go`
- **Resolution**: `pkg/zapscript/titles.go`
- **Strategies**: `pkg/zapscript/titles/strategies.go`
- **Selection**: `pkg/zapscript/titles/selection.go`
- **Fuzzy matching**: `pkg/database/matcher/fuzzy.go`
- **Tag parsing**: `pkg/database/tags/filename_parser.go`
