package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Flags struct {
	Write      *string
	Read       *bool
	Run        *string
	Launch     *string
	Api        *string
	Version    *bool
	Config     *bool
	ShowLoader *string
	ShowPicker *string
	Reload     *bool
}

// SetupFlags defines all common CLI flags between platforms.
func SetupFlags() *Flags {
	return &Flags{
		Write: flag.String(
			"write",
			"",
			"write value to next scanned token",
		),
		Read: flag.Bool(
			"read",
			false,
			"print next scanned token without running",
		),
		Run: flag.String(
			"run",
			"",
			"run value directly as ZapScript",
		),
		Launch: flag.String(
			"launch",
			"",
			"alias of run (DEPRECATED)",
		),
		Api: flag.String(
			"api",
			"",
			"send method and params to API and print response",
		),
		Version: flag.Bool(
			"version",
			false,
			"print version and exit",
		),
		Config: flag.Bool(
			"config",
			false,
			"start the text ui to handle Zaparoo config",
		),
		Reload: flag.Bool(
			"reload",
			false,
			"reload config and mappings from disk",
		),
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// Pre runs flag parsing and actions any immediate flags that don't
// require environment setup. Add any custom flags before running this.
func (f *Flags) Pre(pl platforms.Platform) {
	flag.Parse()

	if *f.Version {
		fmt.Printf("Zaparoo v%s (%s)\n", config.AppVersion, pl.ID())
		os.Exit(0)
	}
}

func runFlag(cfg *config.Instance, value string) {
	data, err := json.Marshal(&models.RunParams{
		Text: &value,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
		os.Exit(1)
	}

	_, err = client.LocalClient(context.Background(), cfg, models.MethodRun, string(data))
	if err != nil {
		log.Error().Err(err).Msg("error running")
		_, _ = fmt.Fprintf(os.Stderr, "Error running: %v\n", err)
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}

// Post actions all remaining common flags that require the environment to be
// set up. Logging is allowed.
func (f *Flags) Post(cfg *config.Instance, _ platforms.Platform) {
	switch {
	case isFlagPassed("write"):
		if *f.Write == "" {
			_, _ = fmt.Fprintf(os.Stderr, "Error: write flag requires a value\n")
			os.Exit(1)
		}

		data, err := json.Marshal(&models.ReaderWriteParams{
			Text: *f.Write,
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
			os.Exit(1)
		}

		enableRun := client.DisableZapScript(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			close(sigs)
			enableRun()
			os.Exit(1)
		}()

		_, err = client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(data))
		if err != nil {
			log.Error().Err(err).Msg("error writing tag")
			_, _ = fmt.Fprintf(os.Stderr, "Error writing tag: %v\n", err)
			close(sigs)
			enableRun()
			os.Exit(1)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Tag: %s written successfully\n", *f.Write)
			close(sigs)
			enableRun()
			os.Exit(0)
		}
	case *f.Read:
		enableRun := client.DisableZapScript(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			close(sigs)
			enableRun()
			os.Exit(0)
		}()

		resp, err := client.WaitNotification(
			context.Background(), 0,
			cfg, models.NotificationTokensAdded,
		)
		if err != nil {
			log.Error().Err(err).Msg("error waiting for notification")
			_, _ = fmt.Fprintf(os.Stderr, "Error waiting for notification: %v\n", err)
			close(sigs)
			enableRun()
			os.Exit(1)
		}

		close(sigs)
		enableRun()
		fmt.Println(resp)
		os.Exit(0)
	case isFlagPassed("launch"):
		if *f.Launch == "" {
			_, _ = fmt.Fprintf(os.Stderr, "Error: launch flag requires a value\n")
			os.Exit(1)
		}
		runFlag(cfg, *f.Launch)
	case isFlagPassed("run"):
		if *f.Run == "" {
			_, _ = fmt.Fprintf(os.Stderr, "Error: run flag requires a value\n")
		}
		runFlag(cfg, *f.Run)
	case isFlagPassed("api"):
		if *f.Api == "" {
			_, _ = fmt.Fprintf(os.Stderr, "Error: api flag requires a value\n")
			os.Exit(1)
		}

		ps := strings.SplitN(*f.Api, ":", 2)
		method := ps[0]
		params := ""
		if len(ps) > 1 {
			params = ps[1]
		}

		resp, err := client.LocalClient(context.Background(), cfg, method, params)
		if err != nil {
			log.Error().Err(err).Msg("error calling API")
			_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(resp)
		os.Exit(0)
	case *f.Reload:
		_, err := client.LocalClient(context.Background(), cfg, models.MethodSettingsReload, "")
		if err != nil {
			log.Error().Err(err).Msg("error reloading settings")
			_, _ = fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	}
}

// Setup initializes the user config and logging. Returns a user config object.
func Setup(pl platforms.Platform, defaultConfig config.Values, writers []io.Writer) *config.Instance {
	err := utils.InitLogging(pl, writers)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing logging: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.NewConfig(utils.ConfigDir(pl), defaultConfig)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.DebugLogging() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	return cfg
}
