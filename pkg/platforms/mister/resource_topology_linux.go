//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	mmcIRQDevice             = "dw-mci"
	resourceTopologyInterval = time.Second
)

var (
	frontendResourceLeasePath = filepath.Join(string(filepath.Separator), "tmp", "zaparoo", "frontend.active.lock")
	interruptsPath            = filepath.Join(string(filepath.Separator), "proc", "interrupts")
	coreTasksPath             = filepath.Join(string(filepath.Separator), "proc", "self", "task")
	irqAffinityRoot           = filepath.Join(string(filepath.Separator), "proc", "irq")
)

type resourceTopologyHooks struct {
	leaseActive     func() (bool, error)
	setCoreAffinity func(bool) error
	setMMCAffinity  func(bool) error
}

// StartResourceTopologyManager keeps CPU0 available to the software frontend
// only while frontend holds its kernel-backed activity lease. The lease is
// released automatically on exit or forced termination, so Core restarts and
// frontend startup ordering cannot leave topology in a stale state.
func StartResourceTopologyManager(ctx context.Context) {
	ticker := time.NewTicker(resourceTopologyInterval)
	go func() {
		defer ticker.Stop()
		runResourceTopologyManager(ctx, ticker.C, resourceTopologyHooks{
			leaseActive:     frontendResourceLeaseActive,
			setCoreAffinity: setCoreAffinity,
			setMMCAffinity:  setMMCAffinity,
		})
	}()
}

func runResourceTopologyManager(
	ctx context.Context,
	ticks <-chan time.Time,
	hooks resourceTopologyHooks,
) {
	initialized := false
	lastActive := false
	for {
		active, err := hooks.leaseActive()
		if err != nil {
			log.Warn().Err(err).Msg("failed to read MiSTer frontend resource lease")
		} else {
			// Reapply process affinity every pass so threads created after a
			// transition inherit or receive the current topology.
			if affinityErr := hooks.setCoreAffinity(active); affinityErr != nil {
				log.Warn().Err(affinityErr).Msg("failed to apply MiSTer Core CPU affinity")
			}
			if !initialized || active != lastActive {
				if irqErr := hooks.setMMCAffinity(active); irqErr != nil {
					log.Warn().Err(irqErr).Msg("failed to apply MiSTer MMC IRQ affinity")
				}
				if active {
					log.Info().Msg("MiSTer frontend active: Core and MMC assigned to CPU1")
				} else {
					log.Info().Msg("MiSTer frontend inactive: Core restored to CPUs 0-1")
				}
				initialized = true
				lastActive = active
			}
		}

		select {
		case <-ctx.Done():
			if affinityErr := hooks.setCoreAffinity(false); affinityErr != nil {
				log.Warn().Err(affinityErr).Msg("failed to restore MiSTer Core CPU affinity during shutdown")
			}
			if irqErr := hooks.setMMCAffinity(false); irqErr != nil {
				log.Warn().Err(irqErr).Msg("failed to restore MiSTer MMC IRQ affinity during shutdown")
			}
			return
		case <-ticks:
		}
	}
}

func frontendResourceLeaseActive() (bool, error) {
	//nolint:gosec // Fixed internal path assembled from constant components.
	file, err := os.OpenFile(
		frontendResourceLeasePath,
		os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return false, fmt.Errorf("opening frontend resource lease: %w", err)
	}
	defer func() { _ = file.Close() }()

	//nolint:gosec // File descriptors fit in int on supported MiSTer Linux.
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("probing frontend resource lease: %w", err)
	}
	//nolint:gosec // File descriptors fit in int on supported MiSTer Linux.
	if unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); unlockErr != nil {
		return false, fmt.Errorf("unlocking frontend resource lease probe: %w", unlockErr)
	}
	return false, nil
}

func setCoreAffinity(frontendActive bool) error {
	var cpus unix.CPUSet
	if frontendActive {
		cpus.Set(1)
	} else {
		cpus.Set(0)
		cpus.Set(1)
	}

	tasks, err := os.ReadDir(coreTasksPath)
	if err != nil {
		return fmt.Errorf("reading Core tasks: %w", err)
	}
	for _, task := range tasks {
		tid, parseErr := strconv.Atoi(task.Name())
		if parseErr != nil {
			continue
		}
		if affinityErr := unix.SchedSetaffinity(tid, &cpus); affinityErr != nil {
			if errors.Is(affinityErr, unix.ESRCH) {
				continue
			}
			return fmt.Errorf("setting task %d affinity: %w", tid, affinityErr)
		}
	}
	return nil
}

func setMMCAffinity(frontendActive bool) error {
	//nolint:gosec // Fixed procfs path assembled from constant components.
	interrupts, err := os.ReadFile(interruptsPath)
	if err != nil {
		return fmt.Errorf("reading interrupts: %w", err)
	}
	irq, ok := findIRQ(string(interrupts), mmcIRQDevice)
	if !ok {
		return errors.New("MiSTer MMC IRQ not found")
	}

	cpus := "0-1"
	if frontendActive {
		cpus = "1"
	}
	affinityPath := filepath.Join(irqAffinityRoot, strconv.Itoa(irq), "smp_affinity_list")
	if err = os.WriteFile(affinityPath, []byte(cpus), 0o600); err != nil {
		return fmt.Errorf("writing IRQ %d affinity: %w", irq, err)
	}
	return nil
}

func findIRQ(interrupts, device string) (int, bool) {
	for line := range strings.SplitSeq(interrupts, "\n") {
		irqText, devices, found := strings.Cut(line, ":")
		if !found || !strings.Contains(devices, device) {
			continue
		}
		irq, err := strconv.Atoi(strings.TrimSpace(irqText))
		if err == nil {
			return irq, true
		}
	}
	return 0, false
}
