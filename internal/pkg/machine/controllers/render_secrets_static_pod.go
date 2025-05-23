// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	stdlibtemplate "text/template"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/gen/xslices"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"go.uber.org/zap"

	"github.com/siderolabs/talemu/internal/pkg/machine/machineconfig"
)

// RenderSecretsStaticPodController manages k8s.SecretsReady and renders secrets from secrets.Kubernetes.
type RenderSecretsStaticPodController struct {
	MachineID string
}

// Name implements controller.Controller interface.
func (ctrl *RenderSecretsStaticPodController) Name() string {
	return "k8s.RenderSecretsStaticPodController"
}

// Inputs implements controller.Controller interface.
func (ctrl *RenderSecretsStaticPodController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesRootType,
			ID:        optional.Some(secrets.KubernetesRootID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesType,
			ID:        optional.Some(secrets.KubernetesID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesDynamicCertsType,
			ID:        optional.Some(secrets.KubernetesDynamicCertsID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: config.NamespaceName,
			Type:      config.MachineConfigType,
			ID:        optional.Some(config.ActiveID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *RenderSecretsStaticPodController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: k8s.SecretsStatusType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
//
//nolint:gocyclo,cyclop,gocognit,maintidx
func (ctrl *RenderSecretsStaticPodController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	certsDir := filepath.Join("_out/state/machines", ctrl.MachineID, "certs")

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		}

		// wipe certs dir if the machine is not configured
		config, err := machineconfig.GetComplete(ctx, r)
		if err != nil && !state.IsNotFoundError(err) {
			return err
		}

		if config == nil {
			if err = os.RemoveAll(certsDir); err != nil && !os.IsNotExist(err) {
				return err
			}

			continue
		}

		if !config.Provider().Machine().Type().IsControlPlane() {
			if err = os.RemoveAll(certsDir); err != nil && !os.IsNotExist(err) {
				return err
			}

			continue
		}

		secretsRes, err := safe.ReaderGet[*secrets.Kubernetes](ctx, r, resource.NewMetadata(secrets.NamespaceName, secrets.KubernetesType, secrets.KubernetesID, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting secrets resource: %w", err)
		}

		certsRes, err := safe.ReaderGet[*secrets.KubernetesDynamicCerts](
			ctx, r,
			resource.NewMetadata(secrets.NamespaceName, secrets.KubernetesDynamicCertsType, secrets.KubernetesDynamicCertsID, resource.VersionUndefined),
		)
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting certificates resource: %w", err)
		}

		rootK8sRes, err := safe.ReaderGet[*secrets.KubernetesRoot](ctx, r, resource.NewMetadata(secrets.NamespaceName, secrets.KubernetesRootType, secrets.KubernetesRootID, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return fmt.Errorf("error getting secrets resource: %w", err)
		}

		rootK8sSecrets := rootK8sRes.TypedSpec()
		k8sSecrets := secretsRes.TypedSpec()
		k8sCerts := certsRes.TypedSpec()

		serviceAccountKey, err := rootK8sSecrets.ServiceAccount.GetKey()
		if err != nil {
			return fmt.Errorf("error parsing service account key: %w", err)
		}

		type secret struct {
			getter       func() *x509.PEMEncodedCertificateAndKey
			certFilename string
			keyFilename  string
		}

		type template struct {
			filename string
			template string
		}

		for _, pod := range []struct {
			name      string
			directory string
			secrets   []secret
			templates []template
			uid       int
			gid       int
		}{
			{
				name:      "kube-apiserver",
				directory: certsDir,
				uid:       constants.KubernetesAPIServerRunUser,
				gid:       constants.KubernetesAPIServerRunGroup,
				secrets: []secret{
					{
						getter: func() *x509.PEMEncodedCertificateAndKey {
							return &x509.PEMEncodedCertificateAndKey{
								Crt: bytes.Join(xslices.Map(rootK8sSecrets.AcceptedCAs, func(ca *x509.PEMEncodedCertificate) []byte { return ca.Crt }), nil),
							}
						},
						certFilename: "ca.crt",
					},
					{
						getter:       func() *x509.PEMEncodedCertificateAndKey { return k8sCerts.APIServer },
						certFilename: "apiserver.crt",
						keyFilename:  "apiserver.key",
					},
					{
						getter:       func() *x509.PEMEncodedCertificateAndKey { return k8sCerts.APIServerKubeletClient },
						certFilename: "apiserver-kubelet-client.crt",
						keyFilename:  "apiserver-kubelet-client.key",
					},
					{
						getter: func() *x509.PEMEncodedCertificateAndKey {
							return &x509.PEMEncodedCertificateAndKey{
								Crt: serviceAccountKey.GetPublicKeyPEM(),
								Key: serviceAccountKey.GetPrivateKeyPEM(),
							}
						},
						certFilename: "service-account.pub",
						keyFilename:  "service-account.key",
					},
					{
						getter:       func() *x509.PEMEncodedCertificateAndKey { return rootK8sSecrets.AggregatorCA },
						certFilename: "aggregator-ca.crt",
					},
					{
						getter:       func() *x509.PEMEncodedCertificateAndKey { return k8sCerts.FrontProxy },
						certFilename: "front-proxy-client.crt",
						keyFilename:  "front-proxy-client.key",
					},
				},
			},
		} {
			if err = os.MkdirAll(pod.directory, 0o755); err != nil {
				return fmt.Errorf("error creating secrets directory for %q: %w", pod.name, err)
			}

			for _, secret := range pod.secrets {
				certAndKey := secret.getter()

				if secret.certFilename != "" {
					if err = os.WriteFile(filepath.Join(pod.directory, secret.certFilename), certAndKey.Crt, 0o400); err != nil {
						return fmt.Errorf("error writing certificate %q for %q: %w", secret.certFilename, pod.name, err)
					}

					if err = os.Chown(filepath.Join(pod.directory, secret.certFilename), pod.uid, pod.gid); err != nil {
						return fmt.Errorf("error chowning %q for %q: %w", secret.certFilename, pod.name, err)
					}
				}

				if secret.keyFilename != "" {
					if err = os.WriteFile(filepath.Join(pod.directory, secret.keyFilename), certAndKey.Key, 0o400); err != nil {
						return fmt.Errorf("error writing key %q for %q: %w", secret.keyFilename, pod.name, err)
					}

					if err = os.Chown(filepath.Join(pod.directory, secret.keyFilename), pod.uid, pod.gid); err != nil {
						return fmt.Errorf("error chowning %q for %q: %w", secret.keyFilename, pod.name, err)
					}
				}
			}

			type templateParams struct {
				Root    *secrets.KubernetesRootSpec
				Secrets *secrets.KubernetesCertsSpec
			}

			params := templateParams{
				Root:    rootK8sSecrets,
				Secrets: k8sSecrets,
			}

			for _, templ := range pod.templates {
				var t *stdlibtemplate.Template

				t, err = stdlibtemplate.New(templ.filename).Parse(templ.template)
				if err != nil {
					return fmt.Errorf("error parsing template %q: %w", templ.filename, err)
				}

				var buf bytes.Buffer

				if err = t.Execute(&buf, &params); err != nil {
					return fmt.Errorf("error executing template %q: %w", templ.filename, err)
				}

				if err = os.WriteFile(filepath.Join(pod.directory, templ.filename), buf.Bytes(), 0o400); err != nil {
					return fmt.Errorf("error writing template %q for %q: %w", templ.filename, pod.name, err)
				}

				if err = os.Chown(filepath.Join(pod.directory, templ.filename), pod.uid, pod.gid); err != nil {
					return fmt.Errorf("error chowning %q for %q: %w", templ.filename, pod.name, err)
				}
			}
		}

		if err = safe.WriterModify(ctx, r, k8s.NewSecretsStatus(k8s.ControlPlaneNamespaceName, k8s.StaticPodSecretsStaticPodID), func(r *k8s.SecretsStatus) error {
			r.TypedSpec().Ready = true
			r.TypedSpec().Version = secretsRes.Metadata().Version().String()

			return nil
		}); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}
