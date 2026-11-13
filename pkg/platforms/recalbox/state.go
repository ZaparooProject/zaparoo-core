//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

const (
	// stateFile is rewritten by EmulationStation on every frontend event,
	// just before the event is published on MQTT.
	stateFile    = "/tmp/es_state.inf"
	maxStateSize = 64 << 10

	esBroker     = "tcp://127.0.0.1:1883"
	esEventTopic = "Recalbox/EmulationStation/Event"

	statePollEvery      = 2 * time.Second
	mqttRetryEvery      = 10 * time.Second
	mqttDisconnectQuiet = 250
)

const (
	actionRunGame = "rungame"
)

// clearingActions end whatever game was running. Scraper, clip,
// configuration and wake events say nothing about the game and are ignored.
//
//nolint:gochecknoglobals // Package-level configuration
var clearingActions = map[string]struct{}{
	"start": {}, "stop": {}, "shutdown": {}, "reboot": {}, "relaunch": {}, "quit": {},
	"systembrowsing": {}, "gamelistbrowsing": {}, "endgame": {}, "rundemo": {},
	"enddemo": {}, "runkodi": {}, "sleep": {},
}

// esState is the part of es_state.inf Core uses.
type esState struct {
	written  time.Time
	Action   string
	SystemID string
	Game     string
	GamePath string
}

// key identifies one write of the file; ES rewrites it for every event, so
// the same game started twice is two keys.
func (s *esState) key() string {
	return s.Action + "\x00" + s.SystemID + "\x00" + s.GamePath + "\x00" +
		strconv.FormatInt(s.written.UnixNano(), 10)
}

// parseState reads es_state.inf. ES rewrites the file in place, so a read can
// catch it half written; State= is always the last line, and a file without
// it is reported as incomplete.
func parseState(data []byte) (esState, bool) {
	var st esState
	complete := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 4096), maxStateSize)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		value = strings.TrimRight(value, "\r")
		switch key {
		case "Action":
			st.Action = strings.ToLower(strings.TrimSpace(value))
		case "SystemId":
			st.SystemID = strings.TrimSpace(value)
		case "Game":
			st.Game = value
		case "GamePath":
			st.GamePath = value
		case "State":
			complete = true
		}
	}
	if scanner.Err() != nil || st.Action == "" {
		return esState{}, false
	}
	return st, complete
}

func (p *Platform) readState() (esState, bool) {
	f, err := p.fs.Open(stateFile)
	if err != nil {
		return esState{}, false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return esState{}, false
	}
	// Taken before reading, so content from an older write never pairs with
	// a newer time and hides the next event.
	written := info.ModTime()
	data, err := io.ReadAll(io.LimitReader(f, maxStateSize+1))
	if err != nil || len(data) > maxStateSize {
		return esState{}, false
	}
	st, ok := parseState(data)
	st.written = written
	return st, ok
}

// poke asks the tracker to read the state file now.
func (p *Platform) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// refresh reads the state file and applies a changed event. The first read
// after start only records the state, unless a game is still running.
func (p *Platform) refresh() {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	st, ok := p.readState()
	if !ok {
		return
	}

	p.stateMu.Lock()
	key := st.key()
	if key == p.lastKey {
		p.stateMu.Unlock()
		return
	}
	first := !p.seenState
	p.seenState = true
	p.lastKey = key
	p.observed = st
	p.generation++
	changed := p.changed
	p.changed = make(chan struct{})
	p.stateMu.Unlock()
	close(changed)

	if first && st.Action == actionRunGame && len(p.findLaunchers()) == 0 {
		log.Debug().Str("path", st.GamePath).Msg("ignoring stale game in ES state file")
		return
	}
	p.applyState(&st)
}

func (p *Platform) applyState(st *esState) {
	if st.Action == actionRunGame {
		p.gameStarted(st)
		return
	}
	if _, ok := clearingActions[st.Action]; ok {
		p.gameEnded()
	}
}

func (p *Platform) gameStarted(st *esState) {
	path := filepath.Clean(st.GamePath)
	if !filepath.IsAbs(path) {
		return
	}
	media := p.mediaForState(st, path)
	if media == nil {
		log.Debug().Str("system", st.SystemID).Str("path", path).
			Msg("game started by EmulationStation is not in a known system")
		return
	}

	p.stateMu.Lock()
	p.gamePath = path
	p.stateMu.Unlock()

	log.Info().Str("system", st.SystemID).Str("path", path).Msg("EmulationStation started a game")
	if p.activeMedia != nil {
		if current := p.activeMedia(); current != nil && current.Path == path {
			return
		}
	}
	if p.setActiveMedia != nil {
		p.setActiveMedia(media)
	}
}

// gameEnded clears active media only when it is the game the tracker saw
// start, so a newer launch is never cleared by a late event.
func (p *Platform) gameEnded() {
	p.stateMu.Lock()
	path := p.gamePath
	p.gamePath = ""
	p.stateMu.Unlock()
	if path == "" || p.setActiveMedia == nil {
		return
	}
	if p.activeMedia != nil {
		if current := p.activeMedia(); current == nil || current.Path != path {
			return
		}
	}
	log.Info().Str("path", path).Msg("game ended")
	p.setActiveMedia(nil)
}

func (p *Platform) forgetGame() {
	p.stateMu.Lock()
	p.gamePath = ""
	p.stateMu.Unlock()
}

func (p *Platform) mediaForState(st *esState, path string) *models.ActiveMedia {
	info, ok := lookupSystem(st.SystemID)
	if !ok {
		systems, err := loadSystems(p.fs)
		if err != nil {
			return nil
		}
		sys, err := systemForPath(systems, path, p.romRoots())
		if err != nil {
			return nil
		}
		if info, ok = lookupSystem(sys.Name); !ok {
			return nil
		}
	}
	systemName := info.SystemID
	if meta, err := assets.GetSystemMetadata(info.SystemID); err == nil {
		systemName = meta.Name
	}
	name := strings.TrimSpace(st.Game)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return models.NewActiveMedia(info.SystemID, systemName, path, name, info.GetLauncherID())
}

// waitForGame waits until ES reports path as started after generation since.
func (p *Platform) waitForGame(ctx context.Context, path string, since uint64, timeout time.Duration) error {
	deadline := p.clock.After(timeout)
	for {
		p.refresh()
		p.stateMu.Lock()
		gen := p.generation
		st := p.observed
		changed := p.changed
		p.stateMu.Unlock()
		if gen > since && st.Action == actionRunGame && strings.EqualFold(filepath.Clean(st.GamePath), path) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for EmulationStation: %w", ctx.Err())
		case <-deadline:
			return errors.New("EmulationStation did not start the game within 20s: " +
				"the ROM may be missing from its game list, or ES may be showing a prompt")
		case <-changed:
		case <-p.wake:
		case <-p.clock.After(statePollEvery):
		}
	}
}

func (p *Platform) stateGeneration() uint64 {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.generation
}

// track follows ES events until ctx ends. MQTT events trigger an immediate
// read; the poll covers a missing or restarting broker.
func (p *Platform) track(ctx context.Context) {
	if p.subscribe != nil {
		go p.subscribe(ctx, p.poke)
	}
	for {
		p.refresh()
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		case <-p.clock.After(statePollEvery):
		}
	}
}

// subscribeESEvents calls onEvent for every EmulationStation event published
// on the local broker until ctx ends. Payloads are not used.
func subscribeESEvents(ctx context.Context, onEvent func()) {
	opts := mqtt.NewClientOptions().
		AddBroker(esBroker).
		SetClientID("zaparoo-core").
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(mqttRetryEvery)
	opts.OnConnect = func(c mqtt.Client) {
		token := c.Subscribe(esEventTopic, 0, func(mqtt.Client, mqtt.Message) { onEvent() })
		if token.WaitTimeout(mqttRetryEvery) && token.Error() != nil {
			log.Debug().Err(token.Error()).Msg("subscribe to EmulationStation events failed")
		}
	}
	client := mqtt.NewClient(opts)
	client.Connect()
	<-ctx.Done()
	client.Disconnect(mqttDisconnectQuiet)
}
