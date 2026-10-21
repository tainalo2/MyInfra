package market

// A starter is a ready-made template: a cluster, its servers and a set of
// catalogue applications that go well together. Choosing one creates a
// template the person completes with a provider, offers, keys and names.
type Starter struct {
	ID string
	// Cluster is the name of the cluster it creates.
	Cluster string
	Servers []StarterServer
	Apps    []string
}

type StarterServer struct {
	Name         string
	ControlPlane bool
}

func single(name string) []StarterServer {
	return []StarterServer{{Name: name, ControlPlane: true}}
}

// trio is three control planes: the cluster keeps running when one server
// stops, during an update for instance.
func trio(prefix string) []StarterServer {
	return []StarterServer{
		{Name: prefix + "-1", ControlPlane: true},
		{Name: prefix + "-2", ControlPlane: true},
		{Name: prefix + "-3", ControlPlane: true},
	}
}

var starters = []Starter{
	{ID: "first-steps", Cluster: "maison", Servers: single("maison-1"), Apps: []string{"homepage", "uptime-kuma", "vaultwarden"}},
	{ID: "website", Cluster: "site", Servers: single("site-1"), Apps: []string{"wordpress", "umami"}},
	{ID: "team", Cluster: "equipe", Servers: trio("equipe"), Apps: []string{"nextcloud", "mattermost", "vaultwarden", "docmost"}},
	{ID: "knowledge", Cluster: "savoir", Servers: single("savoir-1"), Apps: []string{"bookstack", "hedgedoc", "excalidraw", "drawio"}},
	{ID: "dev", Cluster: "atelier", Servers: single("atelier-1"), Apps: []string{"forgejo", "registry", "verdaccio", "code-server"}},
	{ID: "monitoring", Cluster: "vigie", Servers: single("vigie-1"), Apps: []string{"uptime-kuma", "healthchecks", "ntfy"}},
	{ID: "automation", Cluster: "automates", Servers: single("automates-1"), Apps: []string{"n8n", "changedetection", "ntfy"}},
	{ID: "paperwork", Cluster: "papiers", Servers: single("papiers-1"), Apps: []string{"paperless", "stirling-pdf", "actual", "wallos"}},
	{ID: "media", Cluster: "medias", Servers: single("medias-1"), Apps: []string{"jellyfin", "jellyseerr", "navidrome", "audiobookshelf"}},
}

func Starters() []Starter {
	return append([]Starter(nil), starters...)
}

func StarterOf(id string) (Starter, bool) {
	for _, item := range starters {
		if item.ID == id {
			return item, true
		}
	}
	return Starter{}, false
}

// ArticleName is the name of a catalogue article, for a starter to show.
func ArticleName(id string) string {
	if article, ok := find(id); ok {
		return article.Name
	}
	return ""
}

// ArticleLogo is the logo of a catalogue article.
func ArticleLogo(id string) string {
	return profiles[id].Logo
}
