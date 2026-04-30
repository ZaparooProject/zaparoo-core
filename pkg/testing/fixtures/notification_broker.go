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

package fixtures

import "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"

type StopNotificationBroker struct {
	Notifications chan models.Notification
	Unsubscribed  chan struct{}
}

func NewStopNotificationBroker() *StopNotificationBroker {
	return &StopNotificationBroker{
		Notifications: make(chan models.Notification),
		Unsubscribed:  make(chan struct{}),
	}
}

func (b *StopNotificationBroker) Subscribe(_ int) (notifications <-chan models.Notification, subscriptionID int) {
	return b.Notifications, 1
}

func (b *StopNotificationBroker) Unsubscribe(_ int) {
	close(b.Unsubscribed)
}
