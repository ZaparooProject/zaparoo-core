//go:build linux || darwin

package mister

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"os"
	"strings"
)

const (
	AssetsDir          = DataDir + "/" + platforms.AssetsDir
	TempDir            = "/tmp/zaparoo"
	DisableLaunchFile  = TempDir + "/zaparoo.disabled"
	SuccessSoundFile   = AssetsDir + "/success.wav"
	FailSoundFile      = AssetsDir + "/fail.wav"
	SocketFile         = TempDir + "/core.sock"
	LegacyMappingsPath = "/media/fat/nfc.csv"
	TokenReadFile      = "/tmp/TOKENREAD" // TODO: remove this, use file driver
	DataDir            = "/media/fat/zaparoo"
	ArcadeDbUrl        = "https://api.github.com/repositories/521644036/contents/ArcadeDatabase_CSV"
	ArcadeDbFile       = AssetsDir + "/ArcadeDatabase.csv"
	ScriptsDir         = mrextConfig.ScriptsFolder
	CmdInterface       = "/dev/MiSTer_cmd"
	LinuxDir           = "/media/fat/linux"
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

func UserConfigToMrext(cfg *config.Instance) *mrextConfig.UserConfig {
	var setCore []string
	for _, v := range cfg.SystemDefaults() {
		if v.Launcher == "" {
			continue
		}
		setCore = append(setCore, v.System+":"+v.Launcher)
	}
	return &mrextConfig.UserConfig{
		Systems: mrextConfig.SystemsConfig{
			GamesFolder: cfg.IndexRoots(),
			SetCore:     setCore,
		},
	}
}
