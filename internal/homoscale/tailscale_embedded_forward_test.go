package homoscale

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
)

func TestEmbeddedHostTCPForwarderEnabledByDefault(t *testing.T) {
	forwarder := newEmbeddedHostTCPForwarder(DefaultConfig())
	if forwarder == nil {
		t.Fatal("expected embedded host TCP forwarder to be enabled by default")
	}
}

func TestEmbeddedHostTCPForwarderAllowsConfiguredPorts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tailscale.Embedded.ForwardHostTCPPorts = []int{22, 3000}

	forwarder := newEmbeddedHostTCPForwarder(cfg)
	if forwarder == nil {
		t.Fatal("expected embedded host TCP forwarder")
	}
	if _, intercept := forwarder.handle(netip.MustParseAddrPort("100.64.0.1:22")); !intercept {
		t.Fatal("expected port 22 to be forwarded")
	}
	if _, intercept := forwarder.handle(netip.MustParseAddrPort("100.64.0.1:8080")); intercept {
		t.Fatal("expected port 8080 to be rejected")
	}
}

func TestEmbeddedHostTCPForwarderProxiesToLocalhost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		if string(buf) != "ping" {
			return
		}
		_, _ = conn.Write([]byte("pong"))
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	cfg := DefaultConfig()
	cfg.Tailscale.Embedded.ForwardHost = "127.0.0.1"
	cfg.Tailscale.Embedded.ForwardHostTCPPorts = []int{port}

	forwarder := newEmbeddedHostTCPForwarder(cfg)
	handler, intercept := forwarder.handle(netip.MustParseAddrPort(fmt.Sprintf("100.64.0.1:%d", port)))
	if !intercept || handler == nil {
		t.Fatal("expected forwarded handler")
	}

	client, server := net.Pipe()
	defer client.Close()
	go handler(server)

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("write to forwarded conn: %v", err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("read from forwarded conn: %v", err)
	}
	if string(reply) != "pong" {
		t.Fatalf("unexpected forwarded reply: %q", string(reply))
	}
}

func TestEmbeddedHostTCPForwarderCanBeDisabledExplicitly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tailscale.Embedded.ForwardHostTCP = boolPtr(false)

	forwarder := newEmbeddedHostTCPForwarder(cfg)
	if forwarder != nil {
		t.Fatal("expected embedded host TCP forwarder to be disabled explicitly")
	}
}
