module github.com/siderolabs/talemu

go 1.22.5

// forked go-yaml that introduces RawYAML interface, which can be used to populate YAML fields using bytes
// which are then encoded as a valid YAML blocks with proper indentation
replace gopkg.in/yaml.v3 => github.com/unix4ever/yaml v0.0.0-20220527175918-f17b0f05cf2c

require (
	github.com/cosi-project/runtime v0.5.2
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/jsimonetti/rtnetlink v1.4.1
	github.com/martinlindhe/base36 v1.1.1
	github.com/mdlayher/arp v0.0.0-20220512170110-6706a2966875
	github.com/mdlayher/genetlink v1.3.2
	github.com/mdlayher/netlink v1.7.2
	github.com/planetscale/vtprotobuf v0.6.0
	github.com/rs/xid v1.5.0
	github.com/siderolabs/crypto v0.4.4
	github.com/siderolabs/gen v0.5.0
	github.com/siderolabs/go-blockdevice v0.4.7
	github.com/siderolabs/go-pointer v1.0.0
	github.com/siderolabs/go-procfs v0.1.2
	github.com/siderolabs/go-retry v0.3.3
	github.com/siderolabs/image-factory v0.4.2
	github.com/siderolabs/omni/client v0.39.0
	github.com/siderolabs/siderolink v0.3.5
	github.com/siderolabs/talos/pkg/machinery v1.8.0-alpha.0.0.20240705104247-3d35e54683b4
	github.com/spf13/cobra v1.8.1
	github.com/stretchr/testify v1.9.0
	go.etcd.io/bbolt v1.3.10
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.0
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba
	golang.org/x/sync v0.7.0
	golang.org/x/sys v0.21.0
	golang.org/x/time v0.5.0
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20230429144221-925a1e7659e6
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2
)

require (
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/containerd/go-cni v1.1.10 // indirect
	github.com/containernetworking/cni v1.2.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gertd/go-pluralize v0.2.1 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/jsimonetti/rtnetlink/v2 v2.0.2 // indirect
	github.com/mdlayher/ethernet v0.0.0-20220221185849-529eae5b6118 // indirect
	github.com/mdlayher/ethtool v0.1.0 // indirect
	github.com/mdlayher/packet v1.1.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/opencontainers/runtime-spec v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/siderolabs/net v0.4.0 // indirect
	github.com/siderolabs/protoenc v0.2.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240617180043-68d350f18fd4 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
