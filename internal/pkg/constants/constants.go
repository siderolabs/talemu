// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package constants contains Talos emulator specific constants.
package constants

// OmniEndpoint is the Omni endpoint inside the siderolink network.
const OmniEndpoint = "fdae:41e4:649b:9303::1"

// APIDService name.
const APIDService = "apid"

// ETCDService name.
const ETCDService = "etcd"

// KubeletService name.
const KubeletService = "kubelet"

// DefaultImageFactoryBaseURL is the default URL for the Talos image factory.
const DefaultImageFactoryBaseURL = "https://factory.talos.dev"

// EmulatedArchitecture is the architecture the emulated machines claim, in the version they report and
// in the boot media they are provisioned from. Nothing here runs a real image, so it is a claim rather
// than a property of the host.
const EmulatedArchitecture = "amd64"

// OfficialExtensionPrefix is the prefix for official extensions.
const OfficialExtensionPrefix = "siderolabs/"

// ImageFactoryUsernameEnv is the environment variable carrying the optional basic auth username for the
// image factory, needed for schematic reads against an enterprise image factory.
//
// Only for the static mode that is pointed at a factory directly.
const ImageFactoryUsernameEnv = "TALEMU_IMAGE_FACTORY_USERNAME"

// ImageFactoryPasswordEnv is the environment variable carrying the optional basic auth password for the
// image factory. See [ImageFactoryUsernameEnv].
const ImageFactoryPasswordEnv = "TALEMU_IMAGE_FACTORY_PASSWORD"

// StuckBootingKernelArg is a magic kernel arg that makes the emulated machine act broken as long as the
// arg is part of its boot media: the machine stays in the booting stage, never reports ready, and its
// Kubernetes node reports not ready. It simulates a machine broken by a bad kernel args or extensions
// change, and the machine recovers when a schematic without the arg is installed.
const StuckBootingKernelArg = "talemu.stuck=booting"
