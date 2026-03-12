package homoscale

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func ensureTailnetAuth(ctx context.Context, cfg *Config) error {
	args, err := tailscaleUpArgs(cfg)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, cfg.Tailscale.CLIBinary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailscale up failed; if interactive login is needed, run `%s --socket %s up` manually: %w",
			cfg.Tailscale.CLIBinary, cfg.Tailscale.Socket, err)
	}
	return nil
}

func tailscaleUpArgs(cfg *Config) ([]string, error) {
	args := []string{"--socket", cfg.Tailscale.Socket, "up", "--accept-dns=false"}
	if cfg.Tailscale.AcceptDNS {
		args = []string{"--socket", cfg.Tailscale.Socket, "up", "--accept-dns=true"}
	}
	if cfg.Tailscale.AcceptRoutes {
		args = append(args, "--accept-routes")
	}
	routes, err := cfg.Tailscale.AdvertiseRoutePrefixes()
	if err != nil {
		return nil, err
	}
	if len(routes) > 0 {
		args = append(args, "--advertise-routes="+strings.Join(prefixesToStrings(routes), ","))
		if cfg.Tailscale.SNATSubnetRoutes != nil {
			args = append(args, fmt.Sprintf("--snat-subnet-routes=%t", *cfg.Tailscale.SNATSubnetRoutes))
		}
	}
	if cfg.Tailscale.Hostname != "" {
		args = append(args, "--hostname", cfg.Tailscale.Hostname)
	}
	if cfg.Tailscale.LoginServer != "" {
		args = append(args, "--login-server", cfg.Tailscale.LoginServer)
	}
	if authKey := os.Getenv(cfg.Tailscale.AuthKeyEnv); authKey != "" {
		args = append(args, "--auth-key", authKey)
	}
	args = append(args, cfg.Tailscale.ExtraUpFlags...)
	return args, nil
}

func tailscaleStatus(cfg *Config) (map[string]any, error) {
	cmd := exec.Command(cfg.Tailscale.CLIBinary, "--socket", cfg.Tailscale.Socket, "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}
