//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"golang.org/x/sys/unix"
)

const (
	socketName      = "steam-runtime.sock"
	hostSocketName  = "steam-launch-host.sock"
	phaseStarted    = "started"
	phaseExited     = "exited"
	phaseError      = "error"
	roleLaunch      = "launch"
	roleHostLaunch  = "host-launch"
	roleHost        = "host"
	pokeLaunch      = "launch"
	protocolVersion = 1
	protocolLimit   = 64 * 1024
	brokerTimeout   = 30 * time.Second
	// A registered host only has to open a socket, so it answers in
	// milliseconds or it is gone. Steam starting the shortcut is the slow
	// case and keeps the full brokerTimeout.
	hostTimeout             = 5 * time.Second
	helloTimeout            = 10 * time.Second
	runtimeCommandWaitDelay = 2 * time.Second
)

// hello is the first frame a peer sends after connecting, naming what the
// connection is for.
//
// roleLaunch carries exactly one command: the broker sends it, the peer runs
// it and reports the two phases, and the connection is done. The process Steam
// starts from the Runtime shortcut opens one of these and nothing else.
// roleHostLaunch is the same exchange opened by a standing host instead. It is
// a separate role rather than bookkeeping on the broker's side because the two
// can arrive out of order: a slow host whose launch was already given up on
// can still turn up while the shortcut is being waited for. Reading which peer
// answered off the connection itself is the only account that cannot get out
// of step, and it decides who is signalled when the launch is stopped. The
// shortcut process is disposable; a host is the frontend and must survive the
// game it was asked to run.
//
// roleHost is a standing registration from a process that is already inside a
// Steam session of its own and will open a roleHostLaunch connection whenever
// it is poked. The frontend registers as a host when Steam started it, which
// makes the Runtime shortcut redundant for as long as the frontend is on
// screen: no second Steam app has to appear to own the emulator.
type hello struct {
	Role    string `json:"role"`
	Version int    `json:"version"`
}

// poke asks a registered host to open a launch connection. It deliberately
// carries no command. The command travels on the connection the host opens in
// answer, so from that point a hosted launch and a shortcut launch are the
// same exchange.
type poke struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

func validateHello(frame *hello) error {
	if frame.Version != protocolVersion {
		return fmt.Errorf("unsupported runtime protocol version: %d", frame.Version)
	}
	switch frame.Role {
	case roleLaunch, roleHostLaunch, roleHost:
		return nil
	default:
		return fmt.Errorf("unknown runtime peer role: %q", frame.Role)
	}
}

// Command describes one emulator process for Steam Runtime to own.
type Command struct {
	Executable string   `json:"executable"`
	Dir        string   `json:"dir,omitempty"`
	LaunchID   string   `json:"launchId"`
	Args       []string `json:"args,omitempty"`
	Env        []string `json:"env,omitempty"`
	Version    int      `json:"version"`
}

type commandResult struct {
	Phase    string `json:"phase"`
	Error    string `json:"error,omitempty"`
	LaunchID string `json:"launchId"`
	PID      int    `json:"pid,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Version  int    `json:"version"`
}

func newLaunchID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate runtime launch ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func prepareCommand(spec *Command) (*Command, error) {
	if spec == nil || strings.TrimSpace(spec.Executable) == "" {
		return nil, errors.New("runtime command executable is required")
	}
	prepared := *spec
	prepared.Args = append([]string(nil), spec.Args...)
	prepared.Env = append([]string(nil), spec.Env...)
	prepared.Version = protocolVersion
	if prepared.LaunchID == "" {
		launchID, err := newLaunchID()
		if err != nil {
			return nil, err
		}
		prepared.LaunchID = launchID
	}
	encoded, err := json.Marshal(&prepared)
	if err != nil {
		return nil, fmt.Errorf("encode runtime command: %w", err)
	}
	if len(encoded) > protocolLimit {
		return nil, fmt.Errorf("runtime command exceeds %d-byte limit", protocolLimit)
	}
	return &prepared, nil
}

func validateCommand(spec *Command) error {
	if spec.Version != protocolVersion {
		return fmt.Errorf("unsupported runtime protocol version: %d", spec.Version)
	}
	if spec.LaunchID == "" {
		return errors.New("runtime launch ID is required")
	}
	if strings.TrimSpace(spec.Executable) == "" {
		return errors.New("runtime command executable is required")
	}
	return nil
}

func newCommandResult(spec *Command, phase string) commandResult {
	return commandResult{
		Phase: phase, LaunchID: spec.LaunchID, Version: protocolVersion,
	}
}

func validateResult(result *commandResult, launchID string) error {
	if result.Version != protocolVersion {
		return fmt.Errorf("unsupported runtime result version: %d", result.Version)
	}
	if result.LaunchID != launchID {
		return errors.New("runtime result launch ID mismatch")
	}
	return nil
}

// IsInvocation reports whether this process was started through the Runtime symlink.
func IsInvocation(path string) bool {
	return filepath.Base(path) == runtimeExecutableName
}

func runtimeDirectory() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "zaparoo-runtime-"+strconv.Itoa(os.Getuid()))
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("XDG_RUNTIME_DIR must be absolute")
	}
	dir = filepath.Join(dir, "zaparoo")
	//nolint:gosec // XDG_RUNTIME_DIR is current user's private runtime directory; child name is fixed.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create Steam Runtime directory: %w", err)
	}
	// A private parent removes any access window before the socket itself is
	// tightened to 0600, without changing the process-wide umask.
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // Runtime directory must be private and traversable.
		return "", fmt.Errorf("secure Steam Runtime directory: %w", err)
	}
	return dir, nil
}

func socketPath() (string, error) {
	dir, err := runtimeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, socketName), nil
}

// hostSocketPath is deliberately not the launch socket. A Core without host
// support binds the launch socket only for the seconds a launch is in flight,
// and a frontend retrying a registration against it would be handed that
// launch's command and swallow it. A separate path means an older Core simply
// has nothing to connect to.
func hostSocketPath() (string, error) {
	dir, err := runtimeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hostSocketName), nil
}

func socketPeerCredentials(conn *net.UnixConn) (*unix.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("access Unix socket: %w", err)
	}
	var (
		credentials *unix.Ucred
		controlErr  error
	)
	if err := raw.Control(func(fd uintptr) {
		//nolint:gosec // Kernel-provided file descriptor is representable as int on Linux.
		credentials, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return nil, fmt.Errorf("inspect Unix socket: %w", err)
	}
	if controlErr != nil {
		return nil, fmt.Errorf("inspect Unix peer credentials: %w", controlErr)
	}
	if credentials == nil {
		return nil, errors.New("unix peer credentials are unavailable")
	}
	return credentials, nil
}

func verifySocketPeer(conn *net.UnixConn) (int, error) {
	credentials, err := socketPeerCredentials(conn)
	if err != nil {
		return 0, err
	}
	//nolint:gosec // Linux UIDs are unsigned 32-bit values exposed through os.Getuid as int.
	if credentials.Uid != uint32(os.Getuid()) {
		return 0, fmt.Errorf("runtime peer UID %d does not match current UID", credentials.Uid)
	}
	peerPID := int(credentials.Pid)
	if peerPID <= 0 {
		return 0, errors.New("runtime peer process ID is invalid")
	}
	return peerPID, nil
}

func listenSocket(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale Steam Runtime socket: %w", err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen for Steam Runtime: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("secure Steam Runtime socket: %w", err)
	}
	return listener, nil
}

func sendResult(encoder *json.Encoder, result commandResult) error {
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("send Steam Runtime result: %w", err)
	}
	return nil
}

func commandEnvironment(overrides []string) []string {
	return helpers.MergeEnviron(os.Environ(), overrides)
}

func childExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func executeCommand(ctx context.Context, conn *net.UnixConn, spec *Command) error {
	executable, err := exec.LookPath(spec.Executable)
	if err != nil {
		result := newCommandResult(spec, phaseError)
		result.Error = err.Error()
		_ = sendResult(json.NewEncoder(conn), result)
		return fmt.Errorf("find runtime executable: %w", err)
	}
	//nolint:gosec // Core supplies an explicit executable and argv; no shell is involved.
	cmd := exec.CommandContext(ctx, executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = commandEnvironment(spec.Env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err != nil {
			return fmt.Errorf("signal runtime command group: %w", err)
		}
		return nil
	}
	cmd.WaitDelay = runtimeCommandWaitDelay

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	encoder := json.NewEncoder(conn)
	if err := cmd.Start(); err != nil {
		result := newCommandResult(spec, phaseError)
		result.Error = err.Error()
		_ = sendResult(encoder, result)
		return fmt.Errorf("start runtime command: %w", err)
	}
	started := newCommandResult(spec, phaseStarted)
	started.PID = cmd.Process.Pid
	if err := sendResult(encoder, started); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = cmd.Wait()
		return err
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waited:
	case received := <-signals:
		if forwarded, ok := received.(syscall.Signal); ok {
			_ = syscall.Kill(-cmd.Process.Pid, forwarded)
		}
		waitErr = <-waited
	case <-ctx.Done():
		// CommandContext invokes the custom Cancel function above, signaling
		// the whole process group before WaitDelay applies its kill fallback.
		waitErr = <-waited
	}
	result := newCommandResult(spec, phaseExited)
	result.ExitCode = childExitCode(waitErr)
	if waitErr != nil {
		result.Error = waitErr.Error()
	}
	return sendResult(encoder, result)
}

// Run receives and executes one pending command from Core.
func Run(ctx context.Context) error {
	path, err := socketPath()
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	rawConn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		return fmt.Errorf("connect to Zaparoo Steam Runtime broker: %w", err)
	}
	conn, ok := rawConn.(*net.UnixConn)
	if !ok {
		_ = rawConn.Close()
		return errors.New("runtime connection is not a Unix socket")
	}
	defer func() { _ = conn.Close() }()
	if _, err := verifySocketPeer(conn); err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(brokerTimeout)); err != nil {
		return fmt.Errorf("set Steam Runtime read deadline: %w", err)
	}
	// The broker classifies connections by this frame. The shortcut only ever
	// runs one command, so it never registers as a host.
	if err := json.NewEncoder(conn).Encode(hello{Role: roleLaunch, Version: protocolVersion}); err != nil {
		return fmt.Errorf("send Steam Runtime hello: %w", err)
	}
	var spec Command
	if err := json.NewDecoder(io.LimitReader(conn, protocolLimit)).Decode(&spec); err != nil {
		// The broker keeps the socket bound between launches, so a user
		// starting the shortcut by hand connects to a broker with nothing
		// to give and is hung up on. That is a no-op, not a failure.
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("read Steam Runtime command: %w", err)
	}
	if err := validateCommand(&spec); err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear Steam Runtime deadline: %w", err)
	}
	return executeCommand(ctx, conn, &spec)
}
