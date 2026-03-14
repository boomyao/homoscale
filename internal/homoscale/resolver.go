//go:build !android

package homoscale

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const resolverManagedMarker = "# managed by homoscale"

var systemResolverDir = "/etc/resolver"

type resolverFileCloser struct {
	path string
}

func (c resolverFileCloser) Close() error {
	if strings.TrimSpace(c.path) == "" {
		return nil
	}
	return removeManagedResolverFile(c.path)
}

func installSystemResolver(cfg *Config, logWriter io.Writer) io.Closer {
	suffix := strings.TrimSpace(tailscaleMagicDNSSuffix(cfg))
	if suffix == "" {
		return nopCloser{}
	}

	path, err := writeManagedResolverFile(systemResolverDir, suffix)
	if err != nil {
		if logWriter != nil {
			_, _ = fmt.Fprintf(logWriter, "warning: system resolver for %s not installed: %v\n", suffix, err)
		}
		return nopCloser{}
	}
	if logWriter != nil {
		_, _ = fmt.Fprintf(logWriter, "installed system resolver for %s at %s\n", suffix, path)
	}
	return resolverFileCloser{path: path}
}

func writeManagedResolverFile(dir, suffix string) (string, error) {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create resolver dir %s: %w", dir, err)
	}

	path := filepath.Join(dir, suffix)
	if data, err := os.ReadFile(path); err == nil {
		if !isManagedResolverFile(data) {
			return "", fmt.Errorf("resolver file already exists and is not managed: %s", path)
		}
	}

	content := managedResolverFileContents(suffix)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write resolver file %s: %w", path, err)
	}
	return path, nil
}

func removeManagedResolverFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read resolver file %s: %w", path, err)
	}
	if !isManagedResolverFile(data) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove resolver file %s: %w", path, err)
	}
	return nil
}

func isManagedResolverFile(data []byte) bool {
	text := strings.TrimSpace(string(data))
	return strings.HasPrefix(text, resolverManagedMarker)
}

func managedResolverFileContents(suffix string) string {
	return strings.Join([]string{
		resolverManagedMarker,
		fmt.Sprintf("# suffix: %s", suffix),
		"nameserver 127.0.0.1",
		"port 53",
		"",
	}, "\n")
}
