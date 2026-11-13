//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
)

type sentSignal struct {
	pid int
	sig syscall.Signal
}

// fakeProcs is a process table. onSignal runs with the table locked and
// may change it, as a process reacting to a signal would.
type fakeProcs struct {
	cmdlines map[int]string
	parents  map[int]int
	comms    map[int]string
	onSignal func(f *fakeProcs, pid int, sig syscall.Signal)
	signals  []sentSignal
	mu       syncutil.Mutex
}

func newFakeProcs() *fakeProcs {
	return &fakeProcs{
		cmdlines: map[int]string{},
		parents:  map[int]int{},
		comms:    map[int]string{},
	}
}

func (f *fakeProcs) add(pid, ppid int, comm, cmdline string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parents[pid] = ppid
	f.comms[pid] = comm
	f.cmdlines[pid] = cmdline
}

// exit removes a process; the caller holds the lock.
func (f *fakeProcs) exit(pid int) {
	delete(f.parents, pid)
	delete(f.comms, pid)
	delete(f.cmdlines, pid)
}

func (f *fakeProcs) alive(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.parents[pid]
	return ok
}

func (f *fakeProcs) cmdline(pid int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cmdlines[pid]
}

func (f *fakeProcs) descendants(pid int) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	table := make(map[int]procInfo, len(f.parents))
	for p, ppid := range f.parents {
		table[p] = procInfo{comm: f.comms[p], ppid: ppid}
	}
	out := descendantsOf(table, pid)
	slices.Sort(out)
	return out
}

func (f *fakeProcs) findByComm(comm string) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int
	for pid, c := range f.comms {
		if c == comm {
			out = append(out, pid)
		}
	}
	slices.Sort(out)
	return out
}

func (f *fakeProcs) signal(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, sentSignal{pid: pid, sig: sig})
	if f.onSignal != nil {
		f.onSignal(f, pid, sig)
	}
	return nil
}

func (f *fakeProcs) sent() []sentSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentSignal(nil), f.signals...)
}

type mediaRecorder struct {
	current *models.ActiveMedia
	history []*models.ActiveMedia
	mu      syncutil.Mutex
}

func (m *mediaRecorder) get() *models.ActiveMedia {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *mediaRecorder) set(media *models.ActiveMedia) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = media
	m.history = append(m.history, media)
}

func (m *mediaRecorder) sets() []*models.ActiveMedia {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*models.ActiveMedia(nil), m.history...)
}

const (
	testRomsDir = "/home/pi/RetroPie/roms"
	loopPID     = 500
)

type testPlatform struct {
	*Platform
	fs     afero.Fs
	procs  *fakeProcs
	clock  *clockwork.FakeClock
	media  *mediaRecorder
	quits  []string
	quitMu syncutil.Mutex
}

func newTestPlatform(t *testing.T) *testPlatform {
	t.Helper()
	tp := &testPlatform{
		fs:    afero.NewMemMapFs(),
		procs: newFakeProcs(),
		clock: clockwork.NewFakeClock(),
		media: &mediaRecorder{},
	}
	tp.Platform = &Platform{
		fs:         tp.fs,
		procs:      tp.procs,
		clock:      tp.clock,
		configsDir: testConfigsDir,
		homeDir:    "/home/pi",
		stateDir:   testStateDir,
		romsDir:    testRomsDir,
		sendQuit: func(_ context.Context, addr string) error {
			tp.quitMu.Lock()
			defer tp.quitMu.Unlock()
			tp.quits = append(tp.quits, addr)
			return nil
		},
		activeMedia:    tp.media.get,
		setActiveMedia: tp.media.set,
	}
	return tp
}

func (tp *testPlatform) quitAddrs() []string {
	tp.quitMu.Lock()
	defer tp.quitMu.Unlock()
	return append([]string(nil), tp.quits...)
}

// startLoop makes the autostart loop look like it is running.
func (tp *testPlatform) startLoop(t *testing.T) {
	t.Helper()
	tp.procs.add(loopPID, 1, "bash", "bash /opt/retropie/configs/all/autostart.sh")
	writeTestFile(t, tp.fs, filepath.Join(testStateDir, loopPIDFile), "500\n", 0o644)
}

// autoAdvance moves the fake clock on whenever the code under test waits on
// it, so polling loops run without real sleeps.
func autoAdvance(t *testing.T, clock *clockwork.FakeClock, step time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if err := clock.BlockUntilContext(ctx, 1); err != nil {
				return
			}
			clock.Advance(step)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}
