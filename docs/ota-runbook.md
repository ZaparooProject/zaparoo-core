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

The first promote is different. The manifest currently live predates signing, so
there is no `manifest.yaml.sig` to verify and no `generation` to advance from.
Publishing on top of an unsigned manifest is refused by default — a stripped
signature would otherwise rewind the counter and stall updates for every device
holding a watermark. To bootstrap, dispatch that one promote with
`generation_floor` set above any generation already published (0 has never been
published, so any positive value works). Every promote after it verifies a
signature and needs no floor.

Start a significant release at a partial `rollout` — 5, then 25, then 100 over a
few days — and widen it with **OTA rollout**. Bucketing is salted per release,
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
Changing any of them needs the same review as changing the signing key.

### Key rotation

The public key is embedded in the client at
`pkg/service/updater/update_signing.pub` and the manifest names the key it was
signed with in `key_id`. An unknown `key_id` is a hard reject — clients do not
try every key, because that would defeat revocation.

To rotate: ship a release that embeds both the old and new keys, wait for it to
reach the fleet, then change the `key-id` input default in
`.github/actions/ota-metadata/action.yml` and swap the signing secret. Devices
that never took the intermediate release stop updating rather than accepting an
unknown key, so do not rush the middle step.

## How a publish is verified

Every publishing run does the same thing, in this order:

1. Read the live metadata from the Bunny **storage origin**, not the CDN. A
   stale edge response would hand us an old generation, and republishing a
   generation devices have already seen stalls updates for everyone holding
   that watermark.
2. Verify the live signatures before trusting anything in them.
3. Apply exactly one change, and stamp a generation strictly greater than the
   live one.
4. Sign both documents and verify those signatures locally.
5. Check the publish directory holds those four files and nothing else, and
   that no archive URL points anywhere but GitHub.
6. Upload and purge the pull zone.
7. Re-fetch all four files from the public URL, cache-busted, and require them
   to be byte-identical to what was signed. Retries for a while, because purge
   propagation is not instant. **A run that reaches this step and fails it has
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
