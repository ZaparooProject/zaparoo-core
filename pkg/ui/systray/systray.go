package systray

import (
	"fmt"
	"fyne.io/systray"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/nixinwang/dialog"
	"github.com/rs/zerolog/log"
	"golang.design/x/clipboard"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func systrayOnReady(
	cfg *config.Instance,
	pl platforms.Platform,
	icon []byte,
) func() {
	return func() {
		openCmd := ""
		if runtime.GOOS == "windows" {
			openCmd = "explorer"
		} else if runtime.GOOS == "darwin" {
			openCmd = "open"
		} else {
			openCmd = "xdg-open"
		}

		systray.SetIcon(icon)
		if runtime.GOOS != "darwin" {
			systray.SetTitle("Zaparoo Core")
		}
		systray.SetTooltip("Zaparoo Core")

		mWebUI := systray.AddMenuItem("Open", "Open Zaparoo web UI")
		ip := utils.GetLocalIP()
		if ip == "" {
			ip = "Unknown"
		}
		mAddress := systray.AddMenuItem("Address: "+ip, "")
		systray.AddSeparator()

		mEditConfig := systray.AddMenuItem("Edit Config", "Edit Core config file")
		mOpenMappings := systray.AddMenuItem("Mappings", "Open Core mappings directory")
		mOpenLaunchers := systray.AddMenuItem("Launchers", "Open Core custom launchers directory")
		mReloadConfig := systray.AddMenuItem("Reload", "Reload Core settings and files")
		mOpenLog := systray.AddMenuItem("View Log", "View Core log file")

		if cfg.DebugLogging() {
			systray.AddSeparator()
		}
		mOpenDataDir := systray.AddMenuItem("Data (Debug)", "Open Core data directory")
		mOpenDataDir.Hide()
		if cfg.DebugLogging() {
			mOpenDataDir.Show()
		}

		systray.AddSeparator()
		mVersion := systray.AddMenuItem("Version "+config.AppVersion, "")
		mVersion.Disable()
		mAbout := systray.AddMenuItem("About Zaparoo Core", "")

		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit and stop Zaparoo service")

		go func() {
			for {
				select {
				case <-mAddress.ClickedCh:
					err := clipboard.Init()
					if err != nil {
						log.Error().Err(err).Msg("failed to initialize clipboard")
						continue
					}
					clipboard.Write(clipboard.FmtText, []byte(ip))
					// TODO: send notification
				case <-mWebUI.ClickedCh:
					url := fmt.Sprintf("http://localhost:%d/app/", cfg.ApiPort())
					err := exec.Command(openCmd, url).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open web page")
					}
				case <-mOpenLog.ClickedCh:
					err := exec.Command(openCmd, filepath.Join(pl.Settings().TempDir, config.LogFile)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open log file")
					}
				case <-mEditConfig.ClickedCh:
					err := exec.Command(openCmd, filepath.Join(pl.Settings().ConfigDir, config.CfgFile)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open config file")
					}
				case <-mOpenMappings.ClickedCh:
					err := exec.Command(openCmd, filepath.Join(pl.Settings().DataDir, platforms.MappingsDir)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open mappings dir")
					}
				case <-mOpenLaunchers.ClickedCh:
					err := exec.Command(openCmd, filepath.Join(pl.Settings().DataDir, platforms.LaunchersDir)).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open mappings dir")
					}
				case <-mReloadConfig.ClickedCh:
					_, err := client.LocalClient(cfg, models.MethodSettingsReload, "")
					if err != nil {
						log.Error().Err(err).Msg("failed to reload config")
					} else {
						log.Info().Msg("reloaded config")
					}
					// TODO: send notification
				case <-mOpenDataDir.ClickedCh:
					err := exec.Command(openCmd, pl.Settings().DataDir).Start()
					if err != nil {
						log.Error().Err(err).Msg("failed to open data dir")
					}
				case <-mAbout.ClickedCh:
					msg := "Zaparoo Core\n" +
						"Version v%s\n\n" +
						"© %d Zaparoo Contributors\n" +
						"License: GPLv3\n\n" +
						"www.zaparoo.org"
					dialog.Message(msg, config.AppVersion, time.Now().Year()).Title("About Zaparoo Core").Info()
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()
	}
}

func Run(
	cfg *config.Instance,
	pl platforms.Platform,
	icon []byte,
	exit func(),
) {
	systray.Run(systrayOnReady(cfg, pl, icon), exit)
}
