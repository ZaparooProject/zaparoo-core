//go:build !linux

package tui

import (
	"fmt"

	"github.com/rivo/tview"
)

func tryRunApp(
	app *tview.Application,
	_ func() (*tview.Application, error),
) error {
	if err := app.Run(); err != nil {
		return fmt.Errorf("failed to run application: %w", err)
	}
	return nil
}
