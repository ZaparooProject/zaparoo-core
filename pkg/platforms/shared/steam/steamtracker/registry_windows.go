//go:build windows

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

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

package steamtracker

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const steamRegistryPath = `SOFTWARE\Valve\Steam`

// keyNotify is the KEY_NOTIFY access right (0x10) for registry change notifications.
// Not exported by golang.org/x/sys/windows/registry, so we define it here.
const keyNotify = 0x10

// RegistryWatcher monitors Steam's registry for game state changes using
// event-driven notifications (RegNotifyChangeKeyValue).
type RegistryWatcher struct {
	onChange  func(appID int)
	done      chan struct{}
	stopEvent windows.Handle // signaled on Stop() for instant shutdown
	wg        sync.WaitGroup
}

// NewRegistryWatcher creates a new watcher that calls onChange when RunningAppID changes.
func NewRegistryWatcher(onChange func(appID int)) *RegistryWatcher {
	return &RegistryWatcher{
		onChange: onChange,
		done:     make(chan struct{}),
	}
}

// Start begins watching for registry changes. Blocks until Stop is called.
func (w *RegistryWatcher) Start() error {
	// Create manual-reset stop event (signaled by Stop() for instant shutdown)
	stopEvent, err := windows.CreateEvent(nil, 1, 0, nil) // manual-reset, initially non-signaled
	if err != nil {
		return fmt.Errorf("create stop event: %w", err)
	}
	w.stopEvent = stopEvent

	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		steamRegistryPath,
		registry.QUERY_VALUE|keyNotify,
	)
	if err != nil {
		_ = windows.CloseHandle(w.stopEvent)
		return fmt.Errorf("open Steam registry key: %w", err)
	}

	w.wg.Add(1)
	go w.watchLoop(key)
	return nil
}

// Stop stops watching for registry changes.
func (w *RegistryWatcher) Stop() {
	close(w.done)
	_ = windows.SetEvent(w.stopEvent) // Signal immediately for instant shutdown
	w.wg.Wait()
	_ = windows.CloseHandle(w.stopEvent)
}

func (w *RegistryWatcher) watchLoop(key registry.Key) {
	defer w.wg.Done()
	defer func() { _ = key.Close() }()

	// Create event for registry notifications
	regEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to create event for registry watcher")
		return
	}
	defer func() { _ = windows.CloseHandle(regEvent) }()

	// Wait on both registry event and stop event
	handles := []windows.Handle{regEvent, w.stopEvent}

	// Read initial value
	lastAppID := readAppID(key)
	if w.onChange != nil && lastAppID != 0 {
		w.onChange(lastAppID)
	}

	for {
		// Register for notification on value changes
		err := windows.RegNotifyChangeKeyValue(
			windows.Handle(key),
			false,                              // don't watch subtree
			windows.REG_NOTIFY_CHANGE_LAST_SET, // notify on value changes
			regEvent,
			true, // async
		)
		if err != nil {
			log.Error().Err(err).Msg("RegNotifyChangeKeyValue failed")
			return
		}

		// Wait for EITHER registry change OR stop signal (instant response)
		result, err := windows.WaitForMultipleObjects(handles, false, windows.INFINITE)
		if err != nil {
			log.Error().Err(err).Msg("WaitForMultipleObjects failed")
			return
		}

		switch result {
		case windows.WAIT_OBJECT_0: // Registry changed
			appID := readAppID(key)
			if appID != lastAppID {
				lastAppID = appID
				if w.onChange != nil {
					w.onChange(appID)
				}
			}
		case windows.WAIT_OBJECT_0 + 1: // Stop signaled
			return
		default:
			// Unexpected result, exit
			return
		}
	}
}

func readAppID(key registry.Key) int {
	appID, _, err := key.GetIntegerValue("RunningAppID")
	if err != nil {
		return 0
	}
	return int(appID) //nolint:gosec // AppID won't overflow int
}

// GetRunningAppID reads the currently running Steam game AppID from the registry.
// Returns 0 if no game is running or the key doesn't exist.
func GetRunningAppID() (int, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		steamRegistryPath,
		registry.QUERY_VALUE,
	)
	if err != nil {
		// Key not existing means no game running
		return 0, nil
	}
	defer func() { _ = key.Close() }()

	return readAppID(key), nil
}

// IsSteamInstalled checks if Steam is installed by looking for the registry key.
func IsSteamInstalled() bool {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		steamRegistryPath,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return false
	}
	_ = key.Close()
	return true
}
