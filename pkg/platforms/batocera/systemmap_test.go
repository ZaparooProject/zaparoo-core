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
		assert.Regexp(t, `^[a-z0-9+._-]+$`, name,
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

// TestBatoceraOfficialExtensions tests that extensions match Batocera's official es_systems.yml config.
// These were verified against batocera-linux/batocera.linux repository at
// package/batocera/emulationstation/batocera-es-system/es_systems.yml
func TestBatoceraOfficialExtensions(t *testing.T) {
	t.Parallel()

	// Define expected extensions from Batocera's official configuration
	// This ensures our extensions match what Batocera actually uses
	expectedExtensions := map[string][]string{
		// Port/Engine systems
		"ports":       {".sh", ".squashfs"},
		"mrboom":      {".libretro"},
		"quake":       {".quake"},
		"quake2":      {".quake2", ".zip", ".7zip"},
		"quake3":      {".quake3"},
		"sonic-mania": {".sman"},
		"catacomb":    {".game"},
		"fury":        {".grp"},
		"hurrican":    {".game"},
		"dxx-rebirth": {".d1x", ".d2x"},
		"gong":        {".game"},

		// Specialized systems
		"dice":   {".zip", ".dmy"},
		"doom3":  {".d3"},
		"raze":   {".raze"},
		"tyrian": {".game"},
		"library": {
			".jpg", ".jpeg", ".png", ".bmp", ".psd", ".tga", ".gif", ".hdr", ".pic", ".ppm", ".pgm",
			".mkv", ".pdf", ".mp4", ".avi", ".webm", ".cbz", ".mp3", ".wav", ".ogg", ".flac",
			".mod", ".xm", ".stm", ".s3m", ".far", ".it", ".669", ".mtm",
		},

		// Modern console systems
		"ps2":     {".iso", ".mdf", ".nrg", ".bin", ".img", ".dump", ".gz", ".cso", ".chd", ".m3u"},
		"ps3":     {".ps3", ".psn", ".squashfs"},
		"psvita":  {".zip", ".psvita"},
		"switch":  {".xci", ".nsp"},
		"wiiu":    {".wua", ".wup", ".wud", ".wux", ".rpx", ".squashfs", ".wuhb"},
		"xbox360": {".iso", ".xex", ".xbox360", ".zar"},
	}

	for systemName, expectedExts := range expectedExtensions {
		t.Run(systemName, func(t *testing.T) {
			t.Parallel()

			system, exists := SystemMap[systemName]
			assert.True(t, exists, "System %s should exist in SystemMap", systemName)

			if !exists {
				return
			}

			// Verify all expected extensions are present
			for _, expectedExt := range expectedExts {
				assert.Contains(t, system.Extensions, expectedExt,
					"System %s should have extension %s according to Batocera official config",
					systemName, expectedExt)
			}

			// Verify no unexpected extensions are present
			for _, actualExt := range system.Extensions {
				assert.Contains(t, expectedExts, actualExt,
					"System %s has unexpected extension %s (not in Batocera official config)",
					systemName, actualExt)
			}

			// Verify exact match (same count and content)
			assert.ElementsMatch(t, expectedExts, system.Extensions,
				"System %s extensions should exactly match Batocera official config", systemName)
		})
	}
}
