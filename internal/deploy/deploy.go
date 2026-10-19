package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"myinfra/internal/firewall"
	"myinfra/internal/tools"

	"golang.org/x/crypto/ssh"

	_ "embed"
)

//go:embed playbook/site.yml
var playbook string

type Step struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func Steps(cloud bool) []Step {
	machine := Step{ID: "machine", Label: "Ouverture de la machine", Status: "pending"}
	if cloud {
		machine.Status = "running"
		machine.Detail = "Commande chez le provider."
	} else {
		machine.Status = "running"
		machine.Detail = "La machine doit exécuter la commande de liaison."
	}
	return []Step{
		machine,
		{ID: "ssh", Label: "Connexion SSH", Status: "pending"},
		{ID: "keys", Label: "Clés SSH", Status: "pending"},
		{ID: "wireguard", Label: "WireGuard du cluster", Status: "pending"},
		{ID: "kubernetes", Label: "Kubernetes", Status: "pending"},
		{ID: "cluster", Label: "Configuration du cluster", Status: "pending"},
	}
}

// AloneSteps are the steps of a machine that runs alone, with Docker.
func AloneSteps(cloud bool) []Step {
	steps := Steps(cloud)
	return []Step{steps[0], steps[1], steps[2], {ID: "docker", Label: "Docker et proxy", Status: "pending"}}
}

// WireGuardIP is the address of a cluster node inside the WireGuard network.
func WireGuardIP(index int) string {
	return fmt.Sprintf("10.66.0.%d", index)
}

func Mark(steps []Step, id, status, detail string) []Step {
	out := append([]Step(nil), steps...)
	for i := range out {
		if out[i].ID != id {
			continue
		}
		out[i].Status = status
		out[i].Detail = detail
	}
	return out
}

func Done(steps []Step, id string) bool {
	for _, step := range steps {
		if step.ID == id {
			return step.Status == "done"
		}
	}
	return false
}

func Replay(steps []Step) []Step {
	if !Done(steps, "machine") {
		return Resume(steps)
	}
	out := append([]Step(nil), steps...)
	for i := range out {
		if out[i].ID == "machine" {
			continue
		}
		out[i].Status = "pending"
		out[i].Detail = ""
	}
	return out
}

func Resume(steps []Step) []Step {
	out := append([]Step(nil), steps...)
	failed := false
	for i := range out {
		if out[i].Status == "failed" {
			failed = true
		}
		if failed && out[i].Status != "done" {
			out[i].Status = "pending"
			out[i].Detail = ""
		}
	}
	return out
}

func Encode(steps []Step) ([]byte, error) {
	return json.Marshal(steps)
}

func Engine() string {
	dir := strings.TrimSpace(os.Getenv("MYINFRA_KUBESPRAY"))
	if dir == "" {
		return "k3s"
	}
	if _, err := os.Stat(filepath.Join(dir, "cluster.yml")); err != nil {
		return "k3s"
	}
	return "kubespray"
}

func WaitSSH(ctx context.Context, host string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(host, "22")
	for {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("La connexion SSH n'a pas abouti.")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("La connexion SSH n'a pas abouti.")
		case <-time.After(3 * time.Second):
		}
	}
}

type Host struct {
	Address       string
	PrivateKey    string
	PublicKeys    []string
	Cluster       string
	NodeName      string
	Role          string
	ServerURL     string
	Token         string
	WGAddress     string
	NodeIP        string
	WGPeers       []WireGuardPeer
	Peers         []string
	ControlPlanes []string
}

type WireGuardPeer struct {
	PublicKey string `json:"public_key"`
	Address   string `json:"address"`
	Endpoint  string `json:"endpoint"`
}

func readyCommand(dir string) string {
	return fmt.Sprintf(`i=0
while [ "$i" -lt 36 ]; do
  if command -v cloud-init >/dev/null 2>&1; then
    cloud-init status --wait >/dev/null 2>&1 || true
  fi
  if mkdir -p %q && test -w %q; then
    exit 0
  fi
  i=$((i + 1))
  sleep 5
done
exit 1
`, dir, dir)
}

func waitReady(ctx context.Context, host Host) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	var last error
	for {
		_, err := sshOnce(ctx, host, readyCommand("/tmp/.ansible"))
		if err == nil {
			return nil
		}
		last = err
		if ctx.Err() != nil || strings.Contains(err.Error(), "clé d'hôte SSH") {
			break
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
			continue
		}
		break
	}
	if last != nil {
		slog.Warn("machine pas prête", "address", host.Address, "err", last)
	}
	if last != nil && (strings.Contains(strings.ToLower(last.Error()), "password") || strings.Contains(last.Error(), "mot de passe")) {
		return fmt.Errorf("Le mot de passe root de la machine a expiré.")
	}
	return fmt.Errorf("La machine n'est pas prête pour l'installation.")
}

func Play(ctx context.Context, host Host, tag string) error {
	if strings.TrimSpace(host.PrivateKey) == "" {
		return fmt.Errorf("Aucune clé privée pour ouvrir la session SSH.")
	}
	if err := waitReady(ctx, host); err != nil {
		return err
	}
	binary := tools.Ansible()
	if binary == "" {
		return fmt.Errorf("Ansible n'est pas installé sur le plan de contrôle.")
	}
	dir, err := os.MkdirTemp("", "myinfra-ansible-")
	if err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	defer os.RemoveAll(dir)
	keyPath := filepath.Join(dir, "id")
	if err := os.WriteFile(keyPath, []byte(host.PrivateKey), 0o600); err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	if err := os.WriteFile(filepath.Join(dir, "site.yml"), []byte(playbook), 0o644); err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	engine := Engine()
	if tag == "firewall" && engine == "k3s" && len(host.Peers) == 0 {
		return fmt.Errorf("Le pare-feu n'a pas la liste des nœuds.")
	}
	script, err := firewall.Script(firewall.Spec{Peers: host.Peers, ControlPlanes: host.ControlPlanes})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "firewall.sh"), []byte(script), 0o755); err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	knownHostsPath := filepath.Join(dir, "known_hosts")
	inventory := inventoryFor(host.Address, keyPath, knownHostsPath)
	if err := os.WriteFile(filepath.Join(dir, "inventory"), []byte(inventory), 0o644); err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	vars := playVars(host, engine)
	raw, err := json.Marshal(vars)
	if err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	varsPath := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(varsPath, raw, 0o644); err != nil {
		return fmt.Errorf("Ansible n'a pas pu préparer la machine.")
	}
	if tag == "kubernetes" && engine == "kubespray" {
		return run(ctx, binary, dir, []string{"-i", filepath.Join(dir, "inventory"), "cluster.yml", "-e", "@" + varsPath}, os.Getenv("MYINFRA_KUBESPRAY"))
	}
	return run(ctx, binary, dir, []string{"-i", filepath.Join(dir, "inventory"), "site.yml", "--tags", tag, "-e", "@" + varsPath}, dir)
}

func playVars(host Host, engine string) map[string]any {
	peers := host.WGPeers
	if peers == nil {
		peers = []WireGuardPeer{}
	}
	keys := host.PublicKeys
	if keys == nil {
		keys = []string{}
	}
	extra := ""
	if host.NodeIP != "" && engine == "k3s" {
		// Kubernetes talks between nodes over WireGuard only, never over the public network.
		extra = "--node-ip " + host.NodeIP + " --flannel-iface myinfra"
		if host.Role == "server" && net.ParseIP(host.Address) != nil {
			extra += " --tls-san " + host.Address
		}
	}
	return map[string]any{
		"k3s_extra":         extra,
		"ssh_keys":          keys,
		"cluster_name":      host.Cluster,
		"kubernetes_engine": engine,
		"node_name":         host.NodeName,
		"node_role":         host.Role,
		"k3s_server_url":    host.ServerURL,
		"k3s_token":         host.Token,
		"wireguard_address": host.WGAddress,
		"wireguard_peers":   peers,
	}
}

// The keepalive ends the session of a machine that vanished, destroyed during
// a play, in a minute: without it Ansible waits for it, holding the deployments.
func inventoryFor(address, keyPath, knownHostsPath string) string {
	return fmt.Sprintf(
		"[node]\n%s ansible_user=root ansible_ssh_private_key_file=%s ansible_ssh_common_args='-o StrictHostKeyChecking=accept-new -o IdentitiesOnly=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=4 -o UserKnownHostsFile=%s'\n",
		address, keyPath, knownHostsPath,
	)
}

func SSH(ctx context.Context, host Host, command string) (string, error) {
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		out, err := sshOnce(ctx, host, command)
		if err == nil {
			return out, nil
		}
		last = err
		if attempt == 5 || !sshTransient(err) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(3 * time.Second):
		}
	}
	return "", last
}

func sshTransient(err error) bool {
	text := strings.ToLower(err.Error())
	for _, hint := range []string{"connection reset", "kex_exchange", "connection timed out", "i/o timeout", "handshake", "banner exchange", "connection refused", "connection closed", "not allowed at this time"} {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

// SSHInput runs a command that reads input on its standard input: a large or
// secret payload stays off the command line, which Linux caps at 128 KiB.
func SSHInput(ctx context.Context, host Host, command string, input []byte) (string, error) {
	var last error
	for attempt := 0; attempt < 6; attempt++ {
		out, err := sshRun(ctx, host, command, input)
		if err == nil {
			return out, nil
		}
		last = err
		if attempt == 5 || !sshTransient(err) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", err
		case <-time.After(3 * time.Second):
		}
	}
	return "", last
}

func sshOnce(ctx context.Context, host Host, command string) (string, error) {
	return sshRun(ctx, host, command, nil)
}

func sshRun(ctx context.Context, host Host, command string, input []byte) (string, error) {
	if strings.TrimSpace(host.PrivateKey) == "" {
		return "", fmt.Errorf("Aucune clé privée pour ouvrir la session SSH.")
	}
	signer, err := ssh.ParsePrivateKey([]byte(host.PrivateKey))
	if err != nil {
		return "", fmt.Errorf("SSH n'a pas pu préparer la connexion.")
	}
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return checkHostKey(host.Address, key)
		},
		Timeout: 10 * time.Second,
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", sshAddr(host.Address))
	if err != nil {
		return "", err
	}
	conn, chans, reqs, err := ssh.NewClientConn(tcp, sshAddr(host.Address), config)
	if err != nil {
		_ = tcp.Close()
		return "", err
	}
	client := ssh.NewClient(conn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if input != nil {
		session.Stdin = bytes.NewReader(input)
	}
	run := make(chan error, 1)
	go func() { run <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	case err = <-run:
	}
	if err != nil {
		text := strings.TrimSpace(stderr.String())
		if len(text) > 400 {
			text = text[len(text)-400:]
		}
		if text == "" {
			text = err.Error()
		}
		if _, ok := err.(*ssh.ExitError); ok {
			return "", &CommandError{Text: text}
		}
		return "", fmt.Errorf("%s", text)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// CommandError is a command that ran on the machine and exited with an error,
// as opposed to a machine that could not be reached.
type CommandError struct {
	Text string
}

func (e *CommandError) Error() string { return e.Text }

func sshAddr(address string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(address, "22")
}

func briefFailure(text string) string {
	var picked []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "PLAY RECAP") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fatal") || strings.Contains(line, "FAILED!") {
			picked = append(picked, line)
		}
	}
	lines := strings.Split(text, "\n")
	var tail []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "PLAY RECAP") || strings.Contains(line, " : ok=") {
			continue
		}
		tail = append(tail, line)
	}
	if len(tail) > 8 {
		tail = tail[len(tail)-8:]
	}
	if len(picked) > 0 {
		text = strings.Join(append(picked, tail...), "\n")
	} else {
		text = strings.Join(tail, "\n")
	}
	if len(text) > 900 {
		text = text[len(text)-900:]
	}
	if strings.TrimSpace(text) == "" {
		return "Le playbook Ansible a échoué."
	}
	return strings.TrimSpace(text)
}

func NodeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "node"
	}
	return out
}

func run(ctx context.Context, binary, dir string, args []string, work string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"ANSIBLE_LOCAL_TEMP="+filepath.Join(dir, "local-tmp"),
		"ANSIBLE_REMOTE_TEMP=/tmp/.ansible",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", briefFailure(string(out)))
	}
	return nil
}
