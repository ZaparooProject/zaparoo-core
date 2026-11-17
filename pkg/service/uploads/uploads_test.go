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

package uploads

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPurpose = "test"

func newTestManager(t *testing.T, maxBytes int64) (*Manager, afero.Fs, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	dir := filepath.Join("data", "staging")
	m := NewManager(fs)
	m.Register(Purpose{Name: testPurpose, MaxBytes: maxBytes, StagingDir: dir})
	return m, fs, dir
}

func sumHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stagedFiles(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestUploadInChunksThenTake(t *testing.T) {
	t.Parallel()
	m, fs, dir := newTestManager(t, 1<<20)
	data := []byte("the quick brown fox jumps over the lazy dog")

	info, err := m.Begin(testPurpose, "client", int64(len(data)), sumHex(data))
	require.NoError(t, err)
	assert.Equal(t, ChunkBytes, info.ChunkSize)

	for offset := 0; offset < len(data); offset += 10 {
		end := min(offset+10, len(data))
		info, err = m.Chunk(info.ID, "client", int64(offset), data[offset:end])
		require.NoError(t, err)
		assert.Equal(t, int64(end), info.Received)
	}
	info, err = m.Finish(info.ID, "client")
	require.NoError(t, err)
	assert.True(t, info.Finished)

	var taken []byte
	require.NoError(t, m.Take(info.ID, "client", testPurpose, func(path string) error {
		var readErr error
		taken, readErr = afero.ReadFile(fs, path)
		if readErr != nil {
			return fmt.Errorf("read staged upload: %w", readErr)
		}
		return nil
	}))
	assert.Equal(t, data, taken)
	assert.Empty(t, stagedFiles(t, fs, dir), "a taken upload leaves nothing staged")
	_, err = m.Status(info.ID, "client")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUploadResumesFromStatus(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, 1<<20)
	data := []byte("0123456789abcdef")
	info, err := m.Begin(testPurpose, "client", int64(len(data)), sumHex(data))
	require.NoError(t, err)
	_, err = m.Chunk(info.ID, "client", 0, data[:6])
	require.NoError(t, err)

	// The client lost its connection and its place.
	status, err := m.Status(info.ID, "client")
	require.NoError(t, err)
	assert.Equal(t, int64(6), status.Received)
	_, err = m.Chunk(info.ID, "client", status.Received, data[status.Received:])
	require.NoError(t, err)
	_, err = m.Finish(info.ID, "client")
	require.NoError(t, err)
}

func TestUploadRejectsBadChunks(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, 1<<20)
	data := make([]byte, 100)
	info, err := m.Begin(testPurpose, "client", int64(len(data)), sumHex(data))
	require.NoError(t, err)

	got, err := m.Chunk(info.ID, "client", 10, data[:10])
	require.ErrorIs(t, err, ErrWrongOffset)
	assert.Equal(t, int64(0), got.Received, "a wrong offset reports where the upload is")
	_, err = m.Chunk(info.ID, "client", 0, nil)
	require.ErrorIs(t, err, ErrEmptyChunk)
	_, err = m.Chunk(info.ID, "client", 0, make([]byte, ChunkBytes+1))
	require.ErrorIs(t, err, ErrChunkTooLarge)
	_, err = m.Chunk(info.ID, "client", 0, make([]byte, 101))
	require.ErrorIs(t, err, ErrOverrun)
	_, err = m.Chunk(info.ID, "someone else", 0, data[:10])
	require.ErrorIs(t, err, ErrNotFound, "a session belongs to the client that began it")

	_, err = m.Finish(info.ID, "client")
	require.ErrorIs(t, err, ErrIncomplete)
}

func TestUploadBeginLimits(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, 50)
	hash := sumHex(nil)

	_, err := m.Begin("other", "client", 10, hash)
	require.ErrorIs(t, err, ErrUnknownPurpose)
	_, err = m.Begin(testPurpose, "client", 51, hash)
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = m.Begin(testPurpose, "client", 0, hash)
	require.ErrorIs(t, err, ErrInvalidSize)
	_, err = m.Begin(testPurpose, "client", 10, "abc")
	require.ErrorIs(t, err, ErrInvalidHash)

	for range MaxSessions {
		_, err = m.Begin(testPurpose, "client", 10, hash)
		require.NoError(t, err)
	}
	_, err = m.Begin(testPurpose, "client", 10, hash)
	require.ErrorIs(t, err, ErrTooManySessions)
}

func TestUploadWrongHashIsDiscarded(t *testing.T) {
	t.Parallel()
	m, fs, dir := newTestManager(t, 1<<20)
	data := []byte("some bytes")
	info, err := m.Begin(testPurpose, "client", int64(len(data)), sumHex([]byte("other bytes")))
	require.NoError(t, err)
	_, err = m.Chunk(info.ID, "client", 0, data)
	require.NoError(t, err)

	_, err = m.Finish(info.ID, "client")
	require.ErrorIs(t, err, ErrHashMismatch)
	assert.Empty(t, stagedFiles(t, fs, dir))
}

func TestUploadPurposeValidatesContent(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	errNotWanted := errors.New("not wanted")
	m := NewManager(fs)
	m.Register(Purpose{
		Name: testPurpose, MaxBytes: 100, StagingDir: "staging",
		Validate: func(afero.Fs, string) error { return errNotWanted },
	})
	data := []byte("x")
	info, err := m.Begin(testPurpose, "client", 1, sumHex(data))
	require.NoError(t, err)
	_, err = m.Chunk(info.ID, "client", 0, data)
	require.NoError(t, err)

	_, err = m.Finish(info.ID, "client")
	require.ErrorIs(t, err, errNotWanted)
	assert.Empty(t, stagedFiles(t, fs, "staging"))
}

func TestUploadIdleSessionExpires(t *testing.T) {
	t.Parallel()
	m, fs, dir := newTestManager(t, 1<<20)
	now := time.Unix(1_700_000_000, 0)
	m.now = func() time.Time { return now }
	info, err := m.Begin(testPurpose, "client", 10, sumHex(nil))
	require.NoError(t, err)

	now = now.Add(IdleTimeout - time.Second)
	_, err = m.Status(info.ID, "client")
	require.NoError(t, err, "a request keeps the session alive")
	now = now.Add(IdleTimeout)
	_, err = m.Status(info.ID, "client")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, stagedFiles(t, fs, dir))
}

func TestTakeNeedsAFinishedUploadOfThePurpose(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, 1<<20)
	data := []byte("abc")
	info, err := m.Begin(testPurpose, "client", 3, sumHex(data))
	require.NoError(t, err)
	use := func(string) error { return nil }

	require.ErrorIs(t, m.Take(info.ID, "client", testPurpose, use), ErrNotFinished)
	_, err = m.Chunk(info.ID, "client", 0, data)
	require.NoError(t, err)
	_, err = m.Finish(info.ID, "client")
	require.NoError(t, err)
	require.ErrorIs(t, m.Take(info.ID, "client", "other", use), ErrWrongPurpose)

	// A use that fails leaves the upload to be taken again.
	errUse := errors.New("could not use it")
	require.ErrorIs(t, m.Take(info.ID, "client", testPurpose, func(string) error { return errUse }), errUse)
	require.NoError(t, m.Take(info.ID, "client", testPurpose, use))
}

func TestRegisterRemovesLeftoverStagingFiles(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	dir := "staging"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, stagingPrefix+"dead"+stagingSuffix), []byte("x"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "img-0011.png"), []byte("x"), 0o600))

	NewManager(fs).Register(Purpose{Name: testPurpose, MaxBytes: 10, StagingDir: dir})

	assert.Equal(t, []string{"img-0011.png"}, stagedFiles(t, fs, dir), "only staging files are removed")
}

// FuzzChunk feeds a session chunks of any size at any offset. Whatever
// arrives, the session never holds more than its declared size, and what it
// holds is exactly the accepted chunks in order.
func FuzzChunk(f *testing.F) {
	f.Add(int64(0), []byte("abc"), int64(3), []byte("def"))
	f.Add(int64(5), []byte{}, int64(-1), []byte("x"))
	f.Add(int64(0), make([]byte, 64), int64(0), make([]byte, 64))
	f.Fuzz(func(t *testing.T, firstOffset int64, first []byte, secondOffset int64, second []byte) {
		const size = 48
		fs := afero.NewMemMapFs()
		m := NewManager(fs)
		m.Register(Purpose{Name: testPurpose, MaxBytes: size, StagingDir: "staging"})
		info, err := m.Begin(testPurpose, "client", size, sumHex(nil))
		require.NoError(t, err)

		var accepted []byte
		for _, chunk := range []struct {
			data   []byte
			offset int64
		}{{first, firstOffset}, {second, secondOffset}} {
			got, chunkErr := m.Chunk(info.ID, "client", chunk.offset, chunk.data)
			if chunkErr == nil {
				accepted = append(accepted, chunk.data...)
				require.Equal(t, int64(len(accepted)), got.Received)
			}
		}
		require.LessOrEqual(t, len(accepted), size)
		status, err := m.Status(info.ID, "client")
		require.NoError(t, err)
		require.Equal(t, int64(len(accepted)), status.Received)
		staged, err := afero.ReadFile(fs, filepath.Join("staging", stagingPrefix+info.ID+stagingSuffix))
		require.NoError(t, err)
		require.Len(t, staged, len(accepted))
		require.Equal(t, string(accepted), string(staged))
	})
}
