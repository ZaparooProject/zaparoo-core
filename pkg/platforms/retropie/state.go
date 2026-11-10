//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// maxStateSize bounds a state file: five fields, one of them a path.
const maxStateSize = 16 * 1024

// runEvent is what a runcommand hook reported: start or end, the runcommand
// pid, and runcommand's system, emulator and ROM arguments.
type runEvent struct {
	event    string
	system   string
	emulator string
	rom      string
	pid      int
}

func parseState(data []byte) (runEvent, error) {
	if len(data) > maxStateSize {
		return runEvent{}, errors.New("state file too large")
	}
	if len(data) == 0 || data[len(data)-1] != 0 {
		return runEvent{}, errors.New("state file is incomplete")
	}
	fields := strings.Split(string(data[:len(data)-1]), "\x00")
	if len(fields) != 5 {
		return runEvent{}, fmt.Errorf("state file has %d fields, want 5", len(fields))
	}
	ev := runEvent{event: fields[0], system: fields[2], emulator: fields[3], rom: fields[4]}
	if ev.event != eventStart && ev.event != eventEnd {
		return runEvent{}, fmt.Errorf("unknown state event: %q", ev.event)
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil || pid <= 0 {
		return runEvent{}, fmt.Errorf("invalid runcommand pid: %q", fields[1])
	}
	ev.pid = pid
	return ev, nil
}

func (p *Platform) readStateFile() (runEvent, error) {
	f, err := p.fs.Open(filepath.Join(p.stateDir, stateFile))
	if err != nil {
		return runEvent{}, fmt.Errorf("open state file: %w", err)
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(f, maxStateSize+1)); err != nil {
		return runEvent{}, fmt.Errorf("read state file: %w", err)
	}
	return parseState(buf.Bytes())
}

func (p *Platform) loadState() {
	ev, err := p.readStateFile()
	if err != nil {
		log.Debug().Err(err).Msg("retropie: no usable runcommand state")
		return
	}
	p.applyEvent(ev)
}

// applyEvent publishes the game runcommand reported. Only the runcommand
// that started the tracked game can end it, so the end of a game replaced by
// a newer launch never clears the newer game.
func (p *Platform) applyEvent(ev runEvent) {
	switch ev.event {
	case eventStart:
		// A start left over from before a reboot names a process that is
		// gone, or a reused pid that is not runcommand.
		if !p.runcommandAlive(ev.pid) {
			return
		}
		p.mu.Lock()
		p.run = ev
		p.awaitingStart = false
		p.mu.Unlock()
		log.Info().Str("system", ev.system).Str("emulator", ev.emulator).Str("rom", ev.rom).
			Int("pid", ev.pid).Msg("retropie: game started")

		media := mediaForEvent(ev)
		if media == nil || p.setActiveMedia == nil {
			return
		}
		// A launch from Core already published this game with its indexed
		// name, which is better than one made from the file name.
		if p.activeMedia != nil {
			if current := p.activeMedia(); current != nil && current.Path == media.Path {
				return
			}
		}
		p.setActiveMedia(media)
	case eventEnd:
		p.mu.Lock()
		if p.run.pid == 0 || p.run.pid != ev.pid {
			p.mu.Unlock()
			return
		}
		p.run = runEvent{}
		p.mu.Unlock()
		log.Info().Str("rom", ev.rom).Int("pid", ev.pid).Msg("retropie: game ended")
		if p.setActiveMedia != nil {
			p.setActiveMedia(nil)
		}
	}
}

// mediaForEvent builds active media for a runcommand game, or nil when the
// system is not a RetroPie game system, such as a port's own name.
func mediaForEvent(ev runEvent) *models.ActiveMedia {
	info, ok := systemMap[ev.system]
	if !ok || ev.rom == "" {
		return nil
	}
	systemName := info.systemID
	if meta, err := assets.GetSystemMetadata(info.systemID); err == nil {
		systemName = meta.Name
	}
	name := strings.TrimSuffix(filepath.Base(ev.rom), filepath.Ext(ev.rom))
	return models.NewActiveMedia(info.systemID, systemName, ev.rom, name, info.launcherID)
}

// startStateWatcher follows the state file the runcommand hooks write. The
// hooks rename a finished file into place, which arrives as a create.
func (p *Platform) startStateWatcher() (func() error, error) {
	if err := p.fs.MkdirAll(p.stateDir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", p.stateDir, err)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create state watcher: %w", err)
	}
	if err := watcher.Add(p.stateDir); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("watch %s: %w", p.stateDir, err)
	}

	p.loadState()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) == stateFile && event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					p.loadState()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn().Err(err).Msg("retropie: state watcher error")
			}
		}
	}()

	return func() error {
		err := watcher.Close()
		<-done
		if err != nil {
			return fmt.Errorf("close state watcher: %w", err)
		}
		return nil
	}, nil
}
