// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package machine

import (
	"fmt"
	"net/url"
	"regexp"

	"github.com/siderolabs/go-procfs/procfs"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// SideroLinkParams is the siderolink params needed to join Omni instance.
type SideroLinkParams struct {
	// Config is the machine configuration the machine boots with, nil when there is none.
	Config         talosconfig.Provider
	APIEndpoint    string
	JoinToken      string
	LogsEndpoint   string
	EventsEndpoint string
	Host           string
	RawKernelArgs  string
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

	var rawEndpoint string

	if s := cmdline.Get(constants.KernelParamSideroLink).Get(0); s != nil {
		endpoint, err := parseSiderolinkEndpoint(*s)
		if err != nil {
			return nil, err
		}

		// keep the raw URL too: like in Talos, the scheme decides whether the tunnel dials with TLS
		rawEndpoint = *s
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
		APIEndpoint:    rawEndpoint,
		JoinToken:      joinToken,
		Insecure:       insecure,
		EventsEndpoint: eventsEndpoint,
		LogsEndpoint:   logsEndpoint,
		TunnelMode:     tunnelMode,
		RawKernelArgs:  kernelArgs,
	}, nil
}

// ApplyConfig takes the join configuration from the machine configuration the machine boots with. That is the one
// embedded in the boot media, where Omni puts it from Talos 1.12 on instead of the kernel args, or the user data a
// provider hands to the machine. Like in Talos, the machine configuration wins over the kernel args.
func (p *SideroLinkParams) ApplyConfig(machineConfig string) error {
	if machineConfig == "" {
		return nil
	}

	cfg, err := configloader.NewFromBytes([]byte(machineConfig))
	if err != nil {
		return fmt.Errorf("failed to load the machine config: %w", err)
	}

	p.Config = cfg

	if sideroLink := cfg.SideroLink(); sideroLink != nil && sideroLink.APIUrl() != nil {
		rawEndpoint := sideroLink.APIUrl().String()

		endpoint, err := parseSiderolinkEndpoint(rawEndpoint)
		if err != nil {
			return err
		}

		p.APIEndpoint = rawEndpoint
		p.Host = endpoint.apiEndpoint
		p.Insecure = endpoint.insecure
		p.TunnelMode = endpoint.tunnelMode
		p.JoinToken = endpoint.joinToken
	}

	if events := cfg.Runtime().EventsEndpoint(); events != nil {
		p.EventsEndpoint = *events
	}

	// Omni appends its own documents after the user's, so its kernel log destination is the last one.
	if destinations := cfg.Runtime().KmsgLogDestinations(); len(destinations) > 0 {
		p.LogsEndpoint = destinations[len(destinations)-1].Endpoint.String()
	}

	return nil
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
