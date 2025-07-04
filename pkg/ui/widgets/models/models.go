package models

type NoticeArgs struct {
	Text     string `json:"text"`
	Timeout  int    `json:"timeout"`
	Complete string `json:"complete"`
}

type PickerItem struct {
	Name      string `json:"name"`
	ZapScript string `json:"zapscript"`
}

type PickerArgs struct {
	Items    []PickerItem `json:"items"`
	Title    string       `json:"title"`
	Selected int          `json:"selected"`
	Timeout  int          `json:"timeout"`
	Unsafe   bool         `json:"unsafe"`
}
