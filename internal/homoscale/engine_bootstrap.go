package homoscale

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func ensureEngineConfig(cfg *Config) (bool, error) {
	cfg.Engine.Tun.applyDefaults()
	if strings.TrimSpace(cfg.Engine.SourceConfigPath) != "" {
		return writeDerivedEngineConfig(cfg)
	}

	if _, err := os.Stat(cfg.Engine.ConfigPath); err == nil {
		if !isManagedEngineConfig(cfg) {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat engine config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Engine.ConfigPath), 0o755); err != nil {
		return false, fmt.Errorf("create engine config dir: %w", err)
	}

	payload := map[string]any{
		"mixed-port":          cfg.Engine.MixedPort,
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "info",
		"external-controller": controllerListenAddr(cfg),
		"secret":              cfg.Engine.Secret,
	}
	if cfg.Engine.Tun.Enable != nil && *cfg.Engine.Tun.Enable {
		payload["tun"] = map[string]any{
			"enable":                true,
			"stack":                 cfg.Engine.Tun.Stack,
			"auto-route":            boolValue(cfg.Engine.Tun.AutoRoute),
			"auto-redirect":         boolValue(cfg.Engine.Tun.AutoRedirect),
			"auto-detect-interface": boolValue(cfg.Engine.Tun.AutoDetectInterface),
			"strict-route":          boolValue(cfg.Engine.Tun.StrictRoute),
			"dns-hijack":            append([]string(nil), cfg.Engine.Tun.DNSHijack...),
		}
	}
	tailscaleProxy, tailscaleRules, err := buildTailscaleRouting(cfg)
	if err != nil {
		return false, err
	}
	if tailscaleProxy != nil {
		payload["proxies"] = []map[string]any{tailscaleProxy}
	}
	if strings.TrimSpace(cfg.Engine.SubscriptionURL) == "" {
		payload["proxy-groups"] = []map[string]any{
			{
				"name": "PROXY",
				"type": "select",
				"proxies": []string{
					"DIRECT",
				},
			},
		}
	} else {
		payload["proxy-providers"] = map[string]any{
			"subscription": map[string]any{
				"type":     "http",
				"url":      cfg.Engine.SubscriptionURL,
				"path":     cfg.Engine.SubscriptionPath,
				"interval": cfg.Engine.SubscriptionInterval,
				"health-check": map[string]any{
					"enable":   true,
					"url":      "https://cp.cloudflare.com/generate_204",
					"interval": 600,
				},
			},
		}
		payload["proxy-groups"] = []map[string]any{
			{
				"name": "AUTO",
				"type": "url-test",
				"use": []string{
					"subscription",
				},
				"url":      "https://cp.cloudflare.com/generate_204",
				"interval": 300,
			},
			{
				"name": "PROXY",
				"type": "select",
				"use": []string{
					"subscription",
				},
				"proxies": []string{
					"AUTO",
					"DIRECT",
				},
			},
		}
	}
	rules := append([]string(nil), tailscaleRules...)
	rules = append(rules, "MATCH,PROXY")
	payload["rules"] = rules

	data, err := yaml.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal default engine config: %w", err)
	}
	if err := os.WriteFile(cfg.Engine.ConfigPath, data, 0o644); err != nil {
		return false, fmt.Errorf("write default engine config: %w", err)
	}
	if err := os.WriteFile(managedEngineConfigMarkerPath(cfg), []byte("managed\n"), 0o644); err != nil {
		return false, fmt.Errorf("write engine config marker: %w", err)
	}
	return true, nil
}

func writeDerivedEngineConfig(cfg *Config) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Engine.ConfigPath), 0o755); err != nil {
		return false, fmt.Errorf("create derived engine config dir: %w", err)
	}
	data, err := os.ReadFile(cfg.Engine.SourceConfigPath)
	if err != nil {
		return false, fmt.Errorf("read source engine config %s: %w", cfg.Engine.SourceConfigPath, err)
	}

	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return false, fmt.Errorf("parse source engine config %s: %w", cfg.Engine.SourceConfigPath, err)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	payload["external-controller"] = controllerListenAddr(cfg)
	payload["secret"] = cfg.Engine.Secret
	setTopLevelInt(payload, "mixed-port", cfg.Engine.MixedPort)

	if cfg.Engine.Tun.Enable != nil && *cfg.Engine.Tun.Enable {
		payload["tun"] = map[string]any{
			"enable":                true,
			"stack":                 cfg.Engine.Tun.Stack,
			"auto-route":            boolValue(cfg.Engine.Tun.AutoRoute),
			"auto-redirect":         boolValue(cfg.Engine.Tun.AutoRedirect),
			"auto-detect-interface": boolValue(cfg.Engine.Tun.AutoDetectInterface),
			"strict-route":          boolValue(cfg.Engine.Tun.StrictRoute),
			"dns-hijack":            append([]string(nil), cfg.Engine.Tun.DNSHijack...),
		}
	}

	tailscaleProxy, tailscaleRules, err := buildTailscaleRouting(cfg)
	if err != nil {
		return false, err
	}
	injectTailscaleProxy(payload, tailscaleProxy)
	injectTailscaleRules(payload, tailscaleRules)

	out, err := yaml.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal derived engine config: %w", err)
	}
	if err := os.WriteFile(cfg.Engine.ConfigPath, out, 0o644); err != nil {
		return false, fmt.Errorf("write derived engine config %s: %w", cfg.Engine.ConfigPath, err)
	}
	return true, nil
}

func buildTailscaleRouting(cfg *Config) (map[string]any, []string, error) {
	const tailnetCGNAT = "IP-CIDR,100.64.0.0/10,%s,no-resolve"

	target := "DIRECT"
	var proxy map[string]any

	switch cfg.Tailscale.Backend {
	case "embedded":
		snapshot, ok := readRuntimeAuthSnapshot(cfg)
		if !ok || strings.TrimSpace(snapshot.LoopbackAddr) == "" {
			return nil, nil, fmt.Errorf("embedded tailscale runtime is not ready")
		}
		host, port, err := splitHostPort(snapshot.LoopbackAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("parse tailscale loopback addr %q: %w", snapshot.LoopbackAddr, err)
		}
		target = "TAILSCALE"
		proxy = map[string]any{
			"name":     target,
			"type":     "socks5",
			"server":   host,
			"port":     port,
			"username": snapshot.ProxyUser,
			"password": snapshot.ProxyPass,
			"udp":      true,
		}
	case "external":
	default:
		return nil, nil, fmt.Errorf("unsupported tailscale.backend %q", cfg.Tailscale.Backend)
	}

	suffix := tailscaleMagicDNSSuffix(cfg)
	rules := []string{
		fmt.Sprintf(tailnetCGNAT, target),
	}
	if suffix != "" {
		rules = append([]string{fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", suffix, target)}, rules...)
	} else {
		rules = append([]string{fmt.Sprintf("DOMAIN-SUFFIX,ts.net,%s", target)}, rules...)
	}
	return proxy, rules, nil
}

func tailscaleMagicDNSSuffix(cfg *Config) string {
	if snapshot, ok := readRuntimeAuthSnapshot(cfg); ok && snapshot.Status != nil {
		return strings.TrimSpace(snapshot.Status.MagicDNSSuffix)
	}
	if status, err := ReadAuthStatus(cfg); err == nil && status != nil {
		return strings.TrimSpace(status.MagicDNSSuffix)
	}
	return ""
}

func controllerListenAddr(cfg *Config) string {
	addr := strings.TrimSpace(cfg.Engine.ControllerAddr)
	if addr == "" {
		return defaultEngineControllerAddr
	}
	if !strings.Contains(addr, "://") {
		return addr
	}
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Host == "" {
		return addr
	}
	return parsed.Host
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func managedEngineConfigMarkerPath(cfg *Config) string {
	return cfg.Engine.ConfigPath + ".homoscale-managed"
}

func isManagedEngineConfig(cfg *Config) bool {
	_, err := os.Stat(managedEngineConfigMarkerPath(cfg))
	return err == nil
}

func splitHostPort(addr string) (string, int, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func injectTailscaleProxy(payload map[string]any, proxy map[string]any) {
	if proxy == nil {
		return
	}
	items := sliceOfMaps(payload["proxies"])
	filtered := make([]map[string]any, 0, len(items)+1)
	for _, item := range items {
		if strings.TrimSpace(stringValue(item["name"])) == "TAILSCALE" {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, proxy)
	payload["proxies"] = filtered
}

func injectTailscaleRules(payload map[string]any, rules []string) {
	if len(rules) == 0 {
		return
	}
	existing := stringSlice(payload["rules"])
	filtered := make([]string, 0, len(existing))
	seen := map[string]struct{}{}
	for _, rule := range rules {
		seen[rule] = struct{}{}
	}
	for _, rule := range existing {
		if _, ok := seen[rule]; ok {
			continue
		}
		filtered = append(filtered, rule)
	}
	payload["rules"] = append(append([]string(nil), rules...), filtered...)
}

func stringSlice(value any) []string {
	switch items := value.(type) {
	case []string:
		return append([]string(nil), items...)
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			text := strings.TrimSpace(stringValue(item))
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func sliceOfMaps(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), items...)
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func setTopLevelInt(payload map[string]any, key string, value int) {
	if value == 0 {
		return
	}
	payload[key] = value
}
