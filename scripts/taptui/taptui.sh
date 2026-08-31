#!/usr/bin/env bash
# shellcheck disable=SC2094 # Dirty hack avoid runcommand to steal stdout

# TapTo
# Copyright (C) 2023 Sigurd Bøe
#
# This file is part of TapTo.
#
# TapTo is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# TapTo is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with TapTo.  If not, see <http://www.gnu.org/licenses/>.

title="TapTo"
scriptdir="$(dirname "$(readlink -f "${0}")")"
version="0.6"
fullFileBrowser="false"
basedir="/media"
sdroot="${basedir}/fat"
searchCommand="${scriptdir}/search.sh"
nfcCommand="${scriptdir}/tapto.sh"
settings="${scriptdir}/tapto.ini"
map="${sdroot}/nfc.csv"
amiiboApi="https://www.amiiboapi.com/api"
amiiboAPICache="${sdroot}/Scripts/.config/tapto/amiibo.json"
#For debugging purpouse
[[ -d "${sdroot}" ]] || map="${scriptdir}/nfc.csv"
[[ -d "${sdroot}" ]] && PATH="${sdroot}/linux:${sdroot}/Scripts:${PATH}"
mapHeader="match_uid,match_text,text"
taptoAPI="${taptoAPI:=localhost:7497/api/v1}"
remoteConnection="false"
logFile="/var/log/tapto/tapto.log"
# Match MiSTer theme
[[ -f "${sdroot}/Scripts/.dialogrc" ]] && export DIALOGRC="${sdroot}/Scripts/.dialogrc"
#dialog escape codes, requires --colors
# shellcheck disable=SC2034
black="\Z0" red="\Z1" green="\Z2" yellow="\Z3" blue="\Z4" magenta="\Z5" cyan="\Z6" white="\Z7" bold="\Zb" unbold="\ZB" reverse="\Zr" unreverse="\ZR" underline="\Zu" noUnderline="\ZU" reset="\Zn"

cmdPalette=(
  "launch.system"  "Launch a system"
  "launch.random"  "Launch a random game for a system"
  "mister.ini"     "Change to the specified MiSTer ini file"
  "http.get"       "Perform an HTTP GET request to the specified URL"
  "http.post"      "Perform an HTTP POST request to the specified URL"
  "input.key"      "Press a key on the keyboard"
  "input.coinp1"   "Insert a coin/credit for player 1"
  "input.coinp2"   "Insert a coin/credit for player 2"
  "input.coinp3"   "Insert a coin/credit for player 3"
  "input.coinp4"   "Insert a coin/credit for player 4"
  "delay"          "Delay next command by specified milliseconds"
  "shell"          "Run Linux command"
)

nfcjokes=(
"Why did the NFC tag break up with the Wi-Fi router?
  Because it wanted a closer connection!"
"What did the smartphone say to the NFC tag?
  I'm just a tap away!"
"Why did the smartphone break up with the NFC tag?
  Because it felt like it didn't have enough range!"
"Why does the NFC tag need to be scanned to be updated?
  Because it needs to touch base!"
"What did the NFC reader say to the card?
  Tag! You're it!"
"I wanted to tell you an Amiibo joke,
  but they were all out of stock!"
)

keycodes=(
  "Esc"              "1"
  "1"                "2"
  "2"                "3"
  "3"                "4"
  "4"                "5"
  "5"                "6"
  "6"                "7"
  "7"                "8"
  "8"                "9"
  "9"                "10"
  "0"                "11"
  "Minus"            "12"
  "Equal"            "13"
  "Backspace"        "14"
  "Tab"              "15"
  "Q"                "16"
  "W"                "17"
  "E"                "18"
  "R"                "19"
  "T"                "20"
  "Y"                "21"
  "U"                "22"
  "I"                "23"
  "O"                "24"
  "P"                "25"
  "Leftbrace"        "26"
  "Rightbrace"       "27"
  "Enter"            "28"
  "Leftctrl"         "29"
  "A"                "30"
  "S"                "31"
  "D"                "32"
  "F"                "33"
  "G"                "34"
  "H"                "35"
  "J"                "36"
  "K"                "37"
  "L"                "38"
  "Semicolon"        "39"
  "Apostrophe"       "40"
  "Grave"            "41"
  "Leftshift"        "42"
  "Backslash"        "43"
  "Z"                "44"
  "X"                "45"
  "C"                "46"
  "V"                "47"
  "B"                "48"
  "N"                "49"
  "M"                "50"
  "Comma"            "51"
  "Dot"              "52"
  "Slash"            "53"
  "Rightshift"       "54"
  "Kpasterisk"       "55"
  "Leftalt"          "56"
  "Space"            "57"
  "Capslock"         "58"
  "F1"               "59"
  "F2"               "60"
  "F3"               "61"
  "F4"               "62"
  "F5"               "63"
  "F6"               "64"
  "F7"               "65"
  "F8"               "66"
  "F9"               "67"
  "F10"              "68"
  "Numlock"          "69"
  "Scrolllock"       "70"
  "Kp7"              "71"
  "Kp8"              "72"
  "Kp9"              "73"
  "Kpminus"          "74"
  "Kp4"              "75"
  "Kp5"              "76"
  "Kp6"              "77"
  "Kpplus"           "78"
  "Kp1"              "79"
  "Kp2"              "80"
  "Kp3"              "81"
  "Kp0"              "82"
  "Kpdot"            "83"
  "Zenkakuhankaku"   "85"
  "102Nd"            "86"
  "F11"              "87"
  "F12"              "88"
  "Ro"               "89"
  "Katakana"         "90"
  "Hiragana"         "91"
  "Henkan"           "92"
  "Katakanahiragana" "93"
  "Muhenkan"         "94"
  "Kpjpcomma"        "95"
  "Kpenter"          "96"
  "Rightctrl"        "97"
  "Kpslash"          "98"
  "Sysrq"            "99"
  "Rightalt"         "100"
  "Linefeed"         "101"
  "Home"             "102"
  "Up"               "103"
  "Pageup"           "104"
  "Left"             "105"
  "Right"            "106"
  "End"              "107"
  "Down"             "108"
  "Pagedown"         "109"
  "Insert"           "110"
  "Delete"           "111"
  "Macro"            "112"
  "Mute"             "113"
  "Volumedown"       "114"
  "Volumeup"         "115"
  "Power"            "116" #ScSystemPowerDown
  "Kpequal"          "117"
  "Kpplusminus"      "118"
  "Pause"            "119"
  "Scale"            "120" #AlCompizScale(Expose)
  "Kpcomma"          "121"
  "Hangeul"          "122"
  "Hanja"            "123"
  "Yen"              "124"
  "Leftmeta"         "125"
  "Rightmeta"        "126"
  "Compose"          "127"
  "Stop"             "128" #AcStop
  "Again"            "129"
  "Props"            "130" #AcProperties
  "Undo"             "131" #AcUndo
  "Front"            "132"
  "Copy"             "133" #AcCopy
  "Open"             "134" #AcOpen
  "Paste"            "135" #AcPaste
  "Find"             "136" #AcSearch
  "Cut"              "137" #AcCut
  "Help"             "138" #AlIntegratedHelpCenter
  "Menu"             "139" #Menu(ShowMenu)
  "Calc"             "140" #AlCalculator
  "Setup"            "141"
  "Sleep"            "142" #ScSystemSleep
  "Wakeup"           "143" #SystemWakeUp
  "File"             "144" #AlLocalMachineBrowser
  "Sendfile"         "145"
  "Deletefile"       "146"
  "Xfer"             "147"
  "Prog1"            "148"
  "Prog2"            "149"
  "Www"              "150" #AlInternetBrowser
  "Msdos"            "151"
  "Coffee"           "152" #AlTerminalLock/Screensaver
  "Direction"        "153"
  "Cyclewindows"     "154"
  "Mail"             "155"
  "Bookmarks"        "156" #AcBookmarks
  "Computer"         "157"
  "Back"             "158" #AcBack
  "Forward"          "159" #AcForward
  "Closecd"          "160"
  "Ejectcd"          "161"
  "Ejectclosecd"     "162"
  "Nextsong"         "163"
  "Playpause"        "164"
  "Previoussong"     "165"
  "Stopcd"           "166"
  "Record"           "167"
  "Rewind"           "168"
  "Phone"            "169" #MediaSelectTelephone
  "Iso"              "170"
  "Config"           "171" #AlConsumerControlConfiguration
  "Homepage"         "172" #AcHome
  "Refresh"          "173" #AcRefresh
  "Exit"             "174" #AcExit
  "Move"             "175"
  "Edit"             "176"
  "Scrollup"         "177"
  "Scrolldown"       "178"
  "Kpleftparen"      "179"
  "Kprightparen"     "180"
  "New"              "181" #AcNew
  "Redo"             "182" #AcRedo/Repeat
  "F13"              "183"
  "F14"              "184"
  "F15"              "185"
  "F16"              "186"
  "F17"              "187"
  "F18"              "188"
  "F19"              "189"
  "F20"              "190"
  "F21"              "191"
  "F22"              "192"
  "F23"              "193"
  "F24"              "194"
  "Playcd"           "200"
  "Pausecd"          "201"
  "Prog3"            "202"
  "Prog4"            "203"
  "Dashboard"        "204" #AlDashboard
  "Suspend"          "205"
  "Close"            "206" #AcClose
  "Play"             "207"
  "Fastforward"      "208"
  "Bassboost"        "209"
  "Print"            "210" #AcPrint
  "Hp"               "211"
  "Camera"           "212"
  "Sound"            "213"
  "Question"         "214"
  "Email"            "215"
  "Chat"             "216"
  "Search"           "217"
  "Connect"          "218"
  "Finance"          "219" #AlCheckbook/Finance
  "Sport"            "220"
  "Shop"             "221"
  "Alterase"         "222"
  "Cancel"           "223" #AcCancel
  "Brightnessdown"   "224"
  "Brightnessup"     "225"
  "Media"            "226"
  "Switchvideomode"  "227" #CycleBetweenAvailableVideo
  "Kbdillumtoggle"   "228"
  "Kbdillumdown"     "229"
  "Kbdillumup"       "230"
  "Send"             "231" #AcSend
  "Reply"            "232" #AcReply
  "Forwardmail"      "233" #AcForwardMsg
  "Save"             "234" #AcSave
  "Documents"        "235"
  "Battery"          "236"
  "Bluetooth"        "237"
  "Wlan"             "238"
  "Uwb"              "239"
  "Unknown"          "240"
  "VideoNext"        "241" #DriveNextVideoSource
  "VideoPrev"        "242" #DrivePreviousVideoSource
  "BrightnessCycle"  "243" #BrightnessUp,AfterMaxIsMin
  "BrightnessZero"   "244" #BrightnessOff,UseAmbient
  "DisplayOff"       "245" #DisplayDeviceToOffState
  "Wimax"            "246"
  "Rfkill"           "247" #KeyThatControlsAllRadios
  "Micmute"          "248" #Mute/UnmuteTheMicrophone

## Valid uinput keycodes, not supported, may be supported in the future
#  "ButtonGamepad"      "0x130"
#
#  "ButtonSouth"        "0x130" # A / X
#  "ButtonEast"         "0x131" # X / Square
#  "ButtonNorth"        "0x133" # Y / Triangle
#  "ButtonWest"         "0x134" # B / Circle
#
#  "ButtonBumperLeft"   "0x136" # L1
#  "ButtonBumperRight"  "0x137" # R1
#  "ButtonTriggerLeft"  "0x138" # L2
#  "ButtonTriggerRight" "0x139" # R2
#  "ButtonThumbLeft"    "0x13d" # L3
#  "ButtonThumbRight"   "0x13e" # R3
#
#  "ButtonSelect"       "0x13a"
#  "ButtonStart"        "0x13b"
#
#  "ButtonDpadUp"       "0x220"
#  "ButtonDpadDown"     "0x221"
#  "ButtonDpadLeft"     "0x222"
#  "ButtonDpadRight"    "0x223"
#
#  "ButtonMode"         "0x13c" # This is the special button that usually bears the Xbox or Playstation logo
)

jsonTemplate='{"key":"value"}'

xformTemplate='key1=value1&key2=value2'

_depends() {
  if ! [[ -x "$(command -v dialog)" ]]; then
    echo "dialog not installed." >"$(tty)"
    sleep 10
    _exit 1
  fi

  if ! [[ -x "${nfcCommand}" ]]; then
    if ! _yesno "${nfcCommand} not found\n\nRead more at ${underline}github.com/wizzomafizzo/tapto${noUnderline}\n\nContinue" --colors --defaultno; then
      _exit 1
    fi
  fi

  _taptoStatus

  # Optional dependency, use ripgrep if available
  [[ -x "$(command -v rg)" ]] && grep() { rg "${@}"; }
}

_taptoStatus() {
  if taptoStatus="$(_tapto)"; then
    nfcStatus="true"
    msg="Service: Enabled"
    nfcReadingStatus="$(jq -r '.launching' <<< "${taptoStatus}")"
    nfcReader="$(jq -r '.reader' <<< "${taptoStatus}")"
    jq -e '.connected' <<< "${nfcReader}" >/dev/null && msg="${msg} | Connected Reader: $( jq -r '.type' <<< "${nfcReader}")"
    # Disable reading for the duration of the script
    # we trap the EXIT signal and execute the _exit() function to turn it on again
    _tapto PUT settings '{"launching":false}'
  else
    nfcStatus="false"
    msg="Service: Unavailable"
    nfcReadingStatus="false"
    while true; do
      _yesno "Warning! \n\nTapto Service is not running. Enable?" \
        --cancel-label "Remote Con." --no-label "Remote Con." \
        --extra-button --extra-label "Exit" \
        --help-button --help-label "Continue" \
        --colors
      case "${?}" in
        0) # YES button
          "${nfcCommand}" -service start || { _error "Failed to start service"; continue; }
          sleep 3
          "${FUNCNAME[0]}"; return #Relaunch function to update status
          ;;
        3|255) # NO button and Ctrl+C
          _exit
          ;;
        1) # Remote Con. button
          _yesno "Warning! \n\n Experimental feature, continue?" || _exit
          remoteAddress="$(_inputbox "Enter address of MiSTer device to connect to:" "")"
          export taptoAPI="${remoteAddress}:7497/api/v1"
          _tapto &>/dev/null || { _error "Unable to connect to remote: ${remote}"; continue; }
          remoteConnection="true"
          "${FUNCNAME[0]}"; return #Relaunch function to update status
          ;;
        2) # Continue button
          break
          ;;
      esac
    done
  fi
}

main() {
  export selected
  menuOptions=(
    "Read"      "Read NFC tag contents"
    "Write"     "Write game or command to NFC tag"
    "Mappings"  "Edit the mappings database"
    "Settings"  "Options for NFC script"
    "About"     "About this program"
  )


  selected="$(_menu \
    --cancel-label "Exit" --colors \
    --default-item "${selected}" \
    -- "${menuOptions[@]}")"

}

_Read() {
  local nfcType nfcSCAN nfcUID nfcTXT mappedMatch message amiibo amiiboName amiiboGameSeries amiiboCharacter amiiboVariation amiiboType amiiboSeries nolabel

  nfcSCAN="$(_readTag)"
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
  nfcType="$(jq -r '.type' <<< "${nfcSCAN}" )"
  nfcTXT="$(jq -r '.text' <<< "${nfcSCAN}" )"
  nfcData="$(jq -r '.data' <<< "${nfcSCAN}" )"
  nfcUID="$(jq -r '.uid' <<< "${nfcSCAN}" )"
  read -rd '' message <<_EOF_
${bold}Tag type:${unbold} ${nfcType}
${bold}Tag UID:${unbold} ${nfcUID}
${bold}Tag contents:${unbold}
${nfcTXT}
_EOF_
  if [[ "${nfcType}" == 'Amiibo' ]]; then
    amiibo="${nfcData}"
    amiiboVariation="${amiibo:4:2}"
    [[ "${amiiboVariation}" == "ff" ]] && amiiboVariation="Skylander"
    amiibo="$(_amiibo | jq -r --arg head_val "${amiibo:0:-8}" --arg tail_val "${amiibo:8}" '.amiibo[] | select(.head == $head_val and .tail == $tail_val)')"
    amiiboName="$(jq -r '.name' <<< "${amiibo}")"
    [[ "${?}" -ge 1 ]] && amiiboName="${nfcTXT}"
    amiiboGameSeries="$(jq -r '.gameSeries' <<< "${amiibo}")"
    amiiboCharacter="$(jq -r '.character' <<< "${amiibo}")"
    amiiboType="$(jq -r '.type' <<< "${amiibo}")"
    amiiboSeries="$(jq -r '.amiiboSeries' <<< "${amiibo}")"
    unset message
    read -rd '' message <<_EOF_
${bold}Tag type:${unbold} ${nfcType}
${bold}Tag UID:${unbold} ${nfcUID}
${bold}Amiibo:${unbold}
  ${bold}Name:${unbold}           ${amiiboName}
  ${bold}Game Series:${unbold}    ${amiiboGameSeries}
  ${bold}Character:${unbold}      ${amiiboCharacter}
  ${bold}Variation:${unbold}      ${amiiboVariation}
  ${bold}Type:${unbold}           ${amiiboType}
  ${bold}Amiibo Series:${unbold}  ${amiiboSeries}
_EOF_
  fi
  [[ -f "${map}" ]] && mappedMatch="$(grep -i "^${nfcUID}" "${map}")"
  [[ -n "${mappedMatch}" ]] && read -rd '' message <<_EOF_
${message}

${bold}Mapped match by UID:${unbold}
${mappedMatch}
_EOF_

  [[ -f "${map}" ]] && matchedEntry="$(_searchMatchText "${nfcTXT:-${nfcData}}")"
  [[ -n "${matchedEntry}" ]] && read -rd '' message <<_EOF_
${message}

${bold}Mapped match by match_text:${unbold}
${matchedEntry}
_EOF_
  [[ -n "${nfcSCAN}" ]] && _yesno "${message}" \
    --colors --no-collapse \
    --ok-label "OK" --yes-label "OK" \
    --no-label "Re-Map" --cancel-label "Re-Map" \
    --extra-button --extra-label "Clone Tag" \
    --help-button --help-label "Copy to Map"
  case "${?}" in
    1)
      # No button with "Re-Map" label
      [[ "${nfcType}" == "Amiibo" ]] && nolabel="Amiibo"
      if _yesno "Remap by" \
        --ok-label "UID" --yes-label "UID" \
        --no-label "${nolabel:=Match Text}" --cancel-label "${nolabel}"
      then
        # OK button (Label UID)
        _writeTextToMap --uid "${nfcUID}" "$(_commandPalette)"
      else
        # No button (Label Match Text or Amiibo)
        if [[ "${nfcType}" == "Amiibo" ]]; then
          _writeTextToMap --matchText "$(_amiiboRegex "${amiibo}")" "$(_commandPalette)"
        else
          _writeTextToMap --matchText "${nfcTXT}" "$(_commandPalette)"
        fi
      fi
      ;;
    3)
      # Extra button with "Clone Tag" label
      _writeTag "${nfcTXT}"
      ;;
    4)
      # Help button with "Copy to Map" label
      _map "${nfcUID}" "" "${nfcTXT}"
      ;;
  esac
}

_Write() {
  local message txtSize text
  # We can decide text via environment, and we can extend the command via argument
  # but since extending the command is done recursively it inherits the environemnt
  # so we do this check
  if [[ -z "${text}" ]] || [[ -n "${1}" ]]; then
    text="${1}$(_commandPalette)"
  fi
  [[ "${?}" -eq 1 || "${?}" -eq 255 ]] && return
  txtSize="$(echo -n "${text}" | wc --bytes)"
  read -rd '' message <<_EOF_
The following file or command will be written:

${text:0:144}${blue}${text:144:504}${green}${text:504:716}${yellow}${text:716:888}${red}${text:888}${reset}

The NFC tag needs to be able to fit at least ${txtSize} bytes.
Common tag sizes:
NTAG213     144 bytes storage
${blue}NTAG215    504 bytes storage
${green}MIFARE Classic 1K  716 bytes storage
${yellow}NTAG216    888 bytes storage
${red}Text over this size will be colored red.${reset}
_EOF_
  _yesno "${message}" --colors --ok-label "Write to Tag" --yes-label "Write to Tag" \
    --extra-button --extra-label "Write to Map" \
    --no-label "Cancel" --help-button --help-label "Chain Commands"
  answer="${?}"
  [[ -z "${text}" ]] && { _msgbox "Nothing selected for writing." ; return ; }
  case "${answer}" in
    0)
      # Yes button (Write to Tag)
      # if allow_commands is not set to yes (default no), and if text either starts with "**command:" or if it contains "||**command:" display an error instead of writing to tag
      if ! grep -q "^allow_commands=yes" "${settings}" && [[ "${text}" =~ (^\\*\\*|\\|\\|\\*\\*)command:* ]]; then
        _yesno "You are trying to write a linux command to a physical tag.\nWriting system commands to NFC tags is disabled.\nThis can be enabled in the Settings\n\nOffending command:\n${text}" --yes-label "Write Anyway" --no-label "Back" --defaultno || { text="${text}" "${FUNCNAME[0]}" ; return ; }
      fi
      _writeTag "${text}" || { text="${text}" "${FUNCNAME[0]}" ; return ; }
      ;;
    2)
      # Help Button (Chain Commands)
      _Write "${text}||"
      ;;
    3)
      # Extra button (Write to Map)
      _writeTextToMap "${text}" || { text="${text}" "${FUNCNAME[0]}" ; return ; }
      ;;
    1|255)
      return
      ;;
  esac
}

# Gives the user the ability to enter text manually, pick a file, or use a command palette
# Usage: _commandPalette [-r]
# Returns a text string
# Example: text="$(_commandPalette)"
_commandPalette() {
  local menuOptions selected recursion fileSelected gamePath
  menuOptions=(
    "Pick"      "Pick a game, core or arcade file (supports .zip files)"
    "Commands"  "Craft a custom command using the command palette"
    "Search"    "Search for a game"
    "Input"     "Input text manually (requires a keyboard)"
  )

  selected="$(_menu \
    --cancel-label "Back" \
    --default-item "${selected}" \
    -- "${menuOptions[@]}" )"
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"

  case "${selected}" in
    Input)
      inputText="$( _inputbox "Replace match text" "${match_text}")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"

      echo "${inputText}"
      ;;
    Pick)
      "${remoteConnection}" && { fileSelected="$(_gameBrowser)" ; return; }
      fileSelected="$(_fselect "$(_gameLocation)")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
      #[[ ! -f "${fileSelected//.zip\/*/.zip}" ]] && { _error "No file was selected." ; return ; }
      # shellcheck disable=SC2001
      fileSelected="$(sed -E "s#/media/(usb[0-7]|fat|network)(/cifs)?(/games)?/##i" <<< "${fileSelected}")"

      echo "${fileSelected}"
      ;;
    Commands)
      text="$(recursion="${recursion}" _craftCommand)"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      echo "${text}"
      ;;
    Search)
      gamePath="$("${searchCommand}" -print 2>&1 >"$(tty)")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      gamePath="$(sed -E "s#/media/(usb[0-7]|fat|network)(/cifs)?(/games)?/##i" <<< "${gamePath}")"
      [[ -z "${gamePath}" ]] && { "${FUNCNAME[0]}" ; return ; }
      echo "${gamePath}"
      ;;
  esac

}

# Build a command using a command palette
# Usage: _craftCommand
_craftCommand(){
  local command selected system systems recursion ms bulletList contentType tempFile postData categories category
  readarray -t categories < <(_tapto systems | jq -r '.systems[] | .category' | sort -u | sed 's/.*/&\nCategory/')
  command="**"
  selected="$(_menu \
    --cancel-label "Back" \
    -- "${cmdPalette[@]}" )"
  exitcode="${?}"
  [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"

  command="${command}${selected}"

  case "${selected}" in
    launch.system)
      category="$(_menu \
        --backtitle "${title}" \
        -- "${categories[@]}" )"
      readarray -t systems < <(_tapto systems | jq -r  ".systems[] | select(.category == \"${category}\") | .id + \"\n\" + .name")
      system="$(_menu \
        --backtitle "${title}" \
        -- "${systems[@]}" )"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      command="${command}:${system}"
      ;;
    launch.random)
      category="$(_menu \
        --backtitle "${title}" \
        -- "${categories[@]}" )"
      readarray -t systems < <(_tapto systems | jq -r  ".systems[] | select(.category == \"${category}\") | .id + \"\n\" + .name")
      system="$(_menu \
        --backtitle "${title}" \
        -- "${systems[@]}" )"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      while true; do
        read -rd '' message <<_EOF_
Would you like to add more systems to the NFC tag?

Current random systems:
$(IFS=',' read -ra bulletList <<< "${system}"; printf "* %s\n" "${bulletList[@]}")
_EOF_
        _yesno "${message}" --no-label "Done" --cancel-label "Done" || break
        category="$(_menu \
          --backtitle "${title}" \
          -- "${categories[@]}" )"
        readarray -t systems < <(_tapto systems | jq -r  ".systems[] | select(.category == \"${category}\") | .id + \"\n\" + .name")
        system="${system},$(msg="${system}" _menu \
          --backtitle "${title}" \
          -- "${systems[@]}" )"
        exitcode="${?}"
        system="$(tr ',' '\n' <<< "${system}" | sort -u | tr '\n' ',')"
        system="${system#,}"
        system="${system%,}"
        [[ "${exitcode}" -ge 1 ]] && break
      done
      command="${command}:${system}"
      ;;
    mister.ini)
      ini="$(_radiolist -- \
        1 one on 2 two off 3 three off 4 four off )"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      command="${command}:${ini}"
      ;;
    http.get)
      http="$(_inputbox "Enter URL for GET" "https://")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      # use wget to encode the url
      http="$(wget --spider "${http}" 2>&1 | awk -F'--  ' '/--  /{print $2; exit}')"
      command="${command}:${http}"
      ;;
    http.post)
      http="$(_inputbox "Enter URL for POST" "https://")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      # use wget to encode the url
      http="$(wget --spider "${http}" 2>&1 | awk -F'--  ' '/--  /{print $2; exit}')"
      # Add content type
      menuOptions=(
          "application/json" ""
          "application/x-www-form-urlencoded" ""
          "custom" ""
        )
      contentType="$(_menu -- "${menuOptions[@]}")"
      # Add data
      if [[ "${contentType}" == "application/json" ]]; then
        tempFile="$(mktemp)"
        jq '.' <<< "${jsonTemplate}" > "${tempFile}"
        _textEditor "${tempFile}"
        postData="$(jq -c '.' "${tempFile}")"
        rm "${tempFile}"
      elif [[ "${contentType}" == "application/x-www-form-urlencoded" ]]; then
        postData="$(_inputbox "Enter POST data" "${xformTemplate}" )"
      else
        contentType="$(_inputbox "Content-Type:" "")"
        postData="$(_inputbox "Enter POST data" "" )"
      fi

      command="${command}:${http},${contentType},${postData}"
      ;;
    input.key)
      key="$(_menu -- "${keycodes[@]}")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
      for ((i=0; i<${#keycodes[@]}; i++)); do
        if [[ "${keycodes[$i]}" == "${key}" ]]; then
          index=$((i + 1))
          #workaround, for the shape of the array
          [[ "${keycodes[$index]}" == "${key}" ]] && index=$(( index + 1))
          key="${keycodes[$index]}"
          break
        fi
      done
      command="${command}:${key}"
      ;;
    input.coinp*)
      while true; do
        coin="$(_rangebox "Enter number" "1" "99" "1")"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
        [[ "${coin}" == +([0-9]) ]] && break
        _error "${coin} is not a positive number"
      done
      command="${command}:${coin}"
      ;;
    shell)
      while true; do
        linuxcmd="$(_inputbox "Enter Linux command" "reboot" || return )"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
        command -v "${linuxcmd%% *}" >/dev/null && break
        _error "${linuxcmd%% *} from ${linuxcmd} does not seam to be a valid command"
      done
      command="${command}:${linuxcmd}"
      ;;
    delay)
      while true; do
        ms="$(_rangebox "Delay in seconds" "1" "1000" "1")000"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
        [[ "${ms}" == +([0-9]) ]] && break
        _error "${ms} is not a positive number"
      done
      command="${command}:${ms}"
      ;;
  esac
  echo "${command}"

}

_Settings() {
  local menuOptions selected
  menuOptions=(
    "Service"         "Start/stop the TapTo service"
    "Commands"        "Toggles the ability to run Linux commands from NFC tags"
    "Sounds"          "Toggles sounds played when a tag is scanned"
    "Connection"      "Hardware configuration for certain NFC readers"
    "Probe"           "Auto detection of a serial based reader device"
    "Exit Game"       "Exit Game When Token Is Removed"
    "Exit Blocklist"  "Exit Game Core Blocklist"
    "Exit Delay"      "Exit Game Delay"
    "Log Exporter"    "View and export logs"
  )

  while true; do
    selected="$(_menu --cancel-label "Back" -- "${menuOptions[@]}")"
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
    case "${selected}" in
      "Service")        _serviceSetting ;;
      "Commands")       _commandSetting ;;
      "Sounds")         _soundSetting ;;
      "Connection")     _connectionSetting ;;
      "Probe")          _probeSetting ;;
      "Exit Game")      _exitGameSetting ;;
      "Exit Blocklist") _exitGameBlocklistSetting ;;
      "Exit Delay")     _exitGameDelaySetting ;;
      "Log Exporter")   _logExporter ;;
    esac
  done
}

_serviceSetting() {
  local menuOptions selected

  "${remoteConnection}" && { _error "Service control is unavailable when connected remotly"; return; }

  menuOptions=(
    "Enable"   "Enable TapTo service"  "off"
    "Disable"  "Disable TapTo service" "off"
  )
  "${nfcStatus}" && menuOptions[2]="on"
  "${nfcStatus}" || menuOptions[5]="on"

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Enable)
      "${nfcCommand}" -service start || { _error "Unable to start the TapTo service"; return; }
      sleep 3 && _taptoStatus
      _msgbox "The TapTo service started"
      ;;
    Disable)
      "${nfcCommand}" -service stop || { _error "Unable to stop the TapTo service"; return; }
      export nfcStatus="false" msg="Service: Disabled (Menu's may not work)"
      _msgbox "The TapTo service stopped"
      ;;
  esac
}

_commandSetting() {
  local menuOptions selected helpmsg
  menuOptions=(
    "Enable"   "Enable Linux commands"  "off"
    "Disable"  "Disable Linux commands" "off"
  )

  if _tapto settings | jq -e '.allowCommands' >/dev/null; then
    menuOptions[2]="on"
  else
    menuOptions[5]="on"
  fi

  read -rd '' helpmsg <<_EOF_
This option enables the execution of Linux commands stored on NFC tags. If you disable this option, you will be limited to running Linux commands that are stored in the mappings database.

Running Linux commands stored in NFC tags on your MiSTer FPGA device may pose security risks as it opens the door to potential vulnerabilities and unintended consequences. While this approach offers convenience, it lacks the security safeguards inherent in the Mappings database, which is designed to ensure that only known, verified commands are executed. By relying on NFC tags, unauthorized users could potentially create and execute commands with unverified origins, increasing the risk of malicious actions and compromising the integrity of your MiSTer system. Therefore, using the Mappings database remains the more secure option for safeguarding your device and its functionality.
_EOF_

  selected="$(_radiolist --help-button -- "${menuOptions[@]}" )"
  [[ "${?}" -eq 2 ]] && { _msgbox "${helpmsg}" ; "${FUNCNAME[0]}" ; return ; }
  case "${selected}" in
    Enable) _tapto settings PUT '{"allowCommands":true}' ;;
    Disable) _tapto settings PUT '{"allowCommands":false}' ;;
  esac
}

_soundSetting() {
  local menuOptions selected
  menuOptions=(
    "Enable"   "Enable sounds played when a tag is scanned"   "off"
    "Disable"  "Disable sounds played when a tag is scanned"  "off"
  )

  if _tapto settings | jq -e '.disableSounds' >/dev/null; then
    menuOptions[5]="on"
  else
    menuOptions[2]="on"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Enable) _tapto settings PUT '{"disableSounds":false}' ;;
    Disable) _tapto settings PUT '{"disableSounds":true}' ;;
  esac
}

_connectionSetting() {
  local menuOptions selected customString
  menuOptions=(
    "Default"   "Automatically detect hardware (recommended)"               "off"
    "PN532"     "Select this option if you are using a PN532 UART module"   "off"
    "Custom"    "Manually enter a custom connection string"                 "off"
  )

  if [[ -z "$(_tapto settings | jq -r '.connectionString')" ]]; then
    menuOptions[2]="on"
  elif [[ "$(_tapto settings | jq -r '.connectionString')" = "pn532_uart:/dev/ttyUSB0" ]]; then
    menuOptions[5]="on"
  elif [[ -n "$(_tapto settings | jq -r '.connectionString')" ]]; then
    menuOptions[8]="on"
    customString="$(_tapto settings | jq -r '.connectionString')"
    menuOptions[7]="Current custom option: ${customString}"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Default) _tapto settings PUT '{"connectionString":""}' ;;
    PN532) _tapto settings PUT '{"connectionString":"pn532_uart:/dev/ttyUSB0"}' ;;
    Custom)
      customString="$(_inputbox "Enter connection string" "${customString}")"
      _tapto settings PUT "{\"connectionString\":\"${customString}\"}"
      ;;
  esac
}

_probeSetting() {
  local menuOptions selected
  menuOptions=(
    "Enable"   "Enable detection of a serial based reader device"   "off"
    "Disable"  "Disable detection of a serial based reader device"  "off"
  )

  if _tapto settings | jq -e '.probeDevice' >/dev/null; then
    menuOptions[2]="on"
  else
    menuOptions[5]="on"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Enable) _tapto settings PUT '{"probeDevice":true}' ;;
    Disable) _tapto settings PUT '{"probeDevice":false}' ;;
  esac
}

_exitGameSetting() {
    local menuOptions selected
  menuOptions=(
    "Insert"  "Return to menu core when the card is removed"        "off"
    "Tap"     "Do not return to menu core when the card is removed" "off"
  )

  if _tapto settings | jq -e '.exitGame' >/dev/null; then
    menuOptions[2]="on"
  else
    menuOptions[5]="on"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Insert) _tapto settings PUT '{"exitGame":true}' ;;
    Tap) _tapto settings PUT '{"exitGame":false}' ;;
  esac
}

_exitGameBlocklistSetting() {
  local menuOptions selected customString state
  menuOptions=(
    "Disable"   "All cores will exit when a card is removed"  "off"
    "Enable"    "Enter a custom list of core names"  "off"
  )

  state="$(_tapto settings)"

  if [[ -z "$(jq -r '.exitGameBlocklist[]' <<< "${state}" )" ]]; then
    menuOptions[2]="on"
  else
    menuOptions[5]="on"
    customString="$(jq -r '.exitGameBlocklist | @csv' <<< "${state}")"
    customString="${customString//\"}"
    menuOptions[4]="Enter a custom list of core names, current value: ${customString}"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Disable) _tapto settings PUT '{"exitGameBlocklist":[]}' ;;
    Enable)
      customString="$(_inputbox "Enter core list, comma separated (SNES, GENESIS)" "${customString}")"
      customString="\"${customString//,/\",\"}\""
      _tapto settings PUT "{\"exitGameBlocklist\":[${customString}]}"
      ;;
  esac
}

_exitGameDelaySetting() {
  local menuOptions selected currentSetting delayInSeconds exitcode state
  menuOptions=(
    "Disable"   "Set the delay to 0"               "off"
    "Enable"    "Enter a custom delay in seconds"  "off"
  )

  state="$(_tapto settings)"

  if [[ "$(jq -r '.exitGameDelay' <<< "${state}" )" == 0 ]]; then
    menuOptions[2]="on"
  else 
    menuOptions[5]="on"
    currentSetting="$(jq -r '.exitGameDelay' <<< "${state}")"
    menuOptions[4]="Change the delay in seconds, current value: ${currentSetting}"
  fi

  selected="$(_radiolist -- "${menuOptions[@]}" )"
  case "${selected}" in
    Disable) _tapto settings PUT '{"exitGameDelay":0}' ;;
    Enable)
      while true; do
        delayInSeconds="$(_rangebox "Delay in seconds" "0" "60" "${currentSetting}")"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }
        [[ "${delayInSeconds}" == +([0-9]*) ]] && break
        _error "${delayInSeconds} is not a positive number"
      done
      _tapto settings PUT "{\"exitGameDelay\":${delayInSeconds}"
      ;;
  esac
}

_logExporter() {
  local renderedLog exitcode link
  "${remoteConnection}" && { _error "Log management is unavailable when connected remotly"; return; }
  renderedLog="$(_logRender "${logFile}")"
  [[ "${?}" -ge 1 ]] && { _error "Unable to load logs"; return; }
  renderedLog="$(tail -n100 <<<"${renderedLog}")"
  _msgbox "${renderedLog}" --colors \
  --extra-button --extra-label "Export to file" \
  --help-button --help-label "Share Link"
  exitcode="${?}"
  case "${exitcode}" in
    3) # Export to File
      logExport="${sdroot}/tapto.$(date '+%Y-%m-%d_%H-%M-%S').log"
      cp "${logFile}" "${logExport}"
      _msgbox "Log saved to the root of the SD card:\n\n${logExport}"
      ;;
    2) # Share Link
      link="$(nc termbin.com 9999 < "${logFile}")"
      _msgbox "Share the following link:\n\n${link}"
      ;;
  esac
}

_logRender() {
  local output if="${1}"
  output="$(jq -r --arg red "$red" --arg green "$green" --arg yellow "$yellow" --arg reset "$reset" '
    .time as $time |
    .time = ( $time | strftime("%Y-%m-%d %H:%M:%S") ) |
    .level as $level |
    .color = (
      if $level == "error" then $red
      elif $level == "warning" then $yellow
      elif $level == "info" then $green
      else "" end
    ) |
    "\(.time) [\(.color)\(.level)\($reset)] \(.message)"
      ' "${if}")"
  echo "${output}"
}

_About() {
  local about
  read -rd '' about <<_EOF_
${bold}${title}${unbold}
${version}
A tool for making and working with NFC tags on MiSTer FPGA

Whats New? Get involved? Need help?
  ${underline}github.com/wizzomafizzo/tapto${noUnderline}

${nfcjokes[$((RANDOM % ${#nfcjokes[@]}))]}

Gaz            ${underline}github.com/symm${noUnderline}
Wizzo          ${underline}github.com/wizzomafizzo${noUnderline}
Ziggurat       ${underline}github.com/sigboe${noUnderline}
Andrea Bogazzi ${underline}github.com/asturur${noUnderline}

License: GPL v3.0
  ${underline}github.com/wizzomafizzo/tapto/blob/main/LICENSE${noUnderline}
_EOF_
  _msgbox "${about}" --no-collapse --colors --title "About"
}

# dialog --fselect broken out to a function,
# the purpouse is that
# if the screen is smaller then what --fselec can handle
# I can do somethig else
# Usage: _fselect "${fullPath}"
# returns the file that is selected including the full path, if full path is used.
_fselect() {
  local termh windowh relativeComponents selected fullPath newDir defaultLabel
  fullPath="${1}"
  if [[ -f "${fullPath}" ]] && [[ -h "${fullPath}" ]]; then
      read -rd '' message <<_EOF_
The following file is a symlink:
${fullPath}
The path is $(echo -n "${fullPath}" | wc --bytes) bytes


Do you want to use the symlink, or the path to the actual file?
$(readlink -f "${fullPath}")
The path is $(echo -n "$(readlink -f "${fullPath}")" | wc --bytes) bytes
_EOF_
    # Make the default answer be the shortest string
    [[ "$(echo -n "${fullPath}" | wc --bytes)" -lt "$(echo -n "$(readlink -f "${fullPath}")" | wc --bytes)" ]] && defaultLabel="--defaultno"

    if _yesno "${message}" --yes-label "Use Real Path" --no-label "Use Link Path" ${defaultLabel}; then
      readlink -f "${fullPath}"
      return
    else
      echo "${fullPath}"
      return
    fi
  elif [[ ! -d "${fullPath}" ]]; then
    echo "${fullPath}"
    return
  fi
  termh="$(tput lines)"
  ((windowh = "${termh}" - 10))
  [[ "${windowh}" -gt "22" ]] && windowh="22"
  if "${fullFileBrowser}" && [[ "${windowh}" -ge "8" ]]; then
    dialog \
      --backtitle "${title}" \
      --title "${fullPath}" \
      --fselect "${fullPath}/" \
      "${windowh}" 77 3>&1 1>&2 2>&3 >"$(tty)"
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"

  else
    # in case of a very tiny terminal window
    # make an array of the filenames and put them into --menu instead
    relativeComponents=(
      "goto"  "Go to directory (keyboard required)"
      ".."    "Up one directory"
    )

    readarray -t currentDirContents <<< "$( \
      find "${fullPath}" -mindepth 1 -maxdepth 1 \
      \( -type d -printf '%P\tDirectory\n' \) \
      -o \( -type f -printf '%P\tFile\n' \) | sort -t$'\t' -k 2,2 -k 1,1 | tr '\t' '\n')"

    selected="$(msg="Pick a game" \
      _menu  --title "${fullPath}" -- "${relativeComponents[@]}" "${currentDirContents[@]}" )"
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"

    case "${selected,,}" in
    "goto")
      newDir="$(_inputbox "Input a directory to go to" "${fullPath}")"
      _fselect "${newDir%/}"
      ;;
    "..")
      _fselect "${fullPath%/*}"
      ;;
    *.zip)
      zippedPath="$(_browseZip "${fullPath}/${selected}")"
      if [[ -n "${zippedPath}" ]]; then
        echo "${fullPath}/${selected}/${zippedPath}"
      else
        _fselect "${fullPath}"
      fi
      ;;
    *)
      _fselect "${fullPath}/${selected}"
      ;;
    esac

  fi

}

# Browse contents of zip file as if it was a folder
# Usage: _browseZip "file.zip"
# returns a file path of a file inside the zip file
_browseZip() {
  local zipFile currentDir relativeComponents currentDirDirs currentDirFiles tmpFile
  zipFile="${1}"
  currentDir=""

  relativeComponents=(
    ".." "Up one directory"
  )
  tmpFile="$(mktemp -t "$(basename "${zipFile}").XXXXXXXXXX")"
  trap 'rm "${tmpFile}"; _exit' SIGINT # Trap Ctrl+C (SIGINT) to clean up tmp file
  _infobox "Loading."
  zip -sf "${zipFile}" > "${tmpFile}"

  while true; do

    readarray -t currentDirDirs <<< "$( \
      grep -x "^  ${currentDir}[^/]*/$"  "${tmpFile}" |
      while read -r line; do
        line="${line#  }"
        echo -e "${line#"$currentDir"}\nDirectory"
      done )"
    [[ "${#currentDirDirs[@]}" -le "1" ]] && unset currentDirDirs

    readarray -t currentDirFiles <<< "$( \
      grep -x "^  ${currentDir}[^[:space:]][^/]*" "${tmpFile}" |
      while read -r line; do
        line="${line#  }"
        echo -e "${line#"$currentDir"}\nFile"
      done )"
    [[ "${#currentDirFiles[@]}" -le "1" ]] && unset currentDirFiles

    selected="$(msg="${currentDir}" _menu --backtitle "${title}" \
      --title "${zipFile}" -- "${relativeComponents[@]}" "${currentDirDirs[@]}" "${currentDirFiles[@]}")"
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { rm "${tmpFile}" ; return "${exitcode}" ; }

    case "${selected,,}" in
    "..")
      [[ -z "${currentDir}" ]] && break
      [[ "${currentDir%/}" != *"/"* ]] && currentDir=""
      [[ -n "${currentDir}" ]] && currentDir="${currentDir%/*/}/"
      ;;
    */)
      currentDir="${currentDir}${selected}"
      ;;
    *)
      echo "${currentDir}${selected}"
      break
      ;;
    esac
  done
  rm "${tmpFile}"
}

# Search for game folders, and list them
# Usage: _gameLocation
# Returns: path to selected folder
_gameLocation() {
  local gameLocations tag location item selected exitcode dir
  readarray -t gameLocations <<< "$( \
    find "${basedir}" -maxdepth 3 -type d -iregex '.*/\(usb[0-7]\|fat\|network\)\(/cifs\)?\(/games\|/_Arcade\)' |
      while read -r line; do
        tag="${line#"$basedir/"}"
        location="${tag%/*}"
        location="${location#/}"
        location="${location/fat\/cifs/Network}"
        location="${location/fat/SD Card}"
        item="${tag##*/} (${location%/*})"
        echo -e "${tag}\n${item#_}"
      done )"
  gameLocations=(
    "goto"  "Go to directory (keyboard required)"
    "fat"   "${underline}${bold}SD Card${reset}"
    "${gameLocations[@]}"
    "systems" "browse games by system"
  )

  selected="$(_menu --default-item "${selected:-fat}" --cancel-button "Back" --no-button "Back" --colors -- "${gameLocations[@]}")"
  [[ "${?}" =~ ^(1|255)$ ]] && { "${FUNCNAME[1]}" ; return ; }

  case "${selected}" in
    "goto")
      dir="$(_inputbox "Input a directory to go to" "${sdroot}")"
      echo "${dir%/}"
      ;;
    "systems")
      _gameBrowser
      ;;
    *)
      echo "${basedir}/${selected}"
      ;;
  esac
}

# Browse games using _taptui REST API
# Usage: _gameBrowser
# Returns: path to selected game
_gameBrowser() {
  local relativeComponents categories category systems system tempFile currentDirDirs currentDir currentDirFiles selected
  currentDir=""

  relativeComponents=(
    ".." "Up one directory"
  )
  readarray -t categories < <(_tapto systems | jq -r '.systems[] | .category' | sort -u | sed 's/.*/&\nCategory/')
  category="$(_menu  --backtitle "${title}" --cancel-button "Back" --no-button "Back" -- "${categories[@]}" )"
  [[ "${?}" =~ ^(1|255)$ ]] && { selected="systems" "${FUNCNAME[1]}" ; return ; }
  readarray -t systems < <(_tapto systems | jq -r  ".systems[] | select(.category == \"${category}\") | .id + \"\n\" + .name")
  system="$(_menu  --backtitle "${title}" --cancel-button "Back" --no-button "Back"  -- "${systems[@]}" )"
  [[ "${?}" =~ ^(1|255)$ ]] && { "${FUNCNAME[1]}" ; return ; }
  tmpFile="$(mktemp)"
  trap 'rm "${tmpFile}"; _exit' SIGINT # Trap Ctrl+C (SIGINT) to clean up tmp file
  _infobox "Loading."
  _tapto games "${system}" maxResults=0 | jq -r .results[].path > "${tmpFile}"
  while true; do

    readarray -t currentDirDirs <<< "$( \
      grep -o "^${currentDir}[^/]*/" "${tmpFile}" | sort -u |
      while read -r line; do
        echo -e "${line#"$currentDir"}\nDirectory"
      done )"
    [[ "${#currentDirDirs[@]}" -le "1" ]] && unset currentDirDirs

    readarray -t currentDirFiles <<< "$( \
      grep -x "^${currentDir}[^/]*$" "${tmpFile}" |
      while read -r line; do
        echo -e "${line#"$currentDir"}\nFile"
      done )"
    [[ "${#currentDirFiles[@]}" -le "1" ]] && unset currentDirFiles

    selected="$(msg="${currentDir}" _menu --backtitle "${title}" \
      --title "${system}" -- "${relativeComponents[@]}" "${currentDirDirs[@]}" "${currentDirFiles[@]}")"
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { rm "${tmpFile}" ; return "${exitcode}" ; }

    case "${selected,,}" in
    "..")
      [[ -z "${currentDir}" ]] && { "${FUNCNAME[0]}"; return; }
      [[ "${currentDir%/}" != *"/"* ]] && currentDir=""
      [[ -n "${currentDir}" ]] && currentDir="${currentDir%/*/}/"
      ;;
    */)
      currentDir="${currentDir}${selected}"
      ;;
    *)
      echo "${currentDir}${selected}"
      break
      ;;
    esac
  done
  rm "${tmpFile}"
}

# Map or remap filepath or command for a given NFC tag (written to local database)
# Usage: _map "UID" "Match Text" "Text"
# Values may be empty
_map() {
  local uid match txt
  uid="${1}"
  match="${2}"
  txt="${3}"
  [[ -e "${map}" ]] ||  printf "%s\n" "${mapHeader}" >> "${map}" || { _error "Can't initialize mappings database!" ; return 1 ; }
  [[ -z "${uid}" ]] || { grep -q "^${uid}" "${map}" && sed -i "/^${uid}/d" "${map}" ; }
  printf "%s,%s,%s\n" "${uid}" "${match}" "${txt}" >> "${map}"
}

_Mappings() {
  local selected mappings

  while true; do
    readarray -t mappings < <(_tapto mappings | jq -r '.mappings[] | .id + "\n" + if .label != "" then .label + " (" + .type + ")" else .type + ": " +  .pattern end')
    selected="$(_menu \
      --extra-button --extra-label "New" \
      --cancel-label "Back" --colors \
      --default-item "${selected}" \
      -- "${mappings[@]}" \
      "Legacy" "Legacy CSV mappings")"
    case "${?}" in
      1|255) return ;;
      3) _newMapping ;;
      0) # OK button
        [[ "${selected}" == "Legacy" ]] && { _CSVMappings; continue; }
        _editMapping "${selected}"
        ;;
    esac

  done

}

_newMapping() {
  local nfcSCAN exitcode pattern override
  _yesno "Read tag or type match text?" \
    --ok-label "Read tag" --yes-label "Read tag" \
    --no-label "Amiibo" --cancel-label "Amiibo" \
    --extra-button --extra-label "Match text" \
    --help-button --help-label "Cancel"
  case "${?}" in
    0) # Yes button (Read tag)
      nfcSCAN="$(_readTag)"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
      pattern="$(jq -r '.uid' <<< "${nfcSCAN}" )"
      type="uid" match="exact"
      ;;
    1) # No button (Amiibo)
      pattern="$(_amiiboRegex)"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
      type="data" match="regex"
      ;;
    3) # Extra button (Match text)
      pattern="$(_inputbox "Replace match text" "${match_text}")"
      exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
      type=text match="partial"
      ;;
    2|255) # No button (Cancel)
      return
      ;;
  esac
  override="$(_commandPalette)"
  while true; do
    _yesno "Do you want to chain more commands?\nCurrent Command(s):\n\n${override}" --defaultno || break
    override="${override}||$(_commandPalette)"
  done
  label="$(_inputbox "Enter friendly name" "")"

  _tapto POST mappings "{ \"label\": \"${label}\", \"enabled\": true, \"type\": \"${type}\", \"match\": \"${match}\", \"pattern\": \"${pattern}\", \"override\": \"${override}\" }"
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
}

_editMapping() {
  local id menuOptions selected mapEntry entryLabel entryEnabled entryType entryMatch entryPattern entryOverride
  id="${1}"
  mapEntry="$(_tapto mappings | jq ".mappings[] | select(.id == \"${id}\")")"

  entryLabel="$(jq -r '.label' <<<"${mapEntry}")"
  entryEnabled="$(jq -r '.enabled' <<<"${mapEntry}")"
  entryType="$(jq -r '.type' <<<"${mapEntry}")"
  entryMatch="$(jq -r '.match' <<<"${mapEntry}")"
  entryPattern="$(jq -r '.pattern' <<<"${mapEntry}")"
  entryOverride="$(jq -r '.override' <<<"${mapEntry}")"
  entryDate="$(jq -r '.added' <<<"${mapEntry}")"

  menuOptions=(
    "Label"     "${entryLabel}"
    "Enabled"   "${entryEnabled}"
    "Type"      "${entryType}"
    "Match"     "${entryMatch}"
    "Pattern"   "${entryPattern}"
    "Override"  "${entryOverride}"
    "Write"     "Write text to physical tag"
    "Delete"    "Remove entry from mappings database"
  )

  selected="$(msg="ID: ${id} | Added: $(date -d "${entryDate}")" _menu \
    --cancel-label "Done" \
    -- "${menuOptions[@]}" )" || return
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" "${id}" ; return ; }

  case "${selected}" in
  Label)
    entryLabel="$(_inputbox "Enter a new label (friendly name)" "${entryLabel}")" || return
    _tapto PUT mappings "${id}" "{\"label\": \"${entryLabel}\"}"
    ;;
  Enabled)
    entryEnabled="true"
    _yesno "" \
      --ok-button "Enable" --yes-button "Enable" \
      --no-button "Disable" --cancel-label "Disable" \
      || entryEnabled="false"
    _tapto PUT mappings "${id}" "{\"enabled\": ${entryEnabled}}"
    ;;
  Type)
    entryType="$(_menu -- \
      "uid"   "Match by UID" \
      "text"  "Match by Text" \
      "data"  "Match by Amiibo/Data")" || return
    _tapto PUT mappings "${id}" "{\"type\": \"${entryType}\"}"
    ;;
  Match)
    entryMatch="$(_menu -- \
      "exact"   "Exact match" \
      "partial" "Partial match" \
      "regex"   "Match using regular expression")" || return
    _tapto PUT mappings "${id}" "{\"match\": \"${entryMatch}\"}"
    ;;
  Pattern)
    entryLabel="$(_inputbox \
      "Enter Pattern, or pick Amiibo" "${entryLabel}" \
      --extra-button --extra-label "Amiibo")"
    if [[ "${?}" = "3" ]]; then
      entryLabel="$(_amiiboRegex)" || return
      _tapto PUT mappings "${id}" "{\"match\": \"regex\"}"
    else
      _tapto PUT mappings "${id}" "{\"match\": \"exact\"}"
    fi
    _tapto PUT mappings "${id}" "{\"pattern\": \"${entryPattern}\"}"
    ;;
  Override)
    entryOverride="$(_commandPalette)" || return
    _tapto PUT mappings "${id}" "{\"override\": \"${entryOverride}\"}"
    ;;
  Write)
    text="${entryOverride}" _Write
    return
    ;;
  Delete)
    _tapto DELETE mappings "${id}"
    ;;
  esac
}

_CSVMappings() {
  local oldMap arrayIndex line lineNumber match_uid match_text text menuOptions selected replacement_match_text replacement_match_uid replacement_text message new_match_uid new_text
  unset replacement_match_uid replacement_text

  "${remoteConnection}" && { _error "Legacy Mapping not supported while remotely connected"; return; }

  [[ -e "${map}" ]] || printf "%s\n" "${mapHeader}" >> "${map}" || { _error "Can't initialize mappings database!" ; return 1 ; }

  mapfile -t -O 1 -s 1 oldMap < "${map}"

  mapfile -t arrayIndex < <( _numberedArray "${oldMap[@]}" )
  # We don't want to display additional leading and trailing quotes, as quotes have meaning
  for ((i=0; i<${#arrayIndex[@]}; i++)); do
    arrayIndex[i]=${arrayIndex[i]#\"}
    arrayIndex[i]=${arrayIndex[i]%\"}
  done


  # Display something useful if the file is empty
  [[ "${#arrayIndex[@]}" -eq 0 ]] && arrayIndex=( "File Empty" "" )

  line="$(msg="${mapHeader}" _menu \
    --extra-button --extra-label "New" \
    --cancel-label "Back" \
    -- "${arrayIndex[@]}" )"
  exitcode="${?}"

  # Cancel button (Back) or Esc hit
  [[ "${exitcode}" -eq "1" ]] || [[ "${exitcode}" -eq "255" ]] && return "${exitcode}"

  # Extra button (New) pressed
  if [[ "${exitcode}" == "3" ]]; then
    _yesno "Read tag or type match text?" \
      --ok-label "Read tag" --yes-label "Read tag" \
      --no-label "Amiibo" --cancel-label "Amiibo" \
      --extra-button --extra-label "Match text" \
      --help-button --help-label "Cancel"
    case "${?}" in
      0)
        # Yes button (Read tag)
        nfcSCAN="$(_readTag)"
        new_match_uid="$(jq -r '.uid' <<< "${nfcSCAN}")"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
        # Enclose field in quotes if it's needed to escape a comma
        [[ "${new_match_uid}" == \"*\" ]] || [[ "${new_match_uid}" == *,* ]] && new_match_uid="\"${new_match_uid}\""
        ;;
      1)
        # No button (Amiibo)
        new_match_text="amiibo:$(_amiiboRegex)"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
        # Enclose field in quotes if it's needed to escape a comma
        [[ "${new_match_text}" == \"*\" ]] || [[ "${new_match_text}" == *,* ]] && new_match_text="\"${new_match_text}\""
        ;;
      3)
        # Extra button (Match text)
        new_match_text="$(_inputbox "Replace match text" "${match_text}")"
        exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
        # Enclose field in quotes if it's needed to escape a comma
        [[ "${new_match_text}" == \"*\" ]] || [[ "${new_match_text}" == *,* ]] && new_match_text="\"${new_match_text}\""
        ;;
      2|255)
        # No button (Cancel)
        "${FUNCNAME[0]}"
        return
        ;;
    esac
    [[ -z "${new_text}" ]] && new_text="$(_commandPalette)"
    while true; do
      _yesno "Do you want to chain more commands?" --defaultno || break
      new_text="${new_text}||$(_commandPalette)"
    done
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
    # Enclose field in quotes if it's needed to escape a comma
    [[ "${new_text}" == \"*\" ]] || [[ "${new_text}" == *,* ]] && new_text="\"${new_text}\""
    _map "${new_match_uid}" "${new_match_text}" "${new_text}"
    "${FUNCNAME[0]}"
    return
  fi

  [[ ${line} == "File Empty" ]] && return
  lineNumber=$((line + 1))
  match_uid="$(_parseCSV 1 "${oldMap[$line]}")"
  match_text="$(_parseCSV 2 "${oldMap[$line]}")"
  text="$(_parseCSV 3 "${oldMap[$line]}")"

  menuOptions=(
    "UID"     "${match_uid}"
    "Match"   "${match_text}"
    "Text"    "${text}"
    "Write"   "Write text to physical tag"
    "Delete"  "Remove entry from mappings database"
  )

  selected="$(_menu \
    --cancel-label "Done" \
    -- "${menuOptions[@]}" )"
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { "${FUNCNAME[0]}" ; return ; }

  case "${selected}" in
  UID)
    # Replace match_uid
    nfcSCAN="$(_readTag)"
    replacement_match_uid="$(jq -r '.uid' <<< "${nfcSCAN}")"
    # Enclose field in quotes if it's needed to escape a comma
    [[ "${replacement_match_uid}" == \"*\" ]] || [[ "${replacement_match_uid}" == *,* ]] && replacement_match_uid="\"${replacement_match_uid}\""
    [[ -z "${replacement_match_uid}" ]] && return
    replacement_match_text="${match_text}"
    replacement_text="${text}"
    ;;
  Match)
    # Replace match_text
    replacement_match_text="$( _inputbox "Replace match text" "${match_text}")"
    # Enclose field in quotes if it's needed to escape a comma
    [[ "${replacement_match_text}" == \"*\" ]] || [[ "${replacement_match_text}" == *,* ]] && replacement_match_text="\"${replacement_match_text}\""
    exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && return "${exitcode}"
    replacement_match_uid="${match_uid}"
    replacement_text="${text}"
    ;;
  Text)
    # Replace text
    replacement_text="$(_commandPalette)"
    # Enclose field in quotes if it's needed to escape a comma
    [[ "${replacement_text}" == \"*\" ]] || [[ "${replacement_text}" == *,* ]] && replacement_text="\"${replacement_text}\""
    [[ -z "${replacement_text}" ]] && { _msgbox "Nothing selected for writing" ; return ; }
    replacement_match_uid="${match_uid}"
    replacement_match_text="${match_text}"
    ;;
  Write)
    # Write to physical tag
    text="${text}" _Write
    return
    ;;
  Delete)
    # Delete line from Mappings database
    sed -i "${lineNumber}d" "${map}"
    "${FUNCNAME[0]}"
    return
    ;;
  esac

  read -rd '' message <<_EOF_
Replace:
${match_uid},${match_text},${text}
With:
${replacement_match_uid},${replacement_match_text},${replacement_text}
_EOF_
  _yesno "${message}" || return
  sed -i "${lineNumber}c\\${replacement_match_uid},${replacement_match_text},${replacement_text}" "${map}"

}

# Returns field from a comma separated string
# Usage: _parseCSV FIELDNUMBER CSVSTRING
# Returns:
# FIELD
_parseCSV() {
  local line field field_number in_quotes char field
  line="${2}"
  field num_fields="0"
  field_number="${1}"
  in_quotes="0"

  for (( i=0; i<${#line}; i++ )); do
    char="${line:$i:1}"

    # Check if character is a comma and not within quotes
    if [[ "${char}" == "," && $in_quotes -eq 0 ]]; then
      ((num_fields++))
      # Check if this is the field we're looking for
      if [[ ${num_fields} -eq ${field_number} ]]; then
        echo "${field}"
        return
      fi
      field=""
    else
      # Append character to current field
      field+="${char}"
      # Check for quotes
      if [[ "${char}" == "\"" ]]; then
        if [[ ${in_quotes} -eq 0 ]]; then
          in_quotes=1
        else
          in_quotes=0
        fi
      fi
    fi
  done

  # Output the last field if it's the one we're looking for
  ((num_fields++))
  if [[ ${num_fields} -eq ${field_number} ]]; then
    echo "${field}"
  fi
}

# Returns array in a numbered fashion
# Usage: _numberedArray "${array[@]}"
# Returns:
# 1 first_element 2 second_element ....
_numberedArray() {
  local array index
  array=("$@")
  index=1

  for element in "${array[@]}"; do
    printf "%s\n" "${index}"
    printf "%s\n" "\"${element}\""
    ((index++))
  done
}

# Write text string to physical NFC tag
# Usage: _writeTag "Text"
_writeTag() {
  local txt exitcode
  txt="${1}"

  _infobox "Present NFC tag to begin writing..."
  _tapto POST write '{"text": "'"${txt}"'"}'
  exitcode="${?}"; [[ "${exitcode}" -ge 1 ]] && { _error "Unable to write the NFC Tag"; return 1; }
  # Workaround for -write enabling launching games again
  _tapto settings PUT '{"launching":false}'

  _msgbox "${txt} \n successfully written to NFC tag"
}

# Write text string to NFC map (overrides physical NFC Tag contents)
# Usage: _writeTextToMap [--uid "UID" | --matchText "Text"] <"Text">
_writeTextToMap() {
  local txt uid oldMapEntry matchText

  while [[ "${#}" -gt "0" ]]; do
    case "${1}" in
    --uid)
      uid="${2}"
      shift 2
      ;;
    --matchText)
      matchText="${2}"
      shift 2
      ;;
    *)
      txt="${1}"
      shift
      ;;
    esac
  done

  [[ -f "${map}" ]] || echo "${mapHeader}" > "${map}"

  # Check if UID is provided
  [[ -z "${uid}" ]] && [[ -z "${matchText}" ]] && uid="$(_readTag | jq -r '.uid' )"

  if [[ -n "${uid}" ]]; then
    # Check if the map file exists and read the existing entry for the given UID
    [[ -f "${map}" ]] && oldMapEntry="$(grep "^${uid}," "${map}")"

    # If an existing entry is found, ask to replace it
    if [[ -n "${oldMapEntry}" ]] && _yesno "UID:${uid}\nText:${txt}\n\nAdd entry to map? This will replace:\n${oldMapEntry}"; then
      sed -i "s|^${uid},.*|${uid},,${txt}|g" "${map}"
    elif _yesno "UID:${uid}\nText:${txt}\n\nAdd entry to map?"; then
      echo "${uid},,${txt}" >> "${map}"
    fi
  elif [[ -n "${matchText}" ]]; then
    # Check if the map file exists and read the existing entry for the given UID
    [[ -f "${map}" ]] && oldMapEntry="$(grep "^.*,${matchText//[\[\.*^$/]/\\$&}," "${map}")"

    # If an existing entry is found, ask to replace it
    if [[ -n "${oldMapEntry}" ]] && _yesno "Match Text:${matchText}\nText:${txt}\n\nAdd entry to map? This will replace:\n${oldMapEntry}"; then
      sed -i "s|^,${matchText//[\[\.*^$/]/\\$&}.*|,${matchText},${txt}|g" "${map}"
    elif _yesno "Match Text:${matchText}\nText:${txt}\n\nAdd entry to map?"; then
      echo ",${matchText},${txt}" >> "${map}"
    fi
  fi
}

# Read UID and Text from tag, returns comma separated values below
# Usage: _readTag
# Returns: json object
_readTag() {
  local lastScanTime currentScan currentScanTime scanSuccess
  lastScanTime="$(_tapto status | jq -r '.lastToken.scanTime' | date -f - +%s)"
  _infobox "Scan NFC Tag to continue...\n\nPress any key to go back"
  while true; do
    currentScan="$(_tapto status | jq -c '.activeToken')"
    currentScanTime="$( \
      jq -r '.scanTime' <<< "${currentScan}" | \
      date -f - +%s 2>/dev/null || echo 0)"
    [[ "${currentScanTime}" -gt "${lastScanTime}" ]] && { scanSuccess="true" ; break; }
    sleep 1
    read -t 1 -n 1 -r  && return 1
  done
  # I hope this next command is reduntant
  #currentScan="$(_tapto status)"
  if [[ ! "${scanSuccess}" ]]; then
    _yesno "Tag not read" --yes-label "Retry" && _readTag
    return 1
  fi
  [[ -n "${currentScan}" ]] && echo "${currentScan}"
}

# Search for possible matches by match_text in the mappings database
# Usage: _searchMatchText "Text"
# Returns lines that match
_searchMatchText() {
  local nfcTxt
  nfcTxt="${1}"
  [[ -z "${nfcTxt}" ]] && return

  [[ -f "${map}" ]] || return
  [[ $(head -n 1 "${map}") == "${mapHeader}" ]] || return

  sed 1d "${map}" | while IFS=, read -r match_uid match_text text; do
    [[ -z "${match_text}" ]] && continue
    if [[ "${nfcTxt}" == "${match_text}" ]] || [[ "${nfcTxt}" =~ ${match_text} ]]; then
      echo "${match_uid},${match_text},${text}"
    fi
  done
}

# Check if element is in array
# Usage: _isInArray "element" "${array[@]}"
# returns exit code 0 if element is array, returns exitcode 1 if element is in array
_isInArray() {
  local string="${1}"
  shift
  local array=("${@}")
  [[ "${#array}" -eq 0 ]] && return 1

  for item in "${array[@]}"; do
    if [[ "${string}" == "${item}" ]]; then
      return 0
    fi
  done

  return 1
}

_amiibo() {
  local today apiFreshness apiLastUpdate cacheStale

  today="$(date +"%Y-%m-%d")"
  cacheStale="false"

  if [[ ! -f "${amiiboAPICache}" ]] || ! jq -e '.amiibo | length > 0' "${amiiboAPICache}" >/dev/null; then
    cacheStale="true"
  else
    apiFreshness="$(date -r "${amiiboAPICache}" +"%Y-%m-%d")"
  fi

  if [[ "${apiFreshness}" != "${today}" ]]; then
    if ping -c 1 8.8.8.8 > /dev/null 2>&1 && curl -s -o /dev/null "${amiiboApi}" > /dev/null; then
      apiLastUpdate="$(date -d "$(curl -s "${amiiboApi}/amiibo/lastupdated" | jq -r '.lastUpdated')" +%s)"
      [[ "$(date -r "${amiiboAPICache}" +%s)" -lt "${apiLastUpdate}" ]] && cacheStale="true"
    fi
  else
    cacheStale="false"
  fi

  if "${cacheStale}"; then 
    curl -s "${amiiboApi}/amiibo/" -o "${amiiboAPICache}.new"
    if jq -e '.amiibo | length > 0' "${amiiboAPICache}.new" >/dev/null; then
      mv "${amiiboAPICache}.new" "${amiiboAPICache}"
    fi
  else
    touch "${amiiboAPICache}"
  fi

  if [[ -f "${amiiboAPICache}" ]]; then
    cat "${amiiboAPICache}"
  else
    return 1
  fi
}

_amiiboRegex() {
  local categories gameSeries characters variation type amiiboSeries amiibos msg selected selectedGameSeries selectedCharacter selectedVariation selectedType selectedAmiiboSeries selectedAmiibo regex
  [[ -n "${1}" ]] && regex="${1}"

  while true; do
    categories=(1 "Game Series" 2 "Character" 3 "Variation" 4 "Type: Figure, Card, Yarn, Band" 5 "Amiibo Series" 6 "Specific Amiibo" 7 "Clear all choices" 8 "Cancel")
    gameSeries=( "00[0-2]" "Super Mario" "0((0[0-2])|9[c-d])" "Mario (all)" "008" "Yoshi's Woolly World" "00c" "Donkey Kong" "010" "The Legend of Zelda" "014" "The Legend of Zelda: Breath of the Wild" "0(1[89a-f]|[2-4][0-9a-f]|5[0-1])" "Animal Crossing" "058" "Star Fox" "05c" "Metroid" "060" "F-Zero" "064" "Pikmin" "06c" "Punch Out" "070" "Wii Fit" "074" "Kid Icarus" "078" "Classic Nintendo" "07c" "Mii" "080" "Splatoon" "09[c-d]" "Mario Sports Superstars" "0((1[89a-f]|[2-4][0-9a-f]|5[0-1])|a[0-2])" "Animal Crossing (all)" "0(a[0-2])" "Animal Crossing New Horizons" "0a4" "ARMS" "(19[0-9a-f]|1[0-9a-d][0-9a-f]|1d[0-4])" "Pokemon" "1f0" "Kirby" "1f4" "BoxBoy!" "210" "Fire Emblem" "224" "Xenoblade Chronicles" "228" "Earthbound" "22c" "Chibi Robo" "320" "Sonic" "324" "Bayonetta" "334" "Pac-man" "338" "Dark Souls" "33c" "Tekken" "348" "Megaman" "34c" "Street fighter" "350" "Monster Hunter" "35c" "Shovel Knight" "360" "Final Fantasy" "364" "Dragon Quest" "374" "Kellogs" "378" "Metal Gear Solid" "37c" "Castlevania" "380" "Power Pros" "384" "Yu-Gi-Oh!" "38c" "Diablo" "3a0" "Persona" "3b4" "Banjo Kazooie" "3c8" "Fatal Fury" "3dc" "Minecraft" "3f0" "Kingdom Hearts")
    readarray -t characters < <(_amiibo | jq -r '.amiibo[] | .head[:4] + "\n" + .character')
    variation=("00" 1 "01" 2 "02" 3 "03" 4 "04" 5 "05" 6 "ff" "Skylander")
    readarray -t type < <(_amiibo | jq -r '.amiibo[] | .head[6:8] + " " + .type' | sort -u | tr ' ' '\n')
    readarray -t amiiboSeries < <(_amiibo | jq -r '.amiibo[] | .tail[4:6] + " " + .amiiboSeries' | sort -u | sed "s/ /\n/")
    readarray -t amiibos < <(_amiibo | jq -r '.amiibo[] | .head + .tail + " " + .name' | sed "s/ /\n/")

    [[ -n "${regex}" ]] && msg="Current matching string: ${regex}"
    selected="$(_menu  --colors \
      --default-item "${selected}" \
      -- "${categories[@]}")"
    case "${selected}" in
      1)
        # Game Series
        msg="Current matching string: ${selectedCharacter:-${underline}${selectedGameSeries:-...}${reset}.}${selectedVariation:-..}${selectedType:-..}....${selectedAmiiboSeries:-..}02"
        selectedGameSeries="$(_menu --colors \
          --default-item "${selectedGameSeries}" \
          -- ... "Any game Series" "${gameSeries[@]}")"
        if [[ "${selectedGameSeries}" == "..." ]]; then unset selectedGameSeries
        else
          unset selectedCharacter
        fi
      ;;
      2)
        # Character
        if [[ -n "${regex}" ]] && _yesno "Filter by current choices?\n${regex}"; then
          readarray -t characters < <( \
            _amiibo | jq -r '.amiibo[] | .head + .tail + " " + .head[:4] + " " + .character' | grep "^${regex}" | awk '{$1=""; print substr($0,2)}' | sort -u | sed "s/ /\n/")
        fi
        msg="Current matching string: ${selectedGameSeries:-...}${underline}.${reset}${selectedVariation:-..}${selectedType:-..}....${selectedAmiiboSeries:-..}02"
        selectedCharacter="$(_menu --colors \
          --default-item "${selectedCharacter}" \
          -- "${selectedGameSeries:-...}." "Any character" "${characters[@]}")"
        if [[ "${selectedCharacter}" == "...." ]]; then unset selectedCharacter
        else
          unset selectedGameSeries
          readarray -t variation < <( \
            _amiibo | jq -r '.amiibo[] | .head[:4] + " " + .head[4:6] + " " + if .head[4:6] == "ff" then "Skylander" else "Variation" end' | \
            grep "^${selectedCharacter}" | awk '{$1=""; print substr($0,2)}' | sort -u | tr ' ' '\n')
          msg="Current matching string: ${selectedCharacter:-${selectedGameSeries:-...}.}${underline}${selectedVariation:-..}${reset}${selectedType:-..}....${selectedAmiiboSeries:-..}02"
          selectedVariation="$(_menu --colors \
            --default-item "${selectedVariation}" \
            -- .. "Any variation" "${variation[@]}")"
          [[ "${selectedVariation}" == ".." ]] && unset selectedVariation
        fi
      ;;
      3)
        # Variation
        if [[ -n "${regex}" ]] && _yesno "Filter by current choices?\n${regex}"; then
          readarray -t variation < <( \
            _amiibo | jq -r '.amiibo[] | .head + .tail + " " + .head[4:6] + " " + .character' | grep "^${regex}" | awk '{$1=""; print substr($0,2)}' | sed "s/ /\n/")
        fi
        msg="Current matching string: ${selectedCharacter:-${selectedGameSeries:-...}.}${underline}${selectedVariation:-..}${reset}${selectedType:-..}....${selectedAmiiboSeries:-..}02"
        selectedVariation="$(_menu --colors \
          --default-item "${selectedVariation}" \
          -- .. "Any variation" "${variation[@]}")"
        [[ "${selectedVariation}" == ".." ]] && unset selectedVariation
      ;;
      4)
        # Type
        if [[ -n "${regex}" ]] && _yesno "Filter by current choices?\n${regex}"; then
          readarray -t type < <( \
            _amiibo | jq -r '.amiibo[] | .head + .tail + " " + .head[6:8] + " " + .type' | grep "^${regex}" | awk '{$1=""; print substr($0,2)}' | sort -u | tr ' ' '\n')
        fi
        msg="Current matching string: ${selectedCharacter:-${selectedGameSeries:-...}.}${selectedVariation:-..}${underline}${selectedType:-..}${reset}....${selectedAmiiboSeries:-..}02"
        selectedType="$(_menu --colors \
          --default-item "${selectedType}" \
          -- .. "Any type" "${type[@]}")"
        [[ "${selectedType}" == ".." ]] && unset selectedType
      ;;
      5)
        # Amiibo Series
        if [[ -n "${regex}" ]] && _yesno "Filter by current choices?\n${regex}"; then
          readarray -t amiiboSeries < <( \
            _amiibo | jq -r '.amiibo[] | .head + .tail + " " + .tail[4:6] + " " + .amiiboSeries' | grep "^${regex}" | awk '{$1=""; print substr($0,2)}' | sort -u | sed "s/ /\n/")
        fi
        msg="Current matching string: ${selectedCharacter:-${selectedGameSeries:-...}.}${selectedVariation:-..}${selectedType:-..}....${underline}${selectedAmiiboSeries:-..}${reset}02"
        selectedAmiiboSeries="$(_menu --colors \
          --default-item "${selectedAmiiboSeries}" \
          -- .. "Any series" "${amiiboSeries[@]}")"
        [[ "${selectedAmiiboSeries}" == ".." ]] && unset selectedAmiiboSeries
      ;;
      6)
        # Amiibo
        if [[ -n "${regex}" ]] && _yesno "Filter by current choices?\n${regex}"; then
          readarray -t amiibos < <(_amiibo | jq -r '.amiibo[] | .head + .tail + " " + .character' | grep "^${regex}" | sed "s/ /\n/")
        fi
        [[ -n "${regex}" ]] && msg="Current matching string: ${regex}"
        selectedAmiibo="$(_menu --colors \
          --default-item "${selectedAmiibo}" \
          -- "UNSET" "Unset" "${amiibos[@]}")"
        [[ "${selectedAmiibo}" == "UNSET" ]] && unset selectedAmiibo
      ;;
      7)
        # Clear all
        unset selectedGameSeries selectedCharacter selectedVariation selectedType selectedAmiiboSeries selectedAmiibo
      ;;
      8)
        # Cancel
        return
      ;;
    esac

    regex="${selectedCharacter:-${selectedGameSeries:-...}.}${selectedVariation:-..}${selectedType:-..}....${selectedAmiiboSeries:-..}02"
    if [[ -n "${selectedAmiibo}" ]]; then
      regex="${selectedAmiibo}"
      unset selectedGameSeries selectedCharacter selectedVariation selectedType selectedAmiiboSeries selectedAmiibo
    fi

    height=8 width="$(( ${#regex} + 6 ))" _yesno "Continue editing?\nCurrent choice:\n${regex}" --no-label "Finish" --cancel-label "Finish" || break
  done

  echo "${regex}"
}

_textEditor() {
  local opts file editorChoice exitcode output
  file="${1}"
  shift 1
  opts=("${@}")

  editors=(
    "builtin" "Keyboard needed to type"
    "nano" "Warning: Keyboard required to exit"
    "vim"  "Warning: Keyboard required to exit"
    )
  editorChoice="$(msg="Chose editor:" _menu -- "${editors[@]}")"

  case "${editorChoice}" in
    builtin)
      output="$( dialog \
        --backtitle "${title}" \
        "${opts[@]}" \
        --editbox "${file}" \
        22 77 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)" )"
      exitcode="${?}"; [[ "${exitcode}" -eq 0 ]] && echo -n "${output}" > "${file}"
    ;;
    *)
      "${editorChoice}" "${file}" >"$(tty)" <"$(tty)"
      exitcode="${?}"
    ;;
  esac

  return "${exitcode}"
}

# Ask user for a number
# Usage: _rangebox "text" "min-value" "max-value" "default-value" [--optional-arguments]
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_rangebox() {
  local msg opts init
  msg="${1}"
  if [[ "${2}" =~ ^[0-9]*$ ]]; then minValue="${2}"; else return 1; fi
  if [[ "${3}" =~ ^[0-9]*$ ]]; then maxValue="${3}"; else return 1; fi
  if [[ "${4}" =~ ^[0-9]*$ ]]; then defaultValue="${4}"; else return 1; fi
  shift 4
  opts=("${@}")
  dialog \
    --backtitle "${title}" \
    "${opts[@]}" --rangebox "${msg}" 22 77 \
    "${minValue}" "${maxValue}" "${defaultValue}" \
    3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Ask user for a string
# Usage: _inputbox "My message" "Initial text" [--optional-arguments]
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_inputbox() {
  local msg opts init
  msg="${1}"
  init="${2}"
  shift 2
  opts=("${@}")
  dialog \
    --backtitle "${title}" \
    "${opts[@]}" \
    --inputbox "${msg}" \
    22 77 "${init}" 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Display a menu
# Usage: [msg="message"] _menu [--optional-arguments] -- [ tag item ] ...
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_menu() {
  local menu_items optional_args

  # Separate optional arguments from menu items
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--" ]]; then
      shift
      break
    else
      optional_args+=("$1")
      shift
    fi
  done

  # Collect menu items
  while [[ $# -gt 0 ]]; do
    menu_items+=("$1")
    shift
  done

  dialog \
    --backtitle "${title}" \
    "${optional_args[@]}" \
    --menu "${msg:-Chose one}" \
    22 77 16 "${menu_items[@]}" 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Display a radio menu
# Usage: [msg="message"] _radiolist [--optional-arguments] -- [ tag item status ] ...
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_radiolist() {
  local menu_items optional_args

  # Separate optional arguments from menu items
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--" ]]; then
      shift
      break
    else
      optional_args+=("$1")
      shift
    fi
  done

  # Collect menu items
  while [[ $# -gt 0 ]]; do
    menu_items+=("$1")
    shift
  done

  dialog \
    --backtitle "${title}" \
    "${optional_args[@]}" \
    --radiolist "${msg:-Chose one}" \
    22 77 16 "${menu_items[@]}" 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Display an infobox, this exits immediately without clearing the screen
# Usage: _msgbox "My message" [--optional-arguments]
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_infobox() {
  local msg opts height width
  msg="${1}"
  shift
  opts=("${@}")
  dialog \
    --backtitle "${title}" \
    --aspect 0 "${opts[@]}" \
    --infobox "${msg}" \
    "${height:-0}" "${width:-0}"  3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Display a message
# Usage: _msgbox "My message" [--optional-arguments]
# You can pass additioal arguments to the dialog program
# Backtitle is already set
_msgbox() {
  local msg opts
  msg="${1}"
  shift
  opts=("${@}")
  dialog \
    --backtitle "${title}" \
    "${opts[@]}" \
    --msgbox "${msg}" \
    22 77  3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Request user input
# Usage: _yesno "My question" [--optional-arguments]
# You can pass additioal arguments to the dialog program
# Backtitle is already set
# returns the exit code from dialog which depends on the user answer
_yesno() {
  local msg opts height width
  msg="${1}"
  shift
  opts=("${@}")
  dialog \
    --backtitle "${title}" \
    "${opts[@]}" \
    --yesno "${msg}" \
    "${height:=22}" "${width:=77}" 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
  return "${?}"
}

# Display an error
# Usage: _error "My error" [<number>] [--optional-arguments]
# If the second argument is a number, the program will exit with that number as an exit code.
# You can pass additioal arguments to the dialog program
# Backtitle and title are already set
# Returns the exit code of the dialog program
_error() {
  local msg opts answer exitcode logs
  msg="${1}"
  shift
  [[ "${1}" =~ ^[0-9]+$ ]] && exitcode="${1}" && shift
  opts=("${@}")
  [[ "${FUNCNAME[1]}" != "_logExporter" ]] && logs=( --extra-button --extra-label "Logs" )

  while true; do
    dialog \
      --backtitle "${title}" \
      --title "\Z1ERROR:\Zn" \
      --colors \
      "${logs[@]}" \
      "${opts[@]}" \
      --msgbox "${msg}" \
      22 77 3>&1 1>&2 2>&3 >"$(tty)" <"$(tty)"
    answer="${?}"
    case "${answer}" in
      3) _logExporter ;;
      *) break ;;
    esac
  done
  [[ -n "${exitcode}" ]] && exit "${exitcode}"
  return "${answer}"
}

# Tapto REST API handler
# Usage: _tapto [METHOD] [OPTIONS] [ENDPOINT] [DATA]
# - METHOD: HTTP method (POST, PUT, DELETE, GET, OPTIONS)
# - OPTIONS: Additional headers such as Content-Type, Accept, Authorization, Link
# - ENDPOINT: API endpoints such as, status, launch, games, systems, mappings, etc
# - DATA: Data to be sent with the request
# Returns the response from the Tapto REST API
_tapto() {
  local x h d url response exitcode

  while [[ ${#} -gt 0 ]]; do
    case "${1}" in
      POST) x=("-X" "POST"); shift ;;
      PUT) x=("-X" "PUT"); shift ;;
      DELETE) x=("-X" "DELETE"); shift ;;
      GET) x=("-X" "GET"); shift ;;
      OPTIONS) x=("-X" "OPTIONS"); shift ;;
      "Content-Type") h+=("-H" "Content-Type: ${2}"); shift 2 ;;
      Accept) h+=("-H" "Accept: ${2}"); shift 2 ;;
      Authorization) h+=("-H" "Authorization: ${2}"); shift 2 ;;
      Link) h+=("-H" "Link: ${2}"); shift 2 ;;
      status) url="status"; shift ;;
      launch) url="launch"; shift ;;
      games)
        url="games?system=${2}"
        shift 2
        [[ "${1}" =~ ^(query|maxResults)=.* ]] && { url="${url}&${1%%&*}"; shift ; }
        [[ "${1}" =~ ^(query|maxResults)=.* ]] && { url="${url}&${1%%&*}"; shift ; }
        ;;
      systems) url="systems"; shift ;;
      mappings)
        url="mappings";
        shift
        [[ "${1}" =~ ^[0-9]+$ ]] && { url="${url}/${1}" ; shift ; }
        ;;
      history) url="history"; shift ;;
      settings) url="settings"; shift ;;
      log) url="settings/log/download"; shift ;;
      index) url="settings/index/games" x=("-X" "POST"); shift ;;
      write) url="readers/0/write" x=("-X" "POST"); shift ;;
      *) d="${1}"; break ;;
    esac
  done
  [[ "${#h[@]}" -eq 0 ]] && h=("-H" "Content-Type: application/json")
  [[ "${#x[@]}" -eq 0 ]] && x=("-X" "GET")
  [[ -z "${url}" ]] && url="status"

  if [[ -t 1 ]]; then
    response="$(curl -s "${x[@]}" "${h[@]}" -d "${d}" "${taptoAPI}/${url}")"
    exitcode="${?}"
    if jq <<<"${response}" &>/dev/null ; then
      jq --color-output <<<"${response}" | less -R -X -F
    else
      less -R -X -F <<<"${response}"
    fi
  else
    curl -s "${x[@]}" "${h[@]}" -d "${d}" "${taptoAPI}/${url}"
    exitcode="${?}"
  fi

  return "${exitcode}"
}

[[ "${1}" == "api" ]] && { shift; _tapto "${@}"; exit "${?}"; }

_exit() {
  clear
  "${nfcReadingStatus:-false}" && _tapto settings PUT '{"launching":true}'
  exit "${1:-0}"
}
trap _exit EXIT

_depends

while true; do
  main
  "_${selected:-exit}"
done

# vim: set expandtab ts=2 sw=2:
