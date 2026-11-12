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

package ccid

import (
	"context"
	"errors"
)

var (
	// errReaderGone means the reader is no longer there, as opposed to a
	// command failing on a reader that is.
	errReaderGone = errors.New("reader is no longer available")
	// errNoCard is a command sent with no tag to receive it.
	errNoCard = errors.New("no card in the reader")
)

// cardState is what a reader reports about its slot.
type cardState struct {
	// ATR is the card's answer to reset. It can be set without Present when
	// the device is not going to talk to the card.
	ATR []byte
	// Present is a card the driver can exchange commands with.
	Present bool
}

// device is one open reader.
type device interface {
	transmitter
	// Poll reports the slot's state, activating a newly arrived card.
	Poll(ctx context.Context) (cardState, error)
	// Reactivate makes the next Poll activate the card again.
	Reactivate(ctx context.Context)
	Close() error
}

// readerInfo describes a reader a backend can reach.
type readerInfo struct {
	// Details is everything known about the reader, for the first-sight log.
	Details map[string]string
	// Path identifies the reader in config and to the rest of Core. Through
	// PC/SC it is the reader name; on direct USB it is the port position.
	Path string
	// Name is what the reader calls itself.
	Name string
	// Unusable is why a reader that was found cannot be used through this
	// backend, or empty when it can.
	Unusable string
	// ATR is the present card's answer to reset when the backend can see it
	// without touching the card. Only PC/SC can.
	ATR []byte
	// Known is a reader on the built-in list of contactless readers.
	Known bool
	// Busy is an unusable reader that another program holds, which is the
	// normal state of one the PC/SC service is serving.
	Busy bool
}

// backend is one way of reaching readers: the system PC/SC service, or USB
// directly.
type backend interface {
	Name() string
	// List returns the readers currently reachable.
	List(ctx context.Context) ([]readerInfo, error)
	// CardPresent reports whether a reader has a card, without activating it.
	CardPresent(ctx context.Context, info *readerInfo) (bool, error)
	Open(ctx context.Context, info *readerInfo) (device, error)
	Close() error
}
