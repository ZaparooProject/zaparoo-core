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

package service

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// zapScriptLibrary lends the API's media library to the commands a script
// runs. The handlers run with no client identity, so a locked device holds
// them to its kiosk scope the way it holds a member client.
func zapScriptLibrary(svc *ServiceContext) platforms.MediaLibrary {
	return &methods.ZapScriptLibrary{Env: func(ctx context.Context) requests.RequestEnv {
		return requests.RequestEnv{
			Context:         ctx,
			Platform:        svc.Platform,
			Config:          svc.Config,
			State:           svc.State,
			Database:        svc.DB,
			Profiles:        svc.Profiles,
			LimitsManager:   svc.LimitsManager,
			Kiosk:           svc.State.Kiosk(),
			LauncherCache:   helpers.GlobalLauncherCache,
			PlaybackManager: svc.PlaybackManager,
			UI:              svc.UI,
		}
	}}
}
