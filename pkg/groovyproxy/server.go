package groovyproxy

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"
)

func Start(
	cfg *config.Instance,
	st *state.State,
	itq chan<- tokens.Token,
) {
	// Get from server later
	ctx := st.GetContext()

	coreGMCHost := "127.0.0.1"
	coreGMCPort := 32105
	proxyGMCPort := cfg.GmcProxyPort()
	beaconInterval := cfg.GmcProxyBeaconInterval()

	// Setup socket core->beacon send and GMC receipt
	coreConn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		log.Error().Err(err).Msg("error creating GMC Groovy Core listener socket, aborting GMC Proxy")
	}

	// Allow external GMC command runners to beacon to this proxy for forwarding
	proxyConn, err := net.ListenPacket("udp4", fmt.Sprintf(":%v", proxyGMCPort))
	if err != nil {
		log.Error().Err(err).Msg("error creating GMC Proxy listener socket, aborting GMC Proxy")
		return
	}

	// This address is used to send beacons to the Groovy Core
	coreAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%v:%v", coreGMCHost, coreGMCPort))
	if err != nil {
		log.Error().Err(err).Msg("error resolving Groovy Core GMC network address, aborting GMC Proxy")
		return
	}

	// This address is replaced on the fly as messages are received for forwarding
	var proxyAddr *net.Addr = nil
	proxyAddrChan := make(chan net.Addr)
	// Listen for Proxy Server Beacons to get proxyAddr for forwarding
	go func() {
		defer close(proxyAddrChan)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				buf := make([]byte, 1024)
				_, addr, err := proxyConn.ReadFrom(buf)
				if addr == nil || err != nil {
					log.Error().Err(err).Msg("error reading GMC proxy beacon")
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				proxyAddrChan <- addr
			}
		}
	}()

	// Listen for Core GMC commands
	gmcChan := make(chan []byte)
	go func() {
		defer close(gmcChan)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				buf := make([]byte, 1024)
				rlen, _, err := coreConn.ReadFrom(buf)
				if rlen > 0 && err != nil {
					log.Error().Err(err).Msg("error reading GMC command packet from Groovy core")
					continue
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				gmcChan <- buf[:rlen]
			}
		}
	}()

	freq, _ := time.ParseDuration(beaconInterval)
	beaconTicker := time.NewTicker(time.Duration(freq))
	for {
		select {
		case <-beaconTicker.C:
			_, err := coreConn.WriteTo([]byte{0}, coreAddr)
			if err != nil {
				log.Error().Err(err).Msg("error sending GMC beacon to Groovy core")
			}
		case addr := <-proxyAddrChan:
			proxyAddr = &addr
		case gmcBytes := <-gmcChan:
			log.Debug().Msg("Receieved GMC Load Event")
			// **local: can prefix any valid Zapscript to run locally without proxy
			if bytes.Compare(gmcBytes[:10], []byte("zapscript:")) == 0 {
				log.Debug().Msg("GMC Command is Zapscript Format, running as Token")
				text := string(gmcBytes[10:])
				t := tokens.Token{
					Text:     norm.NFC.String(text),
					ScanTime: time.Now(),
				}
				st.SetActiveCard(t)
				itq <- t
			} else if proxyAddr != nil {
				_, err := proxyConn.WriteTo(gmcBytes, *proxyAddr)
				if err != nil {
					log.Error().Err(err).Msg("error forwarding GMC from Groovy core to proxy")
				}
			} else {
				log.Error().Err(err).Msg("error forwarding GMC from Groovy core to proxy")
			}
		case <-ctx.Done():
			log.Debug().Msg("Closing GMC Proxy via context cancellation")
			beaconTicker.Stop()
			coreConn.Close()
			proxyConn.Close()
		}
	}
}
