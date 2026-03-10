package homoscale

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ProxyConfigSnapshot struct {
	Mode      string          `json:"mode"`
	ModeList  []string        `json:"mode_list,omitempty"`
	Selectors []ProxySelector `json:"selectors,omitempty"`
}

type ProxySelector struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now,omitempty"`
	Options []string `json:"options,omitempty"`
}

type EngineStatus struct {
	Reachable      bool                 `json:"reachable"`
	Error          string               `json:"error,omitempty"`
	ControllerAddr string               `json:"controller_addr,omitempty"`
	Process        *ProcessState        `json:"process,omitempty"`
	Snapshot       *ProxyConfigSnapshot `json:"snapshot,omitempty"`
}

func (s ProxySelector) containsOption(name string) bool {
	for _, option := range s.Options {
		if option == name {
			return true
		}
	}
	return false
}

type engineConfigResponse struct {
	Mode     string   `json:"mode"`
	ModeList []string `json:"mode-list"`
	Modes    []string `json:"modes"`
}

type engineProxyResponse struct {
	Proxies map[string]engineProxy `json:"proxies"`
}

type engineProxy struct {
	Name string   `json:"name"`
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

func ReadProxySnapshot(cfg *Config) (*ProxyConfigSnapshot, error) {
	configResp := &engineConfigResponse{}
	if err := engineAPI(cfg, http.MethodGet, "/configs", nil, configResp); err != nil {
		return nil, err
	}

	proxyResp := &engineProxyResponse{}
	if err := engineAPI(cfg, http.MethodGet, "/proxies", nil, proxyResp); err != nil {
		return nil, err
	}

	snapshot := &ProxyConfigSnapshot{
		Mode:     strings.ToLower(strings.TrimSpace(configResp.Mode)),
		ModeList: proxyModeList(configResp),
	}
	for _, proxy := range selectorGroups(proxyResp.Proxies) {
		snapshot.Selectors = append(snapshot.Selectors, ProxySelector{
			Name:    proxy.Name,
			Type:    proxy.Type,
			Now:     proxy.Now,
			Options: append([]string(nil), proxy.All...),
		})
	}
	return snapshot, nil
}

func ReadEngineStatus(cfg *Config) *EngineStatus {
	if err := enginePing(cfg); err != nil {
		return &EngineStatus{
			Reachable:      false,
			Error:          humanizeEngineError(err),
			ControllerAddr: cfg.Engine.ControllerAddr,
			Process:        readEngineProcessState(cfg),
		}
	}
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return &EngineStatus{
			Reachable: false,
			Error:     humanizeEngineError(err),
		}
	}
	return &EngineStatus{
		Reachable:      true,
		ControllerAddr: cfg.Engine.ControllerAddr,
		Process:        readEngineProcessState(cfg),
		Snapshot:       snapshot,
	}
}

func SetProxyMode(cfg *Config, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "rule", "global", "direct":
	default:
		return fmt.Errorf("unsupported proxy mode %q", mode)
	}
	return engineAPI(cfg, http.MethodPatch, "/configs", map[string]string{"mode": mode}, nil)
}

func SelectProxyGroup(cfg *Config, groupName, proxyName string) error {
	groupName = strings.TrimSpace(groupName)
	proxyName = strings.TrimSpace(proxyName)
	if groupName == "" || proxyName == "" {
		return fmt.Errorf("group and proxy names are required")
	}
	path := "/proxies/" + url.PathEscape(groupName)
	return engineAPI(cfg, http.MethodPut, path, map[string]string{"name": proxyName}, nil)
}

func validateProxySelection(cfg *Config, groupName, proxyName string) error {
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return err
	}
	for _, selector := range snapshot.Selectors {
		if selector.Name != groupName {
			continue
		}
		if selector.containsOption(proxyName) {
			return nil
		}
		return fmt.Errorf("proxy %q is not available in group %q", proxyName, groupName)
	}
	return fmt.Errorf("proxy group %q not found", groupName)
}

func printProxySnapshotText(snapshot *ProxyConfigSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode: %s\n", snapshot.Mode)
	if len(snapshot.ModeList) > 0 {
		fmt.Fprintf(&b, "modes: %s\n", strings.Join(snapshot.ModeList, ", "))
	}
	if len(snapshot.Selectors) == 0 {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("groups:\n")
	for _, selector := range snapshot.Selectors {
		fmt.Fprintf(&b, "- %s (%s): %s\n", selector.Name, selector.Type, selector.Now)
		if len(selector.Options) > 0 {
			fmt.Fprintf(&b, "  options: %s\n", strings.Join(selector.Options, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func printProxyCurrentText(snapshot *ProxyConfigSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mode: %s\n", snapshot.Mode)
	for _, selector := range snapshot.Selectors {
		fmt.Fprintf(&b, "%s -> %s\n", selector.Name, selector.Now)
	}
	return strings.TrimRight(b.String(), "\n")
}

func printProxyRulesText(snapshot *ProxyConfigSnapshot) string {
	if len(snapshot.Selectors) == 0 {
		return "no selector groups"
	}
	var b strings.Builder
	for _, selector := range snapshot.Selectors {
		fmt.Fprintf(&b, "%s (%s)\n", selector.Name, selector.Type)
		fmt.Fprintf(&b, "current: %s\n", selector.Now)
		if len(selector.Options) > 0 {
			fmt.Fprintf(&b, "options: %s\n", strings.Join(selector.Options, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func printSelectorOptionsText(snapshot *ProxyConfigSnapshot, groupName string) (string, error) {
	if strings.TrimSpace(groupName) == "" {
		names := make([]string, 0, len(snapshot.Selectors))
		for _, selector := range snapshot.Selectors {
			names = append(names, selector.Name)
		}
		if len(names) == 0 {
			return "no selector groups", nil
		}
		return "selector groups: " + strings.Join(names, ", "), nil
	}

	for _, selector := range snapshot.Selectors {
		if selector.Name != groupName {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s (%s)\n", selector.Name, selector.Type)
		fmt.Fprintf(&b, "current: %s\n", selector.Now)
		fmt.Fprintf(&b, "options: %s", strings.Join(selector.Options, ", "))
		return b.String(), nil
	}
	return "", fmt.Errorf("proxy group %q not found", groupName)
}

func printProxySnapshot(w io.Writer, cfg *Config) error {
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, printProxySnapshotText(snapshot))
	return err
}

func printProxyCurrent(w io.Writer, cfg *Config) error {
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, printProxyCurrentText(snapshot))
	return err
}

func printProxyRules(w io.Writer, cfg *Config) error {
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, printProxyRulesText(snapshot))
	return err
}

func printProxySelectHelp(w io.Writer, cfg *Config, groupName string) error {
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		return err
	}
	text, err := printSelectorOptionsText(snapshot, groupName)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, text)
	return err
}

func proxyModeList(configResp *engineConfigResponse) []string {
	switch {
	case len(configResp.ModeList) > 0:
		return cloneStringsLower(configResp.ModeList)
	case len(configResp.Modes) > 0:
		return cloneStringsLower(configResp.Modes)
	default:
		return []string{"rule", "global", "direct"}
	}
}

func selectorGroups(all map[string]engineProxy) []engineProxy {
	names := make([]string, 0, len(all))
	for name, proxy := range all {
		if len(proxy.All) == 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]engineProxy, 0, len(names))
	for _, name := range names {
		proxy := all[name]
		if proxy.Name == "" {
			proxy.Name = name
		}
		out = append(out, proxy)
	}
	return out
}

func cloneStringsLower(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.ToLower(item))
	}
	return out
}

func engineAPI(cfg *Config, method, path string, payload any, out any) error {
	endpoint := controllerURL(cfg, "")

	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal engine request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, endpoint+path, body)
	if err != nil {
		return fmt.Errorf("build engine request: %w", err)
	}
	if cfg.Engine.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Engine.Secret)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call homoscale engine %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var msg map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&msg); err == nil {
			if value, ok := msg["message"].(string); ok && value != "" {
				return fmt.Errorf("homoscale engine %s %s: %s", method, path, value)
			}
		}
		return fmt.Errorf("homoscale engine %s %s: %s", method, path, resp.Status)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode homoscale engine response %s %s: %w", method, path, err)
	}
	return nil
}

func humanizeEngineError(err error) string {
	if err == nil {
		return ""
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) || strings.Contains(err.Error(), "connection refused") {
		return "homoscale engine is not running or its local controller is unreachable"
	}
	return err.Error()
}
