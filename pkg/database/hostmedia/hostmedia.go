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

// Package hostmedia adapts granted document sources to Core's streaming index hook.
package hostmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
)

const (
	MaxSources       = 64
	MaxBatch         = 128
	MaxEntries       = 200000
	maxMetadataBytes = 32 * 1024 * 1024
	maxDepth         = 64
)

var (
	ErrUnavailable  = errors.New("game folder unavailable; check its permission or storage and scan again")
	ErrMissingGrant = errors.New("game folder permission removed; choose the folder again before scanning")
	ErrInvalid      = errors.New("invalid or ambiguous document metadata")
	ErrLimit        = errors.New("game folder exceeds bounded scan limits")
	ErrNoSystems    = errors.New(
		"no recognized system folders; choose a folder containing folders such as nes, snes or gba")
)

type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}

type Source struct {
	Reference string `json:"reference"`
	Provider  string `json:"provider"`
	Root      Entry  `json:"root"`
}

// Directory returns bounded batches followed by io.EOF. Close is mandatory,
// including cancellation, malformed metadata and yield failures.
type Directory interface {
	Next(context.Context) ([]Entry, error)
	Close() error
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Backend is owned by one scan. Open transfers an owned read-only handle to its
// caller. References and document IDs are opaque, never filesystem paths.
type Backend interface {
	Sources(context.Context) ([]Source, error)
	Children(context.Context, string, string) (Directory, error)
	Open(context.Context, string, string) (ReadSeekCloser, error)
	Close() error
}

type folder struct {
	parts      []string
	extensions []string
	source     Source
	entry      Entry
}

type Scan struct {
	backend        Backend
	registry       *Registry
	folders        map[string][]folder
	walked         map[string]bool
	sources        []Source
	currentSystems []string
	entries        int
	metadataBytes  int
	closed         bool
}

var _ platforms.HostMediaScan = (*Scan)(nil)

// NewScan takes backend ownership even when discovery fails.
func NewScan(ctx context.Context, backend Backend, registry *Registry) (scan *Scan, err error) {
	defer func() {
		if err != nil {
			err = errors.Join(err, backend.Close())
		}
	}()
	sources, err := backend.Sources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list host sources: %w", err)
	}
	if len(sources) > MaxSources {
		return nil, ErrLimit
	}
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if source.Reference == "" || len(source.Reference) > 16384 ||
			source.Provider == "" || len(source.Provider) > 1024 ||
			!validEntry(source.Root) || !source.Root.Directory || seen[source.Reference] {
			return nil, ErrInvalid
		}
		seen[source.Reference] = true
	}
	if err := registry.register(sources); err != nil {
		return nil, err
	}
	scan = &Scan{
		backend: backend, registry: registry, sources: sources,
		folders: make(map[string][]folder), walked: make(map[string]bool),
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("discover host sources: %w", err)
		}
		scan.metadataBytes += len(source.Reference) + len(source.Provider)
		if err := scan.account(source.Root); err != nil {
			return nil, err
		}
		if system, extensions := classify(source.Root.Name); system != "" {
			scan.folders[system] = append(scan.folders[system], folder{
				source: source, entry: source.Root, extensions: extensions,
			})
			continue
		}
		discovered := make(map[string][]folder)
		ignored := false
		err := eachDirectory(ctx, backend, source.Reference, source.Root.ID, func(entry Entry) error {
			if err := scan.account(entry); err != nil {
				return err
			}
			if entry.Name == ".zaparooignore" {
				ignored = true
			}
			if !entry.Directory || hidden(entry.Name) {
				return nil
			}
			if system, extensions := classify(entry.Name); system != "" {
				discovered[system] = append(discovered[system], folder{
					source: source, entry: entry, parts: []string{entry.Name}, extensions: extensions,
				})
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("discover system folders: %w", err)
		}
		if !ignored {
			for system, folders := range discovered {
				scan.folders[system] = append(scan.folders[system], folders...)
			}
		}
	}
	scan.currentSystems = scan.Systems()
	known, knownErr := registry.knownSystems()
	if knownErr != nil {
		return nil, knownErr
	}
	for _, system := range known {
		if _, present := scan.folders[system]; !present {
			scan.folders[system] = nil
		}
	}
	if len(sources) > 0 && len(scan.folders) == 0 {
		return nil, ErrNoSystems
	}
	return scan, nil
}

// account bounds the whole scan, not merely each provider directory.
func (s *Scan) account(entry Entry) error {
	s.entries++
	s.metadataBytes += len(entry.ID) + len(entry.Name) + 256
	if s.entries > MaxEntries || s.metadataBytes > maxMetadataBytes {
		return ErrLimit
	}
	return nil
}

func (s *Scan) Systems() []string {
	result := make([]string, 0, len(s.folders))
	for system := range s.folders {
		result = append(result, system)
	}
	slices.Sort(result)
	return result
}

func (s *Scan) Walk(
	ctx context.Context, system string, checkpoint func() error, yield func(platforms.ScanResult) error,
) error {
	if s.closed {
		return ErrUnavailable
	}
	if checkpoint == nil {
		checkpoint = ctx.Err
	}
	count := 0
	visited := make(map[string]bool)
	for i := range s.folders[system] {
		pending := []folder{s.folders[system][i]}
		for len(pending) > 0 {
			if err := checkpoint(); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("walk host source: %w", err)
			}
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			key := sourcepath.ID(current.source.Reference) + ":" + current.entry.ID
			if visited[key] || len(current.parts) >= maxDepth {
				return ErrInvalid
			}
			visited[key] = true
			var files []Entry
			var children []folder
			ignored := false
			// A directory is bounded independently. Its ignore marker must be seen
			// before any media is yielded, regardless of provider result ordering.
			err := eachDirectory(ctx, s.backend, current.source.Reference, current.entry.ID, func(entry Entry) error {
				count++
				if count%MaxBatch == 0 {
					if err := checkpoint(); err != nil {
						return err
					}
				}
				if err := s.account(entry); err != nil {
					return err
				}
				if entry.Name == ".zaparooignore" {
					ignored = true
				}
				if hidden(entry.Name) {
					return nil
				}
				if entry.Directory {
					parts := append(slices.Clone(current.parts), entry.Name)
					children = append(children, folder{
						source: current.source, entry: entry, parts: parts, extensions: current.extensions,
					})
				} else if slices.Contains(current.extensions, strings.ToLower(path.Ext(entry.Name))) {
					files = append(files, entry)
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("enumerate game folder: %w", err)
			}
			if ignored {
				continue
			}
			pending = append(pending, children...)
			for _, entry := range files {
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("yield host media: %w", err)
				}
				parts := append(slices.Clone(current.parts), entry.Name)
				identity, err := sourcepath.Format(sourcepath.ID(current.source.Reference), parts)
				if err != nil {
					return fmt.Errorf("format source identity: %w", err)
				}
				if err := yield(platforms.ScanResult{Path: identity}); err != nil {
					return err
				}
			}
		}
	}
	if _, present := s.folders[system]; present {
		s.walked[system] = true
	}
	return nil
}

func (s *Scan) Close(successful bool) error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.backend.Close()
	if successful && err == nil && len(s.walked) == len(s.folders) {
		err = s.registry.succeeded(s.sources, s.currentSystems)
	}
	return err
}

func validEntry(entry Entry) bool {
	return entry.ID != "" && len(entry.ID) <= 8192 && sourcepath.ValidName(entry.Name) && entry.Size >= -1
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") || name == "__MACOSX" }

func eachDirectory(ctx context.Context, backend Backend, reference, id string, yield func(Entry) error) (err error) {
	directory, err := backend.Children(ctx, reference, id)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	seen := make(map[string]bool)
	count, budget := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read directory: %w", err)
		}
		batch, err := directory.Next(ctx)
		if errors.Is(err, io.EOF) && len(batch) == 0 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read directory batch: %w", err)
		}
		if len(batch) == 0 || len(batch) > MaxBatch {
			return ErrInvalid
		}
		for _, entry := range batch {
			if !validEntry(entry) || seen[entry.Name] {
				return ErrInvalid
			}
			count++
			budget += len(entry.ID) + len(entry.Name)
			if count > MaxEntries || budget > maxMetadataBytes {
				return ErrLimit
			}
			seen[entry.Name] = true
			if err := yield(entry); err != nil {
				return err
			}
		}
	}
}

// Use Core's existing ES folder/extension vocabulary; there is no Android
// launcher or filename-extension guessing. Aliases resolve through systemdefs.
func classify(name string) (systemID string, extensions []string) {
	if info, ok := esde.SystemMap[strings.ToLower(name)]; ok {
		return info.SystemID, slices.Clone(info.Extensions)
	}
	system, err := systemdefs.LookupSystem(name)
	if err != nil {
		return "", nil
	}
	for _, info := range esde.SystemMap {
		if info.SystemID == system.ID {
			extensions = append(extensions, info.Extensions...)
		}
	}
	if len(extensions) == 0 {
		return "", nil
	}
	slices.Sort(extensions)
	return system.ID, slices.Compact(extensions)
}
