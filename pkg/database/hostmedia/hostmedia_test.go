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

package hostmedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type memoryBackend struct {
	failure error
	dirs    map[string][]Entry
	file    *ownedFixture
	sources []Source
	opened  int
	closed  int
	retired int
}
type memoryDirectory struct {
	owner   *memoryBackend
	entries []Entry
	done    bool
}

func (b *memoryBackend) Sources(context.Context) ([]Source, error) { return b.sources, b.failure }
func (b *memoryBackend) Children(_ context.Context, _, id string) (Directory, error) {
	if b.failure != nil {
		return nil, b.failure
	}
	b.opened++
	return &memoryDirectory{owner: b, entries: b.dirs[id]}, nil
}

func (b *memoryBackend) Open(context.Context, string, string) (ReadSeekCloser, error) {
	b.file = &ownedFixture{Reader: bytes.NewReader([]byte("owned fixture"))}
	return b.file, nil
}
func (b *memoryBackend) Close() error { b.retired++; return nil }
func (d *memoryDirectory) Next(context.Context) ([]Entry, error) {
	if d.done || len(d.entries) == 0 {
		return nil, io.EOF
	}
	d.done = true
	return d.entries, nil
}
func (d *memoryDirectory) Close() error { d.owner.closed++; return nil }

type ownedFixture struct {
	*bytes.Reader
	closed bool
}

func (f *ownedFixture) Close() error { f.closed = true; return nil }

func fixtureBackend() *memoryBackend {
	return &memoryBackend{
		sources: []Source{{
			Reference: "opaque-tree", Provider: "fixture", Root: Entry{ID: "root", Name: "roms", Directory: true},
		}},
		dirs: map[string][]Entry{
			"root": {
				{ID: "nes", Name: "nes", Directory: true}, {ID: "other", Name: "unknown-system", Directory: true},
			},
			"nes": {
				{ID: "game", Name: "Fixture + 50% (USA).NES"},
				{ID: "image", Name: "cover.png"},
				{ID: "hidden", Name: ".hidden.nes"},
				{ID: "nested", Name: "nested", Directory: true},
			},
			"nested": {{ID: "second", Name: "Second.nes"}},
		},
	}
}

func testRegistry() *Registry {
	r := NewRegistry(afero.NewMemMapFs(), filepath.Join("data", "host-sources.json"))
	r.clock = clockwork.NewFakeClock()
	return r
}

func TestResolveReturnsOpaqueLeafWithoutOpeningFile(t *testing.T) {
	t.Parallel()
	backend := fixtureBackend()
	identity, err := sourcepath.Format(sourcepath.ID("opaque-tree"), []string{"nes", "nested", "Second.nes"})
	require.NoError(t, err)
	document, err := Resolve(t.Context(), backend, backend.sources, identity)
	require.NoError(t, err)
	require.Equal(t, Document{Reference: "opaque-tree", ID: "second"}, document)
	require.Nil(t, backend.file)
	require.Equal(t, backend.opened, backend.closed)

	_, err = Resolve(t.Context(), backend, nil, identity)
	require.ErrorIs(t, err, ErrMissingGrant)
	_, err = Resolve(t.Context(), backend, backend.sources, "/storage/roms/nes/Second.nes")
	require.Error(t, err)
	backend.dirs["nested"] = nil
	_, err = Resolve(t.Context(), backend, backend.sources, identity)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, backend.opened, backend.closed)
}

func TestWalkClassifiesAndKeepsOpaqueIdentity(t *testing.T) {
	t.Parallel()
	backend, registry := fixtureBackend(), testRegistry()
	scan, err := NewScan(t.Context(), backend, registry)
	require.NoError(t, err)
	require.Equal(t, []string{"NES"}, scan.Systems())
	var results []platforms.ScanResult
	require.NoError(t, scan.Walk(t.Context(), "NES", nil, func(row platforms.ScanResult) error {
		results = append(results, row)
		return nil
	}))
	require.Len(t, results, 2)
	id, parts, err := sourcepath.Parse(results[0].Path)
	require.NoError(t, err)
	require.Equal(t, sourcepath.ID("opaque-tree"), id)
	require.Equal(t, []string{"nes", "Fixture + 50% (USA).NES"}, parts)
	file, err := Open(t.Context(), backend, backend.sources, results[0].Path)
	require.NoError(t, err)
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "owned fixture", string(data))
	require.NoError(t, scan.Close(true))
	require.False(t, backend.file.closed, "descriptor ownership belongs to caller, not scan")
	require.NoError(t, file.Close())
	require.True(t, backend.file.closed)
	require.Equal(t, backend.opened, backend.closed)
	require.Equal(t, 1, backend.retired)
	records, err := registry.load()
	require.NoError(t, err)
	require.Equal(t, registry.clock.Now().UTC(), records.Sources[0].LastSuccessfulScan)
	require.NoError(t, scan.Close(true))
	require.Equal(t, 1, backend.retired)
}

func TestMissingGrantPreservesRegistryAndRegrantIdentity(t *testing.T) {
	t.Parallel()
	registry := testRegistry()
	first, err := NewScan(t.Context(), fixtureBackend(), registry)
	require.NoError(t, err)
	require.NoError(t, first.Close(false))
	_, err = NewScan(t.Context(), &memoryBackend{}, registry)
	require.ErrorIs(t, err, ErrMissingGrant)
	data, err := registry.load()
	require.NoError(t, err)
	require.Len(t, data.Sources, 1)
	require.False(t, data.Sources[0].Granted)
	restored, err := NewScan(t.Context(), fixtureBackend(), registry)
	require.NoError(t, err)
	require.NoError(t, restored.Close(false))
	updated, err := registry.load()
	require.NoError(t, err)
	require.Equal(t, data.Sources[0].ID, updated.Sources[0].ID)
	require.True(t, updated.Sources[0].Granted)
}

func TestWalkClosesResourcesOnFailures(t *testing.T) {
	t.Parallel()
	modes := []string{"cancel", "consumer", "cycle", "duplicate", "traversal", "batch", "provider", "ignore"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			backend := fixtureBackend()
			registry := testRegistry()
			scan, err := NewScan(t.Context(), backend, registry)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := errors.New("consumer stopped")
			switch mode {
			case "cancel":
				cancel()
			case "cycle":
				backend.dirs["nested"] = []Entry{{ID: "nes", Name: "loop", Directory: true}}
			case "duplicate":
				backend.dirs["nes"] = append(backend.dirs["nes"], backend.dirs["nes"][0])
			case "traversal":
				backend.dirs["nes"] = []Entry{{ID: "bad", Name: "../bad.nes"}}
			case "batch":
				backend.dirs["nes"] = make([]Entry, MaxBatch+1)
			case "provider":
				backend.failure = ErrUnavailable
			case "ignore":
				backend.dirs["nes"] = append(backend.dirs["nes"], Entry{ID: "marker", Name: ".zaparooignore"})
			}
			count := 0
			err = scan.Walk(ctx, "NES", nil, func(platforms.ScanResult) error {
				count++
				if mode == "consumer" {
					return want
				}
				return nil
			})
			if mode == "ignore" {
				require.NoError(t, err)
				require.Zero(t, count)
			} else {
				require.Error(t, err)
			}
			if mode == "consumer" {
				require.ErrorIs(t, err, want)
			}
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.NoError(t, scan.Close(false))
			require.Equal(t, backend.opened, backend.closed)
			require.Equal(t, 1, backend.retired)
			data, err := registry.load()
			require.NoError(t, err)
			require.True(t, data.Sources[0].LastSuccessfulScan.IsZero())
		})
	}
}

func TestRemovedSystemFolderAndRootIgnoreRemainAuthoritative(t *testing.T) {
	t.Parallel()
	for _, ignored := range []bool{false, true} {
		registry := testRegistry()
		first, err := NewScan(t.Context(), fixtureBackend(), registry)
		require.NoError(t, err)
		require.NoError(t, first.Walk(t.Context(), "NES", nil, func(platforms.ScanResult) error { return nil }))
		require.NoError(t, first.Close(true))
		backend := fixtureBackend()
		if ignored {
			backend.dirs["root"] = append(backend.dirs["root"], Entry{ID: "ignore", Name: ".zaparooignore"})
		} else {
			backend.dirs["root"] = nil
		}
		empty, err := NewScan(t.Context(), backend, registry)
		require.NoError(t, err)
		require.Equal(t, []string{"NES"}, empty.Systems())
		count := 0
		require.NoError(t, empty.Walk(t.Context(), "NES", nil, func(platforms.ScanResult) error {
			count++
			return nil
		}))
		require.Zero(t, count)
		require.NoError(t, empty.Close(true))
		known, err := registry.knownSystems()
		require.NoError(t, err)
		require.Empty(t, known)
	}
}

func TestWholeScanBudgetAndPauseCheckpoint(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"entries", "bytes", "pause"} {
		backend := fixtureBackend()
		scan, err := NewScan(t.Context(), backend, testRegistry())
		require.NoError(t, err)
		var checkpoint func() error
		want := ErrLimit
		switch mode {
		case "entries":
			scan.entries = MaxEntries
		case "bytes":
			scan.metadataBytes = maxMetadataBytes
		case "pause":
			want = errors.New("pause checkpoint")
			checkpoint = func() error { return want }
		}
		err = scan.Walk(t.Context(), "NES", checkpoint, func(platforms.ScanResult) error { return nil })
		require.ErrorIs(t, err, want)
		require.NoError(t, scan.Close(false))
		require.Equal(t, backend.opened, backend.closed)
	}
}
