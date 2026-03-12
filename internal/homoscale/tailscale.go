package homoscale

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
)

func tailscaleLocalStatus(cfg *Config) (*ipnstate.Status, error) {
	lc := &local.Client{
		Socket:        cfg.Tailscale.Socket,
		UseSocketOnly: true,
	}
	return lc.StatusWithoutPeers(context.Background())
}

func tailscaleLocalStatusWithPeers(cfg *Config) (*ipnstate.Status, error) {
	lc := &local.Client{
		Socket:        cfg.Tailscale.Socket,
		UseSocketOnly: true,
	}
	return lc.Status(context.Background())
}

func magicDNSSuffixFromStatus(status *ipnstate.Status, fallback string) string {
	if status != nil {
		if status.CurrentTailnet != nil && status.CurrentTailnet.MagicDNSSuffix != "" {
			return status.CurrentTailnet.MagicDNSSuffix
		}
		if status.MagicDNSSuffix != "" {
			return status.MagicDNSSuffix
		}
	}
	return fallback
}

func magicDNSEnabledFromStatus(status *ipnstate.Status) bool {
	return status != nil && status.CurrentTailnet != nil && status.CurrentTailnet.MagicDNSEnabled
}

func netipStrings(items []netip.Addr) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.String())
	}
	return out
}

func magicDNSHostsFromStatus(status *ipnstate.Status) map[string]string {
	if status == nil {
		return nil
	}

	hosts := map[string]string{}
	addMagicDNSHost(hosts, status.Self)
	for _, peerKey := range status.Peers() {
		addMagicDNSHost(hosts, status.Peer[peerKey])
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

func addMagicDNSHost(hosts map[string]string, peer *ipnstate.PeerStatus) {
	if hosts == nil || peer == nil {
		return
	}
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(peer.DNSName), "."))
	if name == "" {
		return
	}
	ip, ok := preferredTailscaleIP(peer.TailscaleIPs)
	if !ok {
		return
	}
	hosts[name] = ip.String()
}

func preferredTailscaleIP(addrs []netip.Addr) (netip.Addr, bool) {
	if len(addrs) == 0 {
		return netip.Addr{}, false
	}
	items := slices.Clone(addrs)
	slices.SortFunc(items, func(a, b netip.Addr) int {
		if a.Is4() && !b.Is4() {
			return -1
		}
		if !a.Is4() && b.Is4() {
			return 1
		}
		return a.Compare(b)
	})
	for _, addr := range items {
		if addr.IsValid() {
			return addr, true
		}
	}
	return netip.Addr{}, false
}
