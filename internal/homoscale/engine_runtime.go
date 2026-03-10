package homoscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type ProcessState struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func StartEngine(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return err
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}
	createdConfig, err := ensureEngineConfig(cfg)
	if err != nil {
		return err
	}
	if _, err := resolveBinaryPath(cfg.Engine.Binary); err != nil {
		return fmt.Errorf("engine binary not found %q: %w", cfg.Engine.Binary, err)
	}
	if state, ok := readProcessState(cfg.Engine.StateFile); ok && processRunning(state.PID) {
		return fmt.Errorf("homoscale engine is already running (pid %d)", state.PID)
	}

	cmd := exec.CommandContext(ctx, cfg.Engine.Binary, buildEngineArgs(cfg)...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start homoscale engine: %w", err)
	}
	if err := writeProcessState(cfg.Engine.StateFile, ProcessState{
		PID:     cmd.Process.Pid,
		Command: cmd.String(),
	}); err != nil {
		_ = terminateProcess(cmd.Process)
		return err
	}
	defer removeProcessState(cfg.Engine.StateFile)

	if err := waitForEngineReady(cfg, 15*time.Second); err != nil {
		_ = terminateProcess(cmd.Process)
		return err
	}
	if logWriter != nil {
		if createdConfig {
			_, _ = fmt.Fprintf(logWriter, "generated default engine config at %s\n", cfg.Engine.ConfigPath)
		}
		_, _ = fmt.Fprintf(logWriter, "homoscale engine ready at %s\n", cfg.Engine.ControllerAddr)
	}

	err = cmd.Wait()
	if ctx.Err() != nil && (errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return nil
	}
	if err == nil {
		return nil
	}
	return fmt.Errorf("homoscale engine exited: %w", err)
}

func StopEngine(cfg *Config) error {
	state, ok := readProcessState(cfg.Engine.StateFile)
	if !ok {
		return fmt.Errorf("homoscale engine is not running")
	}
	if !processRunning(state.PID) {
		removeProcessState(cfg.Engine.StateFile)
		return fmt.Errorf("homoscale engine is not running")
	}
	process, err := os.FindProcess(state.PID)
	if err != nil {
		return fmt.Errorf("find engine process: %w", err)
	}
	if err := terminateProcess(process); err != nil {
		return err
	}
	removeProcessState(cfg.Engine.StateFile)
	return nil
}

func buildEngineArgs(cfg *Config) []string {
	args := []string{
		"-d", cfg.Engine.WorkingDir,
		"-f", cfg.Engine.ConfigPath,
	}
	args = append(args, cfg.Engine.RunArgs...)
	return args
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

func terminateProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	_ = process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			if !processRunning(process.Pid) {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()
	<-done
	if processRunning(process.Pid) {
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("stop engine process: %w", err)
		}
	}
	return nil
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

func resolveBinaryPath(binary string) (string, error) {
	return exec.LookPath(binary)
}
