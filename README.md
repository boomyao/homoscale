# homoscale

`homoscale` is a small CLI for two local control tasks:

1. authenticate a device into Tailscale
2. run and control homoscale's local proxy engine

## Commands

- `homoscale`
- `homoscale start`
- `homoscale stop`
- `homoscale auth`
- `homoscale auth login`
- `homoscale auth logout`
- `homoscale status`
- `homoscale mode <rule|global|direct>`
- `homoscale current`
- `homoscale rules`
- `homoscale select [group] [proxy]`
- `homoscale version`

All engine-related commands also accept `--engine-config <path>` or `-f <path>` to import a local Mihomo config.
When you use `--engine-config`, `homoscale` keeps the source file unchanged, copies its proxy-related settings into `~/.homoscale/engine/source.yaml`, and regenerates the one real runtime config at `~/.homoscale/engine/config.yaml`.
After that, later commands can omit `-f` and homoscale will keep using the imported source snapshot automatically.

## Example config

Embedded Tailscale:

```yaml
tailscale:
  backend: embedded
  auth_key_env: TS_AUTHKEY
  advertise_routes:
    - 192.168.0.0/24

engine:
  binary: mihomo
  config_path: ~/.homoscale/engine/config.yaml
  controller_addr: 127.0.0.1:9090
  mixed_port: 7890
  secret: ""
  subscription_url: https://example.com/subscription.yaml
  tun:
    enable: true
    stack: system
    auto_route: true
    auto_detect_interface: true
```

External Tailscale:

```yaml
tailscale:
  backend: external
  cli_binary: tailscale
  auth_key_env: TS_AUTHKEY
  advertise_routes:
    - 192.168.0.0/24

engine:
  binary: mihomo
  config_path: ~/.homoscale/engine/config.yaml
  controller_addr: 127.0.0.1:9090
  mixed_port: 7890
  secret: ""
  subscription_url: https://example.com/subscription.yaml
  tun:
    enable: true
    stack: system
    auto_route: true
    auto_detect_interface: true
```

## Usage

Start homoscale and bring up both the local tailnet runtime and proxy engine:

```bash
go run ./cmd/homoscale
go run ./cmd/homoscale start -c examples/homoscale.yaml
go run ./cmd/homoscale -d
sudo ./homoscale -d -f ~/.config/mihomo/config.yaml
go run ./cmd/homoscale --engine-config ~/.config/mihomo/config.yaml
HOMOSCALE_SUBSCRIPTION_URL=https://example.com/subscription.yaml go run ./cmd/homoscale
```

Use `-d` or `--daemon` to run homoscale in background. Logs are written to `~/.homoscale/logs/homoscale.log`.
Android does not support homoscale's current self-daemonizing mode, so run it in the foreground there.

If `engine.config_path` does not exist, `homoscale` generates a local engine config automatically on first start.

- Without a subscription URL, the generated config boots with a local `DIRECT` fallback so the engine is still manageable.
- With `engine.subscription_url` or `HOMOSCALE_SUBSCRIPTION_URL`, the generated config creates a `subscription` provider plus `AUTO` and `PROXY` groups automatically.
- Generated configs now enable Mihomo `tun` by default so `homoscale` can take over system traffic. Set `engine.tun.enable: false` if you want to disable that.

Stop the managed proxy engine. If `homoscale start` is running in another terminal, it will exit after the engine stops:

```bash
go run ./cmd/homoscale stop -c examples/homoscale.yaml
```

Log into Tailscale:

```bash
go run ./cmd/homoscale auth login -c examples/homoscale.yaml
go run ./cmd/homoscale login
```

Check overall status:

```bash
go run ./cmd/homoscale status -c examples/homoscale.yaml
go run ./cmd/homoscale status
```

`status` now returns only on/off state for `overall`, `auth`, and `engine`.

Show only current mode and active selector choices:

```bash
go run ./cmd/homoscale current -c examples/homoscale.yaml
```

Show selector groups with candidate nodes:

```bash
go run ./cmd/homoscale rules -c examples/homoscale.yaml
```

Switch homoscale mode:

```bash
go run ./cmd/homoscale mode rule -c examples/homoscale.yaml
go run ./cmd/homoscale mode global -c examples/homoscale.yaml
go run ./cmd/homoscale mode direct -c examples/homoscale.yaml
```

Select a proxy inside a selector group:

```bash
go run ./cmd/homoscale select PROXY HK -c examples/homoscale.yaml
```

Discover available groups or candidates first:

```bash
go run ./cmd/homoscale select -c examples/homoscale.yaml
go run ./cmd/homoscale select PROXY -c examples/homoscale.yaml
```

## Android

Build an Android `arm64` binary:

```bash
GOOS=android GOARCH=arm64 go build ./cmd/homoscale
make build-android
make dist-android
```

Build an installable debug APK:

```bash
cd android
./gradlew assembleDebug
```

The generated APK is written to `android/app/build/outputs/apk/debug/app-debug.apk`.
During the Android build, Gradle downloads the pinned latest stable Mihomo Android `arm64-v8` asset from GitHub and bundles it into the APK together with the upstream `LICENSE`.

Run it in a foreground shell session on Android:

```bash
./homoscale start -c ~/.homoscale/homoscale.yaml
./homoscale status -c ~/.homoscale/homoscale.yaml
```

Notes for Android:

- `-d` / `--daemon` is rejected explicitly.
- The Android app embeds `homoscale` as a Go shared library and exposes start/stop/status/version from a foreground service.
- The Android build currently pins Mihomo `v1.19.21` and bundles `mihomo-android-arm64-v8-v1.19.21.gz` from `MetaCubeX/mihomo`.
- The current APK is `arm64-v8a` only.
- The Android service is now a real `VpnService`: it establishes the TUN device itself, excludes the homoscale app package from the VPN, and passes the inherited TUN file descriptor to Mihomo.
- The embedded Tailscale backend is the safer default because external mode still depends on an already-running `tailscaled` socket.
- The app installs the bundled Mihomo binary into its private files directory on first use.
- The generated Android config enables `engine.tun` with Android-specific `file_descriptor`, addresses, and DNS hijack settings.

Import a local Mihomo config once, then reuse it:

```bash
go run ./cmd/homoscale --engine-config ~/.config/mihomo/config.yaml
go run ./cmd/homoscale status
go run ./cmd/homoscale rules
```

If you use the external Tailscale backend, start `tailscaled` yourself first and point `tailscale.socket` at the live daemon socket.

If `tailscale.hostname` is omitted, `homoscale` uses the local system hostname automatically. Set it explicitly only when you want a fixed custom node name.

If you want other tailnet devices to reach resources behind the homoscale host, set `tailscale.advertise_routes` to the host-side CIDRs you want this node to route. `homoscale` applies these routes for both embedded and external Tailscale backends. `tailscale.snat_subnet_routes` defaults to `true`, which matches the default Tailscale subnet-router behavior.

If you use the embedded Tailscale backend, host TCP forwarding is enabled by default. That means `ssh user@<node-name>` or `<node-name>:<port>` reaches services running on the host machine itself by forwarding matching ports to `tailscale.embedded.forward_host` (default `127.0.0.1`). If `tailscale.embedded.forward_host_tcp_ports` is empty, every TCP port is forwarded.

When Tailscale MagicDNS is enabled, `homoscale` publishes both the full MagicDNS hostname and a short hostname alias inside Mihomo DNS. For example, if the node is `phone.tail123.ts.net`, you can reach it as either `phone.tail123.ts.net` or `phone` while homoscale is handling DNS.

If you want to limit or disable that behavior, configure the embedded block explicitly:

```yaml
tailscale:
  embedded:
    forward_host_tcp: false
```

Or restrict it to selected host ports:

```yaml
tailscale:
  embedded:
    forward_host_tcp_ports:
      - 22
      - 3000
```

If your tailnet requires route approval, approve the advertised routes in the Tailscale admin console before expecting other nodes to use them.

If you do not pass `-c`, `homoscale` looks for `~/.homoscale/homoscale.yaml`. If that file is absent, it falls back to built-in defaults:

- `runtime_dir: ~/.homoscale`
- `tailscale.backend: embedded`
- `tailscale.state_dir: ~/.homoscale/tailscale`
- `tailscale.socket: ~/.homoscale/tailscale/tailscaled.sock`
- `engine.binary: mihomo`
- `engine.config_path: ~/.homoscale/engine/config.yaml`
- `engine.controller_addr: 127.0.0.1:9090`
- `engine.mixed_port: 7890`
- `engine.subscription_url: $HOMOSCALE_SUBSCRIPTION_URL`
- `engine.subscription_path: ~/.homoscale/engine/providers/subscription.yaml`
- `engine.tun.enable: true`
