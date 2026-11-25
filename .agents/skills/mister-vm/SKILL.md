---
name: mister-vm
description: Set up and run the repository's headless MiSTer VM integration rig. Use for MiSTer launch, reader, restart or persistence testing; selecting local or published ARM binaries; diagnosing VM artifacts; or maintaining scripts/mister-vm. For FPGA cores, HDMI capture, physical readers or a real ROM corpus, use real MiSTer hardware instead. For platform-neutral Core behavior on the desktop, use the core-sim skill.
license: GPL-3.0-or-later
---

# MiSTer VM

## Start here

Repository root is three directories above this skill directory. Resolve references from this skill's location, not a guessed working directory. Run commands from that root.

Read [runner README](../../../scripts/mister-vm/README.md) and [asset pins](../../../scripts/mister-vm/pins.json) before setup or execution. README is authoritative for prerequisites, flags and supported scenarios. Do not use superseded `_scratch/mister-vm/*.py` prototype entrypoints.

## This rig or the hardware

Prefer this rig for anything it already covers: service start/stop/restart, reader connection and identity, token and play history across restart and cold boot, hold versus tap scan modes, MGL generation, indexing and the JSON-RPC surface. It is disposable, parallel-safe and writes machine-readable evidence, so a result does not depend on operator technique.

Use a real MiSTer when the question needs a loaded core, a screen, a physical reader or a real ROM corpus: FPGA behavior, HDMI capture, USB/NFC hardware, recents and manual selection, save states, physical timing, or performance against tens of gigabytes of real media on exFAT. That device is shared, destructive state; this rig is not.

A failure here is worth reproducing on hardware before calling it a Core bug. A hardware failure is worth reproducing here before calling it a platform bug.

## Choose inputs deliberately

- Determine whether user wants a local build or pinned published release. Source edits do not rebuild the selected binary.
- For local changes, use normal build workflow: `task mister:build-arm`. Select `_build/mister_arm/zaparoo.sh` explicitly.
- Despite `.sh` extension, input is an extracted little-endian ARM ELF32 executable. Do not execute it on host or pass a release archive to runner.
- For published baseline, use `setup.py --release-only` and version from `pins.json`; select `<assets>/releases/<version>/zaparoo.sh` with matching `--expect-version`.
- Default assets: `_scratch/mister-vm-assets`. In a worktree, reuse assets only through an explicit known `--assets PATH`; do not broadly search sibling checkouts or home directories.

## Prepare when necessary

Reuse completed assets. Otherwise follow README prerequisites and run `python3 scripts/mister-vm/setup.py` with appropriate cross-compiler prefix and optional local header/tool paths. `--download-only` populates verified cache without building.

Do not install host packages, widen permissions, repin downloads or introduce privileged containers to work around failures without appropriate user approval. Setup never needs host block devices or filesystem mounts. Preserve failed setup logs; choose a fresh asset directory rather than overwriting a base or incomplete build.

## Run bounded scenarios

```sh
python3 scripts/mister-vm/run.py --binary _build/mister_arm/zaparoo.sh
python3 scripts/mister-vm/run.py --binary _build/mister_arm/zaparoo.sh --scenario service
```

- `launch`: real indexing, file-reader scan, escaped MGL, actual Main FIFO, simulated SNES, hold removal/Menu and play history.
- `service`: scan/removal, restart, API stop, exact history/reader identity across cold boot, and tmpfs reset.
- Use `--timeout` for whole-run budget; do not disable assertions with `python -O`.
- Runs own their overlays, fixture disks, localhost ports and artifacts. Parallel runs are supported; never share writable overlays.
- Use `--keep-disk` only for diagnosis. Never delete an active run directory or modify backing images while overlays depend on them.

## Interpret results correctly

Read printed artifact directory's `run.json` first: authoritative overall outcome, including integrity and cleanup. Then inspect `results.json`, `failure.txt`, `scenario.log`, `boot-*.log` and notifications as needed. Early failures may have no guest logs.

Report selected binary hash/API version, scenario, exact failure and artifact path. Distinguish provisioning/dependency failures, harness assumptions and actual Core behavior. Rig logs/disks contain synthetic fixtures and generated guest identity; keep them local and uncommitted. Do not generalize permission to inspect real user databases or logs.

**Core deliberately trusts media paths to avoid large ZIP lookups during launch.** Successful submission and optimistic active media are not verified loading. Simulator rejection is independent evidence, not a requirement for Core to add hot-path path validation. Preserve this accepted contract.

## Maintain and verify

```sh
python3 scripts/mister-vm/test_unit.py
python3 scripts/mister-vm/test_integration.py
```

Read README for integration prerequisites and overrides. Deliberate `FAIL:` lines occur in negative runner tests; unittest's final outcome determines suite success. Do not suppress assertions to force compatibility with another release.

After tooling changes, run relevant syntax/unit/integration checks. Apply normal repository validation separately for Go/Core changes. Do not patch Core merely to accommodate simulator without an agreed behavior change.

## Fidelity boundaries

Exercises stock userspace and real exFAT/ext4, SQLite, uinput and inotify. Kernel configuration/init are adapted for QEMU. Main substitute supports Menu and one-file SNES MGL; timing/rejection policy is synthetic. No FPGA/game emulation, USB/NFC hardware, recents/manual selection, save-state, physical timing or unclean-power-loss guarantee. Expand only through an agreed scenario. No OTA/release publication or deployment actions belong to this skill.
