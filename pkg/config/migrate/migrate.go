package migrate

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate/iniconfig"
	"gopkg.in/ini.v1"
	"os"
	"strconv"
	"strings"
)

func IniToToml(iniPath string) (config.Values, error) {
	// allow_commands is being purposely ignored and must be explicitly enabled
	// by the user after migration

	vals := config.BaseDefaults
	var iniVals iniconfig.UserConfig

	iniCfg, err := ini.ShadowLoad(iniPath)
	if err != nil {
		return vals, err
	}

	err = iniCfg.StrictMapTo(&iniVals)
	if err != nil {
		return vals, err
	}

	// readers
	for _, r := range iniVals.TapTo.Reader {
		ps := strings.SplitN(r, ":", 2)
		if len(ps) != 2 {
			continue
		}

		vals.Readers.Connect = append(
			vals.Readers.Connect,
			config.ReadersConnect{
				Driver: ps[0],
				Path:   ps[1],
			},
		)
	}

	// connection string
	conStr := iniVals.TapTo.ConnectionString
	if conStr != "" {
		ps := strings.SplitN(conStr, ":", 2)
		if len(ps) != 2 {
			vals.Readers.Connect = append(
				vals.Readers.Connect,
				config.ReadersConnect{
					Driver: ps[0],
					Path:   ps[1],
				},
			)
		}
	}

	// disable sounds
	vals.Audio.ScanFeedback = !iniVals.TapTo.DisableSounds

	// probe device
	vals.Readers.AutoDetect = iniVals.TapTo.ProbeDevice

	// exit game mode
	if iniVals.TapTo.ExitGame {
		vals.Readers.Scan.Mode = config.ScanModeHold
	} else {
		vals.Readers.Scan.Mode = config.ScanModeTap
	}

	// exit game blocklist
	vals.Readers.Scan.IgnoreSystem = iniVals.TapTo.ExitGameBlocklist

	// exit game delay
	vals.Readers.Scan.ExitDelay = float32(iniVals.TapTo.ExitGameDelay)

	// debug
	vals.DebugLogging = iniVals.TapTo.Debug

	// systems - games folder
	vals.Launchers.IndexRoot = iniVals.Systems.GamesFolder

	// systems - set core
	for _, v := range iniVals.Systems.SetCore {
		ps := strings.SplitN(v, ":", 2)
		if len(ps) != 2 {
			continue
		}

		vals.Systems.Default = append(
			vals.Systems.Default,
			config.SystemsDefault{
				System:   ps[0],
				Launcher: ps[1],
			},
		)
	}

	// launchers - allow file
	vals.Launchers.AllowFile = iniVals.Launchers.AllowFile

	// api - port
	port, err := strconv.Atoi(iniVals.Api.Port)
	if err == nil {
		if port != vals.Service.ApiPort {
			vals.Service.ApiPort = port
		}
	}

	// api - allow launch
	vals.Service.AllowLaunch = iniVals.Api.AllowLaunch

	return vals, nil
}

func Required(oldIni string, newToml string) bool {
	iniExists := false
	if _, err := os.Stat(oldIni); err == nil {
		iniExists = true
	}

	tomlExists := false
	if _, err := os.Stat(newToml); err == nil {
		tomlExists = true
	}

	return iniExists && !tomlExists
}
