package config

import "strings"

type Systems struct {
	Default []SystemsDefault `toml:"default,omitempty"`
}

type SystemsDefault struct {
	System     string `toml:"system"`
	Launcher   string `toml:"launcher,omitempty"`
	BeforeExit string `toml:"before_exit,omitempty"`
}

func (c *Instance) SystemDefaults() []SystemsDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Systems.Default
}

func (c *Instance) LookupSystemDefaults(systemId string) (SystemsDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, defaultSystem := range c.vals.Systems.Default {
		if strings.EqualFold(defaultSystem.System, systemId) {
			return defaultSystem, true
		}
	}
	return SystemsDefault{}, false
}
