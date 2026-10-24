package api

import (
	"encoding/json"
	"net/url"
	"strings"

	"myinfra/internal/market"
	"myinfra/internal/store"
)

func mcpReadTools() []mcpTool {
	return []mcpTool{
		{
			Name: "get_overview", Title: "Vue d'ensemble", Role: store.RoleRead,
			Description: "Ce qui tourne : machines (état, cluster, adresse, offre, charge), applications (destination, adresse, état) et pods en difficulté.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/overview"},
			Run:         runOverview,
		},
		{
			Name: "list_clusters", Title: "Lister les clusters", Role: store.RoleRead,
			Description: "Les clusters, avec leurs machines (plans de contrôle compris) et le nombre d'applications.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/clusters", "GET /api/servers", "GET /api/applications"},
			Run:         runListClusters,
		},
		{
			Name: "get_cluster_usage", Title: "Place restante sur un cluster", Role: store.RoleRead,
			Description: "Processeur, mémoire et disque utilisés et libres sur un cluster, nœud par nœud.",
			Schema:      object([]string{"cluster"}, map[string]any{"cluster": text("Nom du cluster.")}),
			Routes:      []string{"GET /api/clusters", "GET /api/clusters/{id}/usage"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Cluster string }](raw)
				if err != nil {
					return "", err
				}
				cluster, err := c.cluster(args.Cluster)
				if err != nil {
					return "", err
				}
				var out any
				if err := c.get("/api/clusters/"+url.PathEscape(cluster.ID)+"/usage", &out); err != nil {
					return "", err
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "list_servers", Title: "Lister les machines", Role: store.RoleRead,
			Description: "Les machines : état, cluster, adresse, provider, offre, lieu, coût cumulé.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/servers"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var rows []serverView
				if err := c.get("/api/servers", &rows); err != nil {
					return "", err
				}
				out := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					out = append(out, serverBrief(row))
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "get_server", Title: "Détail d'une machine", Role: store.RoleRead,
			Description: "Une machine en détail, avec les étapes de son installation : à suivre après un achat.",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "GET /api/servers/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Server string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				var full serverView
				if err := c.get("/api/servers/"+url.PathEscape(row.ID), &full); err != nil {
					return "", err
				}
				full.JoinCommand = ""
				return asJSON(full), nil
			},
		},
		{
			Name: "list_provider_accounts", Title: "Comptes providers", Role: store.RoleRead,
			Description: "Les comptes de fournisseurs cloud enregistrés, où MyInfra peut acheter des machines. Un compte s'ajoute dans l'interface (Réglages › Providers), jamais par ici.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/secrets"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				rows, err := c.secretsOf("provider_token")
				if err != nil {
					return "", err
				}
				out := []map[string]string{}
				for _, row := range rows {
					out = append(out, map[string]string{"name": row.Name, "provider": row.Provider})
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "list_ssh_keys", Title: "Clés SSH", Role: store.RoleRead,
			Description: "Les clés SSH que MyInfra peut poser sur une nouvelle machine.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/secrets"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				rows, err := c.secretsOf("ssh_key")
				if err != nil {
					return "", err
				}
				out := []map[string]string{}
				for _, row := range rows {
					out = append(out, map[string]string{"name": row.Name, "fingerprint": row.Fingerprint})
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "list_dns_zones", Title: "Domaines utilisables", Role: store.RoleRead,
			Description: "Les zones DNS des comptes enregistrés : un domaine d'application doit être dans l'une d'elles.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/dns/zones"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var rows []zoneView
				if err := c.get("/api/dns/zones", &rows); err != nil {
					return "", err
				}
				out := []map[string]string{}
				for _, row := range rows {
					out = append(out, map[string]string{"zone": row.Name, "account": row.Account, "provider": row.Provider})
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "list_deployments", Title: "Déploiements", Role: store.RoleRead,
			Description: "Les déploiements récents et en cours, avec leur état.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/deployments"},
			Run:         passThrough("/api/deployments"),
		},
		{
			Name: "get_activity", Title: "Opérations en cours", Role: store.RoleRead,
			Description: "Ce qui tourne en ce moment : installations, destructions, mises à jour, transferts.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/activity"},
			Run:         passThrough("/api/activity"),
		},
		{
			Name: "list_history", Title: "Historique", Role: store.RoleRead,
			Description: "Les derniers événements (200 au plus), du plus récent au plus ancien. Filtrable par machine, par cluster, ou sur ce qui a été fait via MCP.",
			Schema: object(nil, map[string]any{
				"server":  text("Nom d'une machine."),
				"cluster": text("Nom d'un cluster."),
				"source":  choice("Origine : ui (interface), mcp (assistant), api (jeton utilisé directement).", "ui", "mcp", "api"),
				"limit":   whole("Nombre d'événements (50 par défaut).", 1, 200),
			}),
			Routes: []string{"GET /api/events", "GET /api/servers", "GET /api/clusters"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Server, Cluster, Source string
					Limit                   int
				}](raw)
				if err != nil {
					return "", err
				}
				query := url.Values{}
				if args.Server != "" {
					row, err := c.server(args.Server)
					if err != nil {
						return "", err
					}
					query.Set("server", row.ID)
				}
				if args.Cluster != "" {
					row, err := c.cluster(args.Cluster)
					if err != nil {
						return "", err
					}
					query.Set("cluster", row.ID)
				}
				if args.Source != "" {
					query.Set("source", args.Source)
				}
				var rows []eventView
				if err := c.get("/api/events?"+query.Encode(), &rows); err != nil {
					return "", err
				}
				limit := args.Limit
				if limit == 0 {
					limit = 50
				}
				if len(rows) > limit {
					rows = rows[:limit]
				}
				return asJSON(rows), nil
			},
		},
		{
			Name: "search_catalogue", Title: "Chercher dans le catalogue", Role: store.RoleRead,
			Description: "Les applications prêtes à installer. Sans texte, tout le catalogue par catégorie ; avec un texte, celles dont le nom, le résumé ou la catégorie le contient.",
			Schema:      object(nil, map[string]any{"query": text("Mots cherchés, par exemple « stockage », « wiki », « nextcloud ».")}),
			Routes:      []string{"GET /api/catalogue"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Query string }](raw)
				if err != nil {
					return "", err
				}
				var groups []market.Group
				if err := c.get("/api/catalogue", &groups); err != nil {
					return "", err
				}
				words := strings.Fields(strings.ToLower(args.Query))
				out := []map[string]any{}
				for _, group := range groups {
					for _, card := range group.Articles {
						hay := strings.ToLower(card.ID + " " + card.Name + " " + card.Summary + " " + card.Category + " " + card.Description)
						match := true
						for _, word := range words {
							if !strings.Contains(hay, word) {
								match = false
							}
						}
						if match {
							out = append(out, articleBrief(card))
						}
					}
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "get_catalogue_article", Title: "Article du catalogue", Role: store.RoleRead,
			Description: "Une application du catalogue : ses services, ses disques, les questions posées à l'installation. Son paquet YAML se lit dans la ressource myinfra://catalogue/{id}.",
			Schema:      object([]string{"article"}, map[string]any{"article": text("Identifiant ou nom de l'article, par exemple nextcloud.")}),
			Routes:      []string{"GET /api/catalogue", "GET /api/catalogue/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Article string }](raw)
				if err != nil {
					return "", err
				}
				id, err := c.article(args.Article)
				if err != nil {
					return "", err
				}
				var card market.Card
				if err := c.get("/api/catalogue/"+url.PathEscape(id), &card); err != nil {
					return "", err
				}
				brief := articleBrief(card)
				brief["description"] = card.Description
				brief["site"] = card.Site
				brief["services"] = card.Services
				brief["package"] = "myinfra://catalogue/" + card.ID
				return asJSON(brief), nil
			},
		},
	}
}

// passThrough answers with what a read route gives.
func passThrough(path string) func(c *mcpCall, _ json.RawMessage) (string, error) {
	return func(c *mcpCall, _ json.RawMessage) (string, error) {
		var out any
		if err := c.get(path, &out); err != nil {
			return "", err
		}
		return asJSON(out), nil
	}
}

func articleBrief(card market.Card) map[string]any {
	out := map[string]any{"id": card.ID, "name": card.Name, "summary": card.Summary, "category": card.Category, "keepsFiles": card.KeepsFiles}
	if len(card.Inputs) > 0 {
		out["inputs"] = card.Inputs
	}
	return out
}

// article finds a catalogue article by id or name.
func (c *mcpCall) article(wanted string) (string, error) {
	var groups []market.Group
	if err := c.get("/api/catalogue", &groups); err != nil {
		return "", err
	}
	var rows []named
	for _, group := range groups {
		for _, card := range group.Articles {
			rows = append(rows, named{card.ID, card.Name})
		}
	}
	row, err := pickNamed("article du catalogue", strings.ToLower(strings.TrimSpace(wanted)), rows)
	if err != nil {
		// Names are written with capitals: try them as given too.
		if again, againErr := pickNamed("article du catalogue", wanted, rows); againErr == nil {
			return again.ID, nil
		}
		return "", err
	}
	return row.ID, nil
}

func serverBrief(row serverView) map[string]any {
	out := map[string]any{"name": row.Name, "status": row.Status}
	if row.ClusterName != "" {
		out["cluster"] = row.ClusterName
		out["controlPlane"] = row.ControlPlane
	}
	if row.Address != "" {
		out["address"] = row.Address
	}
	if row.ProviderName != "" {
		out["providerAccount"] = row.ProviderName
	}
	if row.Provider != "" {
		out["provider"] = row.Provider
	}
	if row.Plan != nil {
		out["offer"] = row.Plan.Name
		out["location"] = row.Plan.Location
		out["vcpu"] = row.Plan.VCPU
		out["memoryMB"] = row.Plan.MemoryMB
		out["diskGB"] = row.Plan.DiskGB
		out["price"] = map[string]string{"hourly": row.Plan.Hourly, "monthly": row.Plan.Monthly, "currency": row.Plan.Currency}
	}
	if row.Cost != nil {
		out["costSinceCreation"] = row.Cost
	}
	return out
}

func runOverview(c *mcpCall, _ json.RawMessage) (string, error) {
	var view overviewView
	if err := c.get("/api/overview", &view); err != nil {
		return "", err
	}
	servers := []map[string]any{}
	for _, row := range view.Servers {
		brief := serverBrief(row.serverView)
		if row.Usage != nil {
			brief["usage"] = row.Usage
		}
		servers = append(servers, brief)
	}
	apps := []map[string]any{}
	for _, app := range view.Apps {
		apps = append(apps, appBrief(app))
	}
	troubled := []overviewPod{}
	for _, pod := range view.Pods {
		if !pod.Ready {
			troubled = append(troubled, pod)
		}
	}
	return asJSON(map[string]any{"servers": servers, "applications": apps, "podsNotReady": troubled}), nil
}

func appBrief(app appView) map[string]any {
	out := map[string]any{"name": app.Name, "address": app.Hostname}
	if app.ClusterName != "" {
		out["cluster"] = app.ClusterName
	}
	if app.ServerName != "" {
		out["server"] = app.ServerName
	}
	if app.RunStatus != "" {
		out["status"] = app.RunStatus
	}
	if app.RunDetail != "" {
		out["detail"] = app.RunDetail
	}
	if app.Rollout != "" {
		out["rollout"] = app.Rollout
	}
	if app.Paused {
		out["paused"] = true
	}
	if app.Failover {
		out["failover"] = true
	}
	if app.Guarded {
		out["hostsMyInfra"] = true
	}
	return out
}

func runListClusters(c *mcpCall, _ json.RawMessage) (string, error) {
	var clusters []clusterView
	if err := c.get("/api/clusters", &clusters); err != nil {
		return "", err
	}
	var servers []serverView
	if err := c.get("/api/servers", &servers); err != nil {
		return "", err
	}
	var apps []appView
	if err := c.get("/api/applications", &apps); err != nil {
		return "", err
	}
	out := []map[string]any{}
	for _, cluster := range clusters {
		nodes := []map[string]any{}
		for _, server := range servers {
			if server.ClusterID == cluster.ID {
				nodes = append(nodes, map[string]any{"name": server.Name, "status": server.Status, "controlPlane": server.ControlPlane, "address": server.Address})
			}
		}
		count := 0
		for _, app := range apps {
			if app.ClusterID == cluster.ID {
				count++
			}
		}
		origin := cluster.Origin
		if origin == "" {
			origin = "created"
		}
		out = append(out, map[string]any{"name": cluster.Name, "origin": origin, "nodes": nodes, "applications": count})
	}
	return asJSON(out), nil
}
