package join

import (
	"fmt"
	"strings"

	"myinfra/internal/agent"
)

// Script prepares a machine that runs alone: SSH keys, then Docker, the proxy
// and the agent that pulls the applications.
func Script(baseURL, token string, publicKeys []string) (string, error) {
	b, err := access(token, publicKeys)
	if err != nil {
		return "", err
	}
	boot, err := agent.Bootstrap(baseURL, token)
	if err != nil {
		return "", err
	}
	b.WriteString(boot)
	b.WriteString("echo\necho \"Machine déclarée dans MyInfra.\"\n")
	return b.String(), nil
}

// NodeScript prepares a future cluster node: SSH keys only. Kubernetes and its
// proxy come later over SSH, so nothing may hold ports 80 and 443 before them.
func NodeScript(baseURL, token string, publicKeys []string) (string, error) {
	b, err := access(token, publicKeys)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.ContainsAny(base, "'\n ") {
		return "", fmt.Errorf("adresse illisible")
	}
	if base != "" {
		fmt.Fprintf(b, "curl -fsS -m 20 -X POST '%s/api/join' -H 'content-type: application/json' -d '{\"token\":\"%s\"}' >/dev/null 2>&1 || true\n", base, token)
	}
	b.WriteString("echo\necho \"Machine déclarée dans MyInfra.\"\n")
	return b.String(), nil
}

func access(token string, publicKeys []string) (*strings.Builder, error) {
	if strings.ContainsAny(token, "'\n") {
		return nil, fmt.Errorf("jeton de liaison illisible")
	}
	b := &strings.Builder{}
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("chage -d 99999 root || true\n")
	b.WriteString("mkdir -p /etc/ssh/sshd_config.d\n")
	b.WriteString("printf '%s\\n' 'PermitRootLogin prohibit-password' > /etc/ssh/sshd_config.d/00-myinfra-root.conf\n")
	b.WriteString("if [ -f /etc/ssh/sshd_config ]; then sed -i 's/^[# ]*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config || true; fi\n")
	if len(publicKeys) > 0 {
		b.WriteString("umask 077\nmkdir -p /root/.ssh\ntouch /root/.ssh/authorized_keys\n")
		// cloud-init with disable_root (OVH, OpenStack images) puts the key here
		// behind a command that closes the session: sshd would pick that line first.
		b.WriteString("sed -i '/Please login as the user/d' /root/.ssh/authorized_keys || true\n")
		for _, key := range publicKeys {
			line := strings.TrimSpace(key)
			if line == "" || strings.ContainsAny(line, "'\n") {
				return nil, fmt.Errorf("clé publique illisible")
			}
			fmt.Fprintf(b, "grep -qxF '%s' /root/.ssh/authorized_keys || echo '%s' >> /root/.ssh/authorized_keys\n", line, line)
		}
	}
	b.WriteString("if command -v systemctl >/dev/null 2>&1; then systemctl try-reload-or-restart ssh >/dev/null 2>&1 || systemctl try-reload-or-restart sshd >/dev/null 2>&1 || true; fi\n")
	return b, nil
}

func Command(baseURL, token string) string {
	return fmt.Sprintf("curl -fsSL %s/join/%s | sudo sh", strings.TrimRight(baseURL, "/"), token)
}
