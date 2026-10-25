package market

import "myinfra/internal/library"

// A starter is a ready-made template of the library: a cluster, its servers
// and packages that go well together. Choosing one creates a template the
// person completes with a provider, offers, keys and names.
type Starter struct {
	ID      string
	Version string
	Name    string
	Summary string
	Sizing  string
	// Translations give the words in the other languages of the screens.
	Translations map[string]library.TemplateWords
	// Cluster is the name of the cluster it creates.
	Cluster string
	Servers []StarterServer
	Apps    []StarterApp
}

type StarterServer struct {
	Name         string
	ControlPlane bool
}

type StarterApp struct {
	// ID is the package, Subdomain the name the application gets.
	ID        string
	Subdomain string
}

func Starters() []Starter {
	lib := library.Current()
	out := make([]Starter, 0, len(lib.Templates))
	for _, t := range lib.Templates {
		out = append(out, starterOf(t))
	}
	return out
}

func StarterOf(id string) (Starter, bool) {
	t, ok := library.Current().Template(id)
	if !ok {
		return Starter{}, false
	}
	return starterOf(t), true
}

func starterOf(t library.Template) Starter {
	b := t.Body
	out := Starter{
		ID: t.ID, Version: t.Version, Name: b.Name, Summary: b.Summary, Sizing: b.Sizing,
		Translations: b.Translations, Cluster: b.Cluster,
	}
	for _, server := range b.Servers {
		out.Servers = append(out.Servers, StarterServer{Name: server.Name, ControlPlane: server.ControlPlane})
	}
	for _, app := range b.Apps {
		sub := app.Subdomain
		if sub == "" {
			sub = app.Package
		}
		out.Apps = append(out.Apps, StarterApp{ID: app.Package, Subdomain: sub})
	}
	return out
}

// StarterOrigin says where a template taken from a starter comes from.
func StarterOrigin(s Starter) library.Origin {
	return library.Origin{Repo: library.Repo(), Kind: "template", ID: s.ID, Version: s.Version}
}

// ArticleName is the name of a catalogue application, for a starter to show.
func ArticleName(id string) string {
	if item, ok := library.Current().Package(id); ok {
		return item.Pkg.Name
	}
	return ""
}

// ArticleLogo is the logo of a catalogue application.
func ArticleLogo(id string) string {
	if item, ok := library.Current().Package(id); ok {
		return item.Pkg.About.Logo
	}
	return ""
}
