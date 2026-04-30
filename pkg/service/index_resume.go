/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// checkAndResumeIndexing checks if media indexing was interrupted and automatically resumes it
func checkAndResumeIndexing(
	pl platforms.Platform,
	cfg *config.Instance,
	db *database.Database,
	st *state.State,
	pauser *syncutil.Pauser,
) {
	// Check if indexing was interrupted
	indexingStatus, err := db.MediaDB.GetIndexingStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get indexing status during startup check")
		return
	}

	// Only resume if indexing was interrupted (running or pending states)
	if indexingStatus != mediadb.IndexingStatusRunning && indexingStatus != mediadb.IndexingStatusPending {
		log.Debug().Msgf("indexing status is '%s', no auto-resume needed", indexingStatus)
		return
	}

	log.Info().Msg("detected interrupted media indexing, automatically resuming")

	// Get the systems that were being indexed from the database
	// If not available, fall back to all systems
	var systems []systemdefs.System
	storedSystemIDs, err := db.MediaDB.GetIndexingSystems()
	if err != nil || len(storedSystemIDs) == 0 {
		log.Debug().Msgf("no stored systems found (err=%v, len=%d), defaulting to all systems",
			err, len(storedSystemIDs))
		systems = systemdefs.AllSystems()
	} else {
		// Convert system IDs to System objects
		systems = make([]systemdefs.System, 0, len(storedSystemIDs))
		for _, systemID := range storedSystemIDs {
			if system, exists := systemdefs.Systems[systemID]; exists {
				systems = append(systems, system)
			} else {
				log.Warn().Msgf("stored system ID '%s' not found in system definitions, skipping", systemID)
			}
		}
		// If we couldn't resolve any systems, fall back to all systems
		if len(systems) == 0 {
			log.Warn().Msg("could not resolve any stored systems, falling back to all systems")
			systems = systemdefs.AllSystems()
		}
	}

	// Resume using the proper function with full notification support
	// GenerateMediaDB spawns its own goroutine and returns immediately
	err = methods.GenerateMediaDB(st.GetContext(), pl, cfg, st.Notifications, systems, db, pauser)
	if err != nil {
		log.Error().Err(err).Msg("failed to start auto-resume of media indexing")
	}
}

// checkAndResumeOptimization checks if optimization was interrupted and automatically resumes it
func checkAndResumeOptimization(db *database.Database, ns chan<- models.Notification, pauser *syncutil.Pauser) {
	status, err := db.MediaDB.GetOptimizationStatus()
	if err != nil {
		log.Debug().Err(err).Msg("failed to get optimization status during startup check")
		return
	}

	// Resume if optimization was interrupted or failed
	if status == mediadb.IndexingStatusPending ||
		status == mediadb.IndexingStatusRunning ||
		status == mediadb.IndexingStatusFailed {
		log.Info().Msgf("detected incomplete optimization (status: %s), automatically resuming", status)
		db.MediaDB.RunBackgroundOptimization(func(optimizing bool) {
			notifications.MediaIndexing(ns, models.IndexingStatusResponse{
				Exists:     true,
				Indexing:   false,
				Optimizing: optimizing,
			})
		}, pauser)
	} else {
		log.Debug().Msgf("optimization status is '%s', no auto-resume needed", status)
	}
}
