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

// Package uploads receives a file from a client in chunks. A session carries
// the bytes; a purpose, registered by the feature that wants the file, owns
// the rules: how large it may be, where it is staged and what it must hold.
// The feature then takes the finished file by its upload ID.
package uploads

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// ChunkBytes is the largest chunk a session accepts, and the size a
	// client is told to send.
	ChunkBytes = 128 << 10
	// MaxSessions caps the uploads in flight at once.
	MaxSessions = 4
	// IdleTimeout ends a session no chunk or request has touched.
	IdleTimeout = 10 * time.Minute

	stagingPrefix = ".upload-"
	stagingSuffix = ".part"
)

var (
	ErrUnknownPurpose  = errors.New("unknown upload purpose")
	ErrTooLarge        = errors.New("upload is larger than its purpose allows")
	ErrInvalidSize     = errors.New("upload size must be greater than zero")
	ErrInvalidHash     = errors.New("upload sha256 must be 64 hex characters")
	ErrTooManySessions = errors.New("too many uploads in progress")
	ErrNotFound        = errors.New("upload not found")
	ErrWrongOffset     = errors.New("chunk offset is not the next byte of the upload")
	ErrChunkTooLarge   = errors.New("chunk is larger than the upload's chunk size")
	ErrEmptyChunk      = errors.New("chunk is empty")
	ErrOverrun         = errors.New("chunk runs past the upload's declared size")
	ErrIncomplete      = errors.New("upload is not complete")
	ErrHashMismatch    = errors.New("upload does not match its sha256")
	ErrFinished        = errors.New("upload is already finished")
	ErrNotFinished     = errors.New("upload is not finished")
	ErrWrongPurpose    = errors.New("upload was made for another purpose")
)

// Purpose is one kind of upload and its rules.
type Purpose struct {
	// Validate checks a complete staged file before the upload may finish.
	// Nil accepts any content.
	Validate func(fs afero.Fs, path string) error
	Name     string
	// StagingDir is where chunks are written. It should be on the volume the
	// finished file will live on, so taking it is a rename.
	StagingDir string
	MaxBytes   int64
	// AdminOnly limits the purpose to local and admin clients. The API
	// layer enforces it.
	AdminOnly bool
}

// Info describes a session to its client.
type Info struct {
	ID        string
	Purpose   string
	Size      int64
	Received  int64
	ChunkSize int
	Finished  bool
}

type session struct {
	touched  time.Time
	hasher   hash.Hash
	id       string
	owner    string
	path     string
	sha256   string
	purpose  Purpose
	size     int64
	received int64
	finished bool
}

func (s *session) info() Info {
	return Info{
		ID: s.id, Purpose: s.purpose.Name, Size: s.size, Received: s.received,
		ChunkSize: ChunkBytes, Finished: s.finished,
	}
}

// Manager holds the upload sessions of one Core process. Sessions live in
// memory: a restart ends them, and CleanStaging removes what they left.
type Manager struct {
	fs       afero.Fs
	now      func() time.Time
	sessions map[string]*session
	purposes map[string]Purpose
	mu       syncutil.Mutex
}

// NewManager returns a manager with no purposes registered.
func NewManager(fs afero.Fs) *Manager {
	return &Manager{
		fs: fs, now: time.Now,
		sessions: make(map[string]*session), purposes: make(map[string]Purpose),
	}
}

// Register adds a purpose and removes staging files a previous run left in
// its folder.
func (m *Manager) Register(purpose Purpose) {
	m.mu.Lock()
	m.purposes[purpose.Name] = purpose
	m.mu.Unlock()
	m.cleanStaging(purpose.StagingDir)
}

// Purpose returns a registered purpose.
func (m *Manager) Purpose(name string) (Purpose, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	purpose, ok := m.purposes[name]
	return purpose, ok
}

// cleanStaging removes every staging file in dir that no session owns.
func (m *Manager) cleanStaging(dir string) {
	entries, err := afero.ReadDir(m.fs, dir)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	owned := make(map[string]struct{}, len(m.sessions))
	for _, s := range m.sessions {
		owned[s.path] = struct{}{}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, stagingPrefix) || !strings.HasSuffix(name, stagingSuffix) {
			continue
		}
		path := filepath.Join(dir, name)
		if _, live := owned[path]; live {
			continue
		}
		if err := m.fs.Remove(path); err != nil {
			log.Debug().Err(err).Str("file", name).Msg("failed to remove a leftover upload")
		}
	}
}

// Begin opens a session for size bytes whose SHA-256 is sha256Hex. owner
// names the client; only it may continue the session.
func (m *Manager) Begin(purposeName, owner string, size int64, sha256Hex string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()

	purpose, ok := m.purposes[purposeName]
	if !ok {
		return Info{}, fmt.Errorf("%w: %q", ErrUnknownPurpose, purposeName)
	}
	if size <= 0 {
		return Info{}, ErrInvalidSize
	}
	if size > purpose.MaxBytes {
		return Info{}, ErrTooLarge
	}
	sha256Hex = strings.ToLower(sha256Hex)
	if decoded, err := hex.DecodeString(sha256Hex); err != nil || len(decoded) != sha256.Size {
		return Info{}, ErrInvalidHash
	}
	if len(m.sessions) >= MaxSessions {
		return Info{}, ErrTooManySessions
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Info{}, fmt.Errorf("create upload id: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := m.fs.MkdirAll(purpose.StagingDir, 0o750); err != nil {
		return Info{}, fmt.Errorf("create upload staging folder: %w", err)
	}
	path := filepath.Join(purpose.StagingDir, stagingPrefix+id+stagingSuffix)
	file, err := m.fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("create upload staging file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = m.fs.Remove(path)
		return Info{}, fmt.Errorf("close upload staging file: %w", err)
	}
	s := &session{
		id: id, owner: owner, purpose: purpose, path: path, size: size, sha256: sha256Hex,
		hasher: sha256.New(), touched: m.now(),
	}
	m.sessions[id] = s
	return s.info(), nil
}

// Chunk appends data at offset, which must be the next byte the session
// expects: chunks arrive in order, and a client that lost its place asks
// Status.
func (m *Manager) Chunk(id, owner string, offset int64, data []byte) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(id, owner)
	if err != nil {
		return Info{}, err
	}
	switch {
	case s.finished:
		return Info{}, ErrFinished
	case len(data) == 0:
		return Info{}, ErrEmptyChunk
	case len(data) > ChunkBytes:
		return Info{}, ErrChunkTooLarge
	case offset != s.received:
		return s.info(), ErrWrongOffset
	case int64(len(data)) > s.size-s.received:
		return Info{}, ErrOverrun
	}

	file, err := m.fs.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("open upload staging file: %w", err)
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || written != len(data) {
		// The file may hold part of the chunk, and the hash none of it.
		m.dropLocked(s)
		return Info{}, fmt.Errorf("write upload chunk: %w", errors.Join(writeErr, closeErr))
	}
	_, _ = s.hasher.Write(data)
	s.received += int64(len(data))
	return s.info(), nil
}

// Status reports a session, so a client that lost its connection can resume
// from Received.
func (m *Manager) Status(id, owner string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(id, owner)
	if err != nil {
		return Info{}, err
	}
	return s.info(), nil
}

// Finish checks a fully received upload against its size, its SHA-256 and its
// purpose's content rule. An upload that fails a check is discarded.
func (m *Manager) Finish(id, owner string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(id, owner)
	if err != nil {
		return Info{}, err
	}
	if s.finished {
		return s.info(), nil
	}
	if s.received != s.size {
		return s.info(), ErrIncomplete
	}
	if hex.EncodeToString(s.hasher.Sum(nil)) != s.sha256 {
		m.dropLocked(s)
		return Info{}, ErrHashMismatch
	}
	if s.purpose.Validate != nil {
		if err := s.purpose.Validate(m.fs, s.path); err != nil {
			m.dropLocked(s)
			return Info{}, err
		}
	}
	s.finished = true
	return s.info(), nil
}

// Cancel discards a session and its staged bytes.
func (m *Manager) Cancel(id, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(id, owner)
	if err != nil {
		return err
	}
	m.dropLocked(s)
	return nil
}

// Take hands a finished upload of the given purpose to use, which gets the
// staged file's path and may read or move it. The session ends when use
// returns without an error, and the staged file is removed if it is still
// there; an error leaves the upload to be taken again.
func (m *Manager) Take(id, owner, purposeName string, use func(path string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.sessionLocked(id, owner)
	if err != nil {
		return err
	}
	if s.purpose.Name != purposeName {
		return ErrWrongPurpose
	}
	if !s.finished {
		return ErrNotFinished
	}
	if err := use(s.path); err != nil {
		return err
	}
	m.dropLocked(s)
	return nil
}

// sessionLocked returns a live session of owner. A session that belongs to
// another client is not found.
func (m *Manager) sessionLocked(id, owner string) (*session, error) {
	m.expireLocked()
	s, ok := m.sessions[id]
	if !ok || s.owner != owner {
		return nil, ErrNotFound
	}
	s.touched = m.now()
	return s, nil
}

func (m *Manager) expireLocked() {
	now := m.now()
	for _, s := range m.sessions {
		if now.Sub(s.touched) >= IdleTimeout {
			m.dropLocked(s)
		}
	}
}

func (m *Manager) dropLocked(s *session) {
	delete(m.sessions, s.id)
	if err := m.fs.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Debug().Err(err).Str("upload", s.id).Msg("failed to remove an upload's staged file")
	}
}
