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

package tui

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var docsScrape = models.MediaScrapeParams{ScraperID: "mister-docs", Systems: []string{}}

func timedOutCall() error {
	return fmt.Errorf("api call failed: %w", client.ErrRequestTimeout)
}

func TestStartMediaScrape_Admitted(t *testing.T) {
	t.Parallel()
	api := mocks.NewMockAPIClient()
	api.On("Call", mock.Anything, models.MethodMediaScrape, mock.Anything).Return("", nil).Once()

	require.NoError(t, startMediaScrape(api, docsScrape, time.Second))
	api.AssertNotCalled(t, "Call", mock.Anything, models.MethodMediaScrapeStatus, "")
}

// Issue #1584: Core starts an admitted scrape even after the TUI stops waiting,
// so a timed out start with that scrape running is not an error.
func TestStartMediaScrape_TimedOutButRunningIsStarted(t *testing.T) {
	t.Parallel()
	api := mocks.NewMockAPIClient()
	api.On("Call", mock.Anything, models.MethodMediaScrape, mock.Anything).Return("", timedOutCall()).Once()
	api.On("Call", mock.Anything, models.MethodMediaScrapeStatus, "").
		Return(`{"scraperId":"mister-docs","scraping":true,"state":"running"}`, nil).Once()

	require.NoError(t, startMediaScrape(api, docsScrape, time.Second))
	api.AssertExpectations(t)
}

func TestStartMediaScrape_TimedOutAndNotRunningFails(t *testing.T) {
	t.Parallel()
	for name, status := range map[string]string{
		"idle":          `{"scraping":false,"state":"idle"}`,
		"other scraper": `{"scraperId":"mister-arcade","scraping":true,"state":"running"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := mocks.NewMockAPIClient()
			api.On("Call", mock.Anything, models.MethodMediaScrape, mock.Anything).Return("", timedOutCall()).Once()
			api.On("Call", mock.Anything, models.MethodMediaScrapeStatus, "").Return(status, nil).Once()

			err := startMediaScrape(api, docsScrape, time.Second)
			require.ErrorIs(t, err, client.ErrRequestTimeout)
		})
	}
}

func TestStartMediaScrape_StatusCheckFailureKeepsTheTimeout(t *testing.T) {
	t.Parallel()
	api := mocks.NewMockAPIClient()
	api.On("Call", mock.Anything, models.MethodMediaScrape, mock.Anything).Return("", timedOutCall()).Once()
	api.On("Call", mock.Anything, models.MethodMediaScrapeStatus, "").Return("", timedOutCall()).Once()

	require.ErrorIs(t, startMediaScrape(api, docsScrape, time.Second), client.ErrRequestTimeout)
}

// A refusal, such as another scrape already running, is reported as is.
func TestStartMediaScrape_RefusalIsNotCheckedAgainstStatus(t *testing.T) {
	t.Parallel()
	api := mocks.NewMockAPIClient()
	refused := errors.New("scraping already in progress")
	api.On("Call", mock.Anything, models.MethodMediaScrape, mock.Anything).Return("", refused).Once()

	err := startMediaScrape(api, docsScrape, time.Second)
	require.ErrorIs(t, err, refused)
	api.AssertNotCalled(t, "Call", mock.Anything, models.MethodMediaScrapeStatus, "")
}

// The start waits longer than an ordinary TUI request, since admission writes
// to the media database.
func TestMediaJobStartTimeoutOutlastsTUIRequests(t *testing.T) {
	t.Parallel()
	assert.Greater(t, mediaJobStartTimeout, TUIRequestTimeout)
}
