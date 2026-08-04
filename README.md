# Talemu is Talos Emulator

Runs multiple fake Talos nodes at the same time.
To be used in pair with Omni.

## Running Emulator Static Mode

Static mode emulates machines that join Omni by themselves, as if they had booted some boot media.
So tell it which media they booted.

### From a factory schematic (the normal case)

Talos boot media is built by an image factory, and its schematic is the media's identity: it carries both the extra kernel args the machine boots with, including how to reach Omni, and the extensions the machine has.
Download an image for your Omni instance and pass the schematic ID of it:

```yaml
version: '3.8'
services:
  talemu:
    command: >-
      args:
        - --schematic-id=e2e8b4d5c0a...
        - --machines=100
```

The schematic is read from the image factory at startup, and startup fails if it does not hold that schematic, since machines booted from media nobody has would never come up either.
Use `--image-factory-base-url` for a factory other than the public one.

### Without a factory

Media built locally, with `imager` rather than a factory, has no schematic.
Give the kernel args and the extensions directly instead:

```yaml
version: '3.8'
services:
  talemu:
    command: >-
      args:
        - --kernel-args="siderolink.api=grpc://192.168.88.219:8090?jointoken=w7uVuW3zbVKIYQuzEcyetAHeYMeo5q2L9RvkAVfCfSCD talos.events.sink=[fdae:41e4:649b:9303::1]:8090 talos.logging.kernel=tcp://[fdae:41e4:649b:9303::1]:8092"
        - --extensions=siderolabs/hello-world-service
        - --machines=100
```

Those machines report no schematic, exactly as a locally built image does.
`Copy Kernel Args` in the Omni UI gives a usable value for `--kernel-args`.

`--schematic-id` and `--kernel-args` are mutually exclusive, and so are
`--schematic-id` and `--extensions`, because a schematic already carries both.
Neither is required: SideroLink is optional in Talos, and machines given no boot media info at all simply run standalone and join nothing.

Run `make docker-compose-up` command.

This will spawn one hundred fake Talos nodes.

## Infra Provider Mode

### Running as executable

Run:

```bash
make infra-provider
```

Then run:

```bash
sudo -E _out/talemu-infra-provider-linux-amd64 --create-service-account --omni-api-endpoint=https://localhost:8099
```

### Running in docker

Create `hack/compose/docker-compose-provider.override.yml` and add the following:

```yml
services:
  talemu-infra-provider:
    command: >-
      args:
        --omni-api-endpoint=https://localhost:8099
        --create-service-account
```

Run `make docker-compose-provider-up` command.

### Creating requests

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
