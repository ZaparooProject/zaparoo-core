package simpleserial

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

type SimpleSerialReader struct {
	cfg       *config.Instance
	device    config.ReadersConnect
	path      string
	polling   bool
	port      serial.Port
	lastToken *tokens.Token
}

func NewReader(cfg *config.Instance) *SimpleSerialReader {
	return &SimpleSerialReader{
		cfg: cfg,
	}
}

func (*SimpleSerialReader) IDs() []string {
	return []string{"simple_serial"}
}

func (r *SimpleSerialReader) parseLine(line string) (*tokens.Token, error) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "\r")

	if len(line) == 0 {
		return nil, nil
	}

	if !strings.HasPrefix(line, "SCAN\t") {
		return nil, nil
	}

	args := line[5:]
	if len(args) == 0 {
		return nil, nil
	}

	t := tokens.Token{
		Data:     line,
		ScanTime: time.Now(),
		Source:   r.device.ConnectionString(),
	}

	ps := strings.Split(args, "\t")
	hasArg := false
	for i := 0; i < len(ps); i++ {
		ps[i] = strings.TrimSpace(ps[i])
		switch {
		case strings.HasPrefix(ps[i], "uid="):
			t.UID = ps[i][4:]
			hasArg = true
		case strings.HasPrefix(ps[i], "text="):
			t.Text = ps[i][5:]
			hasArg = true
		case strings.HasPrefix(ps[i], "removable="):
			// TODO: this isn't really what removable means, but it works
			//		 for now. it will block shell commands though
			t.FromAPI = ps[i][10:] == "no"
			hasArg = true
		}
	}

	// if there are no named arguments, whole args becomes text
	if !hasArg {
		t.Text = args
	}

	return &t, nil
}

func (r *SimpleSerialReader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !helpers.Contains(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if runtime.GOOS != "windows" {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}

	port, err := serial.Open(path, &serial.Mode{
		BaudRate: 115200,
	})
	if err != nil {
		return err
	}

	err = port.SetReadTimeout(100 * time.Millisecond)
	if err != nil {
		return err
	}

	r.port = port
	r.device = device
	r.path = path
	r.polling = true

	go func() {
		var lineBuf []byte

		for r.polling {
			buf := make([]byte, 1024)
			n, err := r.port.Read(buf)
			if err != nil {
				log.Error().Err(err).Msg("failed to read from serial port")
				err = r.Close()
				if err != nil {
					log.Error().Err(err).Msg("failed to close serial port")
				}
				break
			}

			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					line := string(lineBuf)
					lineBuf = nil

					t, err := r.parseLine(line)
					if err != nil {
						log.Error().Err(err).Msg("failed to parse line")
						continue
					}

					if t != nil && !helpers.TokensEqual(t, r.lastToken) {
						iq <- readers.Scan{
							Source: r.device.ConnectionString(),
							Token:  t,
						}
					}

					if t != nil {
						r.lastToken = t
					}
				} else {
					lineBuf = append(lineBuf, buf[i])
				}
			}

			if r.lastToken != nil && time.Since(r.lastToken.ScanTime) > 1*time.Second {
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				r.lastToken = nil
			}
		}
	}()

	return nil
}

func (r *SimpleSerialReader) Close() error {
	r.polling = false
	if r.port != nil {
		err := r.port.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (*SimpleSerialReader) Detect(_ []string) string {
	return ""
}

func (r *SimpleSerialReader) Device() string {
	return r.device.ConnectionString()
}

func (r *SimpleSerialReader) Connected() bool {
	return r.polling && r.port != nil
}

func (r *SimpleSerialReader) Info() string {
	return r.path
}

func (*SimpleSerialReader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*SimpleSerialReader) CancelWrite() {
	// no-op, writing not supported
}
