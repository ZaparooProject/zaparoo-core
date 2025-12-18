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

// Package inbox provides a service for managing persistent system notifications.
//
// The InboxService combines database storage with real-time notifications,
// ensuring that when an inbox entry is added, connected API clients are
// immediately notified with the full entry details.
package inbox

import (
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog/log"
)

// Service manages inbox entries with automatic notification broadcasting.
type Service struct {
	db            database.UserDBI
	notifications chan<- models.Notification
}

// NewService creates a new inbox service.
func NewService(db database.UserDBI, ns chan<- models.Notification) *Service {
	return &Service{
		db:            db,
		notifications: ns,
	}
}

// Add creates a new inbox entry and notifies connected clients with the full entry.
func (s *Service) Add(title, body string) error {
	if title == "" {
		return errors.New("inbox entry title cannot be empty")
	}

	entry := &database.InboxEntry{
		Title:     title,
		Body:      body,
		CreatedAt: time.Now(),
	}

	inserted, err := s.db.AddInboxEntry(entry)
	if err != nil {
		log.Error().Err(err).Str("title", title).Msg("failed to add inbox entry")
		return fmt.Errorf("failed to add inbox entry: %w", err)
	}

	log.Info().
		Int64("id", inserted.DBID).
		Str("title", inserted.Title).
		Msg("inbox entry added")

	notifications.InboxAdded(s.notifications, models.InboxEntry{
		ID:        inserted.DBID,
		Title:     inserted.Title,
		Body:      inserted.Body,
		CreatedAt: inserted.CreatedAt,
	})

	return nil
}
