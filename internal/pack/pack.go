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
	// Disks are shared by the services that mount them with
	// volumes[].disk: the same files, for each of them.
	Disks []SharedDisk `json:"disks,omitempty"`
	// About presents the package: where it comes from, its logo, and its
	// words in each language.
	About *About `json:"about,omitempty"`
}

type About struct {
	Site string `json:"site,omitempty"`
	// Logo names the icon the screens draw, as Simple Icons names it.
	Logo        string `json:"logo,omitempty"`
	Description string `json:"description,omitempty"`
	// Translations give the summary and the description in another
	// language than the one the package is written in.
	Translations map[string]Words `json:"translations,omitempty"`
}

type Words struct {
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
}

// Languages are those the screens speak.
var Languages = []string{"fr", "en", "pt", "it", "de"}

// Input is a question asked at installation, read as {{input:key}}.
type Input struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// SharedDisk is a disk several services of the package mount.
type SharedDisk struct {
	Name   string `json:"name"`
	SizeGi int    `json:"sizeGi"`
}

const maxDisks = 8

// Service runs one image. The first of its volumes is its disk.
type Service struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	// Public services get a domain each; the first is the main one, the
	// application's address.
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

// sensitiveName is a variable that holds a password or a key: MyInfra seals
// its value, and an import draws it anew instead of keeping it.
var sensitiveName = regexp.MustCompile(`(?i)(PASS|SECRET|TOKEN|KEY|PRIVATE|CREDENTIAL|SALT)`)

// Sensitive tells whether a variable holds a password or a key.
func Sensitive(name string) bool {
	return sensitiveName.MatchString(name)
}

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
	if a := p.About; a != nil {
		if a.Site != "" && (!aboutSite.MatchString(a.Site) || len(a.Site) > 200) {
			add("about.site", "Indiquez l'adresse https:// du site de l'application.")
		}
		if a.Logo != "" && !aboutLogo.MatchString(a.Logo) {
			add("about.logo", "Le logo se nomme comme sur Simple Icons, en minuscules, par exemple nextcloud.")
		}
		if utf8.RuneCountInString(a.Description) > 1000 {
			add("about.description", "La description tient en 1000 caractères.")
		}
		for _, lang := range sortedKeys(wordsKeys(a.Translations)) {
			words := a.Translations[lang]
			if !knownLanguage(lang) {
				add("about.translations."+lang, "Les langues sont fr, en, pt, it et de.")
			}
			if utf8.RuneCountInString(words.Summary) > 300 || utf8.RuneCountInString(words.Description) > 1000 {
				add("about.translations."+lang, "Le résumé tient en 300 caractères, la description en 1000.")
			}
		}
	}
	inputs := map[string]bool{}
	for i, input := range p.Inputs {
		at := fmt.Sprintf("inputs[%d]", i)
		if !inputKey.MatchString(input.Key) || inputs[input.Key] {
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
		if err := checkTokens(value, inputs); err != nil {
			add("env."+key, "%s", tokenMessage(err))
		}
	}
	if len(p.Services) == 0 || len(p.Services) > maxServices {
		add("services", "Un paquet compte de 1 à %d services.", maxServices)
	}
	public, exposed := 0, 0
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
		exposed += len(spec.Exposed(svc.Service))
		for j, port := range svc.Ports {
			// The service's own port is listed again only to open it.
			if port.Port == portOf(svc) && port.Proto() == "tcp" && port.Expose == 0 {
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
			if err := checkTokens(svc.Env[key], inputs); err != nil {
				add(at+".env."+key, "%s", tokenMessage(err))
			} else if hasSecret(svc.Env[key]) {
				add(at+".env."+key, "Les secrets vont dans les variables communes (env), qui sont chiffrées.")
			}
		}
		for j, file := range svc.Files {
			if err := checkTokens(file.Content, inputs); err != nil {
				add(fmt.Sprintf("%s.files[%d].content", at, j), "%s", tokenMessage(err))
			} else if hasSecret(file.Content) {
				add(fmt.Sprintf("%s.files[%d].content", at, j), "Les secrets vont dans les variables communes (env), qui sont chiffrées.")
			}
		}
	}
	checkDisks(p, add)
	if len(p.Services) > 0 && public == 0 && exposed == 0 {
		add("services", "Au moins un service est public, celui qui reçoit le domaine, ou ouvre un port sur Internet (expose).")
	}
	checkAddresses(p, add)
	if cycle := dependencyLoop(p.Services); cycle != "" {
		add("services", "Les services s'attendent les uns les autres : %s.", cycle)
	}
	return out
}

var (
	aboutSite = regexp.MustCompile(`^https://[^\s'"<>]+$`)
	aboutLogo = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

func knownLanguage(lang string) bool {
	for _, known := range Languages {
		if known == lang {
			return true
		}
	}
	return false
}

func wordsKeys(m map[string]Words) map[string]string {
	out := make(map[string]string, len(m))
	for key := range m {
		out[key] = ""
	}
	return out
}

// Localized gives the summary and the description in a language, those the
// package is written in when it has none for it.
func (p Package) Localized(lang string) (string, string) {
	summary, description := p.Summary, ""
	if p.About == nil {
		return summary, description
	}
	description = p.About.Description
	if words, ok := p.About.Translations[lang]; ok {
		if words.Summary != "" {
			summary = words.Summary
		}
		if words.Description != "" {
			description = words.Description
		}
	}
	return summary, description
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

// Disk splits a service as a store keeps it: its first volume of its own is
// its disk, the others stay in its spec, with the shared ones.
func (s Service) Disk() (*spec.Volume, spec.Service) {
	rest := s.Service
	rest.Volumes = nil
	var first *spec.Volume
	for _, volume := range s.Volumes {
		if first == nil && volume.Disk == "" {
			copied := volume
			first = &copied
			continue
		}
		rest.Volumes = append(rest.Volumes, volume)
	}
	return first, rest
}

// checkDisks checks the shared disks: declared once with a size, mounted
// by name, by services that run one copy. A package rebuilt from a running
// application carries the size on each volume instead of a declaration.
func checkDisks(p Package, add func(path, format string, args ...any)) {
	if len(p.Disks) > maxDisks {
		add("disks", "Un paquet partage au plus %d disques.", maxDisks)
	}
	sizes := map[string]int{}
	used := map[string]bool{}
	for i, disk := range p.Disks {
		at := fmt.Sprintf("disks[%d]", i)
		if !spec.DiskName.MatchString(disk.Name) {
			add(at+".name", "Le nom d'un disque partagé est en minuscules, par exemple media.")
		} else if _, twice := sizes[disk.Name]; twice {
			add(at+".name", "Le disque %s est déjà déclaré.", disk.Name)
		}
		if disk.SizeGi < 1 || disk.SizeGi > 1024 {
			add(at+".sizeGi", "Un disque fait de 1 à 1024 Gio.")
		}
		sizes[disk.Name] = disk.SizeGi
	}
	for i, svc := range p.Services {
		at := fmt.Sprintf("services[%d]", i)
		shares := false
		for j, volume := range svc.Volumes {
			if volume.Disk == "" {
				continue
			}
			shares = true
			used[volume.Disk] = true
			vat := fmt.Sprintf("%s.volumes[%d]", at, j)
			declared, ok := sizes[volume.Disk]
			switch {
			case !ok && volume.SizeGi > 0:
				sizes[volume.Disk] = volume.SizeGi
			case !ok:
				add(vat+".disk", "Le disque %s n'est pas déclaré dans disks.", volume.Disk)
			case volume.SizeGi > 0 && volume.SizeGi != declared:
				add(vat+".sizeGi", "La taille d'un disque partagé se donne une fois, dans disks.")
			}
		}
		if shares && svc.Replicas > 1 {
			add(at+".replicas", "Un service qui monte un disque partagé tourne en une seule copie.")
		}
	}
	for i, disk := range p.Disks {
		if !used[disk.Name] {
			add(fmt.Sprintf("disks[%d].name", i), "Aucun service ne monte le disque %s.", disk.Name)
		}
	}
}

// checkAddresses checks that {{url:service}} and {{host:service}} name a
// public service of the package.
func checkAddresses(p Package, add func(path, format string, args ...any)) {
	public := map[string]bool{}
	for _, svc := range p.Services {
		if svc.Public {
			public[svc.Name] = true
		}
	}
	check := func(path, value string) {
		for _, name := range addressRefs(value) {
			if !public[name] {
				add(path, "{{url:%s}} et {{host:%s}} nomment un service public du paquet.", name, name)
			}
		}
	}
	for _, key := range sortedKeys(p.Env) {
		check("env."+key, p.Env[key])
	}
	for i, svc := range p.Services {
		for _, key := range sortedKeys(svc.Env) {
			check(fmt.Sprintf("services[%d].env.%s", i, key), svc.Env[key])
		}
		for j, file := range svc.Files {
			check(fmt.Sprintf("services[%d].files[%d].content", i, j), file.Content)
		}
	}
}

// Main is the main public service: the first one.
func (p Package) Main() string {
	for _, svc := range p.Services {
		if svc.Public {
			return svc.Name
		}
	}
	return ""
}

// diskSizes gives each shared disk its size, declared or carried.
func diskSizes(p Package) map[string]int {
	out := map[string]int{}
	for _, disk := range p.Disks {
		out[disk.Name] = disk.SizeGi
	}
	for _, svc := range p.Services {
		for _, volume := range svc.Volumes {
			if volume.Disk != "" && out[volume.Disk] == 0 {
				out[volume.Disk] = volume.SizeGi
			}
		}
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
