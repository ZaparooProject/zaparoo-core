package models

type LoaderArgs struct {
	Text     string `json:"text"`
	Timeout  int    `json:"timeout"`
	Complete string `json:"complete"`
}

type PickerAction struct {
	ZapScript string  `json:"zapscript"`
	Label     *string `json:"label"`
}

type PickerArgs struct {
	Actions []PickerAction `json:"actions"`
	Title   string         `json:"title"`
	Timeout int            `json:"timeout"`
	Trusted *bool          `json:"trusted"`
}
