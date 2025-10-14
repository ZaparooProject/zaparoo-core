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

import (
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// This tag system is inspired by the GameDataBase project's hierarchical
// tag taxonomy. Reference: https://github.com/PigSaint/GameDataBase/blob/main/tags.yml

// Package-level compiled regexes for tag normalization.
// These are compiled once at initialization for optimal performance.
var (
	reColonSpacing = regexp.MustCompile(`\s*:\s*`)
	reSpecialChars = regexp.MustCompile(`[^a-z0-9:,+\-]`)
)

// TagType represents a top-level tag category
type TagType string

// TagValue represents a canonical tag value
type TagValue string

// CanonicalTag represents a pre-split canonical tag for efficient processing
type CanonicalTag struct {
	Type  TagType
	Value TagValue
}

// String returns the full tag in "type:value" format
func (t CanonicalTag) String() string {
	if t.Value == "" {
		return string(t.Type)
	}
	return string(t.Type) + ":" + string(t.Value)
}

// Tag type constants - these define the top-level categories for our hierarchical tag system
// Format: Type defines the category, tags within each type use colon-separated hierarchies
const (
	TagTypeInput         TagType = "input"         // Input devices and controls
	TagTypePlayers       TagType = "players"       // Player count and modes
	TagTypeGameGenre     TagType = "gamegenre"     // Game genre and subgenres
	TagTypeAddon         TagType = "addon"         // External peripherals and add-ons
	TagTypeEmbedded      TagType = "embedded"      // Embedded chips and internal hardware
	TagTypeSave          TagType = "save"          // Save mechanism
	TagTypeArcadeBoard   TagType = "arcadeboard"   // Arcade board types
	TagTypeCompatibility TagType = "compatibility" // System compatibility tags
	TagTypeDisc          TagType = "disc"          // Disc number for multi-disc games
	TagTypeDiscTotal     TagType = "disctotal"     // Total number of discs in a multi-disc set
	TagTypeBased         TagType = "based"         // Based on (movie, manga, etc.)
	TagTypeSearch        TagType = "search"        // Search metadata (franchises, features, orientation)
	TagTypeMultigame     TagType = "multigame"     // Multi-game compilations
	TagTypeReboxed       TagType = "reboxed"       // Re-releases and special editions
	TagTypePort          TagType = "port"          // Ported from other platforms
	TagTypeLang          TagType = "lang"          // Language
	TagTypeUnfinished    TagType = "unfinished"    // Development status (alpha, beta, demo, proto)
	TagTypeRerelease     TagType = "rerelease"     // Digital re-releases and collections
	TagTypeRev           TagType = "rev"           // Revision/version number
	TagTypeSet           TagType = "set"           // Set number
	TagTypeAlt           TagType = "alt"           // Alternate version
	TagTypeUnlicensed    TagType = "unlicensed"    // Unlicensed/bootleg/hacks
	TagTypeMameParent    TagType = "mameparent"    // MAME parent ROM relationship
	TagTypeRegion        TagType = "region"        // Release region
	TagTypeYear          TagType = "year"          // Release year
	TagTypeVideo         TagType = "video"         // Video format (NTSC, PAL, etc.)
	TagTypeCopyright     TagType = "copyright"     // Copyright status (TOSEC)
	TagTypeDump          TagType = "dump"          // Dump quality/status
	TagTypeMedia         TagType = "media"         // Media type (disc, disk, tape, etc.)
	TagTypeExtension     TagType = "extension"     // File extension
	TagTypeEdition       TagType = "edition"       // Edition markers (version/edition words)
	TagTypePerspective   TagType = "perspective"   // Camera perspective and view angle
	TagTypeArt           TagType = "art"           // Art style and visual presentation
	TagTypeAccessibility TagType = "accessibility" // Accessibility features
	TagTypeUnknown       TagType = "unknown"       // Unknown tags
)

// Tag Format:
//   - Flat tags: Just the value (e.g., "trackball", "quiz")
//   - Hierarchical tags: Colon-separated (e.g., "joystick:4", "sports:wrestling")
//   - All values are normalized (lowercase, spaces→dashes, no periods)
//
// Multi-language tags:
//   Filenames can specify multiple languages using either comma or plus separators:
//   - No-Intro format: "(En,Fr,De)" → generates lang:en, lang:fr, lang:de
//   - TOSEC format:    "(En+Fr+De)" → generates lang:en, lang:fr, lang:de
//   Both formats are dynamically parsed and produce individual language tags.
//   Minimum 2 languages required for multi-language detection.
//
// Our format differs from GameDataBase (#type:subtag>value) by using colons throughout
// for simplicity, but the taxonomy and tag values are directly inspired by their work.

// NormalizeTag normalizes a tag string for consistent querying and storage.
// Applied to BOTH type and value parts separately.
// Rules: trim whitespace, normalize colon spacing, lowercase, spaces→dashes,
// periods→dashes, and remove special chars (except colon, dash, and comma)
func NormalizeTag(s string) string {
	// 1. Trim whitespace
	s = strings.TrimSpace(s)

	// 2. Normalize colon spacing - remove spaces around colons first
	s = reColonSpacing.ReplaceAllString(s, ":")

	// 3. Convert to lowercase
	s = strings.ToLower(s)

	// 4. Replace remaining spaces with dashes
	s = strings.ReplaceAll(s, " ", "-")

	// 5. Convert periods to dashes (for version numbers like "1.2.3" → "1-2-3")
	s = strings.ReplaceAll(s, ".", "-")

	// 6. Remove other special chars (except colon, dash, and comma)
	// Keep: a-z, 0-9, dash, colon, comma
	s = reSpecialChars.ReplaceAllString(s, "")

	return s
}

// NormalizeTagFilter normalizes a TagFilter for consistent querying.
// Applies normalization to both Type and Value fields.
func NormalizeTagFilter(filter database.TagFilter) database.TagFilter {
	return database.TagFilter{
		Type:  NormalizeTag(filter.Type),
		Value: NormalizeTag(filter.Value),
	}
}

var CanonicalTagDefinitions = map[TagType][]TagValue{
	TagTypeInput: {
		// Joystick types
		// - 2h/2v: 2-way horizontal/vertical (e.g., Pong paddles)
		// - 3/4/8: Number of directional positions
		// - double: Two joysticks (e.g., twin-stick shooters)
		// - rotary: Rotates continuously (e.g., Tempest spinner)
		TagInputJoystick2H, TagInputJoystick2V, TagInputJoystick3, TagInputJoystick4, TagInputJoystick8,
		TagInputJoystickDouble, TagInputJoystickRotary,

		// Other input devices
		TagInputStickTwin, TagInputTrackball, TagInputPaddle, TagInputSpinner, TagInputWheel, TagInputDial,
		TagInputLightgun, TagInputOptical,

		// Positional controls - crank-based controls with fixed positions
		TagInputPositional2, TagInputPositional3,

		// Buttons - number of in-game action buttons
		// Note: pneumatic = air-pressure activated button (arcade boxing games)
		TagInputButtons1, TagInputButtons2, TagInputButtons3, TagInputButtons4, TagInputButtons5, TagInputButtons6,
		TagInputButtons7, TagInputButtons8, TagInputButtons11, TagInputButtons12, TagInputButtons19, TagInputButtons23,
		TagInputButtons27, TagInputButtonsPneumatic,

		// Pedals - foot controls
		TagInputPedals1, TagInputPedals2,

		// Other input types
		TagInputPuncher, // Physical punching bag input
		TagInputMotion,  // Motion detection/accelerometer
	},
	TagTypePlayers: {
		// Player counts
		TagPlayers1, TagPlayers2, TagPlayers3, TagPlayers4, TagPlayers5, TagPlayers6,
		TagPlayers7, TagPlayers8, TagPlayers9, TagPlayers10, TagPlayers12,

		// Player modes
		TagPlayersMMO,  // Massively multiplayer online
		TagPlayersVS,   // Versus/competitive
		TagPlayersCoop, // Cooperative
		TagPlayersAlt,  // Alternating turns
	},

	TagTypeGameGenre: {
		// Genre hierarchy uses 1-2 levels: "genre" or "genre:subgenre"
		// Examples: "action", "action:platformer", "sports:wrestling"
		// Action
		TagGameGenreAction, TagGameGenreActionPlatformer, TagGameGenreActionMaze,
		TagGameGenreActionBlockbreaker, TagGameGenreActionRunAndGun, TagGameGenreActionHackAndSlash,
		TagGameGenreActionMetroidvania, TagGameGenreActionRoguelite,
		// Adventure
		TagGameGenreAdventure, TagGameGenreAdventurePointClick, TagGameGenreAdventureVisualNovel,
		TagGameGenreAdventureSurvivalHorror, TagGameGenreAdventureText,
		// Board games - digital versions of classic board/card games
		TagGameGenreBoard, TagGameGenreBoardCards, TagGameGenreBoardHanafuda,
		TagGameGenreBoardChess, TagGameGenreBoardShougi,
		TagGameGenreBoardGo, TagGameGenreBoardMahjong, TagGameGenreBoardReversi, TagGameGenreBoardOthello,
		TagGameGenreBoardBackgammon, TagGameGenreBoardParty, TagGameGenreBoardJankenpon,

		// Fighting
		TagGameGenreBrawler,       // Beat'em up (Streets of Rage, Double Dragon)
		TagGameGenreFighting,      // Fighting games
		TagGameGenreFightingMelee, // Close-quarters fighting

		// Minigames
		TagGameGenreMinigames,

		// Parlor games - arcade/amusement games
		TagGameGenreParlor, TagGameGenreParlorPinball, TagGameGenreParlorJackpot, TagGameGenreParlorPachinko,
		TagGameGenreParlorDarts, TagGameGenreParlorBowling, TagGameGenreParlorBilliards,
		TagGameGenreParlorMogurataiji, // Whac-A-Mole
		TagGameGenreParlorKiddieride,  // Coin-operated kiddie rides
		TagGameGenreParlorMechanical,  // Mechanical arcade games
		// Quiz
		TagGameGenreQuiz,

		// Racing
		TagGameGenreRacing, TagGameGenreRacingCombat, TagGameGenreRacingDriving,

		// RPG - Role-Playing Game
		TagGameGenreRPG,
		TagGameGenreRPGAction,         // Action RPG
		TagGameGenreRPGJapanese,       // JRPG (Japanese RPG)
		TagGameGenreRPGStrategy,       // Strategy RPG / Tactical RPG
		TagGameGenreRPGDungeonCrawler, // Dungeon crawler
		TagGameGenreRPGMMO,            // MMO RPG

		// Rhythm
		TagGameGenreRhythm, TagGameGenreRhythmKaraoke, TagGameGenreRhythmDance,

		// Shoot'em up (shmup)
		TagGameGenreShmup,
		TagGameGenreShmupHorizontal, // Horizontal scrolling
		TagGameGenreShmupVertical,   // Vertical scrolling
		TagGameGenreShmupIsometric,  // Isometric
		TagGameGenreShmupDanmaku,    // Bullet hell
		// Shooting
		TagGameGenreShooting, TagGameGenreShootingGallery, TagGameGenreShootingRail,
		TagGameGenreShootingFPS, TagGameGenreShootingTPS,
		// Puzzle
		TagGameGenrePuzzle, TagGameGenrePuzzleDrop, TagGameGenrePuzzleMind,
		// Simulation
		TagGameGenreSim, TagGameGenreSimStrategy, TagGameGenreSimCardgame,
		TagGameGenreSimFlight, TagGameGenreSimTrain, TagGameGenreSimDate, TagGameGenreSimOtome,
		TagGameGenreSimLife, TagGameGenreSimFarm, TagGameGenreSimPet, TagGameGenreSimFishing,
		TagGameGenreSimGod, TagGameGenreSimDerby, TagGameGenreSimBuilding, TagGameGenreSimCooking,
		// Sports
		TagGameGenreSports, TagGameGenreSportsSoccer, TagGameGenreSportsBasketball,
		TagGameGenreSportsBaseball, TagGameGenreSportsVolleyball, TagGameGenreSportsRugby,
		TagGameGenreSportsFootball, TagGameGenreSportsDodgeball, TagGameGenreSportsHockey,
		TagGameGenreSportsSkiing, TagGameGenreSportsSkateboarding, TagGameGenreSportsSnowboarding,
		TagGameGenreSportsTennis, TagGameGenreSportsPingpong, TagGameGenreSportsPaddle,
		TagGameGenreSportsSquash, TagGameGenreSportsBadminton, TagGameGenreSportsFlyingdisc,
		TagGameGenreSportsCycling, TagGameGenreSportsFormula1,
		TagGameGenreSportsRally, TagGameGenreSportsNascar, TagGameGenreSportsMotoGP, TagGameGenreSportsMotocross,
		TagGameGenreSportsKarting, TagGameGenreSportsJetski, TagGameGenreSportsGolf, TagGameGenreSportsCricket,
		TagGameGenreSportsBoxing, TagGameGenreSportsKickboxing, TagGameGenreSportsWrestling, TagGameGenreSportsSumo,
		TagGameGenreSportsKarate, TagGameGenreSportsJudo, TagGameGenreSportsKendo, TagGameGenreSportsTaekwondo,
		TagGameGenreSportsMMA, TagGameGenreSportsDecathlon, TagGameGenreSportsRunning, TagGameGenreSportsArchery,
		TagGameGenreSportsSwimming, TagGameGenreSportsRowing, TagGameGenreSportsKayak, TagGameGenreSportsSurf,
		// Not a game - software/applications that aren't games
		TagGameGenreNotAGame,
		TagGameGenreNotAGameEducational, // Educational software
		TagGameGenreNotAGameDrawing,     // Drawing/paint programs
		TagGameGenreNotAGamePopcorn,     // Popcorn vending machines (arcade)
		TagGameGenreNotAGamePurikura,    // Photo sticker booths
		TagGameGenreNotAGameRedemption,  // Ticket redemption games
		TagGameGenreNotAGameMedia,       // Media playback
		TagGameGenreNotAGameMagazine,    // Digital magazines
		TagGameGenreNotAGameApplication, // General applications
		TagGameGenreNotAGameTest,        // Test/diagnostic software
		TagGameGenreNotAGameSDK,         // Software development kits
		TagGameGenreNotAGameSlideshow,   // Picture slideshows
		TagGameGenreNotAGameSound,       // Audio-only content
	},

	TagTypeAddon: {
		// External peripherals and add-ons recommended or required for gameplay.
		// Organized by: peripheral, controller, lightgun, mouse, keyboard, multitap,
		// link (cables), expansion, lockon, backup, online, and miscellaneous.
		// Peripherals
		TagAddonPeripheralMegaCD, TagAddonPeripheralSuper32X, TagAddonPeripheralDisksystem,
		TagAddonPeripheralSufami, TagAddonPeripheral64DD, TagAddonPeripheralCDROMROM,
		// Controllers
		TagAddonControllerBikehandle, TagAddonControllerPaddlecontrol, TagAddonControllerSportspad,
		TagAddonController6Button, TagAddonControllerActivator, TagAddonController3DPad,
		TagAddonControllerMissionstick, TagAddonControllerTwinstick, TagAddonControllerArcaderacer,
		TagAddonControllerXE1AP, TagAddonControllerAvenuepad3, TagAddonControllerAvenuepad6,
		TagAddonController10Key, TagAddonControllerSBOM, TagAddonControllerArkanoid,
		TagAddonControllerFamilytrainerA, TagAddonControllerFamilytrainerB, TagAddonControllerReeladapter,
		TagAddonControllerPowerglove, TagAddonControllerMahjong, TagAddonControllerHypershot,
		TagAddonControllerDDR, TagAddonControllerTaikanfamicom, TagAddonControllerHardwarebike,
		TagAddonControllerPachinko, TagAddonControllerHissatsupachinko, TagAddonControllerPashislot,
		TagAddonControllerSankyoff, TagAddonControllerHoritrack, TagAddonControllerUforce,
		TagAddonControllerSmash, TagAddonControllerDenshadego, TagAddonControllerComputrainer,
		TagAddonControllerLifefitness, TagAddonControllerTaptapmat, TagAddonControllerTeevgolf,
		TagAddonControllerLasabirdie, TagAddonControllerGrip, TagAddonControllerTsurikon64,
		TagAddonControllerPartytap, TagAddonControllerClimberstick, TagAddonControllerJuujikeycover,
		TagAddonControllerJCart, TagAddonControllerRumble,
		// Light guns
		TagAddonLightgunLightphaser, TagAddonLightgunMenacer, TagAddonLightgunVirtuagun,
		TagAddonLightgunZapper, TagAddonLightgunSuperscope, TagAddonLightgunJustifier,
		TagAddonLightgunLaserscope, TagAddonLightgunBandaihypershot, TagAddonLightgunGamegun,
		TagAddonLightgunAP74,
		// Mouse
		TagAddonMouseMD, TagAddonMouseSaturn, TagAddonMouseSFC, TagAddonMousePCE, TagAddonMousePCFX, TagAddonMouseN64,
		// Keyboard
		TagAddonKeyboardSaturn, TagAddonKeyboardFC, TagAddonKeyboardN64, TagAddonKeyboardWorkboy,
		// Multitap
		TagAddonMultitapSegatap, TagAddonMultitap6Player, TagAddonMultitap4PlayersAdaptor,
		TagAddonMultitapSuper, TagAddonMultitapPCE, TagAddonMultitap4WayPlay,
		// Link cables
		TagAddonLinkTaisencable, TagAddonLinkTaisensaturn, TagAddonLinkGamelinkcable,
		TagAddonLinkFourplayeradapter, TagAddonLinkComcable, TagAddonLinkLinkup,
		TagAddonLinkNGPLink, TagAddonLinkRadiounitwireless, TagAddonLinkSetsuzoku,
		TagAddonLinkSenyoucord, TagAddonLinkBB2Interface, TagAddonLinkVoicerkun,
		TagAddonLinkMidiinterface,
		// Expansion
		TagAddonExpansionFMSoundunit, TagAddonExpansionROMCartridge, TagAddonExpansionRAMCartridge1M,
		TagAddonExpansionRAMCartridge4M, TagAddonExpansionMoviecard, TagAddonExpansionMemorypak,
		TagAddonExpansionSamegame, TagAddonExpansionExpansionpak, TagAddonExpansionMegaLD,
		TagAddonExpansionLDROMROM, TagAddonExpansionSupersystemcard, TagAddonExpansionArcadecard,
		TagAddonExpansionGamesexpresscard,
		// Lock-on
		TagAddonLockonSupergameboy, TagAddonLockonTransferpak, TagAddonLockonDatach,
		TagAddonLockonDeckenhancer, TagAddonLockonOyagame, TagAddonLockonQtai,
		TagAddonLockonKaraokestudio, TagAddonLockonSXT2, TagAddonLockonTristar,
		// Backup
		TagAddonBackupBackupramcart, TagAddonBackupPowermemory, TagAddonBackupFDDSaturn,
		TagAddonBackupControllerpak, TagAddonBackupSmartmediacard, TagAddonBackupDatarecorder,
		TagAddonBackupBattlebox, TagAddonBackupTennokoe, TagAddonBackupMemorybase128,
		TagAddonBackupTurbofile,
		// Online
		TagAddonOnlineMegamodem, TagAddonOnlineMegaanser, TagAddonOnlineToshokan,
		TagAddonOnlineSegachannel, TagAddonOnlineSaturnmodem, TagAddonOnlineNetlink,
		TagAddonOnlineXband, TagAddonOnlineMeganet, TagAddonOnlineTeleplay,
		TagAddonOnlineNetworksystem, TagAddonOnlineNDM24, TagAddonOnlineSatellaview,
		TagAddonOnlineNintendopower, TagAddonOnlineSeganet, TagAddonOnlineRandnetmodem,
		// Other addons
		TagAddonVibrationRumblepak,
		TagAddonGlasses3DGlasses, TagAddonGlassesSegaVR, TagAddonGlasses3DSystem, TagAddonGlasses3DGoggle,
		TagAddonMicFC, TagAddonMicN64, TagAddonMicVRS,
		TagAddonDrawingGraphicboard, TagAddonDrawingIllustbooster, TagAddonDrawingOekakids,
		TagAddonHealthCatalyst, TagAddonHealthBiosensor,
		TagAddonMidiMiracle, TagAddonMidiPianokeyboard,
		TagAddonRobGyro, TagAddonRobBlock,
		TagAddonPrinterPocketprinter, TagAddonPrinterPrintbooster,
		TagAddonBarcodeboy, TagAddonRSS, TagAddonPocketcamera, TagAddonCapturecassette,
		TagAddonPhotoreader, TagAddonDevelobox, TagAddonTeststation,
	},
	TagTypeEmbedded: {
		// Embedded extra hardware inside the cartridge itself.
		// Includes backup systems (save memory), enhancement chips, and special slots.

		// Backup systems - save memory types
		TagEmbeddedBackupBattery, TagEmbeddedBackupFlashRAM, TagEmbeddedBackupFeRAM, TagEmbeddedBackupEEPROM,

		// Enhancement chips - extra processors/coprocessors
		TagEmbeddedChipRAM, TagEmbeddedChipRTC, TagEmbeddedChipSVP, TagEmbeddedChipMMC5,
		TagEmbeddedChipDSP1, TagEmbeddedChipDSP1A, TagEmbeddedChipDSP1B, TagEmbeddedChipDSP2,
		TagEmbeddedChipDSP3, TagEmbeddedChipDSP4,
		TagEmbeddedChipSA1, TagEmbeddedChipSDD1, TagEmbeddedChipSFX1, TagEmbeddedChipSFX2,
		TagEmbeddedChipOBC1,
		TagEmbeddedChipVRC6, TagEmbeddedChipVRC7, TagEmbeddedChipN163, TagEmbeddedChipFME7,
		TagEmbeddedChip5A, TagEmbeddedChip5B,
		TagEmbeddedChipM50805, TagEmbeddedChip7755, TagEmbeddedChip7756, TagEmbeddedChipCX4,
		TagEmbeddedChipSPC7110,
		TagEmbeddedChipST010, TagEmbeddedChipST011, TagEmbeddedChipST018,

		// Slots - physical connectors/ports built into the cartridge
		TagEmbeddedSlotRJ11,       // RJ-11 telephone port (Xband modem)
		TagEmbeddedSlotJCart,      // Codemasters J-Cart (controller ports on cart)
		TagEmbeddedSlotLockon,     // Sonic & Knuckles lock-on technology
		TagEmbeddedSlotKogame,     // Sunsoft child cartridge slot
		TagEmbeddedSlotGameboy,    // GameBoy cartridge slot
		TagEmbeddedSlotGamelink,   // Link cable port
		TagEmbeddedSlotSmartmedia, // SmartMedia card slot

		// Other embedded hardware
		TagEmbeddedLED,         // LED indicator
		TagEmbeddedGBKiss,      // Hudson GB Kiss (IR communication)
		TagEmbeddedPocketsonar, // Bandai Pocket Sonar (fishing game sonar)
	},
	TagTypeSave: {
		TagSaveBackup,   // Battery/memory-based save system
		TagSavePassword, // Password-based progression (no save memory)
	},

	TagTypeArcadeBoard: {
		// Arcade system boards - specific hardware platforms for arcade games
		// CAPCOM
		TagArcadeBoardCapcomCPS, TagArcadeBoardCapcomCPSDash, TagArcadeBoardCapcomCPSChanger,
		TagArcadeBoardCapcomCPS2, TagArcadeBoardCapcomCPS3,
		// SEGA
		TagArcadeBoardSegaVCO, TagArcadeBoardSegaSystem1, TagArcadeBoardSegaSystem2,
		TagArcadeBoardSegaSystem16,
		TagArcadeBoardSegaSystem16A, TagArcadeBoardSegaSystem16B, TagArcadeBoardSegaSystem16C,
		TagArcadeBoardSegaSystem18,
		TagArcadeBoardSegaSystem24, TagArcadeBoardSegaSystem32, TagArcadeBoardSegaMulti32,
		TagArcadeBoardSegaSystemC,
		TagArcadeBoardSegaSystemC2, TagArcadeBoardSegaSystemE, TagArcadeBoardSegaXBoard,
		TagArcadeBoardSegaYBoard, TagArcadeBoardSegaSTV,
		TagArcadeBoardSegaMegaplay, // Sega MegaPlay
		// Irem
		TagArcadeBoardIremM10, TagArcadeBoardIremM15, TagArcadeBoardIremM27, TagArcadeBoardIremM52,
		TagArcadeBoardIremM57, TagArcadeBoardIremM58,
		TagArcadeBoardIremM62, TagArcadeBoardIremM63, TagArcadeBoardIremM72, TagArcadeBoardIremM75,
		TagArcadeBoardIremM77, TagArcadeBoardIremM81,
		TagArcadeBoardIremM82, TagArcadeBoardIremM84, TagArcadeBoardIremM85, TagArcadeBoardIremM90,
		TagArcadeBoardIremM92, TagArcadeBoardIremM97, TagArcadeBoardIremM107,
		// SNK
		TagArcadeBoardSNKMVS,
		// Taito
		TagArcadeBoardTaitoXSystem, TagArcadeBoardTaitoBSystem, TagArcadeBoardTaitoHSystem,
		TagArcadeBoardTaitoLSystem,
		TagArcadeBoardTaitoZSystem, TagArcadeBoardTaitoOSystem, TagArcadeBoardTaitoF1System,
		TagArcadeBoardTaitoF2System,
		TagArcadeBoardTaitoF3System, TagArcadeBoardTaitoLGSystem,
		// Toaplan
		TagArcadeBoardToaplanVersion1, TagArcadeBoardToaplanVersion2,
		// Jaleco
		TagArcadeBoardJalecoMegaSystem1,
		// Nintendo
		TagArcadeBoardNintendoVS, TagArcadeBoardNintendoNSS,
	},
	TagTypeCompatibility: {
		// SEGA systems
		TagCompatibilitySG1000, TagCompatibilitySG1000SC3000, TagCompatibilitySG1000Othello,
		TagCompatibilityMark3, TagCompatibilityMark3MyCard, TagCompatibilityMark3EPMyCard,
		TagCompatibilityMark3TheSegaCard,
		TagCompatibilityMark3TheMegaCartridge, TagCompatibilityMark3SilverCartridge,
		TagCompatibilityMark3GoldCartridge1,
		TagCompatibilityMark3GoldCartridge2, TagCompatibilityMark3GoldCartridge4,
		// Nintendo systems
		TagCompatibilityFamicom, TagCompatibilityFamicomPegasus,
		TagCompatibilityDisksystem, TagCompatibilityDisksystemDW,
		TagCompatibilityGameboy, TagCompatibilityGameboyMono, TagCompatibilityGameboyColor,
		TagCompatibilityGameboySGB, TagCompatibilityGameboyNP,
		TagCompatibilitySuperfamicom, TagCompatibilitySuperfamicomHiROM, TagCompatibilitySuperfamicomLoROM,
		TagCompatibilitySuperfamicomExHiROM, TagCompatibilitySuperfamicomExLoROM, TagCompatibilitySuperfamicomNSS,
		TagCompatibilitySuperfamicomSoundlink, TagCompatibilitySuperfamicomNP, TagCompatibilitySuperfamicomGS,
		// NEC
		TagCompatibilityPCEngine, TagCompatibilityPCEngineSupergrafx,
		// SNK
		TagCompatibilityNeogeoPocket, TagCompatibilityNeogeoPocketMono, TagCompatibilityNeogeoPocketColor,
		// Amiga
		TagCompatibilityAmigaA500, TagCompatibilityAmigaA1000, TagCompatibilityAmigaA1200,
		TagCompatibilityAmigaA2000, TagCompatibilityAmigaA3000,
		TagCompatibilityAmigaA4000, TagCompatibilityAmigaA500Plus, TagCompatibilityAmigaA600,
		TagCompatibilityAmigaCD32, TagCompatibilityAmigaCDTV,
		TagCompatibilityAmigaOCS, TagCompatibilityAmigaECS, TagCompatibilityAmigaAGA,
		// Amiga combinations
		TagCompatibilityAmigaPlus2, TagCompatibilityAmigaPlus2A, TagCompatibilityAmigaPlus3,
		TagCompatibilityAmigaA1200A4000, TagCompatibilityAmigaA2000A3000, TagCompatibilityAmigaA2024,
		TagCompatibilityAmigaA2500A3000UX,
		TagCompatibilityAmigaA4000T, TagCompatibilityAmigaA500A1000A2000, TagCompatibilityAmigaA500A1000A2000CDTV,
		TagCompatibilityAmigaA500A1200, TagCompatibilityAmigaA500A1200A2000A4000, TagCompatibilityAmigaA500A2000,
		TagCompatibilityAmigaA500A600A2000, TagCompatibilityAmigaA570, TagCompatibilityAmigaA600HD,
		TagCompatibilityAmigaAGACD32, TagCompatibilityAmigaECSAGA, TagCompatibilityAmigaOCSAGA,
		// Atari
		TagCompatibilityAtariST, TagCompatibilityAtariSTE, TagCompatibilityAtariTT,
		TagCompatibilityAtariMegaST, TagCompatibilityAtariMegaSTE,
		TagCompatibilityAtari130XE, TagCompatibilityAtariExecutive,
		TagCompatibilityAtariSTEFalcon, // Atari combinations
		// MSX
		TagCompatibilityMSXTurboRGT, TagCompatibilityMSXTurboRST,
		// Commodore
		TagCompatibilityCommodorePlus4,
		// Primo
		TagCompatibilityPrimoPrimoA, TagCompatibilityPrimoPrimoA64, TagCompatibilityPrimoPrimoB,
		TagCompatibilityPrimoPrimoB64, TagCompatibilityPrimoProprimo,
		// ColecoVision
		TagCompatibilityColecoAdam,
		// Other
		TagCompatibilityIBMPCDoctorPCJr, TagCompatibilityOsbourneOsbourne1,
		TagCompatibilityMiscOrch80, TagCompatibilityMiscPiano90,
		// Arcade
		TagCompatibilityNintendoPlaychoice10, TagCompatibilityNintendoVSDualsystem, TagCompatibilityNintendoVSUnisystem,
	},
	TagTypeDisc: {
		// Disc number for multi-disc games (which disc this file is)
		TagDisc1, TagDisc2, TagDisc3, TagDisc4, TagDisc5,
		TagDisc6, TagDisc7, TagDisc8, TagDisc9, TagDisc10,
	},

	TagTypeDiscTotal: {
		// Total number of discs in the complete set
		// Example: Final Fantasy VII disc 2 would have both disc:2 and disctotal:3
		TagDiscTotal2, TagDiscTotal3, TagDiscTotal4, TagDiscTotal5,
		TagDiscTotal6, TagDiscTotal7, TagDiscTotal8, TagDiscTotal9, TagDiscTotal10,
	},

	TagTypeBased: {
		// Games based on other media (movies, comics, TV shows)
		TagBasedManganime, TagBasedMovie, TagBasedDisney, TagBasedDND, TagBasedJurassicpark,
		TagBasedLooneytunes, TagBasedMarvel, TagBasedSimpsons, TagBasedSmurfs, TagBasedStarwars, TagBasedTMNT,
	},
	TagTypeSearch: {
		// Search metadata - franchises, featured characters, technical features, and keywords
		// that aid in game discovery

		// Franchises - game series
		TagSearchFranchiseCastlevania, TagSearchFranchiseDragonslayer, TagSearchFranchiseWonderboy,

		// Featured characters - notable characters appearing in the game
		TagSearchFeatureAlien, TagSearchFeatureAsterix, TagSearchFeatureBatman, TagSearchFeatureCompatihero,
		TagSearchFeatureDracula, TagSearchFeatureDonald, TagSearchFeatureGundam, TagSearchFeatureKuniokun,
		TagSearchFeatureMario, TagSearchFeatureMickey, TagSearchFeaturePacman, TagSearchFeatureSherlock,
		TagSearchFeatureSonic, TagSearchFeatureSpiderman, TagSearchFeatureSuperman, TagSearchFeatureXMen,

		// Screen orientation - vertical monitor games (TATE mode)
		TagSearchTateCW,  // Clockwise rotation
		TagSearchTateCCW, // Counter-clockwise rotation

		// 3D effects
		TagSearch3DStereo,   // Stereoscopic 3D (requires 3D glasses)
		TagSearch3DAnaglyph, // Anaglyph 3D (red/blue glasses)

		// Keywords - special features and attributes
		TagSearchKeywordStrip,    // Adult content (strip rewards)
		TagSearchKeywordPromo,    // Promotional/not-for-sale release
		TagSearchKeywordQSound,   // QSound audio system
		TagSearchKeywordDolby,    // Dolby Surround sound
		TagSearchKeywordRS,       // Response Sound System
		TagSearchKeywordOfficial, // Official licensed sports game
		TagSearchKeywordEndorsed, // Endorsed by public figure
		TagSearchKeywordBrand,    // Branded by company/product
	},
	TagTypeMultigame: {
		TagMultigameCompilation, // Compilation of multiple games in one title
		// Volume numbers and menu
		TagMultigameVol1, TagMultigameVol2, TagMultigameVol3, TagMultigameVol4, TagMultigameVol5,
		TagMultigameVol6, TagMultigameVol7, TagMultigameVol8, TagMultigameVol9, TagMultigameMenu,
	},

	TagTypeReboxed: {
		// Re-releases and special editions with different packaging/branding
		TagReboxedBIOS, TagReboxedBluebox, TagReboxedPurplebox, TagReboxedClassicedition, TagReboxedSegaclassic,
		TagReboxedKixxedition, TagReboxedSatakore, TagReboxedGenteiban, TagReboxedMegadrive3, TagReboxedMegadrive4,
		TagReboxedReactor, TagReboxedGopher, TagReboxedMeisaku, TagReboxedMajesco, TagReboxedMegahit,
		TagReboxedKonamiclassics, TagReboxedEAclassics, TagReboxedVideogameclassics, TagReboxedKoeibest,
		TagReboxedGamenokanzume, TagReboxedSoundware, TagReboxedPlayerschoice, TagReboxedClassicserie,
		TagReboxedKousenjuu, TagReboxedDisneysclassic, TagReboxedSNKBestcollection, TagReboxedXeye,
		TagReboxedLimitedrun, TagReboxedFamicombox, TagReboxedSuperfamicombox,
		// Bundles
		TagReboxedBundle, TagReboxedBundleGenesis,
	},
	TagTypePort: {
		TagPortArcade,
		TagPortCommodoreC64, TagPortCommodoreAmiga,
		TagPortAppleApple2, TagPortAppleMac,
		TagPortBBCMicro, TagPortDragon32, TagPortElektronika60, TagPortSpectrum, TagPortAmstrad,
		TagPortAtariAtari400, TagPortAtariAtariST, TagPortAtariAtari2600, TagPortAtariLynx, TagPortAtariJaguar,
		TagPortNECPC88, TagPortNECPC98, TagPortNECPCEngine, TagPortNECCDROMROM, TagPortNECPCFX,
		TagPortMSX, TagPortMSX2,
		TagPortSharpX1, TagPortSharpMZ700, TagPortSharpX68000,
		TagPortPC,
		TagPortSegaSG1000, TagPortSegaMark3, TagPortSegaGameGear, TagPortSegaMegadrive,
		TagPortSegaMegaCD, TagPortSegaSaturn, TagPortSegaDreamcast,
		TagPortNintendoFamicom, TagPortNintendoSuperfamicom, TagPortNintendoN64,
		TagPortNintendoGameboy, TagPortNintendoGBC, TagPortNintendoGBA,
		TagPortSonyPlayStation,
		TagPort3DO, TagPortCDI, TagPortLaseractive, TagPortFMTowns,
	},
	TagTypeLang: {
		TagLangEN, TagLangES, TagLangFR, TagLangPT, TagLangDE, TagLangIT, TagLangSV, TagLangNL,
		TagLangDA, TagLangNO, TagLangFI,
		TagLangCS, TagLangSL, TagLangRU, TagLangPL, TagLangJA, TagLangZH, TagLangCH,
		TagLangKO,
		TagLangAR, TagLangBG, TagLangBS, TagLangCY, TagLangEL, TagLangEO, TagLangET, TagLangFA, TagLangGA, TagLangGU,
		TagLangHE, TagLangHI, TagLangHR, TagLangHU, TagLangIS, TagLangLT, TagLangLV, TagLangMS, TagLangRO, TagLangSK,
		TagLangSQ, TagLangSR, TagLangTH, TagLangTR, TagLangUR, TagLangVI, TagLangYI,
		// Language variants
		TagLangZHTrad, TagLangZHHans, TagLangPTBR,
	},
	TagTypeUnfinished: {
		// Development status - pre-release or incomplete builds
		TagUnfinishedAlpha, // Alpha version
		// Beta versions
		TagUnfinishedBeta, TagUnfinishedBeta1, TagUnfinishedBeta2, TagUnfinishedBeta3,
		TagUnfinishedBeta4, TagUnfinishedBeta5,
		// Prototype builds
		TagUnfinishedProto, TagUnfinishedProto1, TagUnfinishedProto2, TagUnfinishedProto3,
		TagUnfinishedProto4,
		TagUnfinishedDemo, TagUnfinishedDemo1, TagUnfinishedDemo2, // Demo versions
		TagUnfinishedDemoAuto,    // Auto-playing demo
		TagUnfinishedDemoKiosk,   // Kiosk demo (store display)
		TagUnfinishedSample,      // Sample version
		TagUnfinishedDebug,       // Debug build
		TagUnfinishedCompetition, // Competition version
		TagUnfinishedPreview,     // Preview version (our addition)
		TagUnfinishedPrerelease,  // Pre-release version (our addition)
		// Demo variants
		TagUnfinishedDemoPlayable, TagUnfinishedDemoRolling, TagUnfinishedDemoSlideshow,
	},
	TagTypeRerelease: {
		// Digital re-releases and collections on modern platforms
		// Nintendo
		TagRereleaseVirtualconsoleWii, TagRereleaseVirtualconsoleWiiU, TagRereleaseVirtualconsole3DS,
		TagRereleaseSwitchonline, TagRereleaseEreader, TagRereleaseAnimalcrossing, TagRereleaseSupermario25,

		// Publisher collections
		TagRereleaseCapcomtown,
		TagRereleaseNamcoanthology1, TagRereleaseNamcoanthology2,
		TagRereleaseNamcot1, TagRereleaseNamcot2,
		TagRereleaseCastlevaniaanniversary, TagRereleaseCastlevaniaadvance, TagRereleaseContraanniversary,
		TagRereleaseCowabunga, TagRereleaseKonamicollectors, TagRereleaseDariuscozmic,
		TagRereleaseRockmanclassic1, TagRereleaseRockmanclassic2, TagRereleaseRockmanclassicX,
		TagRereleaseRockmanclassicX2,
		TagRereleaseSeikendensetsu, TagRereleaseDisneyclassic, TagRereleaseBubsytwofur,
		TagRereleaseBlizzardarcadecollection,
		TagRereleaseQubyte, TagRereleaseProjectegg, TagRereleaseLimitedrun, TagRereleaseIam8bit,
		TagRereleaseEvercadeOlivertwins,
		TagRereleaseSteam, TagRereleaseSonicclassic, TagRereleaseSonicmegacollection,
		TagRereleaseMDClassics, TagRereleaseSmashpack,
		TagRereleaseSegaages, TagRereleaseSegaages2500,
		TagRerelease3DFukkoku,
		TagRereleaseMDMini1, TagRereleaseMDMini2,
		TagRereleaseSFCMini,
		TagRereleaseGamenokanzume1, TagRereleaseGamenokanzume2,
		TagRereleaseFightnightround2,
	},
	TagTypeRev: {
		TagRev1, TagRev2, TagRev3, TagRev4, TagRev5,
		TagRevA, TagRevB, TagRevC, TagRevD, TagRevE, TagRevG,
		// Sub-versions (dotted versions like v1.0, v1.1)
		TagRev1_0, TagRev1_1, TagRev1_2, TagRev1_3, TagRev1_4, TagRev1_5, TagRev1_6, TagRev1_7, TagRev1_8, TagRev1_9,
		TagRev2_0, TagRev2_1, TagRev2_2, TagRev2_3, TagRev2_4, TagRev2_5, TagRev2_6, TagRev2_7, TagRev2_8, TagRev2_9,
		TagRev3_0, TagRev3_1, TagRev3_2, TagRev3_3, TagRev3_4, TagRev3_5,
		TagRev4_0, TagRev4_1, TagRev4_2,
		TagRev5_0, TagRev5_1, TagRev5_2,
		// Program revisions (NES-specific)
		TagRevPRG, TagRevPRG0, TagRevPRG1, TagRevPRG2, TagRevPRG3,
	},
	TagTypeSet: {
		TagSet1, TagSet2, TagSet3, TagSet4, TagSet5, TagSet6, TagSet7, TagSet8,
	},
	TagTypeAlt: {
		TagAlt1, TagAlt2, TagAlt3,
	},
	TagTypeUnlicensed: {
		// Unofficial/unlicensed releases
		// Note: Use flat "unlicensed" when specific type is unknown
		TagUnlicensed,               // Generic unlicensed (specific type unknown)
		TagUnlicensedBootleg,        // Unauthorized copy/pirate
		TagUnlicensedHack,           // ROM hack/modification
		TagUnlicensedClone,          // Hardware clone system
		TagUnlicensedTranslation,    // Fan translation (current/generic)
		TagUnlicensedTranslationOld, // Outdated fan translation (T-)
		TagUnlicensedAftermarket,    // Made after original market cycle ended
		// Publisher-specific
		TagUnlicensedSachen, // Sachen unlicensed (NES)
	},

	TagTypeMameParent: {
		// MAME parent ROM relationship (empty - values are dynamic ROM names)
	},

	TagTypeRegion: {
		// Release regions - mix of No-Intro and TOSEC conventions
		TagRegionWorld, TagRegionUS, TagRegionEU, TagRegionJP, TagRegionAsia, TagRegionAU,
		TagRegionBR, TagRegionCA, TagRegionCN,
		TagRegionFR, TagRegionDE, TagRegionHK, TagRegionIT, TagRegionKR, TagRegionNL, TagRegionES,
		TagRegionSE, TagRegionPL, TagRegionFI,
		TagRegionDK, TagRegionPT, TagRegionNO,
		// TOSEC regions
		TagRegionAE, TagRegionAL, TagRegionAS, TagRegionAT, TagRegionBA, TagRegionBE, TagRegionBG,
		TagRegionCH, TagRegionCL, TagRegionCS,
		TagRegionCY, TagRegionCZ, TagRegionEE, TagRegionEG, TagRegionGB, TagRegionGR, TagRegionHR,
		TagRegionHU, TagRegionID, TagRegionIE,
		TagRegionIL, TagRegionIN, TagRegionIR, TagRegionIS, TagRegionJO, TagRegionLT, TagRegionLU,
		TagRegionLV, TagRegionMN, TagRegionMX,
		TagRegionMY, TagRegionNP, TagRegionNZ, TagRegionOM, TagRegionPE, TagRegionPH, TagRegionQA,
		TagRegionRO, TagRegionRU, TagRegionSG,
		TagRegionSI, TagRegionSK, TagRegionTH, TagRegionTR, TagRegionTW, TagRegionVN, TagRegionYU,
		TagRegionZA,
	},
	TagTypeYear: {
		// Release year - supports exact years and wildcards (e.g., "198x" for 1980-1989)
		TagYear1970, TagYear1971, TagYear1972, TagYear1973, TagYear1974, TagYear1975, TagYear1976,
		TagYear1977, TagYear1978, TagYear1979,
		TagYear1980, TagYear1981, TagYear1982, TagYear1983, TagYear1984, TagYear1985, TagYear1986,
		TagYear1987, TagYear1988, TagYear1989,
		TagYear1990, TagYear1991, TagYear1992, TagYear1993, TagYear1994, TagYear1995, TagYear1996,
		TagYear1997, TagYear1998, TagYear1999,
		TagYear2000, TagYear2001, TagYear2002, TagYear2003, TagYear2004, TagYear2005, TagYear2006,
		TagYear2007, TagYear2008, TagYear2009,
		TagYear2010, TagYear2011, TagYear2012, TagYear2013, TagYear2014, TagYear2015, TagYear2016,
		TagYear2017, TagYear2018, TagYear2019,
		TagYear2020, TagYear2021, TagYear2022, TagYear2023, TagYear2024, TagYear2025, TagYear2026,
		TagYear2027, TagYear2028, TagYear2029,
		TagYear19XX, TagYear197X, TagYear198X, TagYear199X, TagYear20XX, TagYear200X, TagYear201X,
		TagYear202X,
	},
	TagTypeVideo: {
		// Video formats and display standards
		// TV standards
		TagVideoNTSC, TagVideoPAL, TagVideoPAL60,
		TagVideoNTSCPAL, TagVideoPALNTSC, // Dual-region support

		// PC graphics standards
		TagVideoCGA, TagVideoEGA, TagVideoHGC, TagVideoMCGA, TagVideoMDA, TagVideoSVGA, TagVideoVGA, TagVideoXGA,
	},

	TagTypeCopyright: {
		// TOSEC copyright status codes
		// Format: <type>-<restriction> where restriction can be 'r' (registered trademark)
		TagCopyrightCW,  // Cardware
		TagCopyrightCWR, // Cardware (registered)
		TagCopyrightFW,  // Freeware
		TagCopyrightGW,  // Giftware
		TagCopyrightGWR, // Giftware (registered)
		TagCopyrightLW,  // Linkware
		TagCopyrightPD,  // Public domain
		TagCopyrightSW,  // Shareware
		TagCopyrightSWR, // Shareware (registered)
	},

	TagTypeDump: {
		// ROM dump quality and status (TOSEC)
		TagDumpCracked,    // Copy protection removed
		TagDumpFixed,      // Bug fixes applied
		TagDumpHacked,     // Modified/hacked
		TagDumpModified,   // Modified from original
		TagDumpPirated,    // Pirated copy
		TagDumpTrained,    // Trainer added (cheats)
		TagDumpTranslated, // Language translation applied
		TagDumpOverdump,   // Dump contains extra data
		TagDumpUnderdump,  // Incomplete dump
		TagDumpVirus,      // Contains virus
		TagDumpBad,        // Bad/corrupt dump
		TagDumpAlternate,  // Alternate version
		TagDumpVerified,   // Verified good dump
		// Dump variants
		TagDumpPending, TagDumpChecksumBad, TagDumpChecksumUnknown, TagDumpBIOS,
		TagDumpHackedFFE, TagDumpHackedIntroRemov,
	},

	TagTypeMedia: {
		// Media type - physical format
		TagMediaDisc, // Optical disc (CD, DVD, etc.)
		TagMediaDisk, // Magnetic disk (floppy)
		TagMediaFile, // File-based
		TagMediaPart, // Multi-part file
		TagMediaSide, // Side of disk/tape
		TagMediaTape, // Cassette tape
		// Additional media types
		TagMediaCart, TagMediaN64DD, TagMediaFDS, TagMediaEReader, TagMediaMultiboot,
	},

	TagTypeExtension: {
		// File extensions - dynamically populated based on system configurations
		// Note: Actual values come from platform-specific supported extensions
	},

	TagTypeEdition: {
		// Edition markers - indicates presence of edition/version words that are stripped from slugs
		// These are generic markers without specific descriptors (e.g., "Special", "Ultimate")
		// TODO: could add specific edition tags
		TagEditionVersion, // "Version" or equivalent in any language
		TagEditionEdition, // "Edition" or equivalent in any language
	},

	TagTypePerspective: {
		// Camera perspective and view angle
		// Describes how the player views the game world
		TagPerspectiveFirstperson, TagPerspectiveThirdperson, TagPerspectiveTopdown,
		TagPerspectiveIsometric, TagPerspectiveFixedcamera,
		// Sidescrolling with sub-categories
		TagPerspectiveSidescrollHorizontal, TagPerspectiveSidescrollVertical,
	},

	TagTypeArt: {
		// Art style and visual presentation
		// Base dimensions - games can have both a dimension tag and a style tag
		TagArt2D, TagArt3D,
		// Art styles
		TagArtPixelart, TagArtCelshaded, TagArtVector, TagArtDigitized, TagArtHanddrawn,
	},

	TagTypeAccessibility: {
		// Accessibility features for players with disabilities
		// Hierarchical structure: accessibility:category:feature
		// Visual accessibility
		TagAccessibilityVisualColorblindMode, TagAccessibilityVisualHighContrast,
		TagAccessibilityVisualTextSizeAdjust,
		// Audio accessibility
		TagAccessibilityAudioSubtitles, TagAccessibilityAudioMonoAudio,
		TagAccessibilityAudioVisualCues,
		// Input accessibility
		TagAccessibilityInputRemappableControls, TagAccessibilityInputOneButtonMode,
	},
}
