//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
)

const runtimeStopTimeout = 10 * time.Second

type brokerSession struct {
	conn       *net.UnixConn
	done       chan struct{}
	err        error
	launchID   string
	childPID   int
	runtimePID int
}

type brokerStart struct {
	session *brokerSession
	process *os.Process
	decoder *json.Decoder
}

// Broker coordinates Steam-owned Runtime sessions for Core launches.
type Broker struct {
	launch         func(context.Context, string) error
	active         *brokerSession
	sessions       map[int]*brokerSession
	paths          *InstallPaths
	launchMu       syncutil.Mutex
	mu             syncutil.Mutex
	latestPID      int
	lastRuntimePID int
}

// NewBroker creates a Broker using the installed Runtime shortcut.
func NewBroker() *Broker {
	executor := &command.RealExecutor{}
	return brokerWithLauncher(DefaultInstallPaths(), func(ctx context.Context, url string) error {
		return executor.Start(ctx, "steam", url)
	})
}

func brokerWithLauncher(paths *InstallPaths, launch func(context.Context, string) error) *Broker {
	return &Broker{
		paths: paths, launch: launch, sessions: make(map[int]*brokerSession),
	}
}

func (b *Broker) resolveShortcutID() (uint64, error) {
	status, err := statusWithPaths(b.paths)
	if err != nil {
		return 0, fmt.Errorf("inspect Steam Runtime integration: %w", err)
	}
	if status.State != statusReady {
		return 0, fmt.Errorf("runtime integration is %s", status.State)
	}
	return status.ShortcutIDs[0], nil
}

func startBrokerSession(
	conn *net.UnixConn,
	spec *Command,
	runtimePID int,
) (*brokerStart, error) {
	if err := conn.SetDeadline(time.Now().Add(brokerTimeout)); err != nil {
		return nil, fmt.Errorf("set runtime handshake deadline: %w", err)
	}
	if err := json.NewEncoder(conn).Encode(spec); err != nil {
		return nil, fmt.Errorf("send runtime command: %w", err)
	}
	decoder := json.NewDecoder(io.LimitReader(conn, protocolLimit))
	var started commandResult
	if err := decoder.Decode(&started); err != nil {
		return nil, fmt.Errorf("read runtime start result: %w", err)
	}
	if err := validateResult(&started, spec.LaunchID); err != nil {
		return nil, err
	}
	if started.Phase != phaseStarted || started.PID <= 0 {
		return nil, fmt.Errorf("runtime failed to start: %s", started.Error)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear runtime handshake deadline: %w", err)
	}
	process, err := os.FindProcess(started.PID)
	if err != nil {
		return nil, fmt.Errorf("track runtime child: %w", err)
	}
	session := &brokerSession{
		conn: conn, done: make(chan struct{}), launchID: spec.LaunchID,
		childPID: started.PID, runtimePID: runtimePID,
	}
	return &brokerStart{session: session, process: process, decoder: decoder}, nil
}

func (b *Broker) watchSession(session *brokerSession, decoder *json.Decoder) {
	var exited commandResult
	err := decoder.Decode(&exited)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		err = fmt.Errorf("read runtime exit result: %w", err)
	}
	if err == nil {
		err = validateResult(&exited, session.launchID)
	}
	if err == nil && exited.Phase != phaseExited {
		err = fmt.Errorf("unexpected runtime result: %s", exited.Phase)
	}

	b.completeSession(session, err)
	_ = session.conn.Close()
}

func (b *Broker) completeSession(session *brokerSession, err error) {
	b.mu.Lock()
	session.err = err
	if b.active == session {
		b.active = nil
	}
	if b.lastRuntimePID == session.runtimePID {
		b.lastRuntimePID = 0
	}
	close(session.done)
	b.mu.Unlock()
}

func waitForSteamRelease(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("wait for Steam session release: %w", ctx.Err())
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("wait for runtime process exit: %w", ctx.Err())
		}
	}
}

func (b *Broker) Start(ctx context.Context, spec *Command) (*os.Process, error) {
	prepared, err := prepareCommand(spec)
	if err != nil {
		return nil, err
	}
	b.launchMu.Lock()
	defer b.launchMu.Unlock()
	if stopErr := b.stopActive(ctx); stopErr != nil {
		return nil, fmt.Errorf("preempt active runtime: %w", stopErr)
	}
	b.mu.Lock()
	lastRuntimePID := b.lastRuntimePID
	b.mu.Unlock()
	releaseCtx, releaseCancel := context.WithTimeout(ctx, 5*time.Second)
	releaseErr := waitForSteamRelease(releaseCtx, lastRuntimePID)
	releaseCancel()
	if releaseErr != nil {
		return nil, releaseErr
	}
	shortcutID, err := b.resolveShortcutID()
	if err != nil {
		return nil, err
	}
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	listener, err := listenSocket(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(path)
	}()
	if launchErr := b.launch(ctx, shortcutURL(shortcutID)); launchErr != nil {
		return nil, fmt.Errorf("launch Zaparoo Steam shortcut: %w", launchErr)
	}
	conn, err := acceptRuntime(ctx, listener)
	if err != nil {
		return nil, err
	}
	runtimePID, err := verifySocketPeer(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	started, err := startBrokerSession(conn, prepared, runtimePID)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	b.mu.Lock()
	b.active = started.session
	b.sessions[started.session.childPID] = started.session
	b.latestPID = started.session.childPID
	b.lastRuntimePID = started.session.runtimePID
	b.mu.Unlock()
	go b.watchSession(started.session, started.decoder)
	return started.process, nil
}

func waitSession(ctx context.Context, session *brokerSession) error {
	select {
	case <-session.done:
		return session.err
	case <-ctx.Done():
		return fmt.Errorf("wait for runtime session: %w", ctx.Err())
	}
}

func waitSessionDone(ctx context.Context, session *brokerSession) error {
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for runtime session stop: %w", ctx.Err())
	}
}

func (b *Broker) stopActive(ctx context.Context) error {
	b.mu.Lock()
	session := b.active
	b.mu.Unlock()
	if session == nil {
		return nil
	}
	if err := syscall.Kill(session.runtimePID, syscall.SIGTERM); err != nil &&
		!errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal runtime process: %w", err)
	}
	stopCtx, cancel := context.WithTimeout(ctx, runtimeStopTimeout)
	err := waitSession(stopCtx, session)
	cancel()
	if err == nil {
		return nil
	}
	_ = syscall.Kill(-session.childPID, syscall.SIGKILL)
	_ = syscall.Kill(session.runtimePID, syscall.SIGKILL)
	forceCtx, forceCancel := context.WithTimeout(context.Background(), time.Second)
	defer forceCancel()
	if forceErr := waitSessionDone(forceCtx, session); forceErr != nil {
		return fmt.Errorf("force runtime stop: %w", forceErr)
	}
	return nil
}

func (b *Broker) Stop(ctx context.Context) error {
	b.launchMu.Lock()
	defer b.launchMu.Unlock()
	return b.stopActive(ctx)
}

func (b *Broker) Wait(ctx context.Context, pid int) error {
	b.mu.Lock()
	session := b.sessions[pid]
	b.mu.Unlock()
	if session == nil {
		return fmt.Errorf("runtime process %d is not tracked", pid)
	}
	return waitSession(ctx, session)
}

func (b *Broker) Available() bool {
	_, err := b.resolveShortcutID()
	return err == nil
}

func (b *Broker) HasActive() bool {
	b.mu.Lock()
	active := b.active != nil
	b.mu.Unlock()
	return active
}

func (b *Broker) Owns(pid int) bool {
	b.mu.Lock()
	_, ok := b.sessions[pid]
	b.mu.Unlock()
	return ok
}

// Clear removes the tracked session for pid. It returns true only when pid is
// the latest Runtime launch, allowing callers to decide whether active-media
// cleanup is still required.
func (b *Broker) Clear(pid int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, tracked := b.sessions[pid]; !tracked {
		return false
	}
	delete(b.sessions, pid)
	if pid != b.latestPID {
		return false
	}
	b.latestPID = 0
	return true
}
