package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOtherLaunchables_ValidEntryPasses(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", Category: "Other", CorePath: "Arduboy"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "arduboy", valid[0].ID)
	assert.Equal(t, "Arduboy", valid[0].Name)
	assert.Equal(t, "Other", valid[0].Category)
	assert.Equal(t, "Arduboy", valid[0].CorePath)
}

func TestValidateOtherLaunchables_CategoryDefaultsToOther(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "solarus", Name: "Solarus", CorePath: "Solarus"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "Other", valid[0].Category)
}

func TestValidateOtherLaunchables_MissingRequiredFieldsRejected(t *testing.T) {
	tests := []struct {
		name  string
		entry OtherLaunchable
	}{
		{name: "missing id", entry: OtherLaunchable{Name: "Arduboy", CorePath: "Arduboy"}},
		{name: "missing name", entry: OtherLaunchable{ID: "arduboy", CorePath: "Arduboy"}},
		{name: "missing core_path", entry: OtherLaunchable{ID: "arduboy", Name: "Arduboy"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := validateOtherLaunchables([]OtherLaunchable{tt.entry})
			assert.Empty(t, valid)
		})
	}
}

func TestValidateOtherLaunchables_CorePathRejectsPathSeparatorsAndTraversal(t *testing.T) {
	tests := []string{"_Other/Arduboy", "..\\Arduboy", "../Arduboy", "sub/dir"}

	for _, corePath := range tests {
		t.Run(corePath, func(t *testing.T) {
			raw := []OtherLaunchable{{ID: "arduboy", Name: "Arduboy", CorePath: corePath}}
			assert.Empty(t, validateOtherLaunchables(raw))
		})
	}
}

func TestValidateOtherLaunchables_UnknownCategoryRejected(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", Category: "Homebrew", CorePath: "Arduboy"},
	}

	assert.Empty(t, validateOtherLaunchables(raw))
}

func TestValidateOtherLaunchables_DuplicateIDKeepsFirst(t *testing.T) {
	raw := []OtherLaunchable{
		{ID: "arduboy", Name: "Arduboy", CorePath: "Arduboy"},
		{ID: "Arduboy", Name: "Arduboy Two", CorePath: "ArduboyTwo"},
	}

	valid := validateOtherLaunchables(raw)

	require.Len(t, valid, 1)
	assert.Equal(t, "Arduboy", valid[0].Name)
}

func TestOtherLaunchables_LoadTOMLRoundTrip(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
core_path = "Arduboy"

[[other_launchables]]
id = "bad"
name = "Bad Entry"
category = "NotARealCategory"
core_path = "Bad"
`))

	entries := cfg.OtherLaunchables()

	require.Len(t, entries, 1)
	assert.Equal(t, "arduboy", entries[0].ID)
	assert.Equal(t, "Other", entries[0].Category)
}

func TestOtherLaunchables_ReturnsIndependentCopy(t *testing.T) {
	cfg := &Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[other_launchables]]
id = "arduboy"
name = "Arduboy"
core_path = "Arduboy"
`))

	entries := cfg.OtherLaunchables()
	entries[0].Name = "Mutated"

	entriesAgain := cfg.OtherLaunchables()
	assert.Equal(t, "Arduboy", entriesAgain[0].Name)
}
