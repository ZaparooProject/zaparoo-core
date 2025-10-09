# Slug System Reference

This document describes the slug normalization and resolution system used by Zaparoo Core for matching game titles across different platforms and naming conventions.

End users and clients should not need to understand the internal workings of this system, but it is documented here for transparency and for developers who wish to implement similar matching logic. The system is designed to expect a string in the format `SystemID/GameName`, where `SystemID` is a known platform identifier and `GameName` is the user-provided title to match.

## Slug Normalization

The slug normalization process converts arbitrary game titles into canonical, normalized strings optimized for fuzzy matching. This allows "The Legend of Zelda: Ocarina of Time (USA) [!]" to match "Legend of Zelda - Ocarina of Time" despite different formatting.

### Normalization Pipeline

Slugification is performed by `SlugifyString()` in `pkg/database/slugs/slugify.go` through the following stages:

#### Stage 1: Unicode Normalization (Symbol Removal + NFKC + Diacritic Removal)

**Process:**

1. **Symbol Removal** - Removes unicode symbols from categories `So` (Other Symbol: ™, ®, ©, ℠) and `Sc` (Currency Symbol: $, €, ¥). Math symbols like `<`, `>`, `+` are preserved for later removal.
2. **NFKC Normalization** - Normalizes compatibility characters to their canonical forms
3. **NFD + Mark Removal + NFC** - Removes diacritical marks while preserving base characters

Unicode normalization happens first to ensure all subsequent regex patterns and string operations work on predictable, canonical text. This prevents bugs where full-width or compatibility characters bypass pattern matching.

**Why remove symbols first?** NFKC converts some symbols to ASCII letters (™→TM, ℠→SM), which would incorrectly become part of the slug. By removing symbols first, we ensure they're completely stripped rather than converted. Math symbols are preserved because they'll be handled by the final non-alphanumeric cleanup stage.

**Examples:**

- Symbols: `"Sonic™"` → `"Sonic"`, `"Game®"` → `"Game"` (removed, not converted to letters)
- Full-width numbers: `"１. Game"` → `"1. Game"` (enables prefix stripping)
- Full-width delimiters: `"Game：Subtitle"` → `"Game:Subtitle"` (enables delimiter matching)
- Diacritics: `"Pokémon"` → `"Pokemon"`, `"Café"` → `"Cafe"`
- Ligatures: `"ﬁnal"` → `"final"`
- Superscripts: `"Game²"` → `"Game2"`
- Other compatibility chars: `"①"` → `"1"`

#### Stage 2: Leading Number Prefix Stripping

**Pattern:** `^\d+[.\s\-]+`

Removes common list numbering prefixes:

- `"1. Game Title"` → `"Game Title"`
- `"01 - Game Title"` → `"Game Title"`
- `"42. Answer"` → `"Answer"`

#### Stage 3: Secondary Title Decomposition and Article Stripping

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
2. Strip leading articles ("The", "A", "An") from both main and secondary titles independently
3. Recombine with single space

**Examples:**

- `"Legend of Zelda: The Minish Cap"` → `"Legend of Zelda Minish Cap"` (secondary article stripped)
- `"Disney's The Lion King"` → `"Disney's Lion King"` (`'s ` used as delimiter, secondary article stripped)
- `"Movie - A New Hope"` → `"Movie New Hope"` (`-` used as delimiter, secondary article stripped)
- `"Someone's Something: Time to Die"` → `"Someone's Something Time to Die"` (`:` takes priority over `'s `)
- `"Player's Choice - Final Battle"` → `"Player's Choice Final Battle"` (`-` takes priority over `'s `)

**Note:** This stage ensures that titles with secondary components containing articles are normalized consistently with the resolution system's `GenerateMatchInfo()` function.

#### Stage 4: Trailing Article Normalization

**Pattern:** `,\s*the\s*($|[\s:\-\(\[])` (case-insensitive)

Removes ", The" from the end:

- `"Legend, The"` → `"Legend"`
- `"Mega Man, The"` → `"Mega Man"`

#### Stage 5: Ampersand Normalization

**Pattern:** `&` → `and`

Converts ampersands to the word "and":

- `"Sonic & Knuckles"` → `"Sonic and Knuckles"`
- `"Rock & Roll"` → `"Rock and Roll"`

#### Stage 6: Metadata Stripping

**Patterns:**

- Parentheses: `\s*\([^)]*\)`
- Brackets: `\s*\[[^\]]*\]`

Removes region codes, tags, and other metadata:

- `"Game (USA)"` → `"Game"`
- `"Game [!]"` → `"Game"`
- `"Title (Rev 1) [b]"` → `"Title"`

#### Stage 7: Edition/Version Suffix Stripping

**Pattern:** `(?i)\s+(Version|Edition|GOTY\s+Edition|Game\s+of\s+the\s+Year\s+Edition|Deluxe\s+Edition|Special\s+Edition|Definitive\s+Edition|Ultimate\s+Edition)$`

Removes common edition suffixes:

- `"Game Special Edition"` → `"Game"`
- `"Title Deluxe Edition"` → `"Title"`
- `"Game Version"` → `"Game"`

#### Stage 8: Separator Normalization

**Pattern:** `[:_\-]+` → ` ` (space)

Converts remaining separators to spaces:

- `"Zelda:Link"` → `"Zelda Link"`
- `"Super_Mario_Bros"` → `"Super Mario Bros"`
- `"Game-Title-Here"` → `"Game Title Here"`

**Note:** Secondary title delimiters (`:`, `-`, `'s `) are already processed in Stage 2.

#### Stage 9: Roman Numeral Conversion

**Patterns (applied in order):**

- `\bIX\b` → `"9"`
- `\bVIII\b` → `"8"`
- `\bVII\b` → `"7"`
- `\bVI\b` → `"6"`
- `\bIV\b` → `"4"`
- `\bV\b` → `"5"`
- `\bIII\b` → `"3"`
- `\bII\b` → `"2"`
- `\sI($|[\s:_\-])` → `" 1$1"`

Converts Roman numerals to Arabic numbers:

- `"Final Fantasy VII"` → `"Final Fantasy 7"`
- `"Street Fighter II"` → `"Street Fighter 2"`
- `"Mega Man X"` → `"Mega Man 10"` (X = 10)

**Note:** Order matters - longer numerals must be matched first to avoid partial replacements.

#### Stage 10: Final Slugification

**Pattern:** `[^a-z0-9]+` → removed

Final cleanup:

1. Convert to lowercase
2. Remove all non-alphanumeric characters
3. Trim whitespace

**Result:** `"legendofzelda7"`, `"streetfighter2"`, etc.

### Idempotency Guarantee

The slugification function is **idempotent and deterministic**:

```
SlugifyString(SlugifyString(x)) == SlugifyString(x)
```

Running slugification multiple times produces the same result.

### Complete Example

```
Input:    "The Legend of Zelda: The Minish Cap (USA) [!]"
Stage 1:  "The Legend of Zelda: The Minish Cap (USA) [!]" (no leading numbers)
Stage 2:  "Legend of Zelda Minish Cap (USA) [!]" (split on ":", stripped "The" from both parts)
Stage 3:  "Legend of Zelda Minish Cap (USA) [!]" (no trailing article)
Stage 4:  "Legend of Zelda Minish Cap (USA) [!]" (no diacritics)
Stage 5:  "Legend of Zelda Minish Cap (USA) [!]" (no ampersands)
Stage 6:  "Legend of Zelda Minish Cap" (removed "(USA) [!]")
Stage 7:  "Legend of Zelda Minish Cap" (no edition suffix)
Stage 8:  "Legend of Zelda Minish Cap" (no remaining separators)
Stage 9:  "Legend of Zelda Minish Cap" (no Roman numerals)
Stage 10: "legendofzeldaminishcap"
```

---

## Slug Resolution

The slug resolution process attempts to match a user-provided game title against the media database using progressively more aggressive fuzzy matching strategies.

### Resolution Strategies

Resolution is performed by `cmdSlug()` in `pkg/zapscript/slugs.go` through multiple fallback strategies:

#### Strategy 0: Exact Match

**Database Function:** `SearchMediaBySlug(systemID, slug, tagFilters)`

Direct lookup of the slugified query:

- Query: `"nes/Super Mario Bros"`
- Slug: `"supermariobros"`
- Matches: Database entries with exact slug `"supermariobros"`

#### Strategy 1: Prefix Match with Edition-Aware Ranking

**Database Function:** `SearchMediaBySlugPrefix(systemID, slug, tagFilters)`

Finds all titles starting with the query slug, then ranks by score:

**Word Sequence Validation:**

- For queries with 2+ words, candidates must start with the same word sequence
- `"super mario"` matches `"super mario bros"` ✓
- `"super mario"` does NOT match `"super metroid"` ✗

**Scoring Algorithm** (`ScorePrefixCandidate`):

```
base_score = 0

// Bonus: Has edition-like suffix
if has_suffix(["se", "specialedition", "remaster", "remastered", "directorscut",
               "ultimate", "gold", "goty", "deluxe", "definitive", "enhanced",
               "cd32", "cdtv", "aga", "missiondisk", "expansion", "addon"]):
    score += 100

// Penalty: Has sequel-like suffix
if has_suffix(["2", "3", "4", "5", "6", "7", "8", "9",
               "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x"]):
    score -= 10
else:
    score += 20  // Bonus for NOT being a sequel

// Penalty: Length difference
score -= abs(len(candidate) - len(query))

return score
```

**Example:**

- Query: `"sonic"` (slug: `"sonic"`)
- Candidates:
  - `"sonic"` (exact) → score: 20
  - `"sonic2"` → score: -10 (sequel penalty)
  - `"soniccd"` → score: 18 (2 char diff)
  - `"sonicdeluxeedition"` → score: 82 (100 edition bonus - 18 length diff)

Best match: `"sonic"` (highest score)

#### Strategy 2: Secondary Title-Dropping Main Title Search

**Function:** `GenerateMatchInfo(title)`

Detects secondary title delimiters and searches for just the main title:

**Secondary Title Delimiters:**

- Colon: `:`
- Dash with spaces: `-`
- Possessive with space: `'s ` (retains `'s` in main title)

**Process:**

1. Split title at delimiter
2. Strip leading articles from secondary title ("The", "A", "An")
3. Slugify main title and secondary title portions separately
4. Exact match search on main title slug

**Examples:**

- `"Legend of Zelda: Link's Awakening"` → main: `"legendofzelda"`, secondary: `"linksawakening"`
- `"Sonic - The Hedgehog"` → main: `"sonic"`, secondary: `"hedgehog"` (article stripped)
- `"Sid Meier's Pirates"` → main: `"sidmeiers"`, secondary: `"pirates"`
- `"Legend of Zelda: The Minish Cap"` → main: `"legendofzelda"`, secondary: `"minishcap"` (article stripped)

#### Strategy 3: Secondary Title-Only Literal Search

For titles with secondary titles, searches using ONLY the secondary title portion:

**Requirements:**

- Must have a secondary title
- Secondary title slug must be ≥4 characters

**Process:**

1. Try exact match on secondary title slug
2. If no match, try prefix match on secondary title slug

**Example:**

- Query: `"Legend of Zelda: Ocarina of Time"`
- Secondary title slug: `"ocarinaoftime"`
- Searches for games matching just `"ocarinaoftime"`

**Use Case:** Matches games where the main title differs but secondary title is consistent (e.g., different "Legend of Zelda" variants)

#### Strategy 4: Progressive Trim Candidates

**Function:** `GenerateProgressiveTrimCandidates(title)`

Progressively removes words from the end of the title and searches:

**Pre-Processing:**

1. Strip parentheses: `\([^)]*\)`
2. Strip brackets: `\[[^\]]*\]`
3. Strip edition suffixes

**Candidate Generation:**

```
words = ["Super", "Mario", "Bros", "Deluxe", "Edition"]

// After pre-processing: ["Super", "Mario", "Bros", "Deluxe"]
// (Edition already stripped by edition suffix regex)

Candidates:
1. "Super Mario Bros Deluxe" (0 words trimmed) → exact + prefix
2. "Super Mario Bros" (1 word trimmed) → exact + prefix
3. "Super Mario" (2 words trimmed) → exact + prefix
4. "Super" (3 words trimmed) → exact + prefix

// Stops when:
// - Down to 1 word (minimum)
// - Slug length < 6 characters
// - 10 trim iterations reached
```

**For each candidate:**

- Try exact match first (`IsExactMatch = true`)
- Try prefix match second (`IsPrefixMatch = true`)
- Stop on first match found

**Deduplication:**

- Candidates with identical slugs are skipped
- Prevents redundant database queries

**Example:**

```
Input: "Legend of Zelda: Link's Awakening DX (USA)"

Pre-processed: "Legend of Zelda Link's Awakening DX"
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

**Example:**

```
Query: "nes/Super Mario Bros"
Filters: [{Type: "region", Value: "USA"}]
→ Only returns USA region variants
```

### Multi-Result Selection

When multiple results match, `selectBestResult()` applies intelligent prioritization:

**Priority 1: User-Specified Tags**

- If tag filters provided, prefer exact tag matches
- Filter down to tagged results only

**Priority 2: Exclude Variants**

- Remove demos: `unfinished:demo*`
- Remove betas: `unfinished:beta*`
- Remove prototypes: `unfinished:proto*`
- Remove alphas: `unfinished:alpha*`
- Remove hacks: `unlicensed:hack`
- Remove translations: `unlicensed:translation`
- Remove bootlegs: `unlicensed:bootleg`
- Remove clones: `unlicensed:clone`

**Priority 3: Exclude Re-releases**

- Remove re-releases: `rerelease:*`
- Remove reboxed: `reboxed:*`

**Priority 4: Preferred Regions**

- Match against `config.DefaultRegions()`
- Prefer tagged with user's regions
- Fallback to untagged entries
- Last resort: other regions

**Priority 5: Preferred Languages**

- Match against `config.DefaultLangs()`
- Prefer tagged with user's languages
- Fallback to untagged entries
- Last resort: other languages

**Priority 6: Alphabetical by Filename**

- If still multiple results, pick first alphabetically
- Uses `filepath.Base()` for comparison

### Resolution Flow Diagram

```
User Input: "nes/Super Mario Bros"
    ↓
Validate Format (SystemID/GameName)
    ↓
Lookup System Definition
    ↓
Slugify Game Name → "supermariobros"
    ↓
┌─────────────────────────────────────┐
│ Strategy 0: Exact Match             │
│ SearchMediaBySlug("supermariobros") │
└─────────────────────────────────────┘
    ↓ (if 0 results)
┌─────────────────────────────────────────────┐
│ Strategy 1: Prefix Match + Ranking         │
│ SearchMediaBySlugPrefix("supermariobros")  │
│ → Score candidates                          │
│ → Pick highest score                        │
└─────────────────────────────────────────────┘
    ↓ (if 0 results)
┌──────────────────────────────────────────┐
│ Strategy 2: Main Title Only             │
│ GenerateMatchInfo() → "supermario"      │
│ SearchMediaBySlug("supermario")         │
└──────────────────────────────────────────┘
    ↓ (if 0 results)
┌──────────────────────────────────────────┐
│ Strategy 3: Secondary Title Only        │
│ GenerateMatchInfo() → "bros"            │
│ SearchMediaBySlug("bros")               │
│ → SearchMediaBySlugPrefix("bros")       │
└──────────────────────────────────────────┘
    ↓ (if 0 results)
┌────────────────────────────────────────────┐
│ Strategy 4: Progressive Trim              │
│ GenerateProgressiveTrimCandidates()       │
│ For each candidate:                        │
│   → Try exact match                        │
│   → Try prefix match                       │
│   → Return first match                     │
└────────────────────────────────────────────┘
    ↓
┌────────────────────────────────┐
│ Multiple Results?              │
│ → Apply selectBestResult()     │
│ → Tag filtering                │
│ → Exclude variants             │
│ → Exclude re-releases          │
│ → Prefer user regions/langs    │
│ → Alphabetical tiebreaker      │
└────────────────────────────────┘
    ↓
Launch Selected Media
```

### Implementation Notes

**For Developers Implementing Matching:**

1. **Always slugify both sides** - Database entries should be pre-slugified and indexed
2. **Respect strategy order** - More precise matches first, fuzzy matching last
3. **Cache slugified values** - Slugification is deterministic, cache results
4. **Index on slugs** - Create database indexes on slug columns for performance
5. **Consider word boundaries** - Don't match `"super"` to `"supersonic"` mid-word
6. **Pre-process during indexing** - Strip metadata/editions during database population
7. **Validate system IDs first** - Invalid systems should fail before slug processing
8. **Log all fallbacks** - Help users understand which strategy matched
9. **Support tag filters** - Allow users to narrow results by region/language/etc.
10. **Test edge cases** - Special characters, unicode, possessives, Roman numerals

### Common Patterns

**Handling possessives:**

```
"Sid Meier's Pirates" → main: "sidmeiers", secondary: "pirates"
(The 's is retained in main title for slugification)
```

**Handling secondary titles with colons:**

```
"Legend of Zelda: Link's Awakening" → main: "legendofzelda", secondary: "linksawakening"
```

**Handling secondary titles with leading articles:**

```
"Legend of Zelda: The Minish Cap" → main: "legendofzelda", secondary: "minishcap"
"Movie - A New Hope" → main: "movie", secondary: "newhope"
(Articles "The", "A", "An" are stripped from secondary title before slugification)
```

**Handling Roman numerals:**

```
"Final Fantasy VII Remake" → "finalfantasy7remake"
```

**Handling editions:**

```
"Skyrim Special Edition" → "skyrim"
(Edition stripped before final slug)
```

**Handling regions:**

```
"Game (USA) (Rev 1)" → "game"
(Metadata stripped completely)
```
