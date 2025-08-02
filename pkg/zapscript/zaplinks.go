package zapscript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/installer"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"github.com/rs/zerolog/log"
)

const (
	MIMEZaparooZapScript = "application/vnd.zaparoo.zapscript"
	WellKnownPath        = "/.well-known/zaparoo"
)

var AcceptedMimeTypes = []string{
	MIMEZaparooZapScript,
}

type WellKnown struct {
	ZapScript int `json:"zapscript"`
}

var zapFetchTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   1 * time.Second,
		KeepAlive: 10 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   1 * time.Second,
	ResponseHeaderTimeout: 1 * time.Second,
	ExpectContinueTimeout: 500 * time.Millisecond,
}

var zapFetchClient = &http.Client{
	Transport: &installer.AuthTransport{
		Base: zapFetchTransport,
	},
	Timeout: 2 * time.Second,
}

func queryZapLinkSupport(u *url.URL) (int, error) {
	baseURL := u.Scheme + "://" + u.Host
	wellKnownURL := baseURL + WellKnownPath
	log.Debug().Msgf("querying zap link support at %s", wellKnownURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, http.NoBody)
	if err != nil {
		return 0, err
	}

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error().Err(err).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.New("invalid status code")
	}

	var wellKnown WellKnown
	err = json.NewDecoder(resp.Body).Decode(&wellKnown)
	if err != nil {
		return 0, err
	}

	log.Debug().Msgf("zap link well known result for %s: %v", wellKnownURL, wellKnown)
	return wellKnown.ZapScript, nil
}

func isZapLink(link string, db *database.Database) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return false
	}

	supported, ok, err := db.UserDB.GetZapLinkHost(u.Host)
	if err != nil {
		log.Error().Err(err).Msgf("error checking db for zap link support: %s", link)
		return false
	}
	if !ok {
		result, err := queryZapLinkSupport(u)
		if isOfflineError(err) {
			// don't permanently log as not supported if it may be temp internet access
			return false
		}
		if err != nil {
			log.Debug().Err(err).Msgf("error querying zap link support: %s", link)
			err := db.UserDB.UpdateZapLinkHost(u.Host, result)
			if err != nil {
				log.Error().Err(err).Msgf("error updating zap link support: %s", link)
			}
			return false
		}
		err = db.UserDB.UpdateZapLinkHost(u.Host, result)
		if err != nil {
			log.Error().Err(err).Msgf("error updating zap link support: %s", link)
		}
		supported = result > 0
	}

	if !supported {
		return false
	}

	return true
}

func getRemoteZapScript(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", strings.Join(AcceptedMimeTypes, ", "))

	resp, err := zapFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error().Err(err).Msg("error closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Debug().Msgf("status code: %d", resp.StatusCode)
		return nil, errors.New("invalid status code")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return nil, errors.New("content type is empty")
	}

	content := ""
	for _, mimeType := range AcceptedMimeTypes {
		if strings.Contains(contentType, mimeType) {
			content = mimeType
			break
		}
	}

	if content == "" {
		return nil, errors.New("no valid content type")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body: %w", err)
	}

	if content != MIMEZaparooZapScript {
		return nil, errors.New("invalid content type")
	}

	log.Debug().Msgf("zap link body: %s", string(body))

	return body, nil
}

// isOfflineError returns true if the error is some network connectivity
// related error. Explicit error responses from a server will still return
// false.
func isOfflineError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var t *os.SyscallError
		switch {
		case errors.As(opErr.Err, &t):
			if errors.Is(t.Err, syscall.ECONNREFUSED) || errors.Is(t.Err, syscall.ENETUNREACH) ||
				errors.Is(t.Err, syscall.EHOSTUNREACH) || errors.Is(t.Err, syscall.ETIMEDOUT) {
				return true
			}
		default:
			if strings.Contains(opErr.Err.Error(), "connection refused") ||
				strings.Contains(opErr.Err.Error(), "no such host") ||
				strings.Contains(opErr.Err.Error(), "network is unreachable") ||
				strings.Contains(opErr.Err.Error(), "host is down") {
				return true
			}
		}
	}

	lowerErrStr := strings.ToLower(err.Error())
	if strings.Contains(lowerErrStr, "no such host") ||
		strings.Contains(lowerErrStr, "network is unreachable") ||
		strings.Contains(lowerErrStr, "connection refused") ||
		strings.Contains(lowerErrStr, "host is down") ||
		strings.Contains(lowerErrStr, "i/o timeout") ||
		strings.Contains(lowerErrStr, "tls handshake timeout") {
		return true
	}

	return false
}

func checkZapLink(
	cfg *config.Instance,
	pl platforms.Platform,
	db *database.Database,
	cmd parser.Command,
) (string, error) {
	if len(cmd.Args) == 0 {
		return "", errors.New("no args")
	}
	value := cmd.Args[0]

	if !isZapLink(value, db) {
		return "", nil
	}

	log.Info().Msgf("checking zap link: %s", value)
	body, err := getRemoteZapScript(value)
	if isOfflineError(err) {
		zapscript, err := db.UserDB.GetZapLinkCache(value)
		if err != nil {
			return "", err
		}
		if zapscript != "" {
			return zapscript, nil
		}
	}
	if err != nil {
		return "", err
	}

	err = db.UserDB.UpdateZapLinkCache(value, string(body))
	if err != nil {
		log.Error().Err(err).Msgf("error updating zap link cache")
	}

	if !utils.MaybeJSON(body) {
		return string(body), nil
	} else {
		return "", fmt.Errorf("zapscript JSON not supported")
	}
}
