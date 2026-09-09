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

package apidiag

import "strings"

// Keep this list explicit: a custom or invalid method name can contain private
// input. The API registration test requires intentional coverage for new methods.
var knownMethods = func() map[string]struct{} {
	methods := strings.Fields(`
launch run confirm ui ui.respond run.script stop tokens media
media.generate media.generate.cancel media.generate.resume media.index
media.search media.tags media.tags.update media.meta.update media.active
media.history media.history.latest media.history.top media.lookup media.meta media.image
scrapers media.scrape media.scrape.status media.scrape.cancel media.scrape.resume
media.browse media.browse.index media.control media.active.update media.clean.orphans
settings settings.update settings.reload settings.logs.download
settings.backup settings.backup.list settings.backup.inspect settings.backup.delete
settings.backup.restore settings.backup.status settings.backup.remote.run
settings.backup.remote.list settings.backup.remote.restore
settings.playtime.limits settings.playtime.limits.update playtime playtime.extend
clients clients.current clients.delete clients.pair.start clients.pair.cancel
profiles profiles.new profiles.update profiles.delete profiles.active profiles.switch profiles.verify
systems launchers launchers.refresh tokens.history mappings mappings.new mappings.delete
mappings.update mappings.reload readers readers.write readers.write.cancel version health
inbox inbox.delete inbox.clear settings.auth.claim settings.auth.status settings.auth.unlink
settings.auth.link settings.auth.link.status settings.auth.link.cancel
update.check update.status update.apply input.keyboard input.gamepad screenshot media.title.parse remote.activity
`)
	result := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		result[method] = struct{}{}
	}
	return result
}()

func MethodName(method string) string {
	if len(method) > 64 {
		return "unknown"
	}
	method = strings.ToLower(method)
	if _, known := knownMethods[method]; known {
		return method
	}
	return "unknown"
}
