//go:build linux

package config

import (
	"os"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
)

const (
	SDRootDir          = "/media/fat"
	TempDir            = "/tmp/zaparoo"
	LegacyMappingsPath = SDRootDir + "/nfc.csv"
	TokenReadFile      = "/tmp/TOKENREAD" //nolint:gosec // Temp file path, not credentials
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

const DefaultIniFilename = "MiSTer.ini"

const (
	MenuCore        = "MENU"
	LinuxFolder     = SDRootDir + "/linux"
	StartupFile     = LinuxFolder + "/user-startup.sh"
	ActiveGameFile  = TempDir + "/ACTIVEGAME"
	LastLaunchFile  = SDRootDir + "/.LASTLAUNCH.mgl"
	CoreNameFile    = TempDir + "/CORENAME"
	CurrentPathFile = TempDir + "/CURRENTPATH"
)
const CoreConfigFolder = SDRootDir + "/config"

var GamesFolders = []string{
	"/media/usb0/games",
	"/media/usb0",
	"/media/usb1/games",
	"/media/usb1",
	"/media/usb2/games",
	"/media/usb2",
	"/media/usb3/games",
	"/media/usb3",
	"/media/usb4/games",
	"/media/usb4",
	"/media/usb5/games",
	"/media/usb5",
	"/media/network/games",
	"/media/network",
	"/media/fat/cifs/games",
	"/media/fat/cifs",
	"/media/fat/games",
	"/media/fat",
}

// FIXME: splitting this out of the platform so it can be called without
// passing platform to the launch/test launcher functions. better solution
// would be to update the platform interface to give launchers methods
// access to the platform
func RootDirs(cfg *config.Instance) []string {
	return append(cfg.IndexRoots(), GamesFolders...)
}
