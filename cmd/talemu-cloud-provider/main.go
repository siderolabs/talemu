// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the Talemu script.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	"github.com/siderolabs/omni/client/api/omni/management"
	"github.com/siderolabs/omni/client/pkg/access"
	"github.com/siderolabs/omni/client/pkg/omni/resources"
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
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "talemu-cloud-provider",
	Short:        "Talos emulator cloud provider",
	Long:         `Connects to Omni as a cloud provider and creates/removes machines for MachineRequests`,
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
			for {
				err = createServiceAccount(cmd.Context())
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

		providerState, err := provider.NewState(cfg.omniAPIEndpoint, cfg.serviceAccountKey)
		if err != nil {
			return err
		}

		defer providerState.Close() //nolint:errcheck

		if err = os.MkdirAll("_out/state", 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}

		emulatorState, backingStore, err := runtime.NewState("_out/state/emulator.db", logger, runtime.NamespacedState{
			Namespace: resources.CloudProviderSpecificNamespacePrefix + meta.ProviderID,
			State:     providerState.State(),
		}, runtime.NamespacedState{
			Namespace: resources.CloudProviderNamespace,
			State:     providerState.State(),
		}, runtime.NamespacedState{
			Namespace: resources.DefaultNamespace,
			State:     providerState.State(),
		})
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

		if err = provider.RegisterControllers(runtime, kubernetes, nc); err != nil {
			return err
		}

		return runtime.Run(cmd.Context())
	},
}

func createServiceAccount(ctx context.Context) error {
	config := clientconfig.New(cfg.omniAPIEndpoint)

	rootClient, err := config.GetClient()
	if err != nil {
		return err
	}

	defer rootClient.Close() //nolint:errcheck

	name := access.CloudProviderServiceAccountPrefix + meta.ProviderID

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

	accounts, err := rootClient.Management().ListServiceAccounts(ctx)
	if err != nil {
		return err
	}

	if slices.ContainsFunc(accounts, func(acc *management.ListServiceAccountsResponse_ServiceAccount) bool {
		return acc.Name == name
	}) {
		err = rootClient.Management().DestroyServiceAccount(ctx, name)
		if err != nil {
			return err
		}
	}

	// create service account with the generated key
	_, err = rootClient.Management().CreateServiceAccount(ctx, name, armoredPublicKey, "CloudProvider", false)

	return err
}

var cfg struct {
	omniAPIEndpoint      string
	serviceAccountKey    string
	kernelArgs           string
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
	rootCmd.Flags().StringVar(&meta.ProviderID, "id", meta.ProviderID, "the id of the cloud provider, it is used to match the resources with the cloud provider label.")
	rootCmd.Flags().StringVar(&cfg.serviceAccountKey, "key", os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"), "Omni service account key, if not set, defaults to OMNI_SERVICE_ACCOUNT_KEY.")
	rootCmd.Flags().BoolVar(&cfg.createServiceAccount, "create-service-account", false,
		"try creating service account for itself (works only if Omni is running in debug mode)")
}
