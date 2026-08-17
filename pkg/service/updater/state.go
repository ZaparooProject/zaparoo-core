// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	// currentStateVersion is the schema of state.json this build writes.
	currentStateVersion = 1

	stateFileName        = "state.json"
	manifestCacheName    = "manifest.yaml"
	stateDirPerm         = 0o750
	stateFilePerm        = 0o600
	maxCachedManifestLen = maxManifestBytes
)

// stateMu serialises readers and writers of state.json within the process. The
// file is replaced by rename so a concurrent reader never sees a torn write,
// but two updates racing could still lose a generation without this.
var stateMu syncutil.Mutex

// updaterState is the small amount of update bookkeeping that has to outlive
// the process. It lives in the data directory rather than the user database on
// purpose: the database is included in backups, so a watermark stored there
// would roll backwards whenever an old backup was restored, which is a
// self-inflicted downgrade window.
type updaterState struct {
	// ManifestSeenAt records when the generation below was accepted. It is for
	// operators reading the file; nothing in the verification path reads a clock.
	ManifestSeenAt time.Time `json:"manifestSeenAt"`
	// ManifestETag and ManifestLastModified let the next check ask the CDN for
	// the manifest only if it changed. The cached bytes are re-verified on every
	// use, so a tampered cache is caught rather than trusted.
	//
	// Both are kept because which one fires depends on the origin. Bunny does
	// not generate ETags; it only forwards one the origin sends, and Bunny
	// Storage sends none, so in production Last-Modified is the validator that
	// actually works. ETag is still honoured when present because it is the
	// stronger of the two and servers that offer both prefer it.
	ManifestETag         string `json:"manifestETag"`
	ManifestLastModified string `json:"manifestLastModified"`
	// ManifestGeneration is the highest generation this device has accepted.
	ManifestGeneration int64 `json:"manifestGeneration"`
	StateVersion       int   `json:"stateVersion"`
}

// stateDirFor returns the updater's private directory inside the data dir.
func stateDirFor(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "updater")
}

// loadState reads state.json. A missing, unreadable or corrupt file is not an
// error: it yields a zero state, which accepts any generation. Failing closed
// here would mean one bad write permanently stops a device updating, which is
// a worse outcome than the replay the watermark exists to prevent.
func loadState(dir string) updaterState {
	if dir == "" {
		return updaterState{}
	}

	//nolint:gosec // path is derived from the platform data dir
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Msg("could not read updater state, treating as unseen")
		}
		return updaterState{}
	}

	var st updaterState
	if err := json.Unmarshal(data, &st); err != nil {
		log.Warn().Err(err).Msg("updater state is corrupt, treating as unseen")
		return updaterState{}
	}

	// A newer build may have written fields this one does not know about. The
	// generation means the same thing in every version, so it is still honoured
	// for the replay check; saveState is what refuses to overwrite the file.
	if st.StateVersion > currentStateVersion {
		log.Warn().
			Int("found", st.StateVersion).
			Int("understood", currentStateVersion).
			Msg("updater state was written by a newer version, not updating it")
	}
	return st
}

// saveState replaces state.json atomically.
func saveState(dir string, st updaterState) error {
	if dir == "" {
		return nil
	}
	if st.StateVersion > currentStateVersion {
		return nil
	}
	st.StateVersion = currentStateVersion

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding updater state: %w", err)
	}
	return writeFileAtomic(dir, stateFileName, data)
}

// loadCachedManifest returns the manifest bytes kept alongside the cache
// validators. The caller re-verifies them, so a caller receiving nil simply
// refetches.
func loadCachedManifest(dir string) []byte {
	if dir == "" {
		return nil
	}

	path := filepath.Join(dir, manifestCacheName)
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 || info.Size() > maxCachedManifestLen {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the platform data dir
	if err != nil {
		log.Debug().Err(err).Msg("could not read cached update manifest")
		return nil
	}
	// Re-checked against what was actually read: the file can grow between the
	// stat and the read, and the cap has to hold for the bytes in hand.
	if int64(len(data)) > maxCachedManifestLen {
		return nil
	}
	return data
}

func saveCachedManifest(dir string, data []byte) error {
	if dir == "" {
		return nil
	}
	return writeFileAtomic(dir, manifestCacheName, data)
}

// writeFileAtomic writes name inside dir via a temporary file and a rename, so
// a reader either sees the previous contents or the new ones and never a
// partial write. The directory is synced afterwards so the rename survives a
// power cut, which these devices take regularly.
func writeFileAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("creating updater state directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return fmt.Errorf("creating temporary updater state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Only fires on the failure paths; the rename below has already moved
		// the file away by the time a successful write returns.
		if removeErr := os.Remove(tmpName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Debug().Err(removeErr).Msg("could not remove temporary updater state file")
		}
	}()

	if err := tmp.Chmod(stateFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting updater state file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing updater state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing updater state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing updater state file: %w", err)
	}

	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("replacing updater state file: %w", err)
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	handle, err := os.Open(dir) //nolint:gosec // path is derived from the platform data dir
	if err != nil {
		return fmt.Errorf("opening updater state directory for sync: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()

	// Windows cannot flush a directory handle through os.File.Sync.
	if syncErr != nil && (runtime.GOOS != "windows" || !errors.Is(syncErr, os.ErrPermission)) {
		return fmt.Errorf("syncing updater state directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing updater state directory: %w", closeErr)
	}
	return nil
}
