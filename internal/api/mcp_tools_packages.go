package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/pack"
	"myinfra/internal/store"
)

func packageBrief(p packageView) map[string]any {
	return map[string]any{"name": p.Name, "summary": p.Summary, "category": p.Category, "services": p.Services, "inputs": p.Inputs}
}

func problemsText(problems []pack.Problem) string {
	var lines []string
	for _, p := range problems {
		lines = append(lines, " - "+p.Error())
	}
	return strings.Join(lines, "\n")
}

func mcpPackageTools() []mcpTool {
	textArg := text("Le paquet en YAML (ou JSON), qui commence par « type: myinfra-package » et « version: 1 ». Le format est dans la ressource myinfra://guide/packages.")
	return []mcpTool{
		{
			Name: "list_packages", Title: "Paquets enregistrés", Role: store.RoleRead,
			Description: "Les paquets que la personne a écrits ou importés, prêts à installer.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/packages"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var rows []packageView
				if err := c.get("/api/packages", &rows); err != nil {
					return "", err
				}
				out := []map[string]any{}
				for _, row := range rows {
					out = append(out, packageBrief(row))
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "get_package", Title: "Lire un paquet", Role: store.RoleRead,
			Description: "Le YAML d'un paquet enregistré, pour le relire ou le modifier avec update_package.",
			Schema:      object([]string{"package"}, map[string]any{"package": text("Nom du paquet.")}),
			Routes:      []string{"GET /api/packages", "GET /api/packages/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Package string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.savedPackage(args.Package)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.get("/api/packages/"+url.PathEscape(row.ID), &view); err != nil {
					return "", err
				}
				return view.YAML, nil
			},
		},
		{
			Name: "check_package", Title: "Vérifier un paquet", Role: store.RoleRead,
			Description: "Vérifie un paquet sans l'enregistrer : chaque problème avec son chemin et sa ligne. À faire avant create_package.",
			Schema:      object([]string{"text"}, map[string]any{"text": textArg}),
			Routes:      []string{"POST /api/packages/check"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Text string }](raw)
				if err != nil {
					return "", err
				}
				var out struct {
					Problems []pack.Problem `json:"problems"`
					Readable bool           `json:"readable"`
				}
				if err := c.send(http.MethodPost, "/api/packages/check", map[string]string{"text": args.Text}, &out); err != nil {
					return "", err
				}
				if len(out.Problems) == 0 {
					return "Le paquet est valide : il peut être enregistré avec create_package.", nil
				}
				return "", failf("Le paquet a %d problème(s) :\n%s", len(out.Problems), problemsText(out.Problems))
			},
		},
		{
			Name: "convert_to_package", Title: "Convertir en paquet", Role: store.RoleDeploy,
			Description: "Transforme un docker-compose, des manifests Kubernetes (texte ou adresse https) ou un chart Helm en paquet MyInfra, sans l'enregistrer : relisez le YAML rendu et ses avertissements, puis create_package.",
			Schema: object(nil, map[string]any{
				"text": text("Un docker-compose.yml ou des manifests Kubernetes, en texte."),
				"url":  text("Adresse https d'un docker-compose ou de manifests."),
				"helm": object([]string{"repo", "chart"}, map[string]any{
					"repo":    text("Dépôt Helm, par exemple https://charts.bitnami.com/bitnami ou oci://registry-1.docker.io/bitnamicharts."),
					"chart":   text("Nom du chart, par exemple wordpress."),
					"version": text("Version du chart ; la dernière sinon."),
					"values":  text("values.yaml à appliquer, en texte."),
				}),
				"name": text("Nom à donner au paquet."),
			}),
			Routes: []string{"POST /api/packages/convert"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				var args map[string]any
				_ = json.Unmarshal(raw, &args)
				if args["text"] == nil && args["url"] == nil && args["helm"] == nil {
					return "", failf("Donnez text, url ou helm.")
				}
				var out struct {
					Format   string         `json:"format"`
					YAML     string         `json:"yaml"`
					Warnings []string       `json:"warnings"`
					Problems []pack.Problem `json:"problems"`
				}
				if err := c.send(http.MethodPost, "/api/packages/convert", args, &out); err != nil {
					return "", err
				}
				text := "Format lu : " + out.Format + "\n\n" + out.YAML
				if len(out.Warnings) > 0 {
					text += "\nAvertissements :\n - " + strings.Join(out.Warnings, "\n - ")
				}
				if len(out.Problems) > 0 {
					text += "\nÀ corriger avant d'enregistrer :\n" + problemsText(out.Problems)
				}
				return text, nil
			},
		},
		{
			Name: "create_package", Title: "Enregistrer un paquet", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un paquet valide, pour l'installer ensuite avec install (package=son nom).",
			Schema:      object([]string{"text"}, map[string]any{"text": textArg}),
			Routes:      []string{"POST /api/packages"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Text string }](raw)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.send(http.MethodPost, "/api/packages", map[string]string{"text": args.Text}, &view); err != nil {
					return "", err
				}
				return "Paquet « " + view.Name + " » enregistré (" + strings.Join(view.Services, ", ") + "). Installez-le avec install, package=" + view.Name + ".", nil
			},
		},
		{
			Name: "update_package", Title: "Modifier un paquet", Role: store.RoleDeploy, Write: true,
			Description: "Remplace le texte d'un paquet enregistré. Les applications déjà installées depuis lui ne changent pas.",
			Schema:      object([]string{"package", "text"}, map[string]any{"package": text("Nom actuel du paquet."), "text": textArg}),
			Routes:      []string{"GET /api/packages", "PUT /api/packages/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Package, Text string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.savedPackage(args.Package)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.send(http.MethodPut, "/api/packages/"+url.PathEscape(row.ID), map[string]string{"text": args.Text}, &view); err != nil {
					return "", err
				}
				return "Paquet « " + view.Name + " » mis à jour.", nil
			},
		},
		{
			Name: "delete_package", Title: "Supprimer un paquet", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Supprime un paquet enregistré ; les applications installées depuis lui restent. Deux appels : récapitulatif et code, puis suppression avec le code après l'accord de l'utilisateur.",
			Schema:      object([]string{"package"}, map[string]any{"package": text("Nom du paquet."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/packages", "DELETE /api/packages/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Package, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.savedPackage(args.Package)
				if err != nil {
					return "", err
				}
				recap := func() (string, string, error) {
					return "Suppression du paquet « " + row.Name + " ». Les applications déjà installées depuis lui restent en place.\n", row.ID, nil
				}
				return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
					if err := c.send(http.MethodDelete, "/api/packages/"+url.PathEscape(row.ID), nil, nil); err != nil {
						return "", err
					}
					return "Paquet « " + row.Name + " » supprimé.", nil
				})
			},
		},
	}
}
