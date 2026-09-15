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

// Package librarysync keeps this device's library converged with the linked
// Zaparoo Online account while the user has Library sync turned on. Every
// local feature it touches works without it: sync only uploads what the
// device already holds and never makes a local list depend on the account.
package librarysync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/rs/zerolog/log"
)

const (
	// DeviceStateKeyEnabledSeen records the Library sync setting the last
	// pass acted on, so turning it off is noticed even across a restart or a
	// hand-edited config file.
	DeviceStateKeyEnabledSeen = "library_sync_enabled_seen"
	// DeviceStateKeyInventoryDeletePending records that the account still
	// holds this device's inventory after Library sync was turned off.
	DeviceStateKeyInventoryDeletePending = "library_inventory_delete_pending"
)

var (
	// ErrDisabled reports a pass skipped because Library sync is off.
	ErrDisabled = errors.New("library sync is disabled")
	// ErrNotSettled reports a pass deferred because the media database is
	// being indexed or optimized.
	ErrNotSettled = errors.New("media database is not settled")
)

// IsIdleError reports an error that means there is nothing to do right now
// rather than a failure: sync is off, the device is not linked, or the index
// is still being written.
func IsIdleError(err error) bool {
	return errors.Is(err, ErrDisabled) || errors.Is(err, ErrNotSettled) || backup.IsRemoteUnlinkedError(err)
}

// ClientFactory returns a client for the Library sync endpoint.
type ClientFactory func(baseURL string) (*backup.OnlineClient, error)

// Options configures a Service.
type Options struct {
	Config    *config.Instance
	DB        *database.Database
	NewClient ClientFactory
	Inbox     *inbox.Service
	Pauser    *syncutil.Pauser
	// SendHeartbeat reports the device's capabilities, including whether
	// Library sync is on, after the setting changes. Optional.
	SendHeartbeat func(context.Context) error
	// Now returns the current time. Optional.
	Now func() time.Time
	// ResolvePace is the least time between resolve requests. Zero uses
	// the default.
	ResolvePace time.Duration
}

// Service runs Library sync passes.
type Service struct {
	cfg           *config.Instance
	db            *database.Database
	newClient     ClientFactory
	inbox         *inbox.Service
	pauser        *syncutil.Pauser
	sendHeartbeat func(context.Context) error
	now           func() time.Time
	resolvePace   time.Duration
	inventoryMu   syncutil.Mutex
	stateMu       syncutil.Mutex
}

// New returns a Service.
func New(opts *Options) *Service {
	s := &Service{
		cfg:           opts.Config,
		db:            opts.DB,
		newClient:     opts.NewClient,
		inbox:         opts.Inbox,
		pauser:        opts.Pauser,
		sendHeartbeat: opts.SendHeartbeat,
		now:           opts.Now,
		resolvePace:   opts.ResolvePace,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.resolvePace <= 0 {
		s.resolvePace = defaultResolvePace
	}
	return s
}

func (s *Service) client() (*backup.OnlineClient, error) {
	if s.newClient == nil {
		return nil, errors.New("library sync has no online client")
	}
	return s.newClient(s.cfg.OnlineBaseURL())
}

// ApplySetting acts on a change of the Library sync setting since the last
// time it was applied: turning it off marks the device's inventory for
// deletion, turning it on forces the next pass to check what the account
// holds, and either way the account is told through a heartbeat. It reports
// whether the setting had changed.
func (s *Service) ApplySetting(ctx context.Context) (bool, error) {
	enabled := s.cfg.LibrarySyncEnabled()
	raw, found, err := s.db.UserDB.GetDeviceState(DeviceStateKeyEnabledSeen)
	if err != nil {
		return false, fmt.Errorf("read library sync setting marker: %w", err)
	}
	seen := found && raw == "1"
	if enabled == seen {
		return false, nil
	}

	if enabled {
		if err := s.db.UserDB.SetDeviceState(DeviceStateKeyInventoryDeletePending, "0"); err != nil {
			return false, fmt.Errorf("clear library inventory delete marker: %w", err)
		}
		if err := s.forgetInventoryConfirmation(ctx); err != nil {
			return false, err
		}
	} else if err := s.db.UserDB.SetDeviceState(DeviceStateKeyInventoryDeletePending, "1"); err != nil {
		return false, fmt.Errorf("mark library inventory for deletion: %w", err)
	}
	value := "0"
	if enabled {
		value = "1"
	}
	if err := s.db.UserDB.SetDeviceState(DeviceStateKeyEnabledSeen, value); err != nil {
		return false, fmt.Errorf("record library sync setting: %w", err)
	}
	log.Info().Bool("enabled", enabled).Msg("library sync setting changed")

	if s.sendHeartbeat != nil {
		if hbErr := s.sendHeartbeat(ctx); hbErr != nil && !backup.IsRemoteUnlinkedError(hbErr) {
			log.Debug().Err(hbErr).Msg("library sync capability heartbeat not sent")
		}
	}
	return true, nil
}

// forgetInventoryConfirmation makes the next inventory pass ask the account
// what it holds instead of trusting the local record, since the account may
// have dropped the inventory while sync was off.
func (s *Service) forgetInventoryConfirmation(ctx context.Context) error {
	state, err := s.db.MediaDB.GetLibraryInventoryState(ctx)
	if err != nil {
		return fmt.Errorf("read library inventory state: %w", err)
	}
	if state.ConfirmedAt.IsZero() {
		return nil
	}
	state.ConfirmedAt = time.Time{}
	if err := s.db.MediaDB.SetLibraryInventoryState(ctx, &state); err != nil {
		return fmt.Errorf("store library inventory state: %w", err)
	}
	return nil
}

// mediaDBSettled reports whether the index is quiet enough to read as the
// device's library.
func mediaDBSettled(mediaDB database.MediaDBI) bool {
	indexing, err := mediaDB.GetIndexingStatus()
	if err != nil || indexing == mediadb.IndexingStatusRunning || indexing == mediadb.IndexingStatusPending {
		return false
	}
	optimizing, err := mediaDB.GetOptimizationStatus()
	if err != nil || optimizing == mediadb.IndexingStatusRunning || optimizing == mediadb.IndexingStatusPending {
		return false
	}
	return true
}
