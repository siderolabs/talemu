# Talemu is Talos Emulator

Runs multiple fake Talos nodes at the same time.
To be used in pair with Omni.

## Running emulator

Build the executable:

```bash
make talemu-linux-amd64
```

Do `Copy Kernel Args` in the Omni UI, then paste the to `--kernel-args` flag.
The final command can look like this:

```bash
_out/talemu-linux-amd64 --kernel-args="siderolink.api=grpc://192.168.88.219:8090?jointoken=w7UVuW3zbVKIYQuzEcyetAHeYMeo5q2L9RvkAVfCfSVD talos.events.sink=[fdae:41e4:649b:9303::1]:8090 talos.logging.kernel=tcp://[fdae:41e4:649b:9303::1]:8092" \
               --machines=100
```

This will spawn one hundred fake Talos nodes.
