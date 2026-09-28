//go:build linux && !android

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
	"github.com/rs/zerolog/log"
)

const runtimeStopTimeout = 10 * time.Second

// acceptRetryDelay is the pause after an accept failure that is not the
// listener closing, so a persistent one cannot become a hot loop.
const acceptRetryDelay = 100 * time.Millisecond

// peerConn is one accepted connection plus the decoder that has already read
// its hello frame. The decoder travels with the connection because a fresh one
// could drop bytes the first read buffered.
type peerConn struct {
	conn    *net.UnixConn
	decoder *json.Decoder
	pid     int
	// hosted is read off the hello rather than inferred from which branch
	// asked for a peer, because a slow host can answer after its launch was
	// given up on and the shortcut was started instead.
	hosted bool
}

type brokerSession struct {
	peer       *peerConn
	done       chan struct{}
	err        error
	launchID   string
	childPID   int
	runtimePID int
	// hosted marks a session owned by a standing host rather than by a
	// Runtime process Steam started for it. The distinction matters when
	// stopping: the one-shot Runtime is the thing to signal, a host is a
	// long-lived process that must survive the game it is running.
	hosted bool
}

type brokerStart struct {
	session *brokerSession
	process *os.Process
}

// Broker coordinates Steam-owned Runtime sessions for Core launches.
type Broker struct {
	launch       func(context.Context, string) error
	active       *brokerSession
	sessions     map[int]*brokerSession
	paths        *InstallPaths
	listener     *net.UnixListener
	hostListener *net.UnixListener
	// hostRegistered runs after a host joins, so Core can revisit anything it
	// decided about that process before it knew what it was. Set once at
	// startup, before any peer can connect.
	hostRegistered func(pid int)
	waiter         chan *peerConn
	socket         string
	hostSocket     string
	hosts          []*peerConn
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

func startBrokerSession(peer *peerConn, spec *Command) (*brokerStart, error) {
	if err := peer.conn.SetDeadline(time.Now().Add(brokerTimeout)); err != nil {
		return nil, fmt.Errorf("set runtime handshake deadline: %w", err)
	}
	if err := json.NewEncoder(peer.conn).Encode(spec); err != nil {
		return nil, fmt.Errorf("send runtime command: %w", err)
	}
	var started commandResult
	if err := peer.decoder.Decode(&started); err != nil {
		return nil, fmt.Errorf("read runtime start result: %w", err)
	}
	if err := validateResult(&started, spec.LaunchID); err != nil {
		return nil, err
	}
	if started.Phase != phaseStarted || started.PID <= 0 {
		return nil, fmt.Errorf("runtime failed to start: %s", started.Error)
	}
	if err := peer.conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear runtime handshake deadline: %w", err)
	}
	process, err := os.FindProcess(started.PID)
	if err != nil {
		return nil, fmt.Errorf("track runtime child: %w", err)
	}
	session := &brokerSession{
		peer: peer, done: make(chan struct{}), launchID: spec.LaunchID,
		childPID: started.PID, runtimePID: peer.pid, hosted: peer.hosted,
	}
	return &brokerStart{session: session, process: process}, nil
}

func (b *Broker) watchSession(session *brokerSession) {
	var exited commandResult
	err := session.peer.decoder.Decode(&exited)
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
	_ = session.peer.conn.Close()
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

// SetHostRegisteredHook installs a callback run when a host registers. Call it
// before Serve: it is not guarded, because a host cannot connect until the
// socket is listening.
func (b *Broker) SetHostRegisteredHook(hook func(pid int)) {
	b.hostRegistered = hook
}

// Serve binds the launch socket and starts accepting peers. Core calls it at
// startup so a host can register before the first launch. Start calls it too,
// so a broker that was never served behaves exactly as it did before hosts
// existed.
func (b *Broker) Serve() error {
	return b.ensureListener()
}

func (b *Broker) ensureListener() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener != nil && b.hostListener != nil {
		return nil
	}
	if b.listener == nil {
		path, err := socketPath()
		if err != nil {
			return err
		}
		listener, err := listenSocket(path)
		if err != nil {
			return err
		}
		b.listener = listener
		b.socket = path
		go b.acceptLoop(listener, roleLaunch)
	}
	if b.hostListener != nil {
		return nil
	}
	// A host registering is not urgent enough to fail a launch over, so a
	// second socket that will not bind is logged and left alone.
	hostPath, err := hostSocketPath()
	if err != nil {
		log.Warn().Err(err).Msg("no path for the launch host socket")
		return nil
	}
	hostListener, err := listenSocket(hostPath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to listen for launch hosts")
		return nil
	}
	b.hostListener = hostListener
	b.hostSocket = hostPath
	go b.acceptLoop(hostListener, roleHost)
	return nil
}

// Close stops accepting peers and removes the socket. The socket outlives an
// individual launch now, because a host has to be able to find it between
// launches.
func (b *Broker) Close() {
	b.mu.Lock()
	listeners := []*net.UnixListener{b.listener, b.hostListener}
	sockets := []string{b.socket, b.hostSocket}
	hosts := b.hosts
	b.listener, b.hostListener, b.hosts = nil, nil, nil
	b.socket, b.hostSocket = "", ""
	b.mu.Unlock()
	for _, host := range hosts {
		_ = host.conn.Close()
	}
	for _, listener := range listeners {
		if listener != nil {
			_ = listener.Close()
		}
	}
	for _, socket := range sockets {
		if socket != "" {
			_ = os.Remove(socket)
		}
	}
}

// acceptLoop serves one socket, which accepts exactly one role: a launch
// connection carries a command, a registration does not, and neither socket
// entertains the other's traffic.
func (b *Broker) acceptLoop(listener *net.UnixListener, role string) {
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if acceptIsFatal(err) {
				return
			}
			// A transient accept failure used to cost one launch, because the
			// listener only existed for that launch. It outlives launches now,
			// so giving up here would disable every future launch until Core
			// restarts. Running out of file descriptors is the realistic way
			// in. Pause so a persistent failure cannot spin, and carry on.
			log.Warn().Err(err).Str("socket", role).Msg("Steam Runtime accept failed")
			time.Sleep(acceptRetryDelay)
			continue
		}
		go b.greet(conn, role)
	}
}

// acceptIsFatal reports whether an accept error means the listener is done
// rather than merely having had a bad moment.
func acceptIsFatal(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

// greet reads the hello frame and routes the connection: a host is registered
// until it disconnects, anything else is offered to the launch that is waiting
// for it.
func (b *Broker) greet(conn *net.UnixConn, role string) {
	pid, err := verifySocketPeer(conn)
	if err != nil {
		log.Warn().Err(err).Msg("rejected Steam Runtime peer")
		_ = conn.Close()
		return
	}
	if err := conn.SetDeadline(time.Now().Add(helloTimeout)); err != nil {
		_ = conn.Close()
		return
	}
	peer := &peerConn{conn: conn, decoder: json.NewDecoder(io.LimitReader(conn, protocolLimit)), pid: pid}
	var frame hello
	if err := peer.decoder.Decode(&frame); err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("failed to read Steam Runtime hello")
		_ = conn.Close()
		return
	}
	if err := validateHello(&frame); err != nil {
		log.Warn().Err(err).Int("pid", pid).Msg("rejected Steam Runtime peer")
		_ = conn.Close()
		return
	}
	if !roleFitsSocket(frame.Role, role) {
		log.Warn().Int("pid", pid).Str("role", frame.Role).Str("socket", role).
			Msg("Steam Runtime peer used the wrong socket for its role")
		_ = conn.Close()
		return
	}
	peer.hosted = frame.Role == roleHostLaunch
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return
	}
	if role == roleHost {
		b.registerHost(peer)
		return
	}
	b.offerLaunchPeer(peer)
}

// roleFitsSocket keeps registrations and launches on their own sockets. Both
// launch roles belong on the launch socket; the registration socket takes only
// registrations.
func roleFitsSocket(role, socket string) bool {
	if socket == roleHost {
		return role == roleHost
	}
	return role == roleLaunch || role == roleHostLaunch
}

// maxHosts bounds the registration list. One frontend is the normal case and
// two is a user who started it twice; anything beyond that is a client
// misbehaving, and the oldest is hung up on rather than letting the list grow
// without limit.
const maxHosts = 8

// registerHost adds a host without evicting the ones already connected.
//
// Only the newest owns launches, but every connected host stays protected from
// being terminated. A second frontend must not cost the first its protection:
// it is still a live frontend, and the next launch would otherwise preempt the
// "game" its Steam app looks like and kill it.
func (b *Broker) registerHost(peer *peerConn) {
	b.mu.Lock()
	b.hosts = append(b.hosts, peer)
	var evicted *peerConn
	if len(b.hosts) > maxHosts {
		evicted = b.hosts[0]
		b.hosts = b.hosts[1:]
	}
	count := len(b.hosts)
	b.mu.Unlock()
	if evicted != nil {
		log.Warn().Int("pid", evicted.pid).Msg("too many Steam launch hosts; dropping the oldest")
		_ = evicted.conn.Close()
	}
	log.Info().Int("pid", peer.pid).Int("hosts", count).Msg("Steam launch host registered")
	if hook := b.hostRegistered; hook != nil {
		hook(peer.pid)
	}
	go b.watchHost(peer)
}

// watchHost drops the registration the moment the host goes away. A host only
// ever speaks on the connection it opens in answer to a poke, so any frame
// arriving on the registration connection is a protocol violation and is
// treated the same as a disconnect.
func (b *Broker) watchHost(peer *peerConn) {
	var frame json.RawMessage
	if err := peer.decoder.Decode(&frame); err == nil {
		log.Warn().Int("pid", peer.pid).Msg("unexpected frame from Steam launch host")
	}
	b.dropHost(peer)
	log.Info().Int("pid", peer.pid).Msg("Steam launch host disconnected")
}

func (b *Broker) dropHost(peer *peerConn) {
	b.mu.Lock()
	b.removeHostLocked(peer)
	b.mu.Unlock()
	_ = peer.conn.Close()
}

func (b *Broker) removeHostLocked(peer *peerConn) {
	for i, candidate := range b.hosts {
		if candidate == peer {
			b.hosts = append(b.hosts[:i], b.hosts[i+1:]...)
			return
		}
	}
}

// currentHost is the newest registration, the one launches are offered to.
func (b *Broker) currentHost() *peerConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.hosts) == 0 {
		return nil
	}
	return b.hosts[len(b.hosts)-1]
}

// HostPIDs are the process ids of every connected launch host. Core needs
// them to know which Steam games are really its own frontend: such a process
// is a launch host, never media, and must never be the thing a launch
// preempts.
func (b *Broker) HostPIDs() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	pids := make([]int, 0, len(b.hosts))
	for _, peer := range b.hosts {
		pids = append(pids, peer.pid)
	}
	return pids
}

// stopTarget names what to signal to end a session: the game's process group
// for a hosted launch, the one-shot Runtime process otherwise.
//
// Signalling a hosted session means signalling the game and never the host,
// which is a long-lived process that has to outlive the game it was asked to
// run. The registered host is checked as well as the session's own record
// because Core and the frontend ship separately: a frontend older than the
// host-launch role answers as an ordinary launch peer, and taking that at
// face value would put its pid in the kill path, which is the exact failure
// this whole change exists to stop.
func (b *Broker) stopTarget(session *brokerSession) int {
	if session.hosted || b.isRegisteredHost(session.runtimePID) {
		return -session.childPID
	}
	return session.runtimePID
}

// isRegisteredHost reports whether pid belongs to any connected host, not just
// the one that owns launches.
func (b *Broker) isRegisteredHost(pid int) bool {
	if pid <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, peer := range b.hosts {
		if peer.pid == pid {
			return true
		}
	}
	return false
}

func (b *Broker) hasHost() bool {
	b.mu.Lock()
	registered := len(b.hosts) > 0
	b.mu.Unlock()
	return registered
}

// pokeHost asks the registered host to open a launch connection. A write
// failure means the host is gone, so the registration is dropped and the
// caller falls back to the Runtime shortcut.
// pokeHost asks the newest host to open a launch connection, returning the one
// it asked so the caller can drop that exact peer if it never answers. Dropping
// "whichever is newest" instead would hang up on an innocent frontend that
// registered during the wait and leave the silent one in place.
func (b *Broker) pokeHost() *peerConn {
	peer := b.currentHost()
	if peer == nil {
		return nil
	}
	if err := peer.conn.SetWriteDeadline(time.Now().Add(helloTimeout)); err != nil {
		b.dropHost(peer)
		return nil
	}
	err := json.NewEncoder(peer.conn).Encode(poke{Kind: pokeLaunch, Version: protocolVersion})
	_ = peer.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		log.Warn().Err(err).Int("pid", peer.pid).Msg("failed to poke Steam launch host")
		b.dropHost(peer)
		return nil
	}
	return peer
}

// beginWait claims the broker as the one launch expecting a peer. Handing
// peers over by rendezvous rather than parking them matters for the Runtime
// shortcut: a user can start it from their Steam library at any time, and a
// parked connection would both hang that process until its deadline and be
// mistaken later for the answer to a real launch.
func (b *Broker) beginWait() chan *peerConn {
	waiter := make(chan *peerConn, 1)
	b.mu.Lock()
	b.waiter = waiter
	b.mu.Unlock()
	return waiter
}

// endWait retires one waiter. Handing a peer over and retiring the waiter both
// happen under the same lock, because a peer arriving exactly as the launch
// gives up must be either taken or closed. Parked in a channel nobody will
// read, its connection leaks and the peer blocks forever on a command that is
// never sent.
func (b *Broker) endWait(waiter chan *peerConn) {
	b.mu.Lock()
	if b.waiter == waiter {
		b.waiter = nil
	}
	var stray *peerConn
	select {
	case stray = <-waiter:
	default:
	}
	b.mu.Unlock()
	if stray != nil {
		_ = stray.conn.Close()
	}
}

func (b *Broker) offerLaunchPeer(peer *peerConn) {
	b.mu.Lock()
	handed := false
	if b.waiter != nil {
		select {
		case b.waiter <- peer:
			handed = true
		default:
		}
	}
	b.mu.Unlock()
	if handed {
		return
	}
	log.Debug().Int("pid", peer.pid).Msg("no launch is waiting; hanging up on the peer")
	_ = peer.conn.Close()
}

func awaitLaunchPeer(
	ctx context.Context,
	waiter chan *peerConn,
	wait time.Duration,
) (*peerConn, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case peer := <-waiter:
		return peer, nil
	case <-timer.C:
		return nil, errors.New("timed out waiting for a Steam launch peer")
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for Steam launch peer: %w", ctx.Err())
	}
}

// claimPeer produces the connection that will run this launch, preferring a
// registered host because it is already a Steam-owned session and spares the
// user a second Steam app appearing for the same game.
//
// A host that is poked and then says nothing is treated as gone rather than as
// a failed launch. That case is not hypothetical: a frontend killed at the
// moment of the poke left the launch waiting out the full timeout and then
// failing, when the shortcut sitting right there would have run the game.
func (b *Broker) claimPeer(ctx context.Context) (*peerConn, error) {
	if b.hasHost() {
		waiter := b.beginWait()
		peer, poked, err := b.claimFromHost(ctx, waiter)
		b.endWait(waiter)
		if peer != nil {
			// Which peer ran a launch is otherwise invisible: both end up as
			// a Steam-owned session, and the only outward difference is how
			// many Steam apps the user sees.
			log.Info().Int("pid", peer.pid).Msg("launch hosted by the running frontend")
			return peer, nil
		}
		if err != nil && poked != nil {
			log.Warn().Err(err).Int("pid", poked.pid).
				Msg("launch host did not answer; using the Runtime shortcut")
			b.dropHost(poked)
		}
	}
	waiter := b.beginWait()
	defer b.endWait(waiter)
	shortcutID, err := b.resolveShortcutID()
	if err != nil {
		return nil, err
	}
	log.Info().Msg("launch handed to the Zaparoo Runtime shortcut")
	if launchErr := b.launch(ctx, shortcutURL(shortcutID)); launchErr != nil {
		return nil, fmt.Errorf("launch Zaparoo Steam shortcut: %w", launchErr)
	}
	return awaitLaunchPeer(ctx, waiter, brokerTimeout)
}

// claimFromHost pokes the registered host and waits for it to dial back. The
// wait is short: the host is a local process that only has to open a socket,
// so anything slower than this is a host that is not coming, and every second
// spent here is a second before the shortcut gets its turn.
func (b *Broker) claimFromHost(
	ctx context.Context,
	waiter chan *peerConn,
) (answered, poked *peerConn, err error) {
	poked = b.pokeHost()
	if poked == nil {
		return nil, nil, nil
	}
	answered, err = awaitLaunchPeer(ctx, waiter, hostTimeout)
	if err != nil {
		return nil, poked, err
	}
	return answered, poked, nil
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
	if listenErr := b.ensureListener(); listenErr != nil {
		return nil, listenErr
	}
	peer, err := b.claimPeer(ctx)
	if err != nil {
		return nil, err
	}
	started, err := startBrokerSession(peer, prepared)
	if err != nil {
		_ = peer.conn.Close()
		return nil, err
	}
	b.mu.Lock()
	b.active = started.session
	b.sessions[started.session.childPID] = started.session
	b.latestPID = started.session.childPID
	// Only a one-shot Runtime has to die before Steam will start the next
	// one. A host stays put between launches, so there is nothing to wait for.
	if peer.hosted {
		b.lastRuntimePID = 0
	} else {
		b.lastRuntimePID = started.session.runtimePID
	}
	b.mu.Unlock()
	go b.watchSession(started.session)
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
	target := b.stopTarget(session)
	if err := syscall.Kill(target, syscall.SIGTERM); err != nil &&
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
	if target != -session.childPID {
		_ = syscall.Kill(session.runtimePID, syscall.SIGKILL)
	}
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
	// A registered host can own a launch without the shortcut existing at all.
	if b.hasHost() {
		return true
	}
	_, err := b.resolveShortcutID()
	return err == nil
}

// Hosted reports whether pid belongs to a launch a standing host owns rather
// than one Steam started from the Runtime shortcut. Steam sets the compositor
// properties for the shortcut's window itself; a hosted window is Core's to
// focus, the same as a direct launch.
func (b *Broker) Hosted(pid int) bool {
	b.mu.Lock()
	session, tracked := b.sessions[pid]
	b.mu.Unlock()
	return tracked && session.hosted
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
