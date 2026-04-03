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

package publishers

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
)

// Publisher is implemented by any notification publisher backend.
type Publisher interface {
	// Start initializes the publisher. It receives a context for lifecycle
	// management. Configuration validation errors should be returned
	// immediately. Transient connection errors should be handled internally.
	Start(ctx context.Context) error

	// Publish sends a notification to the backend. Implementations decide
	// whether to forward raw JSON, transform the notification, or ignore it.
	// Must be safe for concurrent use.
	Publish(notif models.Notification) error

	// Stop performs graceful shutdown.
	Stop()
}
