package cli

import (
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
	"github.com/ZaparooProject/zaparoo-core/pkg/configui"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Flags struct {
	Write        *string
	Read         *bool
	Run          *string
	Launch       *string
	Api          *string
	Clients      *bool
	NewClient    *string
	DeleteClient *string
	Qr           *bool
	Version      *bool
	Config       *bool
	ShowLoader   *string
	ShowPicker   *string
	Reload       *bool
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
		//Clients: flag.Bool(
		//	"clients",
		//	false,
		//	"list all registered API clients and secrets",
		//),
		//NewClient: flag.String(
		//	"new-client",
		//	"",
		//	"register new API client with given display name",
		//),
		//DeleteClient: flag.String(
		//	"delete-client",
		//	"",
		//	"revoke access to API for given client ID",
		//),
		//Qr: flag.Bool(
		//	"qr",
		//	false,
		//	"output a connection QR code along with client details",
		//),
		Version: flag.Bool(
			"version",
			false,
			"print version and exit",
		),
		Config: flag.Bool(
			"config",
			false,
			"start the text ui to handle zaparoo config",
		),
		Reload: flag.Bool(
			"reload",
			false,
			"reload config and mappings from disk",
		),
	}
}

// Pre runs flag parsing and actions any immediate flags that don't
// require environment setup. Add any custom flags before running this.
func (f *Flags) Pre(pl platforms.Platform) {
	flag.Parse()

	if *f.Version {
		fmt.Printf("Zaparoo v%s (%s)\n", config.AppVersion, pl.Id())
		os.Exit(0)
	}
}

type ConnQr struct {
	Id      uuid.UUID `json:"id"`
	Secret  string    `json:"sec"`
	Address string    `json:"addr"`
}

// Post actions all remaining common flags that require the environment to be
// set up. Logging is allowed.
func (f *Flags) Post(cfg *config.Instance, pl platforms.Platform) {
	if *f.Config {
		enabler := client.ZapScriptWrapper(cfg)

		err := configui.ConfigUi(cfg, pl)
		if err != nil {
			log.Error().Err(err).Msg("error starting config ui")
			_, _ = fmt.Fprintf(os.Stderr, "Error starting config UI: %v\n", err)
			os.Exit(1)
		}
		enabler()
		os.Exit(0)
	}

	if *f.Write != "" {
		data, err := json.Marshal(&models.ReaderWriteParams{
			Text: *f.Write,
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
			os.Exit(1)
		}

		enableRun := client.ZapScriptWrapper(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		defer close(sigs)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			enableRun()
			os.Exit(1)
		}()

		_, err = client.LocalClient(cfg, models.MethodReadersWrite, string(data))
		if err != nil {
			log.Error().Err(err).Msg("error writing tag")
			_, _ = fmt.Fprintf(os.Stderr, "Error writing tag: %v\n", err)
			enableRun()
			os.Exit(1)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Tag: %s written sucessfully\n", *f.Write)
			enableRun()
			os.Exit(0)
		}
	} else if *f.Read {
		enableRun := client.ZapScriptWrapper(cfg)

		// cleanup after ctrl-c
		sigs := make(chan os.Signal, 1)
		defer close(sigs)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			enableRun()
			os.Exit(0)
		}()

		resp, err := client.WaitNotification(cfg, models.NotificationTokensAdded)
		if err != nil {
			log.Error().Err(err).Msg("error waiting for notification")
			_, _ = fmt.Fprintf(os.Stderr, "Error waiting for notification: %v\n", err)
			enableRun()
			os.Exit(1)
		}

		enableRun()
		fmt.Println(resp)
		os.Exit(0)
	} else if *f.Run != "" || *f.Launch != "" {
		data, err := json.Marshal(&models.RunParams{
			Text: f.Run,
		})
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
			os.Exit(1)
		}

		_, err = client.LocalClient(cfg, models.MethodRun, string(data))
		if err != nil {
			log.Error().Err(err).Msg("error running")
			_, _ = fmt.Fprintf(os.Stderr, "Error running: %v\n", err)
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	} else if *f.Api != "" {
		ps := strings.SplitN(*f.Api, ":", 2)
		method := ps[0]
		params := ""
		if len(ps) > 1 {
			params = ps[1]
		}

		resp, err := client.LocalClient(cfg, method, params)
		if err != nil {
			log.Error().Err(err).Msg("error calling API")
			_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(resp)
		os.Exit(0)
	} else if *f.Reload {
		_, err := client.LocalClient(cfg, models.MethodSettingsReload, "")
		if err != nil {
			log.Error().Err(err).Msg("error reloading settings")
			_, _ = fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	}

	// clients
	//if *f.Clients {
	//	resp, err := client.LocalClient(cfg, models.MethodClients, "")
	//	if err != nil {
	//		log.Error().Err(err).Msg("error calling API")
	//		_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
	//		os.Exit(1)
	//	}
	//
	//	var clients []models.ClientResponse
	//	err = json.Unmarshal([]byte(resp), &clients)
	//	if err != nil {
	//		log.Error().Err(err).Msg("error decoding API response")
	//		_, _ = fmt.Fprintf(os.Stderr, "Error decoding API response: %v\n", err)
	//	}
	//
	//	for _, c := range clients {
	//		fmt.Println("---")
	//		if c.Name != "" {
	//			fmt.Printf("- Name:   %s\n", c.Name)
	//		}
	//		if c.Address != "" {
	//			fmt.Printf("- Address: %s\n", c.Address)
	//		}
	//		fmt.Printf("- ID:     %s\n", c.Id)
	//		fmt.Printf("- Secret: %s\n", c.Secret)
	//
	//		if *f.Qr {
	//			ip, err := utils.GetLocalIp()
	//			if err != nil {
	//				_, _ = fmt.Fprintf(os.Stderr, "Error getting local IP: %v\n", err)
	//				os.Exit(1)
	//			}
	//
	//			cq := ConnQr{
	//				Id:      c.Id,
	//				Secret:  c.Secret,
	//				Address: ip.String(),
	//			}
	//			respQr, err := json.Marshal(cq)
	//			if err != nil {
	//				_, _ = fmt.Fprintf(os.Stderr, "Error encoding QR code: %v\n", err)
	//				os.Exit(1)
	//			}
	//
	//			qrterminal.Generate(
	//				string(respQr),
	//				qrterminal.L,
	//				os.Stdout,
	//			)
	//		}
	//	}
	//
	//	os.Exit(0)
	//} else if *f.NewClient != "" {
	//	data, err := json.Marshal(&models.NewClientParams{
	//		Name: *f.NewClient,
	//	})
	//	if err != nil {
	//		_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
	//		os.Exit(1)
	//	}
	//
	//	resp, err := client.LocalClient(
	//		cfg,
	//		models.MethodClientsNew,
	//		string(data),
	//	)
	//	if err != nil {
	//		log.Error().Err(err).Msg("error calling API")
	//		_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
	//		os.Exit(1)
	//	}
	//
	//	var c models.ClientResponse
	//	err = json.Unmarshal([]byte(resp), &c)
	//	if err != nil {
	//		log.Error().Err(err).Msg("error decoding API response")
	//		_, _ = fmt.Fprintf(os.Stderr, "Error decoding API response: %v\n", err)
	//	}
	//
	//	fmt.Println("New client registered:")
	//	fmt.Printf("- ID:     %s\n", c.Id)
	//	fmt.Printf("- Name:   %s\n", c.Name)
	//	fmt.Printf("- Secret: %s\n", c.Secret)
	//
	//	if *f.Qr {
	//		ip, err := utils.GetLocalIp()
	//		if err != nil {
	//			_, _ = fmt.Fprintf(os.Stderr, "Error getting local IP: %v\n", err)
	//			os.Exit(1)
	//		}
	//
	//		cq := ConnQr{
	//			Id:      c.Id,
	//			Secret:  c.Secret,
	//			Address: ip.String(),
	//		}
	//		respQr, err := json.Marshal(cq)
	//		if err != nil {
	//			_, _ = fmt.Fprintf(os.Stderr, "Error encoding QR code: %v\n", err)
	//			os.Exit(1)
	//		}
	//
	//		qrterminal.Generate(
	//			string(respQr),
	//			qrterminal.L,
	//			os.Stdout,
	//		)
	//	}
	//
	//	os.Exit(0)
	//} else if *f.DeleteClient != "" {
	//	data, err := json.Marshal(&models.DeleteClientParams{
	//		Id: *f.DeleteClient,
	//	})
	//	if err != nil {
	//		_, _ = fmt.Fprintf(os.Stderr, "Error encoding params: %v\n", err)
	//		os.Exit(1)
	//	}
	//
	//	_, err = client.LocalClient(
	//		cfg,
	//		models.MethodClientsDelete,
	//		string(data),
	//	)
	//	if err != nil {
	//		log.Error().Err(err).Msg("error calling API")
	//		_, _ = fmt.Fprintf(os.Stderr, "Error calling API: %v\n", err)
	//		os.Exit(1)
	//	}
	//
	//	os.Exit(0)
	//}
}

// Setup initializes the user config and logging. Returns a user config object.
func Setup(pl platforms.Platform, defaultConfig config.Values, writers []io.Writer) *config.Instance {
	err := utils.InitLogging(pl, writers)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing logging: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.NewConfig(pl.ConfigDir(), defaultConfig)
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
