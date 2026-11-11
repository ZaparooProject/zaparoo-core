//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

const (
	// esCommandAddr is EmulationStation's UDP command listener. It accepts
	// "START|<system>|<rom path>" and does not reply.
	esCommandAddr = "127.0.0.1:1337"
	// maxCommandSize stays under the 2048-byte buffer ES reads into.
	maxCommandSize = 2000

	sendTimeout          = 2 * time.Second
	launchConfirmTimeout = 20 * time.Second
	// startSpacing keeps START commands apart: ES ignores a launch that comes
	// within 2s of the previous one.
	startSpacing = 2100 * time.Millisecond
)

// buildStartCommand formats a START command. ES splits the frame on '|' and
// would cut a field holding one, so such values are refused.
func buildStartCommand(system, rom string) ([]byte, error) {
	for _, v := range []string{system, rom} {
		if v == "" || strings.ContainsAny(v, "|\x00\r\n") {
			return nil, fmt.Errorf("value cannot be sent to EmulationStation: %q", v)
		}
	}
	msg := "START|" + system + "|" + rom
	if len(msg) > maxCommandSize {
		return nil, errors.New("ROM path is too long for EmulationStation")
	}
	return []byte(msg), nil
}

func (p *Platform) sendCommand(ctx context.Context, msg []byte) error {
	dialer := net.Dialer{Timeout: sendTimeout}
	conn, err := dialer.DialContext(ctx, "udp", p.commandAddr)
	if err != nil {
		return fmt.Errorf("connect to EmulationStation: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetWriteDeadline(time.Now().Add(sendTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("send command to EmulationStation: %w", err)
	}
	return nil
}

// launchGame asks EmulationStation to run a ROM from its game list and waits
// until ES reports the game started.
func (p *Platform) launchGame(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("ROM path is not absolute: %s", path)
	}
	path = filepath.Clean(path)
	info, err := p.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("ROM not found: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("ROM path is a directory: %s", path)
	}

	systems, err := loadSystems(p.fs)
	if err != nil {
		return nil, err
	}
	// ES resolves system folders against its own ROM roots only; a match under
	// a Zaparoo index root would send a START that ES ignores.
	sys, err := systemForPath(systems, path, p.romRoots())
	if err != nil {
		return nil, err
	}
	msg, err := buildStartCommand(sys.Name, path)
	if err != nil {
		return nil, err
	}

	ctx := p.ctx()
	since, err := p.sendStart(ctx, msg)
	if err != nil {
		return nil, err
	}
	log.Info().Str("system", sys.Name).Str("path", path).Msg("sent launch to EmulationStation")
	if err := p.waitForGame(ctx, path, since, launchConfirmTimeout); err != nil {
		return nil, err
	}
	return nil, nil //nolint:nilnil // ES owns the game process; the state tracker follows it
}

// sendStart sends a START command no sooner than startSpacing after the last
// one, and returns the state generation to wait past.
func (p *Platform) sendStart(ctx context.Context, msg []byte) (uint64, error) {
	p.startMu.Lock()
	defer p.startMu.Unlock()
	if !p.lastStart.IsZero() {
		if wait := startSpacing - p.clock.Since(p.lastStart); wait > 0 {
			select {
			case <-ctx.Done():
				return 0, fmt.Errorf("wait to launch: %w", ctx.Err())
			case <-p.clock.After(wait):
			}
		}
	}
	since := p.stateGeneration()
	if err := p.sendCommand(ctx, msg); err != nil {
		return 0, err
	}
	p.lastStart = p.clock.Now()
	return since, nil
}
