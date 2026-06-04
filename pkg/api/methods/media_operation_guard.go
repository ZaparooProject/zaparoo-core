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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

var mediaOperationMu syncutil.Mutex

func startIndexingIfNoScrape() error {
	mediaOperationMu.Lock()
	defer mediaOperationMu.Unlock()

	if scrapingStatusInstance.isRunning() {
		return models.ClientErrf("scraping is in progress")
	}
	if !statusInstance.startIfNotRunning() {
		return models.ClientErrf("indexing already in progress")
	}
	return nil
}

func startScrapingIfNoIndex(scraperID string, force bool) error {
	mediaOperationMu.Lock()
	defer mediaOperationMu.Unlock()

	if statusInstance.isRunning() {
		return models.ClientErrf("media indexing is in progress")
	}
	if !scrapingStatusInstance.startIfNotRunning(scraperID, force) {
		return models.ClientErrf("scraping already in progress")
	}
	return nil
}
