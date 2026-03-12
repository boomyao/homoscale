package homoscale

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

type embeddedHostTCPForwarder struct {
	targetHost   string
	allowedPorts map[uint16]struct{}
}

func newEmbeddedHostTCPForwarder(cfg *Config) *embeddedHostTCPForwarder {
	if cfg == nil || cfg.Tailscale.Embedded.ForwardHostTCP == nil || !*cfg.Tailscale.Embedded.ForwardHostTCP {
		return nil
	}

	targetHost := strings.TrimSpace(cfg.Tailscale.Embedded.ForwardHost)
	if targetHost == "" {
		targetHost = "127.0.0.1"
	}

	var allowedPorts map[uint16]struct{}
	if len(cfg.Tailscale.Embedded.ForwardHostTCPPorts) > 0 {
		allowedPorts = make(map[uint16]struct{}, len(cfg.Tailscale.Embedded.ForwardHostTCPPorts))
		for _, port := range cfg.Tailscale.Embedded.ForwardHostTCPPorts {
			if port <= 0 || port > 65535 {
				continue
			}
			allowedPorts[uint16(port)] = struct{}{}
		}
	}

	return &embeddedHostTCPForwarder{
		targetHost:   targetHost,
		allowedPorts: allowedPorts,
	}
}

func (f *embeddedHostTCPForwarder) register(server *tsnet.Server) func() {
	if f == nil || server == nil {
		return nil
	}
	return server.RegisterFallbackTCPHandler(func(src, dst netip.AddrPort) (func(net.Conn), bool) {
		_ = src
		return f.handle(dst)
	})
}

func (f *embeddedHostTCPForwarder) handle(dst netip.AddrPort) (func(net.Conn), bool) {
	if f == nil || !f.allowsPort(dst.Port()) {
		return nil, false
	}

	target := net.JoinHostPort(f.targetHost, strconv.Itoa(int(dst.Port())))
	return func(inbound net.Conn) {
		defer inbound.Close()

		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		upstream, err := dialer.DialContext(context.Background(), "tcp", target)
		if err != nil {
			return
		}
		defer upstream.Close()

		bridgeTCPConns(inbound, upstream)
	}, true
}

func (f *embeddedHostTCPForwarder) allowsPort(port uint16) bool {
	if f == nil {
		return false
	}
	if len(f.allowedPorts) == 0 {
		return true
	}
	_, ok := f.allowedPorts[port]
	return ok
}

func bridgeTCPConns(left, right net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go proxyTCPHalf(&wg, left, right)
	go proxyTCPHalf(&wg, right, left)

	wg.Wait()
}

func proxyTCPHalf(wg *sync.WaitGroup, dst, src net.Conn) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	if closer, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}
	if closer, ok := src.(interface{ CloseRead() error }); ok {
		_ = closer.CloseRead()
	}
}
