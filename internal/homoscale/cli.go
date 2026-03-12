package homoscale

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func Run(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runStart(args)
	}

	switch args[0] {
	case "start":
		return runStart(args[1:])
	case "stop":
		return runStop(args[1:])
	case "auth":
		return runAuth(args[1:])
	case "login":
		return runLogin(args[1:])
	case "logout":
		return runLogout(args[1:])
	case "status":
		return runStatus(args[1:])
	case "mode":
		return runMode(args[1:])
	case "select":
		return runSelect(args[1:])
	case "current":
		return runCurrent(args[1:])
	case "rules":
		return runRules(args[1:])
	case "version":
		return runVersion(args[1:])
	default:
		return usage()
	}
}

func runStart(args []string) error {
	cfg, daemonize, err := parseStartArgs("start", args)
	if err != nil {
		return err
	}
	if daemonize && !daemonChildProcess() {
		return startDaemonProcess(cfg, args)
	}
	if daemonChildProcess() {
		defer removeDaemonPIDFile(cfg)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartHomoscale(ctx, cfg, os.Stdout)
}

func runStop(args []string) error {
	cfg, err := parseConfigFlag("stop", args)
	if err != nil {
		return err
	}
	if err := StopEngine(cfg); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "engine stopped")
	return nil
}

func runAuth(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runStatus(args)
	}

	switch args[0] {
	case "status":
		return runStatus(args[1:])
	case "login":
		return runLogin(args[1:])
	case "logout":
		return runLogout(args[1:])
	default:
		return authUsage()
	}
}

func runStatus(args []string) error {
	cfg, err := parseConfigFlag("status", args)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(ReadStatus(cfg))
}

func runLogin(args []string) error {
	cfg, err := parseConfigFlag("login", args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return LoginTailscale(ctx, cfg, os.Stdout)
}

func runLogout(args []string) error {
	cfg, err := parseConfigFlag("logout", args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := LogoutTailscale(ctx, cfg); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "logged out")
	return nil
}

func runMode(args []string) error {
	cfg, rest, err := parseControlArgs("mode", args)
	if err != nil {
		return err
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}
	if len(rest) != 1 {
		return modeUsage()
	}
	return setModeAndPrint(cfg, rest[0])
}

func runSelect(args []string) error {
	cfg, rest, err := parseControlArgs("select", args)
	if err != nil {
		return err
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}
	if len(rest) == 0 {
		return printProxySelectHelp(os.Stdout, cfg, "")
	}
	if len(rest) == 1 {
		return printProxySelectHelp(os.Stdout, cfg, rest[0])
	}
	if len(rest) != 2 {
		return selectUsage()
	}
	if err := validateProxySelection(cfg, rest[0], rest[1]); err != nil {
		return fmt.Errorf("%s", humanizeEngineError(err))
	}
	if err := SelectProxyGroup(cfg, rest[0], rest[1]); err != nil {
		return fmt.Errorf("%s", humanizeEngineError(err))
	}
	fmt.Fprintf(os.Stdout, "selected %s -> %s\n", rest[0], rest[1])
	return nil
}

func runCurrent(args []string) error {
	cfg, rest, err := parseControlArgs("current", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return currentUsage()
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}
	if err := printProxyCurrent(os.Stdout, cfg); err != nil {
		return fmt.Errorf("%s", humanizeEngineError(err))
	}
	return nil
}

func runRules(args []string) error {
	cfg, rest, err := parseControlArgs("rules", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return rulesUsage()
	}
	if err := cfg.ValidateProxy(); err != nil {
		return err
	}
	if err := printProxyRules(os.Stdout, cfg); err != nil {
		return fmt.Errorf("%s", humanizeEngineError(err))
	}
	return nil
}

func setModeAndPrint(cfg *Config, mode string) error {
	if err := SetProxyMode(cfg, mode); err != nil {
		return fmt.Errorf("%s", humanizeEngineError(err))
	}
	fmt.Fprintf(os.Stdout, "mode: %s\n", strings.ToLower(mode))
	return nil
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "print version info as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(VersionDetails())
	}
	fmt.Fprintln(os.Stdout, VersionSummary())
	return nil
}

func parseConfigFlag(command string, args []string) (*Config, error) {
	cfg, _, err := parseCLIArgs(command, args)
	return cfg, err
}

func parseStartArgs(command string, args []string) (*Config, bool, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := fs.String("c", defaultCLIConfigPath(), "config file path")
	engineConfigPath := fs.String("engine-config", "", "local mihomo config path")
	engineConfigPathShort := fs.String("f", "", "local mihomo config path")
	daemonize := fs.Bool("d", false, "run homoscale in background")
	daemonizeLong := fs.Bool("daemon", false, "run homoscale in background")
	if err := fs.Parse(args); err != nil {
		return nil, false, err
	}
	cfg, err := loadCLIConfig(fs, *configPath, *engineConfigPath, *engineConfigPathShort)
	if err != nil {
		return nil, false, err
	}
	return cfg, *daemonize || *daemonizeLong, nil
}

func parseControlArgs(command string, args []string) (*Config, []string, error) {
	return parseCLIArgs(command, args)
}

func parseCLIArgs(command string, args []string) (*Config, []string, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	configPath := fs.String("c", defaultCLIConfigPath(), "config file path")
	engineConfigPath := fs.String("engine-config", "", "local mihomo config path")
	engineConfigPathShort := fs.String("f", "", "local mihomo config path")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if *configPath == "" {
		return nil, nil, errors.New("config path is required")
	}
	cfg, err := loadCLIConfig(fs, *configPath, *engineConfigPath, *engineConfigPathShort)
	if err != nil {
		return nil, nil, err
	}
	return cfg, fs.Args(), nil
}

func loadCLIConfig(fs *flag.FlagSet, configPath, engineConfigPath, engineConfigPathShort string) (*Config, error) {
	if configPath == "" {
		return nil, errors.New("config path is required")
	}
	explicitConfig := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "c" {
			explicitConfig = true
		}
	})
	cfg, err := loadConfigWithFallback(configPath, explicitConfig)
	if err != nil {
		return nil, err
	}
	overridePath, err := resolveCLIEngineConfigPath(engineConfigPath, engineConfigPathShort)
	if err != nil {
		return nil, err
	}
	if overridePath != "" {
		if err := applyEngineConfigPathOverride(cfg, overridePath); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err := applyRememberedEngineConfigSource(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func resolveCLIEngineConfigPath(longPath, shortPath string) (string, error) {
	longPath = strings.TrimSpace(longPath)
	shortPath = strings.TrimSpace(shortPath)
	if longPath != "" && shortPath != "" && longPath != shortPath {
		return "", errors.New("use either --engine-config or -f, not both")
	}
	if longPath != "" {
		return longPath, nil
	}
	return shortPath, nil
}

func loadConfigWithFallback(path string, explicit bool) (*Config, error) {
	cfg, err := LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	if explicit || !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return DefaultConfig(), nil
}

func defaultCLIConfigPath() string {
	return filepath.Join(defaultRuntimeDir(), "homoscale.yaml")
}

func usage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale [--engine-config path|-f path] [-c path] [-d|--daemon]
  homoscale start  [--engine-config path|-f path] [-c path] [-d|--daemon]
  homoscale stop   [--engine-config path|-f path] [-c path]
  homoscale auth   [status|login|logout] [--engine-config path|-f path] [-c path]
  homoscale login  [--engine-config path|-f path] [-c path]
  homoscale logout [--engine-config path|-f path] [-c path]
  homoscale status [--engine-config path|-f path] [-c path]
  homoscale mode   [--engine-config path|-f path] [-c path] <rule|global|direct>
  homoscale select [--engine-config path|-f path] [-c path] [group] [proxy]
  homoscale current [--engine-config path|-f path] [-c path]
  homoscale rules  [--engine-config path|-f path] [-c path]
  homoscale version [-json]`)
	return errors.New("unknown command")
}

func authUsage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale auth [status|login|logout] [--engine-config path|-f path] [-c path]`)
	return errors.New("unknown auth command")
}

func modeUsage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale mode [--engine-config path|-f path] [-c path] <rule|global|direct>`)
	return errors.New("invalid mode command")
}

func selectUsage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale select [--engine-config path|-f path] [-c path]
  homoscale select [--engine-config path|-f path] [-c path] <group>
  homoscale select [--engine-config path|-f path] [-c path] <group> <proxy>`)
	return errors.New("invalid select command")
}

func currentUsage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale current [--engine-config path|-f path] [-c path]`)
	return errors.New("invalid current command")
}

func rulesUsage() error {
	fmt.Fprintln(os.Stderr, `usage:
  homoscale rules [--engine-config path|-f path] [-c path]`)
	return errors.New("invalid rules command")
}
