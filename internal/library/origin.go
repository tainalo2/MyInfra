package library

import "myinfra/internal/selfupdate"

// Origin is where a person's package or template comes from, when they
// took it from the library: a newer version is offered to them, and Sum
// tells whether they changed their copy since.
type Origin struct {
	Repo    string `json:"repo"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	// Sum is the sha256 of the body as it was taken.
	Sum string `json:"sum,omitempty"`
}

// Newest is the version the library offers for an origin, "" when the
// library no longer has it or the copy is up to date.
func (lib *Library) Newest(o Origin) (string, Item) {
	if o.Repo != Repo() && lib.Release != Dev {
		return "", Item{}
	}
	var item Item
	switch o.Kind {
	case "package":
		p, ok := lib.Package(o.ID)
		if !ok {
			return "", Item{}
		}
		item = p.Item
	case "template":
		t, ok := lib.Template(o.ID)
		if !ok {
			return "", Item{}
		}
		item = t.Item
	default:
		return "", Item{}
	}
	if !selfupdate.Newer(item.Version, o.Version) {
		return "", Item{}
	}
	return item.Version, item
}
