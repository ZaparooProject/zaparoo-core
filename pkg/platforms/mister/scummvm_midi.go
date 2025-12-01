//go:build linux

package mister

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

const (
	midiMeisterPath = "/media/fat/linux/MIDIMeister"
	midiCPUMask     = "03" // CPU cores 0-1
)

var (
	midiProcess   *os.Process
	midiProcessMu syncutil.Mutex
)

// shouldStartMIDIMeister checks if MIDIMeister is available and should be started
func shouldStartMIDIMeister() bool {
	info, err := os.Stat(midiMeisterPath)
	if err != nil {
		return false
	}

	// Check if it's executable
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return false
	}

	return true
}

// startMIDIMeister launches the MIDIMeister MIDI driver
func startMIDIMeister() error {
	midiProcessMu.Lock()
	defer midiProcessMu.Unlock()

	if !shouldStartMIDIMeister() {
		return nil // Not an error, just not available
	}

	// Kill any existing MIDIMeister process first
	ctx := context.Background()
	_ = exec.CommandContext(ctx, "killall", "MIDIMeister").Run()

	// Start MIDIMeister with CPU affinity
	cmd := exec.CommandContext(ctx, "taskset", midiCPUMask, midiMeisterPath, "QUIET")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MIDIMeister: %w", err)
	}

	midiProcess = cmd.Process
	log.Debug().Int("pid", midiProcess.Pid).Msg("started MIDIMeister")
	return nil
}

// stopMIDIMeister kills the MIDIMeister process
func stopMIDIMeister() {
	midiProcessMu.Lock()
	defer midiProcessMu.Unlock()

	// Try to kill via killall first (more reliable)
	ctx := context.Background()
	if err := exec.CommandContext(ctx, "killall", "MIDIMeister").Run(); err != nil {
		log.Debug().Err(err).Msg("killall MIDIMeister failed (may not be running)")
	}

	// Also try to kill our tracked process if we have one
	if midiProcess != nil {
		if err := midiProcess.Kill(); err != nil {
			log.Debug().Err(err).Int("pid", midiProcess.Pid).Msg("failed to kill MIDIMeister process")
		}
		midiProcess = nil
	}

	log.Debug().Msg("stopped MIDIMeister")
}
