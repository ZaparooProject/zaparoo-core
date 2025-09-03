//go:build linux

package batocera

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAllSystemsHaveValidStructure tests that all systems in SystemMap have valid SystemInfo structure
func TestAllSystemsHaveValidStructure(t *testing.T) {
	t.Parallel()

	for name, system := range SystemMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Test that SystemInfo has the required fields
			assert.NotEmpty(t, system.SystemID, "System %s must have non-empty SystemID", name)
			assert.NotNil(t, system.Extensions, "System %s must have non-nil Extensions", name)
			assert.NotEmpty(t, system.Extensions, "System %s must have at least one extension", name)

			// Test that all extensions are valid format
			for _, ext := range system.Extensions {
				assert.Regexp(t, `^\.[a-zA-Z0-9.]+$`, ext,
					"Extension %s for system %s should start with dot and contain only alphanumeric characters",
					ext, name)
				assert.Greater(t, len(ext), 1, "Extension %s for system %s should be more than just a dot", ext, name)
				assert.Less(t, len(ext), 20, "Extension %s for system %s should be reasonable length", ext, name)
			}

			// Test that SystemID follows expected format (no whitespace, reasonable length)
			assert.NotRegexp(t, `\s`, system.SystemID,
				"SystemID %s should not contain whitespace for system %s", system.SystemID, name)
			assert.Greater(t, len(system.SystemID), 1, "SystemID should be more than 1 character for system %s", name)
			assert.Less(t, len(system.SystemID), 50, "SystemID should be less than 50 characters for system %s", name)
		})
	}
}

// TestSystemMapIntegrity tests the integrity of the SystemMap as a whole
func TestSystemMapIntegrity(t *testing.T) {
	t.Parallel()

	// Test that we have a reasonable number of systems
	assert.GreaterOrEqual(t, len(SystemMap), 50, "Should have at least 50 systems in SystemMap")

	// Test that system names are valid (lowercase, no spaces for batocera compatibility)
	for name := range SystemMap {
		assert.Regexp(t, `^[a-z0-9+.-]+$`, name,
			"System name %s should be lowercase alphanumeric with allowed special chars", name)
		assert.NotEmpty(t, name, "System name should not be empty")
		assert.Less(t, len(name), 30, "System name %s should be reasonable length", name)
	}

	// Test that all SystemIDs are valid
	for name, system := range SystemMap {
		assert.NotEmpty(t, system.SystemID, "System %s should have non-empty SystemID", name)
	}
}

// TestSystemMapAndLauncherMapConsistency tests that SystemMap and LauncherMap are consistent
func TestSystemMapAndLauncherMapConsistency(t *testing.T) {
	t.Parallel()

	// Test that every system in SystemMap has a corresponding launcher
	for name := range SystemMap {
		_, hasLauncher := LauncherMap[name]
		assert.True(t, hasLauncher, "System %s should have corresponding launcher in LauncherMap", name)
	}

	// Test that every launcher has a corresponding system
	for launcherName := range LauncherMap {
		_, hasSystem := SystemMap[launcherName]
		assert.True(t, hasSystem, "Launcher %s should have corresponding system in SystemMap", launcherName)
	}
}

// TestCommonSystemsExist tests that commonly expected systems exist in the SystemMap
func TestCommonSystemsExist(t *testing.T) {
	t.Parallel()

	// Test for some common/critical systems that should exist
	commonSystems := []string{
		"nes", "snes", "megadrive", "arcade", "c64", "amiga500",
		"psx", "n64", "gba", "nds", "psp", "saturn",
	}

	for _, systemName := range commonSystems {
		t.Run(systemName, func(t *testing.T) {
			t.Parallel()
			system, exists := SystemMap[systemName]
			assert.True(t, exists, "Common system %s should exist in SystemMap", systemName)
			if exists {
				assert.NotEmpty(t, system.SystemID, "Common system %s should have SystemID", systemName)
				assert.NotEmpty(t, system.Extensions, "Common system %s should have extensions", systemName)
			}
		})
	}
}

// TestExtensionFormatConsistency tests that all extensions follow consistent format rules
func TestExtensionFormatConsistency(t *testing.T) {
	t.Parallel()

	for systemName, system := range SystemMap {
		t.Run(systemName, func(t *testing.T) {
			t.Parallel()
			for _, ext := range system.Extensions {
				// Extensions should start with dot
				assert.True(t, strings.HasPrefix(ext, "."),
					"Extension %s should start with dot for system %s", ext, systemName)

				// Extensions should be lowercase (batocera convention)
				assert.Equal(t, strings.ToLower(ext), ext,
					"Extension %s should be lowercase for system %s", ext, systemName)

				// Extensions should not contain spaces or invalid characters
				assert.NotContains(t, ext, " ", "Extension %s should not contain spaces for system %s", ext, systemName)
				assert.Regexp(t, `^\.[a-z0-9.]+$`, ext,
					"Extension %s should only contain lowercase alphanumeric chars and dots for system %s",
					ext, systemName)

				// Extensions should be reasonable length
				assert.LessOrEqual(t, len(ext), 15,
					"Extension %s should be reasonable length for system %s", ext, systemName)
				assert.GreaterOrEqual(t, len(ext), 2,
					"Extension %s should be at least 2 characters for system %s", ext, systemName)
			}
		})
	}
}
