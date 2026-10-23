// Package spec holds what a service runs with beyond its image, its port and
// its first disk: the fields a package describes, that the store keeps as
// they are, and that both the cluster and the server renderers read.
package spec

import (
	"fmt"
	"regexp"
	"strings"
)

type Service struct {
	// Command replaces the image's entrypoint; the service's Args follow it.
	Command []string `json:"command,omitempty"`
	// Ports are reached by the other services, besides the one the domain
	// leads to.
	Ports []Port `json:"ports,omitempty"`
	// Volumes are disks besides the first one, kept as it was.
	Volumes []Volume `json:"volumes,omitempty"`
	// Files are written into the container, read only.
	Files []File `json:"files,omitempty"`
	// Env is set on this service only, under the application's variables.
	Env       map[string]string `json:"env,omitempty"`
	Resources *Resources        `json:"resources,omitempty"`
	Health    *Health           `json:"health,omitempty"`
	// DependsOn names the services that must answer before this one starts.
	DependsOn []string `json:"dependsOn,omitempty"`
	// NoPort is a service that listens on nothing, a worker: no copy waits
	// for a port to send it visitors, and no other service can wait for it.
	NoPort bool `json:"noPort,omitempty"`
}

type Port struct {
	Name string `json:"name,omitempty"`
	Port int    `json:"port"`
}

type Volume struct {
	Path   string `json:"path"`
	SizeGi int    `json:"sizeGi,omitempty"`
}

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Resources reserves CPU and memory; memory is also the most the service
// may use.
type Resources struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// Health tells when a copy answers: an HTTP path, or a TCP connection when
// the path is empty. Port 0 is the service's own port.
type Health struct {
	HTTP string `json:"http,omitempty"`
	Port int    `json:"port,omitempty"`
}

// Problem is one thing wrong in a service, at a path such as "volumes[1].path".
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

const (
	maxCommand   = 32
	maxPorts     = 16
	maxVolumes   = 8
	maxFiles     = 32
	maxFile      = 256 << 10
	maxFilesSize = 512 << 10
)

var (
	serviceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	portName    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,14}$`)
	// reservedPort names the ports left unnamed, and main the service's own.
	reservedPort = regexp.MustCompile(`^p[0-9]+$`)
	envKey       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	filePath     = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	httpPath     = regexp.MustCompile(`^/[A-Za-z0-9._~/%?=&+-]{0,255}$`)
	cpu          = regexp.MustCompile(`^([0-9]{1,3}(\.[0-9]{1,3})?|[0-9]{1,6}m)$`)
	memory       = regexp.MustCompile(`^[0-9]{1,6}(Mi|Gi)$`)
)

// Empty is true when the service needs nothing besides its first fields.
func (s Service) Empty() bool {
	return len(s.Command) == 0 && len(s.Ports) == 0 && len(s.Volumes) == 0 && len(s.Files) == 0 &&
		len(s.Env) == 0 && s.Resources == nil && s.Health == nil && len(s.DependsOn) == 0 && !s.NoPort
}

// Check lists what is wrong in a service, on its own: the names it depends
// on and the ports it shares with its first fields are checked by the caller,
// who knows the other services.
func Check(s Service) []Problem {
	var out []Problem
	add := func(path, format string, args ...any) {
		out = append(out, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if len(s.Command) > maxCommand {
		add("command", "Une commande compte au plus %d mots.", maxCommand)
	}
	for i, word := range s.Command {
		if word == "" || len(word) > 1024 || strings.ContainsAny(word, "'\x00\r") {
			add(fmt.Sprintf("command[%d]", i), "Un mot de la commande est vide, trop long ou contient une apostrophe.")
		}
	}
	if len(s.Ports) > maxPorts {
		add("ports", "Un service ouvre au plus %d ports.", maxPorts)
	}
	ports := map[int]bool{}
	names := map[string]bool{}
	for i, port := range s.Ports {
		at := fmt.Sprintf("ports[%d]", i)
		if port.Port < 1 || port.Port > 65535 {
			add(at+".port", "Un port va de 1 à 65535.")
		} else if ports[port.Port] {
			add(at+".port", "Le port %d est déjà ouvert.", port.Port)
		}
		ports[port.Port] = true
		if port.Name != "" {
			if !portName.MatchString(port.Name) {
				add(at+".name", "Le nom d'un port tient en 15 caractères, en minuscules.")
			} else if port.Name == "main" || reservedPort.MatchString(port.Name) {
				add(at+".name", "Le nom %s est réservé.", port.Name)
			} else if names[port.Name] {
				add(at+".name", "Le nom %s sert déjà.", port.Name)
			}
			names[port.Name] = true
		}
	}
	if len(s.Volumes) > maxVolumes {
		add("volumes", "Un service garde au plus %d disques de plus.", maxVolumes)
	}
	paths := map[string]bool{}
	for i, volume := range s.Volumes {
		at := fmt.Sprintf("volumes[%d]", i)
		if !PathOK(volume.Path) {
			add(at+".path", "Indiquez un chemin absolu, par exemple /var/lib/app.")
		} else if paths[volume.Path] {
			add(at+".path", "Le chemin %s sert déjà.", volume.Path)
		}
		paths[volume.Path] = true
		if volume.SizeGi < 0 || volume.SizeGi > 1024 {
			add(at+".sizeGi", "Un disque fait de 1 à 1024 Gio.")
		}
	}
	if len(s.Files) > maxFiles {
		add("files", "Un service reçoit au plus %d fichiers.", maxFiles)
	}
	total := 0
	for i, file := range s.Files {
		at := fmt.Sprintf("files[%d]", i)
		if !PathOK(file.Path) || strings.HasSuffix(file.Path, "/") {
			add(at+".path", "Indiquez le chemin du fichier, par exemple /etc/app/config.ini.")
		} else if paths[file.Path] {
			add(at+".path", "Le chemin %s sert déjà.", file.Path)
		}
		paths[file.Path] = true
		if len(file.Content) > maxFile || strings.ContainsRune(file.Content, 0) {
			add(at+".content", "Un fichier fait au plus 256 Kio de texte.")
		}
		total += len(file.Content)
	}
	if total > maxFilesSize {
		add("files", "Les fichiers d'un service font au plus 512 Kio en tout.")
	}
	for key, value := range s.Env {
		if !envKey.MatchString(key) {
			add("env."+key, "Le nom d'une variable commence par une lettre et ne contient que des lettres, des chiffres et _.")
		} else if len(value) > 8192 || strings.ContainsAny(value, "'\x00\r\n") {
			add("env."+key, "La valeur est trop longue, ou contient une apostrophe ou un retour à la ligne.")
		}
	}
	if r := s.Resources; r != nil {
		if r.CPU != "" && !cpu.MatchString(r.CPU) {
			add("resources.cpu", "Indiquez des cœurs, par exemple 0.5 ou 500m.")
		}
		if r.Memory != "" && !memory.MatchString(r.Memory) {
			add("resources.memory", "Indiquez la mémoire en Mi ou Gi, par exemple 512Mi.")
		}
	}
	if h := s.Health; h != nil {
		if h.HTTP != "" && !httpPath.MatchString(h.HTTP) {
			add("health.http", "Indiquez un chemin qui commence par /, par exemple /health.")
		}
		if h.Port < 0 || h.Port > 65535 {
			add("health.port", "Un port va de 1 à 65535.")
		}
		if s.NoPort && h.Port == 0 {
			add("health.port", "Un service sans port se contrôle sur un port à indiquer.")
		}
	}
	seen := map[string]bool{}
	for i, name := range s.DependsOn {
		if !serviceName.MatchString(name) || seen[name] {
			add(fmt.Sprintf("dependsOn[%d]", i), "Nommez un autre service, une seule fois.")
		}
		seen[name] = true
	}
	return out
}

// PathOK is an absolute path that stays where it points.
func PathOK(path string) bool {
	return filePath.MatchString(path) && !strings.Contains(path, "..")
}

// CPUMilli turns a CPU amount into thousandths of a core, 0 when unset.
func CPUMilli(value string) int {
	if value == "" || !cpu.MatchString(value) {
		return 0
	}
	if strings.HasSuffix(value, "m") {
		var n int
		fmt.Sscan(strings.TrimSuffix(value, "m"), &n)
		return n
	}
	whole, frac, _ := strings.Cut(value, ".")
	var n, f int
	fmt.Sscan(whole, &n)
	frac = (frac + "000")[:3]
	fmt.Sscan(frac, &f)
	return n*1000 + f
}
