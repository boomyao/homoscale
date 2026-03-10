package homoscale

import (
	"context"
	"net/netip"

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
