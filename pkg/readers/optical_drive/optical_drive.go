package optical_drive

import (
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	TokenType         = "disc"
	IDSourceUUID      = "uuid"
	IDSourceLabel     = "label"
	IDSourceMerged    = "merged"
	MergedIDSeparator = "/"
)

type FileReader struct {
	cfg     *config.Instance
	device  config.ReadersConnect
	path    string
	polling bool
}

func NewReader(cfg *config.Instance) *FileReader {
	return &FileReader{
		cfg: cfg,
	}
}

func (r *FileReader) Ids() []string {
	return []string{"optical_drive"}
}

func (r *FileReader) Open(
	device config.ReadersConnect,
	iq chan<- readers.Scan,
) error {
	log.Info().Msgf("opening optical drive reader: %s", device.ConnectionString())
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

	r.device = device
	r.path = path
	r.polling = true

	getID := func(uuid string, label string) string {
		if uuid == "" {
			return label
		} else if label == "" {
			return uuid
		}

		if r.device.IDSource == IDSourceUUID {
			return uuid
		} else if r.device.IDSource == IDSourceLabel {
			return label
		} else if r.device.IDSource == IDSourceMerged {
			return uuid + MergedIDSeparator + label
		} else {
			return uuid + MergedIDSeparator + label
		}
	}

	go func() {
		var token *tokens.Token

		for r.polling {
			time.Sleep(1 * time.Second)

			rawUUID, err := exec.Command("blkid", "-o", "value", "-s", "UUID", r.path).Output()
			if err != nil {
				if token != nil {
					log.Debug().Err(err).Msg("error identifying optical media, removing token")
					token = nil
					iq <- readers.Scan{
						Source: r.device.ConnectionString(),
						Token:  nil,
					}
				} else {
					continue
				}
			}

			rawLabel, err := exec.Command("blkid", "-o", "value", "-s", "LABEL", r.path).Output()
			if err != nil {
				if token != nil {
					log.Debug().Err(err).Msg("error identifying optical media, removing token")
					token = nil
					iq <- readers.Scan{
						Source: r.device.ConnectionString(),
						Token:  nil,
					}
				} else {
					continue
				}
			}

			uuid := strings.TrimSpace(string(rawUUID))
			label := strings.TrimSpace(string(rawLabel))

			if uuid == "" && label == "" && token != nil {
				log.Debug().Msg("id is empty, removing token")
				token = nil
				iq <- readers.Scan{
					Source: r.device.ConnectionString(),
					Token:  nil,
				}
				continue
			}

			id := getID(uuid, label)
			if token != nil && token.UID == id {
				continue
			} else if id == "" {
				continue
			}

			token = &tokens.Token{
				Type:     TokenType,
				ScanTime: time.Now(),
				UID:      id,
			}

			log.Debug().Msgf("new token: %s", token.UID)
			iq <- readers.Scan{
				Source: r.device.ConnectionString(),
				Token:  token,
			}
		}
	}()

	return nil
}

func (r *FileReader) Close() error {
	r.polling = false
	return nil
}

func (r *FileReader) Detect(_ []string) string {
	return ""
}

func (r *FileReader) Device() string {
	return r.device.ConnectionString()
}

func (r *FileReader) Connected() bool {
	return r.polling
}

func (r *FileReader) Info() string {
	return r.path
}

func (r *FileReader) Write(_ string) (*tokens.Token, error) {
	return nil, nil
}

func (r *FileReader) CancelWrite() {
	return
}
