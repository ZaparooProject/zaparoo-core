//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

// Package procscanner provides a shared process scanner for monitoring
// multiple types of processes with a single /proc scan.
package procscanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/proctracker"
	"github.com/rs/zerolog/log"
)

const (
	// DefaultPollInterval is the default polling interval for process scanning.
	DefaultPollInterval = 2 * time.Second
)

// ProcessInfo contains information about a running process.
type ProcessInfo struct {
	Comm    string
	Cmdline string
	PID     int
}

// Matcher determines if a process should be tracked.
type Matcher interface {
	// Match returns true if this process should be tracked.
	Match(proc ProcessInfo) bool
}

// MatcherFunc is a function adapter for Matcher interface.
type MatcherFunc func(proc ProcessInfo) bool

// Match implements Matcher.
func (f MatcherFunc) Match(proc ProcessInfo) bool {
	return f(proc)
}

// Callbacks contains the callbacks for process lifecycle events.
type Callbacks struct {
	// OnStart is called when a matching process is detected.
	OnStart func(proc ProcessInfo)
	// OnStop is called when a tracked process exits.
	OnStop func(pid int)
}

// WatchID uniquely identifies a registered watcher.
type WatchID int

type watcher struct {
	matcher   Matcher
	callbacks Callbacks
}

// Scanner monitors /proc for processes and notifies registered watchers.
type Scanner struct {
	watchers     map[WatchID]*watcher
	procTracker  *proctracker.Tracker
	tracked      map[int]map[WatchID]bool // PID -> set of watcher IDs that matched
	done         chan struct{}
	procPath     string
	wg           sync.WaitGroup
	pollInterval time.Duration
	nextID       WatchID
	mu           syncutil.Mutex
}

// Option configures a Scanner.
type Option func(*Scanner)

// WithPollInterval sets the polling interval for process scanning.
func WithPollInterval(d time.Duration) Option {
	return func(s *Scanner) {
		s.pollInterval = d
	}
}

// WithProcPath sets a custom /proc path (for testing).
func WithProcPath(path string) Option {
	return func(s *Scanner) {
		s.procPath = path
	}
}

// New creates a new process scanner.
func New(opts ...Option) *Scanner {
	s := &Scanner{
		watchers:     make(map[WatchID]*watcher),
		procTracker:  proctracker.New(),
		tracked:      make(map[int]map[WatchID]bool),
		pollInterval: DefaultPollInterval,
		procPath:     "/proc",
		done:         make(chan struct{}),
		nextID:       1,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Watch registers a watcher to be notified when matching processes start/stop.
// Returns a WatchID that can be used to unregister the watcher.
func (s *Scanner) Watch(m Matcher, cb Callbacks) WatchID {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++

	s.watchers[id] = &watcher{
		matcher:   m,
		callbacks: cb,
	}

	log.Debug().Int("watchID", int(id)).Msg("registered process watcher")
	return id
}

// Unwatch removes a registered watcher.
func (s *Scanner) Unwatch(id WatchID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.watchers, id)

	// Clean up any tracked processes for this watcher
	for pid, watcherIDs := range s.tracked {
		delete(watcherIDs, id)
		if len(watcherIDs) == 0 {
			delete(s.tracked, pid)
		}
	}

	log.Debug().Int("watchID", int(id)).Msg("unregistered process watcher")
}

// Start begins monitoring for processes.
func (s *Scanner) Start() error {
	s.wg.Add(1)
	go s.pollLoop()
	log.Info().Msg("process scanner started")
	return nil
}

// Stop stops the scanner and waits for goroutines to finish.
func (s *Scanner) Stop() {
	close(s.done)
	s.procTracker.Stop()
	s.wg.Wait()
	log.Info().Msg("process scanner stopped")
}

// pollLoop periodically scans for processes.
func (s *Scanner) pollLoop() {
	defer s.wg.Done()

	// Initial scan
	s.scan()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan reads /proc once and notifies all matching watchers.
func (s *Scanner) scan() {
	processes, err := s.readProcesses()
	if err != nil {
		log.Warn().Err(err).Msg("failed to scan processes")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check each process against each watcher
	for _, proc := range processes {
		// Skip if already fully tracked
		if existingWatchers, exists := s.tracked[proc.PID]; exists {
			// Check if any new watchers match this process
			for watchID, w := range s.watchers {
				if existingWatchers[watchID] {
					continue // Already tracking for this watcher
				}
				if w.matcher.Match(proc) {
					s.trackProcess(proc, watchID, w)
				}
			}
			continue
		}

		// New process - check all watchers
		for watchID, w := range s.watchers {
			if w.matcher.Match(proc) {
				s.trackProcess(proc, watchID, w)
			}
		}
	}
}

// trackProcess starts tracking a process for a specific watcher.
func (s *Scanner) trackProcess(proc ProcessInfo, watchID WatchID, w *watcher) {
	// Initialize tracking map for this PID if needed
	if s.tracked[proc.PID] == nil {
		s.tracked[proc.PID] = make(map[WatchID]bool)
	}

	// Mark as tracked for this watcher
	s.tracked[proc.PID][watchID] = true

	log.Debug().
		Int("pid", proc.PID).
		Str("comm", proc.Comm).
		Int("watchID", int(watchID)).
		Msg("tracking process")

	// Set up exit tracking (only once per PID)
	if len(s.tracked[proc.PID]) == 1 {
		pid := proc.PID
		err := s.procTracker.Track(pid, func(_ int) {
			s.handleProcessExit(pid)
		})
		if err != nil {
			log.Warn().Err(err).Int("pid", pid).Msg("failed to track process exit")
		}
	}

	// Call start callback
	if w.callbacks.OnStart != nil {
		go w.callbacks.OnStart(proc)
	}
}

// handleProcessExit is called when a tracked process exits.
func (s *Scanner) handleProcessExit(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	watcherIDs, exists := s.tracked[pid]
	if !exists {
		return
	}

	log.Debug().Int("pid", pid).Msg("process exited")

	// Notify all watchers that were tracking this process
	for watchID := range watcherIDs {
		if w, ok := s.watchers[watchID]; ok && w.callbacks.OnStop != nil {
			go w.callbacks.OnStop(pid)
		}
	}

	// Clean up tracking state
	delete(s.tracked, pid)
}

// readProcesses reads all process info from /proc.
func (s *Scanner) readProcesses() ([]ProcessInfo, error) {
	entries, err := os.ReadDir(s.procPath)
	if err != nil {
		return nil, fmt.Errorf("read proc directory: %w", err)
	}

	processes := make([]ProcessInfo, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read process info
		proc, ok := s.readProcessInfo(pid)
		if !ok {
			continue
		}

		processes = append(processes, proc)
	}

	return processes, nil
}

// readProcessInfo reads comm and cmdline for a process.
func (s *Scanner) readProcessInfo(pid int) (ProcessInfo, bool) {
	pidStr := strconv.Itoa(pid)

	// Read comm
	commPath := filepath.Join(s.procPath, pidStr, "comm")
	commData, err := os.ReadFile(commPath) //nolint:gosec // G304: procPath is controlled
	if err != nil {
		return ProcessInfo{}, false
	}

	// Read cmdline
	cmdlinePath := filepath.Join(s.procPath, pidStr, "cmdline")
	cmdlineData, _ := os.ReadFile(cmdlinePath) //nolint:gosec // G304: procPath is controlled

	return ProcessInfo{
		PID:     pid,
		Comm:    strings.TrimSpace(string(commData)),
		Cmdline: string(cmdlineData),
	}, true
}
