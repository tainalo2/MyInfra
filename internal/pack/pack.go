// Package pack reads, checks and writes application packages: the services
// of an application with their disks, files and variables, described in YAML
// or JSON so that a person can keep, share and edit them, as a Helm chart
// would be, without templates.
package pack

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"myinfra/internal/market"
	"myinfra/internal/spec"
)

const (
	Type    = "myinfra-package"
	Version = 1
)

type Package struct {
	Type     string            `json:"type"`
	Version  int               `json:"version"`
	Name     string            `json:"name"`
	Summary  string            `json:"summary,omitempty"`
	Category string            `json:"category,omitempty"`
	Inputs   []Input           `json:"inputs,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Services []Service         `json:"services"`
}

// Input is a question asked at installation, read as {{input:key}}.
type Input struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Service runs one image. The first of its volumes is its disk.
type Service struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	// Public is the one service the application's domain leads to.
	Public bool `json:"public,omitempty"`
	// Port is where the service answers: the domain for the public one, the
	// other services for all.
	Port     int      `json:"port,omitempty"`
	Replicas int      `json:"replicas,omitempty"`
	Args     []string `json:"args,omitempty"`
	spec.Service
}

// Problem is one thing wrong in a package, at a path such as
// "services[1].volumes[0].path" and, once read from a text, its line.
type Problem struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (p Problem) Error() string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + " : " + p.Message
}

const maxServices = 12

var (
	serviceName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	imageName   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@+/-]{0,200}$`)
	envKey      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	argWord     = regexp.MustCompile(`^[A-Za-z0-9_.,:/=@+-]+$`)
)

// Check lists what is wrong in a package. None means it can be installed.
func Check(p Package) []Problem {
	var out []Problem
	add := func(path, format string, args ...any) {
		out = append(out, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if p.Type != Type {
		add("type", "Un paquet commence par type: %s.", Type)
	}
	if p.Version != Version {
		add("version", "Cette version de MyInfra lit les paquets de version %d.", Version)
	}
	name := strings.TrimSpace(p.Name)
	if name == "" || utf8.RuneCountInString(name) > 60 || strings.ContainsAny(name, "\n\r") {
		add("name", "Donnez au paquet un nom de 60 caractères au plus.")
	}
	if utf8.RuneCountInString(p.Summary) > 300 {
		add("summary", "Le résumé tient en 300 caractères.")
	}
	if utf8.RuneCountInString(p.Category) > 40 {
		add("category", "La catégorie tient en 40 caractères.")
	}
	inputs := map[string]bool{}
	for i, input := range p.Inputs {
		at := fmt.Sprintf("inputs[%d]", i)
		if !market.InputKey(input.Key) || inputs[input.Key] {
			add(at+".key", "Une question a un nom unique, en minuscules, par exemple admin-email.")
		}
		inputs[input.Key] = true
		if strings.TrimSpace(input.Label) == "" || utf8.RuneCountInString(input.Label) > 80 {
			add(at+".label", "Écrivez la question posée, en 80 caractères au plus.")
		}
	}
	for _, key := range sortedKeys(p.Env) {
		value := p.Env[key]
		if !envKey.MatchString(key) {
			add("env."+key, "Le nom d'une variable commence par une lettre et ne contient que des lettres, des chiffres et _.")
			continue
		}
		if len(value) > 8192 || strings.ContainsAny(value, "'\x00\r\n") {
			add("env."+key, "La valeur est trop longue, ou contient une apostrophe ou un retour à la ligne.")
		}
		if err := market.CheckTokens(value, inputs); err != nil {
			add("env."+key, "%s", tokenMessage(err))
		}
	}
	if len(p.Services) == 0 || len(p.Services) > maxServices {
		add("services", "Un paquet compte de 1 à %d services.", maxServices)
	}
	public := 0
	names := map[string]bool{}
	quiet := map[string]bool{}
	for _, svc := range p.Services {
		names[svc.Name] = true
		quiet[svc.Name] = svc.NoPort
	}
	seen := map[string]bool{}
	for i, svc := range p.Services {
		at := fmt.Sprintf("services[%d]", i)
		if !serviceName.MatchString(svc.Name) {
			add(at+".name", "Le nom d'un service est en minuscules, par exemple web ou db.")
		} else if seen[svc.Name] {
			add(at+".name", "Le service %s existe déjà.", svc.Name)
		}
		seen[svc.Name] = true
		if !imageName.MatchString(svc.Image) || strings.Contains(svc.Image, "..") || strings.Contains(svc.Image, "://") {
			add(at+".image", "Indiquez l'image, par exemple nginx:1.27 ou ghcr.io/acme/app:2.")
		}
		if svc.Public {
			public++
		}
		if svc.Port < 0 || svc.Port > 65535 {
			add(at+".port", "Un port va de 1 à 65535.")
		}
		if svc.NoPort && (svc.Public || svc.Port > 0 || len(svc.Ports) > 0) {
			add(at+".noPort", "Un service sans port n'est ni public, ni joint sur un port.")
		}
		if svc.Replicas < 0 || svc.Replicas > 10 {
			add(at+".replicas", "Un service tourne en 1 à 10 copies.")
		}
		for j, arg := range svc.Args {
			if !argWord.MatchString(arg) {
				add(fmt.Sprintf("%s.args[%d]", at, j), "Un argument est un mot sans espace ni guillemet ; une commande complète va dans command.")
			}
		}
		for _, problem := range spec.Check(svc.Service) {
			add(at+"."+problem.Path, "%s", problem.Message)
		}
		for j, port := range svc.Ports {
			if port.Port == portOf(svc) {
				add(fmt.Sprintf("%s.ports[%d].port", at, j), "Le port %d est déjà celui du service.", port.Port)
			}
		}
		for j, name := range svc.DependsOn {
			if name == svc.Name || !names[name] {
				add(fmt.Sprintf("%s.dependsOn[%d]", at, j), "Le service %s n'existe pas dans ce paquet.", name)
			} else if quiet[name] {
				add(fmt.Sprintf("%s.dependsOn[%d]", at, j), "Le service %s n'écoute aucun port : rien ne peut l'attendre.", name)
			}
		}
		for _, key := range sortedKeys(svc.Env) {
			if err := market.CheckTokens(svc.Env[key], inputs); err != nil {
				add(at+".env."+key, "%s", tokenMessage(err))
			} else if market.HasSecret(svc.Env[key]) {
				add(at+".env."+key, "Les secrets vont dans les variables communes (env), qui sont chiffrées.")
			}
		}
		for j, file := range svc.Files {
			if err := market.CheckTokens(file.Content, inputs); err != nil {
				add(fmt.Sprintf("%s.files[%d].content", at, j), "%s", tokenMessage(err))
			} else if market.HasSecret(file.Content) {
				add(fmt.Sprintf("%s.files[%d].content", at, j), "Les secrets vont dans les variables communes (env), qui sont chiffrées.")
			}
		}
	}
	if len(p.Services) > 0 && public != 1 {
		add("services", "Un seul service est public : celui qui reçoit le domaine.")
	}
	if cycle := dependencyLoop(p.Services); cycle != "" {
		add("services", "Les services s'attendent les uns les autres : %s.", cycle)
	}
	return out
}

func tokenMessage(err error) string {
	return "Un {{…}} est illisible (" + err.Error() + ") : {{url}}, {{host}}, {{input:clé}}, {{secret:nom}} ou {{encoded:nom}}."
}

// portOf is where a service answers; a public one answers on 80 unless told.
func portOf(svc Service) int {
	if svc.Port < 1 {
		return 80
	}
	return svc.Port
}

// dependencyLoop names a loop of services that wait for each other, "" when
// there is none.
func dependencyLoop(services []Service) string {
	deps := map[string][]string{}
	for _, svc := range services {
		deps[svc.Name] = svc.DependsOn
	}
	state := map[string]int{}
	var path []string
	var visit func(string) string
	visit = func(name string) string {
		switch state[name] {
		case 1:
			for i, step := range path {
				if step == name {
					return strings.Join(append(path[i:], name), " → ")
				}
			}
		case 2:
			return ""
		}
		state[name] = 1
		path = append(path, name)
		for _, next := range deps[name] {
			if _, ok := deps[next]; !ok {
				continue
			}
			if loop := visit(next); loop != "" {
				return loop
			}
		}
		path = path[:len(path)-1]
		state[name] = 2
		return ""
	}
	for _, svc := range services {
		if loop := visit(svc.Name); loop != "" {
			return loop
		}
	}
	return ""
}

// Disk splits a service as a store keeps it: its first volume is its disk,
// the others stay in its spec.
func (s Service) Disk() (*spec.Volume, spec.Service) {
	rest := s.Service
	if len(s.Volumes) == 0 {
		return nil, rest
	}
	first := s.Volumes[0]
	rest.Volumes = append([]spec.Volume(nil), s.Volumes[1:]...)
	return &first, rest
}

// FromArticle writes a catalogue article as a package, to copy and change.
func FromArticle(article market.Article) Package {
	p := Package{
		Type: Type, Version: Version,
		Name: article.Name, Summary: article.Summary, Category: article.Category,
		Env: copyMap(article.Env),
	}
	for _, input := range article.Inputs {
		p.Inputs = append(p.Inputs, Input{Key: input.Key, Label: input.Label})
	}
	for _, svc := range article.Services {
		out := Service{
			Name: svc.Name, Image: svc.Image, Public: svc.Published, Port: svc.Listen,
			Args: append([]string(nil), svc.Args...),
		}
		if svc.Replicas > 1 {
			out.Replicas = svc.Replicas
		}
		if svc.Disk {
			out.Volumes = []spec.Volume{{Path: svc.DiskPath, SizeGi: svc.DiskGi}}
		}
		p.Services = append(p.Services, out)
	}
	return p
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
