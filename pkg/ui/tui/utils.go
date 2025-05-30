package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func centerWidget(width, height int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

func pageDefaults[S PrimitiveWithSetBorder](name string, pages *tview.Pages, widget S) tview.Primitive {
	widget.SetBorder(true)
	pages.AddPage(name, widget, true, false)
	return widget
}

func SetTheme(theme *tview.Theme) {
	theme.BorderColor = tcell.ColorLightYellow
	theme.PrimaryTextColor = tcell.ColorWhite
	theme.ContrastSecondaryTextColor = tcell.ColorFuchsia
	theme.PrimitiveBackgroundColor = tcell.ColorDarkBlue
	theme.ContrastBackgroundColor = tcell.ColorBlack
}

func genericModal(
	message string,
	title string,
	action func(buttonIndex int, buttonLabel string),
	withButton bool,
) *tview.Modal {
	modal := tview.NewModal()
	modal.SetTitle(title).
		SetBorder(true).
		SetTitleAlign(tview.AlignCenter)
	modal.SetText(message)
	if withButton {
		modal.AddButtons([]string{"OK"}).
			SetDoneFunc(action)
	}
	return modal
}

type PrimitiveWithSetBorder interface {
	tview.Primitive
	SetBorder(arg bool) *tview.Box
}

// BuildAndRetry attempts to build and display a TUI dialog, retrying with
// alternate settings on error.
// It's used to work around issues on MiSTer, which has an unusual setup for
// showing TUI applications.
func BuildAndRetry(
	builder func() (*tview.Application, error),
) error {
	app, err := builder()
	if err != nil {
		return err
	}
	return tryRunApp(app, builder)
}
