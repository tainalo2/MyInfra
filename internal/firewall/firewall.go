package firewall

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Spec décrit les machines autorisées à joindre les ports du cluster.
// Une liste vide ferme ces ports : c'est le cas d'une machine seule, sans Kubernetes.
type Spec struct {
	Peers         []string
	ControlPlanes []string
	// Open are the ports applications publish to the Internet.
	Open []Port
}

// Port is a port of the machine, tcp or udp.
type Port struct {
	Port     int
	Protocol string
}

// Script installe un pare-feu qui ne laisse entrer depuis Internet
// que SSH, HTTP, HTTPS et les ports que publient les applications.
// Les ports du cluster ne répondent qu'aux autres nœuds.
func Script(spec Spec) (string, error) {
	open := map[string][]int{}
	seen := map[Port]bool{}
	for _, port := range spec.Open {
		if port.Port < 1 || port.Port > 65535 || (port.Protocol != "tcp" && port.Protocol != "udp") {
			return "", fmt.Errorf("port illisible")
		}
		if !seen[port] {
			seen[port] = true
			open[port.Protocol] = append(open[port.Protocol], port.Port)
		}
	}
	planes, err := ipv4s(spec.ControlPlanes)
	if err != nil {
		return "", err
	}
	peers, err := ipv4s(append(append([]string{}, spec.Peers...), spec.ControlPlanes...))
	if err != nil {
		return "", err
	}
	var nft strings.Builder
	nft.WriteString("add table inet myinfra\n")
	nft.WriteString("delete table inet myinfra\n")
	nft.WriteString("table inet myinfra {\n")
	if len(peers) > 0 {
		fmt.Fprintf(&nft, "  set cluster {\n    type ipv4_addr\n    elements = { %s }\n  }\n", strings.Join(peers, ", "))
	}
	if len(planes) > 0 {
		fmt.Fprintf(&nft, "  set control {\n    type ipv4_addr\n    elements = { %s }\n  }\n", strings.Join(planes, ", "))
	}
	nft.WriteString(`  chain input {
    type filter hook input priority filter + 10; policy drop;
    iif "lo" accept
    ct state established,related accept
    ct state invalid drop
    ip protocol icmp icmp type { destination-unreachable, time-exceeded, parameter-problem, echo-request } accept
    ip6 nexthdr ipv6-icmp icmpv6 type { destination-unreachable, packet-too-big, time-exceeded, parameter-problem, echo-request, nd-router-solicit, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert, mld-listener-query, mld-listener-report, mld-listener-done } accept
    udp sport 67 udp dport 68 accept
    tcp dport { 22, 80, 443 } accept
`)
	for _, protocol := range []string{"tcp", "udp"} {
		if ports := open[protocol]; len(ports) > 0 {
			sort.Ints(ports)
			words := make([]string, len(ports))
			for i, port := range ports {
				words[i] = fmt.Sprint(port)
			}
			fmt.Fprintf(&nft, "    %s dport { %s } accept\n", protocol, strings.Join(words, ", "))
		}
	}
	if len(peers) > 0 {
		nft.WriteString("    ip saddr @cluster tcp dport { 6443, 10250 } accept\n")
		nft.WriteString("    ip saddr @cluster udp dport { 8472, 51820 } accept\n")
		nft.WriteString("    ip saddr { 10.42.0.0/16, 10.43.0.0/16 } iifname { \"cni0\", \"flannel.1\", \"flannel-wg\", \"flannel.0\" } tcp dport { 6443, 10250 } accept\n")
		nft.WriteString("    ip saddr 10.42.0.0/16 iifname \"veth*\" tcp dport { 6443, 10250 } accept\n")
		nft.WriteString("    iifname \"myinfra\" ip saddr 10.66.0.0/24 accept\n")
	}
	if len(planes) > 0 {
		nft.WriteString("    ip saddr @control tcp dport { 2379, 2380 } accept\n")
	}
	nft.WriteString("  }\n}\n")

	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("export PATH=\"/usr/sbin:/sbin:/usr/bin:/bin:$PATH\"\n")
	b.WriteString("export DEBIAN_FRONTEND=noninteractive\n")
	b.WriteString("if [ ! -x /usr/sbin/nft ]; then\n")
	b.WriteString("  i=0\n")
	b.WriteString("  while [ \"$i\" -lt 30 ]; do\n")
	b.WriteString("    if apt-get update -qq && apt-get install -y nftables; then break; fi\n")
	b.WriteString("    i=$((i + 1))\n")
	b.WriteString("    sleep 10\n")
	b.WriteString("  done\n")
	b.WriteString("fi\n")
	b.WriteString("if [ ! -x /usr/sbin/nft ]; then\n")
	b.WriteString("  echo \"nftables n'a pas pu être installé.\"\n")
	b.WriteString("  exit 1\n")
	b.WriteString("fi\n")
	b.WriteString("systemctl disable nftables.service >/dev/null 2>&1 || true\n")
	// Certaines images (Vultr, par exemple) livrent UFW actif avec le seul port 22 :
	// il bloquerait 6443 avant nos règles. Le pare-feu nft de MyInfra le remplace.
	b.WriteString("if command -v ufw >/dev/null 2>&1; then ufw --force disable >/dev/null 2>&1 || true; fi\n")
	b.WriteString("systemctl disable --now ufw.service firewalld.service >/dev/null 2>&1 || true\n")
	b.WriteString("mkdir -p /etc/myinfra /etc/systemd/resolved.conf.d /etc/ssh/sshd_config.d /etc/sysctl.d /etc/systemd/system\n")
	b.WriteString("cat > /etc/systemd/resolved.conf.d/myinfra.conf.new <<'EOF'\n[Resolve]\nLLMNR=no\nMulticastDNS=no\nEOF\n")
	b.WriteString("if ! cmp -s /etc/systemd/resolved.conf.d/myinfra.conf.new /etc/systemd/resolved.conf.d/myinfra.conf 2>/dev/null; then\n")
	b.WriteString("  mv /etc/systemd/resolved.conf.d/myinfra.conf.new /etc/systemd/resolved.conf.d/myinfra.conf\n")
	b.WriteString("  systemctl try-restart systemd-resolved >/dev/null 2>&1 || true\n")
	b.WriteString("else\n  rm -f /etc/systemd/resolved.conf.d/myinfra.conf.new\nfi\n")
	b.WriteString("cat > /etc/ssh/sshd_config.d/myinfra.conf.new <<'EOF'\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nChallengeResponseAuthentication no\nPermitRootLogin prohibit-password\nEOF\n")
	b.WriteString("if ! cmp -s /etc/ssh/sshd_config.d/myinfra.conf.new /etc/ssh/sshd_config.d/myinfra.conf 2>/dev/null; then\n")
	b.WriteString("  if [ -f /etc/ssh/sshd_config.d/myinfra.conf ]; then cp /etc/ssh/sshd_config.d/myinfra.conf /etc/ssh/sshd_config.d/myinfra.conf.bak; fi\n")
	b.WriteString("  mv /etc/ssh/sshd_config.d/myinfra.conf.new /etc/ssh/sshd_config.d/myinfra.conf\n")
	b.WriteString("  if sshd -t >/dev/null 2>&1; then\n")
	b.WriteString("    systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true\n")
	b.WriteString("  elif [ -f /etc/ssh/sshd_config.d/myinfra.conf.bak ]; then\n")
	b.WriteString("    mv /etc/ssh/sshd_config.d/myinfra.conf.bak /etc/ssh/sshd_config.d/myinfra.conf\n")
	b.WriteString("  else\n")
	b.WriteString("    rm -f /etc/ssh/sshd_config.d/myinfra.conf\n")
	b.WriteString("  fi\n")
	b.WriteString("else\n  rm -f /etc/ssh/sshd_config.d/myinfra.conf.new\nfi\n")
	b.WriteString("cat > /etc/sysctl.d/myinfra.conf <<'EOF'\n")
	b.WriteString("net.ipv4.conf.all.accept_redirects=0\n")
	b.WriteString("net.ipv4.conf.default.accept_redirects=0\n")
	b.WriteString("net.ipv4.conf.all.send_redirects=0\n")
	b.WriteString("net.ipv4.conf.all.accept_source_route=0\n")
	b.WriteString("net.ipv4.conf.default.accept_source_route=0\n")
	b.WriteString("net.ipv6.conf.all.accept_redirects=0\n")
	b.WriteString("net.ipv6.conf.default.accept_redirects=0\n")
	b.WriteString("net.ipv4.icmp_echo_ignore_broadcasts=1\n")
	b.WriteString("net.ipv4.conf.all.rp_filter=0\n")
	b.WriteString("net.ipv4.conf.default.rp_filter=0\n")
	b.WriteString("EOF\n")
	b.WriteString("sysctl -p /etc/sysctl.d/myinfra.conf >/dev/null\n")
	b.WriteString("for path in /sys/class/net/*; do\n")
	b.WriteString("  if [ -e \"$path/device\" ]; then\n")
	b.WriteString("    sysctl -w \"net.ipv4.conf.${path##*/}.rp_filter=1\" >/dev/null || true\n")
	b.WriteString("  fi\ndone\n")
	b.WriteString("cat > /etc/myinfra/firewall.nft <<'EOF'\n")
	b.WriteString(nft.String())
	b.WriteString("EOF\n")
	b.WriteString("nft -c -f /etc/myinfra/firewall.nft\n")
	b.WriteString("nft -f /etc/myinfra/firewall.nft\n")
	b.WriteString("cat > /etc/myinfra/firewall-boot.sh <<'EOF'\n")
	b.WriteString("#!/bin/sh\nset -eu\nexport PATH=\"/usr/sbin:/sbin:/usr/bin:/bin:$PATH\"\n")
	b.WriteString("sysctl -p /etc/sysctl.d/myinfra.conf >/dev/null || true\n")
	b.WriteString("for path in /sys/class/net/*; do\n")
	b.WriteString("  if [ -e \"$path/device\" ]; then\n")
	b.WriteString("    sysctl -w \"net.ipv4.conf.${path##*/}.rp_filter=1\" >/dev/null 2>&1 || true\n")
	b.WriteString("  fi\ndone\n")
	b.WriteString("nft -f /etc/myinfra/firewall.nft\n")
	b.WriteString("EOF\n")
	b.WriteString("chmod 755 /etc/myinfra/firewall-boot.sh\n")
	b.WriteString("cat > /etc/systemd/system/myinfra-firewall.service <<'EOF'\n")
	b.WriteString("[Unit]\nDescription=Pare-feu MyInfra\nAfter=network-online.target\nWants=network-online.target\nBefore=k3s.service k3s-agent.service docker.service\n\n")
	b.WriteString("[Service]\nType=oneshot\nExecStart=/etc/myinfra/firewall-boot.sh\nRemainAfterExit=yes\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\nEOF\n")
	b.WriteString("systemctl daemon-reload\n")
	b.WriteString("systemctl enable myinfra-firewall.service >/dev/null 2>&1 || true\n")
	return b.String(), nil
}

func ipv4s(list []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ip := net.ParseIP(raw)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("adresse illisible")
		}
		text := ip.To4().String()
		if seen[text] {
			continue
		}
		seen[text] = true
		out = append(out, text)
	}
	sort.Strings(out)
	return out, nil
}
