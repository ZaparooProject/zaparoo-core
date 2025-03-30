package models

import "encoding/json"

const (
	ZapScriptCmdLaunch       = "launch"
	ZapScriptCmdLaunchSystem = "launch.system"
	ZapScriptCmdLaunchRandom = "launch.random"
	ZapScriptCmdLaunchSearch = "launch.search"

	ZapScriptCmdPlaylistPlay     = "playlist.play"
	ZapScriptCmdPlaylistNext     = "playlist.next"
	ZapScriptCmdPlaylistPrevious = "playlist.previous"

	ZapScriptCmdExecute  = "execute"
	ZapScriptCmdDelay    = "delay"
	ZapScriptCmdEvaluate = "evaluate"

	ZapScriptCmdMisterINI    = "mister.ini"
	ZapScriptCmdMisterCore   = "mister.core"
	ZapScriptCmdMisterScript = "mister.script"
	ZapScriptCmdMisterMGL    = "mister.mgl"

	ZapScriptCmdHTTPGet  = "http.get"
	ZapScriptCmdHTTPPost = "http.post"

	ZapScriptCmdInputKeyboard = "input.keyboard"
	ZapScriptCmdInputGamepad  = "input.gamepad"
	ZapScriptCmdInputCoinP1   = "input.coinp1"
	ZapScriptCmdInputCoinP2   = "input.coinp2"

	ZapScriptCmdUINotice = "ui.notice"
	ZapScriptCmdUIPicker = "ui.picker"

	ZapScriptCmdInputKey = "input.key" // DEPRECATED
	ZapScriptCmdKey      = "key"       // DEPRECATED
	ZapScriptCmdCoinP1   = "coinp1"    // DEPRECATED
	ZapScriptCmdCoinP2   = "coinp2"    // DEPRECATED
	ZapScriptCmdRandom   = "random"    // DEPRECATED
	ZapScriptCmdShell    = "shell"     // DEPRECATED
	ZapScriptCmdCommand  = "command"   // DEPRECATED
	ZapScriptCmdINI      = "ini"       // DEPRECATED
	ZapScriptCmdSystem   = "system"    // DEPRECATED
	ZapScriptCmdGet      = "get"       // DEPRECATED
)

type ZapScript struct {
	ZapScript int            `json:"zapscript"` // schema version
	Name      *string        `json:"name"`      // optional display name
	Cmds      []ZapScriptCmd `json:"cmds"`
}

type ZapScriptCmd struct {
	ID   string          `json:"id"`   // internal id of command instance
	Name *string         `json:"name"` // optional display name
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args"`
}

type CmdEvaluateArgs struct {
	ZapScript string `json:"zapscript" arg:"position=1"`
}

type CmdLaunchArgs struct {
	Path      string  `json:"path" arg:"position=1"`
	Launcher  *string `json:"launcher"`
	Name      *string `json:"name"`
	System    *string `json:"system"`
	URL       *string `json:"url"`
	PreNotice *string `json:"preNotice"`
}

type CmdNotice struct {
	Text   string `json:"text" arg:"position=1"`
	Loader *bool  `json:"loader"`
}

type CmdPicker struct {
	Items []ZapScript `json:"items"`
}
