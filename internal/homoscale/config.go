package homoscale

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultTailscaleCLIBinary   = "tailscale"
	defaultTailscaleBackend     = "embedded"
	defaultEngineControllerAddr = "127.0.0.1:9090"
)

type Config struct {
	RuntimeDir   string          `yaml:"runtime_dir"`
	Tailscale    TailscaleConfig `yaml:"tailscale"`
	Engine       EngineConfig    `yaml:"engine"`
	LegacyMihomo EngineConfig    `yaml:"mihomo"`
}

type TailscaleConfig struct {
	Backend      string                  `yaml:"backend"`
	CLIBinary    string                  `yaml:"cli_binary"`
	StateDir     string                  `yaml:"state_dir"`
	Socket       string                  `yaml:"socket"`
	Hostname     string                  `yaml:"hostname"`
	LoginServer  string                  `yaml:"login_server"`
	AuthKeyEnv   string                  `yaml:"auth_key_env"`
	AcceptDNS    bool                    `yaml:"accept_dns"`
	AcceptRoutes bool                    `yaml:"accept_routes"`
	ExtraUpFlags []string                `yaml:"extra_up_flags"`
	Embedded     EmbeddedTailscaleConfig `yaml:"embedded"`
}

type EmbeddedTailscaleConfig struct {
	Ephemeral     bool     `yaml:"ephemeral"`
	VerboseLogs   bool     `yaml:"verbose_logs"`
	AdvertiseTags []string `yaml:"advertise_tags"`
}

type EngineConfig struct {
	Binary               string          `yaml:"binary"`
	SourceConfigPath     string          `yaml:"-"`
	ConfigPath           string          `yaml:"config_path"`
	WorkingDir           string          `yaml:"working_dir"`
	ControllerAddr       string          `yaml:"controller_addr"`
	Secret               string          `yaml:"secret"`
	RunArgs              []string        `yaml:"run_args"`
	StateFile            string          `yaml:"state_file"`
	MixedPort            int             `yaml:"mixed_port"`
	SubscriptionURL      string          `yaml:"subscription_url"`
	SubscriptionPath     string          `yaml:"subscription_path"`
	SubscriptionInterval int             `yaml:"subscription_interval"`
	Tun                  EngineTunConfig `yaml:"tun"`
}

type EngineTunConfig struct {
	Enable              *bool    `yaml:"enable"`
	Stack               string   `yaml:"stack"`
	AutoRoute           *bool    `yaml:"auto_route"`
	AutoRedirect        *bool    `yaml:"auto_redirect"`
	AutoDetectInterface *bool    `yaml:"auto_detect_interface"`
	StrictRoute         *bool    `yaml:"strict_route"`
	DNSHijack           []string `yaml:"dns_hijack"`
}

type localEngineConfigFile struct {
	ExternalController string `yaml:"external-controller"`
	Secret             string `yaml:"secret"`
	MixedPort          int    `yaml:"mixed-port"`
	Tun                struct {
		Enable *bool `yaml:"enable"`
	} `yaml:"tun"`
}

type importedEngineSourceState struct {
	SourcePath string `yaml:"source_path"`
	WorkingDir string `yaml:"working_dir"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	baseDir := filepath.Dir(path)
	cfg.mergeLegacyFields()
	cfg.applyDefaults(baseDir)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func DefaultConfig() *Config {
	cfg := &Config{}
	cfg.mergeLegacyFields()
	cfg.applyDefaults(".")
	return cfg
}

func (c *Config) mergeLegacyFields() {
	if !c.Engine.isSet() && c.LegacyMihomo.isSet() {
		c.Engine = c.LegacyMihomo
	}
}

func (c *Config) applyDefaults(baseDir string) {
	if c.RuntimeDir == "" {
		c.RuntimeDir = defaultRuntimeDir()
	}
	c.RuntimeDir = resolvePath(baseDir, c.RuntimeDir)

	if c.Tailscale.Backend == "" {
		c.Tailscale.Backend = defaultTailscaleBackend
	}
	if c.Tailscale.CLIBinary == "" {
		c.Tailscale.CLIBinary = defaultTailscaleCLIBinary
	}
	c.Tailscale.CLIBinary = resolveCommandPath(baseDir, c.Tailscale.CLIBinary)
	if c.Tailscale.StateDir == "" {
		c.Tailscale.StateDir = filepath.Join(c.RuntimeDir, "tailscale")
	}
	c.Tailscale.StateDir = resolvePath(baseDir, c.Tailscale.StateDir)
	if c.Tailscale.Socket == "" {
		c.Tailscale.Socket = filepath.Join(c.Tailscale.StateDir, "tailscaled.sock")
	}
	c.Tailscale.Socket = resolvePath(baseDir, c.Tailscale.Socket)
	if c.Tailscale.AuthKeyEnv == "" {
		c.Tailscale.AuthKeyEnv = "TS_AUTHKEY"
	}

	c.Engine.applyDefaults(baseDir, c.RuntimeDir)
}

func defaultRuntimeDir() string {
	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		return filepath.Join(homeDir, ".homoscale")
	}
	return "./.homoscale"
}

func (c *Config) validate() error {
	switch c.Tailscale.Backend {
	case "embedded", "external":
	default:
		return fmt.Errorf("unsupported tailscale.backend %q", c.Tailscale.Backend)
	}
	if c.Tailscale.StateDir == "" {
		return errors.New("tailscale.state_dir is required")
	}
	if c.Tailscale.Socket == "" {
		return errors.New("tailscale.socket is required")
	}
	return nil
}

func (c *Config) ValidateProxy() error {
	if strings.TrimSpace(c.Engine.ControllerAddr) == "" {
		return errors.New("engine.controller_addr is required")
	}
	return nil
}

func (e *EngineConfig) applyDefaults(baseDir, runtimeDir string) {
	if e.Binary == "" {
		e.Binary = "mihomo"
	}
	e.Binary = resolveCommandPath(baseDir, e.Binary)
	if e.ConfigPath == "" {
		e.ConfigPath = filepath.Join(runtimeDir, "engine", "config.yaml")
	}
	e.ConfigPath = resolvePath(baseDir, e.ConfigPath)
	if e.WorkingDir == "" {
		e.WorkingDir = filepath.Join(runtimeDir, "engine")
	}
	e.WorkingDir = resolvePath(baseDir, e.WorkingDir)
	if e.ControllerAddr == "" {
		e.ControllerAddr = defaultEngineControllerAddr
	}
	if e.StateFile == "" {
		e.StateFile = filepath.Join(e.WorkingDir, "engine.pid.json")
	}
	e.StateFile = resolvePath(baseDir, e.StateFile)
	if e.MixedPort == 0 {
		e.MixedPort = 7890
	}
	if e.SubscriptionURL == "" {
		e.SubscriptionURL = strings.TrimSpace(os.Getenv("HOMOSCALE_SUBSCRIPTION_URL"))
	}
	if e.SubscriptionPath == "" {
		e.SubscriptionPath = filepath.Join(e.WorkingDir, "providers", "subscription.yaml")
	}
	e.SubscriptionPath = resolvePath(baseDir, e.SubscriptionPath)
	if e.SubscriptionInterval == 0 {
		e.SubscriptionInterval = 3600
	}
	e.Tun.applyDefaults()
}

func (e EngineConfig) isSet() bool {
	return strings.TrimSpace(e.Binary) != "" ||
		strings.TrimSpace(e.ConfigPath) != "" ||
		strings.TrimSpace(e.WorkingDir) != "" ||
		strings.TrimSpace(e.ControllerAddr) != "" ||
		strings.TrimSpace(e.Secret) != "" ||
		strings.TrimSpace(e.SubscriptionURL) != "" ||
		strings.TrimSpace(e.SubscriptionPath) != "" ||
		strings.TrimSpace(e.StateFile) != "" ||
		e.MixedPort != 0 ||
		e.SubscriptionInterval != 0 ||
		e.Tun.isSet() ||
		len(e.RunArgs) > 0
}

func (c *Config) EnsureRuntimeDirs() error {
	dirs := []string{
		c.RuntimeDir,
		c.Tailscale.StateDir,
		filepath.Dir(c.Tailscale.Socket),
		c.Engine.WorkingDir,
		filepath.Dir(c.Engine.ConfigPath),
		filepath.Dir(c.Engine.SubscriptionPath),
		filepath.Dir(c.Engine.StateFile),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create runtime dir %s: %w", dir, err)
		}
	}
	return nil
}

func resolvePath(baseDir, value string) string {
	if value == "" {
		return value
	}
	candidate := expandUserPath(value)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(baseDir, value)
	}
	abs, err := filepath.Abs(candidate)
	if err == nil {
		return abs
	}
	return filepath.Clean(candidate)
}

func expandUserPath(value string) string {
	if value == "~" {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return homeDir
		}
		return value
	}
	if strings.HasPrefix(value, "~/") {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return filepath.Join(homeDir, strings.TrimPrefix(value, "~/"))
		}
	}
	return value
}

func resolveCommandPath(baseDir, value string) string {
	if value == "" {
		return value
	}
	if filepath.IsAbs(value) || strings.ContainsRune(value, filepath.Separator) {
		return resolvePath(baseDir, value)
	}
	return value
}

func (t *EngineTunConfig) applyDefaults() {
	if t.Enable == nil {
		t.Enable = boolPtr(true)
	}
	if strings.TrimSpace(t.Stack) == "" {
		t.Stack = "system"
	}
	if t.AutoRoute == nil {
		t.AutoRoute = boolPtr(true)
	}
	if t.AutoRedirect == nil {
		t.AutoRedirect = boolPtr(false)
	}
	if t.AutoDetectInterface == nil {
		t.AutoDetectInterface = boolPtr(true)
	}
	if t.StrictRoute == nil {
		t.StrictRoute = boolPtr(false)
	}
	if len(t.DNSHijack) == 0 {
		t.DNSHijack = []string{"any:53", "tcp://any:53"}
	}
}

func (t EngineTunConfig) isSet() bool {
	return t.Enable != nil ||
		strings.TrimSpace(t.Stack) != "" ||
		t.AutoRoute != nil ||
		t.AutoRedirect != nil ||
		t.AutoDetectInterface != nil ||
		t.StrictRoute != nil ||
		len(t.DNSHijack) > 0
}

func boolPtr(v bool) *bool {
	return &v
}

func applyEngineConfigPathOverride(cfg *Config, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	resolvedPath := resolvePath(".", path)
	if err := os.MkdirAll(filepath.Dir(importedEngineSourcePath(cfg)), 0o755); err != nil {
		return fmt.Errorf("create imported engine source dir: %w", err)
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return fmt.Errorf("read local engine config %s: %w", resolvedPath, err)
	}
	if err := os.WriteFile(importedEngineSourcePath(cfg), data, 0o644); err != nil {
		return fmt.Errorf("write imported engine source %s: %w", importedEngineSourcePath(cfg), err)
	}
	state := importedEngineSourceState{
		SourcePath: resolvedPath,
		WorkingDir: filepath.Dir(resolvedPath),
	}
	stateData, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal imported engine source state: %w", err)
	}
	if err := os.WriteFile(importedEngineSourceStatePath(cfg), stateData, 0o644); err != nil {
		return fmt.Errorf("write imported engine source state %s: %w", importedEngineSourceStatePath(cfg), err)
	}
	return applyImportedEngineSource(cfg)
}

func applyRememberedEngineConfigSource(cfg *Config) error {
	_, err := os.Stat(importedEngineSourcePath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat imported engine source %s: %w", importedEngineSourcePath(cfg), err)
	}
	return applyImportedEngineSource(cfg)
}

func applyImportedEngineSource(cfg *Config) error {
	state, err := readImportedEngineSourceState(cfg)
	if err != nil {
		return err
	}
	cfg.Engine.SourceConfigPath = importedEngineSourcePath(cfg)

	defaultWorkingDir := resolvePath(".", filepath.Join(cfg.RuntimeDir, "engine"))
	if cfg.Engine.WorkingDir == defaultWorkingDir && strings.TrimSpace(state.WorkingDir) != "" {
		cfg.Engine.WorkingDir = state.WorkingDir
	}

	localCfg, err := readLocalEngineConfig(cfg.Engine.SourceConfigPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(localCfg.ExternalController) != "" {
		cfg.Engine.ControllerAddr = strings.TrimSpace(localCfg.ExternalController)
	}
	cfg.Engine.Secret = localCfg.Secret
	if localCfg.MixedPort != 0 {
		cfg.Engine.MixedPort = localCfg.MixedPort
	}
	if localCfg.Tun.Enable != nil {
		cfg.Engine.Tun.Enable = localCfg.Tun.Enable
	}
	return nil
}

func readImportedEngineSourceState(cfg *Config) (*importedEngineSourceState, error) {
	data, err := os.ReadFile(importedEngineSourceStatePath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return &importedEngineSourceState{}, nil
		}
		return nil, fmt.Errorf("read imported engine source state %s: %w", importedEngineSourceStatePath(cfg), err)
	}
	var state importedEngineSourceState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse imported engine source state %s: %w", importedEngineSourceStatePath(cfg), err)
	}
	return &state, nil
}

func readLocalEngineConfig(path string) (*localEngineConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local engine config %s: %w", path, err)
	}

	var localCfg localEngineConfigFile
	if err := yaml.Unmarshal(data, &localCfg); err != nil {
		return nil, fmt.Errorf("parse local engine config %s: %w", path, err)
	}
	return &localCfg, nil
}

func importedEngineSourcePath(cfg *Config) string {
	return filepath.Join(cfg.RuntimeDir, "engine", "source.yaml")
}

func importedEngineSourceStatePath(cfg *Config) string {
	return filepath.Join(cfg.RuntimeDir, "engine", "source-state.yaml")
}
