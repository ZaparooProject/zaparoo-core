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
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dialSocket(t *testing.T, path string) *net.UnixConn {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	require.NoError(t, err)
	return conn
}

func launchSocket(t *testing.T) string {
	t.Helper()
	path, err := socketPath()
	require.NoError(t, err)
	return path
}

func hostSocket(t *testing.T) string {
	t.Helper()
	path, err := hostSocketPath()
	require.NoError(t, err)
	return path
}

// serveHostLaunch answers one poke the way the frontend does: open a fresh
// connection for the launch and run the single command it is handed. It takes
// no *testing.T because it runs off the test goroutine.
func serveHostLaunch(path string) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(hello{Role: roleHostLaunch, Version: protocolVersion}); err != nil {
		return
	}
	var spec Command
	if err := json.NewDecoder(conn).Decode(&spec); err != nil {
		return
	}
	if err := validateCommand(&spec); err != nil {
		return
	}
	_ = executeCommand(context.Background(), conn, &spec)
}

// registerTestHost stands in for a frontend that Steam already has on screen.
func registerTestHost(t *testing.T) *net.UnixConn {
	t.Helper()
	launchPath := launchSocket(t)
	conn := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleHost, Version: protocolVersion}))
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var frame poke
			if decodeErr := decoder.Decode(&frame); decodeErr != nil {
				return
			}
			go serveHostLaunch(launchPath)
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// waitForHost gives the broker's accept loop a moment to see the registration.
func waitForHost(t *testing.T, broker *Broker) {
	t.Helper()
	require.Eventually(t, broker.hasHost, 2*time.Second, 10*time.Millisecond)
}

func TestBrokerPrefersRegisteredHostOverShortcut(t *testing.T) {
	broker := testBroker(t)
	shortcutLaunches := 0
	broker.launch = func(context.Context, string) error {
		shortcutLaunches++
		return errors.New("the shortcut must not be used while a host is registered")
	}
	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "true"})

	require.NoError(t, err)
	assert.Zero(t, shortcutLaunches)
	assert.True(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
	// A host stays put between launches, so nothing has to die before the next.
	broker.mu.Lock()
	assert.Zero(t, broker.lastRuntimePID)
	broker.mu.Unlock()
}

func TestBrokerFallsBackToShortcutWithoutAHost(t *testing.T) {
	broker := testBroker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	process, err := broker.Start(ctx, &Command{Executable: "true"})

	require.NoError(t, err)
	assert.False(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

func TestBrokerFallsBackWhenTheHostHasGone(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	host := registerTestHost(t)
	waitForHost(t, broker)
	// Killing the connection without unregistering is what a crashed frontend
	// looks like from here.
	require.NoError(t, host.Close())
	require.Eventually(t, func() bool { return !broker.hasHost() }, 2*time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "true"})

	require.NoError(t, err)
	assert.False(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

func TestBrokerAvailableWithAHostAndNoShortcut(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.paths.fileSystem().Remove(broker.paths.Runtime))
	require.False(t, broker.Available())

	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)

	assert.True(t, broker.Available())
}

func TestBrokerStopKillsTheGameAndNotTheHost(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)

	require.NoError(t, broker.Stop(ctx))

	assert.False(t, broker.HasActive())
	// The host is this test process, and it has to still be here.
	require.NoError(t, syscall.Kill(os.Getpid(), 0))
	assert.True(t, broker.hasHost())
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(process.Pid, 0), syscall.ESRCH)
	}, 5*time.Second, 50*time.Millisecond)
}

func TestBrokerRejectsAnUnknownRole(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	conn := dialSocket(t, hostSocket(t))
	defer func() { _ = conn.Close() }()

	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: "wat", Version: protocolVersion}))

	// The broker hangs up rather than registering anything.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err := conn.Read(make([]byte, 1))
	require.Error(t, err)
	assert.False(t, broker.hasHost())
}

// The frontend is a separate repository and nothing builds the two together,
// so the wire format is pinned here against the same literals its own tests
// assert. A renamed field would otherwise only surface on a Steam Deck.
func TestFrontendFramesDecode(t *testing.T) {
	t.Parallel()

	var registration hello
	require.NoError(t, json.Unmarshal([]byte(`{"role":"host","version":1}`), &registration))
	require.NoError(t, validateHello(&registration))
	assert.Equal(t, roleHost, registration.Role)

	var launch hello
	require.NoError(t, json.Unmarshal([]byte(`{"role":"host-launch","version":1}`), &launch))
	require.NoError(t, validateHello(&launch))
	assert.Equal(t, roleHostLaunch, launch.Role)
	assert.True(t, roleFitsSocket(launch.Role, roleLaunch))

	var started commandResult
	require.NoError(t, json.Unmarshal(
		[]byte(`{"phase":"started","error":"","launchId":"abc123","pid":4242,"exitCode":0,"version":1}`),
		&started,
	))
	require.NoError(t, validateResult(&started, "abc123"))
	assert.Equal(t, phaseStarted, started.Phase)
	assert.Equal(t, 4242, started.PID)

	var exited commandResult
	require.NoError(t, json.Unmarshal(
		[]byte(`{"phase":"exited","error":"","launchId":"abc123","pid":0,"exitCode":3,"version":1}`),
		&exited,
	))
	require.NoError(t, validateResult(&exited, "abc123"))
	assert.Equal(t, phaseExited, exited.Phase)
	assert.Equal(t, 3, exited.ExitCode)
}

// And the two frames Core sends, spelled the way the frontend parses them.
func TestCoreFramesEncode(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(poke{Kind: pokeLaunch, Version: protocolVersion})
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"launch","version":1}`, string(encoded))

	spec, err := prepareCommand(&Command{
		Executable: "/usr/bin/flatpak",
		Dir:        "/home/deck",
		LaunchID:   "abc123",
		Args:       []string{"run", "net.retrodeck.retrodeck"},
		Env:        []string{"HOME=/home/deck"},
	})
	require.NoError(t, err)
	encoded, err = json.Marshal(spec)
	require.NoError(t, err)
	assert.JSONEq(t, `{"executable":"/usr/bin/flatpak","dir":"/home/deck","launchId":"abc123",`+
		`"args":["run","net.retrodeck.retrodeck"],"env":["HOME=/home/deck"],"version":1}`, string(encoded))
}

// The two sockets do not accept each other's traffic. This is what keeps a
// frontend with host support from swallowing a launch on a Core without it:
// registrations never touch the launch socket at all.
func TestSocketsRefuseTheOtherRole(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	onLaunch := dialSocket(t, launchSocket(t))
	defer func() { _ = onLaunch.Close() }()
	require.NoError(t, json.NewEncoder(onLaunch).Encode(hello{Role: roleHost, Version: protocolVersion}))
	require.NoError(t, onLaunch.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err := onLaunch.Read(make([]byte, 1))
	require.Error(t, err)
	assert.False(t, broker.hasHost())

	onHost := dialSocket(t, hostSocket(t))
	defer func() { _ = onHost.Close() }()
	require.NoError(t, json.NewEncoder(onHost).Encode(hello{Role: roleLaunch, Version: protocolVersion}))
	require.NoError(t, onHost.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err = onHost.Read(make([]byte, 1))
	require.Error(t, err)
}

// Someone who never runs the frontend can still start the Zaparoo Runtime
// shortcut from their Steam library by hand. The socket used to be absent
// between launches, which made that a silent no-op; it is bound all the time
// now, so the broker has to hang up on a peer nobody asked for and the
// Runtime has to treat that as nothing to do rather than as a failure.
func TestRuntimeShortcutRunByHandDoesNothing(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	require.NoError(t, Run(t.Context()))

	assert.False(t, broker.HasActive())
	assert.False(t, broker.hasHost())
}

// And the launch it was not part of still works afterwards.
func TestShortcutLaunchStillWorksAfterAStrayPeer(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	require.NoError(t, Run(t.Context()))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "true"})

	require.NoError(t, err)
	assert.False(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

// Close puts the runtime directory back the way it found it.
func TestCloseRemovesBothSockets(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	launch, host := launchSocket(t), hostSocket(t)
	require.FileExists(t, launch)
	require.FileExists(t, host)

	broker.Close()

	assert.NoFileExists(t, launch)
	assert.NoFileExists(t, host)
}

// A second frontend takes over the launches without evicting the first. Only
// the newest is poked, but both stay connected and both stay protected.
func TestASecondHostTakesOverWithoutEvictingTheFirst(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	first := registerTestHost(t)
	waitForHost(t, broker)

	registerTestHost(t)
	require.Eventually(t, func() bool {
		return len(broker.HostPIDs()) == 2
	}, 2*time.Second, 10*time.Millisecond)

	// The first stays connected and stays protected; the newest takes the
	// launches. A second frontend must not cost the first its protection.
	assert.False(t, connClosed(first))
	assert.True(t, broker.hasHost())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "true"})
	require.NoError(t, err)
	assert.True(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

// connClosed reports whether the peer hung up on this connection.
func connClosed(conn *net.UnixConn) bool {
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		return true
	}
	_, err := conn.Read(make([]byte, 1))
	return err != nil && !errors.Is(err, os.ErrDeadlineExceeded)
}

// Preempting one hosted game with another kills the first game and leaves the
// host alone, so the frontend survives a user changing their mind.
func TestOneHostedLaunchPreemptsTheNext(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	first, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)
	second, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)

	assert.NotEqual(t, first.Pid, second.Pid)
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(first.Pid, 0), syscall.ESRCH)
	}, 5*time.Second, 50*time.Millisecond, "the preempted game must be gone")
	assert.True(t, broker.hasHost(), "the host outlives the game it was running")
	require.NoError(t, broker.Stop(ctx))
}

// The host vanishing mid-game is not the game ending, but Core has to stop
// waiting on it and must be able to launch again afterwards.
func TestAHostLostMidGameDoesNotWedgeTheBroker(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	host := registerTestHost(t)
	waitForHost(t, broker)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	game, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)
	require.NoError(t, host.Close())
	require.Eventually(t, func() bool { return !broker.hasHost() }, 3*time.Second, 10*time.Millisecond)

	// With no host, the next launch falls back to the shortcut and works.
	next, err := broker.Start(ctx, &Command{Executable: "true"})
	require.NoError(t, err)
	assert.False(t, broker.Hosted(next.Pid))
	require.NoError(t, broker.Wait(ctx, next.Pid))
	_ = syscall.Kill(-game.Pid, syscall.SIGKILL)
}

// HostPID is what the protection predicate keys on, so it has to go away with
// the host. A stale pid would keep protecting whatever reused the number.
func TestHostPIDClearsOnDisconnect(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	host := registerTestHost(t)
	waitForHost(t, broker)

	assert.Equal(t, []int{os.Getpid()}, broker.HostPIDs())

	require.NoError(t, host.Close())
	require.Eventually(t, func() bool {
		return len(broker.HostPIDs()) == 0
	}, 3*time.Second, 10*time.Millisecond)
}

// registerSilentHost registers a host that accepts pokes and does nothing
// with them, standing in for a frontend killed at the moment it was poked.
func registerSilentHost(t *testing.T) {
	t.Helper()
	conn := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleHost, Version: protocolVersion}))
	t.Cleanup(func() { _ = conn.Close() })
}

// A host that is poked and says nothing must not cost the user their launch.
// This is the failure seen on device: the frontend died as it was poked, the
// launch waited out the timeout, and the game never ran even though the
// shortcut was sitting there able to run it.
func TestASilentHostFallsBackToTheShortcut(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerSilentHost(t)
	waitForHost(t, broker)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	started := time.Now()
	process, err := broker.Start(ctx, &Command{Executable: "true"})

	require.NoError(t, err, "the shortcut must pick up what the host dropped")
	assert.False(t, broker.Hosted(process.Pid))
	assert.False(t, broker.hasHost(), "a host that will not answer is forgotten")
	assert.Less(t, time.Since(started), 20*time.Second,
		"the wait on a local host must be short enough to leave time for the shortcut")
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

// A peer arriving in the instant a launch gives up has to be hung up on. Left
// parked in the handover channel its connection leaks, and worse, the peer
// blocks forever waiting for a command that will never be sent.
func TestAPeerArrivingTooLateIsClosed(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	waiter := broker.beginWait()
	late := dialSocket(t, launchSocket(t))
	defer func() { _ = late.Close() }()
	require.NoError(t, json.NewEncoder(late).Encode(hello{Role: roleLaunch, Version: protocolVersion}))
	// Let the accept loop take it before the launch gives up.
	require.Eventually(t, func() bool {
		broker.mu.Lock()
		defer broker.mu.Unlock()
		return len(waiter) == 1
	}, 2*time.Second, 10*time.Millisecond)

	broker.endWait(waiter)

	require.Eventually(t, func() bool { return connClosed(late) }, 2*time.Second, 10*time.Millisecond,
		"the peer must be hung up on, not left waiting on a command")
	broker.mu.Lock()
	assert.Nil(t, broker.waiter)
	broker.mu.Unlock()
}

// Every peer must end up either handed to a launch or hung up on, never parked
// in a channel nobody will read. This hammers the handover and checks that
// invariant; under -race it also covers the locking. It is not a reproducer
// for the give-up race itself, which is a few instructions wide and does not
// arrive on demand: that one is closed by construction, by handing over and
// retiring under the same lock.
func TestNoPeerIsLeftParkedUnderConcurrency(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	path := launchSocket(t)

	const rounds = 60
	conns := make([]*net.UnixConn, 0, rounds)
	for range rounds {
		waiter := broker.beginWait()
		conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		require.NoError(t, err)
		conns = append(conns, conn)
		require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleLaunch, Version: protocolVersion}))
		// No sleep: the point is to land the arrival somewhere unpredictable
		// relative to the give-up.
		broker.endWait(waiter)
	}

	for i, conn := range conns {
		require.Eventuallyf(t, func() bool { return connClosed(conn) }, 5*time.Second, 20*time.Millisecond,
			"peer %d was never handed over or closed", i)
		_ = conn.Close()
	}
	broker.mu.Lock()
	assert.Nil(t, broker.waiter, "no waiter may outlive its launch")
	broker.mu.Unlock()
}

// A host slow enough to be given up on can still answer while the shortcut is
// being waited for. Its connection must be recognised for what it is: recorded
// as the shortcut's, the frontend's own pid ends up in the stop path and the
// next launch terminates it.
func TestALateHostAnswerIsStillRecognisedAsAHost(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	// Stand in for the host arriving after the give-up, by connecting on the
	// launch socket with the host's role while a shortcut launch waits.
	waiter := broker.beginWait()
	defer broker.endWait(waiter)
	late := dialSocket(t, launchSocket(t))
	defer func() { _ = late.Close() }()
	require.NoError(t, json.NewEncoder(late).Encode(hello{Role: roleHostLaunch, Version: protocolVersion}))

	peer, err := awaitLaunchPeer(t.Context(), waiter, 5*time.Second)
	require.NoError(t, err)
	assert.True(t, peer.hosted, "the role on the wire decides, not which branch was waiting")
}

// And the shortcut's own connection is never mistaken for a host's, which is
// what keeps the disposable process disposable.
func TestAShortcutAnswerIsNotHosted(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	waiter := broker.beginWait()
	defer broker.endWait(waiter)
	conn := dialSocket(t, launchSocket(t))
	defer func() { _ = conn.Close() }()
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleLaunch, Version: protocolVersion}))

	peer, err := awaitLaunchPeer(t.Context(), waiter, 5*time.Second)
	require.NoError(t, err)
	assert.False(t, peer.hosted)
}

// A registration still belongs only on the registration socket.
func TestRoleSocketPairing(t *testing.T) {
	t.Parallel()

	assert.True(t, roleFitsSocket(roleHost, roleHost))
	assert.False(t, roleFitsSocket(roleLaunch, roleHost))
	assert.False(t, roleFitsSocket(roleHostLaunch, roleHost))
	assert.True(t, roleFitsSocket(roleLaunch, roleLaunch))
	assert.True(t, roleFitsSocket(roleHostLaunch, roleLaunch))
	assert.False(t, roleFitsSocket(roleHost, roleLaunch))
}

// registerOldStyleHost registers, then answers pokes with the plain launch
// role, which is what a frontend built before host-launch existed sends.
func registerOldStyleHost(t *testing.T) {
	t.Helper()
	launchPath := launchSocket(t)
	conn := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleHost, Version: protocolVersion}))
	go func() {
		decoder := json.NewDecoder(conn)
		for {
			var frame poke
			if decodeErr := decoder.Decode(&frame); decodeErr != nil {
				return
			}
			go serveOldStyleLaunch(launchPath)
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
}

func serveOldStyleLaunch(path string) {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	if err := json.NewEncoder(conn).Encode(hello{Role: roleLaunch, Version: protocolVersion}); err != nil {
		return
	}
	var spec Command
	if err := json.NewDecoder(conn).Decode(&spec); err != nil {
		return
	}
	if err := validateCommand(&spec); err != nil {
		return
	}
	_ = executeCommand(context.Background(), conn, &spec)
}

// Core and the frontend ship separately, so a Core with host-launch will meet
// frontends without it. Such a frontend answers as an ordinary launch peer,
// and taking that at face value would put its own pid in the kill path on the
// next launch, which is the failure this whole change exists to stop.
func TestAnOlderHostIsStillNeverSignalled(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerOldStyleHost(t)
	waitForHost(t, broker)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	game, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)
	// Recorded as a shortcut peer, because that is what it said it was.
	assert.False(t, broker.Hosted(game.Pid))

	// What matters is where the signal goes. Asserted on the target rather
	// than on the outcome: the stand-in host is this test process, which
	// installs its own SIGTERM handler while running a command, so an actual
	// signal to it would prove nothing either way.
	broker.mu.Lock()
	session := broker.active
	broker.mu.Unlock()
	require.NotNil(t, session)
	assert.Equal(t, -session.childPID, broker.stopTarget(session),
		"an older host must still be spared and the game signalled instead")

	require.NoError(t, broker.Stop(ctx))
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(game.Pid, 0), syscall.ESRCH)
	}, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, syscall.Kill(os.Getpid(), 0))
	assert.True(t, broker.hasHost())
}

// The three cases the stop target has to separate.
func TestStopTargetPicksTheGameOverAnyHost(t *testing.T) {
	t.Parallel()

	broker := &Broker{sessions: make(map[int]*brokerSession)}
	hosted := &brokerSession{hosted: true, childPID: 4242, runtimePID: 99}
	assert.Equal(t, -4242, broker.stopTarget(hosted))

	// An ordinary Runtime peer is disposable and is the thing to signal.
	shortcut := &brokerSession{childPID: 4242, runtimePID: 99}
	assert.Equal(t, 99, broker.stopTarget(shortcut))

	// Same session, but that peer turns out to be the registered host.
	broker.hosts = []*peerConn{{pid: 99}}
	assert.Equal(t, -4242, broker.stopTarget(shortcut))
}

// A frontend that answers with a launch ID from someone else's launch is not
// answering this one. Core must reject it rather than track a session it
// cannot match up later.
func TestAMismatchedLaunchIDIsRejected(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	launchPath := launchSocket(t)
	conn := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleHost, Version: protocolVersion}))
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		var frame poke
		if json.NewDecoder(conn).Decode(&frame) != nil {
			return
		}
		reply, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: launchPath, Net: "unix"})
		if err != nil {
			return
		}
		defer func() { _ = reply.Close() }()
		encoder := json.NewEncoder(reply)
		if encoder.Encode(hello{Role: roleHostLaunch, Version: protocolVersion}) != nil {
			return
		}
		var spec Command
		if json.NewDecoder(reply).Decode(&spec) != nil {
			return
		}
		_ = encoder.Encode(commandResult{
			Phase: phaseStarted, LaunchID: "someone-elses", PID: os.Getpid(), Version: protocolVersion,
		})
	}()
	waitForHost(t, broker)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, err := broker.Start(ctx, &Command{Executable: "true"})

	require.Error(t, err)
	assert.False(t, broker.HasActive(), "a rejected answer must not leave a session behind")
}

// A peer reporting the game already over, without ever reporting it started,
// is a broken answer and not a launch.
func TestAnExitWithoutAStartIsRejected(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	launchPath := launchSocket(t)
	conn := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(conn).Encode(hello{Role: roleHost, Version: protocolVersion}))
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		var frame poke
		if json.NewDecoder(conn).Decode(&frame) != nil {
			return
		}
		reply, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: launchPath, Net: "unix"})
		if err != nil {
			return
		}
		defer func() { _ = reply.Close() }()
		encoder := json.NewEncoder(reply)
		if encoder.Encode(hello{Role: roleHostLaunch, Version: protocolVersion}) != nil {
			return
		}
		var spec Command
		if json.NewDecoder(reply).Decode(&spec) != nil {
			return
		}
		_ = encoder.Encode(commandResult{
			Phase: phaseExited, LaunchID: spec.LaunchID, Version: protocolVersion,
		})
	}()
	waitForHost(t, broker)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, err := broker.Start(ctx, &Command{Executable: "true"})

	require.Error(t, err)
	assert.False(t, broker.HasActive())
}

// Both frontends stay protected while both are connected, and the protection
// only lifts for the one that actually goes away.
func TestProtectionCoversEveryConnectedHost(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	first := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(first).Encode(hello{Role: roleHost, Version: protocolVersion}))
	t.Cleanup(func() { _ = first.Close() })
	second := dialSocket(t, hostSocket(t))
	require.NoError(t, json.NewEncoder(second).Encode(hello{Role: roleHost, Version: protocolVersion}))
	t.Cleanup(func() { _ = second.Close() })
	require.Eventually(t, func() bool { return len(broker.HostPIDs()) == 2 }, 2*time.Second, 10*time.Millisecond)

	// Both are this process in-test, so assert on the count rather than on
	// distinct pids, then watch it drop as one leaves.
	assert.True(t, broker.isRegisteredHost(os.Getpid()))
	require.NoError(t, second.Close())
	require.Eventually(t, func() bool { return len(broker.HostPIDs()) == 1 }, 3*time.Second, 10*time.Millisecond)
	assert.True(t, broker.isRegisteredHost(os.Getpid()), "the one still connected keeps its protection")

	require.NoError(t, first.Close())
	require.Eventually(t, func() bool { return len(broker.HostPIDs()) == 0 }, 3*time.Second, 10*time.Millisecond)
	assert.False(t, broker.isRegisteredHost(os.Getpid()))
}

// The listener outlives launches, so the accept loop has to tell "we are shut
// down" apart from "that one went wrong". Treating every error as fatal would
// let one bad moment, running out of file descriptors being the realistic one,
// disable launching until Core restarts.
func TestAcceptErrorsOnlyEndTheLoopWhenTheListenerIsClosed(t *testing.T) {
	t.Parallel()

	assert.True(t, acceptIsFatal(net.ErrClosed))
	assert.True(t, acceptIsFatal(fmt.Errorf("accept: %w", net.ErrClosed)))
	assert.False(t, acceptIsFatal(syscall.EMFILE))
	assert.False(t, acceptIsFatal(syscall.ECONNABORTED))
	assert.False(t, acceptIsFatal(errors.New("something else")))
}

// A failed host bind leaves the launch listener usable. A later Serve retries
// only the missing host listener so host support can recover without restart.
func TestServeRetriesHostListenerAfterPartialBind(t *testing.T) {
	broker := testBroker(t)
	t.Cleanup(broker.Close)

	hostPath := hostSocket(t)
	require.NoError(t, os.Mkdir(hostPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hostPath, "block"), []byte("block"), 0o600))

	require.NoError(t, broker.Serve())
	broker.mu.Lock()
	launchListener := broker.listener
	hostListener := broker.hostListener
	broker.mu.Unlock()
	require.NotNil(t, launchListener)
	assert.Nil(t, hostListener)

	require.NoError(t, os.RemoveAll(hostPath))
	require.NoError(t, broker.Serve())
	broker.mu.Lock()
	currentLaunchListener := broker.listener
	hostListener = broker.hostListener
	broker.mu.Unlock()
	assert.Same(t, launchListener, currentLaunchListener)
	require.NotNil(t, hostListener)

	registerTestHost(t)
	waitForHost(t, broker)
}

// Closing and serving again has to work, which is the observable half of the
// same thing: the loops end cleanly on close rather than wedging.
func TestServeWorksAgainAfterClose(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)

	broker.Close()
	assert.Empty(t, broker.HostPIDs())

	require.NoError(t, broker.Serve())
	registerTestHost(t)
	waitForHost(t, broker)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	process, err := broker.Start(ctx, &Command{Executable: "true"})
	require.NoError(t, err)
	assert.True(t, broker.Hosted(process.Pid))
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

// A frontend that registers while a silent host is being waited on must not be
// the one hung up on. Dropping "whichever is newest" would punish the newcomer
// and leave the host that is actually not answering in place.
func TestOnlyThePokedHostIsDropped(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())
	registerSilentHost(t)
	waitForHost(t, broker)

	// The launch runs off the test goroutine so the second host can register
	// from it while the silent one is still being waited on; the assertions
	// all stay here.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	type launched struct {
		process *os.Process
		err     error
	}
	done := make(chan launched, 1)
	go func() {
		process, err := broker.Start(ctx, &Command{Executable: "true"})
		done <- launched{process, err}
	}()

	time.Sleep(500 * time.Millisecond)
	registerTestHost(t)
	result := <-done
	require.NoError(t, result.err)
	process := result.process

	// One host was dropped, and the survivor is the one that just joined, so
	// the next launch has somewhere to go.
	assert.Len(t, broker.HostPIDs(), 1, "exactly the silent host is gone")
	assert.True(t, broker.hasHost())
	require.NoError(t, broker.Wait(ctx, process.Pid))
}

// The registration hook is how Core revisits a Steam game it already decided
// about before it knew the process was its own frontend. It has to fire for
// every registration, including the first, which is the one that arrives the
// instant the socket opens.
func TestTheRegistrationHookFiresForEveryHost(t *testing.T) {
	broker := testBroker(t)
	fired := make(chan int, 4)
	broker.SetHostRegisteredHook(func(pid int) { fired <- pid })
	require.NoError(t, broker.Serve())

	registerTestHost(t)
	select {
	case pid := <-fired:
		assert.Equal(t, os.Getpid(), pid)
	case <-time.After(3 * time.Second):
		require.Fail(t, "the hook never fired for the first registration")
	}

	// And again for a second frontend, which is a second thing to reconsider.
	registerTestHost(t)
	select {
	case pid := <-fired:
		assert.Equal(t, os.Getpid(), pid)
	case <-time.After(3 * time.Second):
		require.Fail(t, "the hook never fired for the second registration")
	}
}

// A broker with no hook installed must not fall over when a host arrives.
func TestRegistrationWithoutAHookIsFine(t *testing.T) {
	broker := testBroker(t)
	require.NoError(t, broker.Serve())

	registerTestHost(t)
	waitForHost(t, broker)

	assert.Len(t, broker.HostPIDs(), 1)
}
