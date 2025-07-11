package tui

import (
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
)

func BuildExportLogModal(
	pl platforms.Platform,
	app *tview.Application,
	pages *tview.Pages,
	logDestPath string,
	logDestName string,
) tview.Primitive {
	exportPages := tview.NewPages()
	exportPages.SetTitle("Export Log File")

	exportMenu := tview.NewList()
	exportPages.AddAndSwitchToPage("export", exportMenu, true)

	exportMenu.AddItem(
		"Upload to termbin.com",
		"Upload log file to termbin.com and display URL",
		'1', func() {
			outcome := uploadLog(pl, exportPages, app)
			resultModal := genericModal(outcome, "Upload Log File",
				func(buttonIndex int, buttonLabel string) {
					exportPages.RemovePage("upload")
				}, true)
			exportPages.AddPage("upload", resultModal, true, true)
			app.SetFocus(resultModal)
		})
	if logDestPath != "" {
		exportMenu.AddItem(
			"Copy to "+logDestName,
			"Copy log file to a permanent location on disk",
			'2',
			func() {
				outcome := copyLogToSd(pl, logDestPath, logDestName)
				resultModal := genericModal(outcome, "Copy Log File",
					func(buttonIndex int, buttonLabel string) {
						exportPages.RemovePage("copy")
					}, true)
				exportPages.AddPage("copy", resultModal, true, true)
				app.SetFocus(resultModal)
			})
	}
	exportMenu.AddItem("Go back", "Back to main menu", 'b', func() {
		pages.SwitchToPage(PageMain)
	})
	exportMenu.SetSecondaryTextColor(tcell.ColorYellow)

	exportPages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.SwitchToPage(PageMain)
			return nil
		}
		return event
	})

	pageDefaults(PageExportLog, pages, exportPages)
	return exportMenu
}

func copyLogToSd(pl platforms.Platform, logDestPath string, logDestName string) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	newPath := logDestPath
	err := utils.CopyFile(logPath, newPath)
	outcome := ""
	if err != nil {
		outcome = fmt.Sprintf("Unable to copy log file to %s.", logDestName)
		log.Error().Err(err).Msgf("error copying log file")
	} else {
		outcome = fmt.Sprintf("Copied %s to %s.", config.LogFile, logDestName)
	}
	return outcome
}

func uploadLog(pl platforms.Platform, pages *tview.Pages, app *tview.Application) string {
	logPath := path.Join(pl.Settings().TempDir, config.LogFile)
	modal := genericModal("Uploading log file...", "Log upload", func(buttonIndex int, buttonLabel string) {}, false)
	pages.AddPage("temp_upload", modal, true, true)
	app.SetFocus(modal)
	app.ForceDraw()

	uploadCmd := "cat '" + logPath + "' | nc termbin.com 9999"
	out, err := exec.Command("bash", "-c", uploadCmd).Output()
	pages.RemovePage("temp_upload")
	if err != nil {
		log.Error().Err(err).Msgf("error uploading log file to termbin")
		return "Error uploading log file."
	} else {
		return "Log file URL:\n\n" + strings.TrimSpace(string(out))
	}
}
