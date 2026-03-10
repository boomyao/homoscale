package homoscale

import "testing"

func TestStatusFromAuth(t *testing.T) {
	if got := statusFromAuth(nil); got != StateOff {
		t.Fatalf("expected nil auth to be off, got %s", got)
	}
	if got := statusFromAuth(&AuthStatus{LoggedIn: false}); got != StateOff {
		t.Fatalf("expected logged out auth to be off, got %s", got)
	}
	if got := statusFromAuth(&AuthStatus{LoggedIn: true}); got != StateOn {
		t.Fatalf("expected logged in auth to be on, got %s", got)
	}
}

func TestStatusFromEngine(t *testing.T) {
	if got := statusFromEngine(nil); got != StateOff {
		t.Fatalf("expected nil engine to be off, got %s", got)
	}
	if got := statusFromEngine(&EngineStatus{Reachable: false}); got != StateOff {
		t.Fatalf("expected unreachable engine to be off, got %s", got)
	}
	if got := statusFromEngine(&EngineStatus{Reachable: true}); got != StateOn {
		t.Fatalf("expected reachable engine to be on, got %s", got)
	}
}
