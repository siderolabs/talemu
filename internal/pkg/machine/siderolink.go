// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machine

import (
	"net/url"
	"regexp"

	"github.com/siderolabs/go-procfs/procfs"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// SideroLinkParams is the siderolink params needed to join Omni instance.
type SideroLinkParams struct {
	APIEndpoint    string
	JoinToken      string
	LogsEndpoint   string
	EventsEndpoint string
	Host           string
	Insecure       bool
	TunnelMode     bool
}

// ParseKernelArgs string into siderolink params.
func ParseKernelArgs(kernelArgs string) (*SideroLinkParams, error) {
	cmdline := procfs.NewCmdline(kernelArgs)

	var (
		eventsEndpoint string
		apiEndpoint    string
		joinToken      string
		logsEndpoint   string
		insecure       bool
		tunnelMode     bool
	)

	if s := cmdline.Get(constants.KernelParamEventsSink).Get(0); s != nil {
		eventsEndpoint = *s
	}

	if s := cmdline.Get(constants.KernelParamSideroLink).Get(0); s != nil {
		endpoint, err := parseSiderolinkEndpoint(*s)
		if err != nil {
			return nil, err
		}

		apiEndpoint = endpoint.apiEndpoint
		insecure = endpoint.insecure
		tunnelMode = endpoint.tunnelMode

		if endpoint.joinToken != "" {
			joinToken = endpoint.joinToken
		}
	}

	if s := cmdline.Get(constants.KernelParamLoggingKernel).Get(0); s != nil {
		logsEndpoint = *s
	}

	return &SideroLinkParams{
		Host:           apiEndpoint,
		APIEndpoint:    apiEndpoint,
		JoinToken:      joinToken,
		Insecure:       insecure,
		EventsEndpoint: eventsEndpoint,
		LogsEndpoint:   logsEndpoint,
		TunnelMode:     tunnelMode,
	}, nil
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
