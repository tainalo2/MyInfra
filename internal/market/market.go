// Package market is the catalogue: applications described as packages, one
// file each under catalogue/, installed in a few clicks. Its files are the
// packages a person can write, with an about: block to present them.
package market

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"myinfra/internal/pack"
)

const (
	Sites         = "Sites"
	Collaboration = "Collaboration"
	Code          = "Code et données"
	Identity      = "Identité"
	Files         = "Fichiers et tableaux"
	Automation    = "Automatisation"
	Exchange      = "Échanges"
	Notes         = "Notes et liens"
	Accounts      = "Comptes personnels"
	Media         = "Médias"
	Tools         = "Outils"
)

// files holds the catalogue: one package per application, and order.txt,
// the order the screens show them in.
//
//go:embed catalogue/*.yaml catalogue/order.txt
var files embed.FS

type PublicService struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Published bool   `json:"published"`
	Disk      bool   `json:"disk"`
}

type Card struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Site        string          `json:"site"`
	Logo        string          `json:"logo"`
	Category    string          `json:"category"`
	KeepsFiles  bool            `json:"keepsFiles"`
	Inputs      []pack.Input    `json:"inputs"`
	Services    []PublicService `json:"services"`
	// Translations give the summary and the description in the other
	// languages of the screens.
	Translations map[string]pack.Words `json:"translations,omitempty"`
}

type Group struct {
	Name     string `json:"name"`
	Articles []Card `json:"articles"`
}

type article struct {
	id  string
	pkg pack.Package
}

var (
	idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	articles  []article
)

func init() {
	list, err := load(files)
	if err != nil {
		panic(err)
	}
	articles = list
}

// load reads the catalogue in its order and checks each application: a
// valid package, presented in every language.
func load(fsys fs.FS) ([]article, error) {
	raw, err := fs.ReadFile(fsys, "catalogue/order.txt")
	if err != nil {
		return nil, err
	}
	var out []article
	listed := map[string]bool{}
	for _, id := range strings.Fields(string(raw)) {
		if !idPattern.MatchString(id) || listed[id] {
			return nil, fmt.Errorf("catalogue : identifiant illisible ou en double dans order.txt : %s", id)
		}
		listed[id] = true
		data, err := fs.ReadFile(fsys, "catalogue/"+id+".yaml")
		if err != nil {
			return nil, fmt.Errorf("catalogue : %s.yaml est absent", id)
		}
		p, problems := pack.Parse(data)
		if len(problems) > 0 {
			return nil, fmt.Errorf("catalogue : %s.yaml, ligne %d : %s", id, problems[0].Line, problems[0].Error())
		}
		if err := presented(id, p); err != nil {
			return nil, err
		}
		out = append(out, article{id: id, pkg: p})
	}
	entries, err := fs.ReadDir(fsys, "catalogue")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if path.Ext(name) == ".yaml" && !listed[strings.TrimSuffix(name, ".yaml")] {
			return nil, fmt.Errorf("catalogue : %s manque dans order.txt", name)
		}
	}
	return out, nil
}

// presented checks what a catalogue application needs beyond a package:
// a name, a category, a site, a logo, a description, and its words in the
// other languages; and that each question it asks is read somewhere.
func presented(id string, p pack.Package) error {
	fail := func(what string) error { return fmt.Errorf("catalogue : %s.yaml : %s", id, what) }
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Summary) == "" || strings.TrimSpace(p.Category) == "" {
		return fail("name, summary et category sont demandés")
	}
	a := p.About
	if a == nil || !strings.HasPrefix(a.Site, "https://") || a.Logo == "" || utf8.RuneCountInString(strings.TrimSpace(a.Description)) < 80 {
		return fail("about demande site (https://), logo et une description de 80 caractères au moins")
	}
	for _, lang := range []string{"en", "pt", "it", "de"} {
		words := a.Translations[lang]
		if strings.TrimSpace(words.Summary) == "" || strings.TrimSpace(words.Description) == "" {
			return fail("about.translations." + lang + " demande summary et description")
		}
	}
	raw, _ := json.Marshal(p)
	for _, input := range p.Inputs {
		if !strings.Contains(string(raw), "{{input:"+input.Key+"}}") {
			return fail("la question " + input.Key + " n'est lue nulle part")
		}
	}
	return nil
}

func Groups() []Group {
	var order []string
	byName := map[string][]Card{}
	for _, item := range articles {
		if _, ok := byName[item.pkg.Category]; !ok {
			order = append(order, item.pkg.Category)
		}
		byName[item.pkg.Category] = append(byName[item.pkg.Category], cardOf(item))
	}
	out := make([]Group, 0, len(order))
	for _, name := range order {
		out = append(out, Group{Name: name, Articles: byName[name]})
	}
	return out
}

// Logo is the logo of the catalogue application that runs one of these
// images, "" when none does.
func Logo(images ...string) string {
	want := map[string]bool{}
	for _, image := range images {
		if base := imageBase(image); base != "" {
			want[base] = true
		}
	}
	if len(want) == 0 {
		return ""
	}
	for _, item := range articles {
		for _, svc := range item.pkg.Services {
			if want[imageBase(svc.Image)] {
				return item.pkg.About.Logo
			}
		}
	}
	return ""
}

func imageBase(image string) string {
	image = strings.TrimSpace(image)
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		image = image[:colon]
	}
	return image
}

func CardOf(id string) (Card, bool) {
	item, ok := find(id)
	if !ok {
		return Card{}, false
	}
	return cardOf(item), true
}

func find(id string) (article, bool) {
	for _, item := range articles {
		if item.id == id {
			return item, true
		}
	}
	return article{}, false
}

func cardOf(item article) Card {
	p := item.pkg
	inputs := p.Inputs
	if inputs == nil {
		inputs = []pack.Input{}
	}
	keeps := false
	services := make([]PublicService, 0, len(p.Services))
	for _, svc := range p.Services {
		disk := len(svc.Volumes) > 0
		keeps = keeps || disk
		services = append(services, PublicService{Name: svc.Name, Image: svc.Image, Published: svc.Public, Disk: disk})
	}
	return Card{
		ID: item.id, Name: p.Name, Summary: p.Summary, Description: p.About.Description,
		Site: p.About.Site, Logo: p.About.Logo, Category: p.Category,
		KeepsFiles: keeps, Inputs: inputs, Services: services, Translations: p.About.Translations,
	}
}

// PackageOf gives a catalogue application as a package of its own, to
// install, copy or carry in a template.
func PackageOf(id string) (pack.Package, bool) {
	item, ok := find(id)
	if !ok {
		return pack.Package{}, false
	}
	// A copy: the caller may change it.
	var out pack.Package
	raw, _ := json.Marshal(item.pkg)
	_ = json.Unmarshal(raw, &out)
	return out, true
}

// Expand fills in the values of a catalogue application for one
// installation.
func Expand(id, host string, inputs map[string]string) ([]pack.Service, map[string]string, error) {
	p, ok := PackageOf(id)
	if !ok {
		return nil, nil, fmt.Errorf("Cet article est introuvable.")
	}
	return pack.Expand(p, host, inputs)
}

// IDs lists the applications of the catalogue, in its order.
func IDs() []string {
	out := make([]string, len(articles))
	for i, item := range articles {
		out[i] = item.id
	}
	return out
}
