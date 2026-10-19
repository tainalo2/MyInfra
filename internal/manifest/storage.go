package manifest

import "fmt"

// Garage is an object store built for nodes far apart. MyInfra runs one on
// every node of a cluster: the backups of the applications live there, three
// copies on three different nodes, and survive the loss of any one of them.

const (
	StorageNamespace = "myinfra-storage"
	StorageImage     = "dxflrs/garage:v2.4.1"
	// StorageEndpoint is how the applications of the cluster reach it.
	StorageEndpoint = "http://garage.myinfra-storage.svc.cluster.local:3900"
	StorageRegion   = "garage"
	// StorageCopies is the number of nodes holding each object.
	StorageCopies = 3
)

// Storage renders the object store of a cluster. secret joins its nodes
// together; it never leaves the cluster.
func Storage(secret string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels:
    # A system label: the delivery removes application namespaces it no longer knows.
    myinfra.io/system: "true"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: garage
  namespace: %[1]s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: myinfra-garage
rules:
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch", "create", "patch"]
  - apiGroups: ["deuxfleurs.fr"]
    resources: ["garagenodes"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: myinfra-garage
subjects:
  - kind: ServiceAccount
    name: garage
    namespace: %[1]s
roleRef:
  kind: ClusterRole
  name: myinfra-garage
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: Secret
metadata:
  name: garage
  namespace: %[1]s
type: Opaque
stringData:
  rpc: %[2]q
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: garage
  namespace: %[1]s
data:
  garage.toml: |
    metadata_dir = "/var/lib/garage/meta"
    data_dir = "/var/lib/garage/data"
    db_engine = "sqlite"
    replication_factor = %[3]d
    rpc_bind_addr = "[::]:3901"

    [kubernetes_discovery]
    namespace = "%[1]s"
    service_name = "garage"
    skip_crd = false

    [s3_api]
    s3_region = "%[4]s"
    api_bind_addr = "[::]:3900"
    root_domain = ".s3.garage"

    [admin]
    api_bind_addr = "[::]:3903"
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: garage
  namespace: %[1]s
  labels:
    myinfra.io/system: "true"
spec:
  selector:
    matchLabels:
      app: garage
  template:
    metadata:
      labels:
        app: garage
    spec:
      serviceAccountName: garage
      containers:
        - name: garage
          image: %[5]s
          command: ["/garage", "server"]
          env:
            - name: GARAGE_RPC_SECRET
              valueFrom:
                secretKeyRef:
                  name: garage
                  key: rpc
          ports:
            - containerPort: 3900
            - containerPort: 3901
          resources:
            requests:
              memory: 64Mi
          volumeMounts:
            - name: config
              mountPath: /etc/garage.toml
              subPath: garage.toml
            - name: data
              mountPath: /var/lib/garage
      volumes:
        - name: config
          configMap:
            name: garage
        - name: data
          hostPath:
            path: /var/lib/myinfra-garage
            type: DirectoryOrCreate
---
apiVersion: v1
kind: Service
metadata:
  name: garage
  namespace: %[1]s
spec:
  selector:
    app: garage
  ports:
    - name: s3
      port: 3900
      targetPort: 3900
`, StorageNamespace, secret, StorageCopies, StorageRegion, StorageImage)
}
