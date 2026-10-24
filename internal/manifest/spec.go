package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"myinfra/internal/spec"
)

// WaitImage checks that the services a service depends on answer.
const WaitImage = "rancher/mirrored-library-busybox:1.37.0"

// specOK checks a service's spec, and that it names other services of the
// application and ports that differ from its own.
func specOK(app App, svc Service) bool {
	if len(spec.Check(svc.Spec)) > 0 {
		return false
	}
	for _, port := range svc.Spec.Ports {
		if port.Port == listenOf(svc) {
			return false
		}
	}
	for _, path := range svc.Spec.Volumes {
		if svc.Disk && path.Path == diskPathOf(svc) {
			return false
		}
	}
	for _, name := range svc.Spec.DependsOn {
		found := false
		for _, other := range app.Services {
			if other.Name == name && other.Name != svc.Name {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// servicePorts maps each service to the port the others reach it on. A
// service without port is left out: nothing waits for it.
func servicePorts(app App) map[string]int {
	out := make(map[string]int, len(app.Services))
	for _, svc := range app.Services {
		if !svc.Spec.NoPort {
			out[svc.Name] = listenOf(svc)
		}
	}
	return out
}

// withFiles adds the files of a service to the sum its copies started with:
// a ConfigMap read through subPath changes nothing in a running container.
func withFiles(sum string, svc Service) string {
	if len(svc.Spec.Files) == 0 {
		return sum
	}
	h := sha256.New()
	h.Write([]byte(sum))
	for _, file := range svc.Spec.Files {
		fmt.Fprintf(h, "\x00%s\x00%s", file.Path, file.Content)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func filesName(svc Service) string {
	return svc.Name + ".files"
}

// claimName names the claim of a disk besides the first: service names hold
// no dot, so it never meets another service's own claim. A shared disk has
// one claim for the application, whichever service mounts it.
func claimName(svc Service, i int) string {
	if disk := svc.Spec.Volumes[i].Disk; disk != "" {
		return SharedClaim(disk)
	}
	return fmt.Sprintf("%s.%s", svc.Name, volumeName(i))
}

// SharedClaim names the claim of a disk the services of an application
// share; "disk-" never begins a service name followed by a dot.
func SharedClaim(disk string) string {
	return "disk-" + disk
}

// sharedDisks lists the disks the services of an application share, with
// the largest size asked for each.
func sharedDisks(app App) ([]string, map[string]int) {
	sizes := map[string]int{}
	var names []string
	for _, svc := range app.Services {
		for _, volume := range svc.Spec.Volumes {
			if volume.Disk == "" {
				continue
			}
			if _, seen := sizes[volume.Disk]; !seen {
				names = append(names, volume.Disk)
			}
			if sizeOf(volume) > sizes[volume.Disk] {
				sizes[volume.Disk] = sizeOf(volume)
			}
		}
	}
	sort.Strings(names)
	return names, sizes
}

// sharedLabels marks the pods of a service with the disks it shares, and
// sharedAffinity keeps them on the node of the pods that mount the same:
// the claim lives on one node's disk.
func sharedLabels(svc Service) string {
	var b strings.Builder
	for _, name := range diskNames(svc) {
		fmt.Fprintf(&b, "        myinfra.io/disk-%s: \"true\"\n", name)
	}
	return b.String()
}

func sharedAffinity(svc Service) string {
	names := diskNames(svc)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("      affinity:\n        podAffinity:\n          requiredDuringSchedulingIgnoredDuringExecution:\n")
	for _, name := range names {
		fmt.Fprintf(&b, "            - topologyKey: kubernetes.io/hostname\n              labelSelector:\n                matchLabels:\n                  myinfra.io/disk-%s: \"true\"\n", name)
	}
	return b.String()
}

func diskNames(svc Service) []string {
	var out []string
	for _, volume := range svc.Spec.Volumes {
		if volume.Disk != "" {
			out = append(out, volume.Disk)
		}
	}
	sort.Strings(out)
	return out
}

func volumeName(i int) string {
	return fmt.Sprintf("v%d", i+1)
}

func sizeOf(volume spec.Volume) int {
	if volume.SizeGi < 1 {
		return 1
	}
	return volume.SizeGi
}

func healthPort(svc Service) int {
	if svc.Spec.Health != nil && svc.Spec.Health.Port > 0 {
		return svc.Spec.Health.Port
	}
	return listenOf(svc)
}

func writeFiles(b *strings.Builder, ns string, svc Service) {
	if len(svc.Spec.Files) == 0 {
		return
	}
	fmt.Fprintf(b, "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: %s\ndata:\n", filesName(svc), ns)
	for i, file := range svc.Spec.Files {
		fmt.Fprintf(b, "  f%d: %q\n", i, file.Content)
	}
}

// writeClaims writes the claims of the disks besides the first, for a
// service that is not a StatefulSet.
func writeClaims(b *strings.Builder, ns string, svc Service) {
	for i, volume := range svc.Spec.Volumes {
		if volume.Disk != "" {
			continue
		}
		fmt.Fprintf(b, "---\napiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  accessModes: [ReadWriteOnce]\n  resources:\n    requests:\n      storage: %dGi\n", claimName(svc, i), ns, sizeOf(volume))
	}
}

// waitFor writes the init containers that hold a service until the services
// it depends on accept connections.
func waitFor(svc Service, ports map[string]int) string {
	items := waitItems(svc, ports)
	if items == "" {
		return ""
	}
	return "      initContainers:\n" + items
}

func waitItems(svc Service, ports map[string]int) string {
	var b strings.Builder
	for _, name := range svc.Spec.DependsOn {
		port, ok := ports[name]
		if !ok {
			continue
		}
		script := fmt.Sprintf("until nc -z -w 2 %s %d; do echo 'waiting for %s'; sleep 2; done", name, port, name)
		fmt.Fprintf(&b, "        - name: wait-%s\n          image: %s\n          command: [\"sh\", \"-c\", %q]\n", name, WaitImage, script)
	}
	return b.String()
}

// containerHead starts a service's container: its image, command, ports,
// probes and reservations. probe adds the default readiness check; a health
// check the service declares is always written.
func containerHead(svc Service, probe bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "        - name: %s\n          image: %s\n", svc.Name, svc.Image)
	if len(svc.Spec.Command) > 0 {
		b.WriteString("          command:\n")
		for _, word := range svc.Spec.Command {
			fmt.Fprintf(&b, "            - %q\n", word)
		}
	}
	if !svc.Spec.NoPort {
		fmt.Fprintf(&b, "          ports:\n            - containerPort: %d\n", listenOf(svc))
		for _, port := range svc.Spec.Ports {
			fmt.Fprintf(&b, "            - containerPort: %d\n              name: %s\n", port.Port, portLabel(port))
		}
	}
	if probe || svc.Spec.Health != nil {
		b.WriteString(readiness(svc))
	}
	if r := svc.Spec.Resources; r != nil && (r.CPU != "" || r.Memory != "") {
		b.WriteString("          resources:\n            requests:\n")
		if r.CPU != "" {
			fmt.Fprintf(&b, "              cpu: %s\n", r.CPU)
		}
		if r.Memory != "" {
			fmt.Fprintf(&b, "              memory: %s\n            limits:\n              memory: %s\n", r.Memory, r.Memory)
		}
	}
	return b.String()
}

func portLabel(port spec.Port) string {
	if port.Name != "" {
		return port.Name
	}
	return fmt.Sprintf("p%d", port.Port)
}

// servicePortLines lists the ports of a service's Service. With more than
// one, Kubernetes wants each named.
func servicePortLines(svc Service) string {
	if len(svc.Spec.Ports) == 0 {
		return fmt.Sprintf("    - port: %d\n      targetPort: %d\n", listenOf(svc), listenOf(svc))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "    - name: main\n      port: %d\n      targetPort: %d\n", listenOf(svc), listenOf(svc))
	for _, port := range svc.Spec.Ports {
		fmt.Fprintf(&b, "    - name: %s\n      port: %d\n      targetPort: %d\n", portLabel(port), port.Port, port.Port)
	}
	return b.String()
}

// extraMounts mounts the disks besides the first, and the files.
func extraMounts(svc Service) string {
	var b strings.Builder
	for i, volume := range svc.Spec.Volumes {
		fmt.Fprintf(&b, "            - name: %s\n              mountPath: %s\n", volumeName(i), volume.Path)
	}
	for i, file := range svc.Spec.Files {
		fmt.Fprintf(&b, "            - name: files\n              mountPath: %s\n              subPath: f%d\n              readOnly: true\n", file.Path, i)
	}
	return b.String()
}

// extraVolumes lists the pod volumes extraMounts uses. A StatefulSet claims
// its disks from templates instead.
func extraVolumes(svc Service, stateful bool) string {
	var b strings.Builder
	if !stateful {
		for i := range svc.Spec.Volumes {
			fmt.Fprintf(&b, "        - name: %s\n          persistentVolumeClaim:\n            claimName: %s\n", volumeName(i), claimName(svc, i))
		}
	}
	if len(svc.Spec.Files) > 0 {
		fmt.Fprintf(&b, "        - name: files\n          configMap:\n            name: %s\n", filesName(svc))
	}
	return b.String()
}
