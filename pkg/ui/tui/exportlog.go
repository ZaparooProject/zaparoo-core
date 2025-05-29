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

func BuildExportLog(
	pl platforms.Platform,
	app *tview.Application,
	pages *tview.Pages,
	logDestPath string,
	logDestName string,
) *tview.List {
	logExport := tview.NewList()

	logExport.
		AddItem("Upload to termbin.com", "", '1', func() {
			outcome := uploadLog(pl, pages, app)
			modal := genericModal(outcome, "Upload Log File", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("upload")
			}, true)
			pages.AddPage("upload", modal, true, true)
		})

	if logDestPath != "" {
		logExport.AddItem("Copy to "+logDestName, "", '2', func() {
			outcome := copyLogToSd(pl, logDestPath, logDestName)
			modal := genericModal(outcome, "Copy Log File", func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("copy")
			}, true)
			pages.AddPage("copy", modal, true, true)
		})
	}

	logExport.AddItem("Go back", "Back to main menu", 'b', func() {
		pages.SwitchToPage(PageMain)
	})

	logExport.SetTitle("Export Log File")
	logExport.SetSecondaryTextColor(tcell.ColorYellow)

	pageDefaults(PageExportLog, pages, logExport)
	return logExport
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
	pages.RemovePage("export")
	pages.AddPage("temp_upload", modal, true, true)
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
