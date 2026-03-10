package homoscale

import (
	"context"
	"fmt"
	"io"
)

func StartHomoscale(ctx context.Context, cfg *Config, logWriter io.Writer) error {
	if err := cfg.EnsureRuntimeDirs(); err != nil {
		return err
	}

	tailscaleRuntime, err := StartTailscaleRuntime(ctx, cfg, logWriter)
	if err != nil {
		return err
	}
	if tailscaleRuntime != nil {
		defer tailscaleRuntime.Close()
	}

	return StartEngine(ctx, cfg, logWriter)
}

func StartTailscaleRuntime(ctx context.Context, cfg *Config, logWriter io.Writer) (io.Closer, error) {
	switch cfg.Tailscale.Backend {
	case "embedded":
		return startEmbeddedTailscaleRuntime(ctx, cfg, logWriter)
	case "external":
		return startExternalTailscaleRuntime(ctx, cfg, logWriter)
	default:
		return nil, fmt.Errorf("unsupported tailscale.backend %q", cfg.Tailscale.Backend)
	}
}
