package file

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

const TokenType = "file"

type Reader struct {
	cfg     *config.Instance
	device  config.ReadersConnect
	path    string
	polling bool
}

func NewReader(cfg *config.Instance) *Reader {
	return &Reader{
		cfg: cfg,
	}
}

func (r *Reader) Ids() []string {
	return []string{"file"}
}

func (r *Reader) Open(device config.ReadersConnect, iq chan<- readers.Scan) error {
	if !utils.Contains(r.Ids(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if !filepath.IsAbs(path) {
		return errors.New("invalid device path, must be absolute")
	}

	parent := filepath.Dir(path)
	if parent == "" {
		return errors.New("invalid device path")
	}

	if _, err := os.Stat(parent); err != nil {
		return err
	}

	if _, err := os.Stat(path); err != nil {
		// attempt to create empty file
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		_ = f.Close()
	}

	r.device = device
	r.path = path
	r.polling = true

	go func() {
		var token *tokens.Token

		for r.polling {
			time.Sleep(100 * time.Millisecond)

			contents, err := os.ReadFile(r.path)
			if err != nil {
				// TODO: have a max retries?
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Error:  err,
				}
				continue
			}

			text := strings.TrimSpace(string(contents))

			// "remove" the token if the file is now empty
			if text == "" && token != nil {
				log.Debug().Msg("file is empty, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				continue
			}

			if token != nil && token.Text == text {
				continue
			}

			if text == "" {
				continue
			}

			token = &tokens.Token{
				Type:     TokenType,
				Text:     text,
				Data:     hex.EncodeToString(contents),
				ScanTime: time.Now(),
				Source:   r.device.ConnectionString(),
			}

			log.Debug().Msgf("new token: %s", token.Text)
			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token:  token,
			}
		}
	}()

	return nil
}

func (r *Reader) Close() error {
	r.polling = false
	return nil
}

func (r *Reader) Detect(connected []string) string {
	return ""
}

func (r *Reader) Device() string {
	return r.device.ConnectionString()
}

func (r *Reader) Connected() bool {
	return r.polling
}

func (r *Reader) Info() string {
	return r.path
}

func (r *Reader) Write(text string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (r *Reader) CancelWrite() {
	return
}
