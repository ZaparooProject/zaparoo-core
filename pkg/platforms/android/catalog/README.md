# Reviewed Android launcher catalog

These files are embedded into the Android platform. Every entry becomes a
bounded, typed launch definition that the embedding host validates again before
it builds an intent.

## RetroArch cores

`retroarch-aarch64-catalog-v1.json` lists launcher metadata for the RetroArch
AArch64 package (`com.retroarch.aarch64`). It was reviewed on 2026-09-19 against:

- Core's canonical system IDs and ES-DE extension mappings;
- Libretro's official Android arm64 buildbot index:
  <https://buildbot.libretro.com/nightly/android/latest/arm64-v8a/>;
- `libretro/libretro-core-info` revision
  `5a74858ab2f7a50cebb5a6330895bc38899531c0`;
- RetroArch 1.22.2's `MainMenuActivity` external-launch extras and
  `RetroActivityFuture` target.

The index contained 236 artifacts. The catalog includes 169 applicable emulator
cores as 260 core/system profiles across 92 canonical systems. Each row records
the exact downloaded filename because most Android cores use
`_libretro_android.so`, while exceptions such as Azahar use `_libretro.so`.

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

## Order is precedence

Launchers register in file order: the standalone profiles first, then the
RetroArch profiles. When nothing else chooses a launcher (a media override, a
system default or the global launcher preference), the first registered
launcher that matches the media and is not known to be missing wins.

Within each system the profiles are therefore listed most preferred first:
mature compatibility and stable performance lead, accuracy breaks ties, and a
reviewed standalone app leads the equivalent RetroArch core. Add a profile at
the position its preference deserves, not at the end. Stable ID
`RetroArch.Mesen` remains for compatibility.

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
