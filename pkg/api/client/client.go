package client

import (
	"encoding/json"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"net/url"
	"strconv"
	"time"
)

var (
	ErrRequestTimeout = errors.New("request timed out")
	ErrInvalidParams  = errors.New("invalid params")
)

// LocalClient sends a single unauthenticated method with params to the local
// running API service, waits for a response until timeout then disconnects.
func LocalClient(
	cfg *config.Instance,
	method string,
	params string,
) (string, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:" + strconv.Itoa(cfg.ApiPort()),
		Path:   "/api/v1.0",
	}

	id, err := uuid.NewUUID()
	if err != nil {
		return "", err
	}

	req := models.RequestObject{
		JsonRpc: "2.0",
		Id:      &id,
		Method:  method,
	}

	if len(params) == 0 {
		req.Params = nil
	} else if json.Valid([]byte(params)) {
		var ps interface{}
		err := json.Unmarshal([]byte(params), &ps)
		if err != nil {
			return "", err
		}
		req.Params = &ps
	} else {
		return "", ErrInvalidParams
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

			if m.JsonRpc != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if m.Id != id {
				continue
			}

			resp = &m
			return
		}
	}()

	//reqFmt, _ := json.MarshalIndent(req, "", "    ")
	//fmt.Println(string(reqFmt))

	err = c.WriteJSON(req)
	if err != nil {
		return "", err
	}

	timer := time.NewTimer(api.RequestTimeout)
	select {
	case <-done:
		break
	case <-timer.C:
		return "", ErrRequestTimeout
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

	//respFmt, _ := json.MarshalIndent(resp, "", "    ")
	//fmt.Println(string(respFmt))

	return string(b), nil
}

func WaitNotification(
	cfg *config.Instance,
	id string,
) (string, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   "localhost:" + strconv.Itoa(cfg.ApiPort()),
		Path:   "/api/v1.0",
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

			if m.JsonRpc != "2.0" {
				log.Error().Msg("invalid jsonrpc version")
				continue
			}

			if m.Id != nil {
				continue
			}

			if m.Method != id {
				continue
			}

			resp = &m

			return
		}
	}()

	timer := time.NewTimer(api.RequestTimeout)
	select {
	case <-done:
		break
	case <-timer.C:
		return "", ErrRequestTimeout
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
