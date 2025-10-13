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

// This file contains constants for all canonical tag values.
// Use these constants when referring to specific tags in code for type safety.

// Input tag values
const (
	TagInputJoystick2H       TagValue = "joystick:2h"
	TagInputJoystick2V       TagValue = "joystick:2v"
	TagInputJoystick3        TagValue = "joystick:3"
	TagInputJoystick4        TagValue = "joystick:4"
	TagInputJoystick8        TagValue = "joystick:8"
	TagInputJoystickDouble   TagValue = "joystick:double"
	TagInputJoystickRotary   TagValue = "joystick:rotary"
	TagInputStickTwin        TagValue = "stick:twin"
	TagInputTrackball        TagValue = "trackball"
	TagInputPaddle           TagValue = "paddle"
	TagInputSpinner          TagValue = "spinner"
	TagInputWheel            TagValue = "wheel"
	TagInputDial             TagValue = "dial"
	TagInputLightgun         TagValue = "lightgun"
	TagInputOptical          TagValue = "optical"
	TagInputPositional2      TagValue = "positional:2"
	TagInputPositional3      TagValue = "positional:3"
	TagInputButtons1         TagValue = "buttons:1"
	TagInputButtons2         TagValue = "buttons:2"
	TagInputButtons3         TagValue = "buttons:3"
	TagInputButtons4         TagValue = "buttons:4"
	TagInputButtons5         TagValue = "buttons:5"
	TagInputButtons6         TagValue = "buttons:6"
	TagInputButtons7         TagValue = "buttons:7"
	TagInputButtons8         TagValue = "buttons:8"
	TagInputButtons11        TagValue = "buttons:11"
	TagInputButtons12        TagValue = "buttons:12"
	TagInputButtons19        TagValue = "buttons:19"
	TagInputButtons23        TagValue = "buttons:23"
	TagInputButtons27        TagValue = "buttons:27"
	TagInputButtonsPneumatic TagValue = "buttons:pneumatic"
	TagInputPedals1          TagValue = "pedals:1"
	TagInputPedals2          TagValue = "pedals:2"
	TagInputPuncher          TagValue = "puncher"
	TagInputMotion           TagValue = "motion"
)

// Players tag values
const (
	TagPlayers1    TagValue = "1"
	TagPlayers2    TagValue = "2"
	TagPlayers3    TagValue = "3"
	TagPlayers4    TagValue = "4"
	TagPlayers5    TagValue = "5"
	TagPlayers6    TagValue = "6"
	TagPlayers7    TagValue = "7"
	TagPlayers8    TagValue = "8"
	TagPlayers9    TagValue = "9"
	TagPlayers10   TagValue = "10"
	TagPlayers12   TagValue = "12"
	TagPlayersMMO  TagValue = "mmo"
	TagPlayersVS   TagValue = "vs"
	TagPlayersCoop TagValue = "coop"
	TagPlayersAlt  TagValue = "alt"
)

// Game genre tag values
const (
	TagGameGenreAction                  TagValue = "action"
	TagGameGenreActionPlatformer        TagValue = "action:platformer"
	TagGameGenreActionMaze              TagValue = "action:maze"
	TagGameGenreActionBlockbreaker      TagValue = "action:blockbreaker"
	TagGameGenreActionRunAndGun         TagValue = "action:runandgun"
	TagGameGenreActionHackAndSlash      TagValue = "action:hackandslash"
	TagGameGenreActionMetroidvania      TagValue = "action:metroidvania"
	TagGameGenreActionRoguelite         TagValue = "action:roguelite"
	TagGameGenreAdventure               TagValue = "adventure"
	TagGameGenreAdventurePointClick     TagValue = "adventure:pointandclick"
	TagGameGenreAdventureVisualNovel    TagValue = "adventure:visualnovel"
	TagGameGenreAdventureSurvivalHorror TagValue = "adventure:survivalhorror"
	TagGameGenreAdventureText           TagValue = "adventure:text"
	TagGameGenreBoard                   TagValue = "board"
	TagGameGenreBoardCards              TagValue = "board:cards"
	TagGameGenreBoardHanafuda           TagValue = "board:hanafuda"
	TagGameGenreBoardChess              TagValue = "board:chess"
	TagGameGenreBoardShougi             TagValue = "board:shougi"
	TagGameGenreBoardGo                 TagValue = "board:go"
	TagGameGenreBoardMahjong            TagValue = "board:mahjong"
	TagGameGenreBoardReversi            TagValue = "board:reversi"
	TagGameGenreBoardOthello            TagValue = "board:othello"
	TagGameGenreBoardBackgammon         TagValue = "board:backgammon"
	TagGameGenreBoardParty              TagValue = "board:party"
	TagGameGenreBoardJankenpon          TagValue = "board:jankenpon"
	TagGameGenreBrawler                 TagValue = "brawler"
	TagGameGenreFighting                TagValue = "fighting"
	TagGameGenreFightingMelee           TagValue = "fighting:melee"
	TagGameGenreMinigames               TagValue = "minigames"
	TagGameGenreParlor                  TagValue = "parlor"
	TagGameGenreParlorPinball           TagValue = "parlor:pinball"
	TagGameGenreParlorJackpot           TagValue = "parlor:jackpot"
	TagGameGenreParlorPachinko          TagValue = "parlor:pachinko"
	TagGameGenreParlorDarts             TagValue = "parlor:darts"
	TagGameGenreParlorBowling           TagValue = "parlor:bowling"
	TagGameGenreParlorBilliards         TagValue = "parlor:billiards"
	TagGameGenreParlorMogurataiji       TagValue = "parlor:mogurataiji"
	TagGameGenreParlorKiddieride        TagValue = "parlor:kiddieride"
	TagGameGenreParlorMechanical        TagValue = "parlor:mechanical"
	TagGameGenreQuiz                    TagValue = "quiz"
	TagGameGenreRacing                  TagValue = "racing"
	TagGameGenreRacingCombat            TagValue = "racing:combat"
	TagGameGenreRacingDriving           TagValue = "racing:driving"
	TagGameGenreRPG                     TagValue = "rpg"
	TagGameGenreRPGAction               TagValue = "rpg:a"
	TagGameGenreRPGJapanese             TagValue = "rpg:j"
	TagGameGenreRPGStrategy             TagValue = "rpg:s"
	TagGameGenreRPGDungeonCrawler       TagValue = "rpg:dungeoncrawler"
	TagGameGenreRPGMMO                  TagValue = "rpg:mmo"
	TagGameGenreRhythm                  TagValue = "rhythm"
	TagGameGenreRhythmKaraoke           TagValue = "rhythm:karaoke"
	TagGameGenreRhythmDance             TagValue = "rhythm:dance"
	TagGameGenreShmup                   TagValue = "shmup"
	TagGameGenreShmupHorizontal         TagValue = "shmup:h"
	TagGameGenreShmupVertical           TagValue = "shmup:v"
	TagGameGenreShmupIsometric          TagValue = "shmup:i"
	TagGameGenreShmupDanmaku            TagValue = "shmup:danmaku"
	TagGameGenreShooting                TagValue = "shooting"
	TagGameGenreShootingGallery         TagValue = "shooting:gallery"
	TagGameGenreShootingRail            TagValue = "shooting:rail"
	TagGameGenreShootingFPS             TagValue = "shooting:fps"
	TagGameGenreShootingTPS             TagValue = "shooting:tps"
	TagGameGenrePuzzle                  TagValue = "puzzle"
	TagGameGenrePuzzleDrop              TagValue = "puzzle:drop"
	TagGameGenrePuzzleMind              TagValue = "puzzle:mind"
	TagGameGenreSim                     TagValue = "sim"
	TagGameGenreSimStrategy             TagValue = "sim:strategy"
	TagGameGenreSimCardgame             TagValue = "sim:cardgame"
	TagGameGenreSimFlight               TagValue = "sim:flight"
	TagGameGenreSimTrain                TagValue = "sim:train"
	TagGameGenreSimDate                 TagValue = "sim:date"
	TagGameGenreSimOtome                TagValue = "sim:otome"
	TagGameGenreSimLife                 TagValue = "sim:life"
	TagGameGenreSimFarm                 TagValue = "sim:farm"
	TagGameGenreSimPet                  TagValue = "sim:pet"
	TagGameGenreSimFishing              TagValue = "sim:fishing"
	TagGameGenreSimGod                  TagValue = "sim:god"
	TagGameGenreSimDerby                TagValue = "sim:derby"
	TagGameGenreSimBuilding             TagValue = "sim:building"
	TagGameGenreSimCooking              TagValue = "sim:cooking"
	TagGameGenreSports                  TagValue = "sports"
	TagGameGenreSportsSoccer            TagValue = "sports:soccer"
	TagGameGenreSportsBasketball        TagValue = "sports:basketball"
	TagGameGenreSportsBaseball          TagValue = "sports:baseball"
	TagGameGenreSportsVolleyball        TagValue = "sports:volleyball"
	TagGameGenreSportsRugby             TagValue = "sports:rugby"
	TagGameGenreSportsFootball          TagValue = "sports:football"
	TagGameGenreSportsDodgeball         TagValue = "sports:dodgeball"
	TagGameGenreSportsHockey            TagValue = "sports:hockey"
	TagGameGenreSportsSkiing            TagValue = "sports:skiing"
	TagGameGenreSportsSkateboarding     TagValue = "sports:skateboarding"
	TagGameGenreSportsSnowboarding      TagValue = "sports:snowboarding"
	TagGameGenreSportsTennis            TagValue = "sports:tennis"
	TagGameGenreSportsPingpong          TagValue = "sports:pingpong"
	TagGameGenreSportsPaddle            TagValue = "sports:paddle"
	TagGameGenreSportsSquash            TagValue = "sports:squash"
	TagGameGenreSportsBadminton         TagValue = "sports:badminton"
	TagGameGenreSportsFlyingdisc        TagValue = "sports:flyingdisc"
	TagGameGenreSportsCycling           TagValue = "sports:cycling"
	TagGameGenreSportsFormula1          TagValue = "sports:formula1"
	TagGameGenreSportsRally             TagValue = "sports:rally"
	TagGameGenreSportsNascar            TagValue = "sports:nascar"
	TagGameGenreSportsMotoGP            TagValue = "sports:motogp"
	TagGameGenreSportsMotocross         TagValue = "sports:motocross"
	TagGameGenreSportsKarting           TagValue = "sports:karting"
	TagGameGenreSportsJetski            TagValue = "sports:jetski"
	TagGameGenreSportsGolf              TagValue = "sports:golf"
	TagGameGenreSportsCricket           TagValue = "sports:cricket"
	TagGameGenreSportsBoxing            TagValue = "sports:boxing"
	TagGameGenreSportsKickboxing        TagValue = "sports:kickboxing"
	TagGameGenreSportsWrestling         TagValue = "sports:wrestling"
	TagGameGenreSportsSumo              TagValue = "sports:sumo"
	TagGameGenreSportsKarate            TagValue = "sports:karate"
	TagGameGenreSportsJudo              TagValue = "sports:judo"
	TagGameGenreSportsKendo             TagValue = "sports:kendo"
	TagGameGenreSportsTaekwondo         TagValue = "sports:taekwondo"
	TagGameGenreSportsMMA               TagValue = "sports:mma"
	TagGameGenreSportsDecathlon         TagValue = "sports:decathlon"
	TagGameGenreSportsRunning           TagValue = "sports:running"
	TagGameGenreSportsArchery           TagValue = "sports:archery"
	TagGameGenreSportsSwimming          TagValue = "sports:swimming"
	TagGameGenreSportsRowing            TagValue = "sports:rowing"
	TagGameGenreSportsKayak             TagValue = "sports:kayak"
	TagGameGenreSportsSurf              TagValue = "sports:surf"
	TagGameGenreNotAGame                TagValue = "notagame"
	TagGameGenreNotAGameEducational     TagValue = "notagame:educational"
	TagGameGenreNotAGameDrawing         TagValue = "notagame:drawing"
	TagGameGenreNotAGamePopcorn         TagValue = "notagame:popcorn"
	TagGameGenreNotAGamePurikura        TagValue = "notagame:purikura"
	TagGameGenreNotAGameRedemption      TagValue = "notagame:redemption"
	TagGameGenreNotAGameMedia           TagValue = "notagame:media"
	TagGameGenreNotAGameMagazine        TagValue = "notagame:magazine"
	TagGameGenreNotAGameApplication     TagValue = "notagame:application"
	TagGameGenreNotAGameTest            TagValue = "notagame:test"
	TagGameGenreNotAGameSDK             TagValue = "notagame:sdk"
	TagGameGenreNotAGameSlideshow       TagValue = "notagame:slideshow"
	TagGameGenreNotAGameSound           TagValue = "notagame:sound"
)

// Save tag values
const (
	TagSaveBackup   TagValue = "backup"
	TagSavePassword TagValue = "password"
)

// Region tag values
const (
	TagRegionWorld TagValue = "world"
	TagRegionUS    TagValue = "us"
	TagRegionEU    TagValue = "eu"
	TagRegionJP    TagValue = "jp"
	TagRegionAsia  TagValue = "asia"
	TagRegionAU    TagValue = "au"
	TagRegionBR    TagValue = "br"
	TagRegionCA    TagValue = "ca"
	TagRegionCN    TagValue = "cn"
	TagRegionFR    TagValue = "fr"
	TagRegionDE    TagValue = "de"
	TagRegionHK    TagValue = "hk"
	TagRegionIT    TagValue = "it"
	TagRegionKR    TagValue = "kr"
	TagRegionNL    TagValue = "nl"
	TagRegionES    TagValue = "es"
	TagRegionSE    TagValue = "se"
	TagRegionPL    TagValue = "pl"
	TagRegionFI    TagValue = "fi"
	TagRegionDK    TagValue = "dk"
	TagRegionPT    TagValue = "pt"
	TagRegionNO    TagValue = "no"
	// TOSEC regions
	TagRegionAE TagValue = "ae"
	TagRegionAL TagValue = "al"
	TagRegionAS TagValue = "as"
	TagRegionAT TagValue = "at"
	TagRegionBA TagValue = "ba"
	TagRegionBE TagValue = "be"
	TagRegionBG TagValue = "bg"
	TagRegionCH TagValue = "ch"
	TagRegionCL TagValue = "cl"
	TagRegionCS TagValue = "cs"
	TagRegionCY TagValue = "cy"
	TagRegionCZ TagValue = "cz"
	TagRegionEE TagValue = "ee"
	TagRegionEG TagValue = "eg"
	TagRegionGB TagValue = "gb"
	TagRegionGR TagValue = "gr"
	TagRegionHR TagValue = "hr"
	TagRegionHU TagValue = "hu"
	TagRegionID TagValue = "id"
	TagRegionIE TagValue = "ie"
	TagRegionIL TagValue = "il"
	TagRegionIN TagValue = "in"
	TagRegionIR TagValue = "ir"
	TagRegionIS TagValue = "is"
	TagRegionJO TagValue = "jo"
	TagRegionLT TagValue = "lt"
	TagRegionLU TagValue = "lu"
	TagRegionLV TagValue = "lv"
	TagRegionMN TagValue = "mn"
	TagRegionMX TagValue = "mx"
	TagRegionMY TagValue = "my"
	TagRegionNP TagValue = "np"
	TagRegionNZ TagValue = "nz"
	TagRegionOM TagValue = "om"
	TagRegionPE TagValue = "pe"
	TagRegionPH TagValue = "ph"
	TagRegionQA TagValue = "qa"
	TagRegionRO TagValue = "ro"
	TagRegionRU TagValue = "ru"
	TagRegionSG TagValue = "sg"
	TagRegionSI TagValue = "si"
	TagRegionSK TagValue = "sk"
	TagRegionTH TagValue = "th"
	TagRegionTR TagValue = "tr"
	TagRegionTW TagValue = "tw"
	TagRegionVN TagValue = "vn"
	TagRegionYU TagValue = "yu"
	TagRegionZA TagValue = "za"
)

// Language tag values
const (
	TagLangEN TagValue = "en"
	TagLangES TagValue = "es"
	TagLangFR TagValue = "fr"
	TagLangPT TagValue = "pt"
	TagLangDE TagValue = "de"
	TagLangIT TagValue = "it"
	TagLangSV TagValue = "sv"
	TagLangNL TagValue = "nl"
	TagLangDA TagValue = "da"
	TagLangNO TagValue = "no"
	TagLangFI TagValue = "fi"
	TagLangCS TagValue = "cs"
	TagLangSL TagValue = "sl"
	TagLangRU TagValue = "ru"
	TagLangPL TagValue = "pl"
	TagLangJA TagValue = "ja"
	TagLangZH TagValue = "zh"
	TagLangCH TagValue = "ch"
	TagLangKO TagValue = "ko"
	TagLangAR TagValue = "ar"
	TagLangBG TagValue = "bg"
	TagLangBS TagValue = "bs"
	TagLangCY TagValue = "cy"
	TagLangEL TagValue = "el"
	TagLangEO TagValue = "eo"
	TagLangET TagValue = "et"
	TagLangFA TagValue = "fa"
	TagLangGA TagValue = "ga"
	TagLangGU TagValue = "gu"
	TagLangHE TagValue = "he"
	TagLangHI TagValue = "hi"
	TagLangHR TagValue = "hr"
	TagLangHU TagValue = "hu"
	TagLangIS TagValue = "is"
	TagLangLT TagValue = "lt"
	TagLangLV TagValue = "lv"
	TagLangMS TagValue = "ms"
	TagLangRO TagValue = "ro"
	TagLangSK TagValue = "sk"
	TagLangSQ TagValue = "sq"
	TagLangSR TagValue = "sr"
	TagLangTH TagValue = "th"
	TagLangTR TagValue = "tr"
	TagLangUR TagValue = "ur"
	TagLangVI TagValue = "vi"
	TagLangYI TagValue = "yi"
)

// Video tag values
const (
	TagVideoNTSC    TagValue = "ntsc"
	TagVideoPAL     TagValue = "pal"
	TagVideoPAL60   TagValue = "pal-60"
	TagVideoNTSCPAL TagValue = "ntsc-pal"
	TagVideoPALNTSC TagValue = "pal-ntsc"
	TagVideoCGA     TagValue = "cga"
	TagVideoEGA     TagValue = "ega"
	TagVideoHGC     TagValue = "hgc"
	TagVideoMCGA    TagValue = "mcga"
	TagVideoMDA     TagValue = "mda"
	TagVideoSVGA    TagValue = "svga"
	TagVideoVGA     TagValue = "vga"
	TagVideoXGA     TagValue = "xga"
)

// Media tag values
const (
	TagMediaDisc TagValue = "disc"
	TagMediaDisk TagValue = "disk"
	TagMediaFile TagValue = "file"
	TagMediaPart TagValue = "part"
	TagMediaSide TagValue = "side"
	TagMediaTape TagValue = "tape"
)

// Revision tag values
const (
	TagRev1 TagValue = "1"
	TagRev2 TagValue = "2"
	TagRev3 TagValue = "3"
	TagRev4 TagValue = "4"
	TagRev5 TagValue = "5"
	TagRevA TagValue = "a"
	TagRevB TagValue = "b"
	TagRevC TagValue = "c"
	TagRevD TagValue = "d"
	TagRevE TagValue = "e"
	TagRevG TagValue = "g"
)

// Unfinished tag values
const (
	TagUnfinishedAlpha       TagValue = "alpha"
	TagUnfinishedBeta        TagValue = "beta"
	TagUnfinishedBeta1       TagValue = "beta:1"
	TagUnfinishedBeta2       TagValue = "beta:2"
	TagUnfinishedBeta3       TagValue = "beta:3"
	TagUnfinishedBeta4       TagValue = "beta:4"
	TagUnfinishedBeta5       TagValue = "beta:5"
	TagUnfinishedProto       TagValue = "proto"
	TagUnfinishedProto1      TagValue = "proto:1"
	TagUnfinishedProto2      TagValue = "proto:2"
	TagUnfinishedProto3      TagValue = "proto:3"
	TagUnfinishedProto4      TagValue = "proto:4"
	TagUnfinishedDemo        TagValue = "demo"
	TagUnfinishedDemo1       TagValue = "demo:1"
	TagUnfinishedDemo2       TagValue = "demo:2"
	TagUnfinishedDemoAuto    TagValue = "demo:auto"
	TagUnfinishedDemoKiosk   TagValue = "demo:kiosk"
	TagUnfinishedSample      TagValue = "sample"
	TagUnfinishedDebug       TagValue = "debug"
	TagUnfinishedCompetition TagValue = "competition"
	TagUnfinishedPreview     TagValue = "preview"
	TagUnfinishedPrerelease  TagValue = "prerelease"
)

// Unlicensed tag values
const (
	TagUnlicensed               TagValue = "unlicensed"
	TagUnlicensedBootleg        TagValue = "bootleg"
	TagUnlicensedHack           TagValue = "hack"
	TagUnlicensedClone          TagValue = "clone"
	TagUnlicensedTranslation    TagValue = "translation"
	TagUnlicensedTranslationOld TagValue = "translation:old"
	TagUnlicensedAftermarket    TagValue = "aftermarket"
)

// Dump tag values
const (
	TagDumpCracked    TagValue = "cracked"
	TagDumpFixed      TagValue = "fixed"
	TagDumpHacked     TagValue = "hacked"
	TagDumpModified   TagValue = "modified"
	TagDumpPirated    TagValue = "pirated"
	TagDumpTrained    TagValue = "trained"
	TagDumpTranslated TagValue = "translated"
	TagDumpOverdump   TagValue = "overdump"
	TagDumpUnderdump  TagValue = "underdump"
	TagDumpVirus      TagValue = "virus"
	TagDumpBad        TagValue = "bad"
	TagDumpAlternate  TagValue = "alternate"
	TagDumpVerified   TagValue = "verified"
)

// Copyright tag values
const (
	TagCopyrightCW  TagValue = "cw"
	TagCopyrightCWR TagValue = "cw-r"
	TagCopyrightFW  TagValue = "fw"
	TagCopyrightGW  TagValue = "gw"
	TagCopyrightGWR TagValue = "gw-r"
	TagCopyrightLW  TagValue = "lw"
	TagCopyrightPD  TagValue = "pd"
	TagCopyrightSW  TagValue = "sw"
	TagCopyrightSWR TagValue = "sw-r"
)

// Addon tag values - External peripherals and add-ons
const (
	// Peripherals
	TagAddonPeripheralMegaCD     TagValue = "peripheral:megacd"
	TagAddonPeripheralSuper32X   TagValue = "peripheral:super32x"
	TagAddonPeripheralDisksystem TagValue = "peripheral:disksystem"
	TagAddonPeripheralSufami     TagValue = "peripheral:sufami"
	TagAddonPeripheral64DD       TagValue = "peripheral:64dd"
	TagAddonPeripheralCDROMROM   TagValue = "peripheral:cdromrom"

	// Controllers
	TagAddonControllerBikehandle       TagValue = "controller:bikehandle"
	TagAddonControllerPaddlecontrol    TagValue = "controller:paddlecontrol"
	TagAddonControllerSportspad        TagValue = "controller:sportspad"
	TagAddonController6Button          TagValue = "controller:6button"
	TagAddonControllerActivator        TagValue = "controller:activator"
	TagAddonController3DPad            TagValue = "controller:3dpad"
	TagAddonControllerMissionstick     TagValue = "controller:missionstick"
	TagAddonControllerTwinstick        TagValue = "controller:twinstick"
	TagAddonControllerArcaderacer      TagValue = "controller:arcaderacer"
	TagAddonControllerXE1AP            TagValue = "controller:xe1ap"
	TagAddonControllerAvenuepad3       TagValue = "controller:avenuepad3"
	TagAddonControllerAvenuepad6       TagValue = "controller:avenuepad6"
	TagAddonController10Key            TagValue = "controller:10key"
	TagAddonControllerSBOM             TagValue = "controller:sbom"
	TagAddonControllerArkanoid         TagValue = "controller:arkanoid"
	TagAddonControllerFamilytrainerA   TagValue = "controller:familytrainera"
	TagAddonControllerFamilytrainerB   TagValue = "controller:familytrainerb"
	TagAddonControllerReeladapter      TagValue = "controller:reeladapter"
	TagAddonControllerPowerglove       TagValue = "controller:powerglove"
	TagAddonControllerMahjong          TagValue = "controller:mahjong"
	TagAddonControllerHypershot        TagValue = "controller:hypershot"
	TagAddonControllerDDR              TagValue = "controller:ddr"
	TagAddonControllerTaikanfamicom    TagValue = "controller:taikanfamicom"
	TagAddonControllerHardwarebike     TagValue = "controller:hardwarebike"
	TagAddonControllerPachinko         TagValue = "controller:pachinko"
	TagAddonControllerHissatsupachinko TagValue = "controller:hissatsupachinko"
	TagAddonControllerPashislot        TagValue = "controller:pashislot"
	TagAddonControllerSankyoff         TagValue = "controller:sankyoff"
	TagAddonControllerHoritrack        TagValue = "controller:horitrack"
	TagAddonControllerUforce           TagValue = "controller:uforce"
	TagAddonControllerSmash            TagValue = "controller:smash"
	TagAddonControllerDenshadego       TagValue = "controller:denshadego"
	TagAddonControllerComputrainer     TagValue = "controller:computrainer"
	TagAddonControllerLifefitness      TagValue = "controller:lifefitness"
	TagAddonControllerTaptapmat        TagValue = "controller:taptapmat"
	TagAddonControllerTeevgolf         TagValue = "controller:teevgolf"
	TagAddonControllerLasabirdie       TagValue = "controller:lasabirdie"
	TagAddonControllerGrip             TagValue = "controller:grip"
	TagAddonControllerTsurikon64       TagValue = "controller:tsurikon64"
	TagAddonControllerPartytap         TagValue = "controller:partytap"
	TagAddonControllerClimberstick     TagValue = "controller:climberstick"
	TagAddonControllerJuujikeycover    TagValue = "controller:juujikeycover"
	TagAddonControllerJCart            TagValue = "controller:jcart"  // J-Cart (Genesis controller ports on cartridge)
	TagAddonControllerRumble           TagValue = "controller:rumble" // Rumble support

	// Light guns
	TagAddonLightgunLightphaser     TagValue = "lightgun:lightphaser"
	TagAddonLightgunMenacer         TagValue = "lightgun:menacer"
	TagAddonLightgunVirtuagun       TagValue = "lightgun:virtuagun"
	TagAddonLightgunZapper          TagValue = "lightgun:zapper"
	TagAddonLightgunSuperscope      TagValue = "lightgun:superscope"
	TagAddonLightgunJustifier       TagValue = "lightgun:justifier"
	TagAddonLightgunLaserscope      TagValue = "lightgun:laserscope"
	TagAddonLightgunBandaihypershot TagValue = "lightgun:bandaihypershot"
	TagAddonLightgunGamegun         TagValue = "lightgun:gamegun"
	TagAddonLightgunAP74            TagValue = "lightgun:ap74"

	// Mouse
	TagAddonMouseMD     TagValue = "mouse:md"
	TagAddonMouseSaturn TagValue = "mouse:saturn"
	TagAddonMouseSFC    TagValue = "mouse:sfc"
	TagAddonMousePCE    TagValue = "mouse:pce"
	TagAddonMousePCFX   TagValue = "mouse:pcfx"
	TagAddonMouseN64    TagValue = "mouse:n64"

	// Keyboard
	TagAddonKeyboardSaturn  TagValue = "keyboard:saturn"
	TagAddonKeyboardFC      TagValue = "keyboard:fc"
	TagAddonKeyboardN64     TagValue = "keyboard:n64"
	TagAddonKeyboardWorkboy TagValue = "keyboard:workboy"

	// Multitap
	TagAddonMultitapSegatap         TagValue = "multitap:segatap"
	TagAddonMultitap6Player         TagValue = "multitap:6player"
	TagAddonMultitap4PlayersAdaptor TagValue = "multitap:4playersadaptor"
	TagAddonMultitapSuper           TagValue = "multitap:super"
	TagAddonMultitapPCE             TagValue = "multitap:pce"
	TagAddonMultitap4WayPlay        TagValue = "multitap:4wayplay"

	// Link cables
	TagAddonLinkTaisencable       TagValue = "link:taisencable"
	TagAddonLinkTaisensaturn      TagValue = "link:taisensaturn"
	TagAddonLinkGamelinkcable     TagValue = "link:gamelinkcable"
	TagAddonLinkFourplayeradapter TagValue = "link:fourplayeradapter"
	TagAddonLinkComcable          TagValue = "link:comcable"
	TagAddonLinkLinkup            TagValue = "link:linkup"
	TagAddonLinkNGPLink           TagValue = "link:ngplink"
	TagAddonLinkRadiounitwireless TagValue = "link:radiounitwireless"
	TagAddonLinkSetsuzoku         TagValue = "link:setsuzoku"
	TagAddonLinkSenyoucord        TagValue = "link:senyoucord"
	TagAddonLinkBB2Interface      TagValue = "link:bb2interface"
	TagAddonLinkVoicerkun         TagValue = "link:voicerkun"
	TagAddonLinkMidiinterface     TagValue = "link:midiinterface"

	// Expansion
	TagAddonExpansionFMSoundunit      TagValue = "expansion:fmsoundunit"
	TagAddonExpansionROMCartridge     TagValue = "expansion:romcartridge"
	TagAddonExpansionRAMCartridge1M   TagValue = "expansion:ramcartridge1m"
	TagAddonExpansionRAMCartridge4M   TagValue = "expansion:ramcartridge4m"
	TagAddonExpansionMoviecard        TagValue = "expansion:moviecard"
	TagAddonExpansionMemorypak        TagValue = "expansion:memorypak"
	TagAddonExpansionSamegame         TagValue = "expansion:samegame"
	TagAddonExpansionExpansionpak     TagValue = "expansion:expansionpak"
	TagAddonExpansionMegaLD           TagValue = "expansion:megald"
	TagAddonExpansionLDROMROM         TagValue = "expansion:ldromrom"
	TagAddonExpansionSupersystemcard  TagValue = "expansion:supersystemcard"
	TagAddonExpansionArcadecard       TagValue = "expansion:arcadecard"
	TagAddonExpansionGamesexpresscard TagValue = "expansion:gamesexpresscard"

	// Lock-on
	TagAddonLockonSupergameboy  TagValue = "lockon:supergameboy"
	TagAddonLockonTransferpak   TagValue = "lockon:transferpak"
	TagAddonLockonDatach        TagValue = "lockon:datach"
	TagAddonLockonDeckenhancer  TagValue = "lockon:deckenhancer"
	TagAddonLockonOyagame       TagValue = "lockon:oyagame"
	TagAddonLockonQtai          TagValue = "lockon:qtai"
	TagAddonLockonKaraokestudio TagValue = "lockon:karaokestudio"
	TagAddonLockonSXT2          TagValue = "lockon:sxt2"
	TagAddonLockonTristar       TagValue = "lockon:tristar"

	// Backup
	TagAddonBackupBackupramcart  TagValue = "backup:backupramcart"
	TagAddonBackupPowermemory    TagValue = "backup:powermemory"
	TagAddonBackupFDDSaturn      TagValue = "backup:fddsaturn"
	TagAddonBackupControllerpak  TagValue = "backup:controllerpak"
	TagAddonBackupSmartmediacard TagValue = "backup:smartmediacard"
	TagAddonBackupDatarecorder   TagValue = "backup:datarecorder"
	TagAddonBackupBattlebox      TagValue = "backup:battlebox"
	TagAddonBackupTennokoe       TagValue = "backup:tennokoe"
	TagAddonBackupMemorybase128  TagValue = "backup:memorybase128"
	TagAddonBackupTurbofile      TagValue = "backup:turbofile"

	// Online
	TagAddonOnlineMegamodem     TagValue = "online:megamodem"
	TagAddonOnlineMegaanser     TagValue = "online:megaanser"
	TagAddonOnlineToshokan      TagValue = "online:toshokan"
	TagAddonOnlineSegachannel   TagValue = "online:segachannel"
	TagAddonOnlineSaturnmodem   TagValue = "online:saturnmodem"
	TagAddonOnlineNetlink       TagValue = "online:netlink"
	TagAddonOnlineXband         TagValue = "online:xband"
	TagAddonOnlineMeganet       TagValue = "online:meganet"
	TagAddonOnlineTeleplay      TagValue = "online:teleplay"
	TagAddonOnlineNetworksystem TagValue = "online:networksystem"
	TagAddonOnlineNDM24         TagValue = "online:ndm24"
	TagAddonOnlineSatellaview   TagValue = "online:satellaview"
	TagAddonOnlineNintendopower TagValue = "online:nintendopower"
	TagAddonOnlineSeganet       TagValue = "online:seganet"
	TagAddonOnlineRandnetmodem  TagValue = "online:randnetmodem"

	// Other addons
	TagAddonVibrationRumblepak   TagValue = "vibration:rumblepak"
	TagAddonGlasses3DGlasses     TagValue = "glasses:3dglasses"
	TagAddonGlassesSegaVR        TagValue = "glasses:segavr"
	TagAddonGlasses3DSystem      TagValue = "glasses:3dsystem"
	TagAddonGlasses3DGoggle      TagValue = "glasses:3dgoggle"
	TagAddonMicFC                TagValue = "mic:fc"
	TagAddonMicN64               TagValue = "mic:n64"
	TagAddonMicVRS               TagValue = "mic:vrs"
	TagAddonDrawingGraphicboard  TagValue = "drawing:graphicboard"
	TagAddonDrawingIllustbooster TagValue = "drawing:illustbooster"
	TagAddonDrawingOekakids      TagValue = "drawing:oekakids"
	TagAddonHealthCatalyst       TagValue = "health:catalyst"
	TagAddonHealthBiosensor      TagValue = "health:biosensor"
	TagAddonMidiMiracle          TagValue = "midi:miracle"
	TagAddonMidiPianokeyboard    TagValue = "midi:pianokeyboard"
	TagAddonRobGyro              TagValue = "rob:gyro"
	TagAddonRobBlock             TagValue = "rob:block"
	TagAddonPrinterPocketprinter TagValue = "printer:pocketprinter"
	TagAddonPrinterPrintbooster  TagValue = "printer:printbooster"
	TagAddonBarcodeboy           TagValue = "barcodeboy"
	TagAddonRSS                  TagValue = "rss"
	TagAddonPocketcamera         TagValue = "pocketcamera"
	TagAddonCapturecassette      TagValue = "capturecassette"
	TagAddonPhotoreader          TagValue = "photoreader"
	TagAddonDevelobox            TagValue = "develobox"
	TagAddonTeststation          TagValue = "teststation"
)

// Embedded tag values - Internal cartridge hardware
const (
	// Backup systems
	TagEmbeddedBackupBattery  TagValue = "backup:battery"
	TagEmbeddedBackupFlashRAM TagValue = "backup:flashram"
	TagEmbeddedBackupFeRAM    TagValue = "backup:feram"
	TagEmbeddedBackupEEPROM   TagValue = "backup:eeprom"

	// Enhancement chips
	TagEmbeddedChipRAM     TagValue = "chip:ram"
	TagEmbeddedChipRTC     TagValue = "chip:rtc"
	TagEmbeddedChipSVP     TagValue = "chip:svp"
	TagEmbeddedChipMMC5    TagValue = "chip:mmc5"
	TagEmbeddedChipDSP1    TagValue = "chip:dsp1"
	TagEmbeddedChipDSP1A   TagValue = "chip:dsp1a"
	TagEmbeddedChipDSP1B   TagValue = "chip:dsp1b"
	TagEmbeddedChipDSP2    TagValue = "chip:dsp2"
	TagEmbeddedChipDSP3    TagValue = "chip:dsp3"
	TagEmbeddedChipDSP4    TagValue = "chip:dsp4"
	TagEmbeddedChipSA1     TagValue = "chip:sa1"
	TagEmbeddedChipSDD1    TagValue = "chip:sdd1"
	TagEmbeddedChipSFX1    TagValue = "chip:sfx1"
	TagEmbeddedChipSFX2    TagValue = "chip:sfx2"
	TagEmbeddedChipOBC1    TagValue = "chip:obc1"
	TagEmbeddedChipVRC6    TagValue = "chip:vrc6"
	TagEmbeddedChipVRC7    TagValue = "chip:vrc7"
	TagEmbeddedChipN163    TagValue = "chip:n163"
	TagEmbeddedChipFME7    TagValue = "chip:fme7"
	TagEmbeddedChip5A      TagValue = "chip:5a"
	TagEmbeddedChip5B      TagValue = "chip:5b"
	TagEmbeddedChipM50805  TagValue = "chip:m50805"
	TagEmbeddedChip7755    TagValue = "chip:7755"
	TagEmbeddedChip7756    TagValue = "chip:7756"
	TagEmbeddedChipCX4     TagValue = "chip:cx4"
	TagEmbeddedChipSPC7110 TagValue = "chip:spc7110"
	TagEmbeddedChipST010   TagValue = "chip:st010"
	TagEmbeddedChipST011   TagValue = "chip:st011"
	TagEmbeddedChipST018   TagValue = "chip:st018"

	// Slots
	TagEmbeddedSlotRJ11       TagValue = "slot:rj11"
	TagEmbeddedSlotJCart      TagValue = "slot:jcart"
	TagEmbeddedSlotLockon     TagValue = "slot:lockon"
	TagEmbeddedSlotKogame     TagValue = "slot:kogame"
	TagEmbeddedSlotGameboy    TagValue = "slot:gameboy"
	TagEmbeddedSlotGamelink   TagValue = "slot:gamelink"
	TagEmbeddedSlotSmartmedia TagValue = "slot:smartmedia"

	// Other embedded hardware
	TagEmbeddedLED         TagValue = "led"
	TagEmbeddedGBKiss      TagValue = "gbkiss"
	TagEmbeddedPocketsonar TagValue = "pocketsonar"
)

// Arcade board tag values
const (
	// CAPCOM
	TagArcadeBoardCapcomCPS        TagValue = "capcom:cps"
	TagArcadeBoardCapcomCPSDash    TagValue = "capcom:cpsdash"
	TagArcadeBoardCapcomCPSChanger TagValue = "capcom:cpschanger"
	TagArcadeBoardCapcomCPS2       TagValue = "capcom:cps2"
	TagArcadeBoardCapcomCPS3       TagValue = "capcom:cps3"

	// SEGA
	TagArcadeBoardSegaVCO       TagValue = "sega:vco"
	TagArcadeBoardSegaSystem1   TagValue = "sega:system1"
	TagArcadeBoardSegaSystem2   TagValue = "sega:system2"
	TagArcadeBoardSegaSystem16  TagValue = "sega:system16"
	TagArcadeBoardSegaSystem16A TagValue = "sega:system16a"
	TagArcadeBoardSegaSystem16B TagValue = "sega:system16b"
	TagArcadeBoardSegaSystem16C TagValue = "sega:system16c"
	TagArcadeBoardSegaSystem18  TagValue = "sega:system18"
	TagArcadeBoardSegaSystem24  TagValue = "sega:system24"
	TagArcadeBoardSegaSystem32  TagValue = "sega:system32"
	TagArcadeBoardSegaMulti32   TagValue = "sega:multi32"
	TagArcadeBoardSegaSystemC   TagValue = "sega:systemc"
	TagArcadeBoardSegaSystemC2  TagValue = "sega:systemc2"
	TagArcadeBoardSegaSystemE   TagValue = "sega:systeme"
	TagArcadeBoardSegaXBoard    TagValue = "sega:xboard"
	TagArcadeBoardSegaYBoard    TagValue = "sega:yboard"
	TagArcadeBoardSegaSTV       TagValue = "sega:stv"

	// Irem
	TagArcadeBoardIremM10  TagValue = "irem:m10"
	TagArcadeBoardIremM15  TagValue = "irem:m15"
	TagArcadeBoardIremM27  TagValue = "irem:m27"
	TagArcadeBoardIremM52  TagValue = "irem:m52"
	TagArcadeBoardIremM57  TagValue = "irem:m57"
	TagArcadeBoardIremM58  TagValue = "irem:m58"
	TagArcadeBoardIremM62  TagValue = "irem:m62"
	TagArcadeBoardIremM63  TagValue = "irem:m63"
	TagArcadeBoardIremM72  TagValue = "irem:m72"
	TagArcadeBoardIremM75  TagValue = "irem:m75"
	TagArcadeBoardIremM77  TagValue = "irem:m77"
	TagArcadeBoardIremM81  TagValue = "irem:m81"
	TagArcadeBoardIremM82  TagValue = "irem:m82"
	TagArcadeBoardIremM84  TagValue = "irem:m84"
	TagArcadeBoardIremM85  TagValue = "irem:m85"
	TagArcadeBoardIremM90  TagValue = "irem:m90"
	TagArcadeBoardIremM92  TagValue = "irem:m92"
	TagArcadeBoardIremM97  TagValue = "irem:m97"
	TagArcadeBoardIremM107 TagValue = "irem:m107"

	// SNK
	TagArcadeBoardSNKMVS TagValue = "snk:mvs"

	// Taito
	TagArcadeBoardTaitoXSystem  TagValue = "taito:xsystem"
	TagArcadeBoardTaitoBSystem  TagValue = "taito:bsystem"
	TagArcadeBoardTaitoHSystem  TagValue = "taito:hsystem"
	TagArcadeBoardTaitoLSystem  TagValue = "taito:lsystem"
	TagArcadeBoardTaitoZSystem  TagValue = "taito:zsystem"
	TagArcadeBoardTaitoOSystem  TagValue = "taito:osystem"
	TagArcadeBoardTaitoF1System TagValue = "taito:f1system"
	TagArcadeBoardTaitoF2System TagValue = "taito:f2system"
	TagArcadeBoardTaitoF3System TagValue = "taito:f3system"
	TagArcadeBoardTaitoLGSystem TagValue = "taito:lgsystem"

	// Toaplan
	TagArcadeBoardToaplanVersion1 TagValue = "toaplan:version1"
	TagArcadeBoardToaplanVersion2 TagValue = "toaplan:version2"

	// Jaleco
	TagArcadeBoardJalecoMegaSystem1 TagValue = "jaleco:megasystem1"
)

// Compatibility tag values
const (
	// SEGA systems
	TagCompatibilitySG1000                TagValue = "sg1000"
	TagCompatibilitySG1000SC3000          TagValue = "sg1000:sc3000"
	TagCompatibilitySG1000Othello         TagValue = "sg1000:othello"
	TagCompatibilityMark3                 TagValue = "mark3"
	TagCompatibilityMark3MyCard           TagValue = "mark3:mycard"
	TagCompatibilityMark3EPMyCard         TagValue = "mark3:epmycard"
	TagCompatibilityMark3TheSegaCard      TagValue = "mark3:thesegacard"
	TagCompatibilityMark3TheMegaCartridge TagValue = "mark3:themegacartridge"
	TagCompatibilityMark3SilverCartridge  TagValue = "mark3:silvercartridge"
	TagCompatibilityMark3GoldCartridge1   TagValue = "mark3:goldcartridge1"
	TagCompatibilityMark3GoldCartridge2   TagValue = "mark3:goldcartridge2"
	TagCompatibilityMark3GoldCartridge4   TagValue = "mark3:goldcartridge4"

	// Nintendo systems
	TagCompatibilityFamicom               TagValue = "famicom"
	TagCompatibilityFamicomPegasus        TagValue = "famicom:pegasus"
	TagCompatibilityDisksystem            TagValue = "disksystem"
	TagCompatibilityDisksystemDW          TagValue = "disksystem:dw"
	TagCompatibilityGameboy               TagValue = "gameboy"
	TagCompatibilityGameboyMono           TagValue = "gameboy:mono"
	TagCompatibilityGameboyColor          TagValue = "gameboy:color"
	TagCompatibilityGameboySGB            TagValue = "gameboy:sgb"
	TagCompatibilityGameboyNP             TagValue = "gameboy:np"
	TagCompatibilitySuperfamicom          TagValue = "superfamicom"
	TagCompatibilitySuperfamicomHiROM     TagValue = "superfamicom:hirom"
	TagCompatibilitySuperfamicomLoROM     TagValue = "superfamicom:lorom"
	TagCompatibilitySuperfamicomExHiROM   TagValue = "superfamicom:exhirom"
	TagCompatibilitySuperfamicomExLoROM   TagValue = "superfamicom:exlorom"
	TagCompatibilitySuperfamicomNSS       TagValue = "superfamicom:nss"
	TagCompatibilitySuperfamicomSoundlink TagValue = "superfamicom:soundlink"
	TagCompatibilitySuperfamicomNP        TagValue = "superfamicom:np"
	TagCompatibilitySuperfamicomGS        TagValue = "superfamicom:gs"

	// NEC
	TagCompatibilityPCEngine           TagValue = "pcengine"
	TagCompatibilityPCEngineSupergrafx TagValue = "pcengine:supergrafx"

	// SNK
	TagCompatibilityNeogeoPocket      TagValue = "neogeopocket"
	TagCompatibilityNeogeoPocketMono  TagValue = "neogeopocket:mono"
	TagCompatibilityNeogeoPocketColor TagValue = "neogeopocket:color"

	// Amiga
	TagCompatibilityAmigaA500     TagValue = "amiga:a500"
	TagCompatibilityAmigaA1000    TagValue = "amiga:a1000"
	TagCompatibilityAmigaA1200    TagValue = "amiga:a1200"
	TagCompatibilityAmigaA2000    TagValue = "amiga:a2000"
	TagCompatibilityAmigaA3000    TagValue = "amiga:a3000"
	TagCompatibilityAmigaA4000    TagValue = "amiga:a4000"
	TagCompatibilityAmigaA500Plus TagValue = "amiga:a500plus"
	TagCompatibilityAmigaA600     TagValue = "amiga:a600"
	TagCompatibilityAmigaCD32     TagValue = "amiga:cd32"
	TagCompatibilityAmigaCDTV     TagValue = "amiga:cdtv"
	TagCompatibilityAmigaOCS      TagValue = "amiga:ocs"
	TagCompatibilityAmigaECS      TagValue = "amiga:ecs"
	TagCompatibilityAmigaAGA      TagValue = "amiga:aga"

	// Atari
	TagCompatibilityAtariST        TagValue = "atari:st"
	TagCompatibilityAtariSTE       TagValue = "atari:ste"
	TagCompatibilityAtariTT        TagValue = "atari:tt"
	TagCompatibilityAtariMegaST    TagValue = "atari:megast"
	TagCompatibilityAtariMegaSTE   TagValue = "atari:megaste"
	TagCompatibilityAtari130XE     TagValue = "atari:130xe"
	TagCompatibilityAtariExecutive TagValue = "atari:executive"

	// MSX
	TagCompatibilityMSXTurboRGT TagValue = "msx:turbor-gt"
	TagCompatibilityMSXTurboRST TagValue = "msx:turbor-st"

	// Commodore
	TagCompatibilityCommodorePlus4 TagValue = "commodore:plus4"

	// Primo
	TagCompatibilityPrimoPrimoA   TagValue = "primo:primoa"
	TagCompatibilityPrimoPrimoA64 TagValue = "primo:primoa64"
	TagCompatibilityPrimoPrimoB   TagValue = "primo:primob"
	TagCompatibilityPrimoPrimoB64 TagValue = "primo:primob64"
	TagCompatibilityPrimoProprimo TagValue = "primo:proprimo"

	// Other
	TagCompatibilityIBMPCDoctorPCJr   TagValue = "ibmpc:doctorpcjr"
	TagCompatibilityOsbourneOsbourne1 TagValue = "osbourne:osbourne1"
	TagCompatibilityMiscOrch80        TagValue = "misc:orch80"
	TagCompatibilityMiscPiano90       TagValue = "misc:piano90"

	// Arcade
	TagCompatibilityNintendoPlaychoice10 TagValue = "nintendo:playchoice10"
	TagCompatibilityNintendoVSDualsystem TagValue = "nintendo:vsdualsystem"
	TagCompatibilityNintendoVSUnisystem  TagValue = "nintendo:vsunisystem"
)

// Disc tag values
const (
	TagDisc1  TagValue = "1"
	TagDisc2  TagValue = "2"
	TagDisc3  TagValue = "3"
	TagDisc4  TagValue = "4"
	TagDisc5  TagValue = "5"
	TagDisc6  TagValue = "6"
	TagDisc7  TagValue = "7"
	TagDisc8  TagValue = "8"
	TagDisc9  TagValue = "9"
	TagDisc10 TagValue = "10"
)

// Disc total tag values
const (
	TagDiscTotal2  TagValue = "2"
	TagDiscTotal3  TagValue = "3"
	TagDiscTotal4  TagValue = "4"
	TagDiscTotal5  TagValue = "5"
	TagDiscTotal6  TagValue = "6"
	TagDiscTotal7  TagValue = "7"
	TagDiscTotal8  TagValue = "8"
	TagDiscTotal9  TagValue = "9"
	TagDiscTotal10 TagValue = "10"
)

// Based tag values
const (
	TagBasedManganime    TagValue = "manganime"
	TagBasedMovie        TagValue = "movie"
	TagBasedDisney       TagValue = "disney"
	TagBasedDND          TagValue = "dnd"
	TagBasedJurassicpark TagValue = "jurassicpark"
	TagBasedLooneytunes  TagValue = "looneytunes"
	TagBasedMarvel       TagValue = "marvel"
	TagBasedSimpsons     TagValue = "simpsons"
	TagBasedSmurfs       TagValue = "smurfs"
	TagBasedStarwars     TagValue = "starwars"
	TagBasedTMNT         TagValue = "tmnt"
)

// Search tag values
const (
	// Franchises
	TagSearchFranchiseCastlevania  TagValue = "franchise:castlevania"
	TagSearchFranchiseDragonslayer TagValue = "franchise:dragonslayer"
	TagSearchFranchiseWonderboy    TagValue = "franchise:wonderboy"

	// Featured characters
	TagSearchFeatureAlien       TagValue = "feature:alien"
	TagSearchFeatureAsterix     TagValue = "feature:asterix"
	TagSearchFeatureBatman      TagValue = "feature:batman"
	TagSearchFeatureCompatihero TagValue = "feature:compatihero"
	TagSearchFeatureDracula     TagValue = "feature:dracula"
	TagSearchFeatureDonald      TagValue = "feature:donald"
	TagSearchFeatureGundam      TagValue = "feature:gundam"
	TagSearchFeatureKuniokun    TagValue = "feature:kuniokun"
	TagSearchFeatureMario       TagValue = "feature:mario"
	TagSearchFeatureMickey      TagValue = "feature:mickey"
	TagSearchFeaturePacman      TagValue = "feature:pacman"
	TagSearchFeatureSherlock    TagValue = "feature:sherlock"
	TagSearchFeatureSonic       TagValue = "feature:sonic"
	TagSearchFeatureSpiderman   TagValue = "feature:spiderman"
	TagSearchFeatureSuperman    TagValue = "feature:superman"
	TagSearchFeatureXMen        TagValue = "feature:xmen"

	// Screen orientation
	TagSearchTateCW  TagValue = "tate:cw"
	TagSearchTateCCW TagValue = "tate:ccw"

	// 3D effects
	TagSearch3DStereo   TagValue = "3d:stereo"
	TagSearch3DAnaglyph TagValue = "3d:anaglyph"

	// Keywords
	TagSearchKeywordStrip    TagValue = "keyword:strip"
	TagSearchKeywordPromo    TagValue = "keyword:promo"
	TagSearchKeywordQSound   TagValue = "keyword:qsound"
	TagSearchKeywordDolby    TagValue = "keyword:dolby"
	TagSearchKeywordRS       TagValue = "keyword:rs"
	TagSearchKeywordOfficial TagValue = "keyword:official"
	TagSearchKeywordEndorsed TagValue = "keyword:endorsed"
	TagSearchKeywordBrand    TagValue = "keyword:brand"
)

// Multigame tag values
const (
	TagMultigameCompilation TagValue = "compilation"
)

// Reboxed tag values
const (
	TagReboxedBIOS              TagValue = "bios"
	TagReboxedBluebox           TagValue = "bluebox"
	TagReboxedPurplebox         TagValue = "purplebox"
	TagReboxedClassicedition    TagValue = "classicedition"
	TagReboxedSegaclassic       TagValue = "segaclassic"
	TagReboxedKixxedition       TagValue = "kixxedition"
	TagReboxedSatakore          TagValue = "satakore"
	TagReboxedGenteiban         TagValue = "genteiban"
	TagReboxedMegadrive3        TagValue = "megadrive3"
	TagReboxedMegadrive4        TagValue = "megadrive4"
	TagReboxedReactor           TagValue = "reactor"
	TagReboxedGopher            TagValue = "gopher"
	TagReboxedMeisaku           TagValue = "meisaku"
	TagReboxedMajesco           TagValue = "majesco"
	TagReboxedMegahit           TagValue = "megahit"
	TagReboxedKonamiclassics    TagValue = "konamiclassics"
	TagReboxedEAclassics        TagValue = "eaclassics"
	TagReboxedVideogameclassics TagValue = "videogameclassics"
	TagReboxedKoeibest          TagValue = "koeibest"
	TagReboxedGamenokanzume     TagValue = "gamenokanzume"
	TagReboxedSoundware         TagValue = "soundware"
	TagReboxedPlayerschoice     TagValue = "playerschoice"
	TagReboxedClassicserie      TagValue = "classicserie"
	TagReboxedKousenjuu         TagValue = "kousenjuu"
	TagReboxedDisneysclassic    TagValue = "disneysclassic"
	TagReboxedSNKBestcollection TagValue = "snkbestcollection"
	TagReboxedXeye              TagValue = "xeye"
	TagReboxedLimitedrun        TagValue = "limitedrun"
	TagReboxedFamicombox        TagValue = "famicombox"
	TagReboxedSuperfamicombox   TagValue = "superfamicombox"
)

// Port tag values
const (
	TagPortArcade               TagValue = "arcade"
	TagPortCommodoreC64         TagValue = "commodore:c64"
	TagPortCommodoreAmiga       TagValue = "commodore:amiga"
	TagPortAppleApple2          TagValue = "apple:apple2"
	TagPortAppleMac             TagValue = "apple:mac"
	TagPortBBCMicro             TagValue = "bbcmicro"
	TagPortDragon32             TagValue = "dragon32"
	TagPortElektronika60        TagValue = "elektronika60"
	TagPortSpectrum             TagValue = "spectrum"
	TagPortAmstrad              TagValue = "amstrad"
	TagPortAtariAtari400        TagValue = "atari:atari400"
	TagPortAtariAtariST         TagValue = "atari:atarist"
	TagPortAtariAtari2600       TagValue = "atari:atari2600"
	TagPortAtariLynx            TagValue = "atari:lynx"
	TagPortAtariJaguar          TagValue = "atari:jaguar"
	TagPortNECPC88              TagValue = "nec:pc88"
	TagPortNECPC98              TagValue = "nec:pc98"
	TagPortNECPCEngine          TagValue = "nec:pcengine"
	TagPortNECCDROMROM          TagValue = "nec:cdromrom"
	TagPortNECPCFX              TagValue = "nec:pcfx"
	TagPortMSX                  TagValue = "msx"
	TagPortMSX2                 TagValue = "msx:2"
	TagPortSharpX1              TagValue = "sharp:x1"
	TagPortSharpMZ700           TagValue = "sharp:mz700"
	TagPortSharpX68000          TagValue = "sharp:x68000"
	TagPortPC                   TagValue = "pc"
	TagPortSegaSG1000           TagValue = "sega:sg1000"
	TagPortSegaMark3            TagValue = "sega:mark3"
	TagPortSegaGameGear         TagValue = "sega:gamegear"
	TagPortSegaMegadrive        TagValue = "sega:megadrive"
	TagPortSegaMegaCD           TagValue = "sega:megacd"
	TagPortSegaSaturn           TagValue = "sega:saturn"
	TagPortSegaDreamcast        TagValue = "sega:dreamcast"
	TagPortNintendoFamicom      TagValue = "nintendo:famicom"
	TagPortNintendoSuperfamicom TagValue = "nintendo:superfamicom"
	TagPortNintendoN64          TagValue = "nintendo:n64"
	TagPortNintendoGameboy      TagValue = "nintendo:gameboy"
	TagPortNintendoGBC          TagValue = "nintendo:gbc"
	TagPortNintendoGBA          TagValue = "nintendo:gba"
	TagPortSonyPlayStation      TagValue = "sony:playstation"
	TagPort3DO                  TagValue = "3do"
	TagPortCDI                  TagValue = "cdi"
	TagPortLaseractive          TagValue = "laseractive"
	TagPortFMTowns              TagValue = "fmtowns"
)

// Rerelease tag values
const (
	// Nintendo
	TagRereleaseVirtualconsoleWii  TagValue = "virtualconsole:wii"
	TagRereleaseVirtualconsoleWiiU TagValue = "virtualconsole:wiiu"
	TagRereleaseVirtualconsole3DS  TagValue = "virtualconsole:3ds"
	TagRereleaseSwitchonline       TagValue = "switchonline"
	TagRereleaseEreader            TagValue = "ereader"
	TagRereleaseAnimalcrossing     TagValue = "animalcrossing"
	TagRereleaseSupermario25       TagValue = "supermario25"

	// Publisher collections
	TagRereleaseCapcomtown               TagValue = "capcomtown"
	TagRereleaseNamcoanthology1          TagValue = "namcoanthology:1"
	TagRereleaseNamcoanthology2          TagValue = "namcoanthology:2"
	TagRereleaseNamcot1                  TagValue = "namcot:1"
	TagRereleaseNamcot2                  TagValue = "namcot:2"
	TagRereleaseCastlevaniaanniversary   TagValue = "castlevaniaanniversary"
	TagRereleaseCastlevaniaadvance       TagValue = "castlevaniaadvance"
	TagRereleaseContraanniversary        TagValue = "contraanniversary"
	TagRereleaseCowabunga                TagValue = "cowabunga"
	TagRereleaseKonamicollectors         TagValue = "konamicollectors"
	TagRereleaseDariuscozmic             TagValue = "dariuscozmic"
	TagRereleaseRockmanclassic1          TagValue = "rockmanclassic:1"
	TagRereleaseRockmanclassic2          TagValue = "rockmanclassic:2"
	TagRereleaseRockmanclassicX          TagValue = "rockmanclassic:x"
	TagRereleaseRockmanclassicX2         TagValue = "rockmanclassic:x2"
	TagRereleaseSeikendensetsu           TagValue = "seikendensetsu"
	TagRereleaseDisneyclassic            TagValue = "disneyclassic"
	TagRereleaseBubsytwofur              TagValue = "bubsytwofur"
	TagRereleaseBlizzardarcadecollection TagValue = "blizzardarcadecollection"
	TagRereleaseQubyte                   TagValue = "qubyte"
	TagRereleaseProjectegg               TagValue = "projectegg"
	TagRereleaseLimitedrun               TagValue = "limitedrun"
	TagRereleaseIam8bit                  TagValue = "iam8bit"
	TagRereleaseEvercadeOlivertwins      TagValue = "evercade:olivertwins"
	TagRereleaseSteam                    TagValue = "steam"
	TagRereleaseSonicclassic             TagValue = "sonicclassic"
	TagRereleaseSonicmegacollection      TagValue = "sonicmegacollection"
	TagRereleaseMDClassics               TagValue = "mdclassics"
	TagRereleaseSmashpack                TagValue = "smashpack"
	TagRereleaseSegaages                 TagValue = "segaages"
	TagRereleaseSegaages2500             TagValue = "segaages:2500"
	TagRerelease3DFukkoku                TagValue = "3dfukkoku"
	TagRereleaseMDMini1                  TagValue = "mdmini:1"
	TagRereleaseMDMini2                  TagValue = "mdmini:2"
	TagRereleaseSFCMini                  TagValue = "sfcmini"
	TagRereleaseGamenokanzume1           TagValue = "gamenokanzume:1"
	TagRereleaseGamenokanzume2           TagValue = "gamenokanzume:2"
	TagRereleaseFightnightround2         TagValue = "fightnightround2"
)

// Set tag values
const (
	TagSet1 TagValue = "1"
	TagSet2 TagValue = "2"
	TagSet3 TagValue = "3"
	TagSet4 TagValue = "4"
	TagSet5 TagValue = "5"
	TagSet6 TagValue = "6"
	TagSet7 TagValue = "7"
	TagSet8 TagValue = "8"
)

// Alt tag values
const (
	TagAlt1 TagValue = "1"
	TagAlt2 TagValue = "2"
	TagAlt3 TagValue = "3"
)

// Language variant tag values (specific regional variants)
const (
	TagLangZHTrad TagValue = "zh-trad" // Traditional Chinese
	TagLangZHHans TagValue = "zh-hans" // Simplified Chinese (Hans)
	TagLangPTBR   TagValue = "pt-br"   // Brazilian Portuguese
)

// Revision sub-version tag values (dotted versions like v1.0, v1.1)
const (
	// v1.x versions
	TagRev1_0 TagValue = "1-0"
	TagRev1_1 TagValue = "1-1"
	TagRev1_2 TagValue = "1-2"
	TagRev1_3 TagValue = "1-3"
	TagRev1_4 TagValue = "1-4"
	TagRev1_5 TagValue = "1-5"
	TagRev1_6 TagValue = "1-6"
	TagRev1_7 TagValue = "1-7"
	TagRev1_8 TagValue = "1-8"
	TagRev1_9 TagValue = "1-9"
	// v2.x versions
	TagRev2_0 TagValue = "2-0"
	TagRev2_1 TagValue = "2-1"
	TagRev2_2 TagValue = "2-2"
	TagRev2_3 TagValue = "2-3"
	TagRev2_4 TagValue = "2-4"
	TagRev2_5 TagValue = "2-5"
	TagRev2_6 TagValue = "2-6"
	TagRev2_7 TagValue = "2-7"
	TagRev2_8 TagValue = "2-8"
	TagRev2_9 TagValue = "2-9"
	// v3.x versions
	TagRev3_0 TagValue = "3-0"
	TagRev3_1 TagValue = "3-1"
	TagRev3_2 TagValue = "3-2"
	TagRev3_3 TagValue = "3-3"
	TagRev3_4 TagValue = "3-4"
	TagRev3_5 TagValue = "3-5"
	// v4.x versions
	TagRev4_0 TagValue = "4-0"
	TagRev4_1 TagValue = "4-1"
	TagRev4_2 TagValue = "4-2"
	// v5.x versions
	TagRev5_0 TagValue = "5-0"
	TagRev5_1 TagValue = "5-1"
	TagRev5_2 TagValue = "5-2"
	// Program revisions (NES-specific)
	TagRevPRG  TagValue = "prg"
	TagRevPRG0 TagValue = "prg:0"
	TagRevPRG1 TagValue = "prg:1"
	TagRevPRG2 TagValue = "prg:2"
	TagRevPRG3 TagValue = "prg:3"
)

// Unfinished (demo) variant tag values
const (
	TagUnfinishedDemoPlayable  TagValue = "demo:playable"
	TagUnfinishedDemoRolling   TagValue = "demo:rolling"
	TagUnfinishedDemoSlideshow TagValue = "demo:slideshow"
)

// Dump variant tag values
const (
	TagDumpPending          TagValue = "pending"              // Pending verification (GoodTools)
	TagDumpChecksumBad      TagValue = "checksum-bad"         // Bad checksum but good dump (GoodTools)
	TagDumpChecksumUnknown  TagValue = "checksum-unknown"     // Unknown checksum status (GoodTools)
	TagDumpBIOS             TagValue = "bios"                 // BIOS dump
	TagDumpHackedFFE        TagValue = "hacked:ffe"           // Far East Copier hack
	TagDumpHackedIntroRemov TagValue = "hacked:intro-removed" // Hacked intro removed
)

// Amiga compatibility combination tag values
const (
	TagCompatibilityAmigaPlus2               TagValue = "amiga:plus2"
	TagCompatibilityAmigaPlus2A              TagValue = "amiga:plus2a"
	TagCompatibilityAmigaPlus3               TagValue = "amiga:plus3"
	TagCompatibilityAmigaA1200A4000          TagValue = "amiga:a1200-a4000"
	TagCompatibilityAmigaA2000A3000          TagValue = "amiga:a2000-a3000"
	TagCompatibilityAmigaA2024               TagValue = "amiga:a2024"
	TagCompatibilityAmigaA2500A3000UX        TagValue = "amiga:a2500-a3000ux"
	TagCompatibilityAmigaA4000T              TagValue = "amiga:a4000t"
	TagCompatibilityAmigaA500A1000A2000      TagValue = "amiga:a500-a1000-a2000"
	TagCompatibilityAmigaA500A1000A2000CDTV  TagValue = "amiga:a500-a1000-a2000-cdtv"
	TagCompatibilityAmigaA500A1200           TagValue = "amiga:a500-a1200"
	TagCompatibilityAmigaA500A1200A2000A4000 TagValue = "amiga:a500-a1200-a2000-a4000"
	TagCompatibilityAmigaA500A2000           TagValue = "amiga:a500-a2000"
	TagCompatibilityAmigaA500A600A2000       TagValue = "amiga:a500-a600-a2000"
	TagCompatibilityAmigaA570                TagValue = "amiga:a570"
	TagCompatibilityAmigaA600HD              TagValue = "amiga:a600hd"
	TagCompatibilityAmigaAGACD32             TagValue = "amiga:aga-cd32"
	TagCompatibilityAmigaECSAGA              TagValue = "amiga:ecs-aga"
	TagCompatibilityAmigaOCSAGA              TagValue = "amiga:ocs-aga"
)

// Atari compatibility combination tag values
const (
	TagCompatibilityAtariSTEFalcon TagValue = "atari:ste-falcon"
)

// ColecoVision compatibility tag values
const (
	TagCompatibilityColecoAdam TagValue = "coleco:adam"
)

// Arcade board tag values (Nintendo/Sega)
const (
	TagArcadeBoardNintendoVS   TagValue = "nintendo:vs"
	TagArcadeBoardNintendoNSS  TagValue = "nintendo:nss"
	TagArcadeBoardSegaMegaplay TagValue = "sega:megaplay"
)

// Unlicensed publisher tag values
const (
	TagUnlicensedSachen TagValue = "unlicensed:sachen"
)

// Media type tag values
const (
	TagMediaCart      TagValue = "cart"
	TagMediaN64DD     TagValue = "n64dd"
	TagMediaFDS       TagValue = "fds"
	TagMediaEReader   TagValue = "ereader"
	TagMediaMultiboot TagValue = "multiboot"
)

// Multigame tag values (volume numbers and menu)
const (
	TagMultigameVol1 TagValue = "vol:1"
	TagMultigameVol2 TagValue = "vol:2"
	TagMultigameVol3 TagValue = "vol:3"
	TagMultigameVol4 TagValue = "vol:4"
	TagMultigameVol5 TagValue = "vol:5"
	TagMultigameVol6 TagValue = "vol:6"
	TagMultigameVol7 TagValue = "vol:7"
	TagMultigameVol8 TagValue = "vol:8"
	TagMultigameVol9 TagValue = "vol:9"
	TagMultigameMenu TagValue = "menu"
)

// Reboxed (bundle) tag values
const (
	TagReboxedBundle        TagValue = "bundle"
	TagReboxedBundleGenesis TagValue = "bundle:genesis"
)

// Year tag values
const (
	TagYear1970 TagValue = "1970"
	TagYear1971 TagValue = "1971"
	TagYear1972 TagValue = "1972"
	TagYear1973 TagValue = "1973"
	TagYear1974 TagValue = "1974"
	TagYear1975 TagValue = "1975"
	TagYear1976 TagValue = "1976"
	TagYear1977 TagValue = "1977"
	TagYear1978 TagValue = "1978"
	TagYear1979 TagValue = "1979"
	TagYear1980 TagValue = "1980"
	TagYear1981 TagValue = "1981"
	TagYear1982 TagValue = "1982"
	TagYear1983 TagValue = "1983"
	TagYear1984 TagValue = "1984"
	TagYear1985 TagValue = "1985"
	TagYear1986 TagValue = "1986"
	TagYear1987 TagValue = "1987"
	TagYear1988 TagValue = "1988"
	TagYear1989 TagValue = "1989"
	TagYear1990 TagValue = "1990"
	TagYear1991 TagValue = "1991"
	TagYear1992 TagValue = "1992"
	TagYear1993 TagValue = "1993"
	TagYear1994 TagValue = "1994"
	TagYear1995 TagValue = "1995"
	TagYear1996 TagValue = "1996"
	TagYear1997 TagValue = "1997"
	TagYear1998 TagValue = "1998"
	TagYear1999 TagValue = "1999"
	TagYear2000 TagValue = "2000"
	TagYear2001 TagValue = "2001"
	TagYear2002 TagValue = "2002"
	TagYear2003 TagValue = "2003"
	TagYear2004 TagValue = "2004"
	TagYear2005 TagValue = "2005"
	TagYear2006 TagValue = "2006"
	TagYear2007 TagValue = "2007"
	TagYear2008 TagValue = "2008"
	TagYear2009 TagValue = "2009"
	TagYear2010 TagValue = "2010"
	TagYear2011 TagValue = "2011"
	TagYear2012 TagValue = "2012"
	TagYear2013 TagValue = "2013"
	TagYear2014 TagValue = "2014"
	TagYear2015 TagValue = "2015"
	TagYear2016 TagValue = "2016"
	TagYear2017 TagValue = "2017"
	TagYear2018 TagValue = "2018"
	TagYear2019 TagValue = "2019"
	TagYear2020 TagValue = "2020"
	TagYear2021 TagValue = "2021"
	TagYear2022 TagValue = "2022"
	TagYear2023 TagValue = "2023"
	TagYear2024 TagValue = "2024"
	TagYear2025 TagValue = "2025"
	TagYear2026 TagValue = "2026"
	TagYear2027 TagValue = "2027"
	TagYear2028 TagValue = "2028"
	TagYear2029 TagValue = "2029"
	TagYear19XX TagValue = "19xx"
	TagYear197X TagValue = "197x"
	TagYear198X TagValue = "198x"
	TagYear199X TagValue = "199x"
	TagYear20XX TagValue = "20xx"
	TagYear200X TagValue = "200x"
	TagYear201X TagValue = "201x"
	TagYear202X TagValue = "202x"
)

// Edition tag values
const (
	TagEditionVersion TagValue = "version"
	TagEditionEdition TagValue = "edition"
)

// Perspective tag values
const (
	TagPerspectiveFirstperson          TagValue = "firstperson"
	TagPerspectiveThirdperson          TagValue = "thirdperson"
	TagPerspectiveTopdown              TagValue = "topdown"
	TagPerspectiveIsometric            TagValue = "isometric"
	TagPerspectiveFixedcamera          TagValue = "fixedcamera"
	TagPerspectiveSidescrollHorizontal TagValue = "sidescroll:horizontal"
	TagPerspectiveSidescrollVertical   TagValue = "sidescroll:vertical"
)

// Art style tag values
const (
	// Base dimensions
	TagArt2D TagValue = "2d"
	TagArt3D TagValue = "3d"

	// Art styles
	TagArtPixelart  TagValue = "pixelart"
	TagArtCelshaded TagValue = "celshaded"
	TagArtVector    TagValue = "vector"
	TagArtDigitized TagValue = "digitized"
	TagArtHanddrawn TagValue = "handdrawn"
)

// Accessibility tag values
const (
	// Visual accessibility
	TagAccessibilityVisualColorblindMode TagValue = "visual:colorblindmode"
	TagAccessibilityVisualHighContrast   TagValue = "visual:highcontrast"
	TagAccessibilityVisualTextSizeAdjust TagValue = "visual:textsizeadjust"

	// Audio accessibility
	TagAccessibilityAudioSubtitles  TagValue = "audio:subtitles"
	TagAccessibilityAudioMonoAudio  TagValue = "audio:monoaudio"
	TagAccessibilityAudioVisualCues TagValue = "audio:visualcues"

	// Input accessibility
	TagAccessibilityInputRemappableControls TagValue = "input:remappablecontrols"
	TagAccessibilityInputOneButtonMode      TagValue = "input:onebuttonmode"
)
