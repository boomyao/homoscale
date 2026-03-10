package homoscale

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tailscale.com/ipn/ipnstate"
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
	cfg, err := loadConfigWithFallback("homoscale.yaml", false)
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

func TestParseConfigFlagSupportsEngineConfigOverride(t *testing.T) {
	dir := t.TempDir()
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
