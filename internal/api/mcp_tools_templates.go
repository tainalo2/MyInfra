package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/blueprint"
	"myinfra/internal/store"
)

func mcpTemplateTools() []mcpTool {
	bodyArg := loose("Corps du modèle : clusterName, servers (name, providerId, location, offerId, keyIds, controlPlane) et applications (name, sourceKind, source, catalog).")
	return []mcpTool{
		{
			Name: "list_templates", Title: "Lister les modèles", Role: store.RoleRead,
			Description: "Les modèles enregistrés : nom, nombre de machines et d'applications. Le détail se lit avec get_template.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/templates"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				rows, err := c.templates()
				if err != nil {
					return "", err
				}
				out := []map[string]any{}
				for _, row := range rows {
					out = append(out, map[string]any{"name": row.Name, "servers": len(row.Body.Servers), "applications": len(row.Body.Applications)})
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "get_template", Title: "Lire un modèle", Role: store.RoleRead,
			Description: "Un modèle en entier : machines, offres, applications. À relire avant de le lancer ou de le modifier.",
			Schema:      object([]string{"template"}, map[string]any{"template": text("Nom du modèle.")}),
			Routes:      []string{"GET /api/templates", "GET /api/templates/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.templateFrom(raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/templates/"+url.PathEscape(row.ID), nil)
			},
		},
		{
			Name: "export_template", Title: "Exporter un modèle", Role: store.RoleRead,
			Description: "Le fichier JSON d'un modèle, tel qu'on le réimporte.",
			Schema:      object([]string{"template"}, map[string]any{"template": text("Nom du modèle.")}),
			Routes:      []string{"GET /api/templates", "GET /api/templates/{id}/export"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.templateFrom(raw)
				if err != nil {
					return "", err
				}
				return c.callText(http.MethodGet, "/api/templates/"+url.PathEscape(row.ID)+"/export", nil)
			},
		},
		{
			Name: "list_starters", Title: "Modèles de départ", Role: store.RoleRead,
			Description: "Les modèles prêts de la bibliothèque : ce qu'ils contiennent, avant d'en faire un modèle à soi avec use_starter.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/starters"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var rows []starterView
				if err := c.get("/api/starters", &rows); err != nil {
					return "", err
				}
				for i := range rows {
					for j := range rows[i].Apps {
						rows[i].Apps[j].Logo = ""
					}
				}
				return asJSON(rows), nil
			},
		},
		{
			Name: "get_template_run", Title: "Suivre un lancement de modèle", Role: store.RoleRead,
			Description: "L'avancement d'un lancement de modèle : chaque machine et chaque application, avec son état.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant rendu par run_template.")}),
			Routes:      []string{"GET /api/template-runs/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/template-runs/"+url.PathEscape(args.ID), nil)
			},
		},
		{
			Name: "create_template", Title: "Enregistrer un modèle", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un modèle. body décrit les machines et les applications ; elles ne sont pas achetées ici. Pour les acheter, lancez le modèle avec run_template.",
			Schema:      object([]string{"name", "body"}, map[string]any{"name": text("Nom du modèle."), "body": bodyArg}),
			Routes:      []string{"POST /api/templates"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				payload, err := templatePayload(raw)
				if err != nil {
					return "", err
				}
				var view templateView
				if err := c.send(http.MethodPost, "/api/templates", payload, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » enregistré.", nil
			},
		},
		{
			Name: "update_template", Title: "Modifier un modèle", Role: store.RoleDeploy, Write: true,
			Description: "Remplace le corps d'un modèle déjà enregistré. Les lancements déjà partis ne changent pas.",
			Schema:      object([]string{"template", "body"}, map[string]any{"template": text("Nom actuel du modèle."), "name": text("Nouveau nom ; l'actuel sinon."), "body": bodyArg}),
			Routes:      []string{"GET /api/templates", "PUT /api/templates/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Template string          `json:"template"`
					Name     string          `json:"name"`
					Body     json.RawMessage `json:"body"`
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.template(args.Template)
				if err != nil {
					return "", err
				}
				name := args.Name
				if strings.TrimSpace(name) == "" {
					name = row.Name
				}
				payload, err := mergeTemplate(name, args.Body)
				if err != nil {
					return "", err
				}
				var view templateView
				if err := c.send(http.MethodPut, "/api/templates/"+url.PathEscape(row.ID), payload, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » mis à jour.", nil
			},
		},
		{
			Name: "delete_template", Title: "Supprimer un modèle", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Supprime un modèle enregistré. Les machines et applications déjà lancées depuis lui restent. Deux appels : récapitulatif et code, puis suppression après l'accord.",
			Schema:      object([]string{"template"}, map[string]any{"template": text("Nom du modèle."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/templates", "DELETE /api/templates/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Template, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.template(args.Template)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Suppression du modèle « " + row.Name + " ». Ce qui a déjà été lancé depuis lui reste.\n", row.ID, nil
				}, func() (string, error) {
					if err := c.send(http.MethodDelete, "/api/templates/"+url.PathEscape(row.ID), nil, nil); err != nil {
						return "", err
					}
					return "Modèle « " + row.Name + " » supprimé.", nil
				})
			},
		},
		{
			Name: "import_template", Title: "Importer un modèle", Role: store.RoleDeploy, Write: true,
			Description: "Importe le fichier JSON d'un modèle (celui de export_template). Les comptes et les zones sont reliés par leur nom quand ils existent ici.",
			Schema:      object([]string{"document"}, map[string]any{"document": loose("Le fichier JSON du modèle, objet complet.")}),
			Routes:      []string{"POST /api/templates/import"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Document json.RawMessage `json:"document"`
				}](raw)
				if err != nil {
					return "", err
				}
				var doc map[string]any
				if err := json.Unmarshal(args.Document, &doc); err != nil || doc == nil {
					return "", failf("Le document du modèle est illisible.")
				}
				var view templateView
				if err := c.send(http.MethodPost, "/api/templates/import", doc, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » importé.", nil
			},
		},
		{
			Name: "import_template_url", Title: "Importer un modèle par URL", Role: store.RoleDeploy, Write: true,
			Description: "Importe un modèle depuis une adresse https qui rend son fichier JSON.",
			Schema:      object([]string{"url"}, map[string]any{"url": text("Adresse https du fichier JSON.")}),
			Routes:      []string{"POST /api/templates/import-url"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ URL string }](raw)
				if err != nil {
					return "", err
				}
				var view templateView
				if err := c.send(http.MethodPost, "/api/templates/import-url", map[string]string{"url": args.URL}, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » importé.", nil
			},
		},
		{
			Name: "template_from_cluster", Title: "Modèle depuis un cluster", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un modèle qui décrit un cluster créé par MyInfra, tel qu'il est : ses machines et ses applications. Un cluster importé ne se copie pas ainsi.",
			Schema:      object([]string{"cluster"}, map[string]any{"cluster": text("Nom du cluster.")}),
			Routes:      []string{"GET /api/clusters", "POST /api/templates/from-cluster"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Cluster string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.cluster(args.Cluster)
				if err != nil {
					return "", err
				}
				var view templateView
				if err := c.send(http.MethodPost, "/api/templates/from-cluster", map[string]string{"clusterId": row.ID}, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » enregistré depuis le cluster " + row.Name + ".", nil
			},
		},
		{
			Name: "use_starter", Title: "Partir d'un modèle de départ", Role: store.RoleDeploy, Write: true,
			Description: "Copie un modèle de la bibliothèque (list_starters) parmi les modèles de la personne. Il reste à compléter providers, offres et domaines avant de le lancer.",
			Schema:      object([]string{"starter", "name"}, map[string]any{"starter": text("Identifiant du modèle de départ."), "name": text("Nom du modèle à créer.")}),
			Routes:      []string{"POST /api/starters/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Starter, Name string }](raw)
				if err != nil {
					return "", err
				}
				var view templateView
				if err := c.send(http.MethodPost, "/api/starters/"+url.PathEscape(args.Starter), map[string]string{"name": args.Name}, &view); err != nil {
					return "", err
				}
				return "Modèle « " + view.Name + " » créé depuis " + args.Starter + ". Complétez-le avant de le lancer.", nil
			},
		},
		{
			Name: "run_template", Title: "Lancer un modèle", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Lance un modèle : achète les machines qui y sont décrites et installe ses applications. Chaque machine doit déjà avoir son provider, son offre et une clé SSH (get_template, puis update_template). body remplace le corps enregistré pour ce lancement seulement. Deux appels : le récapitulatif et un code, puis le lancement après l'accord explicite de l'utilisateur.",
			Schema: object([]string{"template"}, map[string]any{
				"template":       text("Nom du modèle."),
				"body":           bodyArg,
				"target_cluster": text("Cluster existant où installer les applications, au lieu d'en créer un."),
				"confirmation":   confirmationArg,
			}),
			Routes: []string{"GET /api/templates", "GET /api/clusters", "POST /api/templates/{id}/runs"},
			Run:    runTemplate,
		},
		{
			Name: "resume_template_run", Title: "Reprendre un lancement", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Reprend un lancement de modèle en échec. Deux appels : récapitulatif et code, puis reprise après l'accord.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant du lancement."), "confirmation": confirmationArg}),
			Routes:      []string{"POST /api/template-runs/{id}/resume"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Reprise du lancement " + args.ID + ".\n", args.ID, nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPost, "/api/template-runs/"+url.PathEscape(args.ID)+"/resume", map[string]any{})
				})
			},
		},
	}
}

func (c *mcpCall) templates() ([]templateView, error) {
	var rows []templateView
	err := c.get("/api/templates", &rows)
	return rows, err
}

func (c *mcpCall) template(name string) (templateView, error) {
	rows, err := c.templates()
	if err != nil {
		return templateView{}, err
	}
	return pick("modèle", name, rows, func(row templateView) named { return named{row.ID, row.Name} })
}

func (c *mcpCall) templateFrom(raw json.RawMessage) (templateView, error) {
	args, err := decodeArgs[struct{ Template string }](raw)
	if err != nil {
		return templateView{}, err
	}
	return c.template(args.Template)
}

func templatePayload(raw json.RawMessage) (map[string]any, error) {
	args, err := decodeArgs[struct {
		Name string          `json:"name"`
		Body json.RawMessage `json:"body"`
	}](raw)
	if err != nil {
		return nil, err
	}
	return mergeTemplate(args.Name, args.Body)
}

func mergeTemplate(name string, body json.RawMessage) (map[string]any, error) {
	payload := map[string]any{}
	if len(bytesTrim(body)) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
			return nil, failf("Le corps du modèle est illisible.")
		}
	}
	payload["name"] = name
	return payload, nil
}

func bytesTrim(raw json.RawMessage) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func runTemplate(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Template      string          `json:"template"`
		Body          json.RawMessage `json:"body"`
		TargetCluster string          `json:"target_cluster"`
		Confirmation  string          `json:"confirmation"`
	}](raw)
	if err != nil {
		return "", err
	}
	row, err := c.template(args.Template)
	if err != nil {
		return "", err
	}
	payload := map[string]any{}
	if len(bytesTrim(args.Body)) > 0 {
		if err := json.Unmarshal(args.Body, &payload); err != nil || payload == nil {
			return "", failf("Le corps du modèle est illisible.")
		}
	} else {
		encoded, err := json.Marshal(row.Body)
		if err != nil {
			return "", err
		}
		_ = json.Unmarshal(encoded, &payload)
	}
	label := row.Name
	if strings.TrimSpace(args.TargetCluster) != "" {
		cluster, err := c.cluster(args.TargetCluster)
		if err != nil {
			return "", err
		}
		payload["targetClusterId"] = cluster.ID
		delete(payload, "clusterName")
		label = row.Name + " sur " + cluster.Name
	}
	return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
		return templateRecap(label, payload), fingerprintOf(payload), nil
	}, func() (string, error) {
		var view runView
		if err := c.send(http.MethodPost, "/api/templates/"+url.PathEscape(row.ID)+"/runs", payload, &view); err != nil {
			return "", err
		}
		return "Lancement de « " + row.Name + " » parti (identifiant " + view.ID + "). Suivez-le avec get_template_run.", nil
	})
}

func templateRecap(label string, payload map[string]any) string {
	raw, _ := json.Marshal(payload)
	var body blueprint.Body
	_ = json.Unmarshal(raw, &body)
	var b strings.Builder
	b.WriteString("Lancement du modèle « " + label + " ».\n")
	if len(body.Servers) == 0 {
		b.WriteString("Aucune machine à acheter.\n")
	}
	for _, server := range body.Servers {
		b.WriteString("Machine " + server.Name)
		if server.OfferID != "" {
			b.WriteString(", offre " + server.OfferID)
		} else {
			b.WriteString(", offre à compléter")
		}
		if server.ControlPlane {
			b.WriteString(", plan de contrôle")
		}
		b.WriteString(".\n")
	}
	for _, app := range body.Applications {
		b.WriteString("Application " + app.Name + ".\n")
	}
	if body.ClusterName != "" {
		b.WriteString("Cluster créé : " + body.ClusterName + ".\n")
	}
	return b.String()
}
