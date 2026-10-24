package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"myinfra/internal/store"
)

func mcpInfraTools() []mcpTool {
	tools := []mcpTool{
		{
			Name: "get_costs", Title: "Coût de l'infrastructure", Role: store.RoleRead,
			Description: "Le coût théorique mensuel des machines, par devise et machine par machine, et le budget mensuel s'il est fixé.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/costs"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var view costsView
				if err := c.get("/api/costs", &view); err != nil {
					return "", err
				}
				out := map[string]any{"monthly": amountsOf(view.Monthly).String(), "servers": view.Servers, "note": view.Note}
				if view.Budget > 0 {
					out["budget"] = view.Budget
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "create_cluster", Title: "Créer un cluster", Role: store.RoleDeploy, Write: true,
			Description: "Crée un cluster vide, sans machine. Pour un cluster avec ses machines, utilisez plutôt order_servers avec new_cluster : il achète et relie les machines en une commande.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom du cluster, par exemple SW.")}),
			Routes:      []string{"POST /api/clusters"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name string }](raw)
				if err != nil {
					return "", err
				}
				var out clusterView
				if err := c.send(http.MethodPost, "/api/clusters", map[string]string{"name": args.Name}, &out); err != nil {
					return "", err
				}
				return "Cluster « " + out.Name + " » créé, sans machine. Ajoutez-en avec order_servers (cluster=" + out.Name + ").", nil
			},
		},
		{
			Name: "get_delete_impact", Title: "Ce qu'une suppression emporterait", Role: store.RoleRead,
			Description: "Dit, sans rien lancer, ce que supprimer une machine, un cluster ou une application détruirait : machines chez le provider, applications, noms DNS, étapes, économie mensuelle.",
			Schema: object([]string{"kind", "name"}, map[string]any{
				"kind": choice("Ce qui serait supprimé.", "server", "cluster", "application"),
				"name": text("Son nom."),
			}),
			Routes: []string{"GET /api/servers", "GET /api/clusters", "GET /api/applications", "GET /api/servers/{id}/delete/impact", "GET /api/clusters/{id}/delete/impact", "GET /api/applications/{id}/delete/impact"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Kind, Name string }](raw)
				if err != nil {
					return "", err
				}
				target, err := c.deletionTarget(args.Kind, args.Name)
				if err != nil {
					return "", err
				}
				impact, err := c.impact(args.Kind, target.ID)
				if err != nil {
					return "", err
				}
				return impact.recap(), nil
			},
		},
		deletionTool("server", "delete_server", "Supprimer une machine",
			"Supprime une machine : achetée par MyInfra, elle est détruite chez son provider avec ses disques ; ajoutée à la main, elle est seulement oubliée. Ses applications et leurs noms DNS partent avec elle."),
		deletionTool("cluster", "delete_cluster", "Supprimer un cluster",
			"Supprime un cluster : ses machines achetées sont détruites chez leur provider, ses applications et leurs noms DNS retirés. Refusé pour le cluster qui héberge MyInfra."),
		deletionTool("application", "delete_application", "Supprimer une application",
			"Supprime une application, ses disques et ses noms DNS. Ses sauvegardes hors site sont gardées. Refusé pour MyInfra elle-même."),
		{
			Name: "get_destruction", Title: "Suivre une suppression", Role: store.RoleRead,
			Description: "L'avancement d'une suppression lancée : chaque étape et son état. Sans identifiant, les suppressions en cours.",
			Schema:      object(nil, map[string]any{"id": text("Identifiant rendu par l'outil de suppression.")}),
			Routes:      []string{"GET /api/destructions", "GET /api/destructions/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				path := "/api/destructions"
				if args.ID != "" {
					path += "/" + url.PathEscape(args.ID)
				}
				var out any
				if err := c.get(path, &out); err != nil {
					return "", err
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "resume_destruction", Title: "Reprendre une suppression", Role: store.RoleDeploy, Write: true,
			Description: "Relance une suppression en échec à partir de l'étape qui a échoué, après en avoir corrigé la cause.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant de la suppression.")}),
			Routes:      []string{"POST /api/destructions/{id}/resume"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				var out destructionView
				if err := c.send(http.MethodPost, "/api/destructions/"+url.PathEscape(args.ID)+"/resume", map[string]any{}, &out); err != nil {
					return "", err
				}
				return "Suppression relancée.\n" + asJSON(out), nil
			},
		},
		{
			Name: "retry_server_deploy", Title: "Relancer l'installation d'une machine", Role: store.RoleDeploy, Write: true,
			Description: "Relance l'installation d'une machine dont une étape a échoué (get_server montre les étapes).",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "POST /api/servers/{id}/deploy"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Server string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/servers/"+url.PathEscape(row.ID)+"/deploy", map[string]any{}, nil); err != nil {
					return "", err
				}
				return "Installation de " + row.Name + " relancée : suivez-la avec get_server.", nil
			},
		},
	}
	return append(tools, mcpOrderTools()...)
}

func (c *mcpCall) deletionTarget(kind, name string) (named, error) {
	switch kind {
	case "server":
		row, err := c.server(name)
		return named{row.ID, row.Name}, err
	case "cluster":
		return c.cluster(name)
	default:
		return c.application(name)
	}
}

var impactPath = map[string]string{"server": "/api/servers/", "cluster": "/api/clusters/", "application": "/api/applications/"}

func (c *mcpCall) impact(kind, id string) (deleteImpactView, error) {
	var out deleteImpactView
	err := c.get(impactPath[kind]+url.PathEscape(id)+"/delete/impact", &out)
	return out, err
}

// deletionTool deletes one thing in two calls: the impact, then the deletion
// with the name the API asks to type, as a person types it in the interface.
func deletionTool(kind, name, title, description string) mcpTool {
	label := map[string]string{"server": "Nom de la machine.", "cluster": "Nom du cluster.", "application": "Nom de l'application."}[kind]
	list := map[string]string{"server": "GET /api/servers", "cluster": "GET /api/clusters", "application": "GET /api/applications"}[kind]
	return mcpTool{
		Name: name, Title: title, Role: store.RoleDeploy, Write: true, Destructive: true,
		Description: description + " Deux appels : le premier rend ce qui sera détruit et un code ; le second, avec le code, après l'accord de l'utilisateur, lance la suppression.",
		Schema:      object([]string{"name"}, map[string]any{"name": text(label), "confirmation": confirmationArg}),
		Routes:      []string{list, "GET " + impactPath[kind] + "{id}/delete/impact", "DELETE " + impactPath[kind] + "{id}"},
		Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
			args, err := decodeArgs[struct{ Name, Confirmation string }](raw)
			if err != nil {
				return "", err
			}
			target, err := c.deletionTarget(kind, args.Name)
			if err != nil {
				// A code sent again once the deletion ran finds its target gone:
				// say the code is spent rather than the name unknown.
				return "", c.spentCode(args.Confirmation, err)
			}
			if target.ID == selfAppID {
				return "", failf("MyInfra ne se supprime pas par l'assistant : faites-le depuis l'interface, avec la phrase de confirmation.")
			}
			var impact deleteImpactView
			recap := func() (string, string, error) {
				var err error
				impact, err = c.impact(kind, target.ID)
				if err != nil {
					return "", "", err
				}
				if impact.HostsMyInfra {
					return "", "", failf("%s héberge MyInfra : sa suppression se fait depuis l'interface, avec la phrase « %s ».", impact.Name, selfDestroyPhrase)
				}
				return impact.recap(), fingerprintOf([]any{impact.Machines, impact.Applications, impact.Domains, impact.Steps}), nil
			}
			return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
				var out destructionView
				if err := c.send(http.MethodDelete, impactPath[kind]+url.PathEscape(target.ID), map[string]string{"name": target.Name}, &out); err != nil {
					return "", err
				}
				return "Suppression de « " + target.Name + " » lancée (identifiant " + out.ID + "). Suivez-la avec get_destruction.\n" + asJSON(out), nil
			})
		},
	}
}
