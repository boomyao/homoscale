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
  hostname: homoscale
  auth_key_env: TS_AUTHKEY

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
  hostname: homoscale
  auth_key_env: TS_AUTHKEY

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

Import a local Mihomo config once, then reuse it:

```bash
go run ./cmd/homoscale --engine-config ~/.config/mihomo/config.yaml
go run ./cmd/homoscale status
go run ./cmd/homoscale rules
```

If you use the external Tailscale backend, start `tailscaled` yourself first and point `tailscale.socket` at the live daemon socket.

If `homoscale.yaml` is absent and you did not pass `-c`, `homoscale` falls back to built-in defaults:

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
