package homoscale

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteManagedResolverFile(t *testing.T) {
	dir := t.TempDir()

	path, err := writeManagedResolverFile(dir, "betta-lungfish.ts.net")
	if err != nil {
		t.Fatalf("write managed resolver file: %v", err)
	}
	if path != filepath.Join(dir, "betta-lungfish.ts.net") {
		t.Fatalf("unexpected resolver path: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resolver file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, resolverManagedMarker) {
		t.Fatalf("missing managed marker:\n%s", text)
	}
	if !strings.Contains(text, "nameserver 127.0.0.1") {
		t.Fatalf("missing nameserver:\n%s", text)
	}
	if !strings.Contains(text, "port 53") {
		t.Fatalf("missing port:\n%s", text)
	}
}

func TestWriteManagedResolverFileDoesNotOverwriteUnmanagedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "betta-lungfish.ts.net")
	if err := os.WriteFile(path, []byte("nameserver 1.1.1.1\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged resolver file: %v", err)
	}

	if _, err := writeManagedResolverFile(dir, "betta-lungfish.ts.net"); err == nil {
		t.Fatal("expected unmanaged resolver file conflict")
	}
}

func TestRemoveManagedResolverFile(t *testing.T) {
	dir := t.TempDir()
	path, err := writeManagedResolverFile(dir, "betta-lungfish.ts.net")
	if err != nil {
		t.Fatalf("write managed resolver file: %v", err)
	}

	if err := removeManagedResolverFile(path); err != nil {
		t.Fatalf("remove managed resolver file: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected resolver file to be removed, stat err=%v", err)
	}
}

func TestRemoveManagedResolverFilePreservesUnmanagedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "betta-lungfish.ts.net")
	if err := os.WriteFile(path, []byte("nameserver 1.1.1.1\n"), 0o644); err != nil {
		t.Fatalf("write unmanaged resolver file: %v", err)
	}

	if err := removeManagedResolverFile(path); err != nil {
		t.Fatalf("remove resolver file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected unmanaged resolver file to remain: %v", err)
	}
}
