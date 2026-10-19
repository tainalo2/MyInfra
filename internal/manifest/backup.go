package manifest

import (
	"fmt"
	"path"
	"strings"
)

const BackupImage = "litestream/litestream:0.3.13"

// failover is how long Kubernetes waits for a node that stopped answering
// before starting its pods elsewhere. The default, five minutes, is long for
// an application that has only one copy.
const failover = 30

// writeBackedUp renders a single writer whose SQLite files are replicated to
// the cluster's object store. Its one copy keeps no node disk: it restores the
// files at start, so it can start on any node. While Copying, it still runs on
// its node disk and only sends the files, for the first copy.
func writeBackedUp(b *strings.Builder, ns, appID string, svc Service, replicas int, refs []string) {
	if replicas > 1 {
		replicas = 1
	}
	name := "backup-" + svc.Name
	fmt.Fprintf(b, "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\ntype: Opaque\nstringData:\n  LITESTREAM_ACCESS_KEY_ID: %q\n  LITESTREAM_SECRET_ACCESS_KEY: %q\n",
		name, ns, svc.Backup.KeyID, svc.Backup.Secret)
	fmt.Fprintf(b, "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n  litestream.yml: |\n    dbs:\n", name, ns)
	for _, db := range svc.Backup.Databases {
		fmt.Fprintf(b, `      - path: %s
        replicas:
          - type: s3
            bucket: %s
            path: %s/%s
            endpoint: %s
            region: %s
            force-path-style: true
            sync-interval: 1s
`, db, svc.Backup.Bucket, svc.Name, path.Base(db), StorageEndpoint, StorageRegion)
	}
	state := "ready"
	if svc.Backup.Copying {
		state = "copying"
		fmt.Fprintf(b, "---\napiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  accessModes: [ReadWriteOnce]\n  resources:\n    requests:\n      storage: %dGi\n", svc.Name, ns, diskGiOf(svc))
	}
	fmt.Fprintf(b, `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    myinfra.io/managed: "true"
    myinfra.io/app: %q
spec:
  replicas: %d
  strategy:
    type: Recreate
  selector:
    matchLabels:
      myinfra.io/service: %s
  template:
    metadata:
      labels:
        myinfra.io/service: %s
      annotations:
        myinfra.io/backup: %s
    spec:
      tolerations:
        - key: node.kubernetes.io/unreachable
          operator: Exists
          effect: NoExecute
          tolerationSeconds: %d
        - key: node.kubernetes.io/not-ready
          operator: Exists
          effect: NoExecute
          tolerationSeconds: %d
`, svc.Name, ns, appID, replicas, svc.Name, svc.Name, state, failover, failover)
	mounts := fmt.Sprintf("          volumeMounts:\n            - name: data\n              mountPath: %s\n            - name: backup\n              mountPath: /etc/litestream.yml\n              subPath: litestream.yml\n", diskPathOf(svc))
	if !svc.Backup.Copying {
		b.WriteString("      initContainers:\n")
		for i, db := range svc.Backup.Databases {
			// Restores only when the disk is empty and a copy exists: a
			// first start begins with no data, as it would without backup.
			fmt.Fprintf(b, "        - name: restore-%d\n          image: %s\n          args: [restore, -if-db-not-exists, -if-replica-exists, -config, /etc/litestream.yml, %q]\n          envFrom:\n            - secretRef:\n                name: %s\n%s",
				i, BackupImage, db, name, mounts)
		}
	}
	fmt.Fprintf(b, "      containers:\n        - name: %s\n          image: %s\n          ports:\n            - containerPort: %d\n", svc.Name, svc.Image, listenOf(svc))
	plain := svc
	plain.Disk = false
	writeContainerTail(b, refs, plain)
	if len(svc.Backup.Env) > 0 {
		b.WriteString("          env:\n")
		for _, key := range sortedKeys(svc.Backup.Env) {
			fmt.Fprintf(b, "            - name: %s\n              value: %q\n", key, svc.Backup.Env[key])
		}
	}
	fmt.Fprintf(b, "          volumeMounts:\n            - name: data\n              mountPath: %s\n", diskPathOf(svc))
	fmt.Fprintf(b, "        - name: backup\n          image: %s\n          args: [replicate, -config, /etc/litestream.yml]\n          envFrom:\n            - secretRef:\n                name: %s\n%s", BackupImage, name, mounts)
	b.WriteString("      volumes:\n")
	if svc.Backup.Copying {
		fmt.Fprintf(b, "        - name: data\n          persistentVolumeClaim:\n            claimName: %s\n", svc.Name)
	} else {
		b.WriteString("        - name: data\n          emptyDir: {}\n")
	}
	fmt.Fprintf(b, "        - name: backup\n          configMap:\n            name: %s\n", name)
}
