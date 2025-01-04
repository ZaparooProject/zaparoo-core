package acr122_pcsc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/ebfe/scard"
	"github.com/rs/zerolog/log"
)

type Acr122Pcsc struct {
	cfg     *config.Instance
	device  string
	name    string
	polling bool
	ctx     *scard.Context
}

func NewAcr122Pcsc(cfg *config.Instance) *Acr122Pcsc {
	return &Acr122Pcsc{
		cfg: cfg,
	}
}

func (r *Acr122Pcsc) Ids() []string {
	return []string{"acr122_pcsc"}
}

func (r *Acr122Pcsc) Open(device string, iq chan<- readers.Scan) error {
	ps := strings.SplitN(device, ":", 2)
	if len(ps) != 2 {
		return errors.New("invalid device string: " + device)
	}

	if !utils.Contains(r.Ids(), ps[0]) {
		return errors.New("invalid reader id: " + ps[0])
	}

	if r.ctx == nil {
		ctx, err := scard.EstablishContext()
		if err != nil {
			return err
		}
		r.ctx = ctx
	}

	rls, err := r.ctx.ListReaders()
	if err != nil {
		return err
	}

	if !utils.Contains(rls, ps[1]) {
		return errors.New("reader not found: " + ps[1])
	}

	r.device = device
	r.name = ps[1]
	r.polling = true

	go func() {
		for r.polling {
			ctx := r.ctx
			if ctx == nil {
				continue
			}

			rls, err := ctx.ListReaders()
			if err != nil {
				log.Debug().Msgf("error listing pcsc readers: %s", err)
				r.polling = false
				break
			}

			if !utils.Contains(rls, r.name) {
				log.Debug().Msgf("reader not found: %s", r.name)
				r.polling = false
				break
			}

			rs := []scard.ReaderState{{
				Reader:       r.name,
				CurrentState: scard.StateUnaware,
			}}

			err = ctx.GetStatusChange(rs, 250*time.Millisecond)
			if err != nil {
				log.Debug().Msgf("error getting status change: %s", err)
				continue
			}

			if rs[0].EventState&scard.StatePresent == 0 {
				continue
			}

			tag, err := ctx.Connect(r.name, scard.ShareShared, scard.ProtocolAny)
			if err != nil {
				log.Debug().Msgf("error connecting to reader: %s", err)
				continue
			}

			status, err := tag.Status()
			if err != nil {
				log.Debug().Msgf("error getting status: %s", err)
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			log.Debug().Msgf("status: %v", hex.EncodeToString(status.Atr))

			res, err := tag.Transmit([]byte{0xFF, 0xCA, 0x00, 0x00, 0x00})
			if err != nil {
				log.Debug().Msgf("error transmitting: %s", err)
				continue
			}

			if len(res) < 2 {
				log.Debug().Msgf("invalid response")
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			resCode := res[len(res)-2:]
			if resCode[0] != 0x90 && resCode[1] != 0x00 {
				log.Debug().Msgf("invalid response code: %x", resCode)
				_ = tag.Disconnect(scard.ResetCard)
				continue
			}

			log.Debug().Msgf("response: %x", res)
			uid := res[:len(res)-2]

			i := 0
			data := make([]byte, 0)
			for {
				res, err = tag.Transmit([]byte{0xFF, 0xB0, 0x00, byte(i), 0x04})
				if err != nil {
					log.Debug().Msgf("error transmitting: %s", err)
					break
				} else if bytes.Equal(res, []byte{0x00, 0x00, 0x00, 0x00, 0x90, 0x00}) {
					break
				} else if len(res) < 6 {
					log.Debug().Msgf("invalid response")
					break
				} else if i >= 221 {
					break
				}

				data = append(data, res[:len(res)-2]...)
				i++
			}

			log.Debug().Msgf("data: %x", data)

			text, err := ParseRecordText(data)
			if err != nil {
				log.Debug().Msgf("error parsing NDEF record: %s", err)
				text = ""
			}

			iq <- readers.Scan{
				Source: r.device,
				Token: &tokens.Token{
					UID:      hex.EncodeToString(uid),
					Text:     text,
					ScanTime: time.Now(),
					Source:   r.device,
				},
			}

			_ = tag.Disconnect(scard.ResetCard)

			for r.polling {
				rs := []scard.ReaderState{{
					Reader:       r.name,
					CurrentState: scard.StatePresent,
				}}

				err := ctx.GetStatusChange(rs, 250*time.Millisecond)
				if err != nil {
					log.Debug().Msgf("error getting status change: %s", err)
					break
				}

				if rs[0].EventState&scard.StatePresent == 0 {
					break
				}
			}

			iq <- readers.Scan{
				Source: r.device,
				Token:  nil,
			}
		}
	}()

	return nil
}

func (r *Acr122Pcsc) Close() error {
	r.polling = false
	if r.ctx != nil {
		err := r.ctx.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

// TODO: this is a hack workaround to stop some log spam, probably the Detect
// functions on readers should actually return an error instead of ""
var detectErrorOnce sync.Once

func (r *Acr122Pcsc) Detect(connected []string) string {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return ""
	}
	defer func(ctx *scard.Context) {
		err := ctx.Release()
		if err != nil {
			log.Warn().Err(err).Msg("error releasing pcsc context")
		}
	}(ctx)

	rs, err := ctx.ListReaders()
	if err != nil {
		detectErrorOnce.Do(func() {
			log.Debug().Err(err).Msg("listing pcsc readers")
		})
		return ""
	}

	acrs := make([]string, 0)
	for _, r := range rs {
		if strings.HasPrefix(r, "ACS ACR122") && !utils.Contains(connected, "acr122_pcsc:"+r) {
			acrs = append(acrs, r)
		}
	}

	if len(acrs) == 0 {
		return ""
	}

	log.Debug().Msgf("acr122 reader found: %s", acrs[0])
	return "acr122_pcsc:" + acrs[0]
}

func (r *Acr122Pcsc) Device() string {
	return r.device
}

func (r *Acr122Pcsc) Connected() bool {
	return r.polling
}

func (r *Acr122Pcsc) Info() string {
	return r.name
}

func (r *Acr122Pcsc) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}
