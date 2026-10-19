package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
)

// The object store of a cluster holds the copies that let an application
// start again on another node: SQLite files of single writers, files of
// applications that keep their data in PostgreSQL.

// storageNodes is the least number of linked servers for three copies on
// three different nodes.
const storageNodes = manifest.StorageCopies

var errStorageNodes = errors.New("Il faut au moins 3 serveurs liés dans le cluster pour garder trois copies des données sur trois serveurs différents.")

var (
	storageMu sync.Mutex
	// storageChecked is when a cluster's store was last found ready.
	storageChecked = map[string]time.Time{}
	// bucketsReady holds the buckets known to exist, per cluster.
	bucketsReady = map[string]bool{}
)

// derive gives a value of its own to each label, from the master key: the
// same cluster or application always gets the same, and nothing is stored.
func (a *API) derive(label string) string {
	mac := hmac.New(sha256.New, a.Master)
	mac.Write([]byte(label))
	return hex.EncodeToString(mac.Sum(nil))
}

func storageBucket(appID string) string {
	return "myinfra-" + strings.ToLower(manifest.Namespace(appID))
}

func (a *API) storageKey(appID string) (string, string) {
	return "GK" + a.derive("garage-key:" + appID)[:24], a.derive("garage-secret:" + appID)
}

// ensureStorage installs the store on a cluster and gives every node a role.
func (a *API) ensureStorage(ctx context.Context, host deploy.Host, owner, clusterID string) error {
	storageMu.Lock()
	checked := storageChecked[clusterID]
	storageMu.Unlock()
	if time.Since(checked) < 2*time.Minute {
		return nil
	}
	nodes, err := a.linkedNodes(ctx, owner, clusterID)
	if err != nil {
		return err
	}
	if nodes < storageNodes {
		return errStorageNodes
	}
	raw := manifest.Storage(a.derive("garage-rpc:" + clusterID))
	key := "storage:" + clusterID
	if !a.appliedRecently(key, digest(raw)) {
		if _, err := deploy.SSHInput(ctx, host, "k3s kubectl apply -f -", []byte(raw)); err != nil {
			return fmt.Errorf("Le stockage du cluster n'a pas pu être installé.")
		}
		_, _ = a.rememberApplied(key, digest(raw))
	}
	if _, err := deploy.SSH(ctx, host, "k3s kubectl -n "+manifest.StorageNamespace+" rollout status ds/garage --timeout=60s"); err != nil {
		return fmt.Errorf("Le stockage du cluster démarre.")
	}
	if err := a.placeStorage(ctx, host); err != nil {
		return err
	}
	// The names of the cluster (its databases, its store) resolve through
	// CoreDNS, which k3s runs once: a second copy keeps them resolving when a
	// node fails. k3s does not set the count, so it stays.
	_, _ = deploy.SSH(ctx, host, "[ \"$(k3s kubectl -n kube-system get deploy coredns -o jsonpath='{.spec.replicas}')\" -ge 2 ] || k3s kubectl -n kube-system scale deploy coredns --replicas=2")
	storageMu.Lock()
	storageChecked[clusterID] = time.Now()
	storageMu.Unlock()
	return nil
}

type garageStatus struct {
	LayoutVersion int `json:"layoutVersion"`
	Nodes         []struct {
		ID       string `json:"id"`
		Addr     string `json:"addr"`
		IsUp     bool   `json:"isUp"`
		LastSeen *int   `json:"lastSeenSecsAgo"`
		Role     *struct {
			Zone string `json:"zone"`
		} `json:"role"`
	} `json:"nodes"`
}

type garageRole struct {
	ID       string   `json:"id"`
	Zone     string   `json:"zone,omitempty"`
	Capacity int64    `json:"capacity,omitempty"`
	Tags     []string `json:"tags"`
	Remove   bool     `json:"remove,omitempty"`
}

// storageCapacity weighs the nodes equally: Garage spreads the data by it.
const storageCapacity = 4_000_000_000

// placeStorage gives a role to every node that runs the store, named after its
// server so that the copies land on different servers, and drops the nodes
// gone for an hour.
func (a *API) placeStorage(ctx context.Context, host deploy.Host) error {
	var status garageStatus
	if err := a.garage(ctx, host, "GetClusterStatus", nil, &status); err != nil {
		return err
	}
	out, err := deploy.SSH(ctx, host, "k3s kubectl -n "+manifest.StorageNamespace+" get pods -l app=garage -o jsonpath='{range .items[*]}{.status.podIP} {.spec.nodeName}{\"\\n\"}{end}'")
	if err != nil {
		return err
	}
	nodeOf := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if ip, node, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			nodeOf[ip] = node
		}
	}
	var changes []garageRole
	placed := 0
	for _, node := range status.Nodes {
		ip := node.Addr
		if i := strings.LastIndex(ip, ":"); i > 0 {
			ip = strings.Trim(ip[:i], "[]")
		}
		switch {
		case node.Role == nil && node.IsUp && nodeOf[ip] != "":
			changes = append(changes, garageRole{ID: node.ID, Zone: nodeOf[ip], Capacity: storageCapacity, Tags: []string{}})
			placed++
		case node.Role != nil && !node.IsUp && node.LastSeen != nil && *node.LastSeen > 3600:
			changes = append(changes, garageRole{ID: node.ID, Remove: true, Tags: []string{}})
		case node.Role != nil && node.IsUp:
			placed++
		}
	}
	if placed < storageNodes {
		return fmt.Errorf("Le stockage du cluster attend ses serveurs.")
	}
	if len(changes) == 0 {
		return nil
	}
	if err := a.garage(ctx, host, "UpdateClusterLayout", map[string]any{"roles": changes}, nil); err != nil {
		return err
	}
	var layout struct {
		Version int `json:"version"`
	}
	if err := a.garage(ctx, host, "GetClusterLayout", nil, &layout); err != nil {
		return err
	}
	return a.garage(ctx, host, "ApplyClusterLayout", map[string]int{"version": layout.Version + 1}, nil)
}

// ensureBucket gives an application its bucket and its key.
func (a *API) ensureBucket(ctx context.Context, host deploy.Host, clusterID, appID string) (string, string, string, error) {
	bucket := storageBucket(appID)
	keyID, secret := a.storageKey(appID)
	storageMu.Lock()
	known := bucketsReady[clusterID+"/"+bucket]
	storageMu.Unlock()
	if known {
		return bucket, keyID, secret, nil
	}
	var info struct {
		ID string `json:"id"`
	}
	if err := a.garage(ctx, host, "GetBucketInfo", map[string]string{"globalAlias": bucket}, &info); err != nil || info.ID == "" {
		if err := a.garage(ctx, host, "CreateBucket", map[string]string{"globalAlias": bucket}, &info); err != nil {
			return "", "", "", fmt.Errorf("Le stockage de l'application n'a pas pu être créé.")
		}
	}
	if err := a.garage(ctx, host, "GetKeyInfo", map[string]string{"id": keyID}, nil); err != nil {
		if err := a.garage(ctx, host, "ImportKey", map[string]string{"accessKeyId": keyID, "secretAccessKey": secret, "name": bucket}, nil); err != nil {
			return "", "", "", fmt.Errorf("La clé du stockage n'a pas pu être créée.")
		}
	}
	allow := map[string]any{"bucketId": info.ID, "accessKeyId": keyID, "permissions": map[string]bool{"read": true, "write": true, "owner": true}}
	if err := a.garage(ctx, host, "AllowBucketKey", allow, nil); err != nil {
		return "", "", "", fmt.Errorf("La clé du stockage n'a pas pu être autorisée.")
	}
	storageMu.Lock()
	bucketsReady[clusterID+"/"+bucket] = true
	storageMu.Unlock()
	return bucket, keyID, secret, nil
}

// bucketObjects counts what a bucket holds.
func (a *API) bucketObjects(ctx context.Context, host deploy.Host, bucket string) (int, error) {
	var info struct {
		Objects int `json:"objects"`
	}
	if err := a.garage(ctx, host, "GetBucketInfo", map[string]string{"globalAlias": bucket}, &info); err != nil {
		return 0, err
	}
	return info.Objects, nil
}

// garage calls the store's administration through its command line, on one
// of its nodes. The payload goes on the standard input.
func (a *API) garage(ctx context.Context, host deploy.Host, endpoint string, payload, out any) error {
	body := []byte("null")
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = raw
	}
	text, err := deploy.SSHInput(ctx, host, "k3s kubectl -n "+manifest.StorageNamespace+" exec -i ds/garage -- /garage json-api "+endpoint+" -", body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(text), out)
}
