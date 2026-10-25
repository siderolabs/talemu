// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talemu/internal/pkg/machine"
)

// embedded is shaped like the configuration Omni embeds in the boot media from Talos 1.12 on: a user document
// first, then Omni's join documents.
const embedded = `apiVersion: v1alpha1
kind: KmsgLogConfig
name: remote-siem
url: tcp://192.168.1.10:5000
---
apiVersion: v1alpha1
kind: SideroLinkConfig
apiUrl: https://omni.example.org:8090?jointoken=embedded-token
---
apiVersion: v1alpha1
kind: EventSinkConfig
endpoint: '[fdae:41e4:649b:9303::1]:8091'
---
apiVersion: v1alpha1
kind: KmsgLogConfig
name: omni-kmsg
url: tcp://[fdae:41e4:649b:9303::1]:8092
`

func TestApplyConfig(t *testing.T) {
	t.Parallel()

	params, err := machine.ParseKernelArgs("console=ttyS0")
	require.NoError(t, err)

	require.NoError(t, params.ApplyConfig(embedded))

	require.Equal(t, "https://omni.example.org:8090?jointoken=embedded-token", params.APIEndpoint)
	require.Equal(t, "omni.example.org:8090", params.Host)
	require.Equal(t, "embedded-token", params.JoinToken)
	require.False(t, params.Insecure)
	require.Equal(t, "[fdae:41e4:649b:9303::1]:8091", params.EventsEndpoint)
	require.Equal(t, "tcp://[fdae:41e4:649b:9303::1]:8092", params.LogsEndpoint)
	require.NotNil(t, params.Config)
}

func TestApplyConfigWinsOverKernelArgs(t *testing.T) {
	t.Parallel()

	params, err := machine.ParseKernelArgs("siderolink.api=grpc://10.5.0.1:8090?jointoken=kernel-token")
	require.NoError(t, err)

	require.NoError(t, params.ApplyConfig(embedded))

	require.Equal(t, "embedded-token", params.JoinToken)
	require.False(t, params.Insecure)
}

func TestApplyConfigTakesTokenOnlyFromConfig(t *testing.T) {
	t.Parallel()

	params, err := machine.ParseKernelArgs("siderolink.api=grpc://10.5.0.1:8090?jointoken=kernel-token")
	require.NoError(t, err)

	require.NoError(t, params.ApplyConfig(`apiVersion: v1alpha1
kind: SideroLinkConfig
apiUrl: https://omni.example.org:8090
`))

	require.Empty(t, params.JoinToken)
}

func TestApplyConfigEmpty(t *testing.T) {
	t.Parallel()

	params, err := machine.ParseKernelArgs("siderolink.api=grpc://10.5.0.1:8090?jointoken=kernel-token")
	require.NoError(t, err)

	require.NoError(t, params.ApplyConfig(""))

	require.Equal(t, "kernel-token", params.JoinToken)
	require.True(t, params.Insecure)
	require.Nil(t, params.Config)
}
