// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package tags

import "strings"

// mapFilenameTagToCanonical converts filename-based tags (No-Intro, TOSEC conventions)
// to canonical GameDataBase-style hierarchical tags.
// Returns a slice of canonical tags as some filename tags map to multiple canonical tags.
func mapFilenameTagToCanonical(tag string) []CanonicalTag {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tags := allTagMappings[tag]
	// Return a copy to avoid modifying the original map values
	if len(tags) == 0 {
		return nil
	}
	result := make([]CanonicalTag, len(tags))
	copy(result, tags)
	return result
}

// allTagMappings is a unified map of all filename tags to canonical tags.
// Keys are normalized (lowercase, spaces→dashes, no periods).
// Using a single map provides O(1) lookup instead of sequential checks across multiple maps.
// Using CanonicalTag structs eliminates runtime string splitting operations.
// Note: Ambiguous tags (ch, tr, bs, hi, st, np) are handled with special logic in filename_parser.go
var allTagMappings = map[string][]CanonicalTag{
	// ============================================================================
	// REGION MAPPINGS
	// ============================================================================
	// Maps filename region tags to canonical region + language tags
	// Note: Many regions auto-add language tags based on typical release patterns
	// (e.g., USA→English, Japan→Japanese). This is intentional for convenience,
	// though individual games may have different language configurations.
	// Multilingual regions (Belgium, Switzerland, Singapore, etc.) do not auto-add
	// language tags as there's no single predominant language.

	// No-Intro style regions (full names)
	"world":       {{Type: TagTypeRegion, Value: TagRegionWorld}},
	"europe":      {{Type: TagTypeRegion, Value: TagRegionEU}},
	"asia":        {{Type: TagTypeRegion, Value: TagRegionAsia}},
	"australia":   {{Type: TagTypeRegion, Value: TagRegionAU}, {Type: TagTypeLang, Value: TagLangEN}},
	"brazil":      {{Type: TagTypeRegion, Value: TagRegionBR}, {Type: TagTypeLang, Value: TagLangPT}},
	"canada":      {{Type: TagTypeRegion, Value: TagRegionCA}, {Type: TagTypeLang, Value: TagLangEN}},
	"china":       {{Type: TagTypeRegion, Value: TagRegionCN}, {Type: TagTypeLang, Value: TagLangZH}},
	"france":      {{Type: TagTypeRegion, Value: TagRegionFR}, {Type: TagTypeLang, Value: TagLangFR}},
	"germany":     {{Type: TagTypeRegion, Value: TagRegionDE}, {Type: TagTypeLang, Value: TagLangDE}},
	"hong-kong":   {{Type: TagTypeRegion, Value: TagRegionHK}}, // Multilingual: Chinese/English
	"italy":       {{Type: TagTypeRegion, Value: TagRegionIT}, {Type: TagTypeLang, Value: TagLangIT}},
	"japan":       {{Type: TagTypeRegion, Value: TagRegionJP}, {Type: TagTypeLang, Value: TagLangJA}},
	"korea":       {{Type: TagTypeRegion, Value: TagRegionKR}, {Type: TagTypeLang, Value: TagLangKO}},
	"netherlands": {{Type: TagTypeRegion, Value: TagRegionNL}, {Type: TagTypeLang, Value: TagLangNL}},
	"spain":       {{Type: TagTypeRegion, Value: TagRegionES}, {Type: TagTypeLang, Value: TagLangES}},
	"sweden":      {{Type: TagTypeRegion, Value: TagRegionSE}, {Type: TagTypeLang, Value: TagLangSV}},
	"usa":         {{Type: TagTypeRegion, Value: TagRegionUS}, {Type: TagTypeLang, Value: TagLangEN}},
	"poland":      {{Type: TagTypeRegion, Value: TagRegionPL}, {Type: TagTypeLang, Value: TagLangPL}},
	"finland":     {{Type: TagTypeRegion, Value: TagRegionFI}, {Type: TagTypeLang, Value: TagLangFI}},
	"denmark":     {{Type: TagTypeRegion, Value: TagRegionDK}, {Type: TagTypeLang, Value: TagLangDA}},
	"portugal":    {{Type: TagTypeRegion, Value: TagRegionPT}, {Type: TagTypeLang, Value: TagLangPT}},
	"norway":      {{Type: TagTypeRegion, Value: TagRegionNO}, {Type: TagTypeLang, Value: TagLangNO}},

	// TOSEC style regions (country codes)
	"ae": {{Type: TagTypeRegion, Value: TagRegionAE}, {Type: TagTypeLang, Value: TagLangAR}}, // United Arab Emirates
	"al": {{Type: TagTypeRegion, Value: TagRegionAL}},
	"as": {{Type: TagTypeRegion, Value: TagRegionAS}},
	"at": {{Type: TagTypeRegion, Value: TagRegionAT}, {Type: TagTypeLang, Value: TagLangDE}}, // Austria
	"au": {{Type: TagTypeRegion, Value: TagRegionAU}, {Type: TagTypeLang, Value: TagLangEN}}, // Australia
	"ba": {{Type: TagTypeRegion, Value: TagRegionBA}},
	"be": {{Type: TagTypeRegion, Value: TagRegionBE}}, // Belgium - multilingual (Dutch/French/German)
	"bg": {{Type: TagTypeRegion, Value: TagRegionBG}, {Type: TagTypeLang, Value: TagLangBG}},
	"br": {{Type: TagTypeRegion, Value: TagRegionBR}, {Type: TagTypeLang, Value: TagLangPT}},
	// Canada - predominantly English in gaming
	"ca": {{Type: TagTypeRegion, Value: TagRegionCA}, {Type: TagTypeLang, Value: TagLangEN}},
	// Switzerland - context-aware: if lang:de present → region:ch; else → lang:zh (see filename_parser.go)
	"ch": {{Type: TagTypeRegion, Value: TagRegionCH}},
	"cl": {{Type: TagTypeRegion, Value: TagRegionCL}, {Type: TagTypeLang, Value: TagLangES}}, // Chile
	"cn": {{Type: TagTypeRegion, Value: TagRegionCN}, {Type: TagTypeLang, Value: TagLangZH}},
	// Serbia, also Czech language
	"cs": {{Type: TagTypeRegion, Value: TagRegionCS}, {Type: TagTypeLang, Value: TagLangCS}},
	"cy": {{Type: TagTypeRegion, Value: TagRegionCY}},
	"cz": {{Type: TagTypeRegion, Value: TagRegionCZ}, {Type: TagTypeLang, Value: TagLangCS}},
	"de": {{Type: TagTypeRegion, Value: TagRegionDE}, {Type: TagTypeLang, Value: TagLangDE}},
	"dk": {{Type: TagTypeRegion, Value: TagRegionDK}, {Type: TagTypeLang, Value: TagLangDA}},
	"ee": {{Type: TagTypeRegion, Value: TagRegionEE}, {Type: TagTypeLang, Value: TagLangET}}, // Estonia
	"eg": {{Type: TagTypeRegion, Value: TagRegionEG}, {Type: TagTypeLang, Value: TagLangAR}}, // Egypt
	"es": {{Type: TagTypeRegion, Value: TagRegionES}, {Type: TagTypeLang, Value: TagLangES}},
	"eu": {{Type: TagTypeRegion, Value: TagRegionEU}}, // Europe - multi-region
	"fi": {{Type: TagTypeRegion, Value: TagRegionFI}, {Type: TagTypeLang, Value: TagLangFI}},
	"fr": {{Type: TagTypeRegion, Value: TagRegionFR}, {Type: TagTypeLang, Value: TagLangFR}},
	"gb": {{Type: TagTypeRegion, Value: TagRegionGB}, {Type: TagTypeLang, Value: TagLangEN}},
	"gr": {{Type: TagTypeRegion, Value: TagRegionGR}, {Type: TagTypeLang, Value: TagLangEL}},
	"hk": {{Type: TagTypeRegion, Value: TagRegionHK}}, // Hong Kong - multilingual (Chinese/English)
	"hr": {{Type: TagTypeRegion, Value: TagRegionHR}, {Type: TagTypeLang, Value: TagLangHR}},
	"hu": {{Type: TagTypeRegion, Value: TagRegionHU}, {Type: TagTypeLang, Value: TagLangHU}},
	"id": {{Type: TagTypeRegion, Value: TagRegionID}},
	"ie": {{Type: TagTypeRegion, Value: TagRegionIE}, {Type: TagTypeLang, Value: TagLangEN}},
	"il": {{Type: TagTypeRegion, Value: TagRegionIL}, {Type: TagTypeLang, Value: TagLangHE}},
	"in": {{Type: TagTypeRegion, Value: TagRegionIN}, {Type: TagTypeLang, Value: TagLangHI}},
	"ir": {{Type: TagTypeRegion, Value: TagRegionIR}, {Type: TagTypeLang, Value: TagLangFA}},
	"is": {{Type: TagTypeRegion, Value: TagRegionIS}, {Type: TagTypeLang, Value: TagLangIS}}, // Iceland
	"it": {{Type: TagTypeRegion, Value: TagRegionIT}, {Type: TagTypeLang, Value: TagLangIT}},
	"jo": {{Type: TagTypeRegion, Value: TagRegionJO}, {Type: TagTypeLang, Value: TagLangAR}}, // Jordan
	"jp": {{Type: TagTypeRegion, Value: TagRegionJP}, {Type: TagTypeLang, Value: TagLangJA}},
	"kr": {{Type: TagTypeRegion, Value: TagRegionKR}, {Type: TagTypeLang, Value: TagLangKO}},
	"lt": {{Type: TagTypeRegion, Value: TagRegionLT}, {Type: TagTypeLang, Value: TagLangLT}},
	"lu": {{Type: TagTypeRegion, Value: TagRegionLU}}, // Luxembourg - multilingual (Luxembourgish/French/German)
	"lv": {{Type: TagTypeRegion, Value: TagRegionLV}, {Type: TagTypeLang, Value: TagLangLV}},
	"mn": {{Type: TagTypeRegion, Value: TagRegionMN}},
	"mx": {{Type: TagTypeRegion, Value: TagRegionMX}, {Type: TagTypeLang, Value: TagLangES}},
	"my": {{Type: TagTypeRegion, Value: TagRegionMY}, {Type: TagTypeLang, Value: TagLangMS}},
	"nl": {{Type: TagTypeRegion, Value: TagRegionNL}, {Type: TagTypeLang, Value: TagLangNL}},
	"no": {{Type: TagTypeRegion, Value: TagRegionNO}, {Type: TagTypeLang, Value: TagLangNO}},
	"np": {{Type: TagTypeRegion, Value: TagRegionNP}},
	"nz": {{Type: TagTypeRegion, Value: TagRegionNZ}, {Type: TagTypeLang, Value: TagLangEN}}, // New Zealand
	"om": {{Type: TagTypeRegion, Value: TagRegionOM}, {Type: TagTypeLang, Value: TagLangAR}}, // Oman
	"pe": {{Type: TagTypeRegion, Value: TagRegionPE}, {Type: TagTypeLang, Value: TagLangES}}, // Peru
	"ph": {{Type: TagTypeRegion, Value: TagRegionPH}},
	"pl": {{Type: TagTypeRegion, Value: TagRegionPL}, {Type: TagTypeLang, Value: TagLangPL}},
	"pt": {{Type: TagTypeRegion, Value: TagRegionPT}, {Type: TagTypeLang, Value: TagLangPT}},
	"qa": {{Type: TagTypeRegion, Value: TagRegionQA}, {Type: TagTypeLang, Value: TagLangAR}}, // Qatar
	"ro": {{Type: TagTypeRegion, Value: TagRegionRO}, {Type: TagTypeLang, Value: TagLangRO}},
	"ru": {{Type: TagTypeRegion, Value: TagRegionRU}, {Type: TagTypeLang, Value: TagLangRU}},
	"se": {{Type: TagTypeRegion, Value: TagRegionSE}, {Type: TagTypeLang, Value: TagLangSV}},
	"sg": {{Type: TagTypeRegion, Value: TagRegionSG}}, // Singapore - multilingual (English/Malay/Chinese/Tamil)
	"si": {{Type: TagTypeRegion, Value: TagRegionSI}, {Type: TagTypeLang, Value: TagLangSL}},
	"sk": {{Type: TagTypeRegion, Value: TagRegionSK}, {Type: TagTypeLang, Value: TagLangSK}},
	"th": {{Type: TagTypeRegion, Value: TagRegionTH}, {Type: TagTypeLang, Value: TagLangTH}},
	// Turkey - context-aware: ()=region/lang, []=translated (see filename_parser.go)
	"tr": {{Type: TagTypeRegion, Value: TagRegionTR}},
	"tw": {{Type: TagTypeRegion, Value: TagRegionTW}, {Type: TagTypeLang, Value: TagLangZH}}, // Taiwan
	"us": {{Type: TagTypeRegion, Value: TagRegionUS}, {Type: TagTypeLang, Value: TagLangEN}},
	"vn": {{Type: TagTypeRegion, Value: TagRegionVN}, {Type: TagTypeLang, Value: TagLangVI}},
	"yu": {{Type: TagTypeRegion, Value: TagRegionYU}},
	"za": {{Type: TagTypeRegion, Value: TagRegionZA}},

	// ============================================================================
	// LANGUAGE MAPPINGS
	// ============================================================================
	// Note: Some language codes overlap with region codes above and are merged there.
	// These are either standalone language codes or ones where the region mapping doesn't auto-add the language.
	// The following are already mapped with regions: cs, da, fi, nl, no, pt, sv
	"ar": {{Type: TagTypeLang, Value: TagLangAR}},
	"bs": {{Type: TagTypeLang, Value: TagLangBS}},
	"da": {{Type: TagTypeLang, Value: TagLangDA}},
	"el": {{Type: TagTypeLang, Value: TagLangEL}},
	"en": {{Type: TagTypeLang, Value: TagLangEN}},
	"eo": {{Type: TagTypeLang, Value: TagLangEO}},
	"fa": {{Type: TagTypeLang, Value: TagLangFA}},
	"ga": {{Type: TagTypeLang, Value: TagLangGA}},
	"gu": {{Type: TagTypeLang, Value: TagLangGU}},
	"he": {{Type: TagTypeLang, Value: TagLangHE}},
	"hi": {{Type: TagTypeLang, Value: TagLangHI}},
	"ja": {{Type: TagTypeLang, Value: TagLangJA}},
	"ko": {{Type: TagTypeLang, Value: TagLangKO}},
	"ms": {{Type: TagTypeLang, Value: TagLangMS}},
	"sl": {{Type: TagTypeLang, Value: TagLangSL}},
	"sq": {{Type: TagTypeLang, Value: TagLangSQ}},
	"sr": {{Type: TagTypeLang, Value: TagLangSR}},
	"sv": {{Type: TagTypeLang, Value: TagLangSV}},
	"ur": {{Type: TagTypeLang, Value: TagLangUR}},
	"vi": {{Type: TagTypeLang, Value: TagLangVI}},
	"yi": {{Type: TagTypeLang, Value: TagLangYI}},
	"zh": {{Type: TagTypeLang, Value: TagLangZH}},
	// Language variants (not multi-language - these are specific dialects/scripts)
	// Traditional Chinese
	"ch-trad": {{Type: TagTypeLang, Value: TagLangZH}, {Type: TagTypeLang, Value: TagLangZHTrad}},
	// Simplified Chinese
	"ch-simple": {{Type: TagTypeLang, Value: TagLangZH}, {Type: TagTypeLang, Value: TagLangZHHans}},
	// Brazilian Portuguese
	"bra": {{Type: TagTypeLang, Value: TagLangPTBR}},

	// ============================================================================
	// YEAR MAPPINGS
	// ============================================================================
	// Note: Exact years (1970-2099) are handled dynamically in extractSpecialPatterns
	// Wildcard years for unknown/approximate dates (TOSEC convention)
	"19xx": {{Type: TagTypeYear, Value: TagYear19XX}}, "197x": {{Type: TagTypeYear, Value: TagYear197X}},
	"198x": {{Type: TagTypeYear, Value: TagYear198X}}, "199x": {{Type: TagTypeYear, Value: TagYear199X}},
	"20xx": {{Type: TagTypeYear, Value: TagYear20XX}}, "200x": {{Type: TagTypeYear, Value: TagYear200X}},
	"201x": {{Type: TagTypeYear, Value: TagYear201X}}, "202x": {{Type: TagTypeYear, Value: TagYear202X}},

	// ============================================================================
	// DEVELOPMENT STATUS MAPPINGS
	// ============================================================================
	"alpha":          {{Type: TagTypeUnfinished, Value: TagUnfinishedAlpha}},
	"beta":           {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta}},
	"beta-1":         {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta1}},
	"beta-2":         {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta2}},
	"beta-3":         {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta3}},
	"beta-4":         {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta4}},
	"beta-5":         {{Type: TagTypeUnfinished, Value: TagUnfinishedBeta5}},
	"preview":        {{Type: TagTypeUnfinished, Value: TagUnfinishedPreview}},
	"pre-release":    {{Type: TagTypeUnfinished, Value: TagUnfinishedPrerelease}},
	"proto":          {{Type: TagTypeUnfinished, Value: TagUnfinishedProto}},
	"proto-1":        {{Type: TagTypeUnfinished, Value: TagUnfinishedProto1}},
	"proto-2":        {{Type: TagTypeUnfinished, Value: TagUnfinishedProto2}},
	"proto-3":        {{Type: TagTypeUnfinished, Value: TagUnfinishedProto3}},
	"proto-4":        {{Type: TagTypeUnfinished, Value: TagUnfinishedProto4}},
	"sample":         {{Type: TagTypeUnfinished, Value: TagUnfinishedSample}},
	"demo":           {{Type: TagTypeUnfinished, Value: TagUnfinishedDemo}},
	"demo-1":         {{Type: TagTypeUnfinished, Value: TagUnfinishedDemo1}},
	"demo-2":         {{Type: TagTypeUnfinished, Value: TagUnfinishedDemo2}},
	"demo-auto":      {{Type: TagTypeUnfinished, Value: TagUnfinishedDemoAuto}},
	"demo-kiosk":     {{Type: TagTypeUnfinished, Value: TagUnfinishedDemoKiosk}},
	"demo-playable":  {{Type: TagTypeUnfinished, Value: TagUnfinishedDemoPlayable}},
	"demo-rolling":   {{Type: TagTypeUnfinished, Value: TagUnfinishedDemoRolling}},
	"demo-slideshow": {{Type: TagTypeUnfinished, Value: TagUnfinishedDemoSlideshow}},
	"debug":          {{Type: TagTypeUnfinished, Value: TagUnfinishedDebug}},
	"competition":    {{Type: TagTypeUnfinished, Value: TagUnfinishedCompetition}},

	// ============================================================================
	// VERSION/REVISION MAPPINGS
	// ============================================================================
	// Note: Dotted versions (v1.0, v1.2.3) are handled dynamically in extractSpecialPatterns
	"rev":   {{Type: TagTypeRev, Value: TagRev1}},
	"rev-1": {{Type: TagTypeRev, Value: TagRev1}},
	"rev-2": {{Type: TagTypeRev, Value: TagRev2}},
	"rev-3": {{Type: TagTypeRev, Value: TagRev3}},
	"rev-4": {{Type: TagTypeRev, Value: TagRev4}},
	"rev-5": {{Type: TagTypeRev, Value: TagRev5}},
	"rev-a": {{Type: TagTypeRev, Value: TagRevA}},
	"rev-b": {{Type: TagTypeRev, Value: TagRevB}},
	"rev-c": {{Type: TagTypeRev, Value: TagRevC}},
	"rev-d": {{Type: TagTypeRev, Value: TagRevD}},
	"rev-e": {{Type: TagTypeRev, Value: TagRevE}},
	"rev-g": {{Type: TagTypeRev, Value: TagRevG}},
	"v":     {{Type: TagTypeRev, Value: TagRev1}},
	"v1":    {{Type: TagTypeRev, Value: TagRev1}},
	"v2":    {{Type: TagTypeRev, Value: TagRev2}},
	"v3":    {{Type: TagTypeRev, Value: TagRev3}},
	"v4":    {{Type: TagTypeRev, Value: TagRev4}},
	"v5":    {{Type: TagTypeRev, Value: TagRev5}},
	// Program revisions (NES-specific)
	"prg":  {{Type: TagTypeRev, Value: TagRevPRG}},
	"prg0": {{Type: TagTypeRev, Value: TagRevPRG0}},
	"prg1": {{Type: TagTypeRev, Value: TagRevPRG1}},
	"prg2": {{Type: TagTypeRev, Value: TagRevPRG2}},
	"prg3": {{Type: TagTypeRev, Value: TagRevPRG3}},
	"unl":  {{Type: TagTypeUnlicensed, Value: TagUnlicensed}}, // No-Intro unlicensed flag

	// ============================================================================
	// UNLICENSED/PIRATE MAPPINGS
	// ============================================================================
	"pirate": {{Type: TagTypeUnlicensed, Value: TagUnlicensedPirate}}, // Pirate/unauthorized release

	// ============================================================================
	// ALTERNATE VERSION MAPPINGS
	// ============================================================================
	"alt": {{Type: TagTypeAlt, Value: TagAlt}}, // Generic alternate version

	// ============================================================================
	// VIDEO FORMAT MAPPINGS (TOSEC)
	// ============================================================================
	"cga":      {{Type: TagTypeVideo, Value: TagVideoCGA}},
	"ega":      {{Type: TagTypeVideo, Value: TagVideoEGA}},
	"hgc":      {{Type: TagTypeVideo, Value: TagVideoHGC}},
	"mcga":     {{Type: TagTypeVideo, Value: TagVideoMCGA}},
	"mda":      {{Type: TagTypeVideo, Value: TagVideoMDA}},
	"ntsc":     {{Type: TagTypeVideo, Value: TagVideoNTSC}},
	"ntsc-pal": {{Type: TagTypeVideo, Value: TagVideoNTSCPAL}},
	"pal":      {{Type: TagTypeVideo, Value: TagVideoPAL}},
	"pal-60":   {{Type: TagTypeVideo, Value: TagVideoPAL60}},
	"pal-ntsc": {{Type: TagTypeVideo, Value: TagVideoPALNTSC}},
	"svga":     {{Type: TagTypeVideo, Value: TagVideoSVGA}},
	"vga":      {{Type: TagTypeVideo, Value: TagVideoVGA}},
	"xga":      {{Type: TagTypeVideo, Value: TagVideoXGA}},

	// ============================================================================
	// COPYRIGHT MAPPINGS (TOSEC)
	// ============================================================================
	"cw":   {{Type: TagTypeCopyright, Value: TagCopyrightCW}},  // Cardware
	"cw-r": {{Type: TagTypeCopyright, Value: TagCopyrightCWR}}, // Cardware (registered)
	"fw":   {{Type: TagTypeCopyright, Value: TagCopyrightFW}},  // Freeware
	"gw":   {{Type: TagTypeCopyright, Value: TagCopyrightGW}},  // Giftware
	"gw-r": {{Type: TagTypeCopyright, Value: TagCopyrightGWR}}, // Giftware (registered)
	"lw":   {{Type: TagTypeCopyright, Value: TagCopyrightLW}},  // Linkware
	"pd":   {{Type: TagTypeCopyright, Value: TagCopyrightPD}},  // Public domain
	"sw":   {{Type: TagTypeCopyright, Value: TagCopyrightSW}},  // Shareware
	"sw-r": {{Type: TagTypeCopyright, Value: TagCopyrightSWR}}, // Shareware (registered)

	// ============================================================================
	// DUMP INFO MAPPINGS (TOSEC)
	// ============================================================================
	// Note: "tr" (translated) is merged with Turkey region mapping above
	// Note: "v" (virus) is merged with version mapping below
	"cr": {{Type: TagTypeDump, Value: TagDumpCracked}},   // Cracked
	"f":  {{Type: TagTypeDump, Value: TagDumpFixed}},     // Fixed
	"h":  {{Type: TagTypeDump, Value: TagDumpHacked}},    // Hacked
	"m":  {{Type: TagTypeDump, Value: TagDumpModified}},  // Modified
	"p":  {{Type: TagTypeDump, Value: TagDumpPirated}},   // Pirated
	"t":  {{Type: TagTypeDump, Value: TagDumpTrained}},   // Trained
	"o":  {{Type: TagTypeDump, Value: TagDumpOverdump}},  // Overdump
	"u":  {{Type: TagTypeDump, Value: TagDumpUnderdump}}, // Underdump
	"b":  {{Type: TagTypeDump, Value: TagDumpBad}},       // Bad dump
	"a":  {{Type: TagTypeDump, Value: TagDumpAlternate}}, // Alternate
	"!":  {{Type: TagTypeDump, Value: TagDumpVerified}},  // Verified good dump
	"!p": {{Type: TagTypeDump, Value: TagDumpPending}},   // Pending verification (GoodTools)
	// Bad checksum but good dump (GoodTools Genesis)
	"c": {{Type: TagTypeDump, Value: TagDumpChecksumBad}},
	// Unknown checksum status (GoodTools Genesis)
	"x":    {{Type: TagTypeDump, Value: TagDumpChecksumUnknown}},
	"bios": {{Type: TagTypeDump, Value: TagDumpBIOS}}, // BIOS dump
	// Far East Copier hack
	"hffe": {{Type: TagTypeDump, Value: TagDumpHacked}, {Type: TagTypeDump, Value: TagDumpHackedFFE}},
	// "hi" is ambiguous: Hindi language vs hacked intro - handled in filename_parser.go
	// Hacked intro removed
	"hir": {{Type: TagTypeDump, Value: TagDumpHacked}, {Type: TagTypeDump, Value: TagDumpHackedIntroRemov}},
	// Intro removed with 00 fill
	"hir00": {{Type: TagTypeDump, Value: TagDumpHacked}, {Type: TagTypeDump, Value: TagDumpHackedIntroRemov}},
	// Intro removed with ff fill
	"hirff": {{Type: TagTypeDump, Value: TagDumpHacked}, {Type: TagTypeDump, Value: TagDumpHackedIntroRemov}},

	// ============================================================================
	// TOSEC SYSTEM-SPECIFIC MAPPINGS
	// ============================================================================
	// Apple II systems
	"ii+": {{Type: TagTypeCompatibility, Value: TagCompatibilityApple2Plus}},
	"iie": {{Type: TagTypeCompatibility, Value: TagCompatibilityApple2E}},
	// Memory requirements
	"16k":      {{Type: TagTypeCompatibility, Value: TagCompatibilityMemory16K}},
	"128k":     {{Type: TagTypeCompatibility, Value: TagCompatibilityMemory128K}},
	"48k-128k": {{Type: TagTypeCompatibility, Value: TagCompatibilityMemory48K128K}},
	// Amiga systems
	"+2":                     {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaPlus2}},
	"+2a":                    {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaPlus2A}},
	"+3":                     {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaPlus3}},
	"130xe":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAtari130XE}},
	"a1000":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA1000}},
	"a1200":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA1200}},
	"a1200-a4000":            {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA1200A4000}},
	"a2000":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA2000}},
	"a2000-a3000":            {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA2000A3000}},
	"a2024":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA2024}},
	"a2500-a3000ux":          {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA2500A3000UX}},
	"a3000":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA3000}},
	"a4000":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA4000}},
	"a4000t":                 {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA4000T}},
	"a500":                   {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500}},
	"a500+":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500Plus}},
	"a500-a1000-a2000":       {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A1000A2000}},
	"a500-a1000-a2000-cdtv":  {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A1000A2000CDTV}},
	"a500-a1200":             {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A1200}},
	"a500-a1200-a2000-a4000": {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A1200A2000A4000}},
	"a500-a2000":             {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A2000}},
	"a500-a600-a2000":        {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA500A600A2000}},
	"a570":                   {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA570}},
	"a600":                   {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA600}},
	"a600hd":                 {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaA600HD}},
	"aga":                    {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaAGA}},
	"aga-cd32":               {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaAGACD32}},
	"aladdin-deck-enhancer":  {{Type: TagTypeAddon, Value: TagAddonLockonDeckenhancer}},
	"cd32":                   {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaCD32}},
	"cdtv":                   {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaCDTV}},
	"computrainer":           {{Type: TagTypeAddon, Value: TagAddonControllerComputrainer}},
	"doctor-pc-jr":           {{Type: TagTypeCompatibility, Value: TagCompatibilityIBMPCDoctorPCJr}},
	"ecs":                    {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaECS}},
	"ecs-aga":                {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaECSAGA}},
	"executive":              {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariExecutive}},
	"mega-st":                {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariMegaST}},
	"mega-ste":               {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariMegaSTE}},
	"ocs":                    {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaOCS}},
	"ocs-aga":                {{Type: TagTypeCompatibility, Value: TagCompatibilityAmigaOCSAGA}},
	"orch80":                 {{Type: TagTypeCompatibility, Value: TagCompatibilityMiscOrch80}},
	"osbourne-1":             {{Type: TagTypeCompatibility, Value: TagCompatibilityOsbourneOsbourne1}},
	"piano90":                {{Type: TagTypeCompatibility, Value: TagCompatibilityMiscPiano90}},
	"playchoice-10":          {{Type: TagTypeCompatibility, Value: TagCompatibilityNintendoPlaychoice10}},
	"plus4":                  {{Type: TagTypeCompatibility, Value: TagCompatibilityCommodorePlus4}},
	"primo-a":                {{Type: TagTypeCompatibility, Value: TagCompatibilityPrimoPrimoA}},
	"primo-a64":              {{Type: TagTypeCompatibility, Value: TagCompatibilityPrimoPrimoA64}},
	"primo-b":                {{Type: TagTypeCompatibility, Value: TagCompatibilityPrimoPrimoB}},
	"primo-b64":              {{Type: TagTypeCompatibility, Value: TagCompatibilityPrimoPrimoB64}},
	"pro-primo":              {{Type: TagTypeCompatibility, Value: TagCompatibilityPrimoProprimo}},
	"st":                     {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariST}},
	"ste":                    {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariSTE}},
	"ste-falcon":             {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariSTEFalcon}},
	"tt":                     {{Type: TagTypeCompatibility, Value: TagCompatibilityAtariTT}},
	"turbo-r-gt":             {{Type: TagTypeCompatibility, Value: TagCompatibilityMSXTurboRGT}},
	"turbo-r-st":             {{Type: TagTypeCompatibility, Value: TagCompatibilityMSXTurboRST}},
	"vs-dualsystem":          {{Type: TagTypeCompatibility, Value: TagCompatibilityNintendoVSDualsystem}},
	"vs-unisystem":           {{Type: TagTypeCompatibility, Value: TagCompatibilityNintendoVSUnisystem}},

	// ============================================================================
	// ARCADE AND SPECIAL HARDWARE MAPPINGS
	// ============================================================================
	"vs":       {{Type: TagTypeArcadeBoard, Value: TagArcadeBoardNintendoVS}},   // VS System arcade
	"nss":      {{Type: TagTypeArcadeBoard, Value: TagArcadeBoardNintendoNSS}},  // Nintendo Super System arcade
	"megaplay": {{Type: TagTypeArcadeBoard, Value: TagArcadeBoardSegaMegaplay}}, // MegaPlay arcade
	"mp":       {{Type: TagTypeArcadeBoard, Value: TagArcadeBoardSegaMegaplay}}, // MegaPlay (short form)
	// "bs" is ambiguous: Bosnian language vs Satellaview - handled in filename_parser.go
	"satellaview": {{Type: TagTypeAddon, Value: TagAddonOnlineSatellaview}}, // Satellaview online service
	// "st" is ambiguous: Sufami Turbo vs other uses - handled in filename_parser.go
	"sufami-turbo": {{Type: TagTypeAddon, Value: TagAddonPeripheralSufami}}, // Sufami Turbo cartridge adapter
	// "np" is ambiguous: Nintendo Power vs other uses - handled in filename_parser.go
	"nintendo-power": {{Type: TagTypeAddon, Value: TagAddonOnlineNintendopower}}, // Nintendo Power kiosk service
	"j-cart":         {{Type: TagTypeAddon, Value: TagAddonControllerJCart}},     // J-Cart (Genesis controller ports)
	"sn":             {{Type: TagTypeAddon, Value: TagAddonOnlineSeganet}},       // Sega-Net online service
	"sega-net":       {{Type: TagTypeAddon, Value: TagAddonOnlineSeganet}},       // Sega-Net (full name)
	"sachen":         {{Type: TagTypeUnlicensed, Value: TagUnlicensedSachen}},    // Sachen unlicensed (NES)
	"rumble-version": {{Type: TagTypeAddon, Value: TagAddonControllerRumble}},    // Rumble Pak version

	// ============================================================================
	// MULTICART AND COMPILATION MAPPINGS
	// ============================================================================
	"vol":   {{Type: TagTypeMultigame, Value: TagMultigameVol1}},
	"vol-1": {{Type: TagTypeMultigame, Value: TagMultigameVol1}},
	"vol-2": {{Type: TagTypeMultigame, Value: TagMultigameVol2}},
	"vol-3": {{Type: TagTypeMultigame, Value: TagMultigameVol3}},
	"vol-4": {{Type: TagTypeMultigame, Value: TagMultigameVol4}},
	"vol-5": {{Type: TagTypeMultigame, Value: TagMultigameVol5}},
	"vol-6": {{Type: TagTypeMultigame, Value: TagMultigameVol6}},
	"vol-7": {{Type: TagTypeMultigame, Value: TagMultigameVol7}},
	"vol-8": {{Type: TagTypeMultigame, Value: TagMultigameVol8}},
	"vol-9": {{Type: TagTypeMultigame, Value: TagMultigameVol9}},
	"menu":  {{Type: TagTypeMultigame, Value: TagMultigameMenu}}, // Multicart menu ROM

	// ============================================================================
	// SPECIAL EDITION AND BUNDLE MAPPINGS
	// ============================================================================
	"bundle":         {{Type: TagTypeReboxed, Value: TagReboxedBundle}},
	"md-bundle":      {{Type: TagTypeReboxed, Value: TagReboxedBundleGenesis}},
	"genesis-bundle": {{Type: TagTypeReboxed, Value: TagReboxedBundleGenesis}},

	// ============================================================================
	// SUPPLEMENT MAPPINGS
	// ============================================================================
	// Supplementary content types (DLC, updates, expansions)
	"dlc":    {{Type: TagTypeSupplement, Value: TagSupplementDLC}},
	"update": {{Type: TagTypeSupplement, Value: TagSupplementUpdate}},
	"patch":  {{Type: TagTypeSupplement, Value: TagSupplementUpdate}}, // Merged with update
	// For content expansions, not hardware peripherals
	"addon":  {{Type: TagTypeSupplement, Value: TagSupplementExpansion}},
	"theme":  {{Type: TagTypeSupplement, Value: TagSupplementTheme}},  // Visual themes (PS3/Vita/PSP)
	"avatar": {{Type: TagTypeSupplement, Value: TagSupplementAvatar}}, // Avatar items (PS3/Vita)

	// ============================================================================
	// DISTRIBUTION PLATFORM MAPPINGS
	// ============================================================================
	// Digital distribution platforms and online services
	"virtual-console": {{Type: TagTypeDistribution, Value: TagDistributionVirtualConsole}}, // Nintendo Virtual Console
	"wiiware":         {{Type: TagTypeDistribution, Value: TagDistributionWiiWare}},        // Nintendo WiiWare
	"xblig":           {{Type: TagTypeDistribution, Value: TagDistributionXBLIG}},          // Xbox Live Indie Games
	"dsiware":         {{Type: TagTypeDistribution, Value: TagDistributionDSiWare}},        // Nintendo DSiWare

	// ============================================================================
	// MEDIA TYPE MAPPINGS
	// ============================================================================
	// Note: "Disc X of Y" format is handled specially in getTagsFromFileName()
	"disc":      {{Type: TagTypeMedia, Value: TagMediaDisc}},
	"disc-1":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc1}},
	"disc-2":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc2}},
	"disc-3":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc3}},
	"disc-4":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc4}},
	"disc-5":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc5}},
	"disc-6":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc6}},
	"disc-7":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc7}},
	"disc-8":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc8}},
	"disc-9":    {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc9}},
	"disc-10":   {{Type: TagTypeMedia, Value: TagMediaDisc}, {Type: TagTypeDisc, Value: TagDisc10}},
	"disk":      {{Type: TagTypeMedia, Value: TagMediaDisk}},
	"file":      {{Type: TagTypeMedia, Value: TagMediaFile}},
	"part":      {{Type: TagTypeMedia, Value: TagMediaPart}},
	"side":      {{Type: TagTypeMedia, Value: TagMediaSide}},
	"side-a":    {{Type: TagTypeMedia, Value: TagMediaSideA}}, // Cassette/disk side A
	"side-b":    {{Type: TagTypeMedia, Value: TagMediaSideB}}, // Cassette/disk side B
	"side-c":    {{Type: TagTypeMedia, Value: TagMediaSideC}}, // Cassette/disk side C
	"side-d":    {{Type: TagTypeMedia, Value: TagMediaSideD}}, // Cassette/disk side D
	"tape":      {{Type: TagTypeMedia, Value: TagMediaTape}},
	"cart":      {{Type: TagTypeMedia, Value: TagMediaCart}},      // Cartridge format
	"cartridge": {{Type: TagTypeMedia, Value: TagMediaCart}},      // Cartridge (full name)
	"n64dd":     {{Type: TagTypeMedia, Value: TagMediaN64DD}},     // Nintendo 64DD disk
	"fds":       {{Type: TagTypeMedia, Value: TagMediaFDS}},       // Famicom Disk System
	"e-reader":  {{Type: TagTypeMedia, Value: TagMediaEReader}},   // e-Reader card
	"mb":        {{Type: TagTypeMedia, Value: TagMediaMultiboot}}, // Multiboot ROM (GBA)
	"multiboot": {{Type: TagTypeMedia, Value: TagMediaMultiboot}}, // Multiboot (full name)
	// Disk-X-of-Y compound patterns (commonly found in TOSEC DATs)
	"disk-1-of-2": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc1},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal2},
	},
	"disk-2-of-2": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc2},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal2},
	},
	"disk-1-of-3": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc1},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal3},
	},
	"disk-2-of-3": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc2},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal3},
	},
	"disk-3-of-3": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc3},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal3},
	},
	"disk-1-of-4": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc1},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal4},
	},
	"disk-2-of-4": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc2},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal4},
	},
	"disk-3-of-4": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc3},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal4},
	},
	"disk-4-of-4": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc4},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal4},
	},
	"disk-1-of-5": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc1},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal5},
	},
	"disk-2-of-5": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc2},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal5},
	},
	"disk-3-of-5": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc3},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal5},
	},
	"disk-4-of-5": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc4},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal5},
	},
	"disk-5-of-5": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc5},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal5},
	},
	"disk-1-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc1},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"disk-2-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc2},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"disk-3-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc3},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"disk-4-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc4},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"disk-5-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc5},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"disk-6-of-6": {
		{Type: TagTypeMedia, Value: TagMediaDisk},
		{Type: TagTypeDisc, Value: TagDisc6},
		{Type: TagTypeDiscTotal, Value: TagDiscTotal6},
	},
	"adam": {{Type: TagTypeCompatibility, Value: TagCompatibilityColecoAdam}}, // ColecoVision ADAM

	// ============================================================================
	// EDITION MAPPINGS
	// ============================================================================
	// Edition markers (remaster, special, deluxe, etc.)
	"remaster":   {{Type: TagTypeEdition, Value: TagEditionRemaster}},
	"remastered": {{Type: TagTypeEdition, Value: TagEditionRemaster}}, // Normalize to same value
}
