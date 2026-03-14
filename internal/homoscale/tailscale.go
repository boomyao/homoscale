package homoscale

import (
	"context"
	"net/netip"
	"slices"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
)

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

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
			return normalizeDNSName(status.CurrentTailnet.MagicDNSSuffix)
		}
		if status.MagicDNSSuffix != "" {
			return normalizeDNSName(status.MagicDNSSuffix)
		}
	}
	return normalizeDNSName(fallback)
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
	name := peerDNSName(peer)
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

func peerDNSName(peer *ipnstate.PeerStatus) string {
	if peer == nil {
		return ""
	}
	return normalizeDNSName(peer.DNSName)
}

func magicDNSShortName(host, magicSuffix string) string {
	host = normalizeDNSName(host)
	magicSuffix = normalizeDNSName(magicSuffix)
	if host == "" || magicSuffix == "" || host == magicSuffix {
		return ""
	}
	suffix := "." + magicSuffix
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	label := strings.TrimSuffix(host, suffix)
	label = strings.TrimSuffix(label, ".")
	if label == "" {
		return ""
	}
	return label
}

func magicDNSShortHostAliases(hosts map[string]string, magicSuffix string) map[string]string {
	if len(hosts) == 0 {
		return nil
	}
	counts := map[string]int{}
	for host := range hosts {
		if short := magicDNSShortName(host, magicSuffix); short != "" {
			counts[short]++
		}
	}
	out := map[string]string{}
	for host, ip := range hosts {
		alias := magicDNSShortName(host, magicSuffix)
		if alias == "" || alias == normalizeDNSName(host) || counts[alias] != 1 {
			continue
		}
		if _, exists := hosts[alias]; exists {
			continue
		}
		if _, exists := out[alias]; exists {
			continue
		}
		out[alias] = strings.TrimSpace(ip)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func accessDomainsForHost(host, magicSuffix string) []string {
	host = normalizeDNSName(host)
	if host == "" {
		return nil
	}
	out := []string{host}
	if alias := magicDNSShortName(host, magicSuffix); alias != "" && alias != host {
		out = append(out, alias)
	}
	return out
}
