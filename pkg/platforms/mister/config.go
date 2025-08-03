//go:build linux

package mister

import (
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	mrextconfig "github.com/wizzomafizzo/mrext/pkg/config"
)

const (
	SDRootDir          = "/media/fat"
	TempDir            = "/tmp/zaparoo"
	LegacyMappingsPath = SDRootDir + "/nfc.csv"
	TokenReadFile      = "/tmp/TOKENREAD"
	DataDir            = SDRootDir + "/zaparoo"
	ArcadeDbURL        = "https://api.github.com/repositories/521644036/contents/ArcadeDatabase_CSV"
	ArcadeDbFile       = "ArcadeDatabase.csv"
	ScriptsDir         = SDRootDir + "/Scripts"
	CmdInterface       = "/dev/MiSTer_cmd"
	LinuxDir           = SDRootDir + "/linux"
	MainPickerDir      = "/tmp/PICKERITEMS"
	MainPickerSelected = "/tmp/PICKERSELECTED"
	MainFeaturesFile   = "/tmp/MAINFEATURES"
	MainFeaturePicker  = "PICKER"
	MainFeatureNotice  = "NOTICE"
)

func MainHasFeature(feature string) bool {
	if _, err := os.Stat(MainFeaturesFile); os.IsNotExist(err) {
		return false
	}

	contents, err := os.ReadFile(MainFeaturesFile)
	if err != nil {
		return false
	}

	features := strings.Split(string(contents), ",")

	for _, f := range features {
		if strings.EqualFold(f, feature) {
			return true
		}
	}

	return false
}

func UserConfigToMrext(cfg *config.Instance) *mrextconfig.UserConfig {
	var setCore []string
	for _, v := range cfg.SystemDefaults() {
		if v.Launcher == "" {
			continue
		}
		setCore = append(setCore, v.System+":"+v.Launcher)
	}
	return &mrextconfig.UserConfig{
		Systems: mrextconfig.SystemsConfig{
			GamesFolder: cfg.IndexRoots(),
			SetCore:     setCore,
		},
	}
}
