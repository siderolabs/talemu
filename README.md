# Talemu is Talos Emulator

Runs multiple fake Talos nodes at the same time.
To be used in pair with Omni.

## Running emulator

Do `Copy Kernel Args` in the Omni UI, then paste the to `--kernel-args` flag.

Create `hack/compose/docker-compose.override.yml` file with the kernel args params.

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
