## [Talemu 1.0.0](https://github.com/siderolabs/talemu/releases/tag/v1.0.0) (2026-07-23)

Welcome to the v1.0.0 release of Talemu!



Please try out the release binaries and report any issues at
https://github.com/siderolabs/talemu/issues.

### Initial Release

This is the first release of Talemu, an internal-only tool for Sidero Labs.


### Contributors

* Artem Chernyshev
* Edward Sammut Alessi
* Utku Ozdemir
* Oguz Kilcan
* Andrey Smirnov
* Orzelius

### Changes
<details><summary>70 commits</summary>
<p>

* [`23122da`](https://github.com/siderolabs/talemu/commit/23122da14655b012835aa0c97cd2b47e43e7925a) fix: keep populating the legacy discovery service fields
* [`dc0a194`](https://github.com/siderolabs/talemu/commit/dc0a1944741b07dc00bcde34a5b325d910f3df78) refactor: simplify enterprise factory handling
* [`8061262`](https://github.com/siderolabs/talemu/commit/80612624466be4fec9b26183a7c3f26fcf49c56c) feat: report version to omni client
* [`c077a83`](https://github.com/siderolabs/talemu/commit/c077a8382da38c2683923571e7ce00ea06d6859c) feat: add versioning to talemu
* [`b21024a`](https://github.com/siderolabs/talemu/commit/b21024a8c49ac173a1b6e483fbd3114b07fb9f29) chore: rekres
* [`6032ce7`](https://github.com/siderolabs/talemu/commit/6032ce75792044f4895ead0bae28cc7b64f7cc28) chore: bump talos machinery
* [`b8bd109`](https://github.com/siderolabs/talemu/commit/b8bd109c1c1eb9ac2508c47a25b03a520963d85a) feat: support emulating Talos Enterprise machines
* [`2a1f250`](https://github.com/siderolabs/talemu/commit/2a1f250137237f28b5c6ce3d63b3d2cd710f5067) fix: write cluster and control-plane labels with etcd member
* [`2c18419`](https://github.com/siderolabs/talemu/commit/2c184197edcdbd21d6820cf583b88265d9c2c6a1) fix: unblock Talos upgrades utilizing LifecycleService.Upgrade
* [`a9c8698`](https://github.com/siderolabs/talemu/commit/a9c8698797c2736c9a35a71ca3fcd430ac351a69) feat: emulate maintenance install/upgrade and boot id
* [`f101d1f`](https://github.com/siderolabs/talemu/commit/f101d1f22ac262822c149edb19baccc83190b8d0) fix: disable idle mode on in-process loopback connection
* [`6b74f17`](https://github.com/siderolabs/talemu/commit/6b74f17d82d358e90334623e7ced14cc8f884d73) chore: support UUID conflict resolution and UUID overrides
* [`01e3528`](https://github.com/siderolabs/talemu/commit/01e35285db55bdc7f36f0c8d4bc00d2c755162c9) chore: bump Omni client
* [`1a3a70e`](https://github.com/siderolabs/talemu/commit/1a3a70e837de32e41b764952cd1f9e04164fcac1) fix: kube api server URL warnings
* [`bb0eb54`](https://github.com/siderolabs/talemu/commit/bb0eb545d689961fef1d2784e45f00fcf47df2ca) fix: add missing dis_vulnceck_version to kres
* [`52fad46`](https://github.com/siderolabs/talemu/commit/52fad4600cf490e54a2f7178f89a06e1b5df11b8) fix: keep apid maintenance listener up while certs generate
* [`e65d0b3`](https://github.com/siderolabs/talemu/commit/e65d0b3a28a8d2f7f5e6aaad2522bfadacebc1af) chore: rekres and bump deps
* [`5f6478e`](https://github.com/siderolabs/talemu/commit/5f6478e543b86671daf3662c0fb11048512ce22c) fix: reset grpc backoff between sa retries
* [`29531d9`](https://github.com/siderolabs/talemu/commit/29531d98937665e1babbc78bd4f6cd2af93b460e) feat: allow configuring factory url
* [`9554483`](https://github.com/siderolabs/talemu/commit/955448395a3f83adbe0635b3167dfb6f146cd10c) chore: bump dependencies
* [`7fab116`](https://github.com/siderolabs/talemu/commit/7fab116aa9c765314230a97df383543f5728c5e9) refactor: add --disable-node-proxying flag to apid director
* [`2e05d22`](https://github.com/siderolabs/talemu/commit/2e05d22eae66a6b3aff2f23332d6216e7afab757) feat: add extra data for talos disks, devices, performance
* [`c49234c`](https://github.com/siderolabs/talemu/commit/c49234c470a65cc321c0a82a11127295582fa92a) fix: prevent kube-apiserver fatal from killing talemu
* [`6f9a23d`](https://github.com/siderolabs/talemu/commit/6f9a23d5507423edaa610280c28e1137abfe0c93) chore: bump deps, rekres and fix linter errors
* [`f1e6803`](https://github.com/siderolabs/talemu/commit/f1e68033b62dcc68cee24679190e4b1d3ff63352) chore: enable pprof server in the Talemu provider
* [`00836d2`](https://github.com/siderolabs/talemu/commit/00836d266e1272d04d00df3783e071b00d1b0bed) fix: make sure that schematic cache dir is present
* [`7b405ea`](https://github.com/siderolabs/talemu/commit/7b405ea2f7e1f154ef658159d5fc9e5a495b4edb) feat: use image factory schematics endpoint to get schematics data
* [`6dff15b`](https://github.com/siderolabs/talemu/commit/6dff15bb087f11318014e8501f9325a814b58253) chore: update go deps to address govuln
* [`d45df01`](https://github.com/siderolabs/talemu/commit/d45df01b3a177065d45c6c8cb567e31b8629f61c) chore: run make lint-fmt
* [`11bb881`](https://github.com/siderolabs/talemu/commit/11bb881e36e916876bb21e811919d90635120bea) chore: rekres and update readme with infra-provider notes
* [`98062cc`](https://github.com/siderolabs/talemu/commit/98062cc38ab1eb8a091e385daa32a37417849d76) feat: add schematic id to schematic conversion and cmdline support
* [`8081839`](https://github.com/siderolabs/talemu/commit/808183908a9b10d361b4c5dfe8dff4c862edbd2c) chore: add gomock version to docker & make
* [`54bfaf6`](https://github.com/siderolabs/talemu/commit/54bfaf6e70ba2e7491cfe348a7cee7ba04d2f7e5) refactor: update and rekres
* [`35d0eb4`](https://github.com/siderolabs/talemu/commit/35d0eb44ad9ce216fbe5809e236c94da07fdbf29) feat: implement `.EtcdStatus` endpoint in machine API
* [`48678c4`](https://github.com/siderolabs/talemu/commit/48678c49dbcd5b8cc04752b81352bb2f69159ff5) chore: add build step name and rekres
* [`f71cb1b`](https://github.com/siderolabs/talemu/commit/f71cb1b0cd46880f2c70da8a78608f4414e2d487) fix: properly check for the service account existence during startup
* [`6a66674`](https://github.com/siderolabs/talemu/commit/6a66674f607658afba98bab744250d0c10831d0d) fix: make Talemu more reliable in the CI
* [`86fdb3b`](https://github.com/siderolabs/talemu/commit/86fdb3bbc3857ebc1b429c175880376511e65055) chore: bump deps
* [`7c750b8`](https://github.com/siderolabs/talemu/commit/7c750b856863f3d3db53c51efefe9469efab8962) feat: support disk resources which were introduced in 1.8
* [`2c12932`](https://github.com/siderolabs/talemu/commit/2c12932b2c2b037d342c0f4a6ab06d84234eb2e6) feat: allow specifying custom Talos version in talemu
* [`f54299d`](https://github.com/siderolabs/talemu/commit/f54299d29d25831370adc312f96e6c82a0146343) feat: support partial machine configs
* [`6bea7b2`](https://github.com/siderolabs/talemu/commit/6bea7b2bdf822d9fcb2a5bc7678266f49c55308e) feat: correctly support unique token flow in the emulator
* [`1d11086`](https://github.com/siderolabs/talemu/commit/1d11086490fa228a2a4353ce40115e3730147308) chore: bump Go dependencies
* [`cf04fa2`](https://github.com/siderolabs/talemu/commit/cf04fa2b3fe61965f2f13e22278ca2702a8cb435) feat: mock secureboot enabled
* [`8ebd68a`](https://github.com/siderolabs/talemu/commit/8ebd68a27ce9572d7a2ba930ec187da496f0dcef) chore: bump go dependencies
* [`d549eda`](https://github.com/siderolabs/talemu/commit/d549eda02be893d219309011dee82c8e0dda8b9e) fix: temporarily revert the Omni client update
* [`77f4053`](https://github.com/siderolabs/talemu/commit/77f40538a18cd1d72ea39741d1093793eabe822b) chore: bump omni client version
* [`d7b70be`](https://github.com/siderolabs/talemu/commit/d7b70be6702bf0daa7b58e97e2e27837de3216f6) chore: update Omni client
* [`ab2d1ed`](https://github.com/siderolabs/talemu/commit/ab2d1edc92683dffdd26a91ef36bd8724e53c513) fix: make Omni integration tests work against provider running in docker
* [`e3651e5`](https://github.com/siderolabs/talemu/commit/e3651e5b1fb1b1da0d568143ec09c15af87b0baf) chore: bump dependencies
* [`a22b088`](https://github.com/siderolabs/talemu/commit/a22b088bf8065c1fd415cf9048e05ce549c073c7) chore: use common infra provider library
* [`602810d`](https://github.com/siderolabs/talemu/commit/602810d042f70a773b250df4d83a9733c8ba4b64) feat: implement `MetaWrite` and `MetaDelete` endpoints
* [`749f8e4`](https://github.com/siderolabs/talemu/commit/749f8e402c4837eeb9492ea166f23d83fdf369b4) fix: improve emulator cloud provider reliability
* [`9b87697`](https://github.com/siderolabs/talemu/commit/9b876975141d7247792488cae35d8ebeb60338b4) feat: support cloud provider mode
* [`c223eaa`](https://github.com/siderolabs/talemu/commit/c223eaa50b2c3b45c27d958040edaeb105053d30) feat: support running emulator on the same host with Omni
* [`f76f7ae`](https://github.com/siderolabs/talemu/commit/f76f7aed508512b9f4e7e8b8b39c449fde686fe6) fix: bind events source to the siderolink address
* [`58c21fb`](https://github.com/siderolabs/talemu/commit/58c21fbcb8143485334b558d06a6640633fbad6e) feat: simulate reboots in the reboot and upgrade API
* [`a920db0`](https://github.com/siderolabs/talemu/commit/a920db074369ed82cd8e393a0b9c229212d4670b) feat: support image pull and image list API
* [`9552c30`](https://github.com/siderolabs/talemu/commit/9552c302a4b23be9eb7ce550be11fb766b1c80e5) feat: render proper Kubernetes state
* [`4c1bfa6`](https://github.com/siderolabs/talemu/commit/4c1bfa6ce13b9cb1ed489915256a755b8faf7c9d) feat: set up kubernetes api server for each machine
* [`ae49d4d`](https://github.com/siderolabs/talemu/commit/ae49d4dd32e40261220b1d41c9f400ff6308928e) feat: support discovery service parts
* [`5a2062e`](https://github.com/siderolabs/talemu/commit/5a2062e5b60c5f12e2083741c06f43b4bd8ff74a) fix: make more Omni integration tests happy
* [`567eeaf`](https://github.com/siderolabs/talemu/commit/567eeaf8266de0592b4563cc72add9e1c493eb45) feat: return mode `reboot` only if going maintenance -> running
* [`591e8b8`](https://github.com/siderolabs/talemu/commit/591e8b84f74d02b1abe2c95e23c17283cd25d6e2) feat: simulate etcd state
* [`041d4b8`](https://github.com/siderolabs/talemu/commit/041d4b8013eba4340df41247e71141ea43d0a63a) feat: implement upgrade, installation, versions and schematics
* [`48eb27c`](https://github.com/siderolabs/talemu/commit/48eb27c669a6ed9a066c00242ab965fff2f326f9) feat: support apply config, bootstrap and reset API
* [`a0bae44`](https://github.com/siderolabs/talemu/commit/a0bae44c0881c7f45d4e4b72ab5dad1b158d4fb4) fix: properly handle address status cleanup
* [`de5b825`](https://github.com/siderolabs/talemu/commit/de5b8254502f5e12d8561e4a867e511615f174bc) feat: implement basic version of Talos emulator
* [`da5b4a1`](https://github.com/siderolabs/talemu/commit/da5b4a15e695477b1d349235ca0769c61549f581) test: add GHA files
* [`7c024f9`](https://github.com/siderolabs/talemu/commit/7c024f966ddaac1c9d3c5116edcf526655b4de48) Initial commit
</p>
</details>

### Dependency Changes

This release has no dependency changes

