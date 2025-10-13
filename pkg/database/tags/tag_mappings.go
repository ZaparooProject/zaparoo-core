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
	return allTagMappings[tag]
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
	"world":       {{TagTypeRegion, TagRegionWorld}},
	"europe":      {{TagTypeRegion, TagRegionEU}},
	"asia":        {{TagTypeRegion, TagRegionAsia}},
	"australia":   {{TagTypeRegion, TagRegionAU}, {TagTypeLang, TagLangEN}},
	"brazil":      {{TagTypeRegion, TagRegionBR}, {TagTypeLang, TagLangPT}},
	"canada":      {{TagTypeRegion, TagRegionCA}, {TagTypeLang, TagLangEN}},
	"china":       {{TagTypeRegion, TagRegionCN}, {TagTypeLang, TagLangZH}},
	"france":      {{TagTypeRegion, TagRegionFR}, {TagTypeLang, TagLangFR}},
	"germany":     {{TagTypeRegion, TagRegionDE}, {TagTypeLang, TagLangDE}},
	"hong-kong":   {{TagTypeRegion, TagRegionHK}}, // Multilingual: Chinese/English
	"italy":       {{TagTypeRegion, TagRegionIT}, {TagTypeLang, TagLangIT}},
	"japan":       {{TagTypeRegion, TagRegionJP}, {TagTypeLang, TagLangJA}},
	"korea":       {{TagTypeRegion, TagRegionKR}, {TagTypeLang, TagLangKO}},
	"netherlands": {{TagTypeRegion, TagRegionNL}, {TagTypeLang, TagLangNL}},
	"spain":       {{TagTypeRegion, TagRegionES}, {TagTypeLang, TagLangES}},
	"sweden":      {{TagTypeRegion, TagRegionSE}, {TagTypeLang, TagLangSV}},
	"usa":         {{TagTypeRegion, TagRegionUS}, {TagTypeLang, TagLangEN}},
	"poland":      {{TagTypeRegion, TagRegionPL}, {TagTypeLang, TagLangPL}},
	"finland":     {{TagTypeRegion, TagRegionFI}, {TagTypeLang, TagLangFI}},
	"denmark":     {{TagTypeRegion, TagRegionDK}, {TagTypeLang, TagLangDA}},
	"portugal":    {{TagTypeRegion, TagRegionPT}, {TagTypeLang, TagLangPT}},
	"norway":      {{TagTypeRegion, TagRegionNO}, {TagTypeLang, TagLangNO}},

	// TOSEC style regions (country codes)
	"ae": {{TagTypeRegion, TagRegionAE}, {TagTypeLang, TagLangAR}}, // United Arab Emirates
	"al": {{TagTypeRegion, TagRegionAL}},
	"as": {{TagTypeRegion, TagRegionAS}},
	"at": {{TagTypeRegion, TagRegionAT}, {TagTypeLang, TagLangDE}}, // Austria
	"au": {{TagTypeRegion, TagRegionAU}, {TagTypeLang, TagLangEN}}, // Australia
	"ba": {{TagTypeRegion, TagRegionBA}},
	"be": {{TagTypeRegion, TagRegionBE}}, // Belgium - multilingual (Dutch/French/German)
	"bg": {{TagTypeRegion, TagRegionBG}, {TagTypeLang, TagLangBG}},
	"br": {{TagTypeRegion, TagRegionBR}, {TagTypeLang, TagLangPT}},
	"ca": {{TagTypeRegion, TagRegionCA}, {TagTypeLang, TagLangEN}}, // Canada - predominantly English in gaming
	// Switzerland - context-aware: if lang:de present → region:ch; else → lang:zh (see filename_parser.go)
	"ch": {{TagTypeRegion, TagRegionCH}},
	"cl": {{TagTypeRegion, TagRegionCL}, {TagTypeLang, TagLangES}}, // Chile
	"cn": {{TagTypeRegion, TagRegionCN}, {TagTypeLang, TagLangZH}},
	"cs": {{TagTypeRegion, TagRegionCS}},
	"cy": {{TagTypeRegion, TagRegionCY}},
	"cz": {{TagTypeRegion, TagRegionCZ}, {TagTypeLang, TagLangCS}},
	"de": {{TagTypeRegion, TagRegionDE}, {TagTypeLang, TagLangDE}},
	"dk": {{TagTypeRegion, TagRegionDK}, {TagTypeLang, TagLangDA}},
	"ee": {{TagTypeRegion, TagRegionEE}, {TagTypeLang, TagLangET}}, // Estonia
	"eg": {{TagTypeRegion, TagRegionEG}, {TagTypeLang, TagLangAR}}, // Egypt
	"es": {{TagTypeRegion, TagRegionES}, {TagTypeLang, TagLangES}},
	"eu": {{TagTypeRegion, TagRegionEU}}, // Europe - multi-region
	"fi": {{TagTypeRegion, TagRegionFI}, {TagTypeLang, TagLangFI}},
	"fr": {{TagTypeRegion, TagRegionFR}, {TagTypeLang, TagLangFR}},
	"gb": {{TagTypeRegion, TagRegionGB}, {TagTypeLang, TagLangEN}},
	"gr": {{TagTypeRegion, TagRegionGR}, {TagTypeLang, TagLangEL}},
	"hk": {{TagTypeRegion, TagRegionHK}}, // Hong Kong - multilingual (Chinese/English)
	"hr": {{TagTypeRegion, TagRegionHR}, {TagTypeLang, TagLangHR}},
	"hu": {{TagTypeRegion, TagRegionHU}, {TagTypeLang, TagLangHU}},
	"id": {{TagTypeRegion, TagRegionID}},
	"ie": {{TagTypeRegion, TagRegionIE}, {TagTypeLang, TagLangEN}},
	"il": {{TagTypeRegion, TagRegionIL}, {TagTypeLang, TagLangHE}},
	"in": {{TagTypeRegion, TagRegionIN}, {TagTypeLang, TagLangHI}},
	"ir": {{TagTypeRegion, TagRegionIR}, {TagTypeLang, TagLangFA}},
	"is": {{TagTypeRegion, TagRegionIS}, {TagTypeLang, TagLangIS}}, // Iceland
	"it": {{TagTypeRegion, TagRegionIT}, {TagTypeLang, TagLangIT}},
	"jo": {{TagTypeRegion, TagRegionJO}, {TagTypeLang, TagLangAR}}, // Jordan
	"jp": {{TagTypeRegion, TagRegionJP}, {TagTypeLang, TagLangJA}},
	"kr": {{TagTypeRegion, TagRegionKR}, {TagTypeLang, TagLangKO}},
	"lt": {{TagTypeRegion, TagRegionLT}, {TagTypeLang, TagLangLT}},
	"lu": {{TagTypeRegion, TagRegionLU}}, // Luxembourg - multilingual (Luxembourgish/French/German)
	"lv": {{TagTypeRegion, TagRegionLV}, {TagTypeLang, TagLangLV}},
	"mn": {{TagTypeRegion, TagRegionMN}},
	"mx": {{TagTypeRegion, TagRegionMX}, {TagTypeLang, TagLangES}},
	"my": {{TagTypeRegion, TagRegionMY}, {TagTypeLang, TagLangMS}},
	"nl": {{TagTypeRegion, TagRegionNL}, {TagTypeLang, TagLangNL}},
	"no": {{TagTypeRegion, TagRegionNO}, {TagTypeLang, TagLangNO}},
	"np": {{TagTypeRegion, TagRegionNP}},
	"nz": {{TagTypeRegion, TagRegionNZ}, {TagTypeLang, TagLangEN}}, // New Zealand
	"om": {{TagTypeRegion, TagRegionOM}, {TagTypeLang, TagLangAR}}, // Oman
	"pe": {{TagTypeRegion, TagRegionPE}, {TagTypeLang, TagLangES}}, // Peru
	"ph": {{TagTypeRegion, TagRegionPH}},
	"pl": {{TagTypeRegion, TagRegionPL}, {TagTypeLang, TagLangPL}},
	"pt": {{TagTypeRegion, TagRegionPT}, {TagTypeLang, TagLangPT}},
	"qa": {{TagTypeRegion, TagRegionQA}, {TagTypeLang, TagLangAR}}, // Qatar
	"ro": {{TagTypeRegion, TagRegionRO}, {TagTypeLang, TagLangRO}},
	"ru": {{TagTypeRegion, TagRegionRU}, {TagTypeLang, TagLangRU}},
	"se": {{TagTypeRegion, TagRegionSE}, {TagTypeLang, TagLangSV}},
	"sg": {{TagTypeRegion, TagRegionSG}}, // Singapore - multilingual (English/Malay/Chinese/Tamil)
	"si": {{TagTypeRegion, TagRegionSI}, {TagTypeLang, TagLangSL}},
	"sk": {{TagTypeRegion, TagRegionSK}, {TagTypeLang, TagLangSK}},
	"th": {{TagTypeRegion, TagRegionTH}, {TagTypeLang, TagLangTH}},
	// Turkey - context-aware: ()=region/lang, []=translated (see filename_parser.go)
	"tr": {{TagTypeRegion, TagRegionTR}},
	"tw": {{TagTypeRegion, TagRegionTW}, {TagTypeLang, TagLangZH}}, // Taiwan
	"us": {{TagTypeRegion, TagRegionUS}, {TagTypeLang, TagLangEN}},
	"vn": {{TagTypeRegion, TagRegionVN}, {TagTypeLang, TagLangVI}},
	"yu": {{TagTypeRegion, TagRegionYU}},
	"za": {{TagTypeRegion, TagRegionZA}},

	// ============================================================================
	// LANGUAGE MAPPINGS
	// ============================================================================
	// Note: Some language codes overlap with region codes above and are merged there.
	// These are either standalone language codes or ones where the region mapping doesn't auto-add the language.
	"ar": {{TagTypeLang, TagLangAR}},
	"bs": {{TagTypeLang, TagLangBS}},
	"el": {{TagTypeLang, TagLangEL}},
	"en": {{TagTypeLang, TagLangEN}},
	"eo": {{TagTypeLang, TagLangEO}},
	"fa": {{TagTypeLang, TagLangFA}},
	"ga": {{TagTypeLang, TagLangGA}},
	"gu": {{TagTypeLang, TagLangGU}},
	"he": {{TagTypeLang, TagLangHE}},
	"hi": {{TagTypeLang, TagLangHI}},
	"ms": {{TagTypeLang, TagLangMS}},
	"sl": {{TagTypeLang, TagLangSL}},
	"sq": {{TagTypeLang, TagLangSQ}},
	"sr": {{TagTypeLang, TagLangSR}},
	"ur": {{TagTypeLang, TagLangUR}},
	"vi": {{TagTypeLang, TagLangVI}},
	"yi": {{TagTypeLang, TagLangYI}},
	"zh": {{TagTypeLang, TagLangZH}},
	// Language variants (not multi-language - these are specific dialects/scripts)
	"ch-trad":   {{TagTypeLang, TagLangZH}, {TagTypeLang, TagLangZHTrad}}, // Traditional Chinese
	"ch-simple": {{TagTypeLang, TagLangZH}, {TagTypeLang, TagLangZHHans}}, // Simplified Chinese
	"bra":       {{TagTypeLang, TagLangPTBR}},                             // Brazilian Portuguese

	// ============================================================================
	// YEAR MAPPINGS
	// ============================================================================
	// Note: Exact years (1970-2099) are handled dynamically in extractSpecialPatterns
	// Wildcard years for unknown/approximate dates (TOSEC convention)
	"19xx": {{TagTypeYear, TagYear19XX}}, "197x": {{TagTypeYear, TagYear197X}},
	"198x": {{TagTypeYear, TagYear198X}}, "199x": {{TagTypeYear, TagYear199X}},
	"20xx": {{TagTypeYear, TagYear20XX}}, "200x": {{TagTypeYear, TagYear200X}},
	"201x": {{TagTypeYear, TagYear201X}}, "202x": {{TagTypeYear, TagYear202X}},

	// ============================================================================
	// DEVELOPMENT STATUS MAPPINGS
	// ============================================================================
	"alpha":          {{TagTypeUnfinished, TagUnfinishedAlpha}},
	"beta":           {{TagTypeUnfinished, TagUnfinishedBeta}},
	"beta-1":         {{TagTypeUnfinished, TagUnfinishedBeta1}},
	"beta-2":         {{TagTypeUnfinished, TagUnfinishedBeta2}},
	"beta-3":         {{TagTypeUnfinished, TagUnfinishedBeta3}},
	"beta-4":         {{TagTypeUnfinished, TagUnfinishedBeta4}},
	"beta-5":         {{TagTypeUnfinished, TagUnfinishedBeta5}},
	"preview":        {{TagTypeUnfinished, TagUnfinishedPreview}},
	"pre-release":    {{TagTypeUnfinished, TagUnfinishedPrerelease}},
	"proto":          {{TagTypeUnfinished, TagUnfinishedProto}},
	"proto-1":        {{TagTypeUnfinished, TagUnfinishedProto1}},
	"proto-2":        {{TagTypeUnfinished, TagUnfinishedProto2}},
	"proto-3":        {{TagTypeUnfinished, TagUnfinishedProto3}},
	"proto-4":        {{TagTypeUnfinished, TagUnfinishedProto4}},
	"sample":         {{TagTypeUnfinished, TagUnfinishedSample}},
	"demo":           {{TagTypeUnfinished, TagUnfinishedDemo}},
	"demo-1":         {{TagTypeUnfinished, TagUnfinishedDemo1}},
	"demo-2":         {{TagTypeUnfinished, TagUnfinishedDemo2}},
	"demo-auto":      {{TagTypeUnfinished, TagUnfinishedDemoAuto}},
	"demo-kiosk":     {{TagTypeUnfinished, TagUnfinishedDemoKiosk}},
	"demo-playable":  {{TagTypeUnfinished, TagUnfinishedDemoPlayable}},
	"demo-rolling":   {{TagTypeUnfinished, TagUnfinishedDemoRolling}},
	"demo-slideshow": {{TagTypeUnfinished, TagUnfinishedDemoSlideshow}},
	"debug":          {{TagTypeUnfinished, TagUnfinishedDebug}},
	"competition":    {{TagTypeUnfinished, TagUnfinishedCompetition}},

	// ============================================================================
	// VERSION/REVISION MAPPINGS
	// ============================================================================
	// Note: Dotted versions (v1.0, v1.2.3) are handled dynamically in extractSpecialPatterns
	"rev":   {{TagTypeRev, TagRev1}},
	"rev-1": {{TagTypeRev, TagRev1}},
	"rev-2": {{TagTypeRev, TagRev2}},
	"rev-3": {{TagTypeRev, TagRev3}},
	"rev-4": {{TagTypeRev, TagRev4}},
	"rev-5": {{TagTypeRev, TagRev5}},
	"rev-a": {{TagTypeRev, TagRevA}},
	"rev-b": {{TagTypeRev, TagRevB}},
	"rev-c": {{TagTypeRev, TagRevC}},
	"rev-d": {{TagTypeRev, TagRevD}},
	"rev-e": {{TagTypeRev, TagRevE}},
	"rev-g": {{TagTypeRev, TagRevG}},
	"v":     {{TagTypeRev, TagRev1}},
	"v1":    {{TagTypeRev, TagRev1}},
	"v2":    {{TagTypeRev, TagRev2}},
	"v3":    {{TagTypeRev, TagRev3}},
	"v4":    {{TagTypeRev, TagRev4}},
	"v5":    {{TagTypeRev, TagRev5}},
	// Program revisions (NES-specific)
	"prg":  {{TagTypeRev, TagRevPRG}},
	"prg0": {{TagTypeRev, TagRevPRG0}},
	"prg1": {{TagTypeRev, TagRevPRG1}},
	"prg2": {{TagTypeRev, TagRevPRG2}},
	"prg3": {{TagTypeRev, TagRevPRG3}},
	"unl":  {{TagTypeUnlicensed, TagUnlicensed}}, // No-Intro unlicensed flag

	// ============================================================================
	// VIDEO FORMAT MAPPINGS (TOSEC)
	// ============================================================================
	"cga":      {{TagTypeVideo, TagVideoCGA}},
	"ega":      {{TagTypeVideo, TagVideoEGA}},
	"hgc":      {{TagTypeVideo, TagVideoHGC}},
	"mcga":     {{TagTypeVideo, TagVideoMCGA}},
	"mda":      {{TagTypeVideo, TagVideoMDA}},
	"ntsc":     {{TagTypeVideo, TagVideoNTSC}},
	"ntsc-pal": {{TagTypeVideo, TagVideoNTSCPAL}},
	"pal":      {{TagTypeVideo, TagVideoPAL}},
	"pal-60":   {{TagTypeVideo, TagVideoPAL60}},
	"pal-ntsc": {{TagTypeVideo, TagVideoPALNTSC}},
	"svga":     {{TagTypeVideo, TagVideoSVGA}},
	"vga":      {{TagTypeVideo, TagVideoVGA}},
	"xga":      {{TagTypeVideo, TagVideoXGA}},

	// ============================================================================
	// COPYRIGHT MAPPINGS (TOSEC)
	// ============================================================================
	"cw":   {{TagTypeCopyright, TagCopyrightCW}},  // Cardware
	"cw-r": {{TagTypeCopyright, TagCopyrightCWR}}, // Cardware (registered)
	"fw":   {{TagTypeCopyright, TagCopyrightFW}},  // Freeware
	"gw":   {{TagTypeCopyright, TagCopyrightGW}},  // Giftware
	"gw-r": {{TagTypeCopyright, TagCopyrightGWR}}, // Giftware (registered)
	"lw":   {{TagTypeCopyright, TagCopyrightLW}},  // Linkware
	"pd":   {{TagTypeCopyright, TagCopyrightPD}},  // Public domain
	"sw":   {{TagTypeCopyright, TagCopyrightSW}},  // Shareware
	"sw-r": {{TagTypeCopyright, TagCopyrightSWR}}, // Shareware (registered)

	// ============================================================================
	// DUMP INFO MAPPINGS (TOSEC)
	// ============================================================================
	// Note: "tr" (translated) is merged with Turkey region mapping above
	// Note: "v" (virus) is merged with version mapping below
	"cr": {{TagTypeDump, TagDumpCracked}},   // Cracked
	"f":  {{TagTypeDump, TagDumpFixed}},     // Fixed
	"h":  {{TagTypeDump, TagDumpHacked}},    // Hacked
	"m":  {{TagTypeDump, TagDumpModified}},  // Modified
	"p":  {{TagTypeDump, TagDumpPirated}},   // Pirated
	"t":  {{TagTypeDump, TagDumpTrained}},   // Trained
	"o":  {{TagTypeDump, TagDumpOverdump}},  // Overdump
	"u":  {{TagTypeDump, TagDumpUnderdump}}, // Underdump
	"b":  {{TagTypeDump, TagDumpBad}},       // Bad dump
	"a":  {{TagTypeDump, TagDumpAlternate}}, // Alternate
	"!":  {{TagTypeDump, TagDumpVerified}},  // Verified good dump
	"!p": {{TagTypeDump, TagDumpPending}},   // Pending verification (GoodTools)
	// Bad checksum but good dump (GoodTools Genesis)
	"c": {{TagTypeDump, TagDumpChecksumBad}},
	// Unknown checksum status (GoodTools Genesis)
	"x":    {{TagTypeDump, TagDumpChecksumUnknown}},
	"bios": {{TagTypeDump, TagDumpBIOS}},                                    // BIOS dump
	"hffe": {{TagTypeDump, TagDumpHacked}, {TagTypeDump, TagDumpHackedFFE}}, // Far East Copier hack
	// "hi" is ambiguous: Hindi language vs hacked intro - handled in filename_parser.go
	"hir":   {{TagTypeDump, TagDumpHacked}, {TagTypeDump, TagDumpHackedIntroRemov}}, // Hacked intro removed
	"hir00": {{TagTypeDump, TagDumpHacked}, {TagTypeDump, TagDumpHackedIntroRemov}}, // Intro removed with 00 fill
	"hirff": {{TagTypeDump, TagDumpHacked}, {TagTypeDump, TagDumpHackedIntroRemov}}, // Intro removed with ff fill

	// ============================================================================
	// TOSEC SYSTEM-SPECIFIC MAPPINGS
	// ============================================================================
	// Amiga systems
	"+2":                     {{TagTypeCompatibility, TagCompatibilityAmigaPlus2}},
	"+2a":                    {{TagTypeCompatibility, TagCompatibilityAmigaPlus2A}},
	"+3":                     {{TagTypeCompatibility, TagCompatibilityAmigaPlus3}},
	"130xe":                  {{TagTypeCompatibility, TagCompatibilityAtari130XE}},
	"a1000":                  {{TagTypeCompatibility, TagCompatibilityAmigaA1000}},
	"a1200":                  {{TagTypeCompatibility, TagCompatibilityAmigaA1200}},
	"a1200-a4000":            {{TagTypeCompatibility, TagCompatibilityAmigaA1200A4000}},
	"a2000":                  {{TagTypeCompatibility, TagCompatibilityAmigaA2000}},
	"a2000-a3000":            {{TagTypeCompatibility, TagCompatibilityAmigaA2000A3000}},
	"a2024":                  {{TagTypeCompatibility, TagCompatibilityAmigaA2024}},
	"a2500-a3000ux":          {{TagTypeCompatibility, TagCompatibilityAmigaA2500A3000UX}},
	"a3000":                  {{TagTypeCompatibility, TagCompatibilityAmigaA3000}},
	"a4000":                  {{TagTypeCompatibility, TagCompatibilityAmigaA4000}},
	"a4000t":                 {{TagTypeCompatibility, TagCompatibilityAmigaA4000T}},
	"a500":                   {{TagTypeCompatibility, TagCompatibilityAmigaA500}},
	"a500+":                  {{TagTypeCompatibility, TagCompatibilityAmigaA500Plus}},
	"a500-a1000-a2000":       {{TagTypeCompatibility, TagCompatibilityAmigaA500A1000A2000}},
	"a500-a1000-a2000-cdtv":  {{TagTypeCompatibility, TagCompatibilityAmigaA500A1000A2000CDTV}},
	"a500-a1200":             {{TagTypeCompatibility, TagCompatibilityAmigaA500A1200}},
	"a500-a1200-a2000-a4000": {{TagTypeCompatibility, TagCompatibilityAmigaA500A1200A2000A4000}},
	"a500-a2000":             {{TagTypeCompatibility, TagCompatibilityAmigaA500A2000}},
	"a500-a600-a2000":        {{TagTypeCompatibility, TagCompatibilityAmigaA500A600A2000}},
	"a570":                   {{TagTypeCompatibility, TagCompatibilityAmigaA570}},
	"a600":                   {{TagTypeCompatibility, TagCompatibilityAmigaA600}},
	"a600hd":                 {{TagTypeCompatibility, TagCompatibilityAmigaA600HD}},
	"aga":                    {{TagTypeCompatibility, TagCompatibilityAmigaAGA}},
	"aga-cd32":               {{TagTypeCompatibility, TagCompatibilityAmigaAGACD32}},
	"aladdin-deck-enhancer":  {{TagTypeAddon, TagAddonLockonDeckenhancer}},
	"cd32":                   {{TagTypeCompatibility, TagCompatibilityAmigaCD32}},
	"cdtv":                   {{TagTypeCompatibility, TagCompatibilityAmigaCDTV}},
	"computrainer":           {{TagTypeAddon, TagAddonControllerComputrainer}},
	"doctor-pc-jr":           {{TagTypeCompatibility, TagCompatibilityIBMPCDoctorPCJr}},
	"ecs":                    {{TagTypeCompatibility, TagCompatibilityAmigaECS}},
	"ecs-aga":                {{TagTypeCompatibility, TagCompatibilityAmigaECSAGA}},
	"executive":              {{TagTypeCompatibility, TagCompatibilityAtariExecutive}},
	"mega-st":                {{TagTypeCompatibility, TagCompatibilityAtariMegaST}},
	"mega-ste":               {{TagTypeCompatibility, TagCompatibilityAtariMegaSTE}},
	"ocs":                    {{TagTypeCompatibility, TagCompatibilityAmigaOCS}},
	"ocs-aga":                {{TagTypeCompatibility, TagCompatibilityAmigaOCSAGA}},
	"orch80":                 {{TagTypeCompatibility, TagCompatibilityMiscOrch80}},
	"osbourne-1":             {{TagTypeCompatibility, TagCompatibilityOsbourneOsbourne1}},
	"piano90":                {{TagTypeCompatibility, TagCompatibilityMiscPiano90}},
	"playchoice-10":          {{TagTypeCompatibility, TagCompatibilityNintendoPlaychoice10}},
	"plus4":                  {{TagTypeCompatibility, TagCompatibilityCommodorePlus4}},
	"primo-a":                {{TagTypeCompatibility, TagCompatibilityPrimoPrimoA}},
	"primo-a64":              {{TagTypeCompatibility, TagCompatibilityPrimoPrimoA64}},
	"primo-b":                {{TagTypeCompatibility, TagCompatibilityPrimoPrimoB}},
	"primo-b64":              {{TagTypeCompatibility, TagCompatibilityPrimoPrimoB64}},
	"pro-primo":              {{TagTypeCompatibility, TagCompatibilityPrimoProprimo}},
	"st":                     {{TagTypeCompatibility, TagCompatibilityAtariST}},
	"ste":                    {{TagTypeCompatibility, TagCompatibilityAtariSTE}},
	"ste-falcon":             {{TagTypeCompatibility, TagCompatibilityAtariSTEFalcon}},
	"tt":                     {{TagTypeCompatibility, TagCompatibilityAtariTT}},
	"turbo-r-gt":             {{TagTypeCompatibility, TagCompatibilityMSXTurboRGT}},
	"turbo-r-st":             {{TagTypeCompatibility, TagCompatibilityMSXTurboRST}},
	"vs-dualsystem":          {{TagTypeCompatibility, TagCompatibilityNintendoVSDualsystem}},
	"vs-unisystem":           {{TagTypeCompatibility, TagCompatibilityNintendoVSUnisystem}},

	// ============================================================================
	// ARCADE AND SPECIAL HARDWARE MAPPINGS
	// ============================================================================
	"vs":       {{TagTypeArcadeBoard, TagArcadeBoardNintendoVS}},   // VS System arcade
	"nss":      {{TagTypeArcadeBoard, TagArcadeBoardNintendoNSS}},  // Nintendo Super System arcade
	"megaplay": {{TagTypeArcadeBoard, TagArcadeBoardSegaMegaplay}}, // MegaPlay arcade
	"mp":       {{TagTypeArcadeBoard, TagArcadeBoardSegaMegaplay}}, // MegaPlay (short form)
	// "bs" is ambiguous: Bosnian language vs Satellaview - handled in filename_parser.go
	"satellaview": {{TagTypeAddon, TagAddonOnlineSatellaview}}, // Satellaview online service
	// "st" is ambiguous: Sufami Turbo vs other uses - handled in filename_parser.go
	"sufami-turbo": {{TagTypeAddon, TagAddonPeripheralSufami}}, // Sufami Turbo cartridge adapter
	// "np" is ambiguous: Nintendo Power vs other uses - handled in filename_parser.go
	"nintendo-power": {{TagTypeAddon, TagAddonOnlineNintendopower}}, // Nintendo Power kiosk service
	"j-cart":         {{TagTypeAddon, TagAddonControllerJCart}},     // J-Cart (Genesis controller ports)
	"sn":             {{TagTypeAddon, TagAddonOnlineSeganet}},       // Sega-Net online service
	"sega-net":       {{TagTypeAddon, TagAddonOnlineSeganet}},       // Sega-Net (full name)
	"sachen":         {{TagTypeUnlicensed, TagUnlicensedSachen}},    // Sachen unlicensed (NES)
	"rumble-version": {{TagTypeAddon, TagAddonControllerRumble}},    // Rumble Pak version

	// ============================================================================
	// MULTICART AND COMPILATION MAPPINGS
	// ============================================================================
	"vol":   {{TagTypeMultigame, TagMultigameVol1}},
	"vol-1": {{TagTypeMultigame, TagMultigameVol1}},
	"vol-2": {{TagTypeMultigame, TagMultigameVol2}},
	"vol-3": {{TagTypeMultigame, TagMultigameVol3}},
	"vol-4": {{TagTypeMultigame, TagMultigameVol4}},
	"vol-5": {{TagTypeMultigame, TagMultigameVol5}},
	"vol-6": {{TagTypeMultigame, TagMultigameVol6}},
	"vol-7": {{TagTypeMultigame, TagMultigameVol7}},
	"vol-8": {{TagTypeMultigame, TagMultigameVol8}},
	"vol-9": {{TagTypeMultigame, TagMultigameVol9}},
	"menu":  {{TagTypeMultigame, TagMultigameMenu}}, // Multicart menu ROM

	// ============================================================================
	// SPECIAL EDITION AND BUNDLE MAPPINGS
	// ============================================================================
	"bundle":         {{TagTypeReboxed, TagReboxedBundle}},
	"md-bundle":      {{TagTypeReboxed, TagReboxedBundleGenesis}},
	"genesis-bundle": {{TagTypeReboxed, TagReboxedBundleGenesis}},

	// ============================================================================
	// MEDIA TYPE MAPPINGS
	// ============================================================================
	// Note: "Disc X of Y" format is handled specially in getTagsFromFileName()
	"disc":      {{TagTypeMedia, TagMediaDisc}},
	"disc-1":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc1}},
	"disc-2":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc2}},
	"disc-3":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc3}},
	"disc-4":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc4}},
	"disc-5":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc5}},
	"disc-6":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc6}},
	"disc-7":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc7}},
	"disc-8":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc8}},
	"disc-9":    {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc9}},
	"disc-10":   {{TagTypeMedia, TagMediaDisc}, {TagTypeDisc, TagDisc10}},
	"disk":      {{TagTypeMedia, TagMediaDisk}},
	"file":      {{TagTypeMedia, TagMediaFile}},
	"part":      {{TagTypeMedia, TagMediaPart}},
	"side":      {{TagTypeMedia, TagMediaSide}},
	"tape":      {{TagTypeMedia, TagMediaTape}},
	"cart":      {{TagTypeMedia, TagMediaCart}},                       // Cartridge format
	"cartridge": {{TagTypeMedia, TagMediaCart}},                       // Cartridge (full name)
	"n64dd":     {{TagTypeMedia, TagMediaN64DD}},                      // Nintendo 64DD disk
	"fds":       {{TagTypeMedia, TagMediaFDS}},                        // Famicom Disk System
	"e-reader":  {{TagTypeMedia, TagMediaEReader}},                    // e-Reader card
	"mb":        {{TagTypeMedia, TagMediaMultiboot}},                  // Multiboot ROM (GBA)
	"multiboot": {{TagTypeMedia, TagMediaMultiboot}},                  // Multiboot (full name)
	"adam":      {{TagTypeCompatibility, TagCompatibilityColecoAdam}}, // ColecoVision ADAM
}
