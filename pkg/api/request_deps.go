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

package api

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
)

// requestDeps is the set of service dependencies every transport hands to
// method handlers through requests.RequestEnv. Transports differ only in how
// they identify the client, so that part is passed per request.
type requestDeps struct {
	platform        platforms.Platform
	cfg             *config.Instance
	st              *state.State
	inTokenQueue    chan<- tokens.Token
	confirmQueue    chan<- chan error
	db              *database.Database
	limitsManager   *playtime.LimitsManager
	profilesSvc     *profiles.Service
	player          audio.Player
	playbackManager audio.PlaybackManager
	indexPauser     *syncutil.Pauser
	scrapePauser    *syncutil.Pauser
	backupPauser    *syncutil.Pauser
}

func newRequestDeps(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	confirmQueue chan<- chan error,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	profilesSvc *profiles.Service,
	player audio.Player,
	playbackManager audio.PlaybackManager,
	indexPauser *syncutil.Pauser,
	scrapePauser *syncutil.Pauser,
	backupPauser *syncutil.Pauser,
) *requestDeps {
	return &requestDeps{
		platform:        platform,
		cfg:             cfg,
		st:              st,
		inTokenQueue:    inTokenQueue,
		confirmQueue:    confirmQueue,
		db:              db,
		limitsManager:   limitsManager,
		profilesSvc:     profilesSvc,
		player:          player,
		playbackManager: playbackManager,
		indexPauser:     indexPauser,
		scrapePauser:    scrapePauser,
		backupPauser:    backupPauser,
	}
}

// newRequestEnv builds the RequestEnv for one request. inputSession is nil
// for transports that cannot hold input across requests. Callers set the
// authentication fields (ClientRole, APIKeyAuthenticated) afterwards because
// their source differs per transport.
func (d *requestDeps) newRequestEnv(
	ctx context.Context,
	inputSession platforms.InputSession,
	clientID string,
	platformID string,
	isLocal bool,
) requests.RequestEnv {
	return requests.RequestEnv{
		Context:         ctx,
		Platform:        d.platform,
		Config:          d.cfg,
		State:           d.st,
		Database:        d.db,
		LimitsManager:   d.limitsManager,
		Profiles:        d.profilesSvc,
		LauncherCache:   helpers.GlobalLauncherCache,
		Player:          d.player,
		PlaybackManager: d.playbackManager,
		UI:              d.st.UIEvents(),
		TokenQueue:      d.inTokenQueue,
		ConfirmQueue:    d.confirmQueue,
		IndexPauser:     d.indexPauser,
		ScrapePauser:    d.scrapePauser,
		BackupPauser:    d.backupPauser,
		InputSession:    inputSession,
		PlatformID:      platformID,
		IsLocal:         isLocal,
		ClientID:        clientID,
	}
}
