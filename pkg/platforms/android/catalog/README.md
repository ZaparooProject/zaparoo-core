# Reviewed Android launcher catalog

These files are embedded into the Android platform. Every entry becomes a
bounded, typed launch definition that the embedding host validates again before
it builds an intent.

## RetroArch cores

`retroarch-android-catalog-v2.json` lists launcher metadata for the RetroArch
AArch64 package (`com.retroarch.aarch64`). It was reviewed on 2026-09-19 against:

- Core's canonical system IDs and ES-DE extension mappings;
- Libretro's official Android arm64 buildbot index:
  <https://buildbot.libretro.com/nightly/android/latest/arm64-v8a/>;
- `libretro/libretro-core-info` revision
  `5a74858ab2f7a50cebb5a6330895bc38899531c0`;
- RetroArch 1.22.2's `MainMenuActivity` external-launch extras and
  `RetroActivityFuture` target.

The index contained 236 artifacts. The catalog includes 169 applicable emulator
cores as 260 core/system profiles across 92 canonical systems. The `cores` map
records each one's exact downloaded filename once (not once per profile row -
verified 1:1 per core, never per system) because most Android cores use
`_libretro_android.so`, while exceptions such as Azahar use `_libretro.so`.
Version 2 (2026-09-30) moved the system→core assignment and its precedence
order out of this file's row order and into
[`pkg/platforms/shared/retroarch`](../../shared/retroarch) (`CoreLaunches(
ProfileAndroid)`), the package Linux/SteamOS/Bazzite/ChimeraOS/ZapOS already
share for the same job - `androidCoreAlternates` there, not this file, is
where to add a system's extra core candidates or move one's rank. This file
now supplies only what that package has no reason to know: each profile's
stable launcher ID and accepted extensions, and each core's display name and
Android `.so` filename.

The remaining 67 artifacts are excluded because they are game engines, demos,
runtimes, media utilities or test cores; target a machine without compatible
indexed extensions; lack official core-info metadata; or have misleading
metadata. VICE x128 stays excluded: its C64 database label does not make a C128
core a valid C64 launcher. The reviewed inventory is
[`retroarch-aarch64-buildbot-2026-09-19.txt`](retroarch-aarch64-buildbot-2026-09-19.txt);
reasons are in
[`retroarch-aarch64-exclusions-v1.json`](retroarch-aarch64-exclusions-v1.json).
Tests require all 236 artifacts to be included or explicitly excluded.

Artifact presence proves filename and ABI availability, not gameplay quality,
BIOS availability, or installation on a device.

## Standalone content-URI profiles

`standalone-content-uri-v2.json` adds version 2 profiles reviewed against the
apps' manifests and current upstream sources:

- DuckStation `0.1-8969-g611bb8fb4`: exported `EmulationActivity`, `bootPath`
  string extra containing the content URI, temporary read grant and `ClipData`;
- PPSSPP `v1.20.4`: exported `PpssppActivity`, `ACTION_VIEW` data URI;
- Dolphin `2603a`: exported `MainActivity`, `ACTION_VIEW` data URI.

Profiles include only single-file formats. Playlist and companion-file formats
such as M3U and CUE stay excluded until multi-document grant semantics are
proven.

MAME4droid 2024 and ARMSX2 are also registered here, as version 2 profiles
like the rest: they take a content URI the same way. A standalone app leads
its system's RetroArch cores, so MAME4droid precedes the RetroArch arcade
cores and ARMSX2 precedes RetroArch's PS2 core.

## App profiles

`app-v3.json` adds version 3 profiles: a launch that starts an installed app
directly, with no media at all. The app's identity is the virtual path
`android://<package>[:<variant>]/<name>`, so the activity, action and extras
that actually start it stay in the catalog; an app update that renames its
launch activity does not invalidate an identity already written to a card.

A profile's `variant` key is opaque to the format and owned by the profile: it
distinguishes one launchable configuration of an app from another (for
example, a game-selection extra a fan-made port reads), so adding a variant is
a catalog change, never a format change. Extras on a version 3 profile carry
only a literal value; there is no media to source one from.

An app the catalog does not describe is still offered: the platform lists
every launchable app the host reports and starts one directly, generic and
without a catalog entry, unless a profile already claims its package (a
profiled variant is always preferred over the generic offer of the same app).

## Launchers Core builds

Two more launchers derive their intent from a file's content rather than a
catalog row: standalone ScummVM and GameNative (`scummvm.go`, `gamenative.go`).
Neither uses a virtual-path scheme of its own; a `.scummvm` file or a
GameNative export is ordinary source-backed media, matched by `Folders` and
`Extensions` exactly like any other system's files. Content is read only at
dispatch, for the few bytes a launch needs, and never reaches the identity or
the indexer:

- ScummVM (`org.scummvm.scummvm`), launcher `ScummVM.Standalone`: exported
  `SplashActivity`, which forwards the intent's action and data to
  `ScummVMActivity`; that passes the `scummvm:<target>` data URI's
  scheme-specific part to ScummVM as its only argument, as ScummVM's own
  home-screen shortcuts do. The `.scummvm` file stays the media identity,
  shared with `RetroArch.ScummVM`, and registers ahead of that core so it is
  preferred when installed. Core reads the target ID from the file (at most
  256 bytes, trimmed), or from its own name when the file is empty, and
  refuses anything that is not a plain target ID. The game must already be
  added in ScummVM with that ID.
- GameNative (`app.gamenative`), launchers `GameNative.Steam` (system PC) and
  `GameNative.Windows`: exported `MainActivity`, action
  `app.gamenative.LAUNCH_GAME`, int extra `app_id` and string extra
  `game_source`. GameNative's frontend export writes one file per installed
  game, `<title>.<ext>`, holding the decimal app ID: `.steam` (PC, the system
  ES-DE's `steam` folder maps to) and `.epic`, `.gog`, `.amazon`, `.pcgame`
  (Windows). The store is read from the file's own extension; the app ID from
  its content (at most 64 bytes, a positive decimal fitting a Java int).

Only these packages may receive a data URI (`scummvm:` for ScummVM), an int
extra or a custom action (`app.gamenative.LAUNCH_GAME` for GameNative);
`definition.go` holds that list.

## Order is precedence

Launchers register in order: the launchers Core builds first (standalone
ScummVM and GameNative lead the same media's RetroArch cores), then the
standalone profiles, then the RetroArch profiles, then the app profiles, then
the generic offer of every other installed app. When nothing else chooses a
launcher (a media override, a system default or the global launcher
preference), the first registered launcher that matches the media and is not
known to be missing wins.

Within each system the launchers are therefore ordered most preferred first:
mature compatibility and stable performance lead, accuracy breaks ties, and a
reviewed standalone app leads the equivalent RetroArch core. For a RetroArch
core, that rank lives in `pkg/platforms/shared/retroarch`'s `androidCoreAlternates`
(add or move a core's position there, not in this file's `profiles` array,
which carries no rank of its own). Stable ID `RetroArch.Mesen` remains for
compatibility.

Arcade is an inherent exception to universal compatibility: ZIP contents must
match the selected core's ROM set. FinalBurn Neo leads, followed by MAME
2003-Plus and current MAME; users with another ROM set can override it.

## Repair text is a fallback, not the contract

Each profile carries a `repair` string. It stays in the schema and remains the
English fallback message for a launcher that is not installed or whose declared
entry point is missing, so a client that only reads `error.message` keeps
working.

It is not what a client should display. A launch failure reports a machine
readable `reason` from the closed set in `pkg/platforms/launch_error.go`, plus
the bounded `launcher` and `plugin` display names, and a client is expected to
write and localize its own wording from those. See the `launch_repair` error in
[`docs/api/methods.md`](../../../../docs/api/methods.md). Keep `repair` short,
generic and free of anything a client must not show verbatim.

## Boundaries

RetroArch profiles use transient filesystem paths only for media selected
through Android's system external storage provider. Standalone profiles use
exact document URIs with read-only intent authority and `ClipData`. Paths and
content URIs are never persisted as media identity.
