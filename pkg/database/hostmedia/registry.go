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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
)

const maxRegistryBytes = 8 * 1024 * 1024

// Registry retains source identity after a grant disappears. The index write
// coordinator serializes updates. This new app-private registry is separate
// from MediaSources, config.toml, and all existing database schemas.
type Registry struct {
	fs    afero.Fs
	clock clockwork.Clock
	path  string
}

type sourceRecord struct {
	LastSuccessfulScan time.Time `json:"lastSuccessfulScan,omitempty"`
	ID                 string    `json:"id"`
	Reference          string    `json:"reference"`
	Provider           string    `json:"provider"`
	Name               string    `json:"name"`
	Granted            bool      `json:"granted"`
	ReadOnly           bool      `json:"readOnly"`
}

type registryData struct {
	Systems []string       `json:"systems,omitempty"`
	Sources []sourceRecord `json:"sources"`
	Version int            `json:"version"`
}

func NewRegistry(fs afero.Fs, path string) *Registry {
	return &Registry{fs: fs, path: path, clock: clockwork.NewRealClock()}
}

func (r *Registry) load() (registryData, error) {
	data := registryData{Version: 1}
	file, err := r.fs.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("open host-source registry: %w", err)
	}
	defer func() { _ = file.Close() }()
	bytes, err := io.ReadAll(io.LimitReader(file, maxRegistryBytes+1))
	if err != nil {
		return data, fmt.Errorf("read host-source registry: %w", err)
	}
	if len(bytes) > maxRegistryBytes {
		return data, ErrLimit
	}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return data, fmt.Errorf("decode host-source registry: %w", err)
	}
	if data.Version != 1 || len(data.Sources) > 256 {
		return data, ErrInvalid
	}
	seen := make(map[string]bool, len(data.Sources))
	for _, source := range data.Sources {
		if source.Reference == "" || len(source.Reference) > 16384 ||
			source.ID != sourcepath.ID(source.Reference) || seen[source.ID] ||
			!sourcepath.ValidName(source.Name) || len(source.Provider) > 1024 || source.Provider == "" {
			return data, ErrInvalid
		}
		seen[source.ID] = true
	}
	if len(data.Systems) > len(systemdefs.AllSystems()) {
		return data, ErrInvalid
	}
	for _, id := range data.Systems {
		if _, err := systemdefs.GetSystem(id); err != nil {
			return data, ErrInvalid
		}
	}
	return data, nil
}

func (r *Registry) save(data registryData) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode host-source registry: %w", err)
	}
	if len(bytes) > maxRegistryBytes {
		return ErrLimit
	}
	parent := filepath.Dir(r.path)
	if mkdirErr := r.fs.MkdirAll(parent, 0o700); mkdirErr != nil {
		return fmt.Errorf("create registry directory: %w", mkdirErr)
	}
	file, err := afero.TempFile(r.fs, parent, ".host-sources-")
	if err != nil {
		return fmt.Errorf("create registry snapshot: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = file.Close(); _ = r.fs.Remove(temporary) }()
	if _, err := file.Write(bytes); err != nil {
		return fmt.Errorf("write registry snapshot: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync registry snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close registry snapshot: %w", err)
	}
	if err := r.fs.Rename(temporary, r.path); err != nil {
		return fmt.Errorf("replace registry snapshot: %w", err)
	}
	return nil
}

func (r *Registry) register(sources []Source) error {
	data, err := r.load()
	if err != nil {
		return err
	}
	current := make(map[string]Source, len(sources))
	for _, source := range sources {
		current[sourcepath.ID(source.Reference)] = source
	}
	missing := false
	for i := range data.Sources {
		record := &data.Sources[i]
		source, ok := current[record.ID]
		record.Granted = ok
		if !ok {
			missing = true
			continue
		}
		record.Name, record.Provider, record.ReadOnly = source.Root.Name, source.Provider, true
		delete(current, record.ID)
	}
	for id, source := range current {
		data.Sources = append(data.Sources, sourceRecord{
			ID: id, Reference: source.Reference, Provider: source.Provider,
			Name: source.Root.Name, Granted: true, ReadOnly: true,
		})
	}
	if len(data.Sources) > 256 {
		return ErrLimit
	}
	slices.SortFunc(data.Sources, func(a, b sourceRecord) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	if err := r.save(data); err != nil {
		return err
	}
	if missing {
		return ErrMissingGrant
	}
	return nil
}

func (r *Registry) knownSystems() ([]string, error) {
	data, err := r.load()
	return data.Systems, err
}

func (r *Registry) succeeded(sources []Source, systems []string) error {
	data, err := r.load()
	if err != nil {
		return err
	}
	data.Systems = slices.Clone(systems)
	current := make(map[string]bool, len(sources))
	for _, source := range sources {
		current[sourcepath.ID(source.Reference)] = true
	}
	for i := range data.Sources {
		if current[data.Sources[i].ID] {
			data.Sources[i].LastSuccessfulScan = r.clock.Now().UTC()
		}
	}
	return r.save(data)
}
