//go:build windows || darwin

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
	"errors"
	"fmt"

	"github.com/ebfe/scard"
)

func establishPCSC() (pcscContext, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("establish PC/SC context: %w", err)
	}
	return &scardContext{ctx: ctx}, nil
}

type scardContext struct {
	ctx *scard.Context
}

func (c *scardContext) ListReaders() ([]string, error) {
	names, err := c.ctx.ListReaders()
	if errors.Is(err, scard.ErrNoReadersAvailable) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scard list readers: %w", err)
	}
	return names, nil
}

func (c *scardContext) Status(name string) (pcscStatus, error) {
	states := []scard.ReaderState{{Reader: name, CurrentState: scard.StateUnaware}}
	if err := c.ctx.GetStatusChange(states, 0); err != nil {
		if errors.Is(err, scard.ErrUnknownReader) || errors.Is(err, scard.ErrReaderUnavailable) ||
			errors.Is(err, scard.ErrNoReadersAvailable) || errors.Is(err, scard.ErrNoService) ||
			errors.Is(err, scard.ErrServiceStopped) {
			return pcscStatus{}, fmt.Errorf("%w: %w", errReaderGone, err)
		}
		return pcscStatus{}, fmt.Errorf("scard status: %w", err)
	}
	if states[0].EventState&(scard.StateUnknown|scard.StateUnavailable) != 0 {
		return pcscStatus{}, errReaderGone
	}
	return pcscStatus{
		Present: states[0].EventState&scard.StatePresent != 0,
		ATR:     states[0].Atr,
	}, nil
}

func (c *scardContext) Connect(name string) (pcscCard, error) {
	card, err := c.ctx.Connect(name, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		if errors.Is(err, scard.ErrUnknownReader) || errors.Is(err, scard.ErrReaderUnavailable) {
			return nil, fmt.Errorf("%w: %w", errReaderGone, err)
		}
		return nil, fmt.Errorf("scard connect: %w", err)
	}
	return &scardCard{card: card}, nil
}

func (c *scardContext) Release() error {
	if err := c.ctx.Release(); err != nil {
		return fmt.Errorf("scard release: %w", err)
	}
	return nil
}

type scardCard struct {
	card *scard.Card
}

func (c *scardCard) Transmit(apdu []byte) ([]byte, error) {
	resp, err := c.card.Transmit(apdu)
	if err != nil {
		return nil, fmt.Errorf("scard transmit: %w", err)
	}
	return resp, nil
}

func (c *scardCard) Disconnect(reset bool) error {
	disposition := scard.LeaveCard
	if reset {
		disposition = scard.ResetCard
	}
	if err := c.card.Disconnect(disposition); err != nil {
		return fmt.Errorf("scard disconnect: %w", err)
	}
	return nil
}
