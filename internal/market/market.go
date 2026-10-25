// Package market is the catalogue: the packages of the library, presented
// to install in a few clicks. The library is published apart from MyInfra
// (internal/library); market shows the one MyInfra proposes now.
package market

import (
	"encoding/json"
	"fmt"
	"strings"

	"myinfra/internal/library"
	"myinfra/internal/pack"
)

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
	// CategoryWords give the category in the other languages.
	CategoryWords map[string]string `json:"categoryWords,omitempty"`
	// Version is the version of the package in the library.
	Version string `json:"version"`
	Changes string `json:"changes,omitempty"`
}

type Group struct {
	Name string `json:"name"`
	// Translations give the name in the other languages of the screens.
	Translations map[string]string `json:"translations,omitempty"`
	Articles     []Card            `json:"articles"`
}

// Release is the version of the library MyInfra proposes.
func Release() string {
	return library.Current().Release
}

// Groups gives the packages by category, in the order of the library.
func Groups() []Group {
	lib := library.Current()
	byName := map[string][]Card{}
	for _, item := range lib.Packages {
		byName[item.Pkg.Category] = append(byName[item.Pkg.Category], cardOf(item))
	}
	out := []Group{}
	for _, cat := range lib.Categories {
		if cards := byName[cat.Name]; len(cards) > 0 {
			out = append(out, Group{Name: cat.Name, Translations: cat.Translations, Articles: cards})
		}
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
	for _, item := range library.Current().Packages {
		for _, svc := range item.Pkg.Services {
			if want[imageBase(svc.Image)] {
				return item.Pkg.About.Logo
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
	item, ok := library.Current().Package(id)
	if !ok {
		return Card{}, false
	}
	return cardOf(item), true
}

func cardOf(item library.Package) Card {
	p := item.Pkg
	var words map[string]string
	if cat := library.Current().Category(p.Category); cat != nil {
		words = cat.Translations
	}
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
		ID: item.ID, Name: p.Name, Summary: p.Summary, Description: p.About.Description,
		Site: p.About.Site, Logo: p.About.Logo, Category: p.Category,
		KeepsFiles: keeps, Inputs: inputs, Services: services, Translations: p.About.Translations,
		CategoryWords: words, Version: item.Version, Changes: item.Changes,
	}
}

// PackageOf gives a catalogue application as a package of its own, to
// install, copy or carry in a template.
func PackageOf(id string) (pack.Package, bool) {
	item, ok := library.Current().Package(id)
	if !ok {
		return pack.Package{}, false
	}
	return copyOf(item.Pkg), true
}

// Origin says where a copy of a catalogue application comes from.
func Origin(id string) (library.Origin, bool) {
	lib := library.Current()
	item, ok := lib.Package(id)
	if !ok {
		return library.Origin{}, false
	}
	return library.Origin{Repo: library.Repo(), Kind: "package", ID: id, Version: item.Version}, true
}

// copyOf is a deep copy: the caller may change it.
func copyOf(p pack.Package) pack.Package {
	var out pack.Package
	raw, _ := json.Marshal(p)
	_ = json.Unmarshal(raw, &out)
	return out
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
	items := library.Current().Packages
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}
