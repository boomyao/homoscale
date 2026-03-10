package homoscale

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadProxySnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/configs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"mode":      "rule",
				"mode-list": []string{"rule", "global", "direct"},
			})
		case "/proxies":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"PROXY": map[string]any{
						"name": "PROXY",
						"type": "Selector",
						"now":  "HK",
						"all":  []string{"HK", "JP", "DIRECT"},
					},
					"HK": map[string]any{
						"name": "HK",
						"type": "Shadowsocks",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{Engine: EngineConfig{ControllerAddr: server.URL}}
	snapshot, err := ReadProxySnapshot(cfg)
	if err != nil {
		t.Fatalf("read proxy snapshot: %v", err)
	}
	if snapshot.Mode != "rule" {
		t.Fatalf("unexpected mode: %s", snapshot.Mode)
	}
	if len(snapshot.Selectors) != 1 {
		t.Fatalf("unexpected selector count: %d", len(snapshot.Selectors))
	}
	if snapshot.Selectors[0].Now != "HK" {
		t.Fatalf("unexpected selected proxy: %s", snapshot.Selectors[0].Now)
	}
}

func TestPrintProxyCurrentText(t *testing.T) {
	snapshot := &ProxyConfigSnapshot{
		Mode: "rule",
		Selectors: []ProxySelector{
			{Name: "PROXY", Now: "HK"},
			{Name: "VIDEO", Now: "JP"},
		},
	}

	text := printProxyCurrentText(snapshot)
	if !strings.Contains(text, "mode: rule") {
		t.Fatalf("unexpected current text: %s", text)
	}
	if !strings.Contains(text, "PROXY -> HK") {
		t.Fatalf("unexpected current text: %s", text)
	}
	if !strings.Contains(text, "VIDEO -> JP") {
		t.Fatalf("unexpected current text: %s", text)
	}
}

func TestPrintSelectorOptionsText(t *testing.T) {
	snapshot := &ProxyConfigSnapshot{
		Selectors: []ProxySelector{
			{Name: "PROXY", Type: "Selector", Now: "HK", Options: []string{"HK", "JP", "DIRECT"}},
			{Name: "VIDEO", Type: "Selector", Now: "JP", Options: []string{"JP", "SG"}},
		},
	}

	text, err := printSelectorOptionsText(snapshot, "")
	if err != nil {
		t.Fatalf("print selector groups: %v", err)
	}
	if !strings.Contains(text, "selector groups: PROXY, VIDEO") {
		t.Fatalf("unexpected group list: %s", text)
	}

	text, err = printSelectorOptionsText(snapshot, "PROXY")
	if err != nil {
		t.Fatalf("print selector options: %v", err)
	}
	if !strings.Contains(text, "current: HK") || !strings.Contains(text, "options: HK, JP, DIRECT") {
		t.Fatalf("unexpected selector options: %s", text)
	}
}

func TestSetProxyMode(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/configs" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := &Config{Engine: EngineConfig{ControllerAddr: server.URL}}
	if err := SetProxyMode(cfg, "global"); err != nil {
		t.Fatalf("set proxy mode: %v", err)
	}
	if got["mode"] != "global" {
		t.Fatalf("unexpected mode payload: %#v", got)
	}
}

func TestSelectProxyGroup(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/proxies/PROXY" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := &Config{Engine: EngineConfig{ControllerAddr: server.URL}}
	if err := SelectProxyGroup(cfg, "PROXY", "JP"); err != nil {
		t.Fatalf("select proxy group: %v", err)
	}
	if got["name"] != "JP" {
		t.Fatalf("unexpected proxy selection payload: %#v", got)
	}
}

func TestValidateProxySelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/configs":
			_ = json.NewEncoder(w).Encode(map[string]any{"mode": "rule"})
		case "/proxies":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"proxies": map[string]any{
					"PROXY": map[string]any{
						"name": "PROXY",
						"type": "Selector",
						"now":  "HK",
						"all":  []string{"HK", "JP"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{Engine: EngineConfig{ControllerAddr: server.URL}}
	if err := validateProxySelection(cfg, "PROXY", "JP"); err != nil {
		t.Fatalf("expected valid selection: %v", err)
	}
	if err := validateProxySelection(cfg, "PROXY", "SG"); err == nil {
		t.Fatal("expected invalid proxy selection")
	}
	if err := validateProxySelection(cfg, "VIDEO", "SG"); err == nil {
		t.Fatal("expected missing group error")
	}
}
