package deploy

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestStepsStartOnTheMachine(t *testing.T) {
	cloud := Steps(true)
	if cloud[0].ID != "machine" || cloud[0].Status != "running" || cloud[1].Status != "pending" {
		t.Fatalf("%+v", cloud)
	}
	manual := Steps(false)
	if !strings.Contains(manual[0].Detail, "commande de liaison") {
		t.Fatal(manual[0].Detail)
	}
	marked := Mark(cloud, "ssh", "running", "Attente du port SSH.")
	if marked[1].Status != "running" || marked[1].Detail == "" || cloud[1].Status != "pending" {
		t.Fatalf("marked %+v original %+v", marked[1], cloud[1])
	}
}

func TestResumeClearsTheFailedStepAndThoseAfterIt(t *testing.T) {
	steps := Steps(true)
	steps = Mark(steps, "machine", "done", "203.0.113.10")
	steps = Mark(steps, "ssh", "done", "203.0.113.10")
	steps = Mark(steps, "keys", "done", "Dépôt des clés SSH.")
	steps = Mark(steps, "wireguard", "done", "Réseau WireGuard du cluster.")
	steps = Mark(steps, "kubernetes", "failed", "install")
	resumed := Resume(steps)
	if resumed[4].ID != "kubernetes" || resumed[4].Status != "pending" || resumed[4].Detail != "" || resumed[5].Status != "pending" {
		t.Fatalf("%+v", resumed[3:])
	}
	if !Done(resumed, "keys") || !Done(resumed, "wireguard") || Done(resumed, "kubernetes") || steps[4].Status != "failed" {
		t.Fatalf("resumed %+v original %s", resumed[4], steps[4].Status)
	}
}

func TestReplayRestartsConfigurationWhenTheMachineExists(t *testing.T) {
	steps := Steps(true)
	for _, id := range []string{"machine", "ssh", "keys", "kubernetes", "cluster", "wireguard"} {
		steps = Mark(steps, id, "done", "fait")
	}
	replayed := Replay(steps)
	if !Done(replayed, "machine") || Done(replayed, "keys") || Done(replayed, "wireguard") || replayed[1].Detail != "" {
		t.Fatalf("%+v", replayed)
	}
	if steps[2].Status != "done" {
		t.Fatal(steps[2].Status)
	}
}

func TestKubernetesTalksOverWireGuard(t *testing.T) {
	server := playVars(Host{Address: "203.0.113.10", Role: "server", NodeIP: WireGuardIP(1)}, "k3s")
	if server["k3s_extra"] != "--node-ip 10.66.0.1 --flannel-iface myinfra --tls-san 203.0.113.10" {
		t.Fatal(server["k3s_extra"])
	}
	agent := playVars(Host{Address: "203.0.113.11", Role: "agent", NodeIP: WireGuardIP(2)}, "k3s")
	if agent["k3s_extra"] != "--node-ip 10.66.0.2 --flannel-iface myinfra" {
		t.Fatal(agent["k3s_extra"])
	}
	gpu := playVars(Host{Address: "203.0.113.11", Role: "agent", NodeIP: WireGuardIP(2), GPUs: 2}, "k3s")
	if gpu["k3s_extra"] != "--node-ip 10.66.0.2 --flannel-iface myinfra --node-label myinfra.io/gpu=2" || gpu["gpu_count"] != 2 {
		t.Fatal(gpu["k3s_extra"], gpu["gpu_count"])
	}
	if steps := Steps(true); steps[3].ID != "wireguard" || steps[4].ID != "kubernetes" {
		t.Fatalf("%+v", steps)
	}
	if alone := AloneSteps(true); alone[len(alone)-1].ID != "docker" {
		t.Fatalf("%+v", alone)
	}
}

func TestPlayVarsEncodesMissingWireGuardPeersAsArray(t *testing.T) {
	raw, err := json.Marshal(playVars(Host{}, "k3s"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"wireguard_peers":[]`) || !strings.Contains(string(raw), `"ssh_keys":[]`) {
		t.Fatal(string(raw))
	}
}

func TestBriefFailureKeepsTheFatalLine(t *testing.T) {
	text := "ok: [node]\n[INFO] Starting k3s-agent\nlevel=fatal msg=\"listen tcp 127.0.0.1:6444: bind: address already in use\"\nPLAY RECAP\nnode : ok=1 failed=1"
	got := briefFailure(text)
	if !strings.Contains(got, "6444") || strings.Contains(got, "PLAY RECAP") {
		t.Fatal(got)
	}
}

func TestSSHRetriesOnlyTransportErrors(t *testing.T) {
	if !sshTransient(fmt.Errorf("kex_exchange_identification: read: Connection reset by peer")) {
		t.Fatal("reset")
	}
	if sshTransient(fmt.Errorf("cat: /var/lib/rancher/k3s/server/node-token: No such file")) {
		t.Fatal("fichier")
	}
}

func TestEngineDefaultsToK3s(t *testing.T) {
	t.Setenv("MYINFRA_KUBESPRAY", "")
	if Engine() != "k3s" {
		t.Fatal(Engine())
	}
}

func TestReadyCommandRetriesUntilTheDirectoryCanBeCreated(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ansible")
	count := filepath.Join(root, "count")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	mkdir := "#!/bin/sh\nn=0\n[ -f \"$READY_COUNT\" ] && n=$(cat \"$READY_COUNT\")\nn=$((n + 1))\necho \"$n\" > \"$READY_COUNT\"\nif [ \"$n\" -lt 3 ]; then exit 1; fi\nexec /bin/mkdir \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "mkdir"), []byte(mkdir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "cloud-init"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", readyCommand(target))
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "READY_COUNT="+count)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatal(err)
	}
}

func TestPlaybookClosesUnplannedPorts(t *testing.T) {
	if !strings.Contains(playbook, "ansible.builtin.script: firewall.sh") || !strings.Contains(playbook, "tags: [firewall]") || !strings.Contains(playbook, "kubernetes_engine == \"k3s\"") || !strings.Contains(playbook, "until: wireguard_packages is succeeded") {
		t.Fatal("pare-feu absent du playbook")
	}
}

func TestSSHUsesTheGoClient(t *testing.T) {
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	pemKey, err := ssh.MarshalPrivateKey(clientKey, "")
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("refusée")
	}}
	config.AddHostKey(hostSigner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		server, channels, requests, err := ssh.NewServerConn(conn, config)
		if err != nil {
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		for channel := range channels {
			if channel.ChannelType() != "session" {
				_ = channel.Reject(ssh.UnknownChannelType, "")
				continue
			}
			ch, reqs, err := channel.Accept()
			if err != nil {
				continue
			}
			go func() {
				defer ch.Close()
				for req := range reqs {
					if req.Type != "exec" {
						_ = req.Reply(false, nil)
						continue
					}
					_ = req.Reply(true, nil)
					_, _ = ch.Write([]byte("pong\n"))
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					return
				}
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := sshOnce(ctx, Host{Address: listener.Addr().String(), PrivateKey: string(pem.EncodeToMemory(pemKey))}, "true")
	if err != nil || out != "pong" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestInventoryUsesAnIsolatedHostKeyFile(t *testing.T) {
	inventory := inventoryFor("203.0.113.10", "/tmp/key", "/tmp/run/known_hosts")
	if !strings.Contains(inventory, "StrictHostKeyChecking=accept-new") ||
		!strings.Contains(inventory, "IdentitiesOnly=yes") ||
		!strings.Contains(inventory, "UserKnownHostsFile=/tmp/run/known_hosts") ||
		strings.Contains(inventory, "~/.ssh/known_hosts") {
		t.Fatal(inventory)
	}
}

func TestGPUPluginSelectsLabelledNodes(t *testing.T) {
	raw := GPUPlugin()
	for _, part := range []string{"kind: RuntimeClass", "handler: nvidia", "myinfra.io/gpu", "operator: Gt", "nvcr.io/nvidia/k8s-device-plugin:v0.17.1"} {
		if !strings.Contains(raw, part) {
			t.Fatal(part)
		}
	}
}
