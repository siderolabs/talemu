// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers_test

import (
	"context"
	"errors"
	"testing"

	cosiruntime "github.com/cosi-project/runtime/pkg/controller/runtime"
	"github.com/cosi-project/runtime/pkg/resource/rtestutils"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
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

type fakeChecker struct{}

func (fakeChecker) IsEnterprise(_ context.Context, baseURL string) (bool, error) {
	return baseURL == "https://"+enterpriseFactoryHost, nil
}

type failingChecker struct{}

func (failingChecker) IsEnterprise(context.Context, string) (bool, error) {
	return false, errors.New("the factory is unreachable")
}

// TestMachineIdentityFollowsImage drives the version and security state controllers through the
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

	require.NoError(t, rt.RegisterController(&controllers.VersionController{
		Checker:          fakeChecker{},
		ImageFactoryHost: communityFactoryHost,
		BootFactoryURL:   "https://" + enterpriseFactoryHost,
	}))
	require.NoError(t, rt.RegisterController(&controllers.SecurityStateController{
		Checker:          fakeChecker{},
		ImageFactoryHost: communityFactoryHost,
		BootFactoryURL:   "https://" + enterpriseFactoryHost,
	}))

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

	require.NoError(t, rt.RegisterController(&controllers.VersionController{
		Checker:          failingChecker{},
		ImageFactoryHost: communityFactoryHost,
		BootFactoryURL:   "https://" + enterpriseFactoryHost,
	}))

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

// TestMachineIdentityConfigInstall verifies the config-apply install path: a maintenance machine
// with no image state takes its identity from the boot factory, and applying a config with a
// community install image flips it to community even though the boot factory is enterprise.
func TestMachineIdentityConfigInstall(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	securityState := runtime.NewSecurityStateSpec(runtime.NamespaceName)
	require.NoError(t, st.Create(ctx, securityState))

	rt, err := cosiruntime.NewRuntime(st, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, rt.RegisterController(&controllers.VersionController{
		Checker:          fakeChecker{},
		ImageFactoryHost: communityFactoryHost,
		BootFactoryURL:   "https://" + enterpriseFactoryHost,
	}))
	require.NoError(t, rt.RegisterController(&controllers.SecurityStateController{
		Checker:          fakeChecker{},
		ImageFactoryHost: communityFactoryHost,
		BootFactoryURL:   "https://" + enterpriseFactoryHost,
	}))

	runtimeCtx, stopRuntime := context.WithCancel(ctx)

	var eg errgroup.Group

	eg.Go(func() error { return rt.Run(runtimeCtx) })

	t.Cleanup(func() {
		stopRuntime()

		require.NoError(t, eg.Wait())
	})

	// nothing installed, no config: the boot media identity wins
	rtestutils.AssertResource(ctx, t, st, runtime.NewVersion().Metadata().ID(), func(res *runtime.Version, a *assert.Assertions) {
		a.Equal("Talos Enterprise", res.TypedSpec().Name)
	})
	rtestutils.AssertResource(ctx, t, st, runtime.SecurityStateID, func(res *runtime.SecurityState, a *assert.Assertions) {
		a.Equal(runtime.FIPSStateEnabled, res.TypedSpec().FIPSState)
	})

	// a config-apply install with a community image flips the machine to community
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
}
