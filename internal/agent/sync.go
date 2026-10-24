package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"myinfra/internal/cert"
	"myinfra/internal/firewall"
	"myinfra/internal/imagefile"
	"myinfra/internal/pause"
	"myinfra/internal/spec"
)

// proxyImage must negotiate the Docker API: Docker Engine 29 refuses the old
// client version of Traefik before 3.6.1, which then sees no container.
const proxyImage = "traefik:v3.6.2"

// ContainerPort is the HTTP port the proxy uses to reach an application
// on the private network. The person never chooses it.
const ContainerPort = 80

var (
	imagePattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,200}(@sha256:[a-f0-9]{64})?$`)
	idPattern       = regexp.MustCompile(`^[a-zA-Z0-9-]{8,80}$`)
	servicePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	hostPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	envKey          = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	diskPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	argPattern      = regexp.MustCompile(`^[A-Za-z0-9_.,:/=@+-]+$`)
)

type Workload struct {
	ID        string
	Service   string
	Image     string
	Hosts     []string
	Zones     map[string]string
	Env       map[string]string
	Disk      bool
	DiskPath  string
	DiskGi    int
	Listen    int
	Args      []string
	Published bool
	Replicas  int
	Paused    bool
	// Login opens the private registry the image comes from.
	Login *Login
	// Spec is what the service runs with besides the fields above.
	Spec spec.Service
}

// Login is what docker login needs for one registry.
type Login struct {
	Host     string
	User     string
	Password string
}

// Install prepares a machine alone: firewall, Docker and the proxy.
// It can run again: each part checks what is already there.
func Install() (string, error) {
	var b strings.Builder
	b.WriteString("export PATH=\"/usr/local/bin:/usr/bin:/bin:$PATH\"\n")
	guard, err := hostFirewall()
	if err != nil {
		return "", err
	}
	b.WriteString(guard)
	b.WriteString("if ! command -v docker >/dev/null 2>&1; then\n  curl -fsSL https://get.docker.com | sh\nfi\n")
	b.WriteString(ensureProxy(nil))
	return b.String(), nil
}

func Bootstrap(baseURL, token string) (string, error) {
	base, token, err := cleanEndpoint(baseURL, token)
	if err != nil {
		return "", err
	}
	syncURL := base + "/agent/sync"
	report := base + "/api/join"
	var b strings.Builder
	install, err := Install()
	if err != nil {
		return "", err
	}
	b.WriteString(install)
	fmt.Fprintf(&b, "curl -fsS -X POST '%s' -H 'Authorization: Bearer %s' -H 'content-type: application/json' -d '{}'\n", report, token)
	fmt.Fprintf(&b, "if command -v systemctl >/dev/null 2>&1; then\n")
	fmt.Fprintf(&b, "cat > /etc/systemd/system/myinfra-sync.service <<'EOF'\n")
	b.WriteString("[Unit]\nDescription=MyInfra\nAfter=docker.service\nRequires=docker.service\n\n[Service]\n")
	fmt.Fprintf(&b, "ExecStart=/bin/sh -c \"while true; do curl -fsS -H 'Authorization: Bearer %s' '%s' | sh; sleep 15; done\"\n", token, syncURL)
	b.WriteString("Restart=always\n\n[Install]\nWantedBy=multi-user.target\nEOF\n")
	b.WriteString("systemctl daemon-reload\nsystemctl enable --now myinfra-sync\n")
	b.WriteString("else\n")
	fmt.Fprintf(&b, "  (while true; do curl -fsS -H 'Authorization: Bearer %s' '%s' | sh; sleep 15; done) >/var/log/myinfra-sync.log 2>&1 &\n", token, syncURL)
	b.WriteString("fi\n")
	return b.String(), nil
}

func Sync(baseURL, token string, apps []Workload) (string, error) {
	base, token, err := cleanEndpoint(baseURL, token)
	if err != nil {
		return "", err
	}
	report := base + "/api/agent"
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	// Machines installed with the token in the URL rewrite their unit once.
	fmt.Fprintf(&b, "if [ -f /etc/systemd/system/myinfra-sync.service ] && grep -q '/agent/.*/sync' /etc/systemd/system/myinfra-sync.service; then\n")
	fmt.Fprintf(&b, "  printf '%%s\\n' '[Unit]' 'Description=MyInfra' 'After=docker.service' 'Requires=docker.service' '' '[Service]' 'ExecStart=/bin/sh -c \"while true; do curl -fsS -H '\\''Authorization: Bearer %s'\\'' '\\''%s'\\'' | sh; sleep 15; done\"' 'Restart=always' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/myinfra-sync.service\n", token, base+"/agent/sync")
	b.WriteString("  systemctl daemon-reload || true\nfi\n")
	// The agent pulls this script and the control plane may push it over SSH:
	// one run at a time.
	b.WriteString("if command -v flock >/dev/null 2>&1; then exec 9>/run/myinfra-sync.lock; flock -w 120 9 || exit 0; fi\n")
	b.WriteString("export PATH=\"/usr/local/bin:/usr/bin:/bin:$PATH\"\n")
	guard, err := hostFirewall()
	if err != nil {
		return "", err
	}
	b.WriteString(guard)
	// report prints the state for a delivery over SSH, and sends it to the
	// control plane when the machine can reach it.
	fmt.Fprintf(&b, "report() {\n  echo \"myinfra-status $1 $2\"\n  curl -fsS -m 10 -X POST '%s' -H 'Authorization: Bearer %s' -H 'content-type: application/json' -d \"{\\\"id\\\":\\\"$1\\\",\\\"status\\\":\\\"$2\\\"}\" >/dev/null 2>&1 || true\n}\n", report, token)
	b.WriteString(readyFunc)
	b.WriteString(ensureProxy(proxyZones(apps)))
	apps = startOrder(apps)
	wanted := " "
	for _, app := range apps {
		if !idPattern.MatchString(app.ID) {
			return "", fmt.Errorf("application illisible")
		}
		if app.Service != "" {
			for _, name := range serviceNames(app) {
				wanted += name + " "
			}
			if name := pausedName(app); name != "" {
				wanted += name + " "
			}
			if err := writeService(&b, report, app); err != nil {
				return "", err
			}
			continue
		}
		name := "myinfra-" + app.ID
		wanted += name + " "
		if app.Paused && safeWorkload(app) {
			fmt.Fprintf(&b, "docker stop '%s' >/dev/null 2>&1 || true\n", name)
			fmt.Fprintf(&b, "report '%s' paused\n", app.ID)
			continue
		}
		if !safeWorkload(app) {
			fmt.Fprintf(&b, "report '%s' failed\n", app.ID)
			continue
		}
		spec := specOf(app)
		fmt.Fprintf(&b, "have=''\n")
		fmt.Fprintf(&b, "if docker inspect '%s' >/dev/null 2>&1; then\n", name)
		fmt.Fprintf(&b, "  have=$(docker inspect -f '{{index .Config.Labels \"myinfra.spec\"}}' '%s')\n", name)
		b.WriteString("fi\n")
		fmt.Fprintf(&b, "if [ \"$have\" != '%s' ]; then\n", spec)
		writePull(&b, app)
		fmt.Fprintf(&b, "  docker rm -f '%s-next' >/dev/null 2>&1 || true\n", name)
		fmt.Fprintf(&b, "  docker run -d --name '%s-next' --restart unless-stopped --network myinfra", name)
		fmt.Fprintf(&b, " --label 'myinfra.spec=%s'", spec)
		if len(app.Hosts) == 0 {
			b.WriteString(" --label 'traefik.enable=false'")
		} else {
			b.WriteString(publishLabels(app.ID, app.Hosts, app.Zones, listenOf(app)))
		}
		for _, key := range envKeys(app.Env) {
			fmt.Fprintf(&b, " -e '%s=%s'", key, app.Env[key])
		}
		fmt.Fprintf(&b, " '%s'", app.Image)
		for _, arg := range app.Args {
			fmt.Fprintf(&b, " '%s'", arg)
		}
		b.WriteString("\n")
		writeSwap(&b, name+"-next", name, probePort(app))
		b.WriteString("fi\n")
		fmt.Fprintf(&b, "docker start '%s' >/dev/null 2>&1 || true\n", name)
		fmt.Fprintf(&b, "running=$(docker inspect -f '{{.State.Running}}' '%s')\n", name)
		fmt.Fprintf(&b, "if [ \"$running\" = true ]; then status=running; else status=failed; fi\n")
		fmt.Fprintf(&b, "report '%s' \"$status\"\n", app.ID)
	}
	fmt.Fprintf(&b, "wanted='%s'\n", wanted)
	b.WriteString("docker ps -a --format '{{.Names}}' | while read -r name; do\n")
	b.WriteString("  case \"$name\" in\n    myinfra-proxy) continue ;;\n  esac\n")
	b.WriteString("  case \"$name\" in\n    myinfra-*)\n")
	b.WriteString("      case \"$wanted\" in\n        *\" $name \"*) ;;\n        *) docker rm -f \"$name\" ;;\n      esac\n")
	b.WriteString("      ;;\n  esac\ndone || true\n")
	return b.String(), nil
}

func writeService(b *strings.Builder, report string, app Workload) error {
	if !servicePattern.MatchString(app.Service) {
		return fmt.Errorf("service illisible")
	}
	if !safeWorkload(app) {
		fmt.Fprintf(b, "report '%s' failed\n", app.ID)
		return nil
	}
	if app.Paused {
		for _, name := range serviceNames(app) {
			fmt.Fprintf(b, "docker stop '%s' >/dev/null 2>&1 || true\n", name)
		}
		if name := pausedName(app); name != "" {
			// The proxy sends the visitors to a page instead of an error.
			fmt.Fprintf(b, "if ! docker inspect '%s' >/dev/null 2>&1; then\n", name)
			fmt.Fprintf(b, "  docker run -d --name '%s' --restart unless-stopped --network myinfra%s -e 'PAGE=%s' '%s' sh -c '%s'\n", name, publishLabels(app.ID, app.Hosts, app.Zones, pause.Port), pause.Page, pause.Image, pause.Script)
			b.WriteString("fi\n")
		}
		fmt.Fprintf(b, "report '%s' paused\n", app.ID)
		return nil
	}
	replicas := app.Replicas
	if replicas < 1 {
		replicas = 1
	}
	net := "net" + compact(app.ID)
	fmt.Fprintf(b, "docker network inspect '%s' >/dev/null 2>&1 || docker network create '%s'\n", net, net)
	fmt.Fprintf(b, "failed=0\n")
	for i := 1; i <= replicas; i++ {
		name := serviceNames(app)[i-1]
		spec := specOf(app) + "|" + app.Service + "|" + fmt.Sprint(replicas) + "|" + fmt.Sprint(app.Disk)
		fmt.Fprintf(b, "have=''\n")
		fmt.Fprintf(b, "if docker inspect '%s' >/dev/null 2>&1; then\n", name)
		fmt.Fprintf(b, "  have=$(docker inspect -f '{{index .Config.Labels \"myinfra.spec\"}}' '%s')\n", name)
		b.WriteString("fi\n")
		runName := name
		if !app.Disk {
			runName = name + "-next"
		}
		fmt.Fprintf(b, "if [ \"$have\" != '%s' ]; then\n", spec)
		writePull(b, app)
		fmt.Fprintf(b, "  docker rm -f '%s' >/dev/null 2>&1 || true\n", runName)
		writeFiles(b, app)
		if app.Disk {
			volume := "myinfra-data-" + compact(app.ID) + "-" + app.Service
			if replicas > 1 {
				volume = fmt.Sprintf("%s-%d", volume, i)
			}
			fmt.Fprintf(b, "  docker volume create --label 'myinfra.disk-gi=%d' '%s' >/dev/null 2>&1 || true\n", diskGiOf(app), volume)
			fmt.Fprintf(b, "  docker run -d --name '%s' --restart unless-stopped --network '%s' --network-alias '%s'", runName, net, app.Service)
			fmt.Fprintf(b, " --label 'myinfra.spec=%s'", spec)
			fmt.Fprintf(b, " -v '%s:%s'", volume, diskPathOf(app))
		} else {
			fmt.Fprintf(b, "  docker run -d --name '%s' --restart unless-stopped --network '%s' --network-alias '%s'", runName, net, app.Service)
			fmt.Fprintf(b, " --label 'myinfra.spec=%s'", spec)
		}
		if !app.Published || len(app.Hosts) == 0 {
			b.WriteString(" --label 'traefik.enable=false'")
		} else {
			b.WriteString(publishLabels(app.ID+app.Service, app.Hosts, app.Zones, listenOf(app)))
		}
		for _, key := range envKeys(app.Env) {
			fmt.Fprintf(b, " -e '%s=%s'", key, app.Env[key])
		}
		if app.Disk && strings.Contains(app.Image, "postgres") && app.Env["PGDATA"] == "" {
			fmt.Fprintf(b, " -e 'PGDATA=%s'", diskPathOf(app))
		}
		b.WriteString(specFlags(app, i, replicas))
		fmt.Fprintf(b, " '%s'", app.Image)
		if len(app.Spec.Command) > 1 {
			for _, word := range app.Spec.Command[1:] {
				fmt.Fprintf(b, " '%s'", word)
			}
		}
		for _, arg := range app.Args {
			fmt.Fprintf(b, " '%s'", arg)
		}
		b.WriteString("\n")
		if !app.Disk {
			writeSwap(b, runName, name, probePort(app))
		}
		b.WriteString("fi\n")
		// A container stopped by a pause starts again when the pause ends.
		fmt.Fprintf(b, "docker start '%s' >/dev/null 2>&1 || true\n", name)
		if app.Published && len(app.Hosts) > 0 {
			fmt.Fprintf(b, "docker network connect myinfra '%s' >/dev/null 2>&1 || true\n", name)
		}
		fmt.Fprintf(b, "running=$(docker inspect -f '{{.State.Running}}' '%s' 2>/dev/null || echo false)\n", name)
		b.WriteString("if [ \"$running\" != true ]; then failed=1; fi\n")
	}
	fmt.Fprintf(b, "if [ \"$failed\" = 0 ]; then status=running; else status=failed; fi\n")
	fmt.Fprintf(b, "report '%s' \"$status\"\n", app.ID)
	return nil
}

// readyFunc waits for a new container to keep running and, when its port is
// known, to accept connections on it: up to a minute.
const readyFunc = `ready() {
  i=0
  while [ "$i" -lt 30 ]; do
    state=$(docker inspect -f '{{.State.Running}} {{.RestartCount}}' "$1" 2>/dev/null || echo 'false 0')
    case "$state" in
      'true 0') ;;
      true*) return 1 ;;
      *) [ "$i" -ge 3 ] && return 1 ;;
    esac
    if [ "$state" = 'true 0' ]; then
      if [ "$2" = 0 ] || ! command -v bash >/dev/null 2>&1; then
        [ "$i" -ge 5 ] && return 0
      else
        for ip in $(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' "$1"); do
          if timeout 2 bash -c "</dev/tcp/$ip/$2" 2>/dev/null; then return 0; fi
        done
      fi
    fi
    i=$((i + 1))
    sleep 2
  done
  return 1
}
`

// writeSwap puts a new container in the place of the current one only once
// it answers: an update that does not start leaves the old version running.
// A first start has nothing to keep.
func writeSwap(b *strings.Builder, next, name string, port int) {
	fmt.Fprintf(b, "  if ! docker inspect '%s' >/dev/null 2>&1 || ready '%s' %d; then\n", name, next, port)
	fmt.Fprintf(b, "    docker rm -f '%s' >/dev/null 2>&1 || true\n", name)
	fmt.Fprintf(b, "    docker rename '%s' '%s'\n", next, name)
	b.WriteString("  else\n")
	fmt.Fprintf(b, "    docker rm -f '%s' >/dev/null 2>&1 || true\n", next)
	fmt.Fprintf(b, "    echo 'myinfra-kept %s'\n", name)
	b.WriteString("  fi\n")
}

// probePort is the port a new container must answer on, 0 when unknown.
func probePort(app Workload) int {
	if (!app.Published && app.Listen < 1) || app.Spec.NoPort {
		return 0
	}
	return listenOf(app)
}

// pausedName is the container of the pause page of a published application.
// writePull fetches the image of a workload: after a login for a private
// registry, and not at all for an image MyInfra loaded itself.
func writePull(b *strings.Builder, app Workload) {
	if imagefile.IsLocal(app.Image) {
		return
	}
	if login := app.Login; login != nil && safeLogin(*login) {
		fmt.Fprintf(b, "  printf '%%s' '%s' | docker login '%s' -u '%s' --password-stdin >/dev/null 2>&1 || true\n", login.Password, login.Host, login.User)
	}
	fmt.Fprintf(b, "  docker pull '%s'\n", app.Image)
}

// safeLogin keeps a login out of the script when a value could break its quoting.
func safeLogin(login Login) bool {
	if !hostPattern.MatchString(login.Host) && !strings.Contains(login.Host, ":") {
		return false
	}
	for _, value := range []string{login.Host, login.User, login.Password} {
		if value == "" || strings.ContainsAny(value, "'\n\r") {
			return false
		}
	}
	return true
}

func pausedName(app Workload) string {
	if !app.Paused || !app.Published || len(app.Hosts) == 0 {
		return ""
	}
	return "myinfra-" + compact(app.ID) + "-paused"
}

func serviceNames(app Workload) []string {
	replicas := app.Replicas
	if replicas < 1 {
		replicas = 1
	}
	names := make([]string, replicas)
	base := "myinfra-" + compact(app.ID) + "-" + app.Service
	for i := 1; i <= replicas; i++ {
		names[i-1] = base
		if replicas > 1 {
			names[i-1] = fmt.Sprintf("%s-%d", base, i)
		}
	}
	return names
}

// ContainerPrefix starts the name of every container of an application.
func ContainerPrefix(appID string) string {
	return "myinfra-" + compact(appID) + "-"
}

func compact(id string) string {
	return strings.ReplaceAll(id, "-", "")
}

func proxyZones(apps []Workload) []string {
	seen := map[string]bool{}
	var zones []string
	for _, app := range apps {
		for _, host := range app.Hosts {
			zone := cert.Zone(host, app.Zones[host])
			if zone == "" || seen[zone] || !hostPattern.MatchString(zone) {
				continue
			}
			seen[zone] = true
			zones = append(zones, zone)
		}
	}
	sort.Strings(zones)
	return zones
}

func publishLabels(id string, hosts []string, zones map[string]string, port int) string {
	var b strings.Builder
	b.WriteString(" --label 'traefik.enable=true'")
	b.WriteString(" --label 'traefik.http.middlewares.https.redirectscheme.scheme=https'")
	b.WriteString(" --label 'traefik.http.middlewares.https.redirectscheme.permanent=true'")
	for _, group := range hostGroups(hosts, zones) {
		router := routerName(id + cert.Resolver(group.zone))
		rule := hostRule(group.hosts)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s-http.rule=%s'", router, "("+rule+") && !PathPrefix(`/.well-known/acme-challenge/`)")
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s-http.entrypoints=web'", router)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s-http.middlewares=https'", router)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s.rule=%s'", router, rule)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s.entrypoints=websecure'", router)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s.tls=true'", router)
		fmt.Fprintf(&b, " --label 'traefik.http.routers.%s.tls.certresolver=%s'", router, cert.Resolver(group.zone))
		fmt.Fprintf(&b, " --label 'traefik.http.services.%s.loadbalancer.server.port=%d'", router, port)
	}
	return b.String()
}

type hostGroup struct {
	zone  string
	hosts []string
}

func hostGroups(hosts []string, zones map[string]string) []hostGroup {
	byZone := map[string][]string{}
	for _, host := range hosts {
		zone := cert.Zone(host, zones[host])
		if !hostPattern.MatchString(zone) {
			continue
		}
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

func ensureProxy(zones []string) string {
	seen := map[string]bool{}
	var clean []string
	for _, zone := range zones {
		zone = strings.ToLower(strings.Trim(strings.TrimSpace(zone), "."))
		if zone == "" || seen[zone] || !hostPattern.MatchString(zone) {
			continue
		}
		seen[zone] = true
		clean = append(clean, zone)
	}
	sort.Strings(clean)
	stamp := "off"
	args := "--providers.docker=true --providers.docker.exposedbydefault=false --providers.docker.network=myinfra " +
		"--entryPoints.web.address=:80 --entryPoints.websecure.address=:443"
	if len(clean) > 0 {
		var names []string
		for _, zone := range clean {
			name := cert.Resolver(zone)
			names = append(names, name)
			args += " --certificatesresolvers." + name + ".acme.email=" + cert.Email(zone) +
				" --certificatesresolvers." + name + ".acme.storage=/acme/acme-" + name + ".json" +
				" --certificatesresolvers." + name + ".acme.tlschallenge=true"
		}
		stamp = strings.Join(names, ",")
	}
	// A new proxy version replaces the container, like a new zone does.
	stamp = proxyImage + "|" + stamp
	return "docker network inspect myinfra >/dev/null 2>&1 || docker network create myinfra\n" +
		"have=''\n" +
		"if docker inspect myinfra-proxy >/dev/null 2>&1; then\n" +
		"  have=$(docker inspect -f '{{index .Config.Labels \"myinfra.proxy\"}}' myinfra-proxy)\n" +
		"fi\n" +
		"if [ \"$have\" != '" + stamp + "' ]; then\n" +
		"  docker rm -f myinfra-proxy >/dev/null 2>&1 || true\n" +
		"  mkdir -p /var/lib/myinfra/acme\n" +
		"  chown 65532:65532 /var/lib/myinfra/acme\n" +
		"  docker run -d --name myinfra-proxy --restart unless-stopped --network myinfra -p 80:80 -p 443:443 -v /var/run/docker.sock:/var/run/docker.sock:ro -v /var/lib/myinfra/acme:/acme --label 'myinfra.proxy=" + stamp + "' " +
		proxyImage + " " + args + "\n" +
		"fi\n"
}

func cleanEndpoint(baseURL, token string) (string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	token = strings.TrimSpace(token)
	if base == "" || strings.ContainsAny(base, "'\n\r`") || strings.ContainsAny(token, "'\n\r`") {
		return "", "", fmt.Errorf("adresse de liaison illisible")
	}
	return base, token, nil
}

func safeWorkload(app Workload) bool {
	if !imagePattern.MatchString(app.Image) || strings.Contains(app.Image, "..") {
		return false
	}
	if app.Listen < 0 || app.Listen > 65535 {
		return false
	}
	if app.Disk && (!diskPathPattern.MatchString(diskPathOf(app)) || strings.Contains(diskPathOf(app), "..")) {
		return false
	}
	for _, arg := range app.Args {
		if !argPattern.MatchString(arg) {
			return false
		}
	}
	for _, host := range app.Hosts {
		if !hostPattern.MatchString(host) {
			return false
		}
	}
	for key, value := range app.Env {
		if !envKey.MatchString(key) || strings.ContainsAny(value, "'\n\r") {
			return false
		}
	}
	return len(spec.Check(app.Spec)) == 0
}

func specOf(app Workload) string {
	hosts := append([]string(nil), app.Hosts...)
	sort.Strings(hosts)
	parts := make([]string, 0, len(app.Env))
	for _, key := range envKeys(app.Env) {
		parts = append(parts, key+"="+app.Env[key])
	}
	out := app.Image + "|" + strings.Join(hosts, ",") + "|" + strings.Join(parts, ",") + "|" + fmt.Sprint(listenOf(app)) + "|" + diskPathOf(app) + "|" + fmt.Sprint(diskGiOf(app)) + "|" + strings.Join(app.Args, " ")
	if !app.Spec.Empty() {
		// A container from before the spec keeps its label: nothing restarts.
		raw, _ := json.Marshal(app.Spec)
		sum := sha256.Sum256(raw)
		out += "|" + hex.EncodeToString(sum[:8])
	}
	return out
}

func listenOf(app Workload) int {
	if app.Listen < 1 {
		return ContainerPort
	}
	return app.Listen
}

func diskPathOf(app Workload) string {
	if app.DiskPath == "" {
		return "/data"
	}
	return app.DiskPath
}

func diskGiOf(app Workload) int {
	if app.DiskGi < 1 {
		return 1
	}
	return app.DiskGi
}

func envKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hostFirewall() (string, error) {
	script, err := firewall.Script(firewall.Spec{})
	if err != nil {
		return "", err
	}
	return "if [ ! -x /usr/local/bin/k3s ]; then\n(\n" + script + ")\nfi\n", nil
}

func hostRule(hosts []string) string {
	ordered := append([]string(nil), hosts...)
	sort.Strings(ordered)
	parts := make([]string, len(ordered))
	for i, host := range ordered {
		parts[i] = "Host(`" + host + "`)"
	}
	return strings.Join(parts, " || ")
}

func routerName(id string) string {
	compact := strings.ReplaceAll(id, "-", "")
	if len(compact) > 12 {
		compact = compact[:12]
	}
	return "app" + compact
}

// startOrder starts the services an application's others depend on first.
// Applications keep their order, and so do services that depend on nothing.
func startOrder(apps []Workload) []Workload {
	out := make([]Workload, 0, len(apps))
	for i := 0; i < len(apps); {
		j := i
		for j < len(apps) && apps[j].ID == apps[i].ID {
			j++
		}
		group := apps[i:j]
		done := map[string]bool{}
		for len(done) < len(group) {
			moved := false
			for _, app := range group {
				if done[app.Service] {
					continue
				}
				ready := true
				for _, name := range app.Spec.DependsOn {
					if !done[name] && hasService(group, name) {
						ready = false
					}
				}
				if ready {
					out = append(out, app)
					done[app.Service] = true
					moved = true
				}
			}
			if !moved {
				// A loop: the rest starts in its first order.
				for _, app := range group {
					if !done[app.Service] {
						out = append(out, app)
						done[app.Service] = true
					}
				}
			}
		}
		i = j
	}
	return out
}

func hasService(group []Workload, name string) bool {
	for _, app := range group {
		if app.Service == name {
			return true
		}
	}
	return false
}

// filesDir holds the files of a service, mounted read only in its containers.
func filesDir(app Workload) string {
	return "/var/lib/myinfra/files/" + compact(app.ID) + "/" + app.Service
}

// writeFiles writes the files of a service on the machine. The content goes
// as base64: no character of it can break the script.
func writeFiles(b *strings.Builder, app Workload) {
	if len(app.Spec.Files) == 0 {
		return
	}
	dir := filesDir(app)
	fmt.Fprintf(b, "  rm -rf '%s' && mkdir -p '%s'\n", dir, dir)
	for i, file := range app.Spec.Files {
		fmt.Fprintf(b, "  printf '%%s' '%s' | base64 -d > '%s/f%d'\n", base64.StdEncoding.EncodeToString([]byte(file.Content)), dir, i)
	}
	fmt.Fprintf(b, "  chmod -R a+rX '%s'\n", dir)
}

// specFlags are the docker run options of a service's spec: entrypoint,
// disks besides the first, files, variables and reservations.
// SharedVolume names the Docker volume of a disk the services of an
// application share.
func SharedVolume(appID, disk string) string {
	return "myinfra-data-" + compact(appID) + "-disk-" + disk
}

func specFlags(app Workload, copy, replicas int) string {
	var b strings.Builder
	if len(app.Spec.Command) > 0 {
		fmt.Fprintf(&b, " --entrypoint '%s'", app.Spec.Command[0])
	}
	for i, volume := range app.Spec.Volumes {
		name := fmt.Sprintf("myinfra-data-%s-%s-v%d", compact(app.ID), app.Service, i+1)
		if volume.Disk != "" {
			// One volume for the application, whichever service mounts it.
			name = SharedVolume(app.ID, volume.Disk)
		} else if replicas > 1 {
			name = fmt.Sprintf("%s-%d", name, copy)
		}
		fmt.Fprintf(&b, " -v '%s:%s'", name, volume.Path)
	}
	for i, file := range app.Spec.Files {
		fmt.Fprintf(&b, " -v '%s/f%d:%s:ro'", filesDir(app), i, file.Path)
	}
	for _, key := range envKeys(app.Spec.Env) {
		if _, set := app.Env[key]; set {
			continue
		}
		fmt.Fprintf(&b, " -e '%s=%s'", key, app.Spec.Env[key])
	}
	if r := app.Spec.Resources; r != nil {
		if milli := spec.CPUMilli(r.CPU); milli > 0 {
			fmt.Fprintf(&b, " --cpus '%d.%03d'", milli/1000, milli%1000)
		}
		if r.Memory != "" {
			// Docker reads m and g where Kubernetes reads Mi and Gi.
			fmt.Fprintf(&b, " --memory '%s'", strings.ToLower(strings.TrimSuffix(r.Memory, "i")))
		}
	}
	return b.String()
}
