package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var (
	ErrRequestTimeout   = errors.New("request timed out")
	ErrInvalidParams    = errors.New("invalid params")
	ErrRequestCancelled = errors.New("request cancelled")
)

const APIPath = "/api/v0.1"

// DisableZapScript disables the service running any processed ZapScript from
// tokens, and returns a function to re-enable it.
// The returned function must be run even if there is an error so the service
// isn't left in an unusable state.
func DisableZapScript(cfg *config.Instance) func() {
	_, err := LocalClient(
		context.Background(),
		cfg,
		models.MethodSettingsUpdate,
		"{\"runZapScript\":false}",
	)
	if err != nil {
		log.Error().Err(err).Msg("error disabling runZapScript")
		return func() {}
	}

	return func() {
		_, err = LocalClient(
			context.Background(),
			cfg,
			models.MethodSettingsUpdate,
			"{\"runZapScript\":true}",
		)
		if err != nil {
			log.Error().Err(err).Msg("error enabling runZapScript")
		}
	}
}

// LocalClient sends a single unauthenticated method with params to the local
// running API service, waits for a response until timeout then disconnects.
func LocalClient(
	ctx context.Context,
	cfg *config.Instance,
	method string,
	params string,
) (string, error) {
	localWebsocketUrl := url.URL{
		Scheme: "ws",
		Host:   "localhost:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	id, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}

	req := models.RequestObject{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
	}

	if len(params) == 0 {
		req.Params = nil
	} else if json.Valid([]byte(params)) {
		req.Params = []byte(params)
	} else {
		return "", ErrInvalidParams
	}

	c, _, err := websocket.DefaultDialer.Dial(localWebsocketUrl.String(), nil)
	if err != nil {
		return "", err
	}
	defer func(c *websocket.Conn) {
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
	}(c)

	done := make(chan struct{})
	var resp *models.ResponseObject

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Error().Err(err).Msg("error reading message")
				return
			}

			var m models.ResponseObject
			err = json.Unmarshal(message, &m)
			if err != nil {
				continue
			}

			if m.JSONRPC != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if m.ID != id {
				continue
			}

			resp = &m
			return
		}
	}()

	err = c.WriteJSON(req)
	if err != nil {
		return "", err
	}

	timer := time.NewTimer(config.APIRequestTimeout)
	select {
	case <-done:
		break
	case <-timer.C:
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
		return "", ErrRequestTimeout
	case <-ctx.Done():
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
		return "", ErrRequestCancelled
	}

	if resp == nil {
		return "", ErrRequestTimeout
	}

	if resp.Error != nil {
		return "", errors.New(resp.Error.Message)
	}

	var b []byte
	b, err = json.Marshal(resp.Result)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func WaitNotification(
	ctx context.Context,
	timeout time.Duration,
	cfg *config.Instance,
	id string,
) (string, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:" + strconv.Itoa(cfg.APIPort()),
		Path:   APIPath,
	}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return "", err
	}
	defer func(c *websocket.Conn) {
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
	}(c)

	done := make(chan struct{})
	var resp *models.RequestObject

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Error().Err(err).Msg("error reading message")
				return
			}

			var m models.RequestObject
			err = json.Unmarshal(message, &m)
			if err != nil {
				continue
			}

			if m.JSONRPC != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if m.ID != nil {
				continue
			}

			if m.Method != id {
				continue
			}

			resp = &m

			return
		}
	}()

	var timerChan <-chan time.Time
	if timeout == 0 {
		timer := time.NewTimer(config.APIRequestTimeout)
		timerChan = timer.C
	} else if timeout > 0 {
		timer := time.NewTimer(timeout)
		timerChan = timer.C
	}
	// or else leave chan nil, which will never receive

	select {
	case <-done:
		break
	case <-timerChan:
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
		return "", ErrRequestTimeout
	case <-ctx.Done():
		err := c.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing websocket")
		}
		return "", ErrRequestCancelled
	}

	if resp == nil {
		return "", ErrRequestTimeout
	}

	var b []byte
	b, err = json.Marshal(resp.Params)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
