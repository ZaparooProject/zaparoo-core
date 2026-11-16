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

package tui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/rivo/tview"
)

// mediaServer is one server Core can link to and index.
type mediaServer struct {
	message   func(serverURL string, link *models.AuthLinkStatusResponse) string
	provider  string
	name      string
	urlHelp   string
	urlNeeded bool
}

var mediaServers = []mediaServer{
	{
		provider: models.AuthLinkProviderPlex,
		name:     "Plex",
		urlHelp:  "Leave blank to use the only server on your Plex account.",
	},
	{
		provider:  models.AuthLinkProviderJellyfin,
		name:      "Jellyfin",
		urlHelp:   "The address you open Jellyfin at, like http://192.168.1.10:8096",
		urlNeeded: true,
		message:   jellyfinLinkMessage,
	},
	{
		provider:  models.AuthLinkProviderRomM,
		name:      "RomM",
		urlHelp:   "The address you open RomM at, like http://192.168.1.10:8080",
		urlNeeded: true,
	},
}

// buildMediaServersMenu lists the media servers Core can link to. Each takes
// a server address and a link action that runs the server's own sign-in.
func buildMediaServersMenu(svc SettingsService, pages *tview.Pages, app *tview.Application, goBack func()) {
	frame := NewPageFrame(app).SetTitle("Settings", "Media servers")
	frame.SetOnEscape(goBack)
	buttonBar := NewButtonBar(app).
		AddButton("Back", goBack).
		SetupNavigation(goBack)
	frame.SetButtonBar(buttonBar)

	menu := NewSettingsList(pages, PageSettingsMain)
	menu.SetDynamicHelpMode(true).
		SetHelpCallback(func(desc string) {
			frame.SetHelpText(desc)
		})
	rebuild := func() {
		pages.SwitchToPage(PageSettingsMediaServers)
		app.SetFocus(menu.List)
	}

	for i := range mediaServers {
		server := &mediaServers[i]
		serverURL := ""
		menu.AddHeader(server.name).
			AddTextEdit("Server address", server.urlHelp, &serverURL, &SettingsTextEditOptions{
				App:          app,
				Title:        server.name + " server address",
				HelpText:     server.urlHelp,
				Placeholder:  "http://",
				EmptyDisplay: emptyServerDisplay(server),
				FieldWidth:   48,
				Validate:     validateServerURL,
			}, nil).
			AddAction("Link "+server.name, "Sign in to "+server.name+" and let Core use its library", func() {
				address := strings.TrimSpace(serverURL)
				if server.urlNeeded && address == "" {
					ShowErrorModal(pages, app, "Enter the "+server.name+" server address first.", rebuild)
					return
				}
				runAuthLinkFlow(svc, pages, app, mediaServerLinkRequest(server, address), rebuild)
			})
	}

	frame.SetContent(menu.List)
	menu.TriggerInitialHelp()
	frame.SetupContentToButtonNavigation()
	pages.AddAndSwitchToPage(PageSettingsMediaServers, frame, true)
}

func emptyServerDisplay(server *mediaServer) string {
	if server.urlNeeded {
		return "Not set"
	}
	return "Automatic"
}

func mediaServerLinkRequest(server *mediaServer, serverURL string) *authLinkRequest {
	req := &authLinkRequest{
		provider:  server.provider,
		serverURL: serverURL,
		title:     "Link " + server.name,
		onApproved: func(_ SettingsService, pages *tview.Pages, app *tview.Application, onDone func()) {
			ShowInfoModal(pages, app, server.name+" linked",
				server.name+" is linked. Update the media index from\n"+
					"Manage media to add its library.", onDone)
		},
	}
	if server.message != nil {
		req.message = func(link *models.AuthLinkStatusResponse) string {
			return server.message(serverURL, link)
		}
	}
	return req
}

// jellyfinLinkMessage explains Quick Connect, which is entered from a
// signed-in Jellyfin app rather than a web page of its own.
func jellyfinLinkMessage(serverURL string, link *models.AuthLinkStatusResponse) string {
	t := CurrentTheme()
	accent := colorToHex(t.LabelColor)
	dim := colorToHex(t.SecondaryTextColor)
	displayURL := strings.TrimPrefix(strings.TrimPrefix(serverURL, "https://"), "http://")
	return fmt.Sprintf(
		"Sign in to Jellyfin at [%s::b]%s[-::-], open\n  [%s::b]Settings > Quick Connect[-::-]\n\n"+
			"and enter this code:\n  [%s::b]%s[-::-]\n\n"+
			"[%s]Waiting for approval...[-::-]",
		accent, displayURL, accent, accent, link.UserCode, dim,
	)
}

var errServerURL = errors.New("enter a full address starting with http:// or https://")

func validateServerURL(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parsed, err := url.Parse(text)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errServerURL
	}
	return nil
}
