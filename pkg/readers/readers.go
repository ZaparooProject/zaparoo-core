package readers

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
)

type Scan struct {
	Source string
	Token  *tokens.Token
	Error  error
}

type Reader interface {
	// TODO: type? file, libnfc, etc.
	// Ids returns the device string prefixes supported by this reader.
	Ids() []string
	// Open any necessary connections to the device and start polling.
	// Takes a device connection string and a channel to send scanned tokens.
	Open(config.ReadersConnect, chan<- Scan) error
	// Close any open connections to the device and stop polling.
	Close() error
	// Detect attempts to search for a connected device and returns the device
	// connection string. If no device is found, an empty string is returned.
	// Takes a list of currently connected device strings.
	Detect([]string) string
	// Device returns the device connection string.
	Device() string
	// Connected returns true if the device is connected and active.
	Connected() bool
	// Info returns a string with information about the connected device.
	Info() string
	// Write sends a string to the device to be written to a token, if
	// that device supports writing. Blocks until completion or timeout.
	Write(string) (*tokens.Token, error)
	// CancelWrite sends a request to cancel an active write request.
	CancelWrite()
}
