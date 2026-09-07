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

package methods

import (
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
)

func HandleMediaLookupCandidates(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler signature
	var params models.MediaLookupCandidatesParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if !database.ValidTitleCandidateName(params.Name) {
		return nil, models.ClientErrf("invalid name: expected 1–256 Unicode characters")
	}
	system, err := resolveSystem(strings.TrimSpace(params.System), params.FuzzySystem != nil && *params.FuzzySystem)
	if err != nil {
		return nil, models.ClientErrf("invalid system: %w", err)
	}
	if slugs.Slugify(system.GetMediaType(), params.Name) == "" {
		return nil, models.ClientErrf("invalid name: normalizes to empty")
	}
	limit := database.TitleCandidateLimit
	if params.MaxResults != nil {
		limit = *params.MaxResults
	}
	if ctxErr := env.Context.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-env.Context.Done():
		return nil, env.Context.Err()
	}
	candidates, err := env.Database.MediaDB.TitleCandidates(env.Context, system.ID, params.Name, limit)
	if err != nil {
		return nil, fmt.Errorf("title candidates: %w", err)
	}
	if candidates == nil {
		candidates = []database.TitleCandidate{}
	}
	return models.MediaLookupCandidatesResponse{Candidates: candidates}, nil
}
