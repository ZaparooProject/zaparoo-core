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

package backup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deltaTestServer is a fake backup server that keeps the device's latest
// snapshot and takes both full and delta commits against it.
type deltaTestServer struct {
	t        *testing.T
	server   *httptest.Server
	manifest map[string][]remoteManifestEntry
	objects  map[string]struct{}
	root     string
	// deltaStatus and deltaCode, when set, answer every delta commit with
	// that error instead of applying it.
	deltaCode string
	deviceID  string
	// deltaBodies records the raw body of every delta commit received.
	deltaBodies  [][]byte
	deltas       []remoteDeltaRequest
	uploaded     []string
	mu           syncutil.Mutex
	deltaStatus  int
	fullCommits  int
	backups      int
	rateLimits   int
	omitRootHash bool
}

func newDeltaTestServer(t *testing.T) *deltaTestServer {
	t.Helper()
	s := &deltaTestServer{t: t, objects: make(map[string]struct{}), deviceID: "device-1"}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

// rootOf derives a root hash that is deliberately not the manifest hash.
func (*deltaTestServer) rootOf(manifestHash string) string {
	return sha256Hex([]byte("root:" + manifestHash))
}

func (s *deltaTestServer) commit(backupType string, version int, coreVersion *string) remoteBackupResponse {
	s.backups++
	request := remoteSnapshotRequest{
		BackupType: backupType, SchemaVersion: version, CoreVersion: coreVersion, Categories: s.manifest,
	}
	response := testCommittedRemoteResponse(s.t, "backup-"+strconv.Itoa(s.backups), platformids.Mister, &request)
	s.root = s.rootOf(response.ManifestHash)
	if !s.omitRootHash {
		response.ManifestRootHash = s.root
	}
	return response
}

func (s *deltaTestServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/device/heartbeat":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/device/me":
		writeJSON(s.t, w, remoteDeviceMeResponse{ID: s.deviceID, Name: "test", BackupActive: true})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backup-objects/check":
		var req remoteCheckRequest
		if !assert.NoError(s.t, json.NewDecoder(r.Body).Decode(&req)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		missing := []string{}
		for _, hash := range req.Hashes {
			if _, ok := s.objects[hash]; !ok {
				missing = append(missing, hash)
			}
		}
		writeJSON(s.t, w, remoteCheckResponse{Missing: missing})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/device/backup-packs/"):
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(s.t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for hash := range parseTestPack(s.t, body) {
			s.objects[hash] = struct{}{}
			s.uploaded = append(s.uploaded, hash)
		}
		writeJSON(s.t, w, remotePackResponse{PackHash: sha256Hex(body), CreatedAt: time.Now().UTC()})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups":
		var req remoteSnapshotRequest
		if !assert.NoError(s.t, json.NewDecoder(r.Body).Decode(&req)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.fullCommits++
		s.manifest = req.Categories
		writeJSON(s.t, w, s.commit(req.BackupType, req.SchemaVersion, req.CoreVersion))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/device/backups/delta":
		s.handleDelta(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/device/backups":
		writeJSON(s.t, w, remoteListResponse{StorageUsedBytes: 99, StorageQuotaBytes: 1 << 30})
	default:
		s.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *deltaTestServer) handleDelta(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if !assert.NoError(s.t, err) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var req remoteDeltaRequest
	if !assert.NoError(s.t, json.Unmarshal(body, &req)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.deltaBodies = append(s.deltaBodies, body)
	s.deltas = append(s.deltas, req)
	if s.rateLimits > 0 {
		s.rateLimits--
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	if s.deltaStatus != 0 {
		s.writeError(w, s.deltaStatus, s.deltaCode)
		return
	}
	if req.BaseManifestRootHash != s.root {
		s.writeError(w, http.StatusConflict, remoteErrorBaseMismatch)
		return
	}
	next := make(map[string][]remoteManifestEntry)
	for category, entries := range s.manifest {
		removed := stringSet(req.Remove[category])
		upserted := make(map[string]struct{})
		for _, entry := range req.Upsert[category] {
			upserted[entry.Path] = struct{}{}
		}
		for _, entry := range entries {
			_, isRemoved := removed[entry.Path]
			_, isUpserted := upserted[entry.Path]
			if !isRemoved && !isUpserted {
				next[category] = append(next[category], entry)
			}
		}
	}
	for category, entries := range req.Upsert {
		for _, entry := range entries {
			assert.Contains(s.t, s.objects, entry.SHA256, "delta upsert must be uploaded first")
			next[category] = append(next[category], entry)
		}
	}
	s.manifest = next
	response := s.commit(req.BackupType, req.SchemaVersion, req.CoreVersion)
	response.Manifest = nil
	w.WriteHeader(http.StatusCreated)
	writeJSON(s.t, w, response)
}

func (s *deltaTestServer) writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if code == "" {
		return
	}
	assert.NoError(s.t, json.NewEncoder(w).Encode(remoteAPIError{
		Error: remoteAPIErrorBody{Code: code, Message: code},
	}))
}

// counts returns how many full and delta commits the server has received.
func (s *deltaTestServer) counts() (full, delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fullCommits, len(s.deltas)
}

func (s *deltaTestServer) lastDelta() remoteDeltaRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(s.t, s.deltas)
	return s.deltas[len(s.deltas)-1]
}

func (s *deltaTestServer) currentRoot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

func (s *deltaTestServer) set(change func(*deltaTestServer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

func newDeltaTestEnv(t *testing.T) (backupTestEnv, *deltaTestServer) {
	t.Helper()
	env := newBackupTestEnv(t, platformids.Mister)
	server := newDeltaTestServer(t)
	configureRemoteTestAuth(t, env.Manager, server.server.URL)
	return env, server
}

func runRemoteDeltaTest(t *testing.T, env *backupTestEnv) RemoteRunInfo {
	t.Helper()
	info, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeScheduled)
	require.NoError(t, err)
	return info
}

func deltaTestSavePath(env *backupTestEnv) string {
	return filepath.Join(env.RootDir, "saves", "game.sav")
}

const deltaTestSaveRestorePath = "saves/game.sav"

func TestManagerRunRemoteFirstBackupIsFullAndStoresIndex(t *testing.T) {
	env, server := newDeltaTestEnv(t)

	info := runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, full)
	assert.Zero(t, delta)
	assert.False(t, info.NoChanges)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.Equal(t, server.currentRoot(), state.ManifestRootHash)
	assert.Equal(t, "device-1", state.DeviceID)
	assert.Equal(t, info.Backup.ID, state.Backup.ID)
	assert.Empty(t, state.Backup.Manifest, "the index is the stored file list, not a second manifest copy")
	assert.Equal(t, server.manifest, state.Files)
	lastFull, ok := parseReliableTime(state.LastFullCommitAt)
	require.True(t, ok)
	assert.WithinDuration(t, time.Now(), lastFull, time.Minute)
}

func TestManagerRunRemoteChangedFileSendsDeltaWithOnlyThatFile(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	server.set(func(s *deltaTestServer) { s.uploaded = nil })
	changed := "changed-save-data\n"
	writeTestFile(t, deltaTestSavePath(&env), changed)

	info := runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, full, "a changed file must not send the whole file list again")
	require.Equal(t, 1, delta)
	request := server.lastDelta()
	assert.Equal(t, RemoteBackupTypeScheduled, request.BackupType)
	assert.Equal(t, remoteSchemaVersion, request.SchemaVersion)
	assert.Equal(t, map[string][]remoteManifestEntry{
		CategorySaves: {{
			Path: deltaTestSaveRestorePath, SHA256: sha256Hex([]byte(changed)), Size: int64(len(changed)),
		}},
	}, request.Upsert)
	assert.Empty(t, request.Remove)
	assert.Equal(t, []string{sha256Hex([]byte(changed))}, server.uploaded)
	assert.Equal(t, 1, info.UploadedFiles)
	assert.Equal(t, 1, info.UploadedPacks)
	assert.False(t, info.NoChanges)
	assert.Equal(t, int64(99), info.StorageUsedBytes)

	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.Equal(t, server.currentRoot(), state.ManifestRootHash)
	assert.NotEqual(t, request.BaseManifestRootHash, state.ManifestRootHash)
	assert.Equal(t, server.manifest[CategorySaves], state.Files[CategorySaves])
	assert.Equal(t, info.Backup.ID, state.Backup.ID)

	// The updated index is the base of the next delta.
	writeTestFile(t, deltaTestSavePath(&env), "changed-again\n")
	runRemoteDeltaTest(t, &env)
	full, delta = server.counts()
	assert.Equal(t, 1, full)
	assert.Equal(t, 2, delta)
	assert.Equal(t, state.ManifestRootHash, server.lastDelta().BaseManifestRootHash)
}

func TestManagerRunRemoteDeletedFileSendsRemove(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	require.NoError(t, os.Remove(deltaTestSavePath(&env)))

	info := runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, full)
	require.Equal(t, 1, delta)
	request := server.lastDelta()
	assert.Empty(t, request.Upsert)
	assert.Equal(t, map[string][]string{CategorySaves: {deltaTestSaveRestorePath}}, request.Remove)
	assert.Zero(t, info.UploadedPacks)
	assert.NotContains(t, info.Categories, CategorySaves)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.NotContains(t, state.Files, CategorySaves)
}

func TestRemoteDeltaRequestAlwaysSendsBothChangeSets(t *testing.T) {
	t.Parallel()
	changes, ok := diffRemoteIndex(
		map[string][]remoteManifestEntry{CategorySaves: {{Path: "saves/a.sav", SHA256: "aa", Size: 1}}},
		nil,
	)
	require.True(t, ok)
	body, err := json.Marshal(&remoteDeltaRequest{
		BaseManifestRootHash: "root", BackupType: RemoteBackupTypeManual,
		Upsert: changes.upsert, Remove: changes.remove,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"base_manifest_root_hash": "root", "backup_type": "manual", "schema_version": 0,
		"upsert": {}, "remove": {"saves": ["saves/a.sav"]}
	}`, string(body))
}

func TestManagerRunRemoteBaseMismatchFallsBackToFullAndResetsIndex(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	// Another commit moved the device's latest snapshot on.
	server.set(func(s *deltaTestServer) { s.root = s.rootOf("someone-else") })
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	info := runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, delta)
	assert.Equal(t, 2, full, "a refused base is answered by a full commit")
	assert.NotEmpty(t, info.Backup.Manifest)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.Equal(t, server.currentRoot(), state.ManifestRootHash)
	assert.Equal(t, server.manifest, state.Files)

	// A refused base does not stop later deltas.
	writeTestFile(t, deltaTestSavePath(&env), "changed-again\n")
	runRemoteDeltaTest(t, &env)
	full, delta = server.counts()
	assert.Equal(t, 2, full)
	assert.Equal(t, 2, delta)
}

func TestManagerRunRemoteDeltaTooLargeFallsBackToFull(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	server.set(func(s *deltaTestServer) {
		s.deltaStatus, s.deltaCode = http.StatusBadRequest, remoteErrorDeltaTooLarge
	})
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, delta)
	assert.Equal(t, 2, full)
	assert.False(t, env.Manager.coordinator.RemoteDeltaUnsupported())
}

func TestManagerRunRemoteDeltaRefusalsForTheFilesKeepDeltaOn(t *testing.T) {
	cases := []struct {
		sentinel error
		code     string
		deltas   int
	}{
		// A changed file is re-read and the run retried once, as for a
		// full commit, so the delta is sent twice.
		{code: "integrity_mismatch", sentinel: errRemoteIntegrityRetry, deltas: 2},
		{code: "backup_too_large", sentinel: errRemoteBackupTooLarge, deltas: 1},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			env, server := newDeltaTestEnv(t)
			runRemoteDeltaTest(t, &env)
			server.set(func(s *deltaTestServer) {
				s.deltaStatus, s.deltaCode = http.StatusBadRequest, tc.code
			})
			writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

			_, err := env.Manager.RunRemote(context.Background(), RemoteBackupTypeScheduled)

			require.ErrorIs(t, err, tc.sentinel)
			full, delta := server.counts()
			assert.Equal(t, tc.deltas, delta)
			assert.Equal(t, 1, full, "a full commit of the same files would be refused too")
			assert.False(t, env.Manager.coordinator.RemoteDeltaUnsupported())

			server.set(func(s *deltaTestServer) { s.deltaStatus, s.deltaCode = 0, "" })
			runRemoteDeltaTest(t, &env)
			full, delta = server.counts()
			assert.Equal(t, tc.deltas+1, delta, "the next run sends a delta again")
			assert.Equal(t, 1, full)
		})
	}
}

func TestCommitRemoteDeltaLeavesAnEmptyRunToTheFullCommit(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	client, err := env.Manager.newRemoteClient()
	require.NoError(t, err)
	base := env.Manager.remoteDeltaBase(client, server.deviceID)
	require.NotNil(t, base)

	_, done, err := env.Manager.commitRemoteDelta(
		context.Background(), client, RemoteBackupTypeScheduled, nil, base, nil,
	)

	require.NoError(t, err)
	assert.False(t, done, "the server refuses a delta that leaves no files")
	_, delta := server.counts()
	assert.Zero(t, delta)
	assert.False(t, env.Manager.coordinator.RemoteDeltaUnsupported())
}

func TestManagerRunRemoteOtherBadRequestDisablesDelta(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	server.set(func(s *deltaTestServer) {
		s.deltaStatus, s.deltaCode = http.StatusBadRequest, "bad_request"
	})
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, delta)
	assert.Equal(t, 2, full)
	assert.True(t, env.Manager.coordinator.RemoteDeltaUnsupported())
}

func TestManagerRunRemoteUnsupportedDeltaIsDisabledForTheRun(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			env, server := newDeltaTestEnv(t)
			runRemoteDeltaTest(t, &env)
			server.set(func(s *deltaTestServer) { s.deltaStatus = status })

			writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")
			runRemoteDeltaTest(t, &env)
			full, delta := server.counts()
			assert.Equal(t, 1, delta)
			assert.Equal(t, 2, full)

			writeTestFile(t, deltaTestSavePath(&env), "changed-again\n")
			runRemoteDeltaTest(t, &env)
			full, delta = server.counts()
			assert.Equal(t, 1, delta, "the delta endpoint is not tried again in this process run")
			assert.Equal(t, 3, full)

			// A new coordinator stands in for the next process start.
			env.Manager.WithCoordinator(NewCoordinator())
			server.set(func(s *deltaTestServer) { s.deltaStatus = 0 })
			writeTestFile(t, deltaTestSavePath(&env), "changed-after-restart\n")
			runRemoteDeltaTest(t, &env)
			full, delta = server.counts()
			assert.Equal(t, 2, delta)
			assert.Equal(t, 3, full)
		})
	}
}

func TestManagerRunRemoteEmptyDiffSendsNoCommit(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	first := runRemoteDeltaTest(t, &env)
	before := env.Manager.readRemoteDeltaState()
	require.NotNil(t, before)
	server.set(func(s *deltaTestServer) { s.uploaded = nil })

	info := runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, full)
	assert.Zero(t, delta)
	assert.Empty(t, server.uploaded)
	assert.True(t, info.NoChanges)
	assert.Equal(t, first.Backup.ID, info.Backup.ID)
	assert.True(t, first.Backup.CreatedAt.Equal(info.Backup.CreatedAt))
	assert.Equal(t, first.Backup.SizeBytes, info.Backup.SizeBytes)
	assert.Equal(t, first.Categories, info.Categories)
	assert.Equal(t, int64(99), info.StorageUsedBytes)
	assert.Equal(t, before, env.Manager.readRemoteDeltaState())
	remote := env.Manager.Status().Remote
	assert.Equal(t, StatusSuccess, remote.LastStatus)
	assert.True(t, remote.LastRunNoChanges)
}

func TestManagerRunRemoteSendsFullCommitWhenDriftCheckIsDue(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	state.LastFullCommitAt = formatTime(time.Now().Add(-remoteFullCommitInterval - time.Minute))
	require.NoError(t, env.Manager.writeRemoteDeltaState(state))
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 2, full, "a full commit is sent once the last one is a day old")
	assert.Zero(t, delta)
	refreshed := env.Manager.readRemoteDeltaState()
	require.NotNil(t, refreshed)
	lastFull, ok := parseReliableTime(refreshed.LastFullCommitAt)
	require.True(t, ok)
	assert.WithinDuration(t, time.Now(), lastFull, time.Minute)

	// Deltas resume, and they do not push the next full commit back.
	writeTestFile(t, deltaTestSavePath(&env), "changed-again\n")
	runRemoteDeltaTest(t, &env)
	full, delta = server.counts()
	assert.Equal(t, 2, full)
	assert.Equal(t, 1, delta)
	afterDelta := env.Manager.readRemoteDeltaState()
	require.NotNil(t, afterDelta)
	assert.Equal(t, refreshed.LastFullCommitAt, afterDelta.LastFullCommitAt)
}

func TestRemoteFullCommitDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last string
		want bool
	}{
		{name: "missing", last: "", want: true},
		{name: "corrupt", last: "not-a-time", want: true},
		{name: "unreliable clock", last: formatTime(time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC)), want: true},
		{name: "in the future", last: formatTime(now.Add(time.Hour)), want: true},
		{name: "recent", last: formatTime(now.Add(-time.Hour)), want: false},
		{name: "just under a day", last: formatTime(now.Add(-remoteFullCommitInterval + time.Second)), want: false},
		{name: "a day old", last: formatTime(now.Add(-remoteFullCommitInterval)), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, remoteFullCommitDue(&remoteDeltaState{LastFullCommitAt: tt.last}, now))
		})
	}
}

func TestManagerRestoreClearsRemoteDeltaIndex(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	local, err := env.Manager.Create(context.Background())
	require.NoError(t, err)
	runRemoteDeltaTest(t, &env)
	require.NotNil(t, env.Manager.readRemoteDeltaState())

	_, err = env.Manager.Restore(context.Background(), local.Name)
	require.NoError(t, err)

	assert.Nil(t, env.Manager.readRemoteDeltaState())
	runRemoteDeltaTest(t, &env)
	full, delta := server.counts()
	assert.Equal(t, 2, full, "the first backup after a restore is a full commit")
	assert.Zero(t, delta)
	assert.NotNil(t, env.Manager.readRemoteDeltaState())
}

func TestManagerRemoteLinkChangeClearsDeltaIndex(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	require.NotNil(t, env.Manager.readRemoteDeltaState())

	env.Manager.MarkRemoteUnlinked()
	assert.Nil(t, env.Manager.readRemoteDeltaState())

	env.Manager.MarkRemoteLinked()
	runRemoteDeltaTest(t, &env)
	require.NotNil(t, env.Manager.readRemoteDeltaState())
	env.Manager.MarkRemoteLinked()
	assert.Nil(t, env.Manager.readRemoteDeltaState())

	// An index left behind by another link is never the base of a delta.
	runRemoteDeltaTest(t, &env)
	server.set(func(s *deltaTestServer) { s.deviceID = "device-2" })
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")
	runRemoteDeltaTest(t, &env)
	full, delta := server.counts()
	assert.Equal(t, 4, full)
	assert.Zero(t, delta)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.Equal(t, "device-2", state.DeviceID)
}

func TestManagerRunRemoteServerWithoutRootHashKeepsFullCommits(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	server.set(func(s *deltaTestServer) { s.omitRootHash = true })

	runRemoteDeltaTest(t, &env)
	assert.Nil(t, env.Manager.readRemoteDeltaState())
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")
	runRemoteDeltaTest(t, &env)
	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 3, full)
	assert.Zero(t, delta)
	assert.Nil(t, env.Manager.readRemoteDeltaState())
}

func TestManagerRunRemoteFullCommitWithoutRootHashDropsIndex(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	require.NotNil(t, env.Manager.readRemoteDeltaState())
	server.set(func(s *deltaTestServer) {
		s.omitRootHash = true
		s.deltaStatus = http.StatusNotFound
	})
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	assert.Nil(t, env.Manager.readRemoteDeltaState())
}

func TestManagerRunRemoteRetriesRateLimitedDeltaWithSameBody(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	env.Manager.rateLimitWaits = testRateLimitWaits()
	runRemoteDeltaTest(t, &env)
	server.set(func(s *deltaTestServer) { s.rateLimits = 1 })
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, full)
	require.Equal(t, 2, delta)
	assert.Equal(t, string(server.deltaBodies[0]), string(server.deltaBodies[1]))
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	assert.Equal(t, server.currentRoot(), state.ManifestRootHash)
}

func TestManagerRunRemoteRejectedDeltaResponseFallsBackToFull(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	// The server's snapshot is missing a file this device's index holds, so
	// the committed snapshot does not total what this run collected.
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")
	server.set(func(s *deltaTestServer) { delete(s.manifest, CategorySavestates) })

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 1, delta)
	assert.Equal(t, 2, full)
	assert.True(t, env.Manager.coordinator.RemoteDeltaUnsupported())
	repaired := env.Manager.readRemoteDeltaState()
	require.NotNil(t, repaired)
	assert.Equal(t, server.manifest, repaired.Files)
	assert.NotEmpty(t, repaired.Files[CategorySavestates])
}

func TestManagerRunRemoteCorruptIndexSendsFullCommit(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	writeTestFile(t, env.Manager.remoteDeltaStatePath(), "{not json")
	writeTestFile(t, deltaTestSavePath(&env), "changed-save-data\n")

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 2, full)
	assert.Zero(t, delta)
	require.NotNil(t, env.Manager.readRemoteDeltaState())
}

func TestDiffRemoteIndex(t *testing.T) {
	t.Parallel()
	indexed := map[string][]remoteManifestEntry{
		CategorySaves: {
			{Path: "saves/kept.sav", SHA256: "aa", Size: 1},
			{Path: "saves/changed.sav", SHA256: "bb", Size: 2},
			{Path: "saves/gone.sav", SHA256: "cc", Size: 3},
		},
		CategorySettings: {{Path: "gone.ini", SHA256: "dd", Size: 4}},
	}
	files := []FileRef{
		{Category: CategorySaves, RestorePath: "saves/kept.sav", SHA256: "aa", Size: 1},
		{Category: CategorySaves, RestorePath: "saves/changed.sav", SHA256: "ee", Size: 2},
		{Category: CategorySaves, RestorePath: "saves/new.sav", SHA256: "ff", Size: 5},
		// The same path in another category is a different file.
		{Category: CategoryInputs, RestorePath: "saves/kept.sav", SHA256: "aa", Size: 1},
	}

	changes, ok := diffRemoteIndex(indexed, files)

	require.True(t, ok)
	assert.Equal(t, 5, changes.count)
	assert.Equal(t, map[string][]remoteManifestEntry{
		CategorySaves: {
			{Path: "saves/changed.sav", SHA256: "ee", Size: 2},
			{Path: "saves/new.sav", SHA256: "ff", Size: 5},
		},
		CategoryInputs: {{Path: "saves/kept.sav", SHA256: "aa", Size: 1}},
	}, changes.upsert)
	assert.Equal(t, map[string][]string{
		CategorySaves:    {"saves/gone.sav"},
		CategorySettings: {"gone.ini"},
	}, changes.remove)
	for category, paths := range changes.remove {
		for _, removed := range paths {
			for _, entry := range changes.upsert[category] {
				assert.NotEqual(t, removed, entry.Path, "a path is never both upserted and removed")
			}
		}
	}

	_, ok = diffRemoteIndex(map[string][]remoteManifestEntry{
		CategorySaves: {{Path: "saves/a.sav"}, {Path: "saves/a.sav"}},
	}, nil)
	assert.False(t, ok, "an index naming a path twice is not trusted")
	_, ok = diffRemoteIndex(nil, []FileRef{
		{Category: CategorySaves, RestorePath: "saves/a.sav"},
		{Category: CategorySaves, RestorePath: "saves/a.sav"},
	})
	assert.False(t, ok)
}

func TestManagerRunRemoteTooManyChangesSendsFullCommit(t *testing.T) {
	env, server := newDeltaTestEnv(t)
	runRemoteDeltaTest(t, &env)
	state := env.Manager.readRemoteDeltaState()
	require.NotNil(t, state)
	for i := range remoteDeltaMaxChanges + 1 {
		state.Files[CategorySaves] = append(state.Files[CategorySaves], remoteManifestEntry{
			Path: "saves/stale-" + strconv.Itoa(i) + ".sav", SHA256: remoteEmptyContentSHA256,
		})
	}
	require.NoError(t, env.Manager.writeRemoteDeltaState(state))

	runRemoteDeltaTest(t, &env)

	full, delta := server.counts()
	assert.Equal(t, 2, full)
	assert.Zero(t, delta)
}
