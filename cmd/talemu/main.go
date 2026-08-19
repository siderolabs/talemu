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
	"syscall"

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
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "talemu",
	Short:        "Talos emulator",
	Long:         `Can simulate as many nodes as you want`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validateFlags(cmd); err != nil {
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

		media, err := resolveBootMedia(ctx, logger)
		if err != nil {
			return err
		}

		params, err := machine.ParseKernelArgs(media.kernelArgs)
		if err != nil {
			return err
		}

		// With a schematic the args above came out of it, and the machines read the
		// command line from the schematic too, so passing them on as well would report
		// every argument twice. Only the caller knows this, which is why the shared
		// machine code does not try to work it out.
		if media.schematicID != "" {
			params.RawKernelArgs = ""
		}

		for i := range cfg.machinesCount {
			m, err := machine.NewMachine(fmt.Sprintf("%04d1802-c798-4da7-a410-f09abb48c8d8", i+1000), logger, emulatorState, media.source)
			if err != nil {
				return err
			}

			eg.Go(func() error {
				return m.Run(ctx, params, i+1000, kubernetes,
					machine.WithNetworkClient(nc),
					machine.WithTalosVersion(cfg.talosVersion),
					machine.WithSchematic(media.schematicID),
					machine.WithExtensions(cfg.extensions),
					machine.WithNodeProxyingDisabled(cfg.nodeProxyingDisabled),
					machine.WithBootFactoryHost(media.factoryHost),
					machine.WithExtraDisks(cfg.extraDisks))
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

var cfg struct {
	kernelArgs           string
	schematicID          string
	talosVersion         string
	schematicCacheDir    string
	imageFactoryBaseURL  string
	extensions           []string
	machinesCount        int
	extraDisks           int
	nodeProxyingDisabled bool
}

// validateFlags rejects the flag combinations that describe more than one kind of boot media.
//
// A schematic describes the whole media, kernel args included, so taking kernel args as well would mean
// deciding which one wins. Neither is required: SideroLink is optional in Talos, and machines given no boot
// media info at all simply run standalone and join nothing.
func validateFlags(cmd *cobra.Command) error {
	if cfg.extraDisks < 0 || cfg.extraDisks > machine.MaxExtraDisks {
		return fmt.Errorf("--extra-disks must be between 0 and %d, got %d", machine.MaxExtraDisks, cfg.extraDisks)
	}

	if cfg.schematicID == "" {
		return nil
	}

	if cmd.Flags().Changed("kernel-args") {
		return errors.New("--schematic-id and --kernel-args are mutually exclusive: a schematic already carries the kernel args of the media it describes")
	}

	if cmd.Flags().Changed("extensions") {
		return errors.New("--schematic-id and --extensions are mutually exclusive: a schematic already lists the extensions of the media it describes")
	}

	return nil
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
	rootCmd.Flags().StringSliceVar(&cfg.extensions, "extensions", []string{emuconst.OfficialExtensionPrefix + "hello-world-service"},
		"list of extensions to report, for media not built by an image factory (mutually exclusive with --schematic-id)")
	rootCmd.Flags().StringVar(&cfg.kernelArgs, "kernel-args", "",
		"specify the whole configuration using kernel args string, for media not built by an image factory (mutually exclusive with --schematic-id)")
	rootCmd.Flags().StringVar(&cfg.schematicID, "schematic-id", "",
		"schematic ID of the boot media the machines pretend to have booted from; it must exist in the image factory, which supplies the kernel args and extensions")
	rootCmd.Flags().StringVar(&cfg.talosVersion, "talos-version", constants.DefaultTalosVersion, "specify the Talos version to use")
	rootCmd.Flags().StringVar(&cfg.schematicCacheDir, "schematic-cache-dir", "/tmp/talemu-schematics", "the directory to use for caching schematics")
	rootCmd.Flags().StringVar(&cfg.imageFactoryBaseURL, "image-factory-base-url", emuconst.DefaultImageFactoryBaseURL, "base URL of the image factory")
	rootCmd.Flags().IntVar(&cfg.machinesCount, "machines", 1, "the number of machines to emulate")
	rootCmd.Flags().IntVar(&cfg.extraDisks, "extra-disks", 0,
		fmt.Sprintf("the number of additional empty disks to give each machine, between 0 and %d", machine.MaxExtraDisks))
	rootCmd.Flags().BoolVar(&cfg.nodeProxyingDisabled, "disable-node-proxying", false,
		"disable node-to-node proxying in apid: rejects the 'node' header, validates that a single-entry 'nodes' header targets this node, multi-node 'nodes' is still proxied")
}
