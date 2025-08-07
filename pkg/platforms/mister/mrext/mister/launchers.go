package mister

import (
	"fmt"
	"os"
	"path/filepath"
	s "strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/games"
)

func GenerateMgl(cfg *config.UserConfig, system *games.System, path string, override string) (string, error) {
	// override the system rbf with the user specified one
	for _, setCore := range cfg.Systems.SetCore {
		parts := s.SplitN(setCore, ":", 2)
		if len(parts) != 2 {
			continue
		}

		if s.EqualFold(parts[0], system.ID) {
			system.RBF = parts[1]
			break
		}
	}

	mgl := fmt.Sprintf("<mistergamedescription>\n\t<rbf>%s</rbf>\n", system.RBF)

	if system.SetName != "" {
		sameDir := ""
		if system.SetNameSameDir {
			sameDir = " same_dir=\"1\""
		}

		mgl += fmt.Sprintf("\t<setname%s>%s</setname>\n", sameDir, system.SetName)
	}

	if path == "" {
		mgl += "</mistergamedescription>"
		return mgl, nil
	} else if override != "" {
		mgl += override
		mgl += "</mistergamedescription>"
		return mgl, nil
	}

	mglDef, err := games.PathToMglDef(*system, path)
	if err != nil {
		return "", err
	}

	mgl += fmt.Sprintf("<file delay=\"%d\" type=\"%s\" index=\"%d\" path=\"../../../../..%s\"/>\n", mglDef.Delay, mglDef.Method, mglDef.Index, path)
	mgl += "</mistergamedescription>"
	return mgl, nil
}

func writeTempFile(content string) (string, error) {
	tmpFile, err := os.Create(config.LastLaunchFile)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	_, err = tmpFile.WriteString(content)
	if err != nil {
		return "", err
	}
	return tmpFile.Name(), nil
}

func launchFile(path string) error {
	_, err := os.Stat(config.CmdInterface)
	if err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	if !(s.HasSuffix(s.ToLower(path), ".mgl") || s.HasSuffix(s.ToLower(path), ".mra") || s.HasSuffix(s.ToLower(path), ".rbf")) {
		return fmt.Errorf("not a valid launch file: %s", path)
	}

	cmd, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer cmd.Close()

	cmd.WriteString(fmt.Sprintf("load_core %s\n", path))

	return nil
}

func launchTempMgl(cfg *config.UserConfig, system *games.System, path string) error {
	override, err := games.RunSystemHook(cfg, *system, path)
	if err != nil {
		return err
	}

	mgl, err := GenerateMgl(cfg, system, path, override)
	if err != nil {
		return err
	}

	tmpFile, err := writeTempFile(mgl)
	if err != nil {
		return err
	}

	return launchFile(tmpFile)
}

// LaunchShortCore attempts to launch a core with a short path, as per what's
// allowed in an MGL file.
func LaunchShortCore(path string) error {
	mgl := fmt.Sprintf(
		"<mistergamedescription>\n\t<rbf>%s</rbf>\n</mistergamedescription>\n",
		path,
	)

	tmpFile, err := writeTempFile(mgl)
	if err != nil {
		return err
	}

	return launchFile(tmpFile)
}

func LaunchGame(cfg *config.UserConfig, system games.System, path string) error {
	switch s.ToLower(filepath.Ext(path)) {
	case ".mra":
		err := launchFile(path)
		if err != nil {
			return err
		}
	case ".mgl":
		err := launchFile(path)
		if err != nil {
			return err
		}

		if ActiveGameEnabled() {
			SetActiveGame(path)
		}
	default:
		err := launchTempMgl(cfg, &system, path)
		if err != nil {
			return err
		}

		if ActiveGameEnabled() {
			SetActiveGame(path)
		}
	}

	return nil
}

// LaunchCore Launch a core given a possibly partial path, as per MGL files.
func LaunchCore(cfg *config.UserConfig, system games.System) error {
	if _, err := os.Stat(config.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	if system.SetName != "" {
		return LaunchGame(cfg, system, "")
	}

	var path string
	rbfs := games.SystemsWithRbf()
	if _, ok := rbfs[system.ID]; ok {
		path = rbfs[system.ID].Path
	} else {
		return fmt.Errorf("no core found for system %s", system.ID)
	}

	cmd, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer cmd.Close()

	cmd.WriteString(fmt.Sprintf("load_core %s\n", path))

	return nil
}

func LaunchMenu() error {
	if _, err := os.Stat(config.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	cmd, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer cmd.Close()

	// TODO: don't hardcode here
	cmd.WriteString(fmt.Sprintf("load_core %s\n", filepath.Join(config.SdFolder, "menu.rbf")))

	return nil
}

// LaunchGenericFile Given a generic file path, launch it using the correct method, if possible.
func LaunchGenericFile(cfg *config.UserConfig, path string) error {
	var err error
	isGame := false
	ext := s.ToLower(filepath.Ext(path))
	switch ext {
	case ".mra":
		err = launchFile(path)
		if err != nil {
			return err
		}
	case ".mgl":
		err = launchFile(path)
		if err != nil {
			return err
		}
		isGame = true
	case ".rbf":
		err = launchFile(path)
		if err != nil {
			return err
		}
	default:
		system, err := games.BestSystemMatch(cfg, path)
		if err != nil {
			return fmt.Errorf("unknown file type: %s", ext)
		}

		err = launchTempMgl(cfg, &system, path)
		if err != nil {
			return err
		}
		isGame = true
	}

	if ActiveGameEnabled() && isGame {
		err := SetActiveGame(path)
		if err != nil {
			return err
		}
	}

	return nil
}
