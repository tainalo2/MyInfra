package api

import (
	"net"
	"net/url"
	"strings"
)

// reachURL is the address a machine on the same network can call. A local
// copy advertises itself as 127.0.0.1, which only this computer understands.
func (a *API) reachURL() string {
	return reachBase(a.PublicURL, lanIPv4())
}

func reachBase(public, lan string) string {
	parsed, err := url.Parse(public)
	if err != nil || lan == "" || !loopbackHost(parsed.Hostname()) {
		return strings.TrimRight(public, "/")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + lan + ":" + port
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func lanIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var ips []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || skipIface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ips = append(ips, ipnet.IP)
		}
	}
	return pickLAN(ips)
}

func skipIface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{
		"docker", "br-", "veth", "cni", "flannel", "cali", "tun", "tap", "utun",
		"awdl", "llw", "bridge", "virbr", "vboxnet", "vmnet", "wg",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// pickLAN prefers a home network address over a VPN or a container range.
func pickLAN(ips []net.IP) string {
	var priv []net.IP
	for _, ip := range ips {
		ip = ip.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || !ip.IsPrivate() {
			continue
		}
		priv = append(priv, ip)
	}
	if len(priv) == 0 {
		return ""
	}
	best := priv[0]
	for _, ip := range priv[1:] {
		if lanRank(ip) < lanRank(best) {
			best = ip
		}
	}
	return best.String()
}

func lanRank(ip net.IP) int {
	switch {
	case ip[0] == 192 && ip[1] == 168:
		return 0
	case ip[0] == 10:
		return 1
	default:
		return 2
	}
}
