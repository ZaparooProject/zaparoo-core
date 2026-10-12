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
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const backendPCSC = "pcsc"

// pcscStatus is a reader's card state as the PC/SC service reports it. The
// service knows the answer to reset without the caller connecting to the card.
type pcscStatus struct {
	ATR     []byte
	Present bool
}

// pcscContext is the part of the PC/SC API the driver uses. Windows and macOS
// reach it through a Go binding; Linux loads the library at run time, because
// it is not there on the systems this driver talks USB on.
type pcscContext interface {
	// ListReaders returns no readers, and no error, when none are attached.
	ListReaders() ([]string, error)
	// Status returns errReaderGone for a reader the service no longer has.
	Status(name string) (pcscStatus, error)
	Connect(name string) (pcscCard, error)
	Release() error
}

type pcscCard interface {
	Transmit(apdu []byte) ([]byte, error)
	Disconnect(reset bool) error
}

// pcscBackend reaches readers through the system PC/SC service.
type pcscBackend struct {
	establish func() (pcscContext, error)
	ctx       pcscContext
	mu        syncutil.Mutex
}

var _ backend = (*pcscBackend)(nil)

func newPCSCBackend(establish func() (pcscContext, error)) *pcscBackend {
	return &pcscBackend{establish: establish}
}

func (*pcscBackend) Name() string {
	return backendPCSC
}

func (b *pcscBackend) context() (pcscContext, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil {
		return b.ctx, nil
	}
	ctx, err := b.establish()
	if err != nil {
		return nil, err
	}
	b.ctx = ctx
	return ctx, nil
}

func (b *pcscBackend) List(context.Context) ([]readerInfo, error) {
	ctx, err := b.context()
	if err != nil {
		return nil, err
	}
	names, err := ctx.ListReaders()
	if err != nil {
		return nil, fmt.Errorf("list readers: %w", err)
	}

	infos := make([]readerInfo, 0, len(names))
	for _, name := range names {
		info := readerInfo{Path: name, Name: name, Known: isKnownName(name)}
		status, statusErr := ctx.Status(name)
		if statusErr != nil {
			// Unplugged between the two calls.
			continue
		}
		if status.Present {
			info.ATR = status.ATR
			if info.ATR == nil {
				info.ATR = []byte{}
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (*pcscBackend) CardPresent(_ context.Context, info *readerInfo) (bool, error) {
	return info.ATR != nil, nil
}

func (b *pcscBackend) Open(_ context.Context, info *readerInfo) (device, error) {
	ctx, err := b.context()
	if err != nil {
		return nil, err
	}
	if _, err := ctx.Status(info.Path); err != nil {
		return nil, fmt.Errorf("reader status: %w", err)
	}
	return &pcscDevice{ctx: ctx, name: info.Path}, nil
}

func (b *pcscBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx == nil {
		return nil
	}
	err := b.ctx.Release()
	b.ctx = nil
	if err != nil {
		return fmt.Errorf("release context: %w", err)
	}
	return nil
}

// pcscDevice is one reader of the PC/SC service.
//
// It connects only to a card whose answer to reset has the contactless form.
// The service shows that without a connection, so a contact card or a
// security key in a reader is never touched.
type pcscDevice struct {
	ctx  pcscContext
	card pcscCard
	name string
}

var _ device = (*pcscDevice)(nil)

func (d *pcscDevice) Poll(context.Context) (cardState, error) {
	status, err := d.ctx.Status(d.name)
	if err != nil {
		d.drop(false)
		return cardState{}, fmt.Errorf("reader status: %w", err)
	}
	if !status.Present {
		d.drop(false)
		return cardState{}, nil
	}
	if !isContactlessATR(status.ATR) {
		d.drop(false)
		return cardState{ATR: status.ATR}, nil
	}
	if d.card == nil {
		card, connectErr := d.ctx.Connect(d.name)
		if connectErr != nil {
			if errors.Is(connectErr, errReaderGone) {
				return cardState{}, fmt.Errorf("connect: %w", connectErr)
			}
			// The tag left between the status and the connect.
			return cardState{}, nil
		}
		d.card = card
	}
	return cardState{ATR: status.ATR, Present: true}, nil
}

func (d *pcscDevice) drop(reset bool) {
	if d.card == nil {
		return
	}
	_ = d.card.Disconnect(reset)
	d.card = nil
}

func (d *pcscDevice) Transmit(_ context.Context, apdu []byte) ([]byte, error) {
	if d.card == nil {
		return nil, errNoCard
	}
	resp, err := d.card.Transmit(apdu)
	if err != nil {
		return nil, fmt.Errorf("transmit: %w", err)
	}
	return resp, nil
}

func (d *pcscDevice) Reactivate(context.Context) {
	d.drop(true)
}

func (d *pcscDevice) Close() error {
	d.drop(false)
	return nil
}
