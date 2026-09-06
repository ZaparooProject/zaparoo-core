# MiSTer VM integration rig

Headless ARM VM running unchanged MiSTer Zaparoo against stock MiSTer userspace, a config-adapted official kernel, real exFAT/ext4 filesystems and a small Main substitute. Tests internal service/platform behavior, **not FPGA game execution**.

Sources and scripts are versioned here. Downloads, compiler outputs, disks, guest state and results stay under ignored `_scratch/`. No host mounts, bridges, USB grants or automatic package installation.

## Prerequisites

Tested host: Fedora 44 x86-64, QEMU 10.2.2, Python 3.14, ARM GCC 10.2.1.

- Linux x86-64 host, Python **3.12+**.
- `qemu-system-arm` supporting `virt-10.2`, `qemu-img`, `mke2fs`, `debugfs`, `mkfs.exfat`, `7z`.
- Kernel build tools: `make`, host GCC, ARM Linux hard-float cross-compiler, binutils, flex, bison, Perl, LZ4 and kernel build dependencies. OpenSSL headers must include ENGINE support; ELF development headers may also be required.
- Several GiB free space for source/build outputs and images. Sparse disks have 2 GiB virtual capacity per base/overlay, not necessarily 2 GiB physical allocation.
- WebSocket client for runner/integration tests. Install into a project-local venv if needed:

```sh
python3 -m venv _scratch/mister-vm-venv
_scratch/mister-vm-venv/bin/pip install -r scripts/mister-vm/requirements.txt
```

Examples below use `python3`; use the venv interpreter instead when dependencies are there. Unit tests and setup use standard library only. Do not use `python -O`; runner requires assertions.

Compiler provisioning remains a host prerequisite, not a hidden download. GCC 10.2.1 is verified; other compiler versions are not yet validated. Setup records compiler version and output hashes, not a claim of bit-identical builds across toolchains.

## Prepare assets

From repository root:

```sh
python3 scripts/mister-vm/setup.py --cross-compile arm-linux-gnueabihf-
```

If compiler is not on PATH, supply its absolute filename prefix, ending in `-`:

```sh
python3 scripts/mister-vm/setup.py \
  --cross-compile /path/to/toolchain/bin/arm-none-linux-gnueabihf-
```

This downloads/checks kernel and userspace hashes from `pins.json`, builds the kernel, edits a copy of stock init offline with `debugfs`, creates regular image files and populates exFAT inside a provisioning VM. Base image contains no Zaparoo binary or user database; each test installs its selected binary.

Defaults:

- Assets: `_scratch/mister-vm-assets/`
- Download cache: `_scratch/mister-vm-downloads/`

Options: `--assets PATH`, `--download-cache PATH`, `--jobs N`, `--host-include PATH`, `--host-bin PATH`. Last two allow existing local headers/tools without changing host installation. In this investigation the existing scratch GCC 10.2.1, ENGINE headers and LZ4 were supplied with these options; ordinary hosts with those dependencies installed do not need the overrides.

Populate verified cache separately, without building:

```sh
python3 scripts/mister-vm/setup.py --download-only
```

Completed setup is idempotent: validates input pins and base/kernel hashes, then returns. Incomplete setup fails closed on rerun; inspect `kernel-build.log`, `rootfs-build.log`, `provision.log` and `FAILED`, then choose a fresh `--assets` path. No automatic destructive reset. Bad/incomplete downloads remain for diagnosis; no automatic checksum repinning or replacement of invalid cached assets.

Do not rebuild or modify a base while overlays depend on it. Kernel source remains unmodified: only configuration enables virtio/PL011/RTC and disables physical FPGA drivers. Stock init adaptations disable SSH/FTP/Samba, replace framebuffer console with serial shell, discard audio and provide controlled startup. VM setup never runs the Windows installer.

## Run local build

Build Core using its normal repository workflow (`task mister:build-arm`), then:

```sh
python3 scripts/mister-vm/run.py --binary _build/mister_arm/zaparoo.sh
python3 scripts/mister-vm/run.py --binary _build/mister_arm/zaparoo.sh --scenario service
```

`launch` is default scenario. Despite `.sh` extension, selected input must be an extracted ELF32 little-endian ARM executable—not an archive or shell script. Runner verifies copied binary hash inside guest before starting it.

## Run pinned published release

```sh
python3 scripts/mister-vm/setup.py --release-only
python3 scripts/mister-vm/run.py \
  --binary _scratch/mister-vm-assets/releases/2.17.2/zaparoo.sh \
  --expect-version 2.17.2
python3 scripts/mister-vm/run.py \
  --binary _scratch/mister-vm-assets/releases/2.17.2/zaparoo.sh \
  --expect-version 2.17.2 --scenario service
```

Published MiSTer **2.17.2 passed both scenarios**. Archive URL/hash are pinned; only the exact `zaparoo.sh` member is copied to a fixed output path. No release publication, updater metadata, signing or rollout action is performed. Other release versions can be supplied as local binaries; their API compatibility is not assumed.

## Scenarios

**Launch:** boot/environment/hash checks → health/API/file reader → real indexing → scan synthetic SNES path with spaces/ampersand → generated MGL/FIFO → simulated SNES/real active media → hold removal → Menu/closed play history. Includes missing-media characterization.

**Service:** boot/hash/API/SQLite checks → real scan/removal → restart with exact history entry and reader identity preserved → API stops → sync and cold boot of same private overlay → history/identity preserved and tmpfs reset. This is orderly persistence, not unclean-power-loss durability.

Core intentionally trusts media paths to avoid scanning large ZIPs during launch. Successful submission/optimistic active media is **not verified game loading**. Missing-media test keeps these observations separate; it does not demand hot-path path validation.

Main substitute supports only Menu and one-file SNES MGL, using actual FIFO reads and truncating status-file writes. Its delay and rejection policy are synthetic, not established hardware Main parity. No manual-selection/recents, save states, USB/NFC or physical timing coverage.

## Isolation, artifacts and failures

Each invocation owns an overlay directly backed by `sd.raw`, read-only fixture-transfer disk, unique artifact directory and dynamic **localhost-only** port. QEMU user networking uses `restrict=on`; no host filesystem sharing. Parallel invocations are tested. A port bind collision fails at boot before API access.

Options:

- `--assets PATH`: select prepared assets.
- `--expect-version STRING`: exact API version assertion.
- `--timeout SECONDS`: whole-run deadline, default 360; bounded shutdown/evidence finalization follows expiry.
- `--keep-disk`: retain overlay/fixture disk for diagnosis; no automatic resume.

Artifacts under `<assets>/runs/<unique-id>/`:

- `run.json`: overall outcome, input/source hashes, QEMU version, port, process exits, base integrity and cleanup evidence. Base and kernel are checked against setup's `ready.json` record before boot, so a run cannot pass against replaced assets.
- `results.json`, `notifications.json`: scenario checks and API evidence, including partial progress.
- `boot-0.log` (plus `boot-1.log` for service): serial boot/command output.
- `scenario.log`: readable scenario transcript.
- `failure.txt`: traceback when run fails.

Early setup errors can lack guest logs. Exit 0 means success; exit 1 means failure. `run.json` is authoritative if scenario succeeds but integrity/cleanup subsequently fails.

QEMU is reaped on success, failure, deadline and SIGTERM. Writable disks/staging are deleted by default; artifacts remain. SIGKILL or host failure cannot guarantee cleanup. Never delete an active run's directory. Retained guest disks can contain generated identity/runtime data: keep them local and uncommitted. Use trusted Zaparoo binaries; this is not a hardened hostile-code sandbox.

## Test tooling

```sh
python3 scripts/mister-vm/test_unit.py
python3 scripts/mister-vm/test_integration.py
```

16 unit tests cover simulator parsing, FIFO command framing and checksum-cache behavior without network/VM dependencies. Seven integration tests cover parallel local/release launch, published-release persistence, invalid input, wrong version, timeout, termination and propagation of unrelated `SystemExit` exceptions. Intentional `FAIL:` run lines are expected; unittest's final `OK` determines suite success. Integration tests require setup, pinned release download and local `_build/mister_arm/zaparoo.sh`; override `--assets` / `--binary` when needed.

Tests verify process exit, released ports (accounting for TCP TIME_WAIT), immutable base/kernel and disk cleanup. One integration case intentionally retains disks per suite execution.

No CI job is added yet: host compiler/QEMU prerequisites and runtime/storage budget should be selected explicitly before wiring into mandatory CI.
