//go:build !android

package homoscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

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

	cmd, extraFiles, err := buildEngineCommand(ctx, cfg)
	if err != nil {
		return err
	}
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	if err := cmd.Start(); err != nil {
		closeFiles(extraFiles)
		return fmt.Errorf("start homoscale engine: %w", err)
	}
	closeFiles(extraFiles)
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
	resolverCloser := installSystemResolver(cfg, logWriter)
	defer resolverCloser.Close()
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

func buildEngineCommand(ctx context.Context, cfg *Config) (*exec.Cmd, []*os.File, error) {
	cmd := exec.CommandContext(ctx, cfg.Engine.Binary, buildEngineArgs(cfg)...)
	extraFiles, err := buildEngineExtraFiles(cfg)
	if err != nil {
		return nil, nil, err
	}
	if len(extraFiles) > 0 {
		cmd.ExtraFiles = extraFiles
	}
	return cmd, extraFiles, nil
}

func buildEngineExtraFiles(cfg *Config) ([]*os.File, error) {
	if cfg == nil || cfg.Engine.Tun.FileDescriptor == 0 {
		return nil, nil
	}
	if cfg.Engine.Tun.FileDescriptor != 3 {
		return nil, fmt.Errorf("unsupported engine.tun.file_descriptor %d: only 3 is supported", cfg.Engine.Tun.FileDescriptor)
	}
	if cfg.Engine.Tun.RuntimeFile == nil {
		return nil, fmt.Errorf("engine.tun.file_descriptor is configured but no runtime tun file was provided")
	}
	return []*os.File{cfg.Engine.Tun.RuntimeFile}, nil
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

func resolveBinaryPath(binary string) (string, error) {
	return exec.LookPath(binary)
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		if file == nil {
			continue
		}
		_ = file.Close()
	}
}

func RuntimeDebugInfo(cfg *Config) any {
	return map[string]any{
		"runtimeGoos":            runtimeGOOS,
		"runningOnAndroid":       runningOnAndroid(),
		"defaultFindProcessMode": defaultFindProcessMode(),
	}
}
