//go:build linux || darwin

package mister

import (
	mrextConfig "github.com/wizzomafizzo/mrext/pkg/config"
	"github.com/wizzomafizzo/tapto/pkg/config"
)

const (
	TempFolder        = "/tmp/tapto"
	LogFile           = TempFolder + "/tapto.log"
	DisableLaunchFile = TempFolder + "/tapto.disabled"
	SuccessSoundFile  = TempFolder + "/success.wav"
	FailSoundFile     = TempFolder + "/fail.wav"
	SocketFile        = TempFolder + "/tapto.sock"
	PidFile           = TempFolder + "/tapto.pid"
	vplayFilePath     = TempFolder + "/vplay.sh"
	MappingsFile      = "/media/fat/nfc.csv"
	TokenReadFile     = "/tmp/TOKENREAD"
	ConfigFolder      = mrextConfig.ScriptsConfigFolder + "/tapto"
	DbFile            = ConfigFolder + "/tapto.db"
	GamesDbFile       = ConfigFolder + "/games.db"
	ArcadeDbUrl       = "https://api.github.com/repositories/521644036/contents/ArcadeDatabase_CSV"
	ArcadeDbFile      = ConfigFolder + "/ArcadeDatabase.csv"
	ScriptsFolder     = mrextConfig.ScriptsFolder
	CmdInterface      = "/dev/MiSTer_cmd"
    	vplayContent      = `#!/bin/bash

declare -g crtmode_640="video_mode=640,16,64,80,240,1,3,14,12380"
declare -g crtmode_320="video_mode=320,-16,32,32,240,1,3,13,5670"

# Read the contents of the INI file
declare -g ini_file="/media/fat/MiSTer.ini"
declare -g ini_contents=$(cat "$ini_file")

declare -g branch="main"
declare -g repository_url="https://github.com/mrchrisster/MiSTer_SAM"

function curl_download() { # curl_download ${filepath} ${URL}

	curl \
		--connect-timeout 15 --max-time 600 --retry 3 --retry-delay 5 --silent --show-error \
		--insecure \
		--fail \
		--location \
		-o "${1}" \
		"${2}"
}

function get_mplayer() {
	echo " Downloading wizzo's mplayer for SAM..."
	echo " Created for MiSTer by wizzo"
	echo " https://github.com/wizzomafizzo/mrext"
	latest_mplayer="${repository_url}/blob/${branch}/.MiSTer_SAM/mplayer.zip?raw=true"
	latest_mbc="${repository_url}/blob/${branch}/.MiSTer_SAM/mbc?raw=true"
	curl_download "/tmp/tapto/mplayer.zip" "${latest_mplayer}"
	curl_download "/tmp/tapto/mbc" "${latest_mbc}"
	unzip -ojq /tmp/tapto/mplayer.zip -d "/tmp/tapto"
    chmod +x /tmp/tapto/mplayer
    chmod +x /tmp/tapto/mbc
    rm /tmp/tapto/mplayer.zip
	echo " Done."
}

if [ ! -f /tmp/tapto/mplayer ]; then
    get_mplayer
fi

function mplayer() {
	LD_LIBRARY_PATH=/tmp/tapto /tmp/tapto/mplayer "$@"
}

url=$1

res="$(mplayer -vo null -ao null -identify -frames 0 $url | grep "VIDEO:" | awk '{print $3}')"
res_comma=$(echo "$res" | tr 'x' ',')
res_space=$(echo "$res" | tr 'x' ' ')

function change_menures() {
# Backup mister.ini
if [ -f "$ini_file" ]; then
	cp "$ini_file" "${ini_file}".vpl
else
	touch "$ini_file"
fi

#append menu info
echo -e "\n[menu]" >> "$ini_file"
echo -e "$crtmode_320" >> "$ini_file"
echo "[menu] entry created."

# Replace video_mode if it exists within [menu] entry
if [[ $ini_contents =~ \[menu\].*video_mode=([^,[:space:]]+) ]]; then
    awk -v res_comma="$res_comma" '/\[menu\]/{p=1} p&&/video_mode/{sub(/=.*/, "="res_comma",60"); p=0} 1' "$ini_file" > "$ini_file.tmp" && mv "$ini_file.tmp" "$ini_file"
    echo "video_mode replaced in [menu] entry."
fi
}

## Play video
change_menures
echo load_core /media/fat/menu.rbf > /dev/MiSTer_cmd
sleep 2
# open mister terminal
/media/fat/Scripts/.MiSTer_SAM/mbc raw_seq :43
chvt 2
vmode -r ${res_space} rgb32
sleep 2
mplayer -cache 8192 "$url"
cp "$ini_file.vpl" "$ini_file"
echo load_core /media/fat/menu.rbf > /dev/MiSTer_cmd
`



)

func UserConfigToMrext(cfg *config.UserConfig) *mrextConfig.UserConfig {
	return &mrextConfig.UserConfig{
		AppPath: cfg.AppPath,
		IniPath: cfg.IniPath,
		Nfc: mrextConfig.NfcConfig{
			ConnectionString: cfg.GetConnectionString(),
			AllowCommands:    cfg.GetAllowCommands(),
			DisableSounds:    cfg.GetDisableSounds(),
			ProbeDevice:      cfg.GetProbeDevice(),
		},
		Systems: mrextConfig.SystemsConfig{
			GamesFolder: cfg.Systems.GamesFolder,
			SetCore:     cfg.Systems.SetCore,
		},
	}
}
