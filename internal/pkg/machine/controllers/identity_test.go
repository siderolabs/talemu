// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/image-factory/pkg/schematic"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	configv1alpha1 "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/siderolabs/talemu/internal/pkg/machine/controllers"
	"github.com/siderolabs/talemu/internal/pkg/machine/runtime/resources/talos"
)

const (
	communityFactoryHost  = "factory.talos.dev"
	enterpriseFactoryHost = "factory-enterprise.staging.talos.dev"
)

// fakeSource stands in for the boot media source, with both configured factories known so that an image
// reference from either keeps its schematic ID.
type fakeSource struct {
	err error
}

func (fakeSource) GetSchematicByID(context.Context, string, string, string) (*schematic.Schematic, error) {
	return &schematic.Schematic{}, nil
}

func (fakeSource) FactoryHosts() []string {
	return []string{communityFactoryHost, enterpriseFactoryHost}
}

func (s fakeSource) IsEnterprise(_ context.Context, _, factoryHost string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}

	return factoryHost == enterpriseFactoryHost, nil
}

// failingSource cannot tell what the image is, which is what an unreachable factory or Omni looks like.
var failingSource = fakeSource{err: errors.New("the image factory kind could not be determined")}

// TestMachineIdentityFollowsImage runs the version and security state controllers through the
// boot -> install -> upgrade transitions and verifies that the reported version name and FIPS
// state follow the image source, both ways.
func TestMachineIdentityFollowsImage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	// the security state is seeded unowned at machine startup, carrying the static secure boot
	// properties, exactly like the machine does
	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	securityState.TypedSpec().SecureBoot = true
	securityState.TypedSpec().BootedWithUKI = true
	require.NoError(t, st.Create(ctx, securityState))

	// the seeded boot media image: an enterprise boot ISO
	bootImage := talos.NewImage(talos.NamespaceName, talos.ImageID)
	bootImage.TypedSpec().Value.Version = "v1.13.6"
	bootImage.TypedSpec().Value.Host = enterpriseFactoryHost
	require.NoError(t, st.Create(ctx, bootImage))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.VersionController{Source: fakeSource{}}))
	require.NoError(t, rt.RegisterController(&controllers.SecurityStateController{Source: fakeSource{}}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// enterprise boot media: enterprise name, FIPS enabled, secure boot untouched
	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos Enterprise", res.TypedSpec().Name)
		a.Equal("v1.13.6", res.TypedSpec().Version)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateEnabled, res.TypedSpec().FIPSState)
		a.True(res.TypedSpec().SecureBoot)
	})

	// the strict kernel argument flips FIPS to strict
	cmdline := runtime.NewKernelCmdline()
	cmdline.TypedSpec().Cmdline = "talos.platform=metal talos.fips140=strict"
	require.NoError(t, st.Create(ctx, cmdline))

	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateStrict, res.TypedSpec().FIPSState)
	})

	// an upgrade to a community factory image flips the identity back
	_, err = safe.StateUpdateWithConflicts(ctx, st, bootImage.Metadata(), func(res *talos.Image) error {
		res.TypedSpec().Value.Host = communityFactoryHost
		res.TypedSpec().Value.Version = "v1.14.0"

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos", res.TypedSpec().Name)
		a.Equal("v1.14.0", res.TypedSpec().Version)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateDisabled, res.TypedSpec().FIPSState)
		a.True(res.TypedSpec().SecureBoot, "secure boot is a firmware property and never follows the image")
	})

	// A same-version upgrade back to the enterprise factory flips the identity and restores FIPS.
	// The existing strict kernel argument applies again once the running build supports FIPS.
	_, err = safe.StateUpdateWithConflicts(ctx, st, bootImage.Metadata(), func(res *talos.Image) error {
		res.TypedSpec().Value.Host = enterpriseFactoryHost

		return nil
	})
	require.NoError(t, err)

	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos Enterprise", res.TypedSpec().Name)
		a.Equal("v1.14.0", res.TypedSpec().Version)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateStrict, res.TypedSpec().FIPSState)
		a.True(res.TypedSpec().SecureBoot)
	})
}

// TestMachineIdentityCheckerFailure verifies that a broken image factory probe neither blocks the
// boot-readiness version write nor leaves the machine without a reported name: the internal
// version is written, and the version name falls back to the community one.
func TestMachineIdentityCheckerFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	require.NoError(t, st.Create(ctx, securityState))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.VersionController{Source: failingSource}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// the boot-readiness gating version resource is written despite the probe failures
	rtestutils.AssertResource(ctx, t, st, talos.VersionID, func(res *talos.Version, a *assert.Assertions) {
		a.NotEmpty(res.TypedSpec().Value.Value)
	})

	// the version name falls back to the community one instead of staying absent
	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos", res.TypedSpec().Name)
	})
}

// TestMachineIdentityConfigInstall verifies the config-apply install path: a machine with no image state,
// which is one given no boot media information at all, takes its identity from the install image of the
// config applied to it, and follows it across factories.
func TestMachineIdentityConfigInstall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	require.NoError(t, st.Create(ctx, securityState))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.VersionController{Source: fakeSource{}}))
	require.NoError(t, rt.RegisterController(&controllers.SecurityStateController{Source: fakeSource{}}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// nothing installed and no config: no image came from a factory, so the machine is community
	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos", res.TypedSpec().Name)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateDisabled, res.TypedSpec().FIPSState)
	})

	// a config-apply install with a plain registry image keeps the machine community
	provider, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineInstall: &configv1alpha1.InstallConfig{
				InstallImage: "ghcr.io/siderolabs/installer:v1.13.6",
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{},
	})
	require.NoError(t, err)

	require.NoError(t, st.Create(ctx, config.NewMachineConfigWithID(provider, config.ActiveID)))

	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos", res.TypedSpec().Name)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateDisabled, res.TypedSpec().FIPSState)
	})

	// installing from the other configured factory flips the identity, which only works because both
	// factories are known and the reference therefore keeps its schematic
	enterpriseInstall, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineInstall: &configv1alpha1.InstallConfig{
				InstallImage: enterpriseFactoryHost + "/metal-installer/abcd1234:v1.14.0",
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{},
	})
	require.NoError(t, err)

	require.NoError(t, st.Destroy(ctx, config.NewMachineConfigWithID(provider, config.ActiveID).Metadata()))
	require.NoError(t, st.Create(ctx, config.NewMachineConfigWithID(enterpriseInstall, config.ActiveID)))

	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos Enterprise", res.TypedSpec().Name)
		a.Equal("v1.14.0", res.TypedSpec().Version)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateEnabled, res.TypedSpec().FIPSState)
	})
}

// TestMachineIdentityUnparseableInstallImage verifies that an install image the emulator cannot parse, a
// digest-pinned one for instance, leaves both controllers reconciling. Such a config is valid Talos, so
// refusing it would stop the machine from ever reporting its identity again.
func TestMachineIdentityUnparseableInstallImage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	// as if an enterprise image had been reported before, so that a controller which stops writing shows up:
	// the state would simply stay as it is
	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	securityState.TypedSpec().FIPSState = runtime.FIPSStateEnabled
	require.NoError(t, st.Create(ctx, securityState))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.VersionController{Source: fakeSource{}}))
	require.NoError(t, rt.RegisterController(&controllers.SecurityStateController{Source: fakeSource{}}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// a digest-pinned install image carries no tag to read a version out of
	provider, err := container.New(&configv1alpha1.Config{
		ConfigVersion: "v1alpha1",
		MachineConfig: &configv1alpha1.MachineConfig{
			MachineInstall: &configv1alpha1.InstallConfig{
				InstallImage: "ghcr.io/siderolabs/installer@sha256:" + strings.Repeat("a", 64),
			},
		},
		ClusterConfig: &configv1alpha1.ClusterConfig{},
	})
	require.NoError(t, err)

	require.NoError(t, st.Create(ctx, config.NewMachineConfigWithID(provider, config.ActiveID)))

	// the boot-readiness gating version resource is written, falling back to the default version
	rtestutils.AssertResource(ctx, t, st, talos.VersionID, func(res *talos.Version, a *assert.Assertions) {
		a.NotEmpty(res.TypedSpec().Value.Value)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos", res.TypedSpec().Name)
	})

	// the machine counts as running no factory image, so the FIPS state is reported and keeps being
	// reconciled rather than stuck at whatever it last was
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateDisabled, res.TypedSpec().FIPSState)
	})
}
