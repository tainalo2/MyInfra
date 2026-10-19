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
	Image            string
	MasterKey        string
	DatabasePassword string
	BootstrapToken   string
	PublicURL        string
	Host             string
	RegistryHost     string
	RegistryUser     string
	RegistryPassword string
}

func PodManifest(spec PodSpec) (string, error) {
	if !safeImage(spec.Image) || !safe(spec.MasterKey) || !safe(spec.DatabasePassword) || !safe(spec.BootstrapToken) || !safe(spec.PublicURL) || !safe(spec.Host) || strings.ContainsAny(spec.Host, "`'\\") {
		return "", fmt.Errorf("paramètre de démarrage refusé")
	}
	zone := cert.Zone(spec.Host, "")
	if zone == "" {
		return "", fmt.Errorf("paramètre de démarrage refusé")
	}
	resolver := cert.Resolver(zone)
	httpMatch := "(Host(`" + spec.Host + "`) && !PathPrefix(`/.well-known/acme-challenge/`))"
	tlsMatch := "Host(`" + spec.Host + "`)"
	password := spec.DatabasePassword
	databaseURL := "postgres://myinfra:" + password + "@postgres:5432/myinfra?sslmode=disable"
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
  name: %s
---
%s
apiVersion: v1
kind: Secret
metadata:
  name: config
  namespace: %s
type: Opaque
stringData:
  POSTGRES_PASSWORD: %s
  DATABASE_URL: %s
  MYINFRA_MASTER_KEY: %s
  MYINFRA_PUBLIC_URL: %s
  MYINFRA_BOOTSTRAP_TOKEN: %s
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres
  namespace: %s
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 8Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: %s
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      myinfra.io/service: postgres
  template:
    metadata:
      labels:
        myinfra.io/service: postgres
    spec:
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              preference:
                matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
                    operator: DoesNotExist
      containers:
        - name: postgres
          image: postgres:16
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_USER
              value: myinfra
            - name: POSTGRES_DB
              value: myinfra
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: config
                  key: POSTGRES_PASSWORD
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: postgres
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: %s
spec:
  selector:
    myinfra.io/service: postgres
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myinfra
  namespace: %s
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
%s      affinity:
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
      containers:
        - name: myinfra
          image: %s
          imagePullPolicy: Always
          ports:
            - containerPort: 8080
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: config
                  key: DATABASE_URL
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
  namespace: %s
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
  namespace: %s
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
  namespace: %s
spec:
  redirectScheme:
    scheme: https
    permanent: true
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: myinfra-http
  namespace: %s
spec:
  entryPoints: [web]
  routes:
    - match: %s
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
  namespace: %s
spec:
  entryPoints: [websecure]
  routes:
    - match: %s
      services:
        - name: myinfra
          port: 8080
  tls:
    certResolver: %s
`, PodNamespace, registryDoc, PodNamespace,
		strconv.Quote(password), strconv.Quote(databaseURL), strconv.Quote(spec.MasterKey), strconv.Quote(spec.PublicURL), strconv.Quote(spec.BootstrapToken),
		PodNamespace, PodNamespace, PodNamespace, PodNamespace, pull, spec.Image, PodNamespace, PodNamespace, PodNamespace, PodNamespace,
		strconv.Quote(httpMatch), PodNamespace, strconv.Quote(tlsMatch), resolver), nil
}

func PodHost(domain string) string {
	domain = strings.Trim(strings.TrimSpace(domain), ".")
	if domain == "" || domain == "apps.localhost" || strings.Contains(domain, "localhost") {
		return ""
	}
	return "myinfra." + domain
}
