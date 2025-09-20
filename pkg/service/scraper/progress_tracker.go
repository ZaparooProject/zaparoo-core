// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package scraper

import (
	"encoding/json"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	scraperpkg "github.com/ZaparooProject/zaparoo-core/v2/pkg/scraper"
)

// ProgressTracker manages scraper progress tracking and notifications
type ProgressTracker struct {
	progress      *scraperpkg.ScraperProgress
	progressMu    sync.RWMutex
	notifications chan<- models.Notification
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(notificationsChan chan<- models.Notification) *ProgressTracker {
	return &ProgressTracker{
		progress:      &scraperpkg.ScraperProgress{Status: "idle"},
		notifications: notificationsChan,
	}
}

// Update updates the progress with the given function
func (pt *ProgressTracker) Update(updateFunc func(*scraperpkg.ScraperProgress)) {
	pt.progressMu.Lock()
	defer pt.progressMu.Unlock()

	updateFunc(pt.progress)

	// Send notification about progress update
	if pt.notifications != nil {
		// Create a copy for notification and marshal to JSON
		progressCopy := *pt.progress
		paramsBytes, err := json.Marshal(progressCopy)
		if err == nil {
			notification := models.Notification{
				Method: "scraper.progress",
				Params: paramsBytes,
			}
			select {
			case pt.notifications <- notification:
			default:
				// Don't block if notifications channel is full
			}
		}
	}
}

// Get returns a copy of the current progress
func (pt *ProgressTracker) Get() *scraperpkg.ScraperProgress {
	pt.progressMu.RLock()
	defer pt.progressMu.RUnlock()

	// Return a copy to avoid race conditions
	progressCopy := *pt.progress
	return &progressCopy
}

// SetStatus sets the scraper status
func (pt *ProgressTracker) SetStatus(status string) {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.Status = status
	})
}

// SetCurrentGame sets the current game being scraped
func (pt *ProgressTracker) SetCurrentGame(gameName string, mediaID int64) {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.CurrentGame = gameName
	})
}

// SetProgress sets the overall progress
func (pt *ProgressTracker) SetProgress(current, total int) {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.ProcessedGames = current
		p.TotalGames = total
	})
}

// IncrementProgress increments the processed games count by 1
func (pt *ProgressTracker) IncrementProgress() {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.ProcessedGames++
	})
}

// SetError sets an error message
func (pt *ProgressTracker) SetError(err error) {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		if err != nil {
			p.LastError = err.Error()
			p.ErrorCount++
		} else {
			p.LastError = ""
		}
	})
}

// Reset resets the progress to initial state
func (pt *ProgressTracker) Reset() {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.Status = "idle"
		p.ProcessedGames = 0
		p.TotalGames = 0
		p.CurrentGame = ""
		p.LastError = ""
		p.ErrorCount = 0
		p.IsRunning = false
	})
}

// Complete marks the scraping as completed
func (pt *ProgressTracker) Complete() {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.Status = "completed"
		p.ProcessedGames = p.TotalGames
		p.CurrentGame = ""
		p.LastError = ""
		p.IsRunning = false
	})

	// Send completion notification
	if pt.notifications != nil {
		notification := models.Notification{
			Method: "scraper.complete",
		}
		select {
		case pt.notifications <- notification:
		default:
			// Don't block if notifications channel is full
		}
	}
}

// Cancel marks the scraping as cancelled
func (pt *ProgressTracker) Cancel() {
	pt.Update(func(p *scraperpkg.ScraperProgress) {
		p.Status = "cancelled"
		p.LastError = ""
		p.IsRunning = false
	})
}