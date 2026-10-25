// Package library is the content MyInfra proposes: application packages and
// templates, published apart from the engine in signed releases of the
// library repository, each item with a version of its own.
//
// A library is a tree of files:
//
//	categories.yaml               the categories, in order, with their words
//	order.txt                     the packages, in the order the screens show them
//	packages/<id>/package.yaml    a package, as a person writes one
//	packages/<id>/item.yaml       its version
//	templates/<id>/template.yaml  a cluster and packages that go well together
//	templates/<id>/item.yaml      its version
//	templates/order.txt           the templates, in the order the screens show them
//	logos/<slug>.svg|png          the logos simple-icons lacks
//
// A release adds index.json, the sum of each file, and is signed.
package library

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"myinfra/internal/pack"
	"myinfra/internal/selfupdate"
)

const (
	// Format is the layout of a library this MyInfra reads.
	Format = 1
	// TemplateType heads a template of the library.
	TemplateType = "myinfra-library-template"
	// TemplateVersion is the format of a template this MyInfra reads.
	TemplateVersion = 1
	// Dev is the release of a library read from a folder, in development.
	Dev = "dev"
)

var (
	idPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	// languages are those the screens show besides French.
	languages = []string{"en", "pt", "it", "de"}
)

// Item is what item.yaml says of a package or a template.
type Item struct {
	Version string `yaml:"version" json:"version"`
	// MinMyInfra is the oldest MyInfra that can use the item; an older one
	// leaves it out.
	MinMyInfra string `yaml:"minMyInfra,omitempty" json:"minMyInfra,omitempty"`
	// Changes says what this version brings.
	Changes string `yaml:"changes,omitempty" json:"changes,omitempty"`
}

type Package struct {
	ID string
	Item
	Pkg pack.Package
	// Sum is the sha256 of package.yaml: a copy that changed no longer has it.
	Sum string
}

type Template struct {
	ID string
	Item
	Body TemplateBody
	Sum  string
}

// TemplateBody is a template of the library: a cluster, its servers and
// packages of the library. The provider, the offers and the keys are left
// to the person who takes it.
type TemplateBody struct {
	Type         string                   `yaml:"type" json:"type"`
	Version      int                      `yaml:"version" json:"version"`
	Name         string                   `yaml:"name" json:"name"`
	Summary      string                   `yaml:"summary" json:"summary"`
	Sizing       string                   `yaml:"sizing" json:"sizing"`
	Translations map[string]TemplateWords `yaml:"translations" json:"translations"`
	Cluster      string                   `yaml:"cluster" json:"cluster"`
	Servers      []TemplateServer         `yaml:"servers" json:"servers"`
	Apps         []TemplateApp            `yaml:"apps" json:"apps"`
}

type TemplateWords struct {
	Name    string `yaml:"name" json:"name"`
	Summary string `yaml:"summary" json:"summary"`
	Sizing  string `yaml:"sizing" json:"sizing"`
}

type TemplateServer struct {
	Name         string `yaml:"name" json:"name"`
	ControlPlane bool   `yaml:"controlPlane" json:"controlPlane"`
}

type TemplateApp struct {
	// Package is the id of a package of the same library.
	Package string `yaml:"package" json:"package"`
	// Subdomain is the name the application gets, the package id by default.
	Subdomain string `yaml:"subdomain,omitempty" json:"subdomain,omitempty"`
}

// Category groups packages; Name is the one packages give, in French.
type Category struct {
	Name         string            `yaml:"name" json:"name"`
	Translations map[string]string `yaml:"translations" json:"translations"`
}

type Library struct {
	Release    string
	Packages   []Package
	Templates  []Template
	Categories []Category
	// Left lists the items this MyInfra is too old for.
	Left  []string
	logos map[string]string
	files fs.FS
}

// Read reads and checks a library. engine is the version of this MyInfra:
// an item that asks for a newer one is left out. A library that does not
// hold together is refused whole, with the file and what to fix.
func Read(fsys fs.FS, release, engine string) (*Library, error) {
	lib := &Library{Release: release, logos: map[string]string{}, files: fsys}
	if err := lib.readCategories(); err != nil {
		return nil, err
	}
	if err := lib.readLogos(); err != nil {
		return nil, err
	}
	if err := lib.readPackages(engine); err != nil {
		return nil, err
	}
	if err := lib.readTemplates(engine); err != nil {
		return nil, err
	}
	return lib, nil
}

func (lib *Library) readCategories() error {
	raw, err := fs.ReadFile(lib.files, "categories.yaml")
	if err != nil {
		return fmt.Errorf("categories.yaml est absent")
	}
	if err := strict(raw, &lib.Categories); err != nil {
		return fmt.Errorf("categories.yaml : %v", err)
	}
	seen := map[string]bool{}
	for _, cat := range lib.Categories {
		if strings.TrimSpace(cat.Name) == "" || seen[cat.Name] {
			return fmt.Errorf("categories.yaml : catégorie sans nom ou en double : %q", cat.Name)
		}
		seen[cat.Name] = true
		for _, lang := range languages {
			if strings.TrimSpace(cat.Translations[lang]) == "" {
				return fmt.Errorf("categories.yaml : %s demande translations.%s", cat.Name, lang)
			}
		}
	}
	return nil
}

func (lib *Library) readLogos() error {
	entries, err := fs.ReadDir(lib.files, "logos")
	if err != nil {
		// A library may need no logo of its own.
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		ext := path.Ext(name)
		if entry.IsDir() || name == "SOURCES.md" {
			continue
		}
		slug := strings.TrimSuffix(name, ext)
		if (ext != ".svg" && ext != ".png") || !slugPattern.MatchString(slug) {
			return fmt.Errorf("logos/%s : un logo est <nom>.svg ou <nom>.png", name)
		}
		if _, ok := lib.logos[slug]; ok {
			return fmt.Errorf("logos/%s : deux fichiers pour %s", name, slug)
		}
		lib.logos[slug] = "logos/" + name
	}
	return nil
}

func (lib *Library) readPackages(engine string) error {
	raw, err := fs.ReadFile(lib.files, "order.txt")
	if err != nil {
		return fmt.Errorf("order.txt est absent")
	}
	categories := map[string]bool{}
	for _, cat := range lib.Categories {
		categories[cat.Name] = true
	}
	listed := map[string]bool{}
	for _, id := range strings.Fields(string(raw)) {
		if !idPattern.MatchString(id) || listed[id] {
			return fmt.Errorf("order.txt : identifiant illisible ou en double : %s", id)
		}
		listed[id] = true
		dir := "packages/" + id
		data, err := fs.ReadFile(lib.files, dir+"/package.yaml")
		if err != nil {
			return fmt.Errorf("%s/package.yaml est absent", dir)
		}
		p, problems := pack.Parse(data)
		if len(problems) > 0 {
			return fmt.Errorf("%s/package.yaml, ligne %d : %s", dir, problems[0].Line, problems[0].Error())
		}
		if err := presented(dir+"/package.yaml", p); err != nil {
			return err
		}
		if !categories[p.Category] {
			return fmt.Errorf("%s/package.yaml : la catégorie %q manque dans categories.yaml", dir, p.Category)
		}
		item, err := lib.readItem(dir)
		if err != nil {
			return err
		}
		if tooNew(item, engine) {
			lib.Left = append(lib.Left, "packages/"+id)
			continue
		}
		lib.Packages = append(lib.Packages, Package{ID: id, Item: item, Pkg: p, Sum: Sum(data)})
	}
	entries, err := fs.ReadDir(lib.files, "packages")
	if err != nil {
		return fmt.Errorf("packages/ est absent")
	}
	for _, entry := range entries {
		if !listed[entry.Name()] {
			return fmt.Errorf("packages/%s manque dans order.txt", entry.Name())
		}
	}
	return nil
}

func (lib *Library) readTemplates(engine string) error {
	entries, err := fs.ReadDir(lib.files, "templates")
	if err != nil {
		// A library may hold packages only.
		return nil
	}
	order, err := lib.templateOrder(entries)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, entry := range entries {
		id := entry.Name()
		dir := "templates/" + id
		if id == "order.txt" {
			continue
		}
		if !entry.IsDir() || !idPattern.MatchString(id) {
			return fmt.Errorf("%s : un template est un dossier au nom en minuscules", dir)
		}
		data, err := fs.ReadFile(lib.files, dir+"/template.yaml")
		if err != nil {
			return fmt.Errorf("%s/template.yaml est absent", dir)
		}
		var body TemplateBody
		if err := strict(data, &body); err != nil {
			return fmt.Errorf("%s/template.yaml : %v", dir, err)
		}
		if other, ok := names[body.Cluster]; ok {
			return fmt.Errorf("%s/template.yaml : le cluster %s est déjà celui de %s", dir, body.Cluster, other)
		}
		names[body.Cluster] = id
		item, err := lib.readItem(dir)
		if err != nil {
			return err
		}
		missing, err := lib.checkTemplate(dir, body)
		if err != nil {
			return err
		}
		if tooNew(item, engine) || missing {
			lib.Left = append(lib.Left, dir)
			continue
		}
		lib.Templates = append(lib.Templates, Template{ID: id, Item: item, Body: body, Sum: Sum(data)})
	}
	sort.SliceStable(lib.Templates, func(i, j int) bool { return order[lib.Templates[i].ID] < order[lib.Templates[j].ID] })
	return nil
}

// templateOrder reads templates/order.txt, the order the screens show the
// templates in; without it, they follow their names.
func (lib *Library) templateOrder(entries []fs.DirEntry) (map[string]int, error) {
	out := map[string]int{}
	raw, err := fs.ReadFile(lib.files, "templates/order.txt")
	if err != nil {
		return out, nil
	}
	for i, id := range strings.Fields(string(raw)) {
		if _, ok := out[id]; ok || !idPattern.MatchString(id) {
			return nil, fmt.Errorf("templates/order.txt : identifiant illisible ou en double : %s", id)
		}
		out[id] = i
	}
	for _, entry := range entries {
		if _, ok := out[entry.Name()]; entry.IsDir() && !ok {
			return nil, fmt.Errorf("templates/%s manque dans templates/order.txt", entry.Name())
		}
	}
	for id := range out {
		found := false
		for _, entry := range entries {
			found = found || (entry.IsDir() && entry.Name() == id)
		}
		if !found {
			return nil, fmt.Errorf("templates/order.txt : %s n'est pas un template", id)
		}
	}
	return out, nil
}

// checkTemplate checks a template; missing is true when one of its packages
// was left out for this MyInfra.
func (lib *Library) checkTemplate(dir string, body TemplateBody) (missing bool, err error) {
	fail := func(what string) error { return fmt.Errorf("%s/template.yaml : %s", dir, what) }
	if body.Type != TemplateType || body.Version != TemplateVersion {
		return false, fail(fmt.Sprintf("l'en-tête est type: %s et version: %d", TemplateType, TemplateVersion))
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Summary) == "" || strings.TrimSpace(body.Sizing) == "" {
		return false, fail("name, summary et sizing sont demandés")
	}
	for _, lang := range languages {
		words := body.Translations[lang]
		if strings.TrimSpace(words.Name) == "" || strings.TrimSpace(words.Summary) == "" || strings.TrimSpace(words.Sizing) == "" {
			return false, fail("translations." + lang + " demande name, summary et sizing")
		}
	}
	if !idPattern.MatchString(body.Cluster) {
		return false, fail("cluster est un nom en minuscules")
	}
	planes := 0
	servers := map[string]bool{}
	for _, server := range body.Servers {
		if !idPattern.MatchString(server.Name) || servers[server.Name] {
			return false, fail("serveur au nom illisible ou en double : " + server.Name)
		}
		servers[server.Name] = true
		if server.ControlPlane {
			planes++
		}
	}
	// An even number of control planes loses its majority with one less.
	if planes%2 == 0 {
		return false, fail(fmt.Sprintf("%d plans de contrôle : il en faut un nombre impair", planes))
	}
	if len(body.Apps) == 0 {
		return false, fail("aucune application")
	}
	subdomains := map[string]bool{}
	for _, app := range body.Apps {
		listed := false
		for _, left := range lib.Left {
			listed = listed || left == "packages/"+app.Package
		}
		if listed {
			missing = true
			continue
		}
		p, ok := lib.Package(app.Package)
		if !ok {
			return false, fail("le paquet " + app.Package + " n'est pas dans la bibliothèque")
		}
		// A template is taken in one click: nothing to type.
		if len(p.Pkg.Inputs) > 0 {
			return false, fail("le paquet " + app.Package + " pose des questions")
		}
		sub := app.Subdomain
		if sub == "" {
			sub = app.Package
		}
		if !idPattern.MatchString(sub) || subdomains[sub] {
			return false, fail("sous-domaine illisible ou en double : " + sub)
		}
		subdomains[sub] = true
		if _, _, err := pack.Expand(p.Pkg, sub+".exemple.fr", nil); err != nil {
			return false, fail(app.Package + " : " + err.Error())
		}
	}
	return missing, nil
}

func (lib *Library) readItem(dir string) (Item, error) {
	var item Item
	raw, err := fs.ReadFile(lib.files, dir+"/item.yaml")
	if err != nil {
		return item, fmt.Errorf("%s/item.yaml est absent", dir)
	}
	if err := strict(raw, &item); err != nil {
		return item, fmt.Errorf("%s/item.yaml : %v", dir, err)
	}
	if !selfupdate.Valid(item.Version) || strings.HasPrefix(item.Version, "v") {
		return item, fmt.Errorf("%s/item.yaml : version est de la forme 1.2.0", dir)
	}
	if item.MinMyInfra != "" && !selfupdate.Valid(item.MinMyInfra) {
		return item, fmt.Errorf("%s/item.yaml : minMyInfra est de la forme 0.2.0", dir)
	}
	return item, nil
}

// tooNew is true when the item asks for a newer MyInfra than engine. A copy
// built from source reads everything.
func tooNew(item Item, engine string) bool {
	return item.MinMyInfra != "" && selfupdate.Valid(engine) && selfupdate.Newer(item.MinMyInfra, engine)
}

// presented checks what a package of the library needs beyond a package: a
// name, a category, a site, a logo, a description, and its words in the
// other languages; and that each question it asks is read somewhere.
func presented(file string, p pack.Package) error {
	fail := func(what string) error { return fmt.Errorf("%s : %s", file, what) }
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Summary) == "" || strings.TrimSpace(p.Category) == "" {
		return fail("name, summary et category sont demandés")
	}
	a := p.About
	if a == nil || !strings.HasPrefix(a.Site, "https://") || a.Logo == "" || utf8.RuneCountInString(strings.TrimSpace(a.Description)) < 80 {
		return fail("about demande site (https://), logo et une description de 80 caractères au moins")
	}
	for _, lang := range languages {
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

func strict(raw []byte, out any) error {
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	return dec.Decode(out)
}

// Package is a package of the library, as it was read: callers copy it
// before changing it.
func (lib *Library) Package(id string) (Package, bool) {
	for _, p := range lib.Packages {
		if p.ID == id {
			return p, true
		}
	}
	return Package{}, false
}

func (lib *Library) Template(id string) (Template, bool) {
	for _, t := range lib.Templates {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// Category gives the words of a category, nil when the library has none
// of that name.
func (lib *Library) Category(name string) *Category {
	for i := range lib.Categories {
		if lib.Categories[i].Name == name {
			return &lib.Categories[i]
		}
	}
	return nil
}

// Logo is the file of a logo the library carries, with its type.
func (lib *Library) Logo(slug string) ([]byte, string, bool) {
	file, ok := lib.logos[slug]
	if !ok {
		return nil, "", false
	}
	raw, err := fs.ReadFile(lib.files, file)
	if err != nil {
		return nil, "", false
	}
	kind := "image/png"
	if path.Ext(file) == ".svg" {
		kind = "image/svg+xml"
	}
	return raw, kind, true
}

// HasLogo is true when the library carries the logo of this slug.
func (lib *Library) HasLogo(slug string) bool {
	_, ok := lib.logos[slug]
	return ok
}
