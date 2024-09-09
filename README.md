# Talemu is Talos Emulator

Runs multiple fake Talos nodes at the same time.
To be used in pair with Omni.

## Running Emulator Static Mode

Do `Copy Kernel Args` in the Omni UI.

Create `hack/compose/docker-compose.override.yml` and paste copied kernel args there.

Final YAML file can look like this:

```yaml
version: '3.8'
services:
  talemu:
    command: >-
      args:
        - --kernel-args="siderolink.api=grpc://192.168.88.219:8090?jointoken=w7uVuW3zbVKIYQuzEcyetAHeYMeo5q2L9RvkAVfCfSCD talos.events.sink=[fdae:41e4:649b:9303::1]:8090 talos.logging.kernel=tcp://[fdae:41e4:649b:9303::1]:8092"
        - --machines=100
```

Run `make docker-compose-up` command.

This will spawn one hundred fake Talos nodes.

## Infra Provider Mode

Run:

```bash
make infra-provider
```

Then run:

```bash
sudo -E _out/talemu-infra-provider-linux-amd64 --create-service-account --omni-api-endpoint=https://localhost:8099
```

Create a machine request using `omnictl`:

```yaml
metadata:
    namespace: infra-provider
    type: MachineRequests.omni.sidero.dev
    id: machine-1
    labels:
      omni.sidero.dev/infra-provider-id: talemu
spec:
  talosversion: v1.7.5
  schematicid: 376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba
```

The machine should be created by the emulator and appear in Omni.
