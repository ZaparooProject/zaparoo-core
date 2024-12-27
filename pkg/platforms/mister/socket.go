//go:build linux || darwin

package mister

import (
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"net"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/rs/zerolog/log"
)

const (
	ReaderTypePN532   = "PN532"
	ReaderTypeACR122U = "ACR122U"
	ReaderTypeUnknown = "Unknown"
)

func StartSocketServer(
	pl *Platform,
	getLastScan func() *tokens.Token,
	getReaders func() map[string]*readers.Reader,
) (func(), error) {
	socket, err := net.Listen("unix", SocketFile)
	if err != nil {
		log.Error().Msgf("error creating socket: %s", err)
		return nil, err
	}

	running := true
	go func() {
		for {
			if !running {
				return
			}

			conn, err := socket.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Error().Msgf("error accepting connection: %s", err)
				}
				return
			}

			go func(conn net.Conn) {
				defer func(conn net.Conn) {
					err := conn.Close()
					if err != nil {
						log.Warn().Msgf("error closing connection: %s", err)
					}
				}(conn)

				buf := make([]byte, 4096)

				n, err := conn.Read(buf)
				if err != nil {
					log.Error().Msgf("error reading from connection: %s", err)
					return
				}

				if n == 0 {
					return
				}

				payload := ""

				switch strings.TrimSpace(string(buf[:n])) {
				case "status":
					lastScanned := getLastScan()
					if lastScanned != nil {
						payload = fmt.Sprintf(
							"%d,%s,%t,%s",
							lastScanned.ScanTime.Unix(),
							lastScanned.UID,
							true,
							lastScanned.Text,
						)
					} else {
						payload = fmt.Sprintf("0,,%t,", true)
					}
				case "disable":
					log.Info().Msg("DEPRECATED: use API instead")
				case "enable":
					log.Info().Msg("DEPRECATED: use API instead")
				case "connection":
					connected, rt := false, ""
					rs := getReaders()
					rids := make([]string, 0)
					for k := range rs {
						rids = append(rids, k)
					}

					if len(rids) > 0 {
						rid := rids[0]

						lt := getLastScan()
						if lt != nil && !lt.ScanTime.IsZero() && !lt.Remote {
							rid = lt.Source
						}

						reader, ok := rs[rid]
						if ok && reader != nil {
							connected = (*reader).Connected()
							info := (*reader).Info()

							var connProto string
							conn := strings.SplitN(strings.ToLower((*reader).Device()), ":", 2)
							if len(connProto) == 2 {
								connProto = conn[0]
							}

							if connProto == "pn532_uart" {
								rt = ReaderTypePN532
							} else if strings.Contains(info, "ACR122U") {
								rt = ReaderTypeACR122U
							} else {
								rt = ReaderTypeUnknown
							}
						}
					}

					payload = fmt.Sprintf("%t,%s", connected, rt)
				default:
					log.Warn().Msgf("unknown command: %s", strings.TrimRight(string(buf[:n]), "\n"))
				}

				_, err = conn.Write([]byte(payload))
				if err != nil {
					log.Error().Msgf("error writing to socket: %s", err)
					return
				}
			}(conn)
		}
	}()

	return func() {
		running = false
		err := socket.Close()
		if err != nil {
			log.Error().Msgf("error closing socket: %s", err)
		}
	}, nil
}
