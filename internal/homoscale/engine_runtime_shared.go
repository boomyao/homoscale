package homoscale

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

type ProcessState struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func waitForEngineReady(cfg *Config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := ReadEngineStatus(cfg)
		if status.Reachable {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("homoscale engine did not become ready within %s", timeout)
}

func readProcessState(path string) (ProcessState, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProcessState{}, false
	}
	var state ProcessState
	if err := json.Unmarshal(data, &state); err != nil {
		return ProcessState{}, false
	}
	if state.PID == 0 {
		return ProcessState{}, false
	}
	return state, true
}

func readEngineProcessState(cfg *Config) *ProcessState {
	state, ok := readProcessState(cfg.Engine.StateFile)
	if !ok {
		return nil
	}
	return &state
}

func writeProcessState(path string, state ProcessState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal process state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write process state %s: %w", path, err)
	}
	return nil
}

func removeProcessState(path string) {
	_ = os.Remove(path)
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func controllerURL(cfg *Config, path string) string {
	endpoint := cfg.Engine.ControllerAddr
	if endpoint == "" {
		endpoint = defaultEngineControllerAddr
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return endpoint + path
}

func enginePing(cfg *Config) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(controllerURL(cfg, "/version"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("engine status: %s", resp.Status)
	}
	return nil
}
