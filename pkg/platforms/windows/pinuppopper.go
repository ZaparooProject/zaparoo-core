//go:build windows

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
package windows

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/pinup"
	"github.com/spf13/afero"
)

// popperIntegration returns the PinUP Popper integration, creating it on
// first use. Launchers() can run before StartPost has supplied the
// ActiveMedia hooks, so the integration reads them through closures at call
// time rather than capturing them when it is built.
func (p *Platform) popperIntegration() *pinup.Integration {
	p.popperMu.Lock()
	defer p.popperMu.Unlock()
	if p.popper == nil {
		p.popper = pinup.NewIntegration(&pinup.Deps{
			ActiveMedia: func() *models.ActiveMedia {
				if p.activeMedia == nil {
					return nil
				}
				return p.activeMedia()
			},
			SetActiveMedia: func(media *models.ActiveMedia) {
				if p.setActiveMedia != nil {
					p.setActiveMedia(media)
				}
			},
			Frontend: pinup.NewFrontend(&command.RealExecutor{}),
			Locator: pinup.Locator{
				FS:         afero.NewOsFs(),
				Registry:   pinup.LocateFromRegistry,
				Candidates: pinup.DefaultInstallDirs(),
			},
		})
	}
	return p.popper
}
