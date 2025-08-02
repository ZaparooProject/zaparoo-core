package config

type Groovy struct {
	GmcProxyBeaconInterval string `toml:"gmc_proxy_beacon_interval"`
	GmcProxyPort           int    `toml:"gmc_proxy_port"`
	GmcProxyEnabled        bool   `toml:"gmc_proxy_enabled"`
}

func (c *Instance) GmcProxyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyEnabled
}

func (c *Instance) GmcProxyPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyPort
}

func (c *Instance) GmcProxyBeaconInterval() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyBeaconInterval
}
