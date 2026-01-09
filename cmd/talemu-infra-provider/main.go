// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the Talemu script.
package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	"github.com/siderolabs/omni/client/pkg/access"
	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/auth"
	infrares "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/panichandler"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	emuruntime "github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/provider"
	"github.com/siderolabs/talemu/internal/pkg/provider/clientconfig"
	"github.com/siderolabs/talemu/internal/pkg/provider/meta"
	"github.com/siderolabs/talemu/internal/pkg/schematic"
)

//go:embed data/schema.json
var schema string

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "talemu-infra-provider",
	Short:        "Talos emulator infra provider",
	Long:         `Connects to Omni as a infra provider and creates/removes machines for MachineRequests`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loggerConfig := zap.NewDevelopmentConfig()
		loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

		logger, err := loggerConfig.Build(
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
		if err != nil {
			return err
		}

		if cfg.createServiceAccount {
			logger.Info("creating service account")
			for {
				err = createServiceAccount(cmd.Context(), logger)
				if err == nil {
					break
				}

				logger.Error("failed to create service account", zap.Error(err))

				select {
				case <-cmd.Context().Done():
					return err
				case <-time.After(time.Second * 5):
				}
			}
		}

		if err = os.MkdirAll("_out/state", 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}

		emulatorState, backingStore, err := runtime.NewState("_out/state/emulator.db", logger)
		if err != nil {
			return err
		}

		defer backingStore.Close() //nolint:errcheck

		if err = emu.Register(cmd.Context(), emulatorState); err != nil {
			return err
		}

		kubernetes, err := kubefactory.New(cmd.Context(), "_out/state", logger)
		if err != nil {
			return err
		}

		runtime, err := emuruntime.NewRuntime(emulatorState, kubernetes, logger)
		if err != nil {
			return err
		}

		nc := network.NewClient()

		if err = nc.Run(cmd.Context()); err != nil {
			return err
		}

		defer nc.Close() //nolint:errcheck

		provisioner := provider.NewProvisioner(emulatorState)

		ip, err := infra.NewProvider(meta.ProviderID, provisioner, infra.ProviderConfig{
			Name:        "Talemu",
			Description: "Emulates fake Talos nodes connected to Omni",
			Schema:      schema,
		})
		if err != nil {
			return err
		}

		schematicService, err := schematic.NewService(cfg.schematicCacheDir, logger.With(zap.String("component", "schematic_service")))
		if err != nil {
			return err
		}

		if err = provider.RegisterControllers(runtime, kubernetes, nc, schematicService); err != nil {
			return err
		}

		eg, ctx := panichandler.ErrGroupWithContext(cmd.Context())

		eg.Go(func() error {
			return ip.Run(ctx, logger, infra.WithOmniEndpoint(cfg.omniAPIEndpoint), infra.WithClientOptions(
				client.WithServiceAccount(cfg.serviceAccountKey),
				client.WithInsecureSkipTLSVerify(true),
			))
		})

		eg.Go(func() error {
			return runtime.Run(ctx)
		})

		return eg.Wait()
	},
}

func createServiceAccount(ctx context.Context, logger *zap.Logger) error {
	config := clientconfig.New(cfg.omniAPIEndpoint)

	rootClient, err := config.GetClient()
	if err != nil {
		return err
	}

	defer rootClient.Close() //nolint:errcheck

	provider := infrares.NewProvider(meta.ProviderID)

	if err = rootClient.Omni().State().Create(ctx, provider); err != nil && !state.IsConflictError(err) {
		return err
	}

	name := access.InfraProviderServiceAccountPrefix + meta.ProviderID

	sa := access.ParseServiceAccountFromName(name)

	key, err := pgp.GenerateKey(sa.BaseName, "", sa.FullID(), 365*24*time.Hour)
	if err != nil {
		return err
	}

	armoredPublicKey, err := key.ArmorPublic()
	if err != nil {
		return err
	}

	cfg.serviceAccountKey, err = serviceaccount.Encode(name, key)
	if err != nil {
		return err
	}

	identity, err := safe.ReaderGetByID[*auth.Identity](ctx, rootClient.Omni().State(), sa.FullID())
	if err != nil && !state.IsNotFoundError(err) {
		return err
	}

	if identity != nil {
		logger.Info("delete service account")

		err = rootClient.Management().DestroyServiceAccount(ctx, name)
		if err != nil {
			return err
		}
	}

	// create service account with the generated key
	_, err = rootClient.Management().CreateServiceAccount(ctx, name, armoredPublicKey, "InfraProvider", false)

	return err
}

var cfg struct {
	omniAPIEndpoint      string
	serviceAccountKey    string
	kernelArgs           string
	schematicCacheDir    string
	createServiceAccount bool
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
	rootCmd.Flags().StringVar(&cfg.omniAPIEndpoint, "omni-api-endpoint", os.Getenv("OMNI_ENDPOINT"),
		"the endpoint of the Omni API, if not set, defaults to OMNI_ENDPOINT env var.")
	rootCmd.Flags().StringVar(&meta.ProviderID, "id", meta.ProviderID, "the id of the infra provider, it is used to match the resources with the infra provider label.")
	rootCmd.Flags().StringVar(&cfg.serviceAccountKey, "key", os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"), "Omni service account key, if not set, defaults to OMNI_SERVICE_ACCOUNT_KEY.")
	rootCmd.Flags().StringVar(&cfg.schematicCacheDir, "schematic-cache-dir", "/tmp/talemu-schematics", "the directory to use for caching schematics")
	rootCmd.Flags().BoolVar(&cfg.createServiceAccount, "create-service-account", false,
		"try creating service account for itself (works only if Omni is running in debug mode)")
}
