package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

type serialDevice struct {
	Vid string
	Pid string
}

var ignoreDevices = []serialDevice{
	// Sinden Lightgun
	{Vid: "16c0", Pid: "0f38"},
	{Vid: "16c0", Pid: "0f39"},
	{Vid: "16c0", Pid: "0f01"},
	{Vid: "16c0", Pid: "0f02"},
	{Vid: "16d0", Pid: "0f38"},
	{Vid: "16d0", Pid: "0f39"},
	{Vid: "16d0", Pid: "0f01"},
	{Vid: "16d0", Pid: "0f02"},
	{Vid: "16d0", Pid: "1094"},
	{Vid: "16d0", Pid: "1095"},
	{Vid: "16d0", Pid: "1096"},
	{Vid: "16d0", Pid: "1097"},
	{Vid: "16d0", Pid: "1098"},
	{Vid: "16d0", Pid: "1099"},
	{Vid: "16d0", Pid: "109a"},
	{Vid: "16d0", Pid: "109b"},
	{Vid: "16d0", Pid: "109c"},
	{Vid: "16d0", Pid: "109d"},
}

func ignoreSerialDevice(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return true
	}

	if _, err := os.Stat("/usr/bin/udevadm"); err != nil {
		log.Debug().Msgf("udevadm not found, skipping ignore list check")
		return false
	}

	cmd := exec.Command("/usr/bin/udevadm", "info", "--name="+path)
	out, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("udevadm failed")
		return false
	}

	vid := ""
	pid := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "E: ID_VENDOR_ID=") {
			vid = strings.TrimPrefix(line, "E: ID_VENDOR_ID=")
		} else if strings.HasPrefix(line, "E: ID_MODEL_ID=") {
			pid = strings.TrimPrefix(line, "E: ID_MODEL_ID=")
		}
	}

	if vid == "" || pid == "" {
		return false
	}

	vid = strings.ToLower(vid)
	pid = strings.ToLower(pid)

	for _, v := range ignoreDevices {
		if vid == v.Vid && pid == v.Pid {
			return true
		}
	}

	return false
}

func getLinuxList() ([]string, error) {
	path := "/dev"

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close serial device folder")
		}
	}(f)

	files, err := f.Readdir(0)
	if err != nil {
		return nil, err
	}

	var devices []string

	for _, v := range files {
		if v.IsDir() {
			continue
		}

		if !strings.HasPrefix(v.Name(), "ttyUSB") && !strings.HasPrefix(v.Name(), "ttyACM") {
			continue
		}

		if ignoreSerialDevice(filepath.Join(path, v.Name())) {
			continue
		}

		devices = append(devices, filepath.Join(path, v.Name()))
	}

	return devices, nil
}

func GetSerialDeviceList() ([]string, error) {
	if runtime.GOOS == "linux" {
		return getLinuxList()
	} else if runtime.GOOS == "darwin" {
		var devices []string
		ports, err := serial.GetPortsList()
		if err != nil {
			return nil, err
		}

		for _, v := range ports {
			if !strings.HasPrefix(v, "/dev/tty.usbserial") {
				continue
			}

			// TODO: check against ignore list

			devices = append(devices, v)
		}

		return devices, nil
	} else if runtime.GOOS == "windows" {
		var devices []string
		ports, err := serial.GetPortsList()
		if err != nil {
			return nil, err
		}

		for _, v := range ports {
			if !strings.HasPrefix(v, "COM") {
				continue
			}

			// TODO: check against ignore list

			devices = append(devices, v)
		}

		return devices, nil
	}

	return serial.GetPortsList()
}
