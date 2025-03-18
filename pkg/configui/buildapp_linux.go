package configui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

		tty, err := tcell.NewDevTtyFromDev("/dev/tty2")
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
