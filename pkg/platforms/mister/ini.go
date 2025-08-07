package mister

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

type INIFile struct {
	ID          int
	DisplayName string
	Filename    string
	Path        string
}

func GetAllINIFiles() ([]INIFile, error) {
	var inis []INIFile

	files, err := os.ReadDir(config.SDRootDir)
	if err != nil {
		return nil, err
	}

	var iniFilenames []string

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(strings.ToLower(file.Name())) == ".ini" {
			iniFilenames = append(iniFilenames, file.Name())
		}
	}

	currentID := 1

	for _, filename := range iniFilenames {
		lower := strings.ToLower(filename)

		if lower == strings.ToLower(config.DefaultIniFilename) {
			inis = append(inis, INIFile{
				ID:          currentID,
				DisplayName: "Main",
				Filename:    filename,
				Path:        filepath.Join(config.SDRootDir, filename),
			})

			currentID++
		} else if strings.HasPrefix(lower, "mister_") {
			iniFile := INIFile{
				ID:          currentID,
				DisplayName: "",
				Filename:    filename,
				Path:        filepath.Join(config.SDRootDir, filename),
			}

			iniFile.DisplayName = filename[7:]
			iniFile.DisplayName = strings.TrimSuffix(iniFile.DisplayName, filepath.Ext(iniFile.DisplayName))

			if iniFile.DisplayName == "" {
				iniFile.DisplayName = " -- "
			} else if iniFile.DisplayName == "alt_1" {
				iniFile.DisplayName = "Alt1"
			} else if iniFile.DisplayName == "alt_2" {
				iniFile.DisplayName = "Alt2"
			} else if iniFile.DisplayName == "alt_3" {
				iniFile.DisplayName = "Alt3"
			}

			if len(iniFile.DisplayName) > 4 {
				iniFile.DisplayName = iniFile.DisplayName[0:4]
			}

			if len(inis) < 4 {
				inis = append(inis, iniFile)
			}

			currentID++
		}
	}

	return inis, nil
}
