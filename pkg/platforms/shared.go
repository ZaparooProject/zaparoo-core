package platforms

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"os"
	"path/filepath"
)

func HasUserDir() (string, bool) {
	exeDir := ""
	envExe := os.Getenv(config.AppEnv)
	var err error

	if envExe != "" {
		exeDir = envExe
	} else {
		exeDir, err = os.Executable()
		if err != nil {
			return "", false
		}
	}

	parent := filepath.Dir(exeDir)
	userDir := filepath.Join(parent, config.UserDir)

	if info, err := os.Stat(userDir); err == nil {
		if !info.IsDir() {
			return "", false
		} else {
			return userDir, true
		}
	} else {
		return "", false
	}
}
