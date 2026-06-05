package backends

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

var privateRanges []*net.IPNet

func init() {
	privCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"192.0.0.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			privateRanges = append(privateRanges, network)
		}
	}
}

// ValidateBackendURL rejects non-HTTPS URLs pointing to private/link-local addresses.
func ValidateBackendURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid backend URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("backend URL must use http or https scheme")
	}
	hostname := parsed.Hostname()
	if isLocalhost(hostname) {
		return nil
	}
	if parsed.Scheme == "http" {
		return fmt.Errorf("http is only allowed for localhost backends; use https for remote backends")
	}
	ip := net.ParseIP(hostname)
	if ip == nil {
		addrs, err := net.LookupHost(hostname)
		if err != nil || len(addrs) == 0 {
			return nil
		}
		for _, addr := range addrs {
			if ip2 := net.ParseIP(addr); ip2 != nil && isPrivateIP(ip2) {
				return fmt.Errorf("backend URL resolves to a private/reserved IP address")
			}
		}
		return nil
	}
	if isPrivateIP(ip) {
		return fmt.Errorf("backend URL points to a private/reserved IP address")
	}
	return nil
}

func isLocalhost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, network := range privateRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func stripAegisHeaders(header map[string][]string) {
	for key := range header {
		if strings.HasPrefix(strings.ToLower(key), "x-aegis-") {
			delete(header, key)
		}
	}
}
