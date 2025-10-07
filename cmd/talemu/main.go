// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the Talemu script.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/omni/client/pkg/constants"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	emuconst "github.com/siderolabs/talemu/internal/pkg/constants"
	emuruntime "github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	schematicsvc "github.com/siderolabs/talemu/internal/pkg/schematic"
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "talemu",
	Short:        "Talos emulator",
	Long:         `Can simulate as many nodes as you want`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		params, err := machine.ParseKernelArgs(cfg.kernelArgs)
		if err != nil {
			return err
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

		if err = os.MkdirAll("_out/state", 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
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

		nc := network.NewClient()

		if err = nc.Run(cmd.Context()); err != nil {
			return err
		}

		defer nc.Close() //nolint:errcheck

		schematicService, err := schematicsvc.NewService(cfg.schematicCacheDir, cfg.useImageInitramfsSource, logger.With(zap.String("component", "schematic_service")))
		if err != nil {
			return err
		}

		initialSchematicID, err := buildInitialSchematicID()
		if err != nil {
			return err
		}

		for i := range cfg.machinesCount {
			m, err := machine.NewMachine(fmt.Sprintf("%04d1802-c798-4da7-a410-f09abb48c8d8", i+1000), logger, emulatorState, schematicService)
			if err != nil {
				return err
			}

			eg.Go(func() error {
				return m.Run(ctx, params, i+1000, kubernetes, machine.WithNetworkClient(nc), machine.WithTalosVersion(cfg.talosVersion), machine.WithSchematic(initialSchematicID))
			})

			machines = append(machines, m)
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

func buildInitialSchematicID() (string, error) {
	initialSchematic := schematic.Schematic{
		Customization: schematic.Customization{
			ExtraKernelArgs: strings.Fields(cfg.kernelArgs),
			SystemExtensions: schematic.SystemExtensions{
				OfficialExtensions: cfg.extensions,
			},
		},
	}

	return initialSchematic.ID()
}

var cfg struct {
	kernelArgs              string
	talosVersion            string
	schematicCacheDir       string
	extensions              []string
	machinesCount           int
	useImageInitramfsSource bool
}

func main() {
	if err := app(); err != nil {
		os.Exit(1)
	}
}

func app() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()

	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.Flags().StringSliceVar(&cfg.extensions, "extensions", []string{emuconst.OfficialExtensionPrefix + "hello-world-service"}, "list of extensions to enable")
	rootCmd.Flags().StringVar(&cfg.kernelArgs, "kernel-args", "", "specify the whole configuration using kernel args string")
	rootCmd.Flags().StringVar(&cfg.talosVersion, "talos-version", constants.DefaultTalosVersion, "specify the Talos version to use")
	rootCmd.Flags().StringVar(&cfg.schematicCacheDir, "schematic-cache-dir", "/tmp/talemu-schematics", "the directory to use for caching schematics")
	rootCmd.Flags().BoolVar(&cfg.useImageInitramfsSource, "use-image-initramfs-source", true, "when extracting the schematic (extensions, kernel args etc.) from a schematic ID, "+
		"fetch the initramfs by pulling the installer image from the image factory. when false, it will be fetched via HTTP instead")

	rootCmd.Flags().IntVar(&cfg.machinesCount, "machines", 1, "the number of machines to emulate")
}
