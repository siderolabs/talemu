module github.com/siderolabs/talemu

go 1.26.2

// forked go-yaml that introduces RawYAML interface, which can be used to populate YAML fields using bytes
// which are then encoded as a valid YAML blocks with proper indentation
replace gopkg.in/yaml.v3 => github.com/unix4ever/yaml v0.0.0-20220527175918-f17b0f05cf2c

replace (
	k8s.io/api => k8s.io/api v0.35.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.35.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.35.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.35.2
	k8s.io/client-go => k8s.io/client-go v0.35.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.35.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.35.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.35.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.35.2
	k8s.io/cri-api => k8s.io/cri-api v0.35.2
	k8s.io/cri-client => k8s.io/cri-client v0.35.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.35.2
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.35.2
	k8s.io/endpointslice => k8s.io/endpointslice v0.35.2
	k8s.io/externaljwt => k8s.io/externaljwt v0.35.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.35.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.35.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.35.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.35.2
	k8s.io/kubectl => k8s.io/kubectl v0.35.2
	k8s.io/kubelet => k8s.io/kubelet v0.35.2
	k8s.io/metrics => k8s.io/metrics v0.35.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.35.2
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.35.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.35.2
)

require (
	github.com/adrg/xdg v0.5.3
	github.com/akutz/memconn v0.1.0
	github.com/cosi-project/runtime v1.14.1
	github.com/go-logr/zapr v1.3.0
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.3
	github.com/hashicorp/go-multierror v1.1.1
	github.com/jsimonetti/rtnetlink v1.4.2
	github.com/jxskiss/base62 v1.1.0
	github.com/martinlindhe/base36 v1.1.1
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/ethtool v0.5.1
	github.com/mdlayher/genetlink v1.3.2
	github.com/mdlayher/netlink v1.9.0
	github.com/planetscale/vtprotobuf v0.6.1-0.20250313105119-ba97887b0a25
	github.com/rs/xid v1.6.0
	github.com/safchain/ethtool v0.7.0
	github.com/siderolabs/crypto v0.6.5
	github.com/siderolabs/discovery-api v0.1.8
	github.com/siderolabs/discovery-client v0.1.15
	github.com/siderolabs/gen v0.8.6
	github.com/siderolabs/go-api-signature v0.3.12
	github.com/siderolabs/go-circular v0.2.3
	github.com/siderolabs/go-debug v0.6.2
	github.com/siderolabs/go-pointer v1.0.1
	github.com/siderolabs/go-procfs v0.1.2
	github.com/siderolabs/go-retry v0.3.3
	github.com/siderolabs/grpc-proxy v0.5.2
	github.com/siderolabs/image-factory v1.2.0
	github.com/siderolabs/net v0.4.0
	github.com/siderolabs/omni/client v1.7.1
	github.com/siderolabs/siderolink v0.3.16
	github.com/siderolabs/talos/pkg/machinery v1.13.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	go.etcd.io/bbolt v1.4.3
	go.etcd.io/etcd/client/v3 v3.6.9
	go.etcd.io/etcd/server/v3 v3.6.9
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.1
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
	golang.org/x/sync v0.20.0
	golang.org/x/sys v0.43.0
	golang.org/x/time v0.15.0
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20241231184526-a9ab2273dd10
	google.golang.org/grpc v1.80.0
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
	k8s.io/api v0.35.3
	k8s.io/apimachinery v0.35.3
	k8s.io/apiserver v0.35.3
	k8s.io/client-go v0.35.3
	k8s.io/klog/v2 v2.140.0
	k8s.io/kubernetes v1.35.2
)

require (
	cel.dev/expr v0.25.1 // indirect
	cyphar.com/go-pathrs v0.2.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/ProtonMail/go-mime v0.0.0-20230322103455-7d82a3887f2f // indirect
	github.com/ProtonMail/gopenpgp/v2 v2.10.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/containerd/go-cni v1.1.13 // indirect
	github.com/containernetworking/cni v1.3.0 // indirect
	github.com/coreos/go-oidc v2.4.0+incompatible // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.6.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/gertd/go-pluralize v0.2.1 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.5 // indirect
	github.com/go-openapi/swag v0.25.5 // indirect
	github.com/go-openapi/swag/cmdutils v0.25.5 // indirect
	github.com/go-openapi/swag/conv v0.25.5 // indirect
	github.com/go-openapi/swag/fileutils v0.25.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.5 // indirect
	github.com/go-openapi/swag/loading v0.25.5 // indirect
	github.com/go-openapi/swag/mangling v0.25.5 // indirect
	github.com/go-openapi/swag/netutils v0.25.5 // indirect
	github.com/go-openapi/swag/stringutils v0.25.5 // indirect
	github.com/go-openapi/swag/typeutils v0.25.5 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.5 // indirect
	github.com/go-openapi/testify/enable/yaml/v2 v2.4.1 // indirect
	github.com/go-openapi/testify/v2 v2.4.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cel-go v0.28.0 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.1.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/jsimonetti/rtnetlink/v2 v2.2.1-0.20260317095713-310581b9c6ac // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/onsi/ginkgo/v2 v2.28.1 // indirect
	github.com/onsi/gomega v1.39.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.3.0 // indirect
	github.com/opencontainers/selinux v1.13.1 // indirect
	github.com/petermattis/goid v0.0.0-20260226131333-17d1149c6ac6 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pquerna/cachecontrol v0.2.0 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sasha-s/go-deadlock v0.3.9 // indirect
	github.com/siderolabs/proto-codec v0.1.4 // indirect
	github.com/siderolabs/protoenc v0.2.4 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.etcd.io/etcd/api/v3 v3.6.9 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.9 // indirect
	go.etcd.io/etcd/pkg/v3 v3.6.9 // indirect
	go.etcd.io/raft/v3 v3.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.41.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.41.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.4 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/exp v0.0.0-20260312153236-7ab1446f8b90 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/term v0.42.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20250521234502-f333402bd9cb // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/go-jose/go-jose.v2 v2.6.3 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.3 // indirect
	k8s.io/apiextensions-apiserver v0.35.3 // indirect
	k8s.io/cli-runtime v0.35.3 // indirect
	k8s.io/cloud-provider v0.35.2 // indirect
	k8s.io/cluster-bootstrap v0.34.2 // indirect
	k8s.io/component-base v0.35.3 // indirect
	k8s.io/component-helpers v0.35.2 // indirect
	k8s.io/controller-manager v0.35.2 // indirect
	k8s.io/cri-api v0.35.3 // indirect
	k8s.io/csi-translation-lib v0.34.2 // indirect
	k8s.io/dynamic-resource-allocation v0.35.2 // indirect
	k8s.io/endpointslice v0.34.2 // indirect
	k8s.io/externaljwt v0.34.2 // indirect
	k8s.io/kms v0.35.3 // indirect
	k8s.io/kube-aggregator v0.34.2 // indirect
	k8s.io/kube-controller-manager v0.0.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260330154417-16be699c7b31 // indirect
	k8s.io/kube-proxy v0.0.0 // indirect
	k8s.io/kube-scheduler v0.35.3 // indirect
	k8s.io/kubectl v0.35.3 // indirect
	k8s.io/kubelet v0.35.3 // indirect
	k8s.io/metrics v0.35.2 // indirect
	k8s.io/mount-utils v0.34.2 // indirect
	k8s.io/pod-security-admission v0.35.3 // indirect
	k8s.io/utils v0.0.0-20260319190234-28399d86e0b5 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.34.0 // indirect
	sigs.k8s.io/cli-utils v0.37.2 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/kustomize/api v0.21.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.21.1 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
