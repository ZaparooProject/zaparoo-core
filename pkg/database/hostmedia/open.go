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
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
)

// Document identifies a resolved leaf within its granted source. Both fields
// remain opaque host references, not filesystem paths or durable media identity.
type Document struct {
	Reference string
	ID        string
}

// Resolve walks a canonical identity through current source metadata. Hosts can
// use the result for a fresh access preflight without guessing provider IDs.
func Resolve(ctx context.Context, backend Backend, sources []Source, identity string) (Document, error) {
	id, parts, err := sourcepath.Parse(identity)
	if err != nil {
		return Document{}, fmt.Errorf("parse source identity: %w", err)
	}
	for _, source := range sources {
		if sourcepath.ID(source.Reference) != id {
			continue
		}
		parent := source.Root.ID
		for index, name := range parts {
			var match *Entry
			err := eachDirectory(ctx, backend, source.Reference, parent, func(entry Entry) error {
				if entry.Name == name {
					candidate := entry
					match = &candidate
				}
				return nil
			})
			if err != nil {
				return Document{}, err
			}
			if match == nil || match.Directory != (index < len(parts)-1) {
				return Document{}, ErrUnavailable
			}
			parent = match.ID
		}
		return Document{Reference: source.Reference, ID: parent}, nil
	}
	return Document{}, ErrMissingGrant
}

// Open returns a caller-owned handle, including after the backend's metadata
// session closes. Resolution never produces a filesystem path.
func Open(ctx context.Context, backend Backend, sources []Source, identity string) (ReadSeekCloser, error) {
	document, err := Resolve(ctx, backend, sources, identity)
	if err != nil {
		return nil, err
	}
	file, err := backend.Open(ctx, document.Reference, document.ID)
	if err != nil {
		return nil, fmt.Errorf("open source document: %w", err)
	}
	if ctx.Err() != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open source document cancelled: %w", ctx.Err())
	}
	return file, nil
}
