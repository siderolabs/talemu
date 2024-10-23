module github.com/siderolabs/talemu

go 1.23.0

replace (
	// forked go-yaml that introduces RawYAML interface, which can be used to populate YAML fields using bytes
	// which are then encoded as a valid YAML blocks with proper indentation
	gopkg.in/yaml.v3 => github.com/unix4ever/yaml v0.0.0-20220527175918-f17b0f05cf2c

	// all these rewrites are here to import k8s.io/kubernetes module
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.31.0
	k8s.io/cri-client => k8s.io/cri-client v0.31.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.31.0
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.31.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.31.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.31.0
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.31.0
)

replace (
	// to make goland happy
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.31.0
	k8s.io/endpointslice => k8s.io/endpointslice v0.31.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.31.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.31.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.31.0
	k8s.io/kubectl => k8s.io/kubectl v0.31.0
	k8s.io/metrics => k8s.io/metrics v0.31.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.31.0
)

require (
	github.com/adrg/xdg v0.5.1
	github.com/akutz/memconn v0.1.0
	github.com/cosi-project/runtime v0.6.4
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/jsimonetti/rtnetlink v1.4.2
	github.com/jxskiss/base62 v1.1.0
	github.com/martinlindhe/base36 v1.1.1
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/ethtool v0.2.0
	github.com/mdlayher/genetlink v1.3.2
	github.com/mdlayher/netlink v1.7.2
	github.com/planetscale/vtprotobuf v0.6.1-0.20240917153116-6f2963f01587
	github.com/rs/xid v1.6.0
	github.com/safchain/ethtool v0.4.1
	github.com/siderolabs/crypto v0.5.0
	github.com/siderolabs/discovery-api v0.1.5
	github.com/siderolabs/discovery-client v0.1.10
	github.com/siderolabs/gen v0.6.0
	github.com/siderolabs/go-api-signature v0.3.6
	github.com/siderolabs/go-blockdevice v0.4.8
	github.com/siderolabs/go-circular v0.2.1
	github.com/siderolabs/go-pointer v1.0.0
	github.com/siderolabs/go-procfs v0.1.2
	github.com/siderolabs/go-retry v0.3.3
	github.com/siderolabs/grpc-proxy v0.5.1
	github.com/siderolabs/image-factory v0.5.0
	github.com/siderolabs/net v0.4.0
	github.com/siderolabs/omni/client v0.0.0-20241022154508-8c737ba699ff
	github.com/siderolabs/siderolink v0.3.11
	github.com/siderolabs/talos/pkg/machinery v1.8.1
	github.com/spf13/cobra v1.8.1
	github.com/stretchr/testify v1.9.0
	go.etcd.io/bbolt v1.3.11
	go.etcd.io/etcd/client/v3 v3.5.16
	go.etcd.io/etcd/server/v3 v3.5.16
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.0
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
	golang.org/x/sync v0.8.0
	golang.org/x/sys v0.26.0
	golang.org/x/time v0.7.0
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20230429144221-925a1e7659e6
	google.golang.org/grpc v1.67.1
	google.golang.org/protobuf v1.35.1
	k8s.io/api v0.31.2
	k8s.io/apimachinery v0.31.2
	k8s.io/apiserver v0.31.2
	k8s.io/client-go v0.31.2
	k8s.io/kubernetes v1.31.2
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/ProtonMail/go-crypto v1.1.0-beta.0-proton // indirect
	github.com/ProtonMail/go-mime v0.0.0-20230322103455-7d82a3887f2f // indirect
	github.com/ProtonMail/gopenpgp/v2 v2.7.5 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudflare/circl v1.5.0 // indirect
	github.com/containerd/go-cni v1.1.10 // indirect
	github.com/containernetworking/cni v1.2.3 // indirect
	github.com/coreos/go-oidc v2.2.1+incompatible // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/gertd/go-pluralize v0.2.1 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cel-go v0.21.0 // indirect
	github.com/google/gnostic-models v0.6.9-0.20230804172637-c7be7c783f49 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.22.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jonboulle/clockwork v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/jsimonetti/rtnetlink/v2 v2.0.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runc v1.2.0 // indirect
	github.com/opencontainers/runtime-spec v1.2.0 // indirect
	github.com/opencontainers/selinux v1.11.1 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pquerna/cachecontrol v0.2.0 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.60.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/siderolabs/go-blockdevice/v2 v2.0.3 // indirect
	github.com/siderolabs/proto-codec v0.1.1 // indirect
	github.com/siderolabs/protoenc v0.2.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20220101234140-673ab2c3ae75 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xiang90/probing v0.0.0-20221125231312-a49e3df8f510 // indirect
	go.etcd.io/etcd/api/v3 v3.5.16 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.16 // indirect
	go.etcd.io/etcd/client/v2 v2.305.16 // indirect
	go.etcd.io/etcd/pkg/v3 v3.5.16 // indirect
	go.etcd.io/etcd/raft/v3 v3.5.16 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.56.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.56.0 // indirect
	go.opentelemetry.io/otel v1.31.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.31.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.31.0 // indirect
	go.opentelemetry.io/otel/metric v1.31.0 // indirect
	go.opentelemetry.io/otel/sdk v1.31.0 // indirect
	go.opentelemetry.io/otel/trace v1.31.0 // indirect
	go.opentelemetry.io/proto/otlp v1.3.1 // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/exp v0.0.0-20241009180824-f66d83c29e7c // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/oauth2 v0.23.0 // indirect
	golang.org/x/term v0.25.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	golang.org/x/tools v0.26.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
	google.golang.org/genproto v0.0.0-20241021214115-324edc3d5d38 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20241021214115-324edc3d5d38 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241021214115-324edc3d5d38 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/square/go-jose.v2 v2.6.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.31.2 // indirect
	k8s.io/cloud-provider v0.31.2 // indirect
	k8s.io/cluster-bootstrap v0.31.2 // indirect
	k8s.io/component-base v0.31.2 // indirect
	k8s.io/component-helpers v0.31.2 // indirect
	k8s.io/controller-manager v0.31.2 // indirect
	k8s.io/dynamic-resource-allocation v0.31.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kms v0.31.2 // indirect
	k8s.io/kube-aggregator v0.31.2 // indirect
	k8s.io/kube-openapi v0.0.0-20241009091222-67ed5848f094 // indirect
	k8s.io/kubelet v0.31.2 // indirect
	k8s.io/mount-utils v0.31.2 // indirect
	k8s.io/pod-security-admission v0.31.2 // indirect
	k8s.io/utils v0.0.0-20240921022957-49e7df575cb6 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.0 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
