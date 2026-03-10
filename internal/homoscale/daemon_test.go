package homoscale

import "testing"

func TestStripDaemonFlags(t *testing.T) {
	args := []string{"-d", "--daemon", "--daemon=true", "-d=true", "-f", "config.yaml", "-c", "homoscale.yaml"}
	got := stripDaemonFlags(args)
	want := []string{"-f", "config.yaml", "-c", "homoscale.yaml"}
	if len(got) != len(want) {
		t.Fatalf("unexpected arg count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected args: got %v want %v", got, want)
		}
	}
}

func TestParseStartArgsSupportsDaemonFlag(t *testing.T) {
	cfg, daemonize, err := parseStartArgs("start", []string{"-d"})
	if err != nil {
		t.Fatalf("parse start args: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if !daemonize {
		t.Fatal("expected daemonize=true")
	}
}
