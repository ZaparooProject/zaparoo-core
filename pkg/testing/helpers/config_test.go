package helpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTestConfig demonstrates the need for a standard test config helper
func TestNewTestConfig(t *testing.T) {
	t.Parallel()
	
	// Setup in-memory filesystem (for future filesystem-based config support)
	fs := NewMemoryFS()
	
	// Create a temporary directory for config
	configDir := t.TempDir()
	
	// This should create a proper config instance for testing
	cfg, err := NewTestConfig(fs, configDir)
	
	// Verify the config was created properly
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, config.BaseDefaults.Service.APIPort, cfg.APIPort())
	
	// Verify the config file exists on the real filesystem
	configPath := filepath.Join(configDir, config.CfgFile)
	_, err = os.Stat(configPath)
	assert.NoError(t, err, "config file should exist")
}