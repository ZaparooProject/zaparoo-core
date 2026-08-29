# OTA update runbook

How a release reaches the updater, and how to stop it if something is wrong.

Publishing a GitHub release does **not** publish an update. The two are
deliberately separate: a release can sit on GitHub for as long as it takes to
gain confidence in it, and only an explicit workflow dispatch makes devices
aware of it.

## What is published

Four files live at `https://updates.zaparoo.org/ZaparooProject/zaparoo-core/`:

| File | Purpose |
|---|---|
| `manifest.yaml` | The list of installable releases, their channels, rollouts and per-asset digests |
| `manifest.yaml.sig` | Detached ed25519 signature over `manifest.yaml` |
| `checksums.txt` | `sha256sum`-format digests, derived from the manifest so the two cannot disagree |
| `checksums.txt.sig` | Detached ed25519 signature over `checksums.txt` |

Release archives themselves are served from GitHub, never from the CDN. The
manifest only ever points at `https://github.com/ZaparooProject/zaparoo-core/releases/download/...`,
and the publish workflow refuses to upload a manifest that says otherwise.

The manifest carries a `generation` counter that only ever increases; a device
that has seen generation 412 rejects 411 as a replay. There is no expiry field
and nothing re-signs on a schedule, so a device never needs a working clock to
accept a manifest — which matters on MiSTer, where there is no RTC.

## Workflows

| Workflow | Trigger | Environment | What it does |
|---|---|---|---|
| **OTA validate** | Every published release | none | Read-only. Checks the release would promote cleanly. Publishes nothing. |
| **OTA promote** | Manual | `ota-publish` | Adds a release to the manifest. |
| **OTA rollout** | Manual | `ota-publish` | Changes one release's rollout percentage. |
| **OTA withdraw** | Manual | `ota-publish` | Removes a release from the manifest entirely. |

All three publishing workflows share one concurrency group, so they queue behind
each other rather than racing for the manifest.

There is deliberately no scheduled workflow. Every run that touches the signing
key passes a required reviewer, which keeps the trust base to one environment
and removes a cron as something that can silently fail.

### Promoting a release

Prereleases promoted to the beta OTA channel must use `vX.Y.Z-beta.N` or
`vX.Y.Z-rc.N`, where `N` is an unpadded numeric SemVer prerelease identifier,
for example `v2.17.0-beta.10` or `v2.17.0-rc.1`. Do not use forms such as
`-beta10` or `-rc01`: SemVer either sorts them incorrectly or rejects their
numeric identifiers. Alpha remains a valid version stage for builds and GitHub
releases, but alpha releases are not promotable to the beta OTA channel. OTA
validate checks this policy before release assets, and the shared manifest
generator enforces it during manual promotion. Nightly tags keep their existing
convention.

1. Wait for **OTA validate** to pass on the release. Its job summary lists the
   inputs to use. A failure here means the release is not promotable at all —
   usually a platform archive missing from the build matrix.
2. Dispatch **OTA promote** with `dry_run: true`. It produces the exact signed
   metadata it would publish and attaches it as an artifact. Nothing reaches
   the CDN.
3. Check the artifact, then dispatch again with `dry_run: false`.

Promote downloads every archive to the runner and hashes it there. GitHub's
reported digests are cross-checked against those hashes in both directions, but
what gets signed is always the bytes on disk.

The signed manifest was bootstrapped at generation 1 on 2026-08-17. Every
promote now verifies the live signature and advances from its generation;
`generation_floor` is disaster-recovery input, not part of a normal promote.
Do not use it unless the signed live metadata is genuinely unavailable and the
recovery generation is known to exceed every generation devices may have seen.

Start a significant release at a partial `rollout` — 5, then 25, then 100 — and
widen it with **OTA rollout**. Hold each rung long enough to see failures that
only a real fleet produces: at least 24 hours at 5, 72 hours at 25, and 7 days
at 100 before the release counts as fully out. Bucketing is salted per release,
so widening keeps the devices already on it and a different release picks a
fresh, uncorrelated cohort. No device is permanently a guinea pig.

`min_upgrade_from` sets a floor on the version a device must *already* be
running to install this release directly. Use it when a release is only safe as
a step up from a recent version — devices below the floor will not be offered
it and will have to take an intermediate release first. Leave it empty
otherwise; a floor set without that specific reason just strands old installs.

### Something is wrong with a release

Escalate in this order. Each rung is faster and less disruptive than the next.

1. **Halt the rollout.** Dispatch **OTA rollout** with `rollout: 0`. Automatic
   installation stops within one device check interval. The release stays
   installable on request, which is the right call while it is still unclear
   whether the report is one bad device. This is fast — no archives are
   downloaded, one number changes.
2. **Withdraw the release.** Dispatch **OTA withdraw**. The release leaves the
   manifest and its digests leave `checksums.txt`, so it stops being an
   installable target for automatic *and* manual updates. Target: under five
   minutes from decision to verified.
3. **Promote a fix** at `rollout: 100`, with no `min_upgrade_from`, so every
   device is offered it — including the ones sitting on the withdrawn version.

Withdrawing does not roll anything back. Devices that already updated stay on
that version; only the fix in step 3 moves them.

### A Windows device has no executable to start

Windows cannot overwrite a running binary, so installing over one is two
renames: the outgoing binary moves to a sibling name, then the new one takes the
name it left. Between them the install path holds nothing, and Windows starts
Core only from that path — so a device interrupted in that window has nothing
left to launch and never reaches the startup watchdog that would recover it.

Both renames are retried. If the new-file move fails, the updater retries
restoring the outgoing binary. Manual recovery is needed only if both moves
fail, or if power loss or process termination interrupts the gap between them.
External locks are ordinary transient rename failures; the updater does not
lock the install directory.

Before changing any sidecar, stop Core and its service. After a logged double
failure, Core may still be running from the mapped `superseded` file even though
the normal install path is empty.

When the error was logged, use its exact `target` and `superseded` paths; the
executable may not be named `zaparoo.exe`:

```text
ERR binary swap left the install path empty; rename the superseded binary back
to the target path to recover target=... superseded=...
```

A power loss in the gap cannot write that log entry. Sidecar names are derived
from the target in the same directory, so inspect hidden files there. For a
target named `zaparoo.exe`:

- `zaparoo.zaparoo-update-old.exe` (or `-old-1`, `-old-2`, up to `-old-7`) is an
  outgoing binary moved aside by a swap. Use the newest slot belonging to the
  interrupted attempt when that can be identified.
- `zaparoo.zaparoo-update-backup.exe` is the durable copy of the outgoing binary.
  If no unambiguous `-old` slot exists, copy this backup to the empty target.
- `zaparoo.zaparoo-update-new.exe` is the verified candidate, not the recovery
  source.

With Core stopped, rename the logged or identified `-old` file to the target,
or copy the backup there, then start the service. That restores the version the
device was already running; the pending update is unwound on the next start.
Never put the `-new` file in place by hand: the durable marker still records an
interrupted install, so recovery must restore the known-good outgoing binary.

### A device will not start after going back to an older version

Core refuses to start when the user database has been migrated by a newer build
than the one now installed:

```text
database schema is newer than this binary supports: database is at version
20260828000000 but this binary only supports up to 20260818120000, update to a
newer version or reinstall the previous version
```

This is deliberate. The media database is rebuilt in the same situation because
a reindex reconstructs it, but the user database holds history, mappings,
profiles and favourites that nothing can reconstruct, so starting by discarding
them would be worse than not starting.

Rolling back through Core never causes this: the update snapshot restores the
database alongside the binary, so the schema goes back with it. What causes it
is replacing the binary some other way — reinstalling an older release by hand,
or a package manager moving the install backwards.

On a platform with a service supervisor this presents as a service that keeps
restarting rather than an error, because each attempt fails the same way. On
MiSTer there is no journal at all. Either way the explanation is in
`core.log`, and it names both versions.

Recover in this order:

1. **Reinstall the newer version.** Always works and loses nothing, and is the
   right answer whenever going back was not deliberate.
2. **Restore a user database backup taken before the newer build ran**, from
   `backups/` in the data directory, then start the older version. Check the
   backup predates the upgrade — restoring one taken *after* it puts the same
   schema back and fails identically.
3. If neither is possible, the database has to be moved aside and recreated
   empty, which loses that data. Keep the file: a later build that understands
   the schema can still open it.

Automatic backups are pruned to the most recent three, so a device that ran the
newer build for several boots may no longer hold a backup old enough for step 2.
Take one before deliberately moving a device back.

## Automatic installation controls

Automatic installation has two independent controls. Neither one changes the
other:

- `[updates] install = true` is an explicit per-device opt-in. Its default is
  `false` and remains off when update checking is disabled.
- `rollout` is a signed field on one release. It limits which opted-in devices
  may install that release automatically. It never enables installation.

| Device `install` | Release `rollout` | Result |
|---|---:|---|
| `false` | 0–100 | Check/notification only; no automatic install |
| `true` | 0 | Automatic install held for every device |
| `true` | 5 | Deterministically selected 5% of opted-in devices may install |
| `true` | 100 | Every opted-in eligible device may install |

The bucket is stable for one device and release, widens monotonically for that
release, and is re-salted by a different release tag. Manual `update.apply`
ignores rollout; authorization, platform, power, storage and activity gates
still apply.

### Platform support

Raw-binary automatic installation is intended for non-package-managed Windows,
Linux, SteamOS, Bazzite, ChimeraOS, ReplayOS, RetroPie, Recalbox, ZapOS, MiSTeX,
LibreELEC, MiSTer and Batocera installs that pass platform preflight.

- Windows must be able to create/remove an install-directory probe and open the
  target for rename. Protected installer-owned locations remain on the Windows
  installer path; Core reports `eligibility: unsupported` and never elevates.
- Managed MiSTer/Batocera installations remain on their package-manager path.
  Batocera payload files are installed only for unmanaged archive installs.
- macOS is excluded. Its supported update path must operate on a signed app
  bundle or App Store installation, not replace one raw executable.

### Readiness checklist

Before changing a device or publishing test metadata:

1. Confirm OTA validate passed and inspect an OTA promote dry-run artifact.
2. Record tag, channel, rollout, `min_upgrade_from`, manifest generation and
   asset digest/size.
3. Confirm device version, platform/architecture, channel, managed status,
   `updates.check`, `updates.install`, install target and preflight eligibility.
4. Confirm free space and power state. Automatic installs require charging,
   external power, or at least 40% battery where battery status is available.
5. Read current update result and ensure no unresolved pending or quarantined
   marker exists.
6. Take a recoverable UserDB backup and confirm local console, SSH, removable
   media or installer recovery access before any destructive test.
7. Test one device at a time. Device changes and workflow dispatches require
   explicit approval.

### Observation and evidence

Use `update.check` to record selected version, eligibility, `autoInstall`,
`rolloutHeld`, deferral reason/time and previous update outcome. Device
`updater/state.json` also carries `lastCheckAt`, `lastCheckOK`, `checkFailures`
and `lastOfferedVersion`; these are diagnostics, not permission to install.
Collect updater logs and relevant Sentry/support reports. Sentry reporting is
opt-in and support reports are incomplete, so absence of either is not proof of
fleet health.

For every validation record:

```text
operator/date:
release tag/channel/generation/rollout/floor:
device label/platform/arch/from/to:
DeviceID bucket result (never publish raw DeviceID):
power/free-space/managed status:
check, install, restart and confirmation timestamps:
outcome and deferral details:
binary/payload/database/marker/sidecar checks:
logs, screenshots or downloaded evidence:
go/no-go decision:
```

Stop immediately on `rollbackBlocked`, `recoveryRequired`, an empty Windows
boot target, unrecovered database, payload mismatch, signature/generation
inconsistency, or repeated unexplained check/install failure. Set rollout to 0
while investigating. Withdraw when release safety or metadata integrity is in
doubt. Do not widen based only on elapsed time.

## Final validation matrix

Run this only after updater code, transaction-boundary tests, documentation and
post-merge CI are complete.

Stopping Core within the confirmation window counts as a failed start and rolls
the update back. That is correct — a process killed partway through is
indistinguishable from one that crashed — but it means a device stopped by hand
between the restart and the confirmation will report a rollback that nothing
was actually wrong with. Let an install confirm before stopping it, and read any
rollback that follows a manual stop as an artifact of the stop.

### Windows

On a writable portable or per-user install, cover apply/confirm, forced startup
rollback, failed incoming rename with successful undo, sharing/scanner
violations, repeated swaps, sidecar cleanup and a read-only outgoing executable.
Exercise the double-failure/manual-recovery path only with local recovery access.
Confirm the restart actually happens: a Windows install replaces the binary and
stops the service well before anything re-execs, so an update that installs and
never comes back looks from the outside like one that simply took a while.
On an installer-owned protected location, verify unsupported eligibility and
installer guidance with no elevation or mutation. On battery-equipped hardware,
include the 20% manual and 40% automatic thresholds.

### Steam Deck / SteamOS

Verify checking while charging/discharging, automatic rejection at 39%,
acceptance at 40%, charger removal after staging but before the immediate
pre-install check, normal confirmation, and failed-start rollback without a
supervisor restart loop.

### Batocera

For an unmanaged archive install, verify all seven payload destinations and
modes, backup of existing files, original-absence handling for new files,
confirmation cleanup and failed-start restoration of binary, payload and
UserDB. A managed installation must remain payload-ineligible and untouched.

### Cross-platform confirmation cycle

Observe one complete signed check → automatic install → restart → confirmation
on MiSTer, SteamOS, writable Windows, unmanaged Batocera, and one install that
is not package-manager managed. Verify generation/asset selection, gates,
restarted version, update result, marker/sidecar cleanup and retained UserDB
data on each.

Pick that set for the differences that actually reach the updater: a target
filesystem where `chmod` is a no-op and directory fsync is unsupported
(MiSTer's exfat), a battery-backed device (Steam Deck), the rename-aside
replacement backend (Windows), platform-owned payload extras (Batocera), and an
install `ManagedByPackageManager` reports false for, since that is the only
shape where automatic installation is permitted at all. Every release is built
against glibc, so libc variance is not one of the axes.

## Controlled beta rollback and withdrawal drill

Run a deliberately bad build only on beta after the normal hardware matrix
passes. Build it so `-version` succeeds but normal service startup fails; this
exercises watchdog rollback rather than archive/probe rejection.

Contain it with all of these controls:

1. Put controlled devices on a unique precursor version higher than every
   public release.
2. Give the bad release a version higher than that precursor. `min_upgrade_from`
   only tests the running version against the floor; a release is separately
   refused unless it is newer than the version already running, so a bad build
   at or below the precursor is never installed and the drill proves nothing.
3. Set bad release `min_upgrade_from` to that precursor so ordinary beta devices
   cannot qualify.
4. Check release-tag bucketing offline and select intended controlled devices
   inside a 5% rollout without publishing raw DeviceIDs.
5. Inspect a promote dry-run before publishing. Never use stable and never widen
   the deliberately bad release above 5%.

Require automatic selection/install, failed confirmation, complete binary,
payload and UserDB rollback, durable `rolledBack` result and return to the
precursor. Then dispatch OTA withdraw and verify signed public metadata advances
and no longer offers the release. Record decision-to-verification time against
the under-five-minute target. Confirm a non-qualifying beta device never
installs it.

## Opt-in beta rollout evidence gates

After withdrawing the bad build, use a good beta release to exercise 5% → 25%
→ 100% rollout among devices whose owners explicitly enabled
`[updates] install = true`. Dry-run and review each metadata change first.

- Dwell at least 24 hours at 5%, 72 hours at 25%, and 7 days at 100%.
- Require explained outcomes from controlled matrix devices at each rung.
- Require zero unexplained `rollbackBlocked`/`recoveryRequired` outcomes,
  Windows empty-target incidents, updater-related Sentry regressions or
  corroborated support failures.
- Confirm expected deferrals/backoff recover without intervention.
- Record explicit go/no-go before widening.

At any blocker, set rollout to 0, preserve evidence and withdraw if safety is in
doubt. This validates opt-in operation only; automatic installation remains off
by default.

## Setup

### Secrets and environments

The signing key and the CDN credentials live on GitHub Environments, not on the
repository, so a workflow file alone cannot reach them.

`ota-publish` is the only one, and it has **required reviewers configured.**
Used by promote, rollout and withdraw. Holds:

- `UPDATE_SIGNING_KEY` — PEM-encoded ed25519 private key
- `BUNNY_STORAGE_ZONE`, `BUNNY_STORAGE_ZONE_PASSWORD`
- `BUNNY_API_KEY`, `BUNNY_PULL_ZONE_ID`

Every path to the signing key therefore goes through a human. What stops that
being circumvented by editing a workflow is CODEOWNERS:
`.github/workflows/ota-*.yml`, `.github/actions/ota-metadata/` and
`scripts/generate-update-manifest/` are owned by `@ZaparooProject/admins`.
Changing any of them needs the same review as changing the signing key. So is
`pkg/service/updater/otameta/keys/`, because adding a file there makes every
device trust manifests signed with that key.

### CDN edge rule

Release archives are served from GitHub, not from Bunny, so the pull zone has
an edge rule that redirects requests on to GitHub releases. The four metadata
files are the exception and must be served from the storage zone:
`manifest.yaml`, `manifest.yaml.sig`, `checksums.txt` and `checksums.txt.sig`.

If a metadata file is not exempted from that rule it still uploads to storage
perfectly well, and then the public URL answers with a 302 to a GitHub release
asset that does not exist. The publish succeeds and step 8 below fails,
reporting that the edge is serving older metadata — which is misleading, since
what actually came back was a redirect page rather than stale bytes. Anything
added to the publish set needs the edge rule updated first.

### Key rotation

Public keys are embedded in the client from
`pkg/service/updater/otameta/keys/`, one bare base64 key per file, named for its
key id — `k1.pub` is key id `k1`. The manifest names the key it was signed with
in `key_id`, and an unknown `key_id` is a hard reject: clients do not try every
key, because that would defeat revocation.

Publishing builds its verification key from the same directory, so a `key-id`
with no matching `.pub` file fails the run before the signing key is loaded
rather than publishing a manifest no device trusts.

To rotate: add the new `.pub` file and ship a release embedding both keys, wait
for it to reach the fleet, then change the `key-id` input default in
`.github/actions/ota-metadata/action.yml` and swap the signing secret. Devices
that never took the intermediate release stop updating rather than accepting an
unknown key, so do not rush the middle step. Removing an old key from the
directory is what revokes it, and should be a separate later release.

## How a publish is verified

Every publishing run does the same thing, in this order:

1. Read the live metadata from the Bunny **storage origin**, not the CDN. A
   stale edge response would hand us an old generation, and republishing a
   generation devices have already seen stalls updates for everyone holding
   that watermark.
2. Verify the live signatures before trusting anything in them.
3. Apply exactly one change, and stamp a generation strictly greater than the
   live one.
4. Run the client's own asset selection over the generated manifest, for every
   platform and architecture the build produces. Two archives a device could
   both install fails anywhere in the manifest; a missing archive fails on the
   newest release in each channel, which is the one devices are offered. This
   runs before the signing key is loaded.
5. Sign both documents and verify those signatures locally.
6. Check the publish directory holds those four files and nothing else, and
   that no archive URL points anywhere but GitHub.
7. Upload and purge the pull zone.
8. Re-fetch all four files from the public URL — the plain URL a device would
   request, through the edge cache — and require them to be byte-identical to
   what was signed. Cache-busting is deliberately not used: the thing being
   checked is what the edge serves, and bypassing the cache would verify the
   storage origin instead and miss the case that matters. Step 7's purge is what
   makes the edge current, so a mismatch here means the purge has not landed
   yet, and the step retries for a while because propagation is not instant.
   Each failed attempt logs the `Cdn-Cache` header per file: a `HIT` means the
   purge did not take, a `MISS` means the origin itself is serving something
   other than what was uploaded. **A run that reaches this step and fails it has
   published something unverified** — investigate before dispatching anything
   else.

The signed metadata is attached as a build artifact on every run, dry or not,
and kept for 90 days.

## Retention

The manifest keeps the newest five stable and five beta releases. Older entries
are pruned from the manifest *and* from `checksums.txt` on every promote — if
they were only pruned from one, a dropped release would stay installable
through the other. Release notes for superseded releases are truncated; the
newest release in each channel always keeps its full notes.
