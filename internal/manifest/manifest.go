package manifest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"myinfra/internal/cert"
	"myinfra/internal/pause"
	"myinfra/internal/spec"
)

var (
	deployService = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	deployImage   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@+/-]{0,200}$`)
	deployEnv     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	deployPath    = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	deployArg     = regexp.MustCompile(`^[A-Za-z0-9_.,:/=@+-]+$`)
	sharedName    = regexp.MustCompile(`^sec[a-z0-9]{1,40}$`)
)

func EnvOK(env map[string]string) bool {
	for key, value := range env {
		if !deployEnv.MatchString(key) || len(value) > 8192 || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func Deployable(app App) bool {
	if app.ID == "" || !EnvOK(app.Env) {
		return false
	}
	for _, shared := range app.Shared {
		if !sharedName.MatchString(shared.Name) || len(shared.Data) == 0 || !EnvOK(shared.Data) {
			return false
		}
	}
	for _, host := range app.Hosts {
		if host == "" || strings.ContainsAny(host, "\r\n '\"\\") {
			return false
		}
	}
	for _, svc := range app.Services {
		if !deployService.MatchString(svc.Name) || !deployImage.MatchString(svc.Image) || strings.Contains(svc.Image, "..") {
			return false
		}
		if svc.Listen < 0 || svc.Listen > 65535 || svc.DiskGi < 0 || svc.DiskGi > 1024 || svc.Replicas < 0 || svc.Replicas > 10 {
			return false
		}
		if svc.Disk {
			path := diskPathOf(svc)
			if !deployPath.MatchString(path) || strings.Contains(path, "..") {
				return false
			}
		}
		for _, arg := range svc.Args {
			if !deployArg.MatchString(arg) {
				return false
			}
		}
		if svc.Backup != nil && !backupOK(svc) {
			return false
		}
		if !EnvOK(svc.Env) || !specOK(app, svc) {
			return false
		}
		for key, ref := range svc.SecretEnv {
			if !deployEnv.MatchString(key) || !objectName.MatchString(ref.Secret) || !deployEnv.MatchString(ref.Key) {
				return false
			}
		}
	}
	for _, db := range app.Databases {
		if !objectName.MatchString(db.Name) || !deployEnv.MatchString(db.Database) || db.Instances > 5 || db.SizeGi > 1024 {
			return false
		}
	}
	for _, secret := range app.Secrets {
		if !objectName.MatchString(secret.Name) || !EnvOK(secret.Data) {
			return false
		}
	}
	return true
}

// objectName is a Kubernetes object MyInfra names itself.
var objectName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,50}$`)

var (
	backupBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,62}$`)
	backupKey    = regexp.MustCompile(`^GK[0-9a-f]{24}$`)
	backupSecret = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// backupOK checks a backup: its files sit on the service's disk.
func backupOK(svc Service) bool {
	if !svc.Disk || len(svc.Backup.Databases) == 0 || !backupBucket.MatchString(svc.Backup.Bucket) ||
		!backupKey.MatchString(svc.Backup.KeyID) || !backupSecret.MatchString(svc.Backup.Secret) {
		return false
	}
	if !EnvOK(svc.Backup.Env) {
		return false
	}
	root := strings.TrimSuffix(diskPathOf(svc), "/") + "/"
	for _, db := range svc.Backup.Databases {
		if !deployPath.MatchString(db) || strings.Contains(db, "..") || !strings.HasPrefix(db, root) {
			return false
		}
	}
	return true
}

type Service struct {
	Name      string
	Image     string
	Published bool
	Disk      bool
	DiskPath  string
	DiskGi    int
	Listen    int
	Args      []string
	Replicas  int
	// Backup copies the SQLite files of a single writer to the cluster's
	// object store, so that its one copy can start again on any node.
	Backup *Backup
	// Env and SecretEnv are set on the container: a value, or a key of a
	// Secret of the namespace.
	Env       map[string]string
	SecretEnv map[string]SecretKey
	// Stopped runs no copy while its data moves elsewhere.
	Stopped bool
	// Spec is what the service runs with besides the fields above.
	Spec spec.Service
	// configSum changes with the variables and secrets the copies read: a
	// change of the Secrets alone restarts them, one after the other.
	configSum string
}

// SecretKey names one key of a Secret.
type SecretKey struct {
	Secret string
	Key    string
}

// S3Replica is a Litestream destination outside the cluster's Garage.
type S3Replica struct {
	Endpoint string
	Bucket   string
	Region   string
	// Path is the prefix inside the bucket (cluster and application).
	Path   string
	KeyID  string
	Secret string
}

// Backup describes how Litestream replicates a service's SQLite files.
type Backup struct {
	Databases []string
	Bucket    string
	KeyID     string
	Secret    string
	// Offsite adds a second S3 replica (external object store).
	Offsite *S3Replica
	// Copying keeps the node disk while the first copy is made from it.
	Copying bool
	// Env is set on the application's container.
	Env map[string]string
}

type SharedSecret struct {
	Name string
	Data map[string]string
}

type App struct {
	ID       string
	Paused   bool
	Hosts    []string
	Zones    map[string]string
	Env      map[string]string
	Shared   []SharedSecret
	Services []Service
	// Databases are replicated PostgreSQL clusters of the application.
	Databases []Database
	// Secrets are rendered as they are, for the keys services read.
	Secrets []SharedSecret
	// Pull is the docker config JSON of the private registries the images
	// come from. Given to the default service account, it reaches every pod.
	Pull string
}

// PullSecret is the secret the pods of an application pull private images with.
const PullSecret = "myinfra-pull"

func SecretObject(id string) string {
	var b strings.Builder
	b.WriteString("sec")
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
		if b.Len() >= 23 {
			break
		}
	}
	if b.Len() < 4 {
		return "secapp"
	}
	return b.String()
}

func Proxy(zones []string) string {
	seen := map[string]bool{}
	var clean []string
	for _, zone := range zones {
		zone = cert.Zone("", zone)
		if zone == "" || seen[zone] {
			continue
		}
		seen[zone] = true
		clean = append(clean, zone)
	}
	sort.Strings(clean)
	var resolvers strings.Builder
	for _, zone := range clean {
		fmt.Fprintf(&resolvers, `      %s:
        acme:
          email: %s
          storage: /data/acme-%s.json
          tlsChallenge: true
`, cert.Resolver(zone), strconv.Quote(cert.Email(zone)), cert.Resolver(zone))
	}
	return fmt.Sprintf(`apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    certificatesResolvers:
%s    persistence:
      enabled: false
    # One Traefik receives all the traffic: when its node stops answering it
    # starts elsewhere after 30 s instead of five minutes. Its certificates
    # come back from the seed, so moving costs no new certificate.
    tolerations:
      - key: CriticalAddonsOnly
        operator: Exists
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      - key: node.kubernetes.io/unreachable
        operator: Exists
        effect: NoExecute
        tolerationSeconds: 30
      - key: node.kubernetes.io/not-ready
        operator: Exists
        effect: NoExecute
        tolerationSeconds: 30
    podSecurityContext:
      fsGroup: 65532
      fsGroupChangePolicy: OnRootMismatch
    deployment:
      additionalVolumes:
        - name: certificate-seed
          secret:
            secretName: %s
            optional: true
      initContainers:
        - name: certificate-seed
          image: rancher/mirrored-library-busybox:1.37.0
          command:
            - sh
            - -c
            - |
              for f in /seed/*.json; do
                [ -f "$f" ] || continue
                cp "$f" "/data/${f##*/}"
                chmod 600 "/data/${f##*/}"
              done
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: [ALL]
          volumeMounts:
            - name: data
              mountPath: /data
            - name: certificate-seed
              mountPath: /seed
              readOnly: true
`, resolvers.String(), CertificateSecret)
}

// CertificateSecret holds the certificate files MyInfra kept. Traefik keeps
// its own on a disk that a restart empties: the init container puts these
// back first, so that Let's Encrypt is not asked again for the same names.
const CertificateSecret = "myinfra-certificates"

// CertificateFiles renders the Secret that seeds Traefik's certificate files.
func CertificateFiles(files map[string][]byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: kube-system\ntype: Opaque\n", CertificateSecret)
	if len(files) == 0 {
		b.WriteString("data: {}\n")
		return b.String()
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	b.WriteString("data:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "  %s: %s\n", name, base64.StdEncoding.EncodeToString(files[name]))
	}
	return b.String()
}

func Namespace(id string) string {
	var b strings.Builder
	for _, r := range strings.ReplaceAll(id, "-", "") {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() == 20 {
			break
		}
	}
	out := b.String()
	if out == "" {
		out = "app"
	}
	return "mi" + out
}

func Render(app App) string {
	ns := Namespace(app.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n  labels:\n    myinfra.io/managed: \"true\"\n    myinfra.io/app: %q\n", ns, app.ID)
	if app.Pull != "" {
		fmt.Fprintf(&b, "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\ntype: kubernetes.io/dockerconfigjson\ndata:\n  .dockerconfigjson: %s\n", PullSecret, ns, base64.StdEncoding.EncodeToString([]byte(app.Pull)))
		fmt.Fprintf(&b, "---\napiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: default\n  namespace: %s\nimagePullSecrets:\n  - name: %s\n", ns, PullSecret)
	}
	if len(app.Env) > 0 {
		fmt.Fprintf(&b, "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: env\n  namespace: %s\ntype: Opaque\nstringData:\n", ns)
		for _, key := range sortedKeys(app.Env) {
			fmt.Fprintf(&b, "  %s: %q\n", key, app.Env[key])
		}
	}
	for _, shared := range app.Shared {
		if shared.Name == "" || len(shared.Data) == 0 {
			continue
		}
		fmt.Fprintf(&b, "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\n  labels:\n    myinfra.io/shared: \"true\"\ntype: Opaque\nstringData:\n", shared.Name, ns)
		for _, key := range sortedKeys(shared.Data) {
			fmt.Fprintf(&b, "  %s: %q\n", key, shared.Data[key])
		}
	}
	for _, secret := range app.Secrets {
		fmt.Fprintf(&b, "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\n  namespace: %s\ntype: Opaque\nstringData:\n", secret.Name, ns)
		for _, key := range sortedKeys(secret.Data) {
			fmt.Fprintf(&b, "  %s: %q\n", key, secret.Data[key])
		}
	}
	for _, db := range app.Databases {
		writeDatabase(&b, ns, db)
	}
	refs := envRefs(app)
	sum := configSum(app)
	ports := servicePorts(app)
	for _, svc := range app.Services {
		svc.configSum = withFiles(sum, svc)
		writeFiles(&b, ns, svc)
		replicas := svc.Replicas
		if replicas < 1 {
			replicas = 1
		}
		if app.Paused || svc.Stopped {
			replicas = 0
		}
		stored := svc.Replicas
		if stored < 1 {
			stored = 1
		}
		if svc.Disk && svc.Backup != nil {
			writeClaims(&b, ns, svc)
			writeBackedUp(&b, ns, app.ID, svc, replicas, refs, ports)
		} else if svc.Disk && stored > 1 {
			writeStateful(&b, ns, app.ID, svc, replicas, refs, ports)
		} else {
			strategy := "RollingUpdate"
			if svc.Disk || len(svc.Spec.Volumes) > 0 {
				strategy = "Recreate"
			}
			if svc.Disk {
				fmt.Fprintf(&b, "---\napiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  accessModes: [ReadWriteOnce]\n  resources:\n    requests:\n      storage: %dGi\n", svc.Name, ns, diskGiOf(svc))
			}
			writeClaims(&b, ns, svc)
			fmt.Fprintf(&b, `---
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
    type: %s
%s  selector:
    matchLabels:
      myinfra.io/service: %s
  template:
    metadata:
      labels:
        myinfra.io/service: %s
%s    spec:
`, svc.Name, ns, app.ID, replicas, strategy, rolling(strategy), svc.Name, svc.Name, configAnnotation(svc, true))
			if replicas > 1 {
				b.WriteString(spread(svc.Name))
			}
			b.WriteString(moveAway())
			b.WriteString(waitFor(svc, ports))
			b.WriteString("      containers:\n")
			b.WriteString(containerHead(svc, true))
			writeContainerTail(&b, refs, svc, false)
		}
		// After the workload's last line: the budget is a document of its own.
		if replicas > 1 {
			writeBudget(&b, ns, svc.Name)
		}
		fmt.Fprintf(&b, `---
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  selector:
    myinfra.io/service: %s
  ports:
%s`, svc.Name, ns, svc.Name, servicePortLines(svc))
		if svc.Published && len(app.Hosts) > 0 {
			fmt.Fprintf(&b, `---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: https
  namespace: %s
spec:
  redirectScheme:
    scheme: https
    permanent: true
`, ns)
			target, port := svc.Name, listenOf(svc)
			if app.Paused {
				// A paused application shows a page instead of a proxy error.
				writePausePage(&b, ns)
				target, port = "paused", pause.Port
			}
			for _, group := range hostGroups(app.Hosts, app.Zones) {
				suffix := "-" + cert.Resolver(group.zone)
				match := hostMatch(group.hosts)
				fmt.Fprintf(&b, `---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: %s-http%s
  namespace: %s
spec:
  entryPoints: [web]
  routes:
    - match: %q
      middlewares:
        - name: https
      services:
        - name: %s
          port: %d
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: %s%s
  namespace: %s
spec:
  entryPoints: [websecure]
  routes:
    - match: %q
      services:
        - name: %s
          port: %d
  tls:
    certResolver: %s
`, svc.Name, suffix, ns, httpMatch(group.hosts), target, port, svc.Name, suffix, ns, match, target, port, cert.Resolver(group.zone))
			}
		}
	}
	return b.String()
}

func writePausePage(b *strings.Builder, ns string) {
	fmt.Fprintf(b, `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: paused
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      myinfra.io/service: paused
  template:
    metadata:
      labels:
        myinfra.io/service: paused
    spec:
      containers:
        - name: page
          image: %s
          command: ["sh", "-c", %q]
          env:
            - name: PAGE
              value: %q
          ports:
            - containerPort: %d
          resources:
            requests:
              cpu: 5m
              memory: 8Mi
            limits:
              memory: 32Mi
---
apiVersion: v1
kind: Service
metadata:
  name: paused
  namespace: %s
spec:
  selector:
    myinfra.io/service: paused
  ports:
    - port: %d
      targetPort: %d
`, ns, pause.Image, pause.Script, pause.Page, pause.Port, ns, pause.Port, pause.Port)
}

func writeStateful(b *strings.Builder, ns, appID string, svc Service, replicas int, refs []string, ports map[string]int) {
	fmt.Fprintf(b, `---
apiVersion: v1
kind: Service
metadata:
  name: %s-copies
  namespace: %s
spec:
  clusterIP: None
  selector:
    myinfra.io/service: %s
  ports:
    - port: %d
      targetPort: %d
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: %s
  namespace: %s
  labels:
    myinfra.io/managed: "true"
    myinfra.io/app: %q
spec:
  serviceName: %s-copies
  replicas: %d
  selector:
    matchLabels:
      myinfra.io/service: %s
  template:
    metadata:
      labels:
        myinfra.io/service: %s
%s    spec:
%s%s%s      containers:
%s`, svc.Name, ns, svc.Name, listenOf(svc), listenOf(svc), svc.Name, ns, appID, svc.Name, replicas, svc.Name, svc.Name, configAnnotation(svc, true), spread(svc.Name), moveAway(), waitFor(svc, ports), containerHead(svc, true))
	writeContainerTail(b, refs, svc, true)
	fmt.Fprintf(b, `  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: %dGi
`, diskGiOf(svc))
	for i, volume := range svc.Spec.Volumes {
		fmt.Fprintf(b, `    - metadata:
        name: %s
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: %dGi
`, volumeName(i), sizeOf(volume))
	}
}

func envRefs(app App) []string {
	refs := make([]string, 0, len(app.Shared)+1)
	for _, shared := range app.Shared {
		if shared.Name != "" && len(shared.Data) > 0 {
			refs = append(refs, shared.Name)
		}
	}
	if len(app.Env) > 0 {
		refs = append(refs, "env")
	}
	return refs
}

// writeContainerTail ends a service's container and its pod: arguments,
// variables, disks and files. A stateful service claims its disks from
// templates, written after.
func writeContainerTail(b *strings.Builder, refs []string, svc Service, stateful bool) {
	writeContainerEnv(b, refs, svc, nil)
	var mounts, volumes strings.Builder
	if svc.Disk {
		fmt.Fprintf(&mounts, "            - name: data\n              mountPath: %s\n", diskPathOf(svc))
		if !stateful {
			volumes.WriteString("        - name: data\n          persistentVolumeClaim:\n            claimName: " + svc.Name + "\n")
		}
	}
	mounts.WriteString(extraMounts(svc))
	volumes.WriteString(extraVolumes(svc, stateful))
	if mounts.Len() > 0 {
		b.WriteString("          volumeMounts:\n" + mounts.String())
	}
	if volumes.Len() > 0 {
		b.WriteString("      volumes:\n" + volumes.String())
	}
}

// writeContainerEnv writes the arguments and the variables of a container.
// extra is set last, over the others.
func writeContainerEnv(b *strings.Builder, refs []string, svc Service, extra map[string]string) {
	if len(svc.Args) > 0 {
		b.WriteString("          args:\n")
		for _, arg := range svc.Args {
			fmt.Fprintf(b, "            - %q\n", arg)
		}
	}
	if len(refs) > 0 {
		b.WriteString("          envFrom:\n")
		for _, name := range refs {
			fmt.Fprintf(b, "            - secretRef:\n                name: %s\n", name)
		}
	}
	env := map[string]string{}
	for key, value := range svc.Spec.Env {
		env[key] = value
	}
	for key, value := range svc.Env {
		env[key] = value
	}
	if svc.Disk && strings.Contains(svc.Image, "postgres") {
		env["PGDATA"] = diskPathOf(svc)
	}
	for key, value := range extra {
		env[key] = value
	}
	if len(env)+len(svc.SecretEnv) > 0 {
		b.WriteString("          env:\n")
		for _, key := range sortedKeys(env) {
			fmt.Fprintf(b, "            - name: %s\n              value: %q\n", key, env[key])
		}
		keys := make([]string, 0, len(svc.SecretEnv))
		for key := range svc.SecretEnv {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			ref := svc.SecretEnv[key]
			fmt.Fprintf(b, "            - name: %s\n              valueFrom:\n                secretKeyRef:\n                  name: %s\n                  key: %s\n", key, ref.Secret, ref.Key)
		}
	}
}

func listenOf(svc Service) int {
	if svc.Listen < 1 {
		return 80
	}
	return svc.Listen
}

func diskPathOf(svc Service) string {
	if svc.DiskPath == "" {
		return "/data"
	}
	return svc.DiskPath
}

func diskGiOf(svc Service) int {
	if svc.DiskGi < 1 {
		return 1
	}
	return svc.DiskGi
}

func configSum(app App) string {
	h := sha256.New()
	write := func(kind, name string, data map[string]string) {
		for _, key := range sortedKeys(data) {
			fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\n", kind, name, key, data[key])
		}
	}
	write("env", "", app.Env)
	for _, shared := range app.Shared {
		write("shared", shared.Name, shared.Data)
	}
	for _, secret := range app.Secrets {
		write("secret", secret.Name, secret.Data)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// configAnnotation marks the copies with the variables they started with.
// alone is true when the template has no other annotation.
func configAnnotation(svc Service, alone bool) string {
	if svc.configSum == "" {
		return ""
	}
	line := fmt.Sprintf("        myinfra.io/config: %q\n", svc.configSum)
	if alone {
		return "      annotations:\n" + line
	}
	return line
}

// rolling starts a new copy before an old one stops: an update never lowers
// the number of copies answering.
func rolling(strategy string) string {
	if strategy != "RollingUpdate" {
		return ""
	}
	return "    rollingUpdate:\n      maxUnavailable: 0\n      maxSurge: 1\n"
}

// readiness sends visitors to a copy only once it accepts connections on its
// port: a new version that does not start never replaces the old one. Only
// for a service known to listen: a published one, or one with its port set.
// A service that tells how it answers is asked that way, and restarted when
// it stops answering for a minute.
func readiness(svc Service) string {
	if h := svc.Spec.Health; h != nil {
		check := fmt.Sprintf("            tcpSocket:\n              port: %d\n", healthPort(svc))
		if h.HTTP != "" {
			check = fmt.Sprintf("            httpGet:\n              path: %q\n              port: %d\n", h.HTTP, healthPort(svc))
		}
		return "          readinessProbe:\n" + check + "            periodSeconds: 5\n            failureThreshold: 3\n" +
			"          livenessProbe:\n" + check + "            initialDelaySeconds: 30\n            periodSeconds: 10\n            failureThreshold: 6\n"
	}
	if !svc.Published && svc.Listen < 1 {
		return ""
	}
	return fmt.Sprintf("          readinessProbe:\n            tcpSocket:\n              port: %d\n            periodSeconds: 5\n            failureThreshold: 3\n", listenOf(svc))
}

// moveAway starts the copies of a lost node elsewhere after 30 seconds,
// instead of the 5 minutes Kubernetes waits by default.
func moveAway() string {
	return fmt.Sprintf(`      tolerations:
        - key: node.kubernetes.io/unreachable
          operator: Exists
          effect: NoExecute
          tolerationSeconds: %d
        - key: node.kubernetes.io/not-ready
          operator: Exists
          effect: NoExecute
          tolerationSeconds: %d
`, failover, failover)
}

// writeBudget keeps all but one copy of a service running while a server is
// emptied for maintenance.
func writeBudget(b *strings.Builder, ns, service string) {
	fmt.Fprintf(b, `---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: %s
  namespace: %s
spec:
  maxUnavailable: 1
  selector:
    matchLabels:
      myinfra.io/service: %s
`, service, ns, service)
}

func spread(service string) string {
	return fmt.Sprintf(`      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                topologyKey: kubernetes.io/hostname
                labelSelector:
                  matchLabels:
                    myinfra.io/service: %s
`, service)
}

type hostGroup struct {
	zone  string
	hosts []string
}

func hostGroups(hosts []string, zones map[string]string) []hostGroup {
	byZone := map[string][]string{}
	for _, host := range hosts {
		zone := cert.Zone(host, zones[host])
		byZone[zone] = append(byZone[zone], host)
	}
	order := make([]string, 0, len(byZone))
	for zone := range byZone {
		order = append(order, zone)
	}
	sort.Strings(order)
	out := make([]hostGroup, 0, len(order))
	for _, zone := range order {
		out = append(out, hostGroup{zone: zone, hosts: byZone[zone]})
	}
	return out
}

func hostMatch(hosts []string) string {
	parts := make([]string, len(hosts))
	for i, host := range hosts {
		parts[i] = "Host(`" + host + "`)"
	}
	return strings.Join(parts, " || ")
}

func httpMatch(hosts []string) string {
	return "(" + hostMatch(hosts) + ") && !PathPrefix(`/.well-known/acme-challenge/`)"
}

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
