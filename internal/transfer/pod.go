package transfer

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"myinfra/internal/cert"
)

const PodNamespace = "myinfra"

type PodSpec struct {
	Image          string
	MasterKey      string
	BootstrapToken string
	PublicURL      string
	Host           string
	// Instances of the database: two on two nodes, one on a cluster of one.
	Instances        int
	RegistryHost     string
	RegistryUser     string
	RegistryPassword string
}

// DatabaseName is the replicated PostgreSQL of MyInfra in its namespace.
// CloudNativePG creates the Secret database-app with the address to reach
// it, and the Service database-rw that always leads to its primary.
const DatabaseName = "database"

// PodManifest is MyInfra in a cluster: two copies of the program, and its
// database replicated by CloudNativePG on two nodes. Losing a node loses no
// data and stops nothing: a replica becomes the primary.
func PodManifest(spec PodSpec) (string, error) {
	if !safeImage(spec.Image) || !safe(spec.MasterKey) || !safe(spec.BootstrapToken) || !safe(spec.PublicURL) || !safe(spec.Host) || strings.ContainsAny(spec.Host, "`'\\") {
		return "", fmt.Errorf("paramètre de démarrage refusé")
	}
	zone := cert.Zone(spec.Host, "")
	if zone == "" {
		return "", fmt.Errorf("paramètre de démarrage refusé")
	}
	instances := spec.Instances
	if instances < 1 {
		instances = 2
	}
	resolver := cert.Resolver(zone)
	httpMatch := "(Host(`" + spec.Host + "`) && !PathPrefix(`/.well-known/acme-challenge/`))"
	tlsMatch := "Host(`" + spec.Host + "`)"
	registryDoc := ""
	pull := ""
	if spec.RegistryHost != "" || spec.RegistryUser != "" || spec.RegistryPassword != "" {
		if !safe(spec.RegistryHost) || !safe(spec.RegistryUser) || !safe(spec.RegistryPassword) {
			return "", fmt.Errorf("paramètre de démarrage refusé")
		}
		raw := registryAuth(spec.RegistryHost, spec.RegistryUser, spec.RegistryPassword)
		registryDoc = fmt.Sprintf("apiVersion: v1\nkind: Secret\nmetadata:\n  name: registry\n  namespace: %s\ntype: kubernetes.io/dockerconfigjson\ndata:\n  .dockerconfigjson: %s\n---\n", PodNamespace, base64.StdEncoding.EncodeToString(raw))
		pull = "      imagePullSecrets:\n        - name: registry\n"
	}
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
---
%[2]sapiVersion: v1
kind: Secret
metadata:
  name: config
  namespace: %[1]s
type: Opaque
stringData:
  MYINFRA_MASTER_KEY: %[3]s
  MYINFRA_PUBLIC_URL: %[4]s
  MYINFRA_BOOTSTRAP_TOKEN: %[5]s
---
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: %[6]s
  namespace: %[1]s
spec:
  instances: %[7]d
  storage:
    size: 8Gi
  bootstrap:
    initdb:
      database: myinfra
      owner: myinfra
  affinity:
    enablePodAntiAffinity: true
    topologyKey: kubernetes.io/hostname
    podAntiAffinityType: required
    tolerations:
      - key: node.kubernetes.io/unreachable
        operator: Exists
        effect: NoExecute
        tolerationSeconds: 30
      - key: node.kubernetes.io/not-ready
        operator: Exists
        effect: NoExecute
        tolerationSeconds: 30
  resources:
    requests:
      memory: 256Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myinfra
  namespace: %[1]s
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  selector:
    matchLabels:
      myinfra.io/service: myinfra
  template:
    metadata:
      labels:
        myinfra.io/service: myinfra
    spec:
%[8]s      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              preference:
                matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
                    operator: DoesNotExist
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                topologyKey: kubernetes.io/hostname
                labelSelector:
                  matchLabels:
                    myinfra.io/service: myinfra
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              myinfra.io/service: myinfra
      tolerations:
        - key: node.kubernetes.io/unreachable
          operator: Exists
          effect: NoExecute
          tolerationSeconds: 30
        - key: node.kubernetes.io/not-ready
          operator: Exists
          effect: NoExecute
          tolerationSeconds: 30
      # MyInfra starts once its database answers, instead of failing until then.
      initContainers:
        - name: database
          image: rancher/mirrored-library-busybox:1.37.0
          command: [sh, -c, "until nc -z -w 2 %[6]s-rw 5432; do sleep 2; done"]
      containers:
        - name: myinfra
          image: %[9]s
          imagePullPolicy: Always
          ports:
            - containerPort: 8080
          readinessProbe:
            httpGet:
              path: /api/health
              port: 8080
            periodSeconds: 5
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: %[6]s-app
                  key: uri
            - name: MYINFRA_MASTER_KEY
              valueFrom:
                secretKeyRef:
                  name: config
                  key: MYINFRA_MASTER_KEY
            - name: MYINFRA_PUBLIC_URL
              valueFrom:
                secretKeyRef:
                  name: config
                  key: MYINFRA_PUBLIC_URL
            - name: MYINFRA_BOOTSTRAP_TOKEN
              valueFrom:
                secretKeyRef:
                  name: config
                  key: MYINFRA_BOOTSTRAP_TOKEN
            - name: MYINFRA_ADDR
              value: ":8080"
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: myinfra
  namespace: %[1]s
spec:
  minAvailable: 1
  selector:
    matchLabels:
      myinfra.io/service: myinfra
---
apiVersion: v1
kind: Service
metadata:
  name: myinfra
  namespace: %[1]s
spec:
  selector:
    myinfra.io/service: myinfra
  ports:
    - port: 8080
      targetPort: 8080
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: https
  namespace: %[1]s
spec:
  redirectScheme:
    scheme: https
    permanent: true
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: myinfra-http
  namespace: %[1]s
spec:
  entryPoints: [web]
  routes:
    - match: %[10]s
      middlewares:
        - name: https
      services:
        - name: myinfra
          port: 8080
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: myinfra
  namespace: %[1]s
spec:
  entryPoints: [websecure]
  routes:
    - match: %[11]s
      services:
        - name: myinfra
          port: 8080
  tls:
    certResolver: %[12]s
`, PodNamespace, registryDoc,
		strconv.Quote(spec.MasterKey), strconv.Quote(spec.PublicURL), strconv.Quote(spec.BootstrapToken),
		DatabaseName, instances, pull, spec.Image,
		strconv.Quote(httpMatch), strconv.Quote(tlsMatch), resolver), nil
}

func PodHost(domain string) string {
	domain = strings.Trim(strings.TrimSpace(domain), ".")
	if domain == "" || domain == "apps.localhost" || strings.Contains(domain, "localhost") {
		return ""
	}
	return "myinfra." + domain
}
