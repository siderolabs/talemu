// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package controllers

import (
	"context"
	stdlibx509 "crypto/x509"
	"fmt"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/crypto/x509"
	"github.com/siderolabs/gen/optional"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
	"go.uber.org/zap"
)

// KubernetesDynamicCertsController manages secrets.KubernetesDynamicCerts based on configuration.
type KubernetesDynamicCertsController struct{}

// Name implements controller.Controller interface.
func (ctrl *KubernetesDynamicCertsController) Name() string {
	return "secrets.KubernetesDynamicCertsController"
}

// Inputs implements controller.Controller interface.
func (ctrl *KubernetesDynamicCertsController) Inputs() []controller.Input {
	return []controller.Input{
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.KubernetesRootType,
			ID:        optional.Some(secrets.KubernetesRootID),
			Kind:      controller.InputWeak,
		},
		{
			Namespace: secrets.NamespaceName,
			Type:      secrets.CertSANType,
			ID:        optional.Some(secrets.CertSANKubernetesID),
			Kind:      controller.InputWeak,
		},
	}
}

// Outputs implements controller.Controller interface.
func (ctrl *KubernetesDynamicCertsController) Outputs() []controller.Output {
	return []controller.Output{
		{
			Type: secrets.KubernetesDynamicCertsType,
			Kind: controller.OutputExclusive,
		},
	}
}

// Run implements controller.Controller interface.
func (ctrl *KubernetesDynamicCertsController) Run(ctx context.Context, r controller.Runtime, _ *zap.Logger) error {
	refreshTicker := time.NewTicker(KubernetesCertificateValidityDuration / 2)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.EventCh():
		case <-refreshTicker.C:
		}

		k8sRoot, err := safe.ReaderGet[*secrets.KubernetesRoot](ctx, r, resource.NewMetadata(secrets.NamespaceName, secrets.KubernetesRootType, secrets.KubernetesRootID, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				if err = ctrl.teardownAll(ctx, r); err != nil {
					return fmt.Errorf("error destroying resources: %w", err)
				}

				continue
			}

			return fmt.Errorf("error getting root k8s secrets: %w", err)
		}

		certSANs, err := safe.ReaderGet[*secrets.CertSAN](ctx, r, resource.NewMetadata(secrets.NamespaceName, secrets.CertSANType, secrets.CertSANKubernetesID, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return err
		}

		if err = safe.WriterModify(ctx, r, secrets.NewKubernetesDynamicCerts(), func(r *secrets.KubernetesDynamicCerts) error {
			return ctrl.updateSecrets(k8sRoot.TypedSpec(), r.TypedSpec(), certSANs.TypedSpec())
		}); err != nil {
			return err
		}

		r.ResetRestartBackoff()
	}
}

func (ctrl *KubernetesDynamicCertsController) updateSecrets(k8sRoot *secrets.KubernetesRootSpec, k8sCerts *secrets.KubernetesDynamicCertsSpec,
	certSANs *secrets.CertSANSpec,
) error {
	ca, err := x509.NewCertificateAuthorityFromCertificateAndKey(k8sRoot.IssuingCA)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	apiServer, err := x509.NewKeyPair(ca,
		x509.IPAddresses(certSANs.StdIPs()),
		x509.DNSNames(certSANs.DNSNames),
		x509.CommonName("kube-apiserver"),
		x509.Organization("kube-master"),
		x509.NotAfter(time.Now().Add(KubernetesCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature|stdlibx509.KeyUsageKeyEncipherment),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageServerAuth,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to generate api-server cert: %w", err)
	}

	k8sCerts.APIServer = x509.NewCertificateAndKeyFromKeyPair(apiServer)

	apiServerKubeletClient, err := x509.NewKeyPair(ca,
		x509.CommonName(constants.KubernetesAPIServerKubeletClientCommonName),
		x509.Organization(constants.KubernetesAdminCertOrganization),
		x509.NotAfter(time.Now().Add(KubernetesCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature|stdlibx509.KeyUsageKeyEncipherment),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageClientAuth,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to generate api-server cert: %w", err)
	}

	k8sCerts.APIServerKubeletClient = x509.NewCertificateAndKeyFromKeyPair(apiServerKubeletClient)

	aggregatorCA, err := x509.NewCertificateAuthorityFromCertificateAndKey(k8sRoot.AggregatorCA)
	if err != nil {
		return fmt.Errorf("failed to parse aggregator CA: %w", err)
	}

	frontProxy, err := x509.NewKeyPair(aggregatorCA,
		x509.CommonName("front-proxy-client"),
		x509.NotAfter(time.Now().Add(KubernetesCertificateValidityDuration)),
		x509.KeyUsage(stdlibx509.KeyUsageDigitalSignature|stdlibx509.KeyUsageKeyEncipherment),
		x509.ExtKeyUsage([]stdlibx509.ExtKeyUsage{
			stdlibx509.ExtKeyUsageClientAuth,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to generate aggregator cert: %w", err)
	}

	k8sCerts.FrontProxy = x509.NewCertificateAndKeyFromKeyPair(frontProxy)

	return nil
}

func (ctrl *KubernetesDynamicCertsController) teardownAll(ctx context.Context, r controller.Runtime) error {
	list, err := r.List(ctx, resource.NewMetadata(secrets.NamespaceName, secrets.KubernetesDynamicCertsType, "", resource.VersionUndefined))
	if err != nil {
		return err
	}

	// TODO: change this to proper teardown sequence

	for _, res := range list.Items {
		if err = r.Destroy(ctx, res.Metadata()); err != nil {
			return err
		}
	}

	return nil
}
