package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

// A local volume is a k3s disk pinned to one node. Leaving that node without
// taking the files along leaves the pods pending, on a path the uninstaller
// then deletes.
type localVolume struct {
	Name       string
	Namespace  string
	Claim      string
	Path       string
	Storage    string
	AccessMode string
	Class      string
}

type workload struct {
	Kind      string
	Namespace string
	Name      string
	Replicas  int
	Claims    []string
}

var (
	kubeName   = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,61}[a-z0-9])?$`)
	volumePath = regexp.MustCompile(`^/var/lib/rancher/k3s/storage/[A-Za-z0-9][A-Za-z0-9._-]{0,200}$`)
	volumeSize = regexp.MustCompile(`^[1-9][0-9]{0,4}Gi$`)
)

// volumesOnNode reads the persistent volumes whose files sit on node.
func volumesOnNode(raw []byte, node string) ([]localVolume, error) {
	var listed struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				StorageClassName string            `json:"storageClassName"`
				Capacity         map[string]string `json:"capacity"`
				AccessModes      []string          `json:"accessModes"`
				Local            *struct {
					Path string `json:"path"`
				} `json:"local"`
				NodeAffinity *struct {
					Required struct {
						NodeSelectorTerms []nodeTerm `json:"nodeSelectorTerms"`
					} `json:"required"`
				} `json:"nodeAffinity"`
				ClaimRef *struct {
					Namespace string `json:"namespace"`
					Name      string `json:"name"`
				} `json:"claimRef"`
			} `json:"spec"`
		} `json:"items"`
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, err
	}
	var out []localVolume
	for _, item := range listed.Items {
		if item.Spec.Local == nil || item.Spec.ClaimRef == nil || item.Spec.NodeAffinity == nil {
			continue
		}
		if !hostnamePinned(item.Spec.NodeAffinity.Required.NodeSelectorTerms, node) {
			continue
		}
		mode := "ReadWriteOnce"
		if len(item.Spec.AccessModes) > 0 {
			mode = item.Spec.AccessModes[0]
		}
		className := item.Spec.StorageClassName
		if className == "" {
			className = "local-path"
		}
		out = append(out, localVolume{
			Name:       item.Metadata.Name,
			Namespace:  item.Spec.ClaimRef.Namespace,
			Claim:      item.Spec.ClaimRef.Name,
			Path:       item.Spec.Local.Path,
			Storage:    item.Spec.Capacity["storage"],
			AccessMode: mode,
			Class:      className,
		})
	}
	return out, nil
}

type nodeTerm struct {
	MatchExpressions []struct {
		Key    string   `json:"key"`
		Values []string `json:"values"`
	} `json:"matchExpressions"`
}

func hostnamePinned(terms []nodeTerm, node string) bool {
	for _, term := range terms {
		for _, expr := range term.MatchExpressions {
			if expr.Key != "kubernetes.io/hostname" {
				continue
			}
			for _, value := range expr.Values {
				if value == node {
					return true
				}
			}
		}
	}
	return false
}

// workloadsOf finds the deployments and statefulsets that mount these volumes.
// It reads the controllers, not the running pods: a move already interrupted
// may have scaled them to zero, and they still have to come back.
func workloadsOf(raw []byte, vols []localVolume) ([]workload, error) {
	claims := map[string]bool{}
	for _, vol := range vols {
		claims[vol.Namespace+"/"+vol.Claim] = true
	}
	var listed struct {
		Items []struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name        string            `json:"name"`
				Namespace   string            `json:"namespace"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Replicas *int `json:"replicas"`
				Template struct {
					Spec struct {
						Volumes []struct {
							PersistentVolumeClaim *struct {
								ClaimName string `json:"claimName"`
							} `json:"persistentVolumeClaim"`
						} `json:"volumes"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []workload
	for _, item := range listed.Items {
		kind := strings.ToLower(item.Kind)
		if kind != "deployment" && kind != "statefulset" {
			continue
		}
		var used []string
		for _, volume := range item.Spec.Template.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			claim := volume.PersistentVolumeClaim.ClaimName
			if claims[item.Metadata.Namespace+"/"+claim] {
				used = append(used, claim)
			}
		}
		if len(used) == 0 || !kubeName.MatchString(item.Metadata.Namespace) || !kubeName.MatchString(item.Metadata.Name) {
			continue
		}
		key := item.Metadata.Namespace + "/" + kind + "/" + item.Metadata.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		replicas := 1
		if item.Spec.Replicas != nil {
			replicas = *item.Spec.Replicas
		}
		if n, err := strconv.Atoi(item.Metadata.Annotations["myinfra.io/leave-replicas"]); err == nil && n > 0 {
			replicas = n
		}
		out = append(out, workload{Kind: kind, Namespace: item.Metadata.Namespace, Name: item.Metadata.Name, Replicas: replicas, Claims: used})
	}
	return out, nil
}

// placedPod is one pod and where it runs.
type placedPod struct {
	Namespace string
	Name      string
	Node      string
	Service   string
	OwnerKind string
	Claim     bool
	Daemon    bool
	Ready     bool
	Done      bool
}

func podsOf(raw []byte) ([]placedPod, error) {
	var listed struct {
		Items []struct {
			Metadata struct {
				Namespace       string            `json:"namespace"`
				Name            string            `json:"name"`
				Labels          map[string]string `json:"labels"`
				OwnerReferences []struct {
					Kind string `json:"kind"`
				} `json:"ownerReferences"`
			} `json:"metadata"`
			Spec struct {
				NodeName string `json:"nodeName"`
				Volumes  []struct {
					PersistentVolumeClaim *struct {
						ClaimName string `json:"claimName"`
					} `json:"persistentVolumeClaim"`
				} `json:"volumes"`
			} `json:"spec"`
			Status struct {
				Phase      string `json:"phase"`
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, err
	}
	var out []placedPod
	for _, item := range listed.Items {
		pod := placedPod{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			Node:      item.Spec.NodeName,
			Service:   item.Metadata.Labels["myinfra.io/service"],
			Done:      item.Status.Phase == "Succeeded" || item.Status.Phase == "Failed",
		}
		if pod.Service == "" {
			pod.Service = item.Metadata.Labels["app"]
		}
		if len(item.Metadata.OwnerReferences) > 0 {
			pod.OwnerKind = item.Metadata.OwnerReferences[0].Kind
			pod.Daemon = pod.OwnerKind == "DaemonSet"
		}
		for _, volume := range item.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				pod.Claim = true
			}
		}
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				pod.Ready = true
			}
		}
		out = append(out, pod)
	}
	return out, nil
}

// statelessOnNode lists the pods that can move by being deleted: the cordon
// makes Kubernetes start them on another machine. Disks are handled apart.
func statelessOnNode(pods []placedPod, node string) []placedPod {
	var out []placedPod
	for _, pod := range pods {
		if pod.Node != node || pod.Daemon || pod.Claim || pod.Done {
			continue
		}
		if pod.OwnerKind != "ReplicaSet" && pod.OwnerKind != "StatefulSet" && pod.OwnerKind != "Deployment" {
			continue
		}
		out = append(out, pod)
	}
	return out
}

// readyOffNode reports whether every named service has a ready pod that is
// not on the leaving node. Leaving is allowed only then.
func readyOffNode(pods []placedPod, node string, services []string) bool {
	for _, service := range services {
		ns, name, ok := strings.Cut(service, "/")
		if !ok {
			return false
		}
		found := false
		for _, pod := range pods {
			if pod.Namespace != ns || !pod.Ready || pod.Node == "" || pod.Node == node {
				continue
			}
			if pod.Service == name || strings.HasPrefix(pod.Name, name+"-") {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// busyNodes lists workload pods still sitting on the node. DaemonSets stay
// until the node object itself is removed.
func busyOnNode(pods []placedPod, node string) bool {
	for _, pod := range pods {
		if pod.Node != node || pod.Daemon || pod.Done {
			continue
		}
		if pod.OwnerKind == "ReplicaSet" || pod.OwnerKind == "StatefulSet" || pod.OwnerKind == "Deployment" {
			return true
		}
	}
	return false
}

// volumeManifest binds the same files to another node.
func volumeManifest(v localVolume, node string) (string, error) {
	if !kubeName.MatchString(v.Name) || !kubeName.MatchString(v.Namespace) || !kubeName.MatchString(v.Claim) || !kubeName.MatchString(node) || !kubeName.MatchString(v.Class) {
		return "", fmt.Errorf("nom de disque refusé")
	}
	if !volumePath.MatchString(v.Path) || !volumeSize.MatchString(v.Storage) {
		return "", fmt.Errorf("chemin de disque refusé")
	}
	if v.AccessMode != "ReadWriteOnce" && v.AccessMode != "ReadWriteMany" {
		return "", fmt.Errorf("mode de disque refusé")
	}
	return fmt.Sprintf(`apiVersion: v1
kind: PersistentVolume
metadata:
  name: %s
spec:
  capacity:
    storage: %s
  accessModes: [%s]
  persistentVolumeReclaimPolicy: Delete
  storageClassName: %s
  local:
    path: %s
  nodeAffinity:
    required:
      nodeSelectorTerms:
      - matchExpressions:
        - key: kubernetes.io/hostname
          operator: In
          values: [%q]
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes: [%s]
  storageClassName: %s
  volumeName: %s
  resources:
    requests:
      storage: %s
`, v.Name, v.Storage, v.AccessMode, v.Class, v.Path, node, v.Claim, v.Namespace, v.AccessMode, v.Class, v.Name, v.Storage), nil
}

// drainNodeCommand empties a node and removes it. It does not force the
// eviction: a pod that cannot run elsewhere stops the departure, instead of
// being deleted with its disk.
func drainNodeCommand(node string) string {
	return fmt.Sprintf("k3s kubectl drain %q --ignore-daemonsets --delete-emptydir-data --timeout=600s && k3s kubectl delete node %q --wait=true --timeout=120s", node, node)
}

// relocateLocalVolumes copies every disk of the leaving node onto another
// machine of the cluster and points the volumes there, before k3s is removed.
func (a *API) relocateLocalVolumes(ctx context.Context, owner string, leaving store.Server, plane deploy.Host) error {
	node := deploy.NodeName(leaving.Name)
	raw, err := deploy.SSH(ctx, plane, "k3s kubectl get pv -o json")
	if err != nil {
		return fmt.Errorf("Les disques du cluster n'ont pas pu être lus.")
	}
	vols, err := volumesOnNode([]byte(raw), node)
	if err != nil {
		return fmt.Errorf("Les disques du cluster n'ont pas pu être lus.")
	}
	if len(vols) == 0 {
		return nil
	}
	target, dest, err := a.volumeTarget(ctx, owner, leaving)
	if err != nil {
		return err
	}
	source, err := a.serverHost(ctx, owner, leaving.ID)
	if err != nil {
		return err
	}
	ctrl, err := deploy.SSH(ctx, plane, "k3s kubectl get deploy,statefulset -A -o json")
	if err != nil {
		return fmt.Errorf("Les services du cluster n'ont pas pu être lus.")
	}
	works, err := workloadsOf([]byte(ctrl), vols)
	if err != nil {
		return err
	}
	a.keepCatalogReplicas(ctx, leaving.ClusterID, works)
	// Each service is stopped only while its own disks move, then it must be
	// ready on the other machine before the node can leave.
	handled := map[string]bool{}
	destNode := deploy.NodeName(target.Name)
	for _, work := range works {
		var mine []localVolume
		for _, vol := range vols {
			for _, claim := range work.Claims {
				if vol.Namespace == work.Namespace && vol.Claim == claim {
					mine = append(mine, vol)
					handled[vol.Namespace+"/"+vol.Claim] = true
				}
			}
		}
		if len(mine) == 0 {
			continue
		}
		if err := a.moveWorkload(ctx, plane, source, dest, destNode, node, work, mine); err != nil {
			return err
		}
	}
	for _, vol := range vols {
		if handled[vol.Namespace+"/"+vol.Claim] {
			continue
		}
		if err := a.moveWorkload(ctx, plane, source, dest, destNode, node, workload{}, []localVolume{vol}); err != nil {
			return err
		}
	}
	return nil
}

func (a *API) keepCatalogReplicas(ctx context.Context, clusterID string, works []workload) {
	apps, err := a.Store.ClusterApps(ctx, clusterID)
	if err != nil {
		return
	}
	want := map[string]int{}
	for _, app := range apps {
		if app.Paused {
			continue
		}
		ns := manifest.Namespace(app.ID)
		for _, svc := range app.Services {
			n := svc.Replicas
			if n < 1 {
				n = 1
			}
			want[ns+"/"+svc.Name] = n
		}
	}
	for i := range works {
		if works[i].Replicas > 0 {
			continue
		}
		if n, ok := want[works[i].Namespace+"/"+works[i].Name]; ok {
			works[i].Replicas = n
		}
	}
}

func (a *API) moveWorkload(ctx context.Context, plane, source, dest deploy.Host, destNode, leaving string, work workload, vols []localVolume) error {
	for _, vol := range vols {
		if !kubeName.MatchString(vol.Namespace) || !kubeName.MatchString(vol.Claim) || !kubeName.MatchString(vol.Name) {
			return fmt.Errorf("nom de disque refusé")
		}
		slog.Info("copie du disque", "claim", vol.Claim, "to", destNode)
	}
	if work.Name != "" && work.Replicas > 0 {
		_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl -n %s annotate %s %s myinfra.io/leave-replicas=%d --overwrite", work.Namespace, work.Kind, work.Name, work.Replicas))
		if _, err := deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl -n %s scale %s/%s --replicas=0", work.Namespace, work.Kind, work.Name)); err != nil {
			return fmt.Errorf("Le service %s n'a pas pu être arrêté.", work.Name)
		}
	}
	restore := func() {
		if work.Name == "" || work.Replicas < 1 {
			return
		}
		_, _ = deploy.SSH(context.Background(), plane, fmt.Sprintf("k3s kubectl -n %s scale %s/%s --replicas=%d", work.Namespace, work.Kind, work.Name, work.Replicas))
	}
	if err := waitClaimsFree(ctx, plane, vols); err != nil {
		restore()
		return err
	}
	for _, vol := range vols {
		if err := copyVolume(ctx, source, dest, vol.Path); err != nil {
			restore()
			return fmt.Errorf("Le disque %s n'a pas pu être copié.", vol.Claim)
		}
		if err := rebindVolume(ctx, plane, vol, destNode); err != nil {
			restore()
			return err
		}
	}
	restore()
	if work.Name == "" || work.Replicas < 1 {
		return nil
	}
	if err := waitReadyOff(ctx, plane, leaving, []string{work.Namespace + "/" + work.Name}); err != nil {
		return fmt.Errorf("Le service %s n'est pas prêt sur une autre machine.", work.Name)
	}
	slog.Info("service revenu", "service", work.Name, "node", destNode)
	return nil
}

func rebindVolume(ctx context.Context, plane deploy.Host, vol localVolume, node string) error {
	_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl patch pv %s -p '{\"spec\":{\"persistentVolumeReclaimPolicy\":\"Retain\"}}'", vol.Name))
	_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl -n %s delete pvc %s --wait=false", vol.Namespace, vol.Claim))
	if err := waitGone(ctx, plane, fmt.Sprintf("k3s kubectl -n %s get pvc %s", vol.Namespace, vol.Claim)); err != nil {
		_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl -n %s patch pvc %s -p '{\"metadata\":{\"finalizers\":null}}'", vol.Namespace, vol.Claim))
	}
	_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl patch pv %s --type json -p '[{\"op\":\"remove\",\"path\":\"/spec/claimRef\"}]'", vol.Name))
	_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl delete pv %s --wait=false", vol.Name))
	if err := waitGone(ctx, plane, fmt.Sprintf("k3s kubectl get pv %s", vol.Name)); err != nil {
		_, _ = deploy.SSH(ctx, plane, fmt.Sprintf("k3s kubectl patch pv %s -p '{\"metadata\":{\"finalizers\":null}}'", vol.Name))
		_ = waitGone(ctx, plane, fmt.Sprintf("k3s kubectl get pv %s", vol.Name))
	}
	manifest, err := volumeManifest(vol, node)
	if err != nil {
		return err
	}
	if _, err := deploy.SSHInput(ctx, plane, "k3s kubectl apply -f -", []byte(manifest)); err != nil {
		return fmt.Errorf("Le disque %s n'a pas pu être repris.", vol.Claim)
	}
	return nil
}

func waitGone(ctx context.Context, plane deploy.Host, command string) error {
	deadline := time.Now().Add(45 * time.Second)
	var last error
	for {
		_, err := deploy.SSH(ctx, plane, command)
		if notFound(err) {
			return nil
		}
		last = err
		if time.Now().After(deadline) {
			if last == nil {
				last = fmt.Errorf("encore présent")
			}
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func waitReadyOff(ctx context.Context, plane deploy.Host, leaving string, services []string) error {
	if len(services) == 0 {
		return nil
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		raw, err := deploy.SSH(ctx, plane, "k3s kubectl get pods -A -o json")
		if err == nil {
			if pods, err := podsOf([]byte(raw)); err == nil && readyOffNode(pods, leaving, services) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pas prêt")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// volumeTarget is another linked machine of the same cluster, the one that
// receives the disks.
func (a *API) volumeTarget(ctx context.Context, owner string, leaving store.Server) (store.Server, deploy.Host, error) {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return store.Server{}, deploy.Host{}, err
	}
	for _, row := range rows {
		if row.ID == leaving.ID || row.ClusterID != leaving.ClusterID || row.Status != "linked" || row.Address == "" {
			continue
		}
		if state, _ := a.reachOf(row.ID); state != "" {
			continue
		}
		host, err := a.serverHost(ctx, owner, row.ID)
		if err != nil {
			continue
		}
		return row, host, nil
	}
	return store.Server{}, deploy.Host{}, fmt.Errorf("Aucune autre machine du cluster ne peut recevoir les disques.")
}

func waitClaimsFree(ctx context.Context, plane deploy.Host, vols []localVolume) error {
	claims := map[string]bool{}
	for _, vol := range vols {
		claims[vol.Namespace+"/"+vol.Claim] = true
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		raw, err := deploy.SSH(ctx, plane, "k3s kubectl get pods -A -o json")
		if err != nil {
			return fmt.Errorf("Les pods du cluster n'ont pas pu être lus.")
		}
		busy, err := claimsHeld(raw, claims)
		if err != nil {
			return fmt.Errorf("Les pods du cluster n'ont pas pu être lus.")
		}
		if !busy {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Un service utilise encore son disque.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func claimsHeld(raw string, claims map[string]bool) (bool, error) {
	var listed struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Volumes []struct {
					PersistentVolumeClaim *struct {
						ClaimName string `json:"claimName"`
					} `json:"persistentVolumeClaim"`
				} `json:"volumes"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &listed); err != nil {
		return false, err
	}
	for _, pod := range listed.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			if claims[pod.Metadata.Namespace+"/"+volume.PersistentVolumeClaim.ClaimName] {
				return true, nil
			}
		}
	}
	return false, nil
}

func notFound(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "notfound") || strings.Contains(text, "not found")
}

const (
	recvScript = `import socket, sys
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((sys.argv[1], int(sys.argv[2])))
s.listen(1)
s.settimeout(180)
c, _ = s.accept()
while True:
    b = c.recv(1024 * 1024)
    if not b:
        break
    sys.stdout.buffer.write(b)
`
	sendScript = `import socket, sys
s = socket.create_connection((sys.argv[1], int(sys.argv[2])), 60)
while True:
    b = sys.stdin.buffer.read(1024 * 1024)
    if not b:
        break
    s.sendall(b)
s.shutdown(socket.SHUT_WR)
`
)

func copyVolume(ctx context.Context, source, dest deploy.Host, path string) error {
	if !volumePath.MatchString(path) {
		return fmt.Errorf("chemin de disque refusé")
	}
	directCtx, cancel := context.WithCancel(ctx)
	err := copyVolumeDirect(directCtx, source, dest, path)
	cancel()
	if err == nil {
		return nil
	}
	slog.Warn("copie directe échouée, repli par MyInfra", "err", err)
	return copyVolumeRelay(ctx, source, dest, path)
}

// copyVolumeDirect sends the files over the cluster network, from one machine
// to the other. They do not travel through MyInfra.
func copyVolumeDirect(ctx context.Context, source, dest deploy.Host, path string) error {
	dstIP, err := wireIP(ctx, dest)
	if err != nil {
		return err
	}
	quoted := "'" + path + "'"
	if _, err := deploy.SSHInput(ctx, dest, "cat > /tmp/myinfra-recv.py", []byte(recvScript)); err != nil {
		return err
	}
	if _, err := deploy.SSHInput(ctx, source, "cat > /tmp/myinfra-send.py", []byte(sendScript)); err != nil {
		return err
	}
	port := 19050
	errc := make(chan error, 1)
	go func() {
		_, err := deploy.SSH(ctx, dest, fmt.Sprintf("rm -rf %s && mkdir -p %s && python3 /tmp/myinfra-recv.py %s %d | tar -C %s -xf -", quoted, quoted, dstIP, port, quoted))
		errc <- err
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	if _, err := deploy.SSH(ctx, source, fmt.Sprintf("tar -C %s -cf - . | python3 /tmp/myinfra-send.py %s %d", quoted, dstIP, port)); err != nil {
		return err
	}
	select {
	case err := <-errc:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	before, err := diskBytes(ctx, source, path)
	if err != nil {
		return err
	}
	after, err := diskBytes(ctx, dest, path)
	if err != nil {
		return err
	}
	if after < before {
		return fmt.Errorf("copie incomplète")
	}
	return nil
}

func wireIP(ctx context.Context, host deploy.Host) (string, error) {
	out, err := deploy.SSH(ctx, host, "ip -4 -o addr show dev myinfra")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	for _, field := range fields {
		ip := strings.Split(field, "/")[0]
		if strings.HasPrefix(ip, "10.66.0.") && !strings.ContainsAny(ip, " '\"") {
			return ip, nil
		}
	}
	return "", fmt.Errorf("pas d'adresse WireGuard")
}

func diskBytes(ctx context.Context, host deploy.Host, path string) (int64, error) {
	out, err := deploy.SSH(ctx, host, "du -sb '"+path+"'")
	if err != nil {
		return 0, err
	}
	field := strings.Fields(out)
	if len(field) == 0 {
		return 0, fmt.Errorf("taille illisible")
	}
	return strconv.ParseInt(field[0], 10, 64)
}

func copyVolumeRelay(ctx context.Context, source, dest deploy.Host, path string) error {
	quoted := "'" + path + "'"
	pr, pw := io.Pipe()
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		err := deploy.SSHPipe(copyCtx, source, "tar -C "+quoted+" -cf - .", pw)
		_ = pw.CloseWithError(err)
		errc <- err
	}()
	err := deploy.SSHPipeStdin(copyCtx, dest, "mkdir -p "+quoted+" && tar -C "+quoted+" -xf -", pr)
	if err != nil {
		cancel()
		_ = pr.CloseWithError(err)
		<-errc
		return err
	}
	return <-errc
}
