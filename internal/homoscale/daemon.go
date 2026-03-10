package homoscale

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const daemonChildEnv = "HOMOSCALE_DAEMON_CHILD"

func daemonChildProcess() bool {
	return os.Getenv(daemonChildEnv) == "1"
}

func startDaemonProcess(cfg *Config, args []string) error {
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return err
	}
	logPath := daemonLogPath(cfg)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create daemon log dir: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daemon log %s: %w", logPath, err)
	}
	defer logFile.Close()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(executable, stripDaemonFlags(args)...)
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), daemonChildEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background homoscale: %w", err)
	}
	if err := writeDaemonPIDFile(cfg, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	fmt.Fprintf(os.Stdout, "homoscale started in background (pid %d)\n", cmd.Process.Pid)
	fmt.Fprintf(os.Stdout, "log: %s\n", logPath)
	return nil
}

func daemonLogPath(cfg *Config) string {
	return filepath.Join(cfg.RuntimeDir, "logs", "homoscale.log")
}

func daemonPIDFilePath(cfg *Config) string {
	return filepath.Join(cfg.RuntimeDir, "homoscale.pid")
}

func writeDaemonPIDFile(cfg *Config, pid int) error {
	return os.WriteFile(daemonPIDFilePath(cfg), []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func removeDaemonPIDFile(cfg *Config) {
	_ = os.Remove(daemonPIDFilePath(cfg))
}

func stripDaemonFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case arg == "-d", arg == "--daemon":
			continue
		case strings.HasPrefix(arg, "--daemon="):
			continue
		case strings.HasPrefix(arg, "-d="):
			continue
		default:
			out = append(out, arg)
		}
	}
	return out
}
