package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"os"
)

func tryRunApp(
	app *tview.Application,
	builder func() (*tview.Application, error),
) error {
	if err := app.Run(); err != nil {
		appTty2, err := builder()
		if err != nil {
			return err
		}

		ttyPath := "/dev/tty2"
		if os.Getenv("ZAPAROO_RUN_SCRIPT") == "2" {
			log.Debug().Msg("alternate tty for widgets from zapscript")
			ttyPath = "/dev/tty4"
		}

		tty, err := tcell.NewDevTtyFromDev(ttyPath)
		if err != nil {
			return err
		}

		screen, err := tcell.NewTerminfoScreenFromTty(tty)
		if err != nil {
			return err
		}

		appTty2.SetScreen(screen)

		if err := appTty2.Run(); err != nil {
			return err
		}
	}
	return nil
}
