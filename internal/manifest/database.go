package manifest

import (
	"fmt"
	"strings"
)

// DatabaseOperator installs CloudNativePG, which keeps a PostgreSQL primary
// and its replicas on different nodes and promotes a replica when the
// primary's node fails.
const DatabaseOperator = "https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v1.30.1/cnpg-1.30.1.yaml"

// DatabaseOperatorPatch keeps the operator itself out of the way of a node
// failure: it decides the failover, so two copies on two nodes, one leading,
// and a short wait for an unreachable node.
const DatabaseOperatorPatch = `{"spec":{"replicas":2,"template":{"spec":{
"tolerations":[
{"key":"node.kubernetes.io/unreachable","operator":"Exists","effect":"NoExecute","tolerationSeconds":30},
{"key":"node.kubernetes.io/not-ready","operator":"Exists","effect":"NoExecute","tolerationSeconds":30}],
"affinity":{"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":100,"podAffinityTerm":{
"topologyKey":"kubernetes.io/hostname","labelSelector":{"matchLabels":{"app.kubernetes.io/name":"cloudnative-pg"}}}}]}}}}}}`

// Database is a replicated PostgreSQL of an application. The operator creates
// the Secret <Name>-app (username, password, dbname) and the Service <Name>-rw
// that always leads to the primary.
type Database struct {
	Name      string
	Database  string
	Instances int
	SizeGi    int
	// Offsite sends WAL and base backups to an external S3-compatible store.
	Offsite *S3Replica
	// BackupSchedule is the cron for ScheduledBackup when Offsite is set.
	BackupSchedule string
}

const cnpgOffsiteSecret = "myinfra-cnpg-offsite"

func writeDatabase(b *strings.Builder, ns string, db Database) {
	instances := db.Instances
	if instances < 2 {
		instances = 2
	}
	size := db.SizeGi
	if size < 1 {
		size = 2
	}
	if db.Offsite != nil {
		writeCNPGOffsiteSecret(b, ns, db.Offsite)
	}
	backup := cnpgBackupBlock(db.Offsite)
	fmt.Fprintf(b, `---
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  instances: %[3]d
  storage:
    size: %[4]dGi
  bootstrap:
    initdb:
      database: %[5]s
      owner: %[6]s
  affinity:
    enablePodAntiAffinity: true
    topologyKey: kubernetes.io/hostname
    podAntiAffinityType: required
    # The operator promotes a replica once the primary's pod is gone: by
    # default Kubernetes waits five minutes for an unreachable node.
    tolerations:
      - key: node.kubernetes.io/unreachable
        operator: Exists
        effect: NoExecute
        tolerationSeconds: %[7]d
      - key: node.kubernetes.io/not-ready
        operator: Exists
        effect: NoExecute
        tolerationSeconds: %[7]d
  resources:
    requests:
      memory: 128Mi
%[8]s`, db.Name, ns, instances, size, db.Database, db.Database, failover, backup)
	if db.Offsite != nil {
		schedule := strings.TrimSpace(db.BackupSchedule)
		if schedule == "" {
			schedule = "0 4 * * *"
		}
		fmt.Fprintf(b, `---
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: backup-%s
  namespace: %s
  labels:
    myinfra.io/managed: "true"
spec:
  schedule: %q
  backupOwnerReference: self
  cluster:
    name: %s
`, db.Name, ns, schedule, db.Name)
	}
}

func writeCNPGOffsiteSecret(b *strings.Builder, ns string, off *S3Replica) {
	if off == nil || off.Bucket == "" || off.Endpoint == "" {
		return
	}
	fmt.Fprintf(b, `---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
  labels:
    myinfra.io/managed: "true"
type: Opaque
stringData:
  ACCESS_KEY_ID: %q
  SECRET_ACCESS_KEY: %q
`, cnpgOffsiteSecret, ns, off.KeyID, off.Secret)
}

func cnpgBackupBlock(off *S3Replica) string {
	if off == nil || off.Bucket == "" || off.Endpoint == "" {
		return ""
	}
	path := strings.TrimSuffix(strings.TrimPrefix(off.Path, "/"), "/")
	dest := fmt.Sprintf("s3://%s/%s", off.Bucket, path)
	return fmt.Sprintf(`  backup:
    retentionPolicy: 30d
    barmanObjectStore:
      destinationPath: %q
      endpointURL: %q
      s3Credentials:
        accessKeyId:
          name: %s
          key: ACCESS_KEY_ID
        secretAccessKey:
          name: %s
          key: SECRET_ACCESS_KEY
      wal:
        compression: gzip
      data:
        compression: gzip
`, dest, off.Endpoint, cnpgOffsiteSecret, cnpgOffsiteSecret)
}

// Migration renders the Job that moves a service's data from its disk to
// PostgreSQL and object storage: it dumps with the service's present settings,
// then restores with the new ones. The service is stopped meanwhile. Some
// applications report success whatever happens, so the Job checks: the dump
// must exist, and every table with rows in the dump must have rows after.
type Migration struct {
	Service Service
	Dump    []string
	Restore []string
	// RestoreInput is written on the restore's standard input.
	RestoreInput string
	// ToolEnv is set on the dump and the restore: run from the mounted image,
	// the program may need to be told where it would live.
	ToolEnv map[string]string
	Tables  []string
	// Database is where the restore writes; its Secret holds the URI.
	Database string
	// Env is what the restore reads: PostgreSQL and object storage.
	Env       map[string]string
	SecretEnv map[string]SecretKey
}

const (
	migrationDir = "myinfra-migration"
	toolImage    = "rancher/mirrored-library-busybox:1.37.0"
	checkImage   = "postgres:17-alpine"
)

func MigrationJob(app App, m Migration) string {
	ns := Namespace(app.ID)
	svc := m.Service
	dir := strings.TrimSuffix(diskPathOf(svc), "/") + "/" + migrationDir
	// The application's image may hold no shell: its files are mounted in a
	// busybox container, which runs its program and can answer it.
	run := func(parts []string) string {
		quoted := make([]string, len(parts))
		for i, part := range parts {
			part = strings.ReplaceAll(part, "{dir}", dir)
			if i == 0 && strings.HasPrefix(part, "/") {
				part = "/image" + part
			}
			quoted[i] = shellQuote(part)
		}
		return strings.Join(quoted, " ")
	}
	mount := fmt.Sprintf("          volumeMounts:\n            - name: data\n              mountPath: %s\n            - name: image\n              mountPath: /image\n              readOnly: true\n", diskPathOf(svc))
	tables := strings.Join(m.Tables, " ")
	check := fmt.Sprintf(`z=%[1]s/dump.zip
test -s "$z" || { echo "Export absent ou vide." >&2; exit 1; }
: > %[1]s/expected
for t in %[2]s; do
  c=$(unzip -p "$z" "database/$t.json" 2>/dev/null | tr -d ' \n')
  case "$c" in ""|"[]"|"null") ;; *) echo "$t" >> %[1]s/expected ;; esac
done
echo "Tables à retrouver : $(cat %[1]s/expected | tr '\n' ' ')"`, dir, tables)
	verify := fmt.Sprintf(`fail=0
for t in $(cat %[1]s/expected); do
  n=$(psql "$PGURI" -tAc "SELECT count(*) FROM $t") || { echo "Lecture de $t impossible." >&2; exit 1; }
  echo "$t : $n"
  [ "$n" -gt 0 ] || { echo "La table $t est vide après l'import." >&2; fail=1; }
done
exit $fail`, dir)
	var b strings.Builder
	fmt.Fprintf(&b, `apiVersion: batch/v1
kind: Job
metadata:
  name: migrate-%s
  namespace: %s
  labels:
    myinfra.io/managed: "true"
spec:
  backoffLimit: 0
  # A move that takes longer has gone wrong: the Job fails and the service
  # goes back to its disk.
  activeDeadlineSeconds: 1200
  template:
    spec:
      restartPolicy: Never
      initContainers:
        - name: clean
          image: %s
          command: ["sh", "-c", %q]
%s        - name: dump
          image: %s
          command: ["sh", "-c", %q]
`, svc.Name, ns, toolImage, fmt.Sprintf("rm -rf %s && mkdir -p %s && chmod 777 %s", dir, dir, dir), mount, toolImage, "cd "+shellQuote(diskPathOf(svc))+" && "+run(m.Dump))
	refs := envRefs(app)
	dump := svc
	dump.Env, dump.SecretEnv, dump.Args = m.ToolEnv, nil, nil
	writeContainerEnv(&b, refs, withoutVolumes(dump), nil)
	b.WriteString(mount)
	fmt.Fprintf(&b, "        - name: check-dump\n          image: %s\n          command: [\"sh\", \"-c\", %q]\n%s", toolImage, check, mount)
	restoreCmd := run(m.Restore)
	if m.RestoreInput != "" {
		restoreCmd = "printf %s " + shellQuote(m.RestoreInput) + " | " + restoreCmd
	}
	restoreCmd = "cd " + shellQuote(diskPathOf(svc)) + " && " + restoreCmd
	fmt.Fprintf(&b, "        - name: restore\n          image: %s\n          command: [\"sh\", \"-c\", %q]\n", toolImage, restoreCmd)
	restore := svc
	restore.Env, restore.SecretEnv, restore.Args = map[string]string{}, m.SecretEnv, nil
	for key, value := range m.ToolEnv {
		restore.Env[key] = value
	}
	for key, value := range m.Env {
		restore.Env[key] = value
	}
	writeContainerEnv(&b, refs, withoutVolumes(restore), nil)
	b.WriteString(mount)
	fmt.Fprintf(&b, "      containers:\n        - name: verify\n          image: %s\n          command: [\"sh\", \"-c\", %q]\n          env:\n            - name: PGURI\n              valueFrom:\n                secretKeyRef:\n                  name: %s-app\n                  key: uri\n%s",
		checkImage, verify, m.Database, mount)
	fmt.Fprintf(&b, "      volumes:\n        - name: data\n          persistentVolumeClaim:\n            claimName: %s\n        - name: image\n          image:\n            reference: %s\n            pullPolicy: IfNotPresent\n", svc.Name, svc.Image)
	return b.String()
}

// shellQuote quotes a word for sh.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// withoutVolumes keeps the container settings of a service and leaves the
// volumes to the caller.
func withoutVolumes(svc Service) Service {
	svc.Disk = false
	return svc
}
