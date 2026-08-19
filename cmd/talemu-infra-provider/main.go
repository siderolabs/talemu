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
	"github.com/siderolabs/go-debug"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/omni/client/pkg/access"
	"github.com/siderolabs/omni/client/pkg/client"
	omnicli "github.com/siderolabs/omni/client/pkg/client/omni"
	"github.com/siderolabs/omni/client/pkg/imagefactory"
	"github.com/siderolabs/omni/client/pkg/infra"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/auth"
	infrares "github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/panichandler"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/siderolabs/talemu/internal/pkg/bootmedia"
	emuruntime "github.com/siderolabs/talemu/internal/pkg/emu"
	"github.com/siderolabs/talemu/internal/pkg/kubefactory"
	"github.com/siderolabs/talemu/internal/pkg/machine/network"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/emu"
	"github.com/siderolabs/talemu/internal/pkg/provider"
	"github.com/siderolabs/talemu/internal/pkg/provider/clientconfig"
	"github.com/siderolabs/talemu/internal/pkg/provider/meta"
	"github.com/siderolabs/talemu/internal/version"
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

		// There is no infra provider without an Omni to serve, so this is required rather than defaulted.
		if cfg.omniAPIEndpoint == "" {
			return errors.New("--omni-api-endpoint is required: an infra provider only works against an Omni instance")
		}

		if cfg.createServiceAccount {
			if err = createServiceAccountWithRetries(cmd.Context(), logger); err != nil {
				return err
			}
		}

		if cfg.serviceAccountKey == "" {
			return errors.New("no Omni service account key: pass --key, set OMNI_SERVICE_ACCOUNT_KEY, or use --create-service-account against an Omni debug build")
		}

		omniClient, err := client.New(
			cfg.omniAPIEndpoint,
			client.WithServiceAccount(cfg.serviceAccountKey),
			client.WithInsecureSkipTLSVerify(true),
			client.WithOmniClientOptions(omnicli.WithProviderID(meta.ProviderID)),
		)
		if err != nil {
			return err
		}

		defer omniClient.Close() //nolint:errcheck

		omniState, err := infra.NewState(omniClient)
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

		source, err := bootmedia.NewOmniSource(cmd.Context(), cfg.schematicCacheDir, omniClient,
			logger.With(zap.String("component", "boot_media")))
		if err != nil {
			return err
		}

		if err = provider.RegisterControllers(runtime, kubernetes, nc, source, cfg.nodeProxyingDisabled); err != nil {
			return err
		}

		eg, ctx := panichandler.ErrGroupWithContext(cmd.Context())

		eg.Go(func() error {
			// WithState hands the runtime the client built above instead of letting it open a second one.
			// That leaves it without a way to reach Omni for boot assets, so the resolver it would have
			// built for itself is supplied explicitly over the same client.
			return ip.Run(ctx, logger,
				infra.WithState(omniState.State()),
				infra.WithBootAssetResolver(
					func(ctx context.Context, talosVersion string, sch schematic.Schematic, spec provision.BootAssetSpec) (imagefactory.BootAsset, error) {
						return infra.EnsureBootAsset(ctx, omniClient, talosVersion, sch, spec)
					},
				),
				infra.WithEncodeRequestIDsIntoTokens(), infra.WithVersion(version.Tag))
		})

		eg.Go(func() error {
			return runtime.Run(ctx)
		})

		eg.Go(func() error {
			return debug.ListenAndServe(ctx, ":2135", func(msg string) {
				logger.Info(msg)
			})
		})

		return eg.Wait()
	},
}

// createServiceAccountWithRetries mints the provider's own service account and stores its key in the config.
//
// This runs over the debug bootstrap client, which registers a throwaway key for a test user and confirms it
// with a header only an Omni built with the sidero.debug tag honors. The provider itself never uses that
// client: everything after this point goes through the service account created here.
func createServiceAccountWithRetries(ctx context.Context, logger *zap.Logger) error {
	logger.Info("creating service account")

	config := clientconfig.New(cfg.omniAPIEndpoint)

	for {
		rootClient, err := config.GetClient()
		if err != nil {
			return err
		}

		err = createServiceAccount(ctx, rootClient, logger)

		// Closed and re-opened on every attempt to reset the gRPC backoff.
		rootClient.Close() //nolint:errcheck

		if err == nil {
			return nil
		}

		logger.Error("failed to create service account", zap.Error(err))

		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second * 5):
		}
	}
}

func createServiceAccount(ctx context.Context, rootClient *client.Client, logger *zap.Logger) error {
	provider := infrares.NewProvider(meta.ProviderID)

	if err := rootClient.Omni().State().Create(ctx, provider); err != nil && !state.IsConflictError(err) {
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
	schematicCacheDir    string
	createServiceAccount bool
	nodeProxyingDisabled bool
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
	rootCmd.Flags().BoolVar(&cfg.nodeProxyingDisabled, "disable-node-proxying", false,
		"disable node-to-node proxying in apid: rejects the 'node' header, validates that a single-entry 'nodes' header targets this node, multi-node 'nodes' is still proxied")
}
