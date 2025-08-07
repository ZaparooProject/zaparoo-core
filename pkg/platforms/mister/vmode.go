package mister

import (
	"fmt"
	"os"

	misterconfig "github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

const VideoModeFormatRGB32 = "18888"

// fb_cmd0 = scaled = fb_cmd0 $fmt $rb $scale
// fb_cmd1 = exact = fb_cmd1 $fmt $rb $width $height

// in vmode script, checks for rescount contents at start, sets mode,
// then polls until it's the same value (up to 5 times)
// /sys/module/MiSTer_fb/parameters/res_count

func SetVideoMode(width int, height int) error {
	if _, err := os.Stat(misterconfig.CmdInterface); err != nil {
		return fmt.Errorf("command interface not accessible: %s", err)
	}

	cmd, err := os.OpenFile(misterconfig.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func(cmd *os.File) {
		_ = cmd.Close()
	}(cmd)

	cmdStr := fmt.Sprintf(
		"%s %d %d %d",
		VideoModeFormatRGB32[1:],
		VideoModeFormatRGB32[0],
		width,
		height,
	)

	fmt.Println(cmdStr)

	_, err = cmd.WriteString(cmdStr)
	if err != nil {
		return err
	}

	return nil
}
