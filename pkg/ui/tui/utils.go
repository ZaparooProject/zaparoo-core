package tui

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"os/exec"
	"path"
	"strings"
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
	pages.RemovePage(name)
	pages.AddAndSwitchToPage(name, widget, true)
	return widget
}

func SetTheme(theme *tview.Theme) {
	theme.BorderColor = tcell.ColorLightYellow
	theme.PrimaryTextColor = tcell.ColorWhite
	theme.ContrastSecondaryTextColor = tcell.ColorFuchsia
	theme.PrimitiveBackgroundColor = tcell.ColorDarkBlue
	theme.ContrastBackgroundColor = tcell.ColorBlack
}

func copyLogToSd(pl platforms.Platform, logDestinationPath string) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	newPath := logDestinationPath
	err := utils.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = "Unable to copy log file to SD card."
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = "Copied " + config.LogFile + " to SD card."
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(buttonIndex int, buttonLabel string) {}, false)
	pages.RemovePage("export")
	// fixme: this is not updating, too busy
	pages.AddPage("temp_upload", modal, true, true)
	app.ForceDraw()
	uploadCmd := "cat '" + logPath + "' | nc termbin.com 9999"
	out, err := exec.Command("bash", "-c", uploadCmd).Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Unable to upload log file."
	} else {
		return "Log file URL:\n" + strings.TrimSpace(string(out))
	}
}

func modalBuilder(content tview.Primitive, width int, height int) tview.Primitive {

	itemHeight := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, height, 1, true).
		AddItem(nil, 0, 1, false)

	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(itemHeight, width, 1, true).
		AddItem(nil, 0, 1, false)
}

func genericModal(message string, title string, action func(buttonIndex int, buttonLabel string), withButton bool) *tview.Modal {
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

func BuildAppAndRetry(
	builder func() (*tview.Application, error),
) error {
	app, err := builder()
	if err != nil {
		return err
	}
	return tryRunApp(app, builder)
}
