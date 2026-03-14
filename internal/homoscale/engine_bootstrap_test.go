package homoscale

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureEngineConfigWritesDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:     filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:     filepath.Join(dir, "engine"),
			MixedPort:      7890,
			ControllerAddr: "http://127.0.0.1:19090",
			Secret:         "secret-token",
		},
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}

	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "external-controller: 127.0.0.1:19090") {
		t.Fatalf("missing external-controller in config:\n%s", text)
	}
	if !strings.Contains(text, "mixed-port: 7890") {
		t.Fatalf("missing mixed-port in config:\n%s", text)
	}
	if !strings.Contains(text, "secret: secret-token") {
		t.Fatalf("missing secret in config:\n%s", text)
	}
	if !strings.Contains(text, "tun:") {
		t.Fatalf("missing tun section in config:\n%s", text)
	}
	if !strings.Contains(text, "enable: true") {
		t.Fatalf("missing tun enable in config:\n%s", text)
	}
	if !strings.Contains(text, "auto-route: true") {
		t.Fatalf("missing tun auto-route in config:\n%s", text)
	}
	if !strings.Contains(text, "- DIRECT") {
		t.Fatalf("missing DIRECT fallback in config:\n%s", text)
	}
	if !strings.Contains(text, "- MATCH,PROXY") {
		t.Fatalf("missing default rule in config:\n%s", text)
	}
	if !strings.Contains(text, "DOMAIN-SUFFIX,ts.net,DIRECT") {
		t.Fatalf("missing tailscale domain rule in config:\n%s", text)
	}
	if !strings.Contains(text, "IP-CIDR,100.64.0.0/10,DIRECT,no-resolve") {
		t.Fatalf("missing tailscale CIDR rule in config:\n%s", text)
	}
	if _, err := os.Stat(managedEngineConfigMarkerPath(cfg)); err != nil {
		t.Fatalf("missing managed config marker: %v", err)
	}
}

func TestEnsureEngineConfigWritesSubscriptionBackedConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:           filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:           filepath.Join(dir, "engine"),
			SubscriptionPath:     filepath.Join(dir, "engine", "providers", "subscription.yaml"),
			SubscriptionURL:      "https://example.com/subscription.yaml",
			SubscriptionInterval: 7200,
			MixedPort:            17890,
			ControllerAddr:       "127.0.0.1:9090",
		},
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}

	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "mixed-port: 17890") {
		t.Fatalf("missing custom mixed-port in config:\n%s", text)
	}
	if !strings.Contains(text, "proxy-providers:") {
		t.Fatalf("missing proxy provider in config:\n%s", text)
	}
	if !strings.Contains(text, "tun:") {
		t.Fatalf("missing tun section in config:\n%s", text)
	}
	if !strings.Contains(text, "url: https://example.com/subscription.yaml") {
		t.Fatalf("missing subscription url in config:\n%s", text)
	}
	if !strings.Contains(text, "path: "+cfg.Engine.SubscriptionPath) {
		t.Fatalf("missing subscription path in config:\n%s", text)
	}
	if !strings.Contains(text, "name: AUTO") {
		t.Fatalf("missing AUTO group in config:\n%s", text)
	}
	if !strings.Contains(text, "- AUTO") {
		t.Fatalf("missing AUTO option in config:\n%s", text)
	}
}

func TestEnsureEngineConfigSanitizesAndroidSubscriptionProvider(t *testing.T) {
	previousGOOS := runtimeGOOS
	runtimeGOOS = "android"
	t.Cleanup(func() {
		runtimeGOOS = previousGOOS
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: HK
    type: ss
    server: 1.1.1.1
    port: 8388
    cipher: aes-128-gcm
    password: test
rules:
  - PROCESS-NAME,com.example.app,Proxy
`))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:           filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:           filepath.Join(dir, "engine"),
			SubscriptionPath:     filepath.Join(dir, "engine", "providers", "subscription.yaml"),
			SubscriptionURL:      server.URL,
			SubscriptionInterval: 7200,
			MixedPort:            17890,
			ControllerAddr:       "127.0.0.1:9090",
		},
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}

	configData, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	configText := string(configData)
	if !strings.Contains(configText, "type: file") {
		t.Fatalf("missing file subscription provider in config:\n%s", configText)
	}
	if strings.Contains(configText, "url: "+server.URL) {
		t.Fatalf("unexpected remote subscription url in android config:\n%s", configText)
	}

	providerData, err := os.ReadFile(cfg.Engine.SubscriptionPath)
	if err != nil {
		t.Fatalf("read sanitized provider: %v", err)
	}
	providerText := string(providerData)
	if !strings.Contains(providerText, "name: HK") {
		t.Fatalf("missing proxy in sanitized provider:\n%s", providerText)
	}
	if strings.Contains(providerText, "PROCESS-NAME") {
		t.Fatalf("unexpected process rule in sanitized provider:\n%s", providerText)
	}
}

func TestEnsureEngineConfigWritesEmbeddedTailscaleProxy(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend:  "embedded",
			StateDir: filepath.Join(dir, "tailscale"),
		},
		Engine: EngineConfig{
			ConfigPath:     filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:     filepath.Join(dir, "engine"),
			MixedPort:      7890,
			ControllerAddr: "127.0.0.1:9090",
		},
	}
	if err := os.MkdirAll(cfg.Tailscale.StateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := writeRuntimeAuthStatus(cfg, authRuntimeSnapshot{
		PID:           os.Getpid(),
		Status:        &AuthStatus{Backend: "embedded", LoggedIn: true, Tailnet: "example.com", MagicDNSSuffix: "tail123.ts.net"},
		LoopbackAddr:  "127.0.0.1:16666",
		ProxyUser:     "tsnet",
		ProxyPass:     "secret-proxy",
		MagicDNSHosts: map[string]string{"peer.tail123.ts.net": "100.64.0.20"},
	}); err != nil {
		t.Fatalf("write runtime auth snapshot: %v", err)
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}

	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "name: TAILSCALE") {
		t.Fatalf("missing tailscale proxy in config:\n%s", text)
	}
	if !strings.Contains(text, "use-hosts: true") {
		t.Fatalf("missing dns.use-hosts in config:\n%s", text)
	}
	if !strings.Contains(text, "enable: true") {
		t.Fatalf("missing dns.enable in config:\n%s", text)
	}
	if !strings.Contains(text, "nameserver:") {
		t.Fatalf("missing dns.nameserver in config:\n%s", text)
	}
	if !strings.Contains(text, "peer.tail123.ts.net: 100.64.0.20") {
		t.Fatalf("missing MagicDNS hosts mapping in config:\n%s", text)
	}
	if !strings.Contains(text, "peer: 100.64.0.20") {
		t.Fatalf("missing short host mapping in config:\n%s", text)
	}
	if !strings.Contains(text, "server: 127.0.0.1") {
		t.Fatalf("missing tailscale proxy server in config:\n%s", text)
	}
	if !strings.Contains(text, "port: 16666") {
		t.Fatalf("missing tailscale proxy port in config:\n%s", text)
	}
	if !strings.Contains(text, "DOMAIN-SUFFIX,tail123.ts.net,TAILSCALE") {
		t.Fatalf("missing tailscale suffix rule in config:\n%s", text)
	}
	if !strings.Contains(text, "IP-CIDR,100.64.0.0/10,TAILSCALE,no-resolve") {
		t.Fatalf("missing tailscale CIDR rule in config:\n%s", text)
	}
}

func TestEnsureEngineConfigAllowsTunToBeDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:     filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:     filepath.Join(dir, "engine"),
			MixedPort:      7890,
			ControllerAddr: "127.0.0.1:9090",
			Tun: EngineTunConfig{
				Enable: boolPtr(false),
			},
		},
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created")
	}

	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "tun:") {
		t.Fatalf("expected tun section to be omitted when disabled:\n%s", text)
	}
}

func TestEnsureEngineConfigDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "engine", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := []byte("mode: global\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:     configPath,
			ControllerAddr: "127.0.0.1:9090",
		},
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if created {
		t.Fatal("expected existing config not to be replaced")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != string(original) {
		t.Fatalf("existing config was modified:\n%s", string(data))
	}
}

func TestEnsureEngineConfigRefreshesManagedConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "engine", "config.yaml")
	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend: "external",
		},
		Engine: EngineConfig{
			ConfigPath:     configPath,
			WorkingDir:     filepath.Join(dir, "engine"),
			ControllerAddr: "127.0.0.1:9090",
			MixedPort:      7890,
		},
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("mode: global\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(managedEngineConfigMarkerPath(cfg), []byte("managed\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected managed config to be refreshed")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "MATCH,PROXY") {
		t.Fatalf("expected managed config to be regenerated:\n%s", string(data))
	}
}

func TestEnsureEngineConfigWritesDerivedConfigForUserSource(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.yaml")
	source := []byte(`mixed-port: 7893
mode: rule
proxies:
  - name: HK
    type: ss
    server: 1.1.1.1
    port: 8388
    cipher: aes-128-gcm
    password: test
rules:
  - MATCH,HK
`)
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source config: %v", err)
	}

	cfg := &Config{
		RuntimeDir: dir,
		Tailscale: TailscaleConfig{
			Backend:  "embedded",
			StateDir: filepath.Join(dir, "tailscale"),
		},
		Engine: EngineConfig{
			SourceConfigPath: sourcePath,
			ConfigPath:       filepath.Join(dir, "engine", "config.yaml"),
			WorkingDir:       dir,
			ControllerAddr:   "127.0.0.1:9090",
			MixedPort:        17890,
			Secret:           "runtime-secret",
			Tun: EngineTunConfig{
				Enable: boolPtr(true),
			},
		},
	}
	if err := os.MkdirAll(cfg.Tailscale.StateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := writeRuntimeAuthStatus(cfg, authRuntimeSnapshot{
		Status: &AuthStatus{
			Backend:        "embedded",
			LoggedIn:       true,
			Tailnet:        "example.com",
			MagicDNSSuffix: "tail123.ts.net",
		},
		LoopbackAddr:  "127.0.0.1:16666",
		ProxyUser:     "tsnet",
		ProxyPass:     "secret-proxy",
		MagicDNSHosts: map[string]string{"peer.tail123.ts.net": "100.64.0.20"},
	}); err != nil {
		t.Fatalf("write runtime auth snapshot: %v", err)
	}

	created, err := ensureEngineConfig(cfg)
	if err != nil {
		t.Fatalf("ensure engine config: %v", err)
	}
	if !created {
		t.Fatal("expected derived config to be created")
	}

	data, err := os.ReadFile(cfg.Engine.ConfigPath)
	if err != nil {
		t.Fatalf("read derived config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "secret: runtime-secret") {
		t.Fatalf("missing runtime secret in derived config:\n%s", text)
	}
	if !strings.Contains(text, "mixed-port: 17890") {
		t.Fatalf("missing runtime mixed-port override in derived config:\n%s", text)
	}
	if !strings.Contains(text, "name: TAILSCALE") {
		t.Fatalf("missing tailscale proxy in derived config:\n%s", text)
	}
	if !strings.Contains(text, "use-hosts: true") {
		t.Fatalf("missing dns.use-hosts in derived config:\n%s", text)
	}
	if !strings.Contains(text, "enable: true") {
		t.Fatalf("missing dns.enable in derived config:\n%s", text)
	}
	if !strings.Contains(text, "peer.tail123.ts.net: 100.64.0.20") {
		t.Fatalf("missing MagicDNS hosts mapping in derived config:\n%s", text)
	}
	if !strings.Contains(text, "peer: 100.64.0.20") {
		t.Fatalf("missing short host mapping in derived config:\n%s", text)
	}
	if !strings.Contains(text, "DOMAIN-SUFFIX,tail123.ts.net,TAILSCALE") {
		t.Fatalf("missing tailscale domain rule in derived config:\n%s", text)
	}
	if !strings.Contains(text, "IP-CIDR,100.64.0.0/10,TAILSCALE,no-resolve") {
		t.Fatalf("missing tailscale cidr rule in derived config:\n%s", text)
	}
	if !strings.Contains(text, "- MATCH,HK") {
		t.Fatalf("missing original rule in derived config:\n%s", text)
	}
	if !strings.Contains(text, "tun:") {
		t.Fatalf("missing tun in derived config:\n%s", text)
	}
}
