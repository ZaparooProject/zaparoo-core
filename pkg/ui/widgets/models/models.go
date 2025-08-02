package models

type NoticeArgs struct {
	Text     string `json:"text"`
	Complete string `json:"complete"`
	Timeout  int    `json:"timeout"`
}

type PickerItem struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type PickerArgs struct {
	Title    string       `json:"title"`
	Items    []PickerItem `json:"items"`
	Selected int          `json:"selected"`
	Timeout  int          `json:"timeout"`
	Unsafe   bool         `json:"unsafe"`
}
