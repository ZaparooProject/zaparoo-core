// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package api

import (
	"net"
	"net/http"
	"time"

	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	// remoteRejectionLogInterval is how often one remote IP's rejections for
	// one reason are logged. Clients reconnect about once a second, so logging every attempt
	// would bury the rest of the log.
	remoteRejectionLogInterval = time.Minute
	// remoteRejectionLogMaxEntries bounds the throttle state so a scan from
	// many addresses cannot grow it without limit.
	remoteRejectionLogMaxEntries = 256
)

const (
	rejectReasonAPIKey      = "missing or invalid API key"
	rejectReasonUnencrypted = "unencrypted remote connections are not allowed on this platform"
	rejectHint              = "enable encryption and pair the client, or use an API key"
)

// remoteRejectionLog reports remote API connections that are turned away
// before a WebSocket is established. A client cannot read the status of a
// failed upgrade, so it shows the same "can't reach" error as a firewalled
// host; this line is the only way to tell the two apart from a log.
type remoteRejectionLog struct {
	clock clockwork.Clock
	last  map[string]time.Time
	mu    syncutil.Mutex
}

func newRemoteRejectionLog(clock clockwork.Clock) *remoteRejectionLog {
	return &remoteRejectionLog{
		clock: clock,
		last:  make(map[string]time.Time),
	}
}

// allow reports whether a rejection from ip for reason should be logged now,
// and records it if so. A different reason from the same IP is logged on its
// own, so a changed cause is not hidden behind the previous one.
func (l *remoteRejectionLog) allow(ip, reason string) bool {
	key := ip + "\x00" + reason
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	last, seen := l.last[key]
	if seen && now.Sub(last) < remoteRejectionLogInterval {
		return false
	}
	if !seen && len(l.last) >= remoteRejectionLogMaxEntries {
		for k, at := range l.last {
			if now.Sub(at) >= remoteRejectionLogInterval {
				delete(l.last, k)
			}
		}
		if len(l.last) >= remoteRejectionLogMaxEntries {
			return false
		}
	}
	l.last[key] = now
	return true
}

// record logs a rejected remote request, at most once per interval per IP and
// reason. A nil receiver logs nothing.
func (l *remoteRejectionLog) record(r *http.Request, reason string) {
	if l == nil {
		return
	}
	// A scoped IPv6 peer such as [fe80::1%eth0]:50123 does not parse as an
	// IP. Key it on the host so each reconnect's new source port does not
	// get logged again.
	ip := r.RemoteAddr
	if parsed := apimiddleware.ParseRemoteIP(r.RemoteAddr); parsed != nil {
		ip = parsed.String()
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	if !l.allow(ip, reason) {
		return
	}
	log.Info().
		Str("remote_ip", ip).
		Str("path", r.URL.Path).
		Str("reason", reason).
		Str("hint", rejectHint).
		Msg("rejected remote API connection")
}
