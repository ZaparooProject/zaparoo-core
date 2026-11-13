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

	"github.com/rs/zerolog/log"
)

// sessionWriter is what the dispatcher and the response helpers need from a
// client connection: a way to write one complete message and a way to close
// it. *melody.Session satisfies it unchanged; other transports provide their
// own implementation.
type sessionWriter interface {
	Write([]byte) error
	Close() error
}

// closeSession best-effort closes a client session, logging any error at
// debug level (the connection may already be closed).
func closeSession(session sessionWriter) {
	if err := session.Close(); err != nil {
		log.Debug().Err(err).Msg("failed to close session")
	}
}

// writableWaiter is implemented by transports far slower than the handlers
// that feed them. The dispatcher waits on it before writing a response, so a
// client that asks for more than its link can carry is slowed down instead
// of overflowing the transport's queue and losing the connection.
type writableWaiter interface {
	// WaitWritable blocks until the transport has room for another
	// response, ctx ends, or the session closes.
	WaitWritable(ctx context.Context) error
}
