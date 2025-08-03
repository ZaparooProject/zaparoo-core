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

package acr122pcsc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ebfe/scard"
	"github.com/rs/zerolog/log"
)

type ACR122PCSC struct {
	cfg     *config.Instance
	ctx     *scard.Context
	device  config.ReadersConnect
	name    string
	polling bool
}

func NewAcr122Pcsc(cfg *config.Instance) *ACR122PCSC {
	return &ACR122PCSC{
		cfg: cfg,
	}
}

func (*ACR122PCSC) IDs() []string {
	return []string{"acr122_pcsc"}
}

func (r *ACR122PCSC) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	if r.ctx == nil {
		ctx, err := scard.EstablishContext()
		if err != nil {
			return fmt.Errorf("failed to establish scard context: %w", err)
		}
		r.ctx = ctx
	}

	rls, err := r.ctx.ListReaders()
	if err != nil {
		return fmt.Errorf("failed to list scard readers: %w", err)
	}

	if !helpers.Contains(rls, device.Path) {
		return errors.New("reader not found: " + device.Path)
	}

	r.device = device
	r.name = device.Path
	r.polling = true

	go func() {
		for r.polling {
			ctx := r.ctx
			if ctx == nil {
				continue
			}

			rls, err := ctx.ListReaders()
			if err != nil {
				log.Debug().Msgf("error listing pcsc readers: %s", err)
				r.polling = false
				break
			}

			if !helpers.Contains(rls, r.name) {
				log.Debug().Msgf("reader not found: %s", r.name)
				r.polling = false
				break
			}

			rs := []scard.ReaderState{{
				Reader:       r.name,
				CurrentState: scard.StateUnaware,
			}}

			err = ctx.GetStatusChange(rs, 250*time.Millisecond)
			if err != nil {
				log.Debug().Msgf("error getting status change: %s", err)
				continue
			}

			if rs[0].EventState&scard.StatePresent == 0 {
				continue
			}

			tag, err := ctx.Connect(r.name, scard.ShareShared, scard.ProtocolAny)
			if err != nil {
				log.Debug().Msgf("error connecting to reader: %s", err)
				continue
			}

			status, err := tag.Status()
			if err != nil {
				log.Debug().Msgf("error getting status: %s", err)
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			log.Debug().Msgf("status: %v", hex.EncodeToString(status.Atr))

			res, err := tag.Transmit([]byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
			if err != nil {
				log.Debug().Msgf("error transmitting: %s", err)
				continue
			}

			if len(res) < 2 {
				log.Debug().Msgf("invalid response")
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			resCode := res[len(res)-2:]
			if resCode[0] != 0x90 && resCode[1] != 0x00 {
				log.Debug().Msgf("invalid response code: %x", resCode)
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			log.Debug().Msgf("response: %x", res)
			uid := res[:len(res)-2]

			i := 0
			data := make([]byte, 0)
		dataLoop:
			for {
				res, err = tag.Transmit([]byte{0xFF, 0xB0, 0x00, byte(i), 0x04})
				switch {
				case err != nil:
					log.Debug().Msgf("error transmitting: %s", err)
					break dataLoop
				case bytes.Equal(res, []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}):
					break dataLoop
				case len(res) < 6:
					log.Debug().Msgf("invalid response")
					break dataLoop
				case i >= 221:
					break dataLoop
				}

				data = append(data, res[:len(res)-2]...)
				i++
			}

			log.Debug().Msgf("data: %x", data)

			text, err := ParseRecordText(data)
			if err != nil {
				log.Debug().Msgf("error parsing NDEF record: %s", err)
				text = ""
			}

			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token: &tokens.Token{
					UID:      hex.EncodeToString(uid),
					Text:     text,
					ScanTime: time.Now(),
					Source:   r.device.ConnectionString(),
				},
			}

			_ = tag.Disconnect(scard.ResetCard)

			for r.polling {
				rs := []scard.ReaderState{{
					Reader:       r.name,
					CurrentState: scard.StatePresent,
				}}

				err := ctx.GetStatusChange(rs, 250*time.Millisecond)
				if err != nil {
					log.Debug().Msgf("error getting status change: %s", err)
					break
				}

				if rs[0].EventState&scard.StatePresent == 0 {
					break
				}
			}

			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token:  nil,
			}
		}
	}()

	return nil
}

func (r *ACR122PCSC) Close() error {
	r.polling = false
	if r.ctx != nil {
		err := r.ctx.Release()
		if err != nil {
			return fmt.Errorf("failed to release scard context: %w", err)
		}
	}
	return nil
}

// TODO: this is a hack workaround to stop some log spam, probably the Detect
// functions on readers should actually return an error instead of ""
var detectErrorOnce sync.Once

func (*ACR122PCSC) Detect(connected []string) string {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return ""
	}
	defer func(ctx *scard.Context) {
		if releaseErr := ctx.Release(); releaseErr != nil {
			log.Warn().Err(releaseErr).Msg("error releasing pcsc context")
		}
	}(ctx)

	rs, err := ctx.ListReaders()
	if err != nil {
		detectErrorOnce.Do(func() {
			log.Debug().Err(err).Msg("listing pcsc readers")
		})
		return ""
	}

	acrs := make([]string, 0)
	for _, r := range rs {
		if strings.HasPrefix(r, "ACS ACR122") && !helpers.Contains(connected, "acr122_pcsc:"+r) {
			acrs = append(acrs, r)
		}
	}

	if len(acrs) == 0 {
		return ""
	}

	log.Debug().Msgf("acr122 reader found: %s", acrs[0])
	return "acr122_pcsc:" + acrs[0]
}

func (r *ACR122PCSC) Device() string {
	return r.device.ConnectionString()
}

func (r *ACR122PCSC) Connected() bool {
	return r.polling
}

func (r *ACR122PCSC) Info() string {
	return r.name
}

func (*ACR122PCSC) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*ACR122PCSC) CancelWrite() {
	// no-op, writing not supported
}
