package pack

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"myinfra/internal/spec"
)

// Warning is something of the source a package could not keep as it was:
// the person reads it before keeping the package.
type Warning struct {
	Service string `json:"service,omitempty"`
	Message string `json:"message"`
}

// Detect tells what a text is: "package", "compose", "kubernetes", or ""
// when it is none of them.
func Detect(data []byte) string {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil || root == nil {
		return ""
	}
	if root["type"] == Type {
		return "package"
	}
	if _, ok := root["services"].(map[string]any); ok && root["type"] == nil {
		return "compose"
	}
	if root["apiVersion"] != nil && root["kind"] != nil {
		return "kubernetes"
	}
	return ""
}

var (
	variable  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?-)([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	localURL  = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::(\d+))?(/[^\s"']*)?`)
	sizeValue = regexp.MustCompile(`^(?i)([0-9]+(?:\.[0-9]+)?)\s*([kmgt]?)(?:i?b)?$`)
	plainCPU  = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3})?$`)
)

// composer turns a docker-compose file into a package, and says what it
// left out.
type composer struct {
	pkg      Package
	warnings []Warning
	// secrets names each password once: the same value, or the same
	// variable, in two services is the same secret.
	secrets map[string]string
	inputs  map[string]bool
	configs map[string]map[string]any
	names   map[string]string
	// mounts are the named volumes and folders each service mounts: one
	// mounted by several services becomes a disk they share.
	shared []composeMount
}

type composeMount struct {
	service, path, source string
	// sizeGi is the size the source gave, 0 when it gave none.
	sizeGi int
}

// Compose reads a docker-compose file. name is the package's name when the
// file gives none.
func Compose(data []byte, name string) (Package, []Warning, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Package{}, nil, fmt.Errorf("Le fichier compose est illisible : %s", plainYAMLError(err))
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return Package{}, nil, fmt.Errorf("Le fichier compose n'a pas de services.")
	}
	doc := root.Content[0]
	var top map[string]any
	if err := doc.Decode(&top); err != nil {
		return Package{}, nil, fmt.Errorf("Le fichier compose est illisible : %s", plainYAMLError(err))
	}
	services := child(doc, "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 {
		return Package{}, nil, fmt.Errorf("Le fichier compose n'a pas de services.")
	}
	c := &composer{
		secrets: map[string]string{},
		inputs:  map[string]bool{},
		configs: map[string]map[string]any{},
		names:   map[string]string{},
	}
	c.pkg = Package{Type: Type, Version: Version, Name: strings.TrimSpace(name)}
	if title, ok := top["name"].(string); ok && strings.TrimSpace(title) != "" {
		c.pkg.Name = strings.TrimSpace(title)
	}
	if c.pkg.Name == "" {
		c.pkg.Name = "Import docker-compose"
	}
	if configs, ok := top["configs"].(map[string]any); ok {
		for key, value := range configs {
			if m, ok := value.(map[string]any); ok {
				c.configs[key] = m
			}
		}
	}
	for _, key := range []string{"secrets", "x-secrets"} {
		if _, ok := top[key]; ok {
			c.warn("", "Les secrets Docker ne sont pas repris : placez leurs valeurs dans les variables communes, avec {{secret:nom}}.")
		}
	}
	type raw struct {
		name string
		body map[string]any
	}
	var list []raw
	for i := 0; i+1 < len(services.Content); i += 2 {
		var body map[string]any
		if err := services.Content[i+1].Decode(&body); err != nil {
			return Package{}, nil, fmt.Errorf("Le service %s est illisible.", services.Content[i].Value)
		}
		list = append(list, raw{name: services.Content[i].Value, body: body})
		c.names[services.Content[i].Value] = c.serviceName(services.Content[i].Value)
	}
	published := map[string]int{}
	opened := map[string][]mapping{}
	for _, item := range list {
		svc, open := c.service(item.name, item.body)
		c.pkg.Services = append(c.pkg.Services, svc)
		opened[svc.Name] = open
		for _, port := range open {
			if port.proto != "tcp" {
				continue
			}
			if _, seen := published[svc.Name]; !seen || port.published == 80 || port.published == 443 {
				published[svc.Name] = port.published
			}
		}
	}
	c.choosePublic(published)
	c.exposePublished(opened)
	c.shareDisks()
	return c.pkg, c.warnings, nil
}

// shareDisks turns a volume or a folder several services mount into a disk
// they share, so that what one writes, the others read.
func (c *composer) shareDisks() {
	users := map[string][]composeMount{}
	var order []string
	for _, m := range c.shared {
		if _, seen := users[m.source]; !seen {
			order = append(order, m.source)
		}
		users[m.source] = append(users[m.source], m)
	}
	taken := map[string]bool{}
	for _, source := range order {
		mounts := users[source]
		services := map[string]bool{}
		for _, m := range mounts {
			services[m.service] = true
		}
		if len(services) < 2 || len(c.pkg.Disks) == maxDisks {
			continue
		}
		name := slugOf(strings.TrimLeft(source[strings.Index(source, ":")+1:], "./~$"), "data", 31)
		for base, i := name, 2; taken[name]; i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		taken[name] = true
		size := 0
		for _, m := range mounts {
			size = max(size, m.sizeGi)
		}
		if size == 0 {
			size = 10
		}
		c.pkg.Disks = append(c.pkg.Disks, SharedDisk{Name: name, SizeGi: size})
		var names []string
		for i := range c.pkg.Services {
			svc := &c.pkg.Services[i]
			for j := range svc.Volumes {
				for _, m := range mounts {
					if m.service == svc.Name && m.path == svc.Volumes[j].Path {
						svc.Volumes[j].Disk, svc.Volumes[j].SizeGi = name, 0
						if svc.Replicas > 1 {
							svc.Replicas = 1
						}
						names = append(names, svc.Name)
					}
				}
			}
		}
		c.warn("", "Les services %s montent le même volume : ils partagent le disque %s, de %d Gio. Ils tournent en une copie, sur le même nœud.", strings.Join(names, ", "), name, size)
	}
}

func (c *composer) warn(service, format string, args ...any) {
	c.warnings = append(c.warnings, Warning{Service: service, Message: fmt.Sprintf(format, args...)})
}

// serviceName makes a compose name a MyInfra one: lower case, dashes.
func (c *composer) serviceName(raw string) string {
	return slugOf(raw, "service", 31)
}

func slugOf(raw, fallback string, max int) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteRune('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = fallback
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = fallback + "-" + out
	}
	if len(out) > max {
		out = strings.Trim(out[:max], "-")
	}
	return out
}

// mapping is a port of a compose service: published on the machine when
// published is set.
type mapping struct {
	published, target int
	proto             string
}

// service converts one compose service; open lists the ports it publishes
// on the machine, to find the public one.
func (c *composer) service(raw string, body map[string]any) (Service, []mapping) {
	name := c.names[raw]
	svc := Service{Name: name}
	if name != raw {
		c.warn(name, "Le service %s s'appelle %s : un nom est en minuscules, avec des tirets.", raw, name)
	}
	image := c.literal(name, scalar(body["image"]), "l'image")
	if _, builds := body["build"]; builds {
		if image == "" {
			c.warn(name, "Ce service se construit depuis ses sources (build:) : MyInfra ne construit pas d'image. Publiez-la dans un registre et indiquez-la ici.")
		} else {
			c.warn(name, "La section build: est ignorée : l'image %s est utilisée telle quelle.", image)
		}
	}
	svc.Image = image

	// Ports: those published on the machine, and those only exposed.
	var open []mapping
	var targets []mapping
	add := func(m mapping) {
		for _, seen := range targets {
			if seen.target == m.target && seen.proto == m.proto {
				return
			}
		}
		targets = append(targets, mapping{target: m.target, proto: m.proto})
	}
	for _, item := range list(body["ports"]) {
		for _, m := range c.port(name, item) {
			if m.published > 0 {
				open = append(open, m)
			}
			add(m)
		}
	}
	for _, item := range list(body["expose"]) {
		text, proto := splitProto(scalar(item))
		if n, err := strconv.Atoi(text); err == nil && n > 0 && n < 65536 {
			add(mapping{target: n, proto: proto})
		}
	}
	for _, m := range targets {
		if svc.Port == 0 && m.proto == "tcp" {
			svc.Port = m.target
			continue
		}
		port := spec.Port{Port: m.target}
		if m.proto != "tcp" {
			port.Protocol = m.proto
		}
		svc.Ports = append(svc.Ports, port)
	}

	c.environment(&svc, body["environment"])
	if _, ok := body["env_file"]; ok {
		c.warn(name, "Le fichier env_file n'est pas lu : recopiez ses variables dans celles du service ou dans les variables communes.")
	}
	c.volumes(&svc, body["volumes"])
	c.serviceConfigs(&svc, body["configs"])
	c.command(&svc, body["entrypoint"], body["command"])
	c.health(&svc, body["healthcheck"])
	c.deploy(&svc, body)
	for _, dep := range dependencies(body["depends_on"]) {
		if target, ok := c.names[dep]; ok && target != name {
			svc.DependsOn = appendUniqueString(svc.DependsOn, target)
		}
	}
	for key, message := range map[string]string{
		"privileged":   "Le mode privileged n'est pas repris.",
		"network_mode": "Le mode réseau n'est pas repris : les services se joignent par leur nom.",
		"devices":      "Les périphériques de l'hôte ne sont pas repris.",
		"cap_add":      "Les capacités Linux ajoutées ne sont pas reprises.",
		"user":         "L'utilisateur du conteneur n'est pas repris : celui de l'image sert.",
		"working_dir":  "Le dossier de travail n'est pas repris : celui de l'image sert.",
		"secrets":      "Les secrets Docker du service ne sont pas repris.",
		"extra_hosts":  "Les noms d'hôtes ajoutés ne sont pas repris.",
	} {
		if _, ok := body[key]; ok {
			c.warn(name, "%s", message)
		}
	}
	return svc, open
}

// maxRange is the widest range of ports a compose service may publish.
const maxRange = 16

// port reads "8080:80", "127.0.0.1:8080:80/tcp", "53:53/udp", "80", a range
// such as "21115-21119:21115-21119", or the long syntax.
func (c *composer) port(service string, item any) []mapping {
	if m, isMap := item.(map[string]any); isMap {
		target, _ := strconv.Atoi(c.literal(service, scalar(m["target"]), "un port"))
		published, _ := strconv.Atoi(c.literal(service, scalar(m["published"]), "un port"))
		proto := strings.ToLower(scalar(m["protocol"]))
		if proto != "udp" {
			proto = "tcp"
		}
		if target < 1 || target > 65535 {
			return nil
		}
		return []mapping{{published: published, target: target, proto: proto}}
	}
	raw := c.literal(service, scalar(item), "un port")
	text, proto := splitProto(raw)
	parts := strings.Split(text, ":")
	targets, ok := portRange(parts[len(parts)-1])
	if !ok {
		c.warn(service, "Le port %s est illisible.", raw)
		return nil
	}
	if len(targets) > maxRange {
		c.warn(service, "La plage de ports %s compte plus de %d ports : elle n'est pas reprise.", raw, maxRange)
		return nil
	}
	var published []int
	if len(parts) >= 2 && parts[len(parts)-2] != "" {
		published, ok = portRange(parts[len(parts)-2])
		if !ok || len(published) != len(targets) {
			c.warn(service, "Le port %s est illisible.", raw)
			return nil
		}
	}
	out := make([]mapping, len(targets))
	for i, target := range targets {
		out[i] = mapping{target: target, proto: proto}
		if published != nil {
			out[i].published = published[i]
		}
	}
	return out
}

// splitProto reads "53/udp" as 53 and udp; tcp when unsaid.
func splitProto(text string) (string, string) {
	text, proto, _ := strings.Cut(strings.TrimSpace(text), "/")
	if strings.ToLower(proto) == "udp" {
		return text, "udp"
	}
	return text, "tcp"
}

// portRange reads "80" or "21115-21119".
func portRange(text string) ([]int, bool) {
	low, high, isRange := strings.Cut(text, "-")
	first, err := strconv.Atoi(low)
	if err != nil || first < 1 || first > 65535 {
		return nil, false
	}
	last := first
	if isRange {
		last, err = strconv.Atoi(high)
		if err != nil || last < first || last > 65535 || last-first >= maxRange*4 {
			return nil, false
		}
	}
	out := make([]int, 0, last-first+1)
	for port := first; port <= last; port++ {
		out = append(out, port)
	}
	return out, true
}

// environment splits the variables: passwords and keys become secrets drawn
// at installation, in the common variables; the others stay the service's.
func (c *composer) environment(svc *Service, raw any) {
	values := map[string]string{}
	switch env := raw.(type) {
	case map[string]any:
		for key, value := range env {
			values[key] = scalar(value)
		}
	case []any:
		for _, item := range env {
			key, value, found := strings.Cut(scalar(item), "=")
			if !found {
				value = "${" + key + "}"
			}
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !envKey.MatchString(key) {
			c.warn(svc.Name, "La variable %s a un nom illisible : elle n'est pas reprise.", key)
			continue
		}
		value := c.value(key, values[key])
		if strings.ContainsAny(value, "'\r\n") {
			c.warn(svc.Name, "La variable %s contient une apostrophe ou un retour à la ligne : elle n'est pas reprise.", key)
			continue
		}
		if !strings.Contains(value, "{{secret:") && !strings.Contains(value, "{{encoded:") {
			if svc.Env == nil {
				svc.Env = map[string]string{}
			}
			svc.Env[key] = value
			continue
		}
		// A secret goes to the common variables, sealed.
		if c.pkg.Env == nil {
			c.pkg.Env = map[string]string{}
		}
		if old, taken := c.pkg.Env[key]; taken && old != value {
			c.warn(svc.Name, "La variable %s porte un autre secret dans un autre service : vérifiez-la dans les variables communes.", key)
			continue
		}
		c.pkg.Env[key] = value
	}
}

// value fills the ${…} of a compose value: its default when it has one, a
// secret for a password, a question for the rest. A password written as it
// is becomes a secret drawn at installation.
func (c *composer) value(key, value string) string {
	if Sensitive(key) && value != "" && !strings.Contains(value, "$") {
		return "{{secret:" + c.secretFor("value:"+value, key) + "}}"
	}
	return interpolate(value, func(match string) string {
		parts := variable.FindStringSubmatch(match)
		name := parts[1]
		if name == "" {
			name = parts[4]
		}
		if parts[2] != "" && parts[3] != "" {
			return parts[3]
		}
		if Sensitive(name) || Sensitive(key) && value == match {
			return "{{secret:" + c.secretFor("variable:"+name, name) + "}}"
		}
		return "{{input:" + c.input(name) + "}}"
	})
}

// interpolate replaces each ${…} of a compose value; $$ is a dollar
// written as it is.
func interpolate(value string, each func(string) string) string {
	parts := strings.Split(value, "$$")
	for i, part := range parts {
		parts[i] = variable.ReplaceAllStringFunc(part, each)
	}
	return strings.Join(parts, "$")
}

func (c *composer) secretFor(identity, name string) string {
	if found, ok := c.secrets[identity]; ok {
		return found
	}
	base := slugOf(name, "secret", 40)
	out := base
	for n := 2; c.taken(out); n++ {
		out = fmt.Sprintf("%s-%d", base, n)
	}
	c.secrets[identity] = out
	return out
}

func (c *composer) taken(name string) bool {
	for _, used := range c.secrets {
		if used == name {
			return true
		}
	}
	return false
}

func (c *composer) input(name string) string {
	key := slugOf(name, "valeur", 40)
	if !c.inputs[key] {
		c.inputs[key] = true
		c.pkg.Inputs = append(c.pkg.Inputs, Input{Key: key, Label: name})
	}
	return key
}

// literal is a value that cannot ask anything: its ${…} take their default,
// or stay out with a warning.
func (c *composer) literal(service, value, what string) string {
	if !strings.Contains(value, "$") {
		return value
	}
	out := interpolate(value, func(match string) string {
		parts := variable.FindStringSubmatch(match)
		if parts[2] != "" {
			return parts[3]
		}
		c.warn(service, "%s dépend de la variable %s, sans valeur par défaut : indiquez-la.", strings.ToUpper(what[:1])+what[1:], match)
		return ""
	})
	return out
}

// volumes keeps named volumes as disks, a mounted file as a file to fill
// in, and leaves out what belongs to the machine.
func (c *composer) volumes(svc *Service, raw any) {
	for _, item := range list(raw) {
		var kind, source, target string
		if m, ok := item.(map[string]any); ok {
			kind, source, target = scalar(m["type"]), scalar(m["source"]), scalar(m["target"])
		} else {
			parts := strings.Split(scalar(item), ":")
			switch len(parts) {
			case 1:
				target = parts[0]
			default:
				source, target = parts[0], parts[1]
			}
			kind = "volume"
			// A path, or a variable that holds one, is a folder of the machine.
			if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") || strings.HasPrefix(source, "$") {
				kind = "bind"
			}
		}
		target = c.literal(svc.Name, target, "un volume")
		if !spec.PathOK(target) {
			c.warn(svc.Name, "Le volume monté sur %s a un chemin illisible : il n'est pas repris.", target)
			continue
		}
		switch {
		case kind == "tmpfs":
			continue
		case kind == "bind" && machinePath(source):
			c.warn(svc.Name, "Le service lit %s de la machine : ce n'est pas repris.", source)
			continue
		case kind == "bind" && looksLikeFile(source, target):
			svc.Files = append(svc.Files, spec.File{Path: target})
			c.warn(svc.Name, "Le fichier %s de la machine est monté sur %s : recopiez son contenu dans l'onglet Fichiers.", source, target)
			continue
		case kind == "bind":
			c.warn(svc.Name, "Le dossier %s de la machine devient un disque vide sur %s : ses fichiers ne sont pas copiés.", source, target)
		}
		if hasVolume(svc.Volumes, target) {
			continue
		}
		if source != "" {
			c.shared = append(c.shared, composeMount{service: svc.Name, path: target, source: kind + ":" + source})
		}
		svc.Volumes = append(svc.Volumes, spec.Volume{Path: target, SizeGi: 1})
	}
	if len(svc.Volumes) > 0 {
		c.warn(svc.Name, "Chaque disque fait 1 Gio : ajustez la taille dans l'onglet Stockage avant d'installer.")
	}
}

// machinePath is a file of the machine itself, not of the application:
// its clock, its Docker socket, its devices.
func machinePath(source string) bool {
	for _, prefix := range []string{"/etc/", "/var/run/", "/run/", "/sys", "/proc", "/dev", "/usr/", "/lib"} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func looksLikeFile(source, target string) bool {
	base := target[strings.LastIndex(target, "/")+1:]
	src := source[strings.LastIndex(source, "/")+1:]
	return strings.Contains(base, ".") && !strings.HasPrefix(base, ".") || strings.Contains(src, ".") && !strings.HasPrefix(src, ".")
}

func hasVolume(volumes []spec.Volume, path string) bool {
	for _, volume := range volumes {
		if volume.Path == path {
			return true
		}
	}
	return false
}

// serviceConfigs turns compose configs into files: their content when the
// file holds it, else a file to fill in.
func (c *composer) serviceConfigs(svc *Service, raw any) {
	for _, item := range list(raw) {
		source, target := scalar(item), ""
		if m, ok := item.(map[string]any); ok {
			source, target = scalar(m["source"]), scalar(m["target"])
		}
		if target == "" {
			target = "/" + source
		}
		if !spec.PathOK(target) {
			c.warn(svc.Name, "La configuration %s a un chemin illisible.", source)
			continue
		}
		config := c.configs[source]
		content, ok := config["content"].(string)
		if !ok {
			c.warn(svc.Name, "La configuration %s vient d'un fichier : recopiez son contenu dans %s.", source, target)
		}
		svc.Files = append(svc.Files, spec.File{Path: target, Content: content})
	}
}

// command keeps an entrypoint as the command, and a command as arguments
// when each word reads as one; otherwise the whole line becomes the command.
func (c *composer) command(svc *Service, entrypoint, command any) {
	entry := c.words(svc.Name, entrypoint)
	args := c.words(svc.Name, command)
	simple := true
	for _, arg := range args {
		if !argWord.MatchString(arg) {
			simple = false
		}
	}
	switch {
	case len(entry) > 0 && simple:
		svc.Command, svc.Args = entry, args
	case len(entry) > 0:
		svc.Command = append(entry, args...)
	case simple:
		svc.Args = args
	default:
		svc.Command = args
		c.warn(svc.Name, "La commande remplace aussi le point d'entrée de l'image : vérifiez qu'elle se lance seule.")
	}
}

func (c *composer) words(service string, raw any) []string {
	switch value := raw.(type) {
	case string:
		return splitWords(c.literal(service, value, "la commande"))
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, c.literal(service, scalar(item), "la commande"))
		}
		return out
	}
	return nil
}

// splitWords reads a line as a shell would: spaces apart, quotes keeping a
// phrase whole.
func splitWords(line string) []string {
	var out []string
	var current strings.Builder
	quote := rune(0)
	started := false
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			current.WriteRune(r)
		case r == '"' || r == '\'':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n':
			if started || current.Len() > 0 {
				out = append(out, current.String())
			}
			current.Reset()
			started = false
		default:
			current.WriteRune(r)
		}
	}
	if started || current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// health keeps a check that calls the service on HTTP; another kind of
// check is left to the default one.
func (c *composer) health(svc *Service, raw any) {
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if disabled, _ := m["disable"].(bool); disabled {
		return
	}
	var line string
	switch test := m["test"].(type) {
	case string:
		line = test
	case []any:
		var parts []string
		for _, item := range test {
			parts = append(parts, scalar(item))
		}
		line = strings.Join(parts, " ")
	}
	found := localURL.FindStringSubmatch(line)
	if found == nil {
		if line != "" {
			c.warn(svc.Name, "Le contrôle de santé « %s » n'est pas repris : MyInfra vérifie que le port répond.", strings.TrimPrefix(strings.TrimPrefix(line, "CMD-SHELL "), "CMD "))
		}
		return
	}
	health := &spec.Health{HTTP: found[2]}
	if health.HTTP == "" {
		health.HTTP = "/"
	}
	if port, err := strconv.Atoi(found[1]); err == nil && port != svc.Port {
		health.Port = port
	}
	svc.Health = health
}

// deploy reads copies and limits, from deploy: or the older keys.
func (c *composer) deploy(svc *Service, body map[string]any) {
	deploy, _ := body["deploy"].(map[string]any)
	if n, err := strconv.Atoi(scalar(deploy["replicas"])); err == nil && n > 1 {
		if n > 10 {
			n = 10
		}
		svc.Replicas = n
	}
	var cpus, memory string
	if resources, ok := deploy["resources"].(map[string]any); ok {
		for _, part := range []string{"reservations", "limits"} {
			if values, ok := resources[part].(map[string]any); ok {
				if v := scalar(values["cpus"]); v != "" {
					cpus = v
				}
				if v := scalar(values["memory"]); v != "" {
					memory = v
				}
			}
		}
	}
	if v := scalar(body["cpus"]); v != "" {
		cpus = v
	}
	if v := scalar(body["mem_limit"]); v != "" {
		memory = v
	}
	if cpus == "" && memory == "" {
		return
	}
	resources := &spec.Resources{}
	if plainCPU.MatchString(cpus) {
		resources.CPU = cpus
	}
	if mi := mebibytes(memory); mi > 0 {
		resources.Memory = strconv.Itoa(mi) + "Mi"
	} else if memory != "" {
		c.warn(svc.Name, "La mémoire %s est illisible : indiquez-la dans l'onglet Ressources.", memory)
	}
	if resources.CPU != "" || resources.Memory != "" {
		svc.Resources = resources
	}
}

// mebibytes reads a Docker size, 512m or 1.5G, in MiB.
func mebibytes(value string) int {
	found := sizeValue.FindStringSubmatch(strings.TrimSpace(value))
	if found == nil {
		return 0
	}
	n, err := strconv.ParseFloat(found[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(found[2]) {
	case "":
		n /= 1 << 20
	case "k":
		n /= 1 << 10
	case "g":
		n *= 1 << 10
	case "t":
		n *= 1 << 20
	}
	return int(math.Ceil(n))
}

// choosePublic makes public the service the visitors reach: the one that
// publishes 80 or 443, else the first that publishes a port.
func (c *composer) choosePublic(published map[string]int) {
	best := -1
	for i, svc := range c.pkg.Services {
		port, ok := published[svc.Name]
		if !ok {
			continue
		}
		if best < 0 || port == 80 || port == 443 {
			if best < 0 || published[c.pkg.Services[best].Name] != 80 && published[c.pkg.Services[best].Name] != 443 {
				best = i
			}
		}
	}
	if best < 0 {
		for i, svc := range c.pkg.Services {
			if svc.Port > 0 {
				best = i
				break
			}
		}
		if best < 0 {
			best = 0
		}
		c.warn(c.pkg.Services[best].Name, "Aucun service ne publie de port sur la machine : %s reçoit le domaine. Changez-le au besoin.", c.pkg.Services[best].Name)
	}
	c.pkg.Services[best].Public = true
	if c.pkg.Services[best].Port == 0 {
		c.warn(c.pkg.Services[best].Name, "Le port du service public n'est pas indiqué : celui que l'image annonce servira.")
	}
}

// databasePorts are published in a compose file to reach the base from the
// machine: never opened to the Internet.
var databasePorts = map[int]bool{3306: true, 5432: true, 6379: true, 27017: true, 9200: true, 11211: true, 5672: true}

// exposePublished opens to the Internet the ports a compose file publishes
// on the machine, besides the one the domain leads to: a VPN, a relay.
func (c *composer) exposePublished(opened map[string][]mapping) {
	var exposed []string
	for i := range c.pkg.Services {
		svc := &c.pkg.Services[i]
		for _, m := range opened[svc.Name] {
			label := fmt.Sprintf("%d/%s", m.published, m.proto)
			if m.proto == "tcp" && m.target == portOf(*svc) && svc.Public {
				continue
			}
			if databasePorts[m.target] {
				c.warn(svc.Name, "Le port %s n'est pas ouvert sur Internet : il reste aux services de l'application.", label)
				continue
			}
			if spec.Reserved(m.published, m.proto) {
				c.warn(svc.Name, "Le port %s est celui de MyInfra sur les machines : il n'est pas ouvert sur Internet.", label)
				continue
			}
			found := false
			for j := range svc.Ports {
				if svc.Ports[j].Port == m.target && svc.Ports[j].Proto() == m.proto {
					svc.Ports[j].Expose, found = m.published, true
				}
			}
			if !found {
				// The service's own port, opened as it is.
				svc.Ports = append(svc.Ports, spec.Port{Port: m.target, Expose: m.published})
			}
			exposed = append(exposed, svc.Name+" "+label)
		}
		if svc.Public && len(spec.Exposed(svc.Service)) > 0 {
			c.warn(svc.Name, "%s reçoit le domaine sur son port %d. S'il ne parle pas HTTP, retirez public et ouvrez ce port avec expose.", svc.Name, portOf(*svc))
		}
	}
	if len(exposed) > 0 {
		c.warn("", "Ces ports sont ouverts sur Internet, sur l'adresse des machines : %s. Retirez expose de ceux qui doivent rester privés.", strings.Join(exposed, ", "))
	}
}

func child(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func dependencies(raw any) []string {
	switch deps := raw.(type) {
	case []any:
		out := make([]string, 0, len(deps))
		for _, item := range deps {
			out = append(out, scalar(item))
		}
		return out
	case map[string]any:
		out := make([]string, 0, len(deps))
		for key := range deps {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	}
	return nil
}

func list(raw any) []any {
	items, _ := raw.([]any)
	return items
}

// scalar writes a YAML value as compose reads it: numbers and booleans as
// text.
func scalar(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case int:
		return strconv.Itoa(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func appendUnique(list []int, n int) []int {
	for _, item := range list {
		if item == n {
			return list
		}
	}
	return append(list, n)
}

func appendUniqueString(list []string, s string) []string {
	for _, item := range list {
		if item == s {
			return list
		}
	}
	return append(list, s)
}
