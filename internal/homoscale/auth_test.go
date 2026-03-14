package homoscale

import (
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

func TestLoadConfigAllowsMinimalAuthAndProxyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`tailscale:
  backend: embedded
  state_dir: ./var/tailscale
  socket: ./var/tailscale/tailscaled.sock
engine:
  controller_addr: 127.0.0.1:9090
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Tailscale.Backend != "embedded" {
		t.Fatalf("unexpected backend: %s", cfg.Tailscale.Backend)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:9090" {
		t.Fatalf("unexpected engine controller addr: %s", cfg.Engine.ControllerAddr)
	}
}

func TestAuthStatusFromIPN(t *testing.T) {
	status := authStatusFromIPN("embedded", "", &ipnstate.Status{
		BackendState: "Running",
		CurrentTailnet: &ipnstate.TailnetStatus{
			Name:            "example.com",
			MagicDNSSuffix:  "foo.ts.net",
			MagicDNSEnabled: true,
		},
	})

	if !status.LoggedIn {
		t.Fatal("expected logged_in=true")
	}
	if status.Tailnet != "example.com" {
		t.Fatalf("unexpected tailnet: %s", status.Tailnet)
	}
	if status.MagicDNSSuffix != "foo.ts.net" {
		t.Fatalf("unexpected MagicDNS suffix: %s", status.MagicDNSSuffix)
	}
}

func TestAuthStatusFromIPNIncludesTailnetAccessDomain(t *testing.T) {
	status := authStatusFromIPN("embedded", "", &ipnstate.Status{
		BackendState: "Running",
		CurrentTailnet: &ipnstate.TailnetStatus{
			Name:            "example.com",
			MagicDNSSuffix:  "foo.ts.net",
			MagicDNSEnabled: true,
		},
		Self: &ipnstate.PeerStatus{
			DNSName:      "phone.foo.ts.net.",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.10")},
		},
	})

	if status.SelfDNSName != "phone.foo.ts.net" {
		t.Fatalf("unexpected self dns name: %s", status.SelfDNSName)
	}
	want := []string{"phone.foo.ts.net", "phone"}
	if !reflect.DeepEqual(status.AccessDomains, want) {
		t.Fatalf("unexpected access domains: got %v want %v", status.AccessDomains, want)
	}
}

func TestMagicDNSHostsFromStatus(t *testing.T) {
	status := &ipnstate.Status{
		Self: &ipnstate.PeerStatus{
			DNSName:      "self.foo.ts.net.",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1"), netip.MustParseAddr("100.64.0.10")},
		},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NodePublic{}: {
				DNSName:      "peer.foo.ts.net.",
				TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.20")},
			},
		},
	}

	hosts := magicDNSHostsFromStatus(status)
	if got := hosts["self.foo.ts.net"]; got != "100.64.0.10" {
		t.Fatalf("unexpected self host mapping: %q", got)
	}
	if got := hosts["peer.foo.ts.net"]; got != "100.64.0.20" {
		t.Fatalf("unexpected peer host mapping: %q", got)
	}
}

func TestMagicDNSShortHostAliases(t *testing.T) {
	hosts := map[string]string{
		"phone.foo.ts.net": "100.64.0.10",
		"peer.foo.ts.net":  "100.64.0.20",
	}

	aliases := magicDNSShortHostAliases(hosts, "foo.ts.net")
	if got := aliases["phone"]; got != "100.64.0.10" {
		t.Fatalf("unexpected phone short mapping: %q", got)
	}
	if got := aliases["peer"]; got != "100.64.0.20" {
		t.Fatalf("unexpected peer short mapping: %q", got)
	}
}

func TestMagicDNSShortHostAliasesSkipsExistingShortHost(t *testing.T) {
	hosts := map[string]string{
		"phone.foo.ts.net": "100.64.0.10",
		"phone":            "100.64.0.20",
	}

	aliases := magicDNSShortHostAliases(hosts, "foo.ts.net")
	if len(aliases) != 0 {
		t.Fatalf("expected existing short host to skip aliases, got %v", aliases)
	}
}

func TestLoadConfigSupportsLegacyMihomoBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`mihomo:
  controller_addr: 127.0.0.1:19090
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:19090" {
		t.Fatalf("unexpected migrated controller addr: %s", cfg.Engine.ControllerAddr)
	}
}

func TestLoadConfigWithFallbackUsesDefaultsForImplicitMissingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := loadConfigWithFallback(defaultCLIConfigPath(), false)
	if err != nil {
		t.Fatalf("load config with fallback: %v", err)
	}
	if cfg.Tailscale.Backend != "embedded" {
		t.Fatalf("unexpected default backend: %s", cfg.Tailscale.Backend)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:9090" {
		t.Fatalf("unexpected default engine controller addr: %s", cfg.Engine.ControllerAddr)
	}
	if !strings.Contains(cfg.RuntimeDir, ".homoscale") {
		t.Fatalf("unexpected default runtime dir: %s", cfg.RuntimeDir)
	}
}

func TestDefaultCLIConfigPathUsesHomeRuntimeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := defaultCLIConfigPath()
	want := filepath.Join(home, ".homoscale", "homoscale.yaml")
	if got != want {
		t.Fatalf("unexpected default cli config path: got %q want %q", got, want)
	}
}

func TestLoadConfigWithFallbackKeepsExplicitMissingConfigError(t *testing.T) {
	_, err := loadConfigWithFallback("/definitely/missing/homoscale.yaml", true)
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not-exist error, got: %v", err)
	}
}

func TestExpandUserPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		t.Skip("home dir not available")
	}
	got := expandUserPath("~/engine/config.yaml")
	if !strings.HasPrefix(got, homeDir) {
		t.Fatalf("expected expanded home path, got: %s", got)
	}
}

func TestDefaultConfigReadsSubscriptionURLFromEnv(t *testing.T) {
	t.Setenv("HOMOSCALE_SUBSCRIPTION_URL", "https://example.com/subscription.yaml")

	cfg := DefaultConfig()
	if cfg.Engine.SubscriptionURL != "https://example.com/subscription.yaml" {
		t.Fatalf("unexpected subscription url: %s", cfg.Engine.SubscriptionURL)
	}
	if !strings.Contains(cfg.Engine.SubscriptionPath, ".homoscale") {
		t.Fatalf("unexpected subscription path: %s", cfg.Engine.SubscriptionPath)
	}
	if cfg.Engine.MixedPort != 7890 {
		t.Fatalf("unexpected mixed port: %d", cfg.Engine.MixedPort)
	}
	if cfg.Engine.Tun.Enable == nil || !*cfg.Engine.Tun.Enable {
		t.Fatalf("expected tun to be enabled by default")
	}
	if cfg.Engine.Tun.Stack != "system" {
		t.Fatalf("unexpected tun stack: %s", cfg.Engine.Tun.Stack)
	}
}

func TestDefaultConfigUsesSystemHostname(t *testing.T) {
	want, err := os.Hostname()
	if err != nil {
		t.Fatalf("read system hostname: %v", err)
	}
	want = strings.TrimSpace(want)

	cfg := DefaultConfig()
	if cfg.Tailscale.Hostname != want {
		t.Fatalf("unexpected default hostname: got %q want %q", cfg.Tailscale.Hostname, want)
	}
}

func TestLoadConfigPreservesExplicitHostname(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`tailscale:
  hostname: custom-node
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Tailscale.Hostname != "custom-node" {
		t.Fatalf("unexpected configured hostname: %s", cfg.Tailscale.Hostname)
	}
}

func TestLoadConfigParsesAdvertiseRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`tailscale:
  advertise_routes:
    - 192.168.50.0/24
    - fd7a:115c:a1e0::1
  embedded:
    forward_host_tcp: true
    forward_host: 127.0.0.1
    forward_host_tcp_ports:
      - 22
      - 3000
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, err := cfg.Tailscale.AdvertiseRoutePrefixes()
	if err != nil {
		t.Fatalf("parse advertise routes: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("192.168.50.0/24"),
		netip.MustParsePrefix("fd7a:115c:a1e0::1/128"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected advertise routes: got %v want %v", got, want)
	}
	if cfg.Tailscale.SNATSubnetRoutes == nil || !*cfg.Tailscale.SNATSubnetRoutes {
		t.Fatalf("expected snat_subnet_routes default to true")
	}
	if cfg.Tailscale.Embedded.ForwardHostTCP == nil || !*cfg.Tailscale.Embedded.ForwardHostTCP {
		t.Fatalf("expected forward_host_tcp to be enabled")
	}
	if cfg.Tailscale.Embedded.ForwardHost != "127.0.0.1" {
		t.Fatalf("unexpected forward_host: %s", cfg.Tailscale.Embedded.ForwardHost)
	}
	if !reflect.DeepEqual(cfg.Tailscale.Embedded.ForwardHostTCPPorts, []int{22, 3000}) {
		t.Fatalf("unexpected forward_host_tcp_ports: %v", cfg.Tailscale.Embedded.ForwardHostTCPPorts)
	}
}

func TestLoadConfigRejectsInvalidAdvertiseRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`tailscale:
  advertise_routes:
    - definitely-not-a-route
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected invalid advertise_routes error")
	}
	if !strings.Contains(err.Error(), "tailscale.advertise_routes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidEmbeddedForwardPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "homoscale.yaml")
	config := []byte(`tailscale:
  embedded:
    forward_host_tcp: true
    forward_host_tcp_ports:
      - 70000
`)
	if err := os.WriteFile(path, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected invalid forward_host_tcp_ports error")
	}
	if !strings.Contains(err.Error(), "forward_host_tcp_ports") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultConfigEnablesEmbeddedHostTCPForwarding(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Tailscale.Embedded.ForwardHostTCP == nil || !*cfg.Tailscale.Embedded.ForwardHostTCP {
		t.Fatal("expected embedded.forward_host_tcp to default to true")
	}
	if cfg.Tailscale.Embedded.ForwardHost != "127.0.0.1" {
		t.Fatalf("unexpected default forward_host: %s", cfg.Tailscale.Embedded.ForwardHost)
	}
}

func TestTailscaleUpArgsIncludesAdvertiseRoutes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tailscale.Socket = "/tmp/tailscaled.sock"
	cfg.Tailscale.Hostname = "host-a"
	cfg.Tailscale.AuthKeyEnv = "TS_AUTHKEY"
	cfg.Tailscale.AcceptRoutes = true
	cfg.Tailscale.AdvertiseRoutes = []string{"192.168.50.0/24", "10.10.10.7"}
	cfg.Tailscale.SNATSubnetRoutes = boolPtr(false)
	t.Setenv("TS_AUTHKEY", "tskey-auth-k8s")

	args, err := tailscaleUpArgs(cfg)
	if err != nil {
		t.Fatalf("build tailscale up args: %v", err)
	}

	got := strings.Join(args, " ")
	if !strings.Contains(got, "--accept-routes") {
		t.Fatalf("missing accept-routes flag: %v", args)
	}
	if !strings.Contains(got, "--advertise-routes=192.168.50.0/24,10.10.10.7/32") {
		t.Fatalf("missing advertise-routes flag: %v", args)
	}
	if !strings.Contains(got, "--snat-subnet-routes=false") {
		t.Fatalf("missing snat-subnet-routes flag: %v", args)
	}
	if !strings.Contains(got, "--auth-key tskey-auth-k8s") {
		t.Fatalf("missing auth-key flag: %v", args)
	}
}

func TestParseConfigFlagSupportsEngineConfigOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	configPath := filepath.Join(dir, "homoscale.yaml")
	engineConfigPath := filepath.Join(dir, "mihomo.yaml")
	engineConfig := []byte(`mixed-port: 17890
external-controller: 127.0.0.1:19090
secret: local-secret
tun:
  enable: false
`)
	if err := os.WriteFile(configPath, []byte("runtime_dir: "+dir+"\n"), 0o644); err != nil {
		t.Fatalf("write homoscale config: %v", err)
	}
	if err := os.WriteFile(engineConfigPath, engineConfig, 0o644); err != nil {
		t.Fatalf("write engine config: %v", err)
	}

	cfg, err := parseConfigFlag("start", []string{"-c", configPath, "--engine-config", engineConfigPath})
	if err != nil {
		t.Fatalf("parse config flag: %v", err)
	}
	if cfg.Engine.SourceConfigPath != importedEngineSourcePath(cfg) {
		t.Fatalf("unexpected engine source config path: %s", cfg.Engine.SourceConfigPath)
	}
	if cfg.Engine.ConfigPath != filepath.Join(dir, "engine", "config.yaml") {
		t.Fatalf("unexpected engine config path: %s", cfg.Engine.ConfigPath)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:19090" {
		t.Fatalf("unexpected controller addr: %s", cfg.Engine.ControllerAddr)
	}
	if cfg.Engine.Secret != "local-secret" {
		t.Fatalf("unexpected secret: %s", cfg.Engine.Secret)
	}
	if cfg.Engine.MixedPort != 17890 {
		t.Fatalf("unexpected mixed port: %d", cfg.Engine.MixedPort)
	}
	if cfg.Engine.Tun.Enable == nil || *cfg.Engine.Tun.Enable {
		t.Fatalf("expected tun enable to follow source config")
	}
	if cfg.Engine.WorkingDir != dir {
		t.Fatalf("unexpected working dir: %s", cfg.Engine.WorkingDir)
	}
	importedData, err := os.ReadFile(importedEngineSourcePath(cfg))
	if err != nil {
		t.Fatalf("read imported engine source: %v", err)
	}
	if string(importedData) != string(engineConfig) {
		t.Fatalf("unexpected imported engine source contents:\n%s", string(importedData))
	}
	restoredCfg, err := parseConfigFlag("status", []string{"-c", configPath})
	if err != nil {
		t.Fatalf("parse config flag without engine override: %v", err)
	}
	if restoredCfg.Engine.SourceConfigPath != importedEngineSourcePath(restoredCfg) {
		t.Fatalf("unexpected restored engine source config path: %s", restoredCfg.Engine.SourceConfigPath)
	}
	if restoredCfg.Engine.WorkingDir != dir {
		t.Fatalf("unexpected restored working dir: %s", restoredCfg.Engine.WorkingDir)
	}
	if restoredCfg.Engine.ControllerAddr != "127.0.0.1:19090" {
		t.Fatalf("unexpected restored controller addr: %s", restoredCfg.Engine.ControllerAddr)
	}
	if restoredCfg.Engine.Secret != "local-secret" {
		t.Fatalf("unexpected restored secret: %s", restoredCfg.Engine.Secret)
	}
	if restoredCfg.Engine.MixedPort != 17890 {
		t.Fatalf("unexpected restored mixed port: %d", restoredCfg.Engine.MixedPort)
	}
	if restoredCfg.Engine.Tun.Enable == nil || *restoredCfg.Engine.Tun.Enable {
		t.Fatalf("expected restored tun enable to follow imported source")
	}
}

func TestParseConfigFlagUsesDefaultHomeConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := defaultCLIConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir default config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("engine:\n  controller_addr: 127.0.0.1:19091\n"), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	cfg, err := parseConfigFlag("status", nil)
	if err != nil {
		t.Fatalf("parse config flag: %v", err)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:19091" {
		t.Fatalf("unexpected controller addr from default home config: %s", cfg.Engine.ControllerAddr)
	}
}

func TestParseConfigFlagPrefersExplicitConfigPathOverDefaultHomeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	defaultPath := defaultCLIConfigPath()
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		t.Fatalf("mkdir default config dir: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte("engine:\n  controller_addr: 127.0.0.1:19091\n"), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	customPath := filepath.Join(home, "custom.yaml")
	if err := os.WriteFile(customPath, []byte("engine:\n  controller_addr: 127.0.0.1:29091\n"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	cfg, err := parseConfigFlag("status", []string{"-c", customPath})
	if err != nil {
		t.Fatalf("parse config flag with explicit config: %v", err)
	}
	if cfg.Engine.ControllerAddr != "127.0.0.1:29091" {
		t.Fatalf("unexpected controller addr from explicit config: %s", cfg.Engine.ControllerAddr)
	}
}

func TestResolveCLIEngineConfigPathRejectsConflictingFlags(t *testing.T) {
	_, err := resolveCLIEngineConfigPath("/tmp/a.yaml", "/tmp/b.yaml")
	if err == nil {
		t.Fatal("expected conflicting engine config flags to fail")
	}
}

func TestReadAuthStatusPrefersEmbeddedRuntimeStatusFile(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Tailscale: TailscaleConfig{
			Backend:  "embedded",
			StateDir: dir,
		},
	}
	if err := os.MkdirAll(cfg.Tailscale.StateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	want := &AuthStatus{
		Backend:      "embedded",
		BackendState: "Running",
		LoggedIn:     true,
		Tailnet:      "example.com",
	}
	if err := writeRuntimeAuthStatus(cfg, authRuntimeSnapshot{Status: want}); err != nil {
		t.Fatalf("write runtime auth status: %v", err)
	}

	got, err := ReadAuthStatus(cfg)
	if err != nil {
		t.Fatalf("read auth status: %v", err)
	}
	if got.Tailnet != want.Tailnet || !got.LoggedIn {
		t.Fatalf("unexpected auth status: %+v", got)
	}
}

func TestReadRuntimeAuthStatusIgnoresStaleSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Tailscale: TailscaleConfig{
			Backend:  "embedded",
			StateDir: dir,
		},
	}
	if err := os.MkdirAll(cfg.Tailscale.StateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	payload, err := json.Marshal(authRuntimeSnapshot{
		PID: 0,
		Status: &AuthStatus{
			Backend:  "embedded",
			LoggedIn: true,
		},
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	path := tailscaleRuntimeStatusPath(cfg)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	if status, ok := readRuntimeAuthStatus(cfg); ok || status != nil {
		t.Fatalf("expected stale runtime status to be ignored, got: %+v", status)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale status file to be removed, err=%v", err)
	}
}
