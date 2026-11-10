//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package lakka

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testCore    = "/tmp/cores/snes9x_libretro.so"
	testContent = "/storage/roms/snes/Super Game (USA).sfc"
	playingGame = "GET_STATUS PLAYING super_nes,Super Game (USA),crc32=1234abcd"
	contentless = "GET_STATUS CONTENTLESS"
)

var errInactive = errors.New("exit status 3")

// fakeRetroArch is a UDP network command interface. handle returns the reply
// to a datagram, or false to stay silent.
type fakeRetroArch struct {
	conn     net.PacketConn
	handle   func(msg string) (string, bool)
	messages []string
	mu       syncutil.Mutex
}

func newFakeRetroArch(t *testing.T, handle func(msg string) (string, bool)) *fakeRetroArch {
	t.Helper()
	conn, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeRetroArch{conn: conn, handle: handle}
	done := make(chan struct{})
	go f.serve(done)
	t.Cleanup(func() {
		_ = conn.Close()
		<-done
	})
	return f
}

func (f *fakeRetroArch) serve(done chan struct{}) {
	defer close(done)
	buf := make([]byte, 4096)
	for {
		n, from, err := f.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		msg := string(buf[:n])
		f.mu.Lock()
		f.messages = append(f.messages, msg)
		reply, ok := f.handle(msg)
		f.mu.Unlock()
		if ok {
			_, _ = f.conn.WriteTo([]byte(reply), from)
		}
	}
}

func (f *fakeRetroArch) addr() string {
	return f.conn.LocalAddr().String()
}

func (f *fakeRetroArch) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

func (f *fakeRetroArch) count(msg string) int {
	n := 0
	for _, m := range f.sent() {
		if m == msg {
			n++
		}
	}
	return n
}

// retroArchState answers GET_STATUS with the current status; on a command,
// onCommand may change it.
type retroArchState struct {
	onCommand func(s *retroArchState, msg string)
	status    string
	silent    int
}

func (s *retroArchState) handle(msg string) (string, bool) {
	if msg != commandGetStatus {
		if s.onCommand != nil {
			s.onCommand(s, msg)
		}
		return "", false
	}
	if s.silent > 0 {
		s.silent--
		return "", false
	}
	return s.status, true
}

func testFrontend(t *testing.T, addr string) (*frontend, *mocks.MockCommandExecutor, *clockwork.FakeClock) {
	t.Helper()
	exec := &mocks.MockCommandExecutor{}
	t.Cleanup(func() { exec.AssertExpectations(t) })
	clock := clockwork.NewFakeClock()
	f := newFrontend(exec, clock, addr)
	f.queryTimeout = 50 * time.Millisecond
	return f, exec, clock
}

// closedAddr is a local UDP port nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	conn, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := conn.LocalAddr().String()
	require.NoError(t, conn.Close())
	return addr
}

// drive advances the fake clock whenever the code under test waits on it,
// until run returns.
func drive(t *testing.T, clock *clockwork.FakeClock, step time.Duration, run func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-done
		cancel()
	}()
	for {
		if err := clock.BlockUntilContext(ctx, 1); err != nil {
			<-done
			return
		}
		clock.Advance(step)
	}
}

func expectSystemctl(exec *mocks.MockCommandExecutor, err error, args ...string) *mock.Call {
	return exec.On("Run", mock.Anything, "systemctl", args).Return(err).Once()
}
