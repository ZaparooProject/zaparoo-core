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

// Package inbox provides a service for managing persistent system notifications.
//
// The InboxService combines database storage with real-time notifications,
// ensuring that when an inbox message is added, connected API clients are
// immediately notified with the full message details.
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

// Severity levels for inbox messages
const (
	SeverityInfo    = 0
	SeverityWarning = 1
	SeverityError   = 2
)

// Category constants for deduplication
const (
	CategoryNone = "" // No deduplication
)

// MessageOptions configures optional fields for inbox messages.
type MessageOptions struct {
	Body      string
	Category  string
	Severity  int
	ProfileID int64
}

// MessageOption is a functional option for configuring inbox messages.
type MessageOption func(*MessageOptions)

// WithBody sets the message body.
func WithBody(body string) MessageOption {
	return func(o *MessageOptions) {
		o.Body = body
	}
}

// WithSeverity sets the message severity level.
func WithSeverity(severity int) MessageOption {
	return func(o *MessageOptions) {
		o.Severity = severity
	}
}

// WithCategory sets the category for deduplication.
// Messages with the same category and profileID will be updated instead of duplicated.
func WithCategory(category string) MessageOption {
	return func(o *MessageOptions) {
		o.Category = category
	}
}

// WithProfileID sets the profile ID for profile-specific messages.
// Use 0 for global messages visible to all profiles.
func WithProfileID(profileID int64) MessageOption {
	return func(o *MessageOptions) {
		o.ProfileID = profileID
	}
}

// Service manages inbox messages with automatic notification broadcasting.
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

// Add creates a new inbox message or updates an existing one (if category is set).
// Connected clients are notified with the full message details.
func (s *Service) Add(title string, opts ...MessageOption) error {
	if title == "" {
		return errors.New("inbox message title cannot be empty")
	}

	options := &MessageOptions{}
	for _, opt := range opts {
		opt(options)
	}

	msg := &database.InboxMessage{
		Title:     title,
		Body:      options.Body,
		Severity:  options.Severity,
		Category:  options.Category,
		ProfileID: options.ProfileID,
		CreatedAt: time.Now(),
	}

	inserted, err := s.db.AddInboxMessage(msg)
	if err != nil {
		log.Error().Err(err).Str("title", title).Msg("failed to add inbox message")
		return fmt.Errorf("failed to add inbox message: %w", err)
	}

	log.Info().
		Int64("id", inserted.DBID).
		Str("title", inserted.Title).
		Msg("inbox message added")

	notifications.InboxAdded(s.notifications, &models.InboxMessage{
		ID:        inserted.DBID,
		Title:     inserted.Title,
		Body:      inserted.Body,
		Severity:  inserted.Severity,
		CreatedAt: inserted.CreatedAt,
	})

	return nil
}
