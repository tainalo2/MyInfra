package pack

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"myinfra/internal/spec"
)

// kube turns Kubernetes manifests, as helm template writes them, into a
// package: one service per container of a workload, the domain on what an
// Ingress leads to, disks from claims, files from ConfigMaps, secrets drawn
// anew.
type kube struct {
	*composer
	namespace  string
	objects    []map[string]any
	configMaps map[string]map[string]string
	secretData map[string]map[string]string
	claims     map[string]int
	// aliases maps each Kubernetes Service a workload answers behind to the
	// MyInfra service, with the port the others used.
	aliases map[string]alias
}

type alias struct {
	service string
	port    int
	target  int
}

type workload struct {
	kind, name string
	labels     map[string]string
	pod        map[string]any
	replicas   int
	templates  map[string]int
}

// Kube reads Kubernetes manifests. namespace is the one they were rendered
// for: names under it are the services of the package.
func Kube(data []byte, name, namespace string) (Package, []Warning, error) {
	k := &kube{
		composer: &composer{
			secrets: map[string]string{},
			inputs:  map[string]bool{},
			configs: map[string]map[string]any{},
			names:   map[string]string{},
		},
		namespace:  namespace,
		configMaps: map[string]map[string]string{},
		secretData: map[string]map[string]string{},
		claims:     map[string]int{},
		aliases:    map[string]alias{},
	}
	k.pkg = Package{Type: Type, Version: Version, Name: strings.TrimSpace(name)}
	if k.pkg.Name == "" {
		k.pkg.Name = "Import Helm"
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Package{}, nil, fmt.Errorf("Les manifestes sont illisibles : %s", plainYAMLError(err))
		}
		if doc == nil {
			continue
		}
		if items, ok := doc["items"].([]any); ok && doc["kind"] == "List" {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					k.objects = append(k.objects, m)
				}
			}
			continue
		}
		k.objects = append(k.objects, doc)
	}
	var workloads []workload
	var services, ingresses []map[string]any
	skipped := map[string][]string{}
	for _, obj := range k.objects {
		kind, _ := obj["kind"].(string)
		meta, _ := obj["metadata"].(map[string]any)
		objName := scalar(meta["name"])
		if annotations, ok := meta["annotations"].(map[string]any); ok && annotations["helm.sh/hook"] != nil {
			skipped["hook"] = append(skipped["hook"], kind+"/"+objName)
			continue
		}
		body, _ := obj["spec"].(map[string]any)
		switch kind {
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			template, _ := body["template"].(map[string]any)
			pod, _ := template["spec"].(map[string]any)
			tmeta, _ := template["metadata"].(map[string]any)
			w := workload{kind: kind, name: objName, labels: stringMap(tmeta["labels"]), pod: pod, replicas: 1, templates: map[string]int{}}
			if n, err := strconv.Atoi(scalar(body["replicas"])); err == nil {
				w.replicas = n
			}
			for _, item := range list(body["volumeClaimTemplates"]) {
				claim, _ := item.(map[string]any)
				cmeta, _ := claim["metadata"].(map[string]any)
				w.templates[scalar(cmeta["name"])] = claimSize(claim["spec"])
			}
			if kind == "DaemonSet" {
				k.warn("", "%s tourne sur chaque nœud dans Kubernetes : il devient un service à une copie.", objName)
			}
			if pod != nil {
				workloads = append(workloads, w)
			}
		case "Service":
			services = append(services, obj)
		case "Ingress", "HTTPRoute":
			ingresses = append(ingresses, obj)
		case "ConfigMap":
			k.configMaps[objName] = stringMap(obj["data"])
		case "Secret":
			values := map[string]string{}
			for key, value := range stringMap(obj["data"]) {
				if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
					values[key] = string(decoded)
				}
			}
			for key, value := range stringMap(obj["stringData"]) {
				values[key] = value
			}
			k.secretData[objName] = values
		case "PersistentVolumeClaim":
			k.claims[objName] = claimSize(body)
		case "Namespace":
		default:
			skipped[kind] = append(skipped[kind], objName)
		}
	}
	if len(workloads) == 0 {
		return Package{}, nil, fmt.Errorf("Les manifestes ne décrivent aucun Deployment ni StatefulSet.")
	}
	// Each workload takes the name of the Service in front of it: the
	// addresses the others use stay true.
	names := map[string]bool{}
	for i, w := range workloads {
		base := w.name
		for _, svc := range services {
			sbody, _ := svc["spec"].(map[string]any)
			if scalar(sbody["clusterIP"]) == "None" || !selects(stringMap(sbody["selector"]), w.labels) {
				continue
			}
			smeta, _ := svc["metadata"].(map[string]any)
			base = scalar(smeta["name"])
			break
		}
		name := slugOf(base, "service", 31)
		for n := 2; names[name]; n++ {
			name = slugOf(fmt.Sprintf("%s-%d", base, n), "service", 31)
		}
		names[name] = true
		k.names[w.name] = name
		workloads[i] = w
	}
	for _, w := range workloads {
		k.workload(w, services)
	}
	k.publish(workloads, services, ingresses)
	k.shareDisks()
	k.rename()
	kinds := make([]string, 0, len(skipped))
	for kind := range skipped {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if kind == "hook" {
			k.warn("", "Les hooks et tests Helm ne sont pas repris : %s.", strings.Join(skipped[kind], ", "))
			continue
		}
		k.warn("", "Les objets %s ne sont pas repris : %s.", kind, strings.Join(skipped[kind], ", "))
	}
	return k.pkg, k.warnings, nil
}

func selects(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func stringMap(raw any) map[string]string {
	out := map[string]string{}
	if m, ok := raw.(map[string]any); ok {
		for key, value := range m {
			out[key] = scalar(value)
		}
	}
	return out
}

// claimSize reads the storage a claim asks for, in GiB, 1 at least.
func claimSize(raw any) int {
	body, _ := raw.(map[string]any)
	resources, _ := body["resources"].(map[string]any)
	requests, _ := resources["requests"].(map[string]any)
	mi := mebibytes(strings.TrimSuffix(strings.TrimSuffix(scalar(requests["storage"]), "i"), "I"))
	if mi <= 0 {
		return 1
	}
	gi := (mi + 1023) / 1024
	if gi > 1024 {
		gi = 1024
	}
	return gi
}

// workload converts the containers of one workload: the first keeps its
// name, the others become services of their own.
func (k *kube) workload(w workload, services []map[string]any) {
	volumes := map[string]map[string]any{}
	for _, item := range list(w.pod["volumes"]) {
		if v, ok := item.(map[string]any); ok {
			volumes[scalar(v["name"])] = v
		}
	}
	for _, item := range list(w.pod["initContainers"]) {
		c, _ := item.(map[string]any)
		k.warn(k.names[w.name], "Le conteneur d'initialisation %s n'est pas repris.", scalar(c["name"]))
	}
	for i, item := range list(w.pod["containers"]) {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := k.names[w.name]
		if i > 0 {
			name = slugOf(name+"-"+scalar(c["name"]), "service", 31)
			k.warn(name, "Le conteneur %s partageait le pod de %s : il devient un service à part, joint par son nom.", scalar(c["name"]), k.names[w.name])
		}
		svc := Service{Name: name, Image: scalar(c["image"])}
		if w.replicas > 1 {
			svc.Replicas = min(w.replicas, 10)
		}
		portNames := map[string]int{}
		for _, raw := range list(c["ports"]) {
			p, _ := raw.(map[string]any)
			port, _ := strconv.Atoi(scalar(p["containerPort"]))
			if port < 1 || port > 65535 {
				continue
			}
			portNames[scalar(p["name"])] = port
			if svc.Port == 0 {
				svc.Port = port
			} else if port != svc.Port {
				svc.Ports = append(svc.Ports, spec.Port{Port: port})
			}
		}
		k.command(&svc, c["command"], c["args"])
		k.env(&svc, c)
		k.mounts(&svc, w, c, volumes)
		k.probe(&svc, c, portNames)
		k.resources(&svc, c)
		k.pkg.Services = append(k.pkg.Services, svc)
		if i == 0 {
			k.alias(w, svc, services, portNames)
		}
	}
}

// command keeps command and args; args that do not read as plain words join
// the command.
func (k *kube) command(svc *Service, command, args any) {
	entry := k.words(svc.Name, command)
	rest := k.words(svc.Name, args)
	simple := true
	for _, arg := range rest {
		if !argWord.MatchString(arg) {
			simple = false
		}
	}
	switch {
	case simple:
		svc.Command, svc.Args = entry, rest
	case len(entry) > 0:
		svc.Command = append(entry, rest...)
	default:
		svc.Command = rest
		k.warn(svc.Name, "Les arguments remplacent aussi le point d'entrée de l'image : vérifiez qu'ils se lancent seuls.")
	}
}

// env reads the variables of a container: values, keys of ConfigMaps and of
// Secrets. A Secret's password becomes a secret drawn at installation.
func (k *kube) env(svc *Service, c map[string]any) {
	set := func(key, value string, secret bool) {
		if !envKey.MatchString(key) || strings.ContainsAny(value, "'\r\n") {
			k.warn(svc.Name, "La variable %s n'est pas reprise : son nom ou sa valeur est illisible.", key)
			return
		}
		if secret || strings.Contains(value, "{{secret:") {
			if k.pkg.Env == nil {
				k.pkg.Env = map[string]string{}
			}
			if old, taken := k.pkg.Env[key]; taken && old != value {
				k.warn(svc.Name, "La variable %s porte une autre valeur dans un autre service : vérifiez-la dans les variables communes.", key)
				return
			}
			k.pkg.Env[key] = value
			return
		}
		if svc.Env == nil {
			svc.Env = map[string]string{}
		}
		svc.Env[key] = value
	}
	fromSecret := func(secret, key, env string) (string, bool) {
		value, known := k.secretData[secret][key]
		if Sensitive(key) || Sensitive(env) || !known {
			return "{{secret:" + k.secretFor("secret:"+secret+"/"+key, key) + "}}", true
		}
		return value, true
	}
	for _, raw := range list(c["envFrom"]) {
		from, _ := raw.(map[string]any)
		if ref, ok := from["configMapRef"].(map[string]any); ok {
			for key, value := range k.configMaps[scalar(ref["name"])] {
				set(key, value, false)
			}
		}
		if ref, ok := from["secretRef"].(map[string]any); ok {
			secret := scalar(ref["name"])
			for key := range k.secretData[secret] {
				value, isSecret := fromSecret(secret, key, key)
				set(key, value, isSecret)
			}
		}
	}
	for _, raw := range list(c["env"]) {
		e, _ := raw.(map[string]any)
		key := scalar(e["name"])
		if from, ok := e["valueFrom"].(map[string]any); ok {
			switch {
			case from["secretKeyRef"] != nil:
				ref, _ := from["secretKeyRef"].(map[string]any)
				value, isSecret := fromSecret(scalar(ref["name"]), scalar(ref["key"]), key)
				set(key, value, isSecret)
			case from["configMapKeyRef"] != nil:
				ref, _ := from["configMapKeyRef"].(map[string]any)
				set(key, k.configMaps[scalar(ref["name"])][scalar(ref["key"])], false)
			default:
				k.warn(svc.Name, "La variable %s lit le pod lui-même (fieldRef) : elle n'est pas reprise.", key)
			}
			continue
		}
		value := scalar(e["value"])
		if Sensitive(key) && value != "" {
			set(key, "{{secret:"+k.secretFor("value:"+value, key)+"}}", true)
			continue
		}
		set(key, value, false)
	}
}

// mounts turns claims into disks and ConfigMaps into files; what belongs to
// the node or the cluster is named and left out.
func (k *kube) mounts(svc *Service, w workload, c map[string]any, volumes map[string]map[string]any) {
	for _, raw := range list(c["volumeMounts"]) {
		m, _ := raw.(map[string]any)
		path := scalar(m["mountPath"])
		name := scalar(m["name"])
		sub := scalar(m["subPath"])
		if !spec.PathOK(path) {
			k.warn(svc.Name, "Le volume monté sur %s a un chemin illisible.", path)
			continue
		}
		if size, ok := w.templates[name]; ok {
			svc.Volumes = append(svc.Volumes, spec.Volume{Path: path, SizeGi: size})
			continue
		}
		v := volumes[name]
		switch {
		case v["persistentVolumeClaim"] != nil:
			claim, _ := v["persistentVolumeClaim"].(map[string]any)
			size, ok := k.claims[scalar(claim["claimName"])]
			if !ok {
				size = 1
				k.warn(svc.Name, "La taille du disque %s est inconnue : il fait 1 Gio.", path)
			}
			if !hasVolume(svc.Volumes, path) {
				svc.Volumes = append(svc.Volumes, spec.Volume{Path: path, SizeGi: size})
				k.shared = append(k.shared, composeMount{service: svc.Name, path: path, source: "pvc:" + scalar(claim["claimName"]), sizeGi: size})
			}
		case v["configMap"] != nil:
			ref, _ := v["configMap"].(map[string]any)
			data := k.configMaps[scalar(ref["name"])]
			k.configFiles(svc, path, sub, data, ref["items"])
		case v["secret"] != nil:
			ref, _ := v["secret"].(map[string]any)
			k.warn(svc.Name, "Le secret %s monté sur %s n'est pas repris : ajoutez ses fichiers dans l'onglet Fichiers.", scalar(ref["secretName"]), path)
		case v["emptyDir"] != nil:
		case v["hostPath"] != nil:
			k.warn(svc.Name, "Le dossier du nœud monté sur %s n'est pas repris.", path)
		default:
			k.warn(svc.Name, "Le volume %s monté sur %s n'est pas repris.", name, path)
		}
	}
	if len(svc.Volumes) > 0 && w.kind == "Deployment" && w.replicas > 1 {
		k.warn(svc.Name, "Ses copies partageaient un même disque : chaque copie a maintenant le sien.")
	}
}

func (k *kube) configFiles(svc *Service, path, sub string, data map[string]string, items any) {
	add := func(file, content string) {
		if spec.PathOK(file) {
			svc.Files = append(svc.Files, spec.File{Path: file, Content: content})
		}
	}
	if sub != "" {
		add(path, data[sub])
		return
	}
	if len(list(items)) > 0 {
		for _, raw := range list(items) {
			item, _ := raw.(map[string]any)
			add(strings.TrimSuffix(path, "/")+"/"+scalar(item["path"]), data[scalar(item["key"])])
		}
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(strings.TrimSuffix(path, "/")+"/"+key, data[key])
	}
}

// probe keeps a readiness check that calls HTTP; a command is left to the
// default check on the port.
func (k *kube) probe(svc *Service, c map[string]any, ports map[string]int) {
	for _, key := range []string{"readinessProbe", "livenessProbe", "startupProbe"} {
		p, ok := c[key].(map[string]any)
		if !ok {
			continue
		}
		get, ok := p["httpGet"].(map[string]any)
		if !ok {
			if p["exec"] != nil && key == "readinessProbe" {
				k.warn(svc.Name, "Son contrôle de santé lance une commande : MyInfra vérifie que le port répond.")
			}
			continue
		}
		health := &spec.Health{HTTP: scalar(get["path"])}
		if health.HTTP == "" {
			health.HTTP = "/"
		}
		port, err := strconv.Atoi(scalar(get["port"]))
		if err != nil {
			port = ports[scalar(get["port"])]
		}
		if port > 0 && port != svc.Port {
			health.Port = port
		}
		if !httpPath.MatchString(health.HTTP) {
			continue
		}
		svc.Health = health
		return
	}
}

var httpPath = regexp.MustCompile(`^/[A-Za-z0-9._~/%?=&+-]{0,255}$`)

func (k *kube) resources(svc *Service, c map[string]any) {
	resources, _ := c["resources"].(map[string]any)
	var cpu, memory string
	for _, part := range []string{"limits", "requests"} {
		values, _ := resources[part].(map[string]any)
		if v := scalar(values["cpu"]); v != "" {
			cpu = v
		}
		if v := scalar(values["memory"]); v != "" {
			memory = v
		}
	}
	out := &spec.Resources{}
	if cpuValue.MatchString(cpu) {
		out.CPU = cpu
	}
	if memoryValue.MatchString(memory) {
		out.Memory = memory
	} else if mi := mebibytes(strings.TrimSuffix(memory, "i")); mi > 0 {
		out.Memory = strconv.Itoa(mi) + "Mi"
	}
	if out.CPU != "" || out.Memory != "" {
		svc.Resources = out
	}
}

var (
	cpuValue    = regexp.MustCompile(`^([0-9]{1,3}(\.[0-9]{1,3})?|[0-9]{1,6}m)$`)
	memoryValue = regexp.MustCompile(`^[0-9]{1,6}(Mi|Gi)$`)
)

// alias remembers the Services a workload answers behind, to point their
// names at the MyInfra service.
func (k *kube) alias(w workload, svc Service, services []map[string]any, ports map[string]int) {
	for _, s := range services {
		sbody, _ := s["spec"].(map[string]any)
		if !selects(stringMap(sbody["selector"]), w.labels) {
			continue
		}
		smeta, _ := s["metadata"].(map[string]any)
		a := alias{service: svc.Name}
		for _, raw := range list(sbody["ports"]) {
			p, _ := raw.(map[string]any)
			a.port, _ = strconv.Atoi(scalar(p["port"]))
			if a.target, _ = strconv.Atoi(scalar(p["targetPort"])); a.target == 0 {
				a.target = ports[scalar(p["targetPort"])]
			}
			if a.target == 0 {
				a.target = a.port
			}
			break
		}
		k.aliases[scalar(smeta["name"])] = a
	}
}

// publish gives the domain to what an Ingress leads to, else to the
// workload behind a LoadBalancer, else to the first one.
func (k *kube) publish(workloads []workload, services []map[string]any, ingresses []map[string]any) {
	public := ""
	for _, ing := range ingresses {
		if public != "" {
			break
		}
		body, _ := ing["spec"].(map[string]any)
		for _, raw := range list(body["rules"]) {
			rule, _ := raw.(map[string]any)
			http, _ := rule["http"].(map[string]any)
			for _, rawPath := range list(http["paths"]) {
				path, _ := rawPath.(map[string]any)
				backend, _ := path["backend"].(map[string]any)
				service, _ := backend["service"].(map[string]any)
				if a, ok := k.aliases[scalar(service["name"])]; ok && public == "" {
					public = a.service
					k.publicPort(a)
				}
			}
		}
		for _, raw := range list(body["rules"]) {
			rule, _ := raw.(map[string]any)
			for _, ref := range list(rule["backendRefs"]) {
				r, _ := ref.(map[string]any)
				if a, ok := k.aliases[scalar(r["name"])]; ok && public == "" {
					public = a.service
					k.publicPort(a)
				}
			}
		}
	}
	if public == "" {
		for _, s := range services {
			sbody, _ := s["spec"].(map[string]any)
			kind := scalar(sbody["type"])
			smeta, _ := s["metadata"].(map[string]any)
			if a, ok := k.aliases[scalar(smeta["name"])]; ok && (kind == "LoadBalancer" || kind == "NodePort") {
				public = a.service
				k.publicPort(a)
				break
			}
		}
	}
	if public == "" {
		public = k.pkg.Services[0].Name
		k.warn(public, "Aucun Ingress n'indique le service public : %s reçoit le domaine. Changez-le au besoin.", public)
	}
	for i := range k.pkg.Services {
		if k.pkg.Services[i].Name == public {
			k.pkg.Services[i].Public = true
		}
	}
}

func (k *kube) publicPort(a alias) {
	for i := range k.pkg.Services {
		svc := &k.pkg.Services[i]
		if svc.Name != a.service || a.target == 0 || a.target == svc.Port {
			continue
		}
		var kept []spec.Port
		for _, port := range svc.Ports {
			if port.Port != a.target {
				kept = append(kept, port)
			}
		}
		if svc.Port > 0 {
			kept = append([]spec.Port{{Port: svc.Port}}, kept...)
		}
		svc.Port, svc.Ports = a.target, kept
	}
}

// rename points the addresses of Kubernetes Services at the MyInfra
// services, in variables and files: wiki-postgresql.ns.svc.cluster.local
// becomes the service's name, and a port the Service moved is named.
func (k *kube) rename() {
	type swap struct{ from, to string }
	var swaps []swap
	names := make([]string, 0, len(k.aliases))
	for name := range k.aliases {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, name := range names {
		a := k.aliases[name]
		for _, long := range []string{
			name + "." + k.namespace + ".svc.cluster.local",
			name + "." + k.namespace + ".svc",
			name + "." + k.namespace,
		} {
			swaps = append(swaps, swap{long, a.service})
		}
		if a.port != 0 && a.target != 0 && a.port != a.target {
			k.warn(a.service, "Les autres services le joignaient sur le port %d : c'est maintenant %d.", a.port, a.target)
		}
	}
	replace := func(value string) string {
		for _, s := range swaps {
			value = strings.ReplaceAll(value, s.from, s.to)
		}
		if a, ok := k.aliases[value]; ok {
			return a.service
		}
		return value
	}
	for i := range k.pkg.Services {
		svc := &k.pkg.Services[i]
		for key, value := range svc.Env {
			svc.Env[key] = replace(value)
		}
		for j := range svc.Files {
			svc.Files[j].Content = replace(svc.Files[j].Content)
		}
	}
	for key, value := range k.pkg.Env {
		k.pkg.Env[key] = replace(value)
	}
	used := map[string]bool{}
	for _, svc := range k.pkg.Services {
		used[svc.Name] = true
	}
	for name, a := range k.aliases {
		if !used[name] && name != a.service {
			k.warn(a.service, "Le nom %s de Kubernetes devient %s : une adresse écrite autrement dans l'application est à revoir.", name, a.service)
		}
	}
}
