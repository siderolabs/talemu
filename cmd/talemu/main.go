// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the Talemu script.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/siderolabs/go-procfs/procfs"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	emuruntime "github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "talemu",
	Short:        "Talos emulator",
	Long:         `Can simulate as many nodes as you want`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if cfg.kernelArgs == "" {
			endpoint, err := parseSiderolinkEndpoint(cfg.apiEndpoint)
			if err != nil {
				return err
			}

			cfg.apiEndpoint = endpoint.apiEndpoint
			cfg.insecure = endpoint.insecure
		}

		if err := parseKernelArgs(cfg.kernelArgs); err != nil {
			return err
		}

		params := &machine.SideroLinkParams{
			Host:           cfg.apiEndpoint,
			APIEndpoint:    cfg.apiEndpoint,
			JoinToken:      cfg.joinToken,
			Insecure:       cfg.insecure,
			EventsEndpoint: cfg.eventsEndpoint,
			LogsEndpoint:   cfg.logsEndpoint,
			TunnelMode:     cfg.tunnelMode,
		}

		eg, ctx := errgroup.WithContext(cmd.Context())

		machines := make([]*machine.Machine, 0, cfg.machinesCount)

		loggerConfig := zap.NewDevelopmentConfig()
		loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

		logger, err := loggerConfig.Build(
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
		if err != nil {
			return err
		}

		if err = os.MkdirAll("_out/state", 0o664); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return err
			}
		}

		emulatorState, backingStore, err := runtime.NewState("_out/state/emulator.db", logger)
		if err != nil {
			return err
		}

		defer backingStore.Close() //nolint:errcheck

		if err = emu.Register(ctx, emulatorState); err != nil {
			return err
		}

		kubernetes, err := kubefactory.New(ctx, "_out/state", logger)
		if err != nil {
			return err
		}

		runtime, err := emuruntime.NewRuntime(emulatorState, kubernetes, logger)
		if err != nil {
			return err
		}

		eg.Go(func() error {
			return runtime.Run(ctx)
		})

		for i := range cfg.machinesCount {
			machine, err := machine.NewMachine(fmt.Sprintf("%04d1802-c798-4da7-a410-f09abb48c8d8", i+1000), logger, emulatorState)
			if err != nil {
				return err
			}

			eg.Go(func() error {
				return machine.Run(ctx, params, i+1000, kubernetes)
			})

			machines = append(machines, machine)
		}

		var errors error

		if err := eg.Wait(); err != nil {
			errors = multierr.Append(errors, err)
		}

		eg = &errgroup.Group{}

		errChannel := make(chan error, len(machines))

		eg.Go(func() error {
			count := 0

			for e := range errChannel {
				if e != nil {
					errors = multierr.Append(errors, e)
				}

				count++

				if count == len(machines) {
					break
				}
			}

			return nil
		})

		for _, m := range machines {
			eg.Go(func() error {
				errChannel <- m.Cleanup(context.Background())

				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return err
		}

		return errors
	},
}

var cfg struct {
	wireguardEndpoint string
	apiEndpoint       string
	joinToken         string
	eventsEndpoint    string
	logsEndpoint      string
	host              string

	kernelArgs string

	insecure   bool
	tunnelMode bool

	machinesCount int
}

func main() {
	ctx := context.Background()

	ctx, cancel := context.WithCancel(ctx)

	signals := make(chan os.Signal, 1)

	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

	exitCode := 0

	defer func() {
		signal.Stop(signals)
		cancel()

		os.Exit(exitCode)
	}()

	go func() {
		select {
		case <-signals:
			cancel()

		case <-ctx.Done():
		}
	}()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		exitCode = 1
	}
}

func init() {
	rootCmd.Flags().StringVar(&cfg.wireguardEndpoint, "sidero-link-wireguard-endpoint", "localhost:51821", "advertised Wireguard endpoint")
	rootCmd.Flags().StringVar(&cfg.apiEndpoint, "sidero-link-api-endpoint", "https://localhost:8099", "gRPC API endpoint for the SideroLink")
	rootCmd.Flags().StringVar(&cfg.joinToken, "sidero-link-join-token", "", "join token")
	rootCmd.Flags().StringVar(&cfg.eventsEndpoint, "event-sink-endpoint", "[fdae:41e4:649b:9303::1]:8090", "gRPC API endpoint for the Event Sink")
	rootCmd.Flags().StringVar(&cfg.logsEndpoint, "log-receiver-endpoint", "[fdae:41e4:649b:9303::1]:8092", "TCP log receiver endpoint")

	rootCmd.Flags().StringVar(&cfg.kernelArgs, "kernel-args", "", "specify the whole configuration using kernel args string")

	rootCmd.Flags().IntVar(&cfg.machinesCount, "machines", 1, "the number of machines to emulate")
}

type siderolinkEndpoint struct {
	apiEndpoint string
	joinToken   string
	insecure    bool
	tunnelMode  bool
}

// Parse parses the endpoint from string.
func parseSiderolinkEndpoint(sideroLinkParam string) (*siderolinkEndpoint, error) {
	urlSchemeMatcher := regexp.MustCompile(`[a-zA-z]+://`)

	if !urlSchemeMatcher.MatchString(sideroLinkParam) {
		sideroLinkParam = "grpc://" + sideroLinkParam
	}

	u, err := url.Parse(sideroLinkParam)
	if err != nil {
		return nil, err
	}

	result := siderolinkEndpoint{
		apiEndpoint: u.Host,
		insecure:    u.Scheme == "grpc",
	}

	if token := u.Query().Get("jointoken"); token != "" {
		result.joinToken = token
	}

	if tunnel := u.Query().Get("grpc_tunnel"); tunnel == "true" {
		result.tunnelMode = true
	}

	if u.Port() == "" && u.Scheme == "https" {
		result.apiEndpoint += ":443"
	}

	return &result, nil
}

func parseKernelArgs(kernelArgs string) error {
	if cfg.kernelArgs == "" {
		return nil
	}

	cmdline := procfs.NewCmdline(kernelArgs)

	if s := cmdline.Get(constants.KernelParamEventsSink).Get(0); s != nil {
		cfg.eventsEndpoint = *s
	}

	if s := cmdline.Get(constants.KernelParamSideroLink).Get(0); s != nil {
		endpoint, err := parseSiderolinkEndpoint(*s)
		if err != nil {
			return err
		}

		cfg.apiEndpoint = endpoint.apiEndpoint
		cfg.insecure = endpoint.insecure

		if endpoint.joinToken != "" {
			cfg.joinToken = endpoint.joinToken
		}
	}

	if s := cmdline.Get(constants.KernelParamLoggingKernel).Get(0); s != nil {
		cfg.logsEndpoint = *s
	}

	return nil
}
