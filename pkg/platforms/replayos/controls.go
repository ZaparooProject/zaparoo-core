//go:build linux && !android

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

package replayos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
)

const (
	// netControlBaseURL is RePlayOS's net control REST API, available from
	// RePlayOS 1.7.3 once SYSTEM > NET_CONTROL is on.
	netControlBaseURL = "http://127.0.0.1:55356/api/v1"
	netControlTimeout = 3 * time.Second

	// netControlTokenKey is the replay.cfg key holding the net control code
	// that RePlayOS shows under SYSTEM > INFORMATION > NET CONTROL CODE.
	netControlTokenKey = "replay_http_token" //nolint:gosec // config key name, not a credential

	// dataLocationKey is the replay.cfg key for SYSTEM > DATA LOCATION. Set to
	// dataLocationLocalSD, saves live on the SD card whatever storage holds
	// the ROMs.
	dataLocationKey     = "system_data_location"
	dataLocationLocalSD = "local_sd"

	// replayGameView is the get_status view while a game is running.
	replayGameView = "game_play"

	// readyPollInterval is how often a readiness probe asks get_status. The
	// API is down for about a second while replay.service restarts into the
	// launched game, so this sets how soon after that the game is noticed.
	readyPollInterval = 250 * time.Millisecond

	minStateSlot = 1
	maxStateSlot = 18

	maxNetControlResponse = 64 << 10
)

var (
	errNetControlUnavailable = errors.New(
		"RePlayOS net control is not reachable: needs RePlayOS 1.7.3 or later with SYSTEM > NET_CONTROL on",
	)
	errNetControlToken = errors.New("RePlayOS rejected the net control code in replay.cfg")
	errNoGameRunning   = errors.New("no game is running")

	errNetControlRateLimited = errors.New("RePlayOS net control is rate limiting requests")
)

// netControl talks to RePlayOS's net control REST API. The code it
// authenticates with is read from replay.cfg on every call, so it follows
// changes made in the RePlayOS menu.
type netControl struct {
	client  *http.Client
	baseURL string
	cfgPath string
	// localSD is the SD card mount saves move to with DATA LOCATION set to
	// LOCAL SD.
	localSD string
}

func newNetControl() *netControl {
	return &netControl{
		baseURL: netControlBaseURL,
		cfgPath: replayCfgPath,
		localSD: "/media/sd",
		client:  &http.Client{Timeout: netControlTimeout, Transport: useragent.Transport(nil)},
	}
}

// readCfgValue returns the value of key in a replay.cfg file. Later lines win,
// as RePlayOS rewrites the file with the last value it saw for a key.
func readCfgValue(cfgPath, key string) (string, bool) {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // Path is a package-level constant
	if err != nil {
		return "", false
	}
	var value string
	var found bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		value, found = strings.Trim(strings.TrimSpace(v), `"`), true
	}
	return value, found
}

// get calls an endpoint and returns its JSON body.
func (n *netControl) get(ctx context.Context, endpoint string, query url.Values) ([]byte, error) {
	token, ok := readCfgValue(n.cfgPath, netControlTokenKey)
	if !ok || token == "" {
		return nil, fmt.Errorf("no %s in replay.cfg: %w", netControlTokenKey, errNetControlUnavailable)
	}

	target := n.baseURL + "/" + endpoint
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build net control request: %w", err)
	}
	req.Header.Set("X-RePlay-Token", token)

	resp, err := n.client.Do(req)
	if err != nil {
		// A cancelled caller is not a sign that net control is off.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("net control request: %w", ctxErr)
		}
		return nil, fmt.Errorf("%w: %w", errNetControlUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNetControlResponse))
	if err != nil {
		return nil, fmt.Errorf("read net control response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errNetControlToken
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, errNetControlRateLimited
	case resp.StatusCode != http.StatusOK:
		var apiErr struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("RePlayOS net control: %s %s", apiErr.Error, apiErr.Detail)
		}
		return nil, fmt.Errorf("RePlayOS net control returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// netStatus is the part of get_status the controls and readiness probe use.
type netStatus struct {
	View     string `json:"view"`
	GameFile string `json:"game_file"` //nolint:tagliatelle // RePlayOS names the field
}

func (n *netControl) status(ctx context.Context) (netStatus, error) {
	body, err := n.get(ctx, "get_status", nil)
	if err != nil {
		return netStatus{}, err
	}
	var status netStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return netStatus{}, fmt.Errorf("parse RePlayOS status: %w", err)
	}
	return status, nil
}

// requireGame checks that a game is running, because RePlayOS queues state
// and reset commands without a game and silently drops them.
func (n *netControl) requireGame(ctx context.Context) error {
	_, err := n.runningGame(ctx)
	return err
}

// runningGame returns the status of the running game, or an error when no
// game is running.
func (n *netControl) runningGame(ctx context.Context) (netStatus, error) {
	status, err := n.status(ctx)
	if err != nil {
		return netStatus{}, err
	}
	if status.View != replayGameView {
		return netStatus{}, fmt.Errorf("%w: %w", errNoGameRunning, platforms.ErrControlUnavailable)
	}
	return status, nil
}

// stateFile returns where RePlayOS keeps slot's save state for the game file:
// the game's own path with roms swapped for saves, on dataRoot instead of the
// game's storage when dataRoot is set. It is false when the path has no roms
// folder to swap.
func stateFile(gameFile string, slot int, dataRoot string) (string, bool) {
	dir, base := filepath.Split(filepath.Clean(gameFile))
	parts := strings.Split(filepath.Clean(dir), string(filepath.Separator))
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "roms" {
			continue
		}
		parts[i] = "saves"
		root := string(filepath.Separator)
		if dataRoot != "" {
			root, parts = dataRoot, parts[i:]
		}
		name := strings.TrimSuffix(base, filepath.Ext(base)) + ".sst" + strconv.Itoa(slot)
		return filepath.Join(root, filepath.Join(parts...), name), true
	}
	return "", false
}

// dataRoot returns where RePlayOS keeps saves when it is not beside the ROMs.
func (n *netControl) dataRoot() string {
	if location, _ := readCfgValue(n.cfgPath, dataLocationKey); location == dataLocationLocalSD {
		return n.localSD
	}
	return ""
}

// errNoSavedState reports a load from a slot nothing was saved to. RePlayOS
// accepts such a load and does nothing, so it is caught before asking.
var errNoSavedState = errors.New("no save state in that slot")

// checkStateSaved fails a load from an empty slot. It only refuses when the
// game's saves folder exists, so an install keeping saves elsewhere still
// loads.
func checkStateSaved(gameFile string, slot int, dataRoot string) error {
	path, ok := stateFile(gameFile, slot, dataRoot)
	if !ok {
		return nil
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil //nolint:nilerr // saves kept somewhere else; let RePlayOS decide
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: slot %d: %w", errNoSavedState, slot, platforms.ErrControlUnavailable)
	}
	return nil
}

// waitForGame returns a launcher readiness probe that polls get_status until
// RePlayOS reports the launched game running. The API is unreachable while
// replay.service restarts into the game, so connection failures and rate
// limiting are retried; a rejected net control code ends the wait.
func (n *netControl) waitForGame(
	clock clockwork.Clock,
) func(context.Context, *config.Instance, *models.ActiveMedia) error {
	return func(ctx context.Context, _ *config.Instance, media *models.ActiveMedia) error {
		return platforms.PollReady(ctx, clock, readyPollInterval, func(ctx context.Context) (bool, error) {
			status, err := n.status(ctx)
			switch {
			case errors.Is(err, errNetControlUnavailable), errors.Is(err, errNetControlRateLimited):
				return false, fmt.Errorf("%w: %w", platforms.ErrReadinessUnavailable, err)
			case err != nil:
				return false, err
			}
			return status.View == replayGameView && sameGame(status.GameFile, media.Path), nil
		})
	}
}

// runningGameFile returns the file RePlayOS is playing, and false when it is
// not playing one or net control cannot be reached.
func (n *netControl) runningGameFile(ctx context.Context) (string, bool) {
	status, err := n.status(ctx)
	if err != nil || status.View != replayGameView || status.GameFile == "" {
		return "", false
	}
	return status.GameFile, true
}

// gameRunning reports whether RePlayOS is playing a game. It is unknown, and
// an error, when net control cannot be reached.
func (n *netControl) gameRunning(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) (bool, error) {
	status, err := n.status(ctx)
	if err != nil {
		return false, err
	}
	return status.View == replayGameView, nil
}

// sameGame reports whether the game RePlayOS is running is the launched one.
// RePlayOS reports the path it was started with, which may spell a mount
// differently, so the file name and the folder holding it (the system's
// folder) are accepted in place of the full path. A bare file name matches on
// the name alone.
func sameGame(running, launched string) bool {
	if running == "" || launched == "" {
		return false
	}
	running, launched = filepath.Clean(running), filepath.Clean(launched)
	if running == launched {
		return true
	}
	if filepath.Base(running) != filepath.Base(launched) {
		return false
	}
	runningDir := filepath.Base(filepath.Dir(running))
	return runningDir == "." || runningDir == string(filepath.Separator) ||
		runningDir == filepath.Base(filepath.Dir(launched))
}

func (n *netControl) stateControl(endpoint string) platforms.ControlFunc {
	return func(ctx context.Context, _ *config.Instance, params platforms.ControlParams) error {
		slot, slotErr := platforms.SlotArg(params, minStateSlot, maxStateSlot)
		if slotErr != nil {
			return fmt.Errorf("%s: %w", endpoint, slotErr)
		}
		status, err := n.runningGame(ctx)
		if err != nil {
			return err
		}
		if endpoint == "load_state" {
			if savedErr := checkStateSaved(status.GameFile, slot, n.dataRoot()); savedErr != nil {
				return savedErr
			}
		}
		_, err = n.get(ctx, endpoint, url.Values{"slot": {strconv.Itoa(slot)}})
		return err
	}
}

func (n *netControl) reset(ctx context.Context, _ *config.Instance, _ platforms.ControlParams) error {
	if err := n.requireGame(ctx); err != nil {
		return err
	}
	_, err := n.get(ctx, "set_cmd", url.Values{"cmd": {"game_reset"}})
	return err
}

// controls returns the controls shared by every ReplayOS system launcher.
// The net control API has no pause or menu command, so those are not offered.
func (p *Platform) controls() map[string]platforms.Control {
	n := newNetControl()
	return map[string]platforms.Control{
		platforms.ControlSaveState: {Func: n.stateControl("save_state")},
		platforms.ControlLoadState: {Func: n.stateControl("load_state")},
		platforms.ControlReset:     {Func: n.reset},
		platforms.ControlStop: {Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
			return p.StopActiveLauncher(platforms.StopForMenu)
		}},
	}
}
