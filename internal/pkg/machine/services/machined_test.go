// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/siderolabs/talemu/internal/pkg/machine/services"
)

func TestApplyPartialConfiguration(t *testing.T) {
	partialMachineConfig := `apiVersion: v1alpha1
kind: EventSinkConfig
endpoint: "[fdae:41e4:649b:9303::1]:8090"
---
apiVersion: v1alpha1
kind: KmsgLogConfig
name: omni-kmsg
url: "tcp://[fdae:41e4:649b:9303::1]:8092"`
	apidState := state.WrapCore(namespaced.NewState(inmem.Build))
	globalState := state.WrapCore(namespaced.NewState(inmem.Build))
	logger := zaptest.NewLogger(t)
	machineService := services.NewMachineService("test-machine-id", apidState, globalState, logger)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	resp, err := machineService.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{
		Data: []byte(partialMachineConfig),
		Mode: machine.ApplyConfigurationRequest_AUTO,
	})
	require.NoError(t, err)

	require.Equal(t, machine.ApplyConfigurationRequest_NO_REBOOT, resp.Messages[0].Mode)
}
