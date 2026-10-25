package join

import (
	"fmt"
	"strings"
)

// Script prepares a machine that runs alone: the SSH key, then a call home
// with the address MyInfra should use. Docker and the proxy come over SSH.
func Script(baseURL, token string, publicKeys []string) (string, error) {
	return callHome(baseURL, token, publicKeys)
}

// NodeScript prepares a future cluster node the same way. Kubernetes and its
// proxy come later over SSH, so nothing may hold ports 80 and 443 before them.
func NodeScript(baseURL, token string, publicKeys []string) (string, error) {
	return callHome(baseURL, token, publicKeys)
}

func callHome(baseURL, token string, publicKeys []string) (string, error) {
	b, err := access(token, publicKeys)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.ContainsAny(base, "'\n ") {
		return "", fmt.Errorf("adresse illisible")
	}
	if base != "" {
		// The source of the route toward the internet is the address of this
		// machine. The address seen by MyInfra is a pod address when the
		// request crosses the cluster proxy.
		fmt.Fprintf(b, `addr=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')
printf '%%s' "$addr" | grep -Eq '^[0-9]{1,3}(\.[0-9]{1,3}){3}$' || addr=
curl -fsS -m 20 -X POST '%s/api/join' -H 'Authorization: Bearer %s' -H 'content-type: application/json' -d "{\"address\":\"$addr\"}"
`, base, token)
	}
	b.WriteString("echo\necho \"Machine déclarée dans MyInfra.\"\n")
	return b.String(), nil
}

// Prepare is what the pasted command does before it calls home: root's
// access by key and MyInfra's keys. MyInfra runs it itself through sudo on a
// machine that only lets another user in at first.
func Prepare(publicKeys []string) (string, error) {
	b, err := access("", publicKeys)
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

// SSHPenalties softens the penalties OpenSSH 9.8 and later gives an address:
// MyInfra opens many sessions, from an address that changes, and a session
// cut during an upgrade of OpenSSH counts as a crash that shuts the address
// out for up to ten minutes. Failed logins are still penalised. The setting
// is kept only when sshd accepts it; older versions have no such option.
const SSHPenalties = `if command -v sshd >/dev/null 2>&1 && sshd -T 2>/dev/null | grep -qi '^persourcepenalties'; then
  mkdir -p /etc/ssh/sshd_config.d
  conf=/etc/ssh/sshd_config.d/01-myinfra-penalties.conf
  printf '%s\n' 'PerSourcePenalties crash:1 noauth:1 grace-exceeded:1 authfail:5 min:60' > "$conf"
  if ! sshd -t >/dev/null 2>&1; then
    printf '%s\n' 'PerSourcePenalties no' > "$conf"
    sshd -t >/dev/null 2>&1 || rm -f "$conf"
  fi
  systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true
fi
`

// KeysFile keeps MyInfra's keys in a file of their own, that sshd reads
// beside the ones it already reads. A provider's agent that writes
// authorized_keys again from its own list — on an upgrade, at boot — would
// otherwise lock MyInfra out. The keys stay in authorized_keys too.
func KeysFile(publicKeys []string) (string, error) {
	var b strings.Builder
	b.WriteString("umask 022\nmkdir -p /etc/ssh/sshd_config.d /root/.ssh\n: > /etc/ssh/myinfra_authorized_keys\n")
	for _, key := range publicKeys {
		line := strings.TrimSpace(key)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "'\n\r") {
			return "", fmt.Errorf("clé publique illisible")
		}
		fmt.Fprintf(&b, "printf '%%s\\n' '%s' >> /etc/ssh/myinfra_authorized_keys\n", line)
		fmt.Fprintf(&b, "touch /root/.ssh/authorized_keys; chmod 600 /root/.ssh/authorized_keys; grep -qxF '%s' /root/.ssh/authorized_keys || printf '%%s\\n' '%s' >> /root/.ssh/authorized_keys\n", line, line)
	}
	b.WriteString(`chmod 644 /etc/ssh/myinfra_authorized_keys
if command -v sshd >/dev/null 2>&1; then
  conf=/etc/ssh/sshd_config.d/02-myinfra-keys.conf
  rm -f "$conf"
  files=$(sshd -T 2>/dev/null | awk '$1 == "authorizedkeysfile" { $1 = ""; print }')
  [ -n "$files" ] || files='.ssh/authorized_keys .ssh/authorized_keys2'
  case " $files " in *" /etc/ssh/myinfra_authorized_keys "*) ;; *) files="$files /etc/ssh/myinfra_authorized_keys" ;; esac
  printf 'AuthorizedKeysFile %s\n' "$(echo $files)" > "$conf"
  sshd -t >/dev/null 2>&1 || rm -f "$conf"
  systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true
fi
`)
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
	b.WriteString(SSHPenalties)
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
	if len(publicKeys) > 0 {
		keys, err := KeysFile(publicKeys)
		if err != nil {
			return nil, err
		}
		b.WriteString(keys)
	}
	b.WriteString("if command -v systemctl >/dev/null 2>&1; then systemctl try-reload-or-restart ssh >/dev/null 2>&1 || systemctl try-reload-or-restart sshd >/dev/null 2>&1 || true; fi\n")
	return b, nil
}

func Command(baseURL, token string) string {
	return fmt.Sprintf("curl -fsSL -H 'Authorization: Bearer %s' %s/join | sudo sh", token, strings.TrimRight(baseURL, "/"))
}
