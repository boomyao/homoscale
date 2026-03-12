package homoscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

type AuthStatus struct {
	Backend          string   `json:"backend"`
	BackendState     string   `json:"backend_state"`
	LoggedIn         bool     `json:"logged_in"`
	NeedsLogin       bool     `json:"needs_login,omitempty"`
	NeedsMachineAuth bool     `json:"needs_machine_auth,omitempty"`
	AuthURL          string   `json:"auth_url,omitempty"`
	Tailnet          string   `json:"tailnet,omitempty"`
	MagicDNSSuffix   string   `json:"magic_dns_suffix,omitempty"`
	MagicDNSEnabled  bool     `json:"magic_dns_enabled,omitempty"`
	TailscaleIPs     []string `json:"tailscale_ips,omitempty"`
	Health           []string `json:"health,omitempty"`
	Socket           string   `json:"socket,omitempty"`
}

type authRuntimeSnapshot struct {
	PID           int               `json:"pid"`
	Status        *AuthStatus       `json:"status"`
	LoopbackAddr  string            `json:"loopback_addr,omitempty"`
	ProxyUser     string            `json:"proxy_user,omitempty"`
	ProxyPass     string            `json:"proxy_pass,omitempty"`
	MagicDNSHosts map[string]string `json:"magic_dns_hosts,omitempty"`
}

func ReadAuthStatus(cfg *Config) (*AuthStatus, error) {
	switch cfg.Tailscale.Backend {
	case "embedded":
		if status, ok := readRuntimeAuthStatus(cfg); ok {
			return status, nil
		}
		return readEmbeddedAuthStatus(context.Background(), cfg, io.Discard)
	case "external":
		return readExternalAuthStatus(context.Background(), cfg)
	default:
		return nil, fmt.Errorf("unsupported tailscale.backend %q", cfg.Tailscale.Backend)
	}
}

func LoginTailscale(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return err
	}
	if logWriter == nil {
		logWriter = io.Discard
	}

	switch cfg.Tailscale.Backend {
	case "embedded":
		return loginEmbeddedTailscale(ctx, cfg, logWriter)
	case "external":
		return loginExternalTailscale(ctx, cfg, logWriter)
	default:
		return fmt.Errorf("unsupported tailscale.backend %q", cfg.Tailscale.Backend)
	}
}

func LogoutTailscale(ctx context.Context, cfg *Config) error {
	switch cfg.Tailscale.Backend {
	case "embedded":
		return logoutEmbeddedTailscale(ctx, cfg)
	case "external":
		return logoutExternalTailscale(ctx, cfg)
	default:
		return fmt.Errorf("unsupported tailscale.backend %q", cfg.Tailscale.Backend)
	}
}

func readEmbeddedAuthStatus(ctx context.Context, cfg *Config, logWriter io.Writer) (*AuthStatus, error) {
	server := newEmbeddedTailscaleServer(cfg, logWriter)
	defer server.Close()

	localClient, err := server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("start embedded tailscale client: %w", err)
	}
	status, err := localClient.StatusWithoutPeers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read embedded tailscale status: %w", err)
	}
	return authStatusFromIPN("embedded", "", status), nil
}

func readExternalAuthStatus(ctx context.Context, cfg *Config) (*AuthStatus, error) {
	status, err := tailscaleLocalStatus(cfg)
	if err != nil {
		return nil, fmt.Errorf("read external tailscale status: %w", err)
	}
	return authStatusFromIPN("external", cfg.Tailscale.Socket, status), nil
}

func loginEmbeddedTailscale(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	server := newEmbeddedTailscaleServer(cfg, logWriter)
	defer server.Close()

	status, err := server.Up(ctx)
	if err != nil {
		return fmt.Errorf("tailscale login failed: %w", err)
	}
	localClient, err := server.LocalClient()
	if err != nil {
		return fmt.Errorf("start embedded tailscale client: %w", err)
	}
	if err := configureEmbeddedAdvertiseRoutes(ctx, localClient, cfg); err != nil {
		return err
	}

	return printAuthSummary(logWriter, authStatusFromIPN("embedded", "", status), "logged in")
}

func startEmbeddedTailscaleRuntime(ctx context.Context, cfg *Config, logWriter io.Writer) (io.Closer, error) {
	server := newEmbeddedTailscaleServer(cfg, logWriter)
	forwarder := newEmbeddedHostTCPForwarder(cfg)

	status, err := server.Up(ctx)
	if err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("start embedded tailscale: %w", err)
	}

	localClient, err := server.LocalClient()
	if err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("start embedded tailscale client: %w", err)
	}
	if err := configureEmbeddedAdvertiseRoutes(ctx, localClient, cfg); err != nil {
		_ = server.Close()
		return nil, err
	}

	runtime := &embeddedTailscaleRuntime{
		server:      server,
		localClient: localClient,
		statusPath:  tailscaleRuntimeStatusPath(cfg),
	}
	if forwarder != nil {
		runtime.deregister = forwarder.register(server)
	}
	loopbackAddr, proxyCred, _, err := server.Loopback()
	if err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("start embedded tailscale loopback: %w", err)
	}
	initialStatus := authStatusFromIPN("embedded", "", status)
	runtime.snapshot = authRuntimeSnapshot{
		Status:        initialStatus,
		LoopbackAddr:  loopbackAddr,
		ProxyUser:     "tsnet",
		ProxyPass:     proxyCred,
		MagicDNSHosts: magicDNSHostsFromStatus(status),
	}
	if fullStatus, err := localClient.Status(ctx); err == nil {
		runtime.snapshot.Status = authStatusFromIPN("embedded", "", fullStatus)
		runtime.snapshot.MagicDNSHosts = magicDNSHostsFromStatus(fullStatus)
	}
	if err := writeRuntimeAuthStatus(cfg, runtime.snapshot); err != nil {
		_ = server.Close()
		return nil, err
	}
	go runtime.watchStatus(ctx, cfg)

	if err := printAuthSummary(logWriter, initialStatus, "tailscale ready"); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return runtime, nil
}

func loginExternalTailscale(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	if _, err := os.Stat(cfg.Tailscale.Socket); err != nil {
		return fmt.Errorf("tailscale socket not available at %s; start tailscaled first for external mode", cfg.Tailscale.Socket)
	}
	if err := ensureTailnetAuth(ctx, cfg); err != nil {
		return err
	}

	status, err := tailscaleLocalStatus(cfg)
	if err != nil {
		return nil
	}
	return printAuthSummary(logWriter, authStatusFromIPN("external", cfg.Tailscale.Socket, status), "logged in")
}

func startExternalTailscaleRuntime(ctx context.Context, cfg *Config, logWriter io.Writer) (io.Closer, error) {
	if _, err := os.Stat(cfg.Tailscale.Socket); err != nil {
		return nil, fmt.Errorf("tailscale socket not available at %s; start tailscaled first for external mode", cfg.Tailscale.Socket)
	}
	if err := ensureTailnetAuth(ctx, cfg); err != nil {
		return nil, err
	}

	status, err := tailscaleLocalStatus(cfg)
	if err != nil {
		return nil, fmt.Errorf("read external tailscale status: %w", err)
	}
	if err := printAuthSummary(logWriter, authStatusFromIPN("external", cfg.Tailscale.Socket, status), "tailscale ready"); err != nil {
		return nil, err
	}
	return nopCloser{}, nil
}

func logoutEmbeddedTailscale(ctx context.Context, cfg *Config) error {
	server := newEmbeddedTailscaleServer(cfg, io.Discard)
	defer server.Close()

	localClient, err := server.LocalClient()
	if err != nil {
		return fmt.Errorf("start embedded tailscale client: %w", err)
	}
	if err := localClient.Logout(ctx); err != nil {
		return fmt.Errorf("tailscale logout failed: %w", err)
	}
	return nil
}

func logoutExternalTailscale(ctx context.Context, cfg *Config) error {
	if _, err := os.Stat(cfg.Tailscale.Socket); err != nil {
		return fmt.Errorf("tailscale socket not available at %s; start tailscaled first for external mode", cfg.Tailscale.Socket)
	}

	localClient := &local.Client{
		Socket:        cfg.Tailscale.Socket,
		UseSocketOnly: true,
	}
	if err := localClient.Logout(ctx); err != nil {
		return fmt.Errorf("tailscale logout failed: %w", err)
	}
	return nil
}

func newEmbeddedTailscaleServer(cfg *Config, logWriter io.Writer) *tsnet.Server {
	if logWriter == nil {
		logWriter = io.Discard
	}
	userLogger := log.New(logWriter, "[tailscale] ", log.LstdFlags)
	server := &tsnet.Server{
		Dir:           cfg.Tailscale.StateDir,
		Hostname:      cfg.Tailscale.Hostname,
		ControlURL:    cfg.Tailscale.LoginServer,
		Ephemeral:     cfg.Tailscale.Embedded.Ephemeral,
		AdvertiseTags: slices.Clone(cfg.Tailscale.Embedded.AdvertiseTags),
		UserLogf:      userLogger.Printf,
	}
	if cfg.Tailscale.Embedded.VerboseLogs {
		server.Logf = userLogger.Printf
	}
	if authKey := os.Getenv(cfg.Tailscale.AuthKeyEnv); authKey != "" {
		server.AuthKey = authKey
	}
	return server
}

func configureEmbeddedAdvertiseRoutes(ctx context.Context, localClient *local.Client, cfg *Config) error {
	if localClient == nil {
		return nil
	}

	routes, err := cfg.Tailscale.AdvertiseRoutePrefixes()
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return nil
	}

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			AdvertiseRoutes: routes,
		},
		AdvertiseRoutesSet: true,
	}
	if cfg.Tailscale.SNATSubnetRoutes != nil {
		maskedPrefs.Prefs.NoSNAT = !*cfg.Tailscale.SNATSubnetRoutes
		maskedPrefs.NoSNATSet = true
	}
	if _, err := localClient.EditPrefs(ctx, maskedPrefs); err != nil {
		return fmt.Errorf("configure embedded tailscale advertised routes: %w", err)
	}
	return nil
}

func authStatusFromIPN(backend, socket string, status *ipnstate.Status) *AuthStatus {
	if status == nil {
		return &AuthStatus{Backend: backend, Socket: socket}
	}

	out := &AuthStatus{
		Backend:          backend,
		BackendState:     status.BackendState,
		LoggedIn:         status.BackendState == "Running",
		NeedsLogin:       status.BackendState == "NeedsLogin",
		NeedsMachineAuth: status.BackendState == "NeedsMachineAuth",
		AuthURL:          status.AuthURL,
		MagicDNSSuffix:   magicDNSSuffixFromStatus(status, ""),
		MagicDNSEnabled:  magicDNSEnabledFromStatus(status),
		TailscaleIPs:     netipStrings(status.TailscaleIPs),
		Health:           slices.Clone(status.Health),
		Socket:           socket,
	}
	if status.CurrentTailnet != nil {
		out.Tailnet = status.CurrentTailnet.Name
	}
	return out
}

func printAuthSummary(w io.Writer, status *AuthStatus, verb string) error {
	if w == nil || status == nil {
		return nil
	}
	if status.Tailnet != "" {
		_, err := fmt.Fprintf(w, "%s to tailnet %s\n", verb, status.Tailnet)
		return err
	}
	_, err := fmt.Fprintf(w, "%s\n", verb)
	return err
}

type embeddedTailscaleRuntime struct {
	server      *tsnet.Server
	localClient *local.Client
	statusPath  string
	snapshot    authRuntimeSnapshot
	deregister  func()
}

func (r *embeddedTailscaleRuntime) Close() error {
	if r == nil {
		return nil
	}
	removeRuntimeAuthStatus(r.statusPath)
	if r.deregister != nil {
		r.deregister()
	}
	if r.server == nil {
		return nil
	}
	return r.server.Close()
}

func (r *embeddedTailscaleRuntime) watchStatus(ctx context.Context, cfg *Config) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			removeRuntimeAuthStatus(r.statusPath)
			return
		case <-ticker.C:
			status, err := r.localClient.Status(ctx)
			if err != nil {
				continue
			}
			r.snapshot.Status = authStatusFromIPN("embedded", "", status)
			r.snapshot.MagicDNSHosts = magicDNSHostsFromStatus(status)
			_ = writeRuntimeAuthStatus(cfg, r.snapshot)
		}
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func tailscaleRuntimeStatusPath(cfg *Config) string {
	return filepath.Join(cfg.Tailscale.StateDir, "runtime-status.json")
}

func writeRuntimeAuthStatus(cfg *Config, snapshot authRuntimeSnapshot) error {
	snapshot.PID = os.Getpid()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tailscale runtime status: %w", err)
	}
	path := tailscaleRuntimeStatusPath(cfg)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write tailscale runtime status %s: %w", path, err)
	}
	return nil
}

func readRuntimeAuthStatus(cfg *Config) (*AuthStatus, bool) {
	snapshot, ok := readRuntimeAuthSnapshot(cfg)
	if !ok {
		return nil, false
	}
	return snapshot.Status, true
}

func readRuntimeAuthSnapshot(cfg *Config) (*authRuntimeSnapshot, bool) {
	data, err := os.ReadFile(tailscaleRuntimeStatusPath(cfg))
	if err != nil {
		return nil, false
	}
	var snapshot authRuntimeSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, false
	}
	if snapshot.PID <= 0 || !processRunning(snapshot.PID) || snapshot.Status == nil {
		removeRuntimeAuthStatus(tailscaleRuntimeStatusPath(cfg))
		return nil, false
	}
	return &snapshot, true
}

func removeRuntimeAuthStatus(path string) {
	_ = os.Remove(path)
}
