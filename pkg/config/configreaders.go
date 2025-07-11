package config

import (
	"fmt"
	"slices"
	"strings"
)

type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Scan       ReadersScan      `toml:"scan,omitempty"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}

type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
	OnScan       string   `toml:"on_scan,omitempty"`
	OnRemove     string   `toml:"on_remove,omitempty"`
}

type ReadersConnect struct {
	Driver   string `toml:"driver"`
	Path     string `toml:"path,omitempty"`
	IDSource string `toml:"id_source,omitempty"`
}

func (r ReadersConnect) ConnectionString() string {
	return fmt.Sprintf("%s:%s", r.Driver, r.Path)
}

func (c *Instance) ReadersScan() ReadersScan {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan
}

func (c *Instance) IsHoldModeIgnoredSystem(systemID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var blocklist []string
	for _, v := range c.vals.Readers.Scan.IgnoreSystem {
		blocklist = append(blocklist, strings.ToLower(v))
	}
	return slices.Contains(blocklist, strings.ToLower(systemID))
}

func (c *Instance) TapModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Readers.Scan.Mode == ScanModeTap {
		return true
	} else if c.vals.Readers.Scan.Mode == "" {
		return true
	} else {
		return false
	}
}

func (c *Instance) HoldModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.Mode == ScanModeHold
}

func (c *Instance) SetScanMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.Mode = mode
}

func (c *Instance) SetScanExitDelay(exitDelay float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.ExitDelay = exitDelay
}

func (c *Instance) SetScanIgnoreSystem(ignoreSystem []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.IgnoreSystem = ignoreSystem
}

func (c *Instance) Readers() Readers {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers
}

func (c *Instance) AutoDetect() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.AutoDetect
}

func (c *Instance) SetAutoDetect(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.AutoDetect = enabled
}

func (c *Instance) SetReaderConnections(rcs []ReadersConnect) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Connect = rcs
}
