package assets

import (
	"embed"
	"encoding/json"
)

//go:embed _app
var App embed.FS

// SuccessSound Breviceps (https://freesound.org/people/Breviceps/sounds/445978/)
// Licence: CC0 1.0 Universal (CC0 1.0) Public Domain Dedication
//
//go:embed sounds/success.wav
var SuccessSound []byte

// FailSound PaulMorek (https://freesound.org/people/PaulMorek/sounds/330046/)
// Licence: CC0 1.0 Universal (CC0 1.0) Public Domain Dedication
//
//go:embed sounds/fail.wav
var FailSound []byte

//go:embed icon.ico
var Icon []byte

//go:embed systems/*
var Systems embed.FS

type SystemMetadata struct {
	Id           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	ReleaseDate  string `json:"releaseDate"`
	Manufacturer string `json:"manufacturer"`
}

func GetSystemMetadata(system string) (SystemMetadata, error) {
	var metadata SystemMetadata

	data, err := Systems.ReadFile("systems/" + system + ".json")
	if err != nil {
		return metadata, err
	}

	err = json.Unmarshal(data, &metadata)
	return metadata, err
}
